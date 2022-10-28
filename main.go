package main

import (
	"context"
	"encoding/json"
	"log"
	"math/rand"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/jessevdk/go-flags"
	"github.com/lightninglabs/lndclient"
	"github.com/lightningnetwork/lnd/lnrpc"
	"github.com/lightningnetwork/lnd/lnrpc/routerrpc"
)

type configParams struct {
	Config             string   `short:"f" long:"config" description:"config file path"`
	Connect            string   `short:"c" long:"connect" description:"connect to lnd using host:port" json:"connect" toml:"connect"`
	TLSCert            string   `short:"t" long:"tlscert" description:"path to tls.cert to connect" required:"false" json:"tlscert" toml:"tlscert"`
	MacaroonDir        string   `long:"macaroon-dir" description:"path to the macaroon directory" required:"false" json:"macaroon_dir" toml:"macaroon_dir"`
	MacaroonFilename   string   `long:"macaroon-filename" description:"macaroon filename" json:"macaroon_filename" toml:"macaroon_filename"`
	Network            string   `short:"n" long:"network" description:"bitcoin network to use" json:"network" toml:"network"`
	FromPerc           int64    `long:"pfrom" description:"channels with less than this inbound liquidity percentage will be considered as source channels" json:"pfrom" toml:"pfrom"`
	ToPerc             int64    `long:"pto" description:"channels with less than this outbound liquidity percentage will be considered as target channels" json:"pto" toml:"pto"`
	Perc               int64    `short:"p" long:"perc" description:"use this value as both pfrom and pto from above" json:"perc" toml:"perc"`
	Amount             int64    `short:"a" long:"amount" description:"amount to rebalance" json:"amount" toml:"amount"`
	RelAmountTo        float64  `long:"rel-amount-to" description:"calculate amount as the target channel capacity fraction (for example, 0.2 means you want to achieve at most 20% target channel local balance)"`
	RelAmountFrom      float64  `long:"rel-amount-from" description:"calculate amount as the source channel capacity fraction (for example, 0.2 means you want to achieve at most 20% source channel remote balance)"`
	EconRatio          float64  `short:"r" long:"econ-ratio" description:"economical ratio for fee limit calculation as a multiple of target channel fee (for example, 0.5 means you want to pay at max half the fee you might earn for routing out of the target channel)" json:"econ_ratio" toml:"econ_ratio"`
	EconRatioMaxPPM    int64    `long:"econ-ratio-max-ppm" description:"limits the max fee ppm for a rebalance when using econ ratio" json:"econ_ratio_max_ppm" toml:"econ_ratio_max_ppm"`
	FeeLimitPPM        int64    `short:"F" long:"fee-limit-ppm" description:"don't consider the target channel fee and use this max fee ppm instead (can rebalance at a loss, be careful)" json:"fee_limit_ppm" toml:"fee_limit_ppm"`
	LostProfit         bool     `short:"l" long:"lost-profit" description:"also consider the outbound channel fees when looking for profitable routes so that outbound_fee+inbound_fee < route_fee" json:"lost_profit" toml:"lost_profit"`
	ProbeSteps         int      `short:"b" long:"probe-steps" description:"if the payment fails at the last hop try to probe lower amount using this many steps" json:"probe_steps" toml:"probe_steps"`
	MinAmount          int64    `long:"min-amount" description:"if probing is enabled this will be the minimum amount to try" json:"min_amount" toml:"min_amount"`
	ExcludeChannelsIn  []string `short:"i" long:"exclude-channel-in" description:"don't use this channel as incoming (can be specified multiple times)" json:"exclude_channels_in" toml:"exclude_channels_in"`
	ExcludeChannelsOut []string `short:"o" long:"exclude-channel-out" description:"don't use this channel as outgoing (can be specified multiple times)" json:"exclude_channels_out" toml:"exclude_channels_out"`
	ExcludeChannels    []string `short:"e" long:"exclude-channel" description:"don't use this channel at all (can be specified multiple times)" json:"exclude_channels" toml:"exclude_channels"`
	ExcludeNodes       []string `short:"d" long:"exclude-node" description:"don't use this node for routing (can be specified multiple times)" json:"exclude_nodes" toml:"exclude_nodes"`
	ToChannel          []string `long:"to" description:"try only this channel as target (should satisfy other constraints too; can be specified multiple times)" json:"to" toml:"to"`
	FromChannel        []string `long:"from" description:"try only this channel as source (should satisfy other constraints too; can be specified multiple times)" json:"from" toml:"from"`
	AllowUnbalanceFrom bool     `long:"allow-unbalance-from" description:"let the source channel go below 50% local liquidity, use if you want to drain a channel; you should also set --pfrom to >50" json:"allow_unbalance_from" toml:"allow_unbalance_from"`
	AllowUnbalanceTo   bool     `long:"allow-unbalance-to" description:"let the target channel go above 50% local liquidity, use if you want to refill a channel; you should also set --pto to >50" json:"allow_unbalance_to" toml:"allow_unbalance_to"`
	StatFilename       string   `short:"s" long:"stat" description:"save successful rebalance information to the specified CSV file" json:"stat" toml:"stat"`
	NodeCacheFilename  string   `long:"node-cache-filename" description:"save and load other nodes information to this file, improves cold start performance"  json:"node_cache_filename" toml:"node_cache_filename"`
	NodeCacheLifetime  int      `long:"node-cache-lifetime" description:"nodes with last update older than this time (in minutes) will be removed from cache after loading it" json:"node_cache_lifetime" toml:"node_cache_lifetime" default:"1440"`
	NodeCacheInfo      bool     `long:"node-cache-info" description:"show red and cyan 'x' characters in routes to indicate node cache misses and hits respectively" json:"node_cache_info" toml:"node_cache_info"`
}

