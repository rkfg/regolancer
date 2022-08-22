package main

import (
	"context"
	"encoding/json"
	"log"
	"math/rand"
	"os"
	"time"

	"github.com/jessevdk/go-flags"
	"github.com/lightninglabs/lndclient"
	"github.com/lightningnetwork/lnd/lnrpc"
	"github.com/lightningnetwork/lnd/lnrpc/routerrpc"
)

var mainParams struct {
	Config string `short:"f" long:"config" description:"config file path"`
}

var params struct {
	Config             string   `short:"f" long:"config" description:"config file path"`
	Connect            string   `short:"c" long:"connect" description:"connect to lnd using host:port" json:"connect"`
	TLSCert            string   `short:"t" long:"tlscert" description:"path to tls.cert to connect" required:"false" json:"tlscert"`
	MacaroonDir        string   `long:"macaroon-dir" description:"path to the macaroon directory" required:"false" json:"macaroon_dir"`
	MacaroonFilename   string   `long:"macaroon-filename" description:"macaroon filename" json:"macaroon_filename"`
	Network            string   `short:"n" long:"network" description:"bitcoin network to use" json:"network"`
	FromPerc           int64    `long:"pfrom" description:"channels with less than this inbound liquidity percentage will be considered as source channels" json:"pfrom"`
	ToPerc             int64    `long:"pto" description:"channels with less than this outbound liquidity percentage will be considered as target channels" json:"pto"`
	Perc               int64    `short:"p" long:"perc" description:"use this value as both pfrom and pto from above" json:"perc"`
	Amount             int64    `short:"a" long:"amount" description:"amount to rebalance" json:"amount"`
	EconRatio          float64  `long:"econ-ratio" description:"economical ratio for fee limit calculation as a multiple of target channel fee (for example, 0.5 means you want to pay at max half the fee you might earn for routing out of the target channel)" json:"econ_ratio"`
	ProbeSteps         int      `short:"b" long:"probe-steps" description:"if the payment fails at the last hop try to probe lower amount using this many steps" json:"probe_steps"`
	ExcludeChannelsIn  []uint64 `short:"i" long:"exclude-channel-in" description:"don't use this channel as incoming (can be specified multiple times)" json:"exclude_channels_in"`
	ExcludeChannelsOut []uint64 `short:"o" long:"exclude-channel-out" description:"don't use this channel as outgoing (can be specified multiple times)" json:"exclude_channels_out"`
	ExcludeChannels    []uint64 `short:"e" long:"exclude-channel" description:"don't use this channel at all (can be specified multiple times)" json:"exclude_channels"`
	ExcludeNodes       []string `short:"d" long:"exclude-node" description:"don't use this node for routing (can be specified multiple times)" json:"exclude_nodes"`
	ToChannel          uint64   `long:"to" description:"try only this channel as target (should satisfy other constraints too)" json:"to"`
	FromChannel        uint64   `long:"from" description:"try only this channel as source (should satisfy other constraints too)" json:"from"`
}

type regolancer struct {
	lnClient      lnrpc.LightningClient
	routerClient  routerrpc.RouterClient
	myPK          string
	channels      []*lnrpc.Channel
	fromChannels  []*lnrpc.Channel
	fromChannelId uint64
	toChannels    []*lnrpc.Channel
	toChannelId   uint64
	nodeCache     map[string]*lnrpc.NodeInfo
	chanCache     map[uint64]*lnrpc.ChannelEdge
	failureCache  map[string]*time.Time
	excludeIn     map[uint64]struct{}
	excludeOut    map[uint64]struct{}
	excludeBoth   map[uint64]struct{}
	excludeNodes  [][]byte
}

func loadConfig() {
	flags.NewParser(&mainParams, flags.PrintErrors|flags.IgnoreUnknown).Parse()
	if mainParams.Config == "" {
		return
	}
	f, err := os.Open(mainParams.Config)
	if err != nil {
		log.Fatalf("Error opening config file %s: %s", mainParams.Config, err)
	} else {
		defer f.Close()
		err = json.NewDecoder(f).Decode(&params)
		if err != nil {
			log.Fatalf("Error reading config file %s: %s", mainParams.Config, err)
		}
	}
}

