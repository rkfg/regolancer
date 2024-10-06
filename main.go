package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"os"
	"os/signal"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/jessevdk/go-flags"
	"github.com/lightninglabs/lndclient"
	"github.com/lightningnetwork/lnd/lnrpc"
	"github.com/lightningnetwork/lnd/lnrpc/routerrpc"
	"github.com/rkfg/regolancer/helpmessage"
)

type configParams struct {
	Config              string   `rego-grouping:"Config" short:"f" long:"config" description:"config file path"`
	Connect             string   `rego-grouping:"Node Connection" short:"c" long:"connect" description:"connect to lnd using host:port" json:"connect" toml:"connect"`
	TLSCert             string   `short:"t" long:"tlscert" description:"path to tls.cert to connect" required:"false" json:"tlscert" toml:"tlscert"`
	MacaroonDir         string   `long:"macaroon-dir" description:"path to the macaroon directory" required:"false" json:"macaroon_dir" toml:"macaroon_dir"`
	MacaroonFilename    string   `long:"macaroon-filename" description:"macaroon filename" json:"macaroon_filename" toml:"macaroon_filename"`
	Network             string   `short:"n" long:"network" description:"bitcoin network to use" json:"network" toml:"network"`
	FromPerc            int64    `rego-grouping:"Common" long:"pfrom" description:"channels with less than this inbound liquidity percentage will be considered as source channels" json:"pfrom" toml:"pfrom"`
	ToPerc              int64    `long:"pto" description:"channels with less than this outbound liquidity percentage will be considered as target channels" json:"pto" toml:"pto"`
	Perc                int64    `short:"p" long:"perc" description:"use this value as both pfrom and pto from above" json:"perc" toml:"perc"`
	Amount              int64    `short:"a" long:"amount" description:"amount to rebalance" json:"amount" toml:"amount"`
	RelAmountTo         float64  `long:"rel-amount-to" description:"calculate amount as the target channel capacity fraction (for example, 0.2 means you want to achieve at most 20% target channel local balance)" json:"rel_amount_to" toml:"rel_amount_to"`
	RelAmountFrom       float64  `long:"rel-amount-from" description:"calculate amount as the source channel capacity fraction (for example, 0.2 means you want to achieve at most 20% source channel remote balance)" json:"rel_amount_from" toml:"rel_amount_from"`
	ProbeSteps          int      `short:"b" long:"probe-steps" description:"if the payment fails at the last hop try to probe lower amount using this many steps" json:"probe_steps" toml:"probe_steps"`
	AllowRapidRebalance bool     `long:"allow-rapid-rebalance" description:"if a rebalance succeeds the route will be used for further rebalances until criteria for channels is not satifsied" json:"allow_rapid_rebalance" toml:"allow_rapid_rebalance"`
	MinAmount           int64    `long:"min-amount" description:"if probing is enabled this will be the minimum amount to try" json:"min_amount" toml:"min_amount"`
	ExcludeChannelsIn   []string `short:"i" long:"exclude-channel-in" description:"(DEPRECATED) don't use this channel as incoming (can be specified multiple times)" json:"exclude_channels_in" toml:"exclude_channels_in"`
	ExcludeChannelsOut  []string `short:"o" long:"exclude-channel-out" description:"(DEPRECATED) don't use this channel as outgoing (can be specified multiple times)" json:"exclude_channels_out" toml:"exclude_channels_out"`
	ExcludeFrom         []string `long:"exclude-from" description:"don't use this node or channel as source (can be specified multiple times)" json:"exclude_from" toml:"exclude_from"`
	ExcludeTo           []string `long:"exclude-to" description:"don't use this node or channel as target (can be specified multiple times)" json:"exclude_to" toml:"exclude_to"`
	ExcludeChannels     []string `short:"e" long:"exclude-channel" description:"(DEPRECATED) don't use this channel at all (can be specified multiple times)" json:"exclude_channels" toml:"exclude_channels"`
	ExcludeNodes        []string `short:"d" long:"exclude-node" description:"(DEPRECATED) don't use this node for routing (can be specified multiple times)" json:"exclude_nodes" toml:"exclude_nodes"`
	Exclude             []string `long:"exclude" description:"don't use this node or your channel for routing (can be specified multiple times)" json:"exclude" toml:"exclude"`
	ExcludeChannelAge   uint64   `long:"exclude-channel-age" description:"don't use channels opened less than this number of blocks ago" json:"exclude_channel_age" toml:"exclude_channel_age"`
	To                  []string `long:"to" description:"try only this channel or node as target (should satisfy other constraints too; can be specified multiple times)" json:"to" toml:"to"`
	From                []string `long:"from" description:"try only this channel or node as source (should satisfy other constraints too; can be specified multiple times)" json:"from" toml:"from"`
	FailTolerance       int64    `long:"fail-tolerance" description:"a payment that differs from the prior attempt by this ppm will be cancelled" json:"fail_tolerance" toml:"fail_tolerance"`
	AllowUnbalanceFrom  bool     `long:"allow-unbalance-from" description:"(DEPRECATED) let the source channel go below 50% local liquidity, use if you want to drain a channel; you should also set --pfrom to >50" json:"allow_unbalance_from" toml:"allow_unbalance_from"`
	AllowUnbalanceTo    bool     `long:"allow-unbalance-to" description:"(DEPRECATED) let the target channel go above 50% local liquidity, use if you want to refill a channel; you should also set --pto to >50" json:"allow_unbalance_to" toml:"allow_unbalance_to"`
	EconRatio           float64  `short:"r" long:"econ-ratio" description:"economical ratio for fee limit calculation as a multiple of target channel fee (for example, 0.5 means you want to pay at max half the fee you might earn for routing out of the target channel)" json:"econ_ratio" toml:"econ_ratio"`
	EconRatioMaxPPM     int64    `long:"econ-ratio-max-ppm" description:"limits the max fee ppm for a rebalance when using econ ratio" json:"econ_ratio_max_ppm" toml:"econ_ratio_max_ppm"`
	ChannelMarginPPM    int64    `long:"channel-margin-ppm" description:"if the lost profit in ppm is below this value, we want a higher rebalancing margin up to this value" json:"channel_margin_ppm" toml:"channel_margin_ppm"`
	FeeLimitPPM         int64    `short:"F" long:"fee-limit-ppm" description:"don't consider the target channel fee and use this max fee ppm instead (can rebalance at a loss, be careful)" json:"fee_limit_ppm" toml:"fee_limit_ppm"`
	LostProfit          bool     `short:"l" long:"lost-profit" description:"also consider the source channel fee when looking for profitable routes so that route_fee < target_fee * econ_ratio - source_fee" json:"lost_profit" toml:"lost_profit"`
	NodeCacheFilename   string   `rego-grouping:"Node Cache" long:"node-cache-filename" description:"save and load other nodes information to this file, improves cold start performance"  json:"node_cache_filename" toml:"node_cache_filename"`
	NodeCacheLifetime   int      `long:"node-cache-lifetime" description:"nodes with last update older than this time (in minutes) will be removed from cache after loading it" json:"node_cache_lifetime" toml:"node_cache_lifetime"`
	NodeCacheInfo       bool     `long:"node-cache-info" description:"show red and cyan 'x' characters in routes to indicate node cache misses and hits respectively" json:"node_cache_info" toml:"node_cache_info"`
	TimeoutRebalance    int      `rego-grouping:"Timeouts" long:"timeout-rebalance" description:"max rebalance session time in minutes" json:"timeout_rebalance" toml:"timeout_rebalance"`
	TimeoutAttempt      int      `long:"timeout-attempt" description:"max attempt time in minutes" json:"timeout_attempt" toml:"timeout_attempt"`
	TimeoutInfo         int      `long:"timeout-info" description:"max general info query time (local channels, node id etc.) in seconds" json:"timeout_info" toml:"timeout_info"`
	TimeoutRoute        int      `long:"timeout-route" description:"max channel selection and route query time in seconds" json:"timeout_route" toml:"timeout_route"`
	StatFilename        string   `rego-grouping:"Others" short:"s" long:"stat" description:"save successful rebalance information to the specified CSV file" json:"stat" toml:"stat"`
	Version             bool     `short:"v" long:"version" description:"show program version and exit"`
	Info                bool     `long:"info" description:"show rebalance information"`
	Help                bool     `short:"h" long:"help" description:"Show this help message"`
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
	blockHeight   uint32
	channels      []*lnrpc.Channel
	fromChannels  []*lnrpc.Channel
	fromChannelId map[uint64]struct{}
	toChannels    []*lnrpc.Channel
	toChannelId   map[uint64]struct{}
	channelPairs  map[string][2]*lnrpc.Channel
	nodeCache     map[string]cachedNodeInfo
	chanCache     map[uint64]*lnrpc.ChannelEdge
	failureCache  map[string]failedRoute
	excludeTo     map[uint64]struct{}
	excludeFrom   map[uint64]struct{}
	excludeBoth   map[uint64]struct{}
	excludeNodes  [][]byte
	statFilename  string
	routeFound    bool
	invoiceCache  map[int64]*lnrpc.AddInvoiceResponse
	mcCache       map[string]int64
	failedPairs   []*lnrpc.NodePair
}