var params, cfgParams configParams

type failedRoute struct {
	channelPair [2]*lnrpc.Channel
	expiration  *time.Time
}

type cachedNodeInfo struct {
	*lnrpc.NodeInfo
	Timestamp time.Time
}

type regolancer struct {
	lnClient      lnrpc.LightningClient
	routerClient  routerrpc.RouterClient
	myPK          string
	channels      []*lnrpc.Channel
	fromChannels  []*lnrpc.Channel
	fromChannelId map[uint64]struct{}
	toChannels    []*lnrpc.Channel
	toChannelId   map[uint64]struct{}
	channelPairs  map[string][2]*lnrpc.Channel
	nodeCache     map[string]cachedNodeInfo
	chanCache     map[uint64]*lnrpc.ChannelEdge
	failureCache  map[string]failedRoute
	excludeIn     map[uint64]struct{}
	excludeOut    map[uint64]struct{}
	excludeBoth   map[uint64]struct{}
	excludeNodes  [][]byte
	statFilename  string
	routeFound    bool
	invoiceCache  map[int64]*lnrpc.AddInvoiceResponse
}

func loadConfig() {
	flags.NewParser(&cfgParams, flags.None).Parse()

	if cfgParams.Config == "" {
		return
	}
	if strings.Contains(cfgParams.Config, ".toml") {
		_, err := toml.DecodeFile(cfgParams.Config, &params)

		if err != nil {
			if strings.Contains(err.Error(), "TOML value of type int64 into a Go string") {
				log.Print(infoColor("Info: all prior int channel arrays are now string arrays. " +
					"Make sure the following arguments in the config files are now strings:\n" +
					"ExcludeChannelsIn,ExcludeChannelsOut, ExcludeChannels,ToChannel, FromChannel"))
			}
			log.Fatalf("Error opening config file %s: %s", cfgParams.Config, err.Error())
		}

	} else {
		f, err := os.Open(cfgParams.Config)
		if err != nil {
			log.Fatalf("Error opening config file %s: %s", cfgParams.Config, err)
		} else {
			defer f.Close()
			decoder := json.NewDecoder(f)
			decoder.UseNumber()
			err := decoder.Decode(&params)
			if err != nil {
				if strings.Contains(err.Error(), "cannot unmarshal number into Go struct field") {
					log.Print(infoColor("Info: all prior int channel arrays are now string arrays. " +
						"Make sure the following arguments in the config files are now strings:\n" +
						"ExcludeChannelsIn,ExcludeChannelsOut, ExcludeChannels,ToChannel, FromChannel"))
				}
				log.Fatalf("Error reading config file %s: %s", cfgParams.Config, err)
			}
		}
	}
}

func convertChanStringToInt(chanIds []string) (channels []uint64) {

	for _, cid := range chanIds {

		chanId, err := strconv.ParseInt(cid, 10, 64)

		if err != nil {

			isScid := strings.Count(strings.ToLower(cid), "x") == 2
			if isScid {
				chanId = parseScid(cid)

			} else {
				log.Fatalf("error: parsing Channel with Id %s, %s ", cid, err)

			}
		}
		channels = append(channels, uint64(chanId))

	}

	return channels

}