func tryRebalance(ctx context.Context, r *regolancer, invoice **lnrpc.AddInvoiceResponse,
	attempt *int) (err error, repeat bool) {
	routeCtx, routeCtxCancel := context.WithTimeout(ctx, time.Second*30)
	defer routeCtxCancel()
	from, to, amt, err := r.pickChannelPair(routeCtx, params.Amount)
	if err != nil {
		log.Printf(errColor("Error during picking channel: %s"), err)
		return err, false
	}
	routes, fee, err := r.getRoutes(routeCtx, from, to, amt*1000, params.EconRatio)
	if err != nil {
		if routeCtx.Err() == context.DeadlineExceeded {
			log.Print(errColor("Timed out looking for a route"))
			return err, false
		}
		r.addFailedRoute(from, to)
		return err, true
	}
	routeCtxCancel()
	if params.Amount == 0 || *invoice == nil {
		*invoice, err = r.createInvoice(ctx, from, to, amt)
		if err != nil {
			log.Printf("Error creating invoice: %s", err)
			return err, true
		}
	}
	for _, route := range routes {
		log.Printf("Attempt %s, amount: %s (max fee: %s)", hiWhiteColorF("#%d", *attempt),
			hiWhiteColor(amt), hiWhiteColor(fee/1000))
		r.printRoute(ctx, route)
		err = r.pay(ctx, *invoice, amt, route, params.ProbeSteps)
		if err == nil {
			return nil, false
		}
		if retryErr, ok := err.(ErrRetry); ok {
			amt = retryErr.amount
			log.Printf("Trying to rebalance again with %s", hiWhiteColor(amt))
			probedInvoice, err := r.createInvoice(ctx, from, to, amt)
			if err != nil {
				log.Printf("Error creating invoice: %s", err)
				return err, true
			}
			probedRoute, err := r.rebuildRoute(ctx, route, amt)
			if err != nil {
				log.Printf("Error rebuilding the route for probed payment: %s", errColor(err))
			} else {
				err = r.pay(ctx, probedInvoice, amt, probedRoute, 0)
				if err == nil {
					return nil, false
				} else {
					log.Printf("Probed rebalance failed with error: %s", errColor(err))
				}
			}
		}
		*attempt++
	}
	return nil, true
}

func main() {
	rand.Seed(time.Now().UnixNano())
	loadConfig()
	_, err := flags.NewParser(&params, flags.Default|flags.IgnoreUnknown).Parse()
	if err != nil {
		os.Exit(1)
	}
	if params.Connect == "" {
		params.Connect = "127.0.0.1:10009"
	}
	if params.MacaroonFilename == "" {
		params.MacaroonFilename = "admin.macaroon"
	}
	if params.Network == "" {
		params.Network = "mainnet"
	}
	if params.FromPerc == 0 {
		params.FromPerc = 50
	}
	if params.ToPerc == 0 {
		params.ToPerc = 50
	}
	if params.EconRatio == 0 {
		params.EconRatio = 1
	}
	if params.Perc > 0 {
		params.FromPerc = params.Perc
		params.ToPerc = params.Perc
	}
	conn, err := lndclient.NewBasicConn(params.Connect, params.TLSCert, params.MacaroonDir, params.Network,
		lndclient.MacFilename(params.MacaroonFilename))
	if err != nil {
		log.Fatal(err)
	}
	r := regolancer{
		nodeCache:    map[string]*lnrpc.NodeInfo{},
		chanCache:    map[uint64]*lnrpc.ChannelEdge{},
		failureCache: map[string]*time.Time{},
	}
	r.lnClient = lnrpc.NewLightningClient(conn)
	r.routerClient = routerrpc.NewRouterClient(conn)
	mainCtx, mainCtxCancel := context.WithTimeout(context.Background(), time.Hour*6)
	defer mainCtxCancel()
	infoCtx, infoCtxCancel := context.WithTimeout(mainCtx, time.Second*30)
	defer infoCtxCancel()
	info, err := r.lnClient.GetInfo(infoCtx, &lnrpc.GetInfoRequest{})
	if err != nil {
		log.Fatal(err)
	}
	r.myPK = info.IdentityPubkey
	err = r.getChannels(infoCtx)
	if err != nil {
		log.Fatal("Error listing own channels: ", err)
	}
	if params.FromChannel > 0 {
		r.fromChannelId = params.FromChannel
	}
	if params.ToChannel > 0 {
		r.toChannelId = params.ToChannel
	}
	r.excludeIn = makeChanSet(params.ExcludeChannelsIn)
	r.excludeOut = makeChanSet(params.ExcludeChannelsOut)
	r.excludeBoth = makeChanSet(params.ExcludeChannels)
	err = r.makeNodeList(params.ExcludeNodes)
	if err != nil {
		log.Fatal("Error parsing excluded node list: ", err)
	}
	err = r.getChannelCandidates(params.FromPerc, params.ToPerc, params.Amount)
	if err != nil {
		log.Fatal("Error choosing channels: ", err)
	}
	if len(r.fromChannels) == 0 {
		log.Fatal("No source channels selected")
	}
	if len(r.toChannels) == 0 {
		log.Fatal("No target channels selected")
	}
	infoCtxCancel()
	var invoice *lnrpc.AddInvoiceResponse
	attempt := 1
	for {
		attemptCtx, attemptCancel := context.WithTimeout(mainCtx, time.Minute*5)
		_, retry := tryRebalance(attemptCtx, &r, &invoice, &attempt)
		attemptCancel()
		if attemptCtx.Err() == context.DeadlineExceeded {
			log.Print(errColor("Attempt timed out"))
		}
		if mainCtx.Err() == context.DeadlineExceeded {
			log.Println(errColor("Rebalancing timed out"))
			return
		}
		invoice = nil // create a new invoice next time
		if !retry {
			return
		}
	}
}
