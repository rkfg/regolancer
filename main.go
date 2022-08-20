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
	Connect          string  `short:"c" long:"connect" description:"connect to lnd using host:port" json:"connect"`
	TLSCert          string  `short:"t" long:"tlscert" description:"path to tls.cert to connect" required:"false" json:"tlscert"`
	MacaroonDir      string  `long:"macaroon.dir" description:"path to the macaroon directory" required:"false" json:"macaroon.dir"`
	MacaroonFilename string  `long:"macaroon.filename" description:"macaroon filename" json:"macaroon.filename"`
	Network          string  `short:"n" long:"network" description:"bitcoin network to use" json:"network"`
	FromPerc         int64   `long:"pfrom" description:"channels with less than this inbound liquidity percentage will be considered as source channels" json:"pfrom"`
	ToPerc           int64   `long:"pto" description:"channels with less than this outbound liquidity percentage will be considered as target channels" json:"pto"`
	Perc             int64   `short:"p" long:"perc" description:"use this value as both pfrom and pto from above" json:"perc"`
	Amount           int64   `short:"a" long:"amount" description:"amount to rebalance" json:"amount"`
	EconRatio        float64 `long:"econ-ratio" description:"economical ratio for fee limit calculation as a multiple of target channel fee (for example, 0.5 means you want to pay at max half the fee you might earn for routing out of the target channel)" json:"econ_ratio"`
	ProbeSteps       int     `short:"b" long:"probe" description:"if the payment fails at the last hop try to probe lower amount using binary search" json:"probe_steps"`
}

type regolancer struct {
	lnClient     lnrpc.LightningClient
	routerClient routerrpc.RouterClient
	myPK         string
	channels     []*lnrpc.Channel
	fromChannels []*lnrpc.Channel
	toChannels   []*lnrpc.Channel
	nodeCache    map[string]*lnrpc.NodeInfo
	chanCache    map[uint64]*lnrpc.ChannelEdge
}

func loadConfig() {
	flags.Parse(&mainParams)
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
	r := regolancer{nodeCache: map[string]*lnrpc.NodeInfo{}, chanCache: map[uint64]*lnrpc.ChannelEdge{}}
	r.lnClient = lnrpc.NewLightningClient(conn)
	r.routerClient = routerrpc.NewRouterClient(conn)
	info, err := r.lnClient.GetInfo(context.Background(), &lnrpc.GetInfoRequest{})
	if err != nil {
		log.Fatal(err)
	}
	r.myPK = info.IdentityPubkey
	err = r.getChannels()
	if err != nil {
		log.Fatal("Error listing own channels: ", err)
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
	var invoice *lnrpc.AddInvoiceResponse
	attempt := 1
	for {
		from, to, amt := r.pickChannelPair(params.Amount)
		if params.Amount == 0 || invoice == nil {
			invoice, err = r.createInvoice(from, to, amt)
			if err != nil {
				log.Fatal("Error creating invoice: ", err)
			}
		}
		routes, fee, err := r.getRoutes(from, to, amt*1000, params.EconRatio)
		if err != nil {
			continue
		}
		for _, route := range routes {
			log.Printf("Attempt %s, amount: %s (max fee: %s)", hiWhiteColorF("#%d", attempt),
				hiWhiteColor(amt), hiWhiteColor(fee/1000))
			r.printRoute(route)
			err = r.pay(invoice, amt, route, params.ProbeSteps)
			if err == nil {
				return
			}
			if retryErr, ok := err.(ErrRetry); ok {
				amt = retryErr.amount
				log.Printf("Trying to rebalance again with %s", hiWhiteColor(amt))
				probedInvoice, err := r.createInvoice(from, to, amt)
				if err != nil {
					log.Fatal("Error creating invoice: ", err)
				}
				if err != nil {
					log.Printf("Error rebuilding the route for probed payment: %s", errColor(err))
				} else {
					err = r.pay(probedInvoice, amt, retryErr.route, 0)
					if err == nil {
						return
					} else {
						log.Printf("Probed rebalance failed with error: %s", errColor(err))
					}
				}
			}
			attempt++
		}
	}
}