func tryRebalance(ctx context.Context, r *regolancer, attempt *int) (err error,
	repeat bool) {
	from, to, amt, err := r.pickChannelPair(params.Amount, params.MinAmount, params.RelAmountFrom, params.RelAmountTo)
	if err != nil {
		log.Printf(errColor("Error during picking channel: %s"), err)
		return err, false
	}
	routeCtx, routeCtxCancel := context.WithTimeout(ctx, time.Second*30)
	defer routeCtxCancel()
	routes, fee, err := r.getRoutes(routeCtx, from, to, amt*1000)
	if err != nil {
		if routeCtx.Err() == context.DeadlineExceeded {
			log.Print(errColor("Timed out looking for a route"))
			return err, false
		}
		r.addFailedRoute(from, to)
		return err, true
	}
	routeCtxCancel()
	for _, route := range routes {
		log.Printf("Attempt %s, amount: %s (max fee: %s sat | %s ppm )",
			hiWhiteColorF("#%d", *attempt), hiWhiteColor(amt), formatFee(fee), formatFeePPM(amt*1000, fee))
		r.printRoute(ctx, route)
		err = r.pay(ctx, amt, params.MinAmount, route, params.ProbeSteps)
		if err == nil {
			return nil, false
		}
		if retryErr, ok := err.(ErrRetry); ok {
			amt = retryErr.amount
			log.Printf("Trying to rebalance again with %s", hiWhiteColor(amt))
			probedRoute, err := r.rebuildRoute(ctx, route, amt)
			if err != nil {
				log.Printf("Error rebuilding the route for probed payment: %s", errColor(err))
			} else {
				err = r.pay(ctx, amt, 0, probedRoute, 0)
				if err == nil {
					return nil, false
				} else {
					r.invalidateInvoice(amt)
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
	_, err := flags.Parse(&params)
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
	if params.EconRatio == 0 && params.FeeLimitPPM == 0 {
		params.EconRatio = 1
	}
	if params.EconRatioMaxPPM != 0 && params.FeeLimitPPM != 0 {
		log.Fatalf(errColor("Error EconRatioMaxPPM and FeeLimitPPM not allowed at the same time (safety precaution)"))
	}
	if params.Perc > 0 {
		params.FromPerc = params.Perc
		params.ToPerc = params.Perc
	}
	if params.MinAmount > 0 && params.Amount > 0 &&
		params.MinAmount > params.Amount {
		log.Fatal("Minimum amount should be less than amount")
	}
	if params.Amount > 0 &&
		(params.RelAmountFrom > 0 || params.RelAmountTo > 0) {
		log.Fatal("Use either precise amount or relative amounts but not both.")
	}
	conn, err := lndclient.NewBasicConn(params.Connect, params.TLSCert, params.MacaroonDir, params.Network,
		lndclient.MacFilename(params.MacaroonFilename))
	if err != nil {
		log.Fatal(err)
	}
	r := regolancer{
		nodeCache:    map[string]cachedNodeInfo{},
		chanCache:    map[uint64]*lnrpc.ChannelEdge{},
		channelPairs: map[string][2]*lnrpc.Channel{},
		failureCache: map[string]failedRoute{},
		statFilename: params.StatFilename,
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
	if len(params.FromChannel) > 0 {
		r.fromChannelId = makeChanSet(convertChanStringToInt(params.FromChannel))
	}
	if len(params.ToChannel) > 0 {
		r.toChannelId = makeChanSet(convertChanStringToInt(params.ToChannel))
	}

	r.excludeIn = makeChanSet(convertChanStringToInt(params.ExcludeChannelsIn))
	r.excludeOut = makeChanSet(convertChanStringToInt(params.ExcludeChannelsOut))
	r.excludeBoth = makeChanSet(convertChanStringToInt(params.ExcludeChannels))

	r.invoiceCache = map[int64]*lnrpc.AddInvoiceResponse{}

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
	attempt := 1

	err = r.loadNodeCache(params.NodeCacheFilename, params.NodeCacheLifetime,
		true)
	if err != nil {
		logErrorF("%s", err)
	}
	defer r.saveNodeCache(params.NodeCacheFilename, params.NodeCacheLifetime)
	stopChan := make(chan os.Signal)
	signal.Notify(stopChan, os.Interrupt)
	go func() {
		<-stopChan
		r.saveNodeCache(params.NodeCacheFilename, params.NodeCacheLifetime)
		os.Exit(1)
	}()

	for {
		attemptCtx, attemptCancel := context.WithTimeout(mainCtx, time.Minute*5)
		_, retry := tryRebalance(attemptCtx, &r, &attempt)
		attemptCancel()
		if attemptCtx.Err() == context.DeadlineExceeded {
			log.Print(errColor("Attempt timed out"))
		}
		if mainCtx.Err() == context.DeadlineExceeded {
			log.Println(errColor("Rebalancing timed out"))
			return
		}
		if !retry {
			return
		}
	}
}