func loadConfig() {
	_, err := flags.NewParser(&cfgParams, flags.None).Parse()
	if err != nil {
		log.Fatalf("Error when parsing command line options: %s", err)
	}

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

func preflightChecks(params *configParams) error {
	if params.Version {
		printVersion()
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
	if !params.LostProfit {
		params.ChannelMarginPPM = 0
	}
	if params.EconRatioMaxPPM != 0 && params.FeeLimitPPM != 0 {
		return fmt.Errorf("use either econ-ratio-max-ppm or fee-limit-ppm but not both")
	}
	if params.Perc > 0 {
		params.FromPerc = params.Perc
		params.ToPerc = params.Perc
	}
	if params.MinAmount > 0 && params.Amount > 0 &&
		params.MinAmount > params.Amount {
		return fmt.Errorf("minimum amount should be less than amount")
	}
	if params.Amount > 0 &&
		(params.RelAmountFrom > 0 || params.RelAmountTo > 0) {
		return fmt.Errorf("use either precise amount or relative amounts but not both")
	}
	if params.Amount == 0 && params.RelAmountFrom == 0 && params.RelAmountTo == 0 {
		return fmt.Errorf("no amount specified, use either --amount, --rel-amount-from, or --rel-amount-to")
	}
	if params.FailTolerance == 0 {
		params.FailTolerance = 1000
	}

	if (params.RelAmountFrom > 0 || params.RelAmountTo > 0) && params.AllowRapidRebalance {
		return fmt.Errorf("use either relative amounts or rapid rebalance but not both")
	}
	if params.NodeCacheLifetime == 0 {
		params.NodeCacheLifetime = 1440
	}
	if len(params.ExcludeChannels) > 0 || len(params.ExcludeNodes) > 0 {
		log.Print(infoColor("--exclude-channel and exclude_channel parameter are deprecated, use --exclude or exclude parameter instead for both channels and nodes"))
		if len(params.Exclude) > 0 {
			return fmt.Errorf("can't use --exclude and --exclude-channel/--exclude-node (or config parameters) at the same time")
		}
	}
	if params.AllowUnbalanceFrom || params.AllowUnbalanceTo {
		log.Print(infoColor("--allow-unbalance-from/to are deprecated and enabled by default, please remove them from your config or command line parameters"))
	}
	if len(params.ExcludeChannelsIn) > 0 {
		log.Print(infoColor("--exclude-channel-in are deprecated use --exclude-to instead, please remove them from your config or command line parameters"))
		if len(params.ExcludeTo) > 0 {
			return fmt.Errorf("can't use --exclude-to and --exclude-channel-in (or config parameters) at the same time")
		}
	}
	if len(params.ExcludeChannelsOut) > 0 {
		log.Print(infoColor("--exclude-channel-out are deprecated use --exclude-from instead, please remove them from your config or command line parameters"))
		if len(params.ExcludeFrom) > 0 {
			return fmt.Errorf("can't use --exclude-from and --exclude-channel-out (or config parameters) at the same time")
		}
	}
	if params.TimeoutAttempt == 0 {
		params.TimeoutAttempt = 5
	}
	if params.TimeoutRebalance == 0 {
		params.TimeoutRebalance = 360
	}
	if params.TimeoutInfo == 0 {
		params.TimeoutInfo = 30
	}
	if params.TimeoutRoute == 0 {
		params.TimeoutRoute = 30
	}

	return nil

}

func main() {
	exitCode := 0
	defer func() {
		os.Exit(exitCode)
	}()

	rand.Seed(time.Now().UnixNano())

	loadConfig()
	parser := flags.NewParser(&params, flags.PrintErrors|flags.PassDoubleDash)

	_, err := parser.Parse()

	if err != nil {
		os.Exit(1)
	}

	// Print own Help message instead of using the builtin Help function of goflags to group output
	if params.Help {
		var opt helpmessage.Options
		if reflect.ValueOf(params).Kind() == reflect.Struct {
			v := reflect.ValueOf(params)
			err := helpmessage.ScanStruct(v, &opt)
			if err != nil {
				fmt.Println(err)
				os.Exit(1)
			}
			var b bytes.Buffer
			helpmessage.WriteHelp(&opt, parser, &b)
		}
		return
	}

	err = preflightChecks(&params)

	if err != nil {
		log.Fatal(errColor(err))
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
		mcCache:      map[string]int64{},
		statFilename: params.StatFilename,
	}
	r.lnClient = lnrpc.NewLightningClient(conn)
	r.routerClient = routerrpc.NewRouterClient(conn)
	mainCtx, mainCtxCancel := context.WithTimeout(context.Background(), time.Minute*time.Duration(params.TimeoutRebalance))
	defer mainCtxCancel()
	infoCtx, infoCtxCancel := context.WithTimeout(mainCtx, time.Second*time.Duration(params.TimeoutInfo))
	defer infoCtxCancel()
	info, err := r.lnClient.GetInfo(infoCtx, &lnrpc.GetInfoRequest{})
	if err != nil {
		log.Fatal(err)
	}
	r.myPK = info.IdentityPubkey
	r.blockHeight = info.BlockHeight
	err = r.getChannels(infoCtx)
	if err != nil {
		log.Fatal("Error listing own channels: ", err)
	}

	if len(params.From) > 0 {
		r.fromChannelId = r.filterChannels(infoCtx, params.From)
		if len(r.fromChannelId) == 0 {
			log.Fatal("No source nodes/channels selected, check if the ID is correct and node is online")
		}
	}
	if len(params.To) > 0 {
		r.toChannelId = r.filterChannels(infoCtx, params.To)
		if len(r.toChannelId) == 0 {
			log.Fatal("No target nodes/channels selected, check if the ID is correct and node is online")
		}
	}

	if len(params.ExcludeFrom) > 0 {
		r.excludeFrom = r.filterChannels(infoCtx, params.ExcludeFrom)
	} else {
		r.excludeFrom = makeChanSet(convertChanStringToInt(params.ExcludeChannelsOut))
	}

	if len(params.ExcludeTo) > 0 {
		r.excludeTo = r.filterChannels(infoCtx, params.ExcludeTo)
	} else {
		r.excludeTo = makeChanSet(convertChanStringToInt(params.ExcludeChannelsIn))
	}

	r.excludeBoth = makeChanSet(convertChanStringToInt(params.ExcludeChannels))
	err = r.makeNodeList(params.ExcludeNodes)
	if err != nil {
		log.Fatal("Error parsing excluded node list: ", err)
	}

	if len(params.Exclude) > 0 {
		chans, nodes, err := parseNodeChannelIDs(params.Exclude)
		if err != nil {
			log.Fatal("Error parsing excluded node/channel list:", err)
		}
		r.excludeBoth = chans
		r.excludeNodes = nodes
	}

	r.invoiceCache = map[int64]*lnrpc.AddInvoiceResponse{}

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
	attempt := 1

	err = r.loadNodeCache(params.NodeCacheFilename, params.NodeCacheLifetime,
		true)
	if err != nil {
		logErrorF("%s", err)
	}
	defer r.saveNodeCache(params.NodeCacheFilename, params.NodeCacheLifetime)
	if params.Info {
		err = r.info(infoCtx)
		if err != nil {
			log.Fatal(err)
		}
		return
	}
	infoCtxCancel()
	stopChan := make(chan os.Signal, 1)
	signal.Notify(stopChan, os.Interrupt)
	go func() {
		<-stopChan
		r.saveNodeCache(params.NodeCacheFilename, params.NodeCacheLifetime)
		os.Exit(1)
	}()

	for {
		err, retry := r.tryRebalance(mainCtx, &attempt)
		if mainCtx.Err() == context.DeadlineExceeded {
			log.Println(errColor("Rebalancing timed out"))
			exitCode = 2
			return
		}
		if !retry {
			if err != nil {
				exitCode = 1
			}
			return
		}
	}
}
