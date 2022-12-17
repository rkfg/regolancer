package main

import (
	"context"
	"encoding/hex"
	"fmt"
	"log"
	"math/rand"
	"time"

	"github.com/lightningnetwork/lnd/lnrpc"
	"github.com/lightningnetwork/lnd/lnrpc/routerrpc"
)

const (
	COIN = 1e8
)

func (r *regolancer) getChanInfo(ctx context.Context, chanId uint64) (*lnrpc.ChannelEdge, error) {
	if c, ok := r.chanCache[chanId]; ok {
		return c, nil
	}
	c, err := r.lnClient.GetChanInfo(ctx, &lnrpc.ChanInfoRequest{ChanId: chanId})
	if err != nil {
		return nil, err
	}
	r.chanCache[chanId] = c
	return c, nil
}

func (r *regolancer) calcFeeLimitMsat(ctx context.Context, to uint64,
	amtMsat int64, ppm int64) (feeMsat int64, lastPKstr string, err error) {
	cTo, err := r.getChanInfo(ctx, to)
	if err != nil {
		return 0, "", err
	}
	lastPKstr = cTo.Node1Pub
	if lastPKstr == r.myPK {
		lastPKstr = cTo.Node2Pub
	}
	feeMsat = amtMsat * ppm / 1e6
	return
}

func (r *regolancer) calcEconFeeMsat(ctx context.Context, from, to uint64, amtMsat int64, ratio float64) (feeMsat int64,
	lastPKstr string, err error) {
	cTo, err := r.getChanInfo(ctx, to)
	if err != nil {
		return 0, "", err
	}
	lastPKstr = cTo.Node1Pub
	policyTo := cTo.Node2Policy
	if lastPKstr == r.myPK {
		lastPKstr = cTo.Node2Pub
		policyTo = cTo.Node1Policy
	}
	lostProfitMsat := int64(0)
	if params.LostProfit {
		cFrom, err := r.getChanInfo(ctx, from)
		if err != nil {
			return 0, "", err
		}
		policyFrom := cFrom.Node1Policy
		if cFrom.Node2Pub == r.myPK {
			policyFrom = cFrom.Node2Policy
		}
		lostProfitMsat = int64(float64(policyFrom.FeeBaseMsat+
			amtMsat*policyFrom.FeeRateMilliMsat) / 1e6)
	}
	feeMsat = int64(float64(policyTo.FeeBaseMsat+amtMsat*
		policyTo.FeeRateMilliMsat)*ratio/1e6) - lostProfitMsat

	if params.EconRatioMaxPPM != 0 && int64(float64(feeMsat)/float64(amtMsat)*1e6) > params.EconRatioMaxPPM {
		feeMsat = params.EconRatioMaxPPM * amtMsat / 1e6
	}
	if feeMsat < 0 {
		return 0, "", fmt.Errorf("max fee less than zero")
	}
	return
}

func (r *regolancer) calcFeeMsat(ctx context.Context, from, to uint64,
	amtMsat int64) (feeMsat int64, lastPKstr string, err error) {
	if params.FeeLimitPPM > 0 {
		return r.calcFeeLimitMsat(ctx, to, amtMsat, params.FeeLimitPPM)
	} else {
		return r.calcEconFeeMsat(ctx, from, to, amtMsat, params.EconRatio)
	}
}

func (r *regolancer) getRoutes(ctx context.Context, from, to uint64, amtMsat int64) ([]*lnrpc.Route, int64, error) {
	routeCtx, cancel := context.WithTimeout(ctx, time.Second*time.Duration(params.TimeoutRoute))
	defer cancel()
	feeMsat, lastPKstr, err := r.calcFeeMsat(routeCtx, from, to, amtMsat)
	if err != nil {
		return nil, 0, err
	}
	lastPK, err := hex.DecodeString(lastPKstr)
	if err != nil {
		return nil, 0, err
	}
	routes, err := r.lnClient.QueryRoutes(routeCtx, &lnrpc.QueryRoutesRequest{
		PubKey:            r.myPK,
		OutgoingChanId:    from,
		LastHopPubkey:     lastPK,
		AmtMsat:           amtMsat,
		UseMissionControl: true,
		FeeLimit:          &lnrpc.FeeLimit{Limit: &lnrpc.FeeLimit_FixedMsat{FixedMsat: feeMsat}},
		IgnoredNodes:      r.excludeNodes,
		IgnoredPairs:      r.failedPairs,
	})
	if err != nil {
		return nil, 0, err
	}
	result := []*lnrpc.Route{}
	for i := range routes.Routes { // lnd always returns 1 route for now but just in case it changes
		if err := r.validateRoute(routes.Routes[i]); err == nil {
			result = append(result, routes.Routes[i])
		} else {
			log.Print(err)
		}
	}
	if len(result) == 0 {
		return r.getRoutes(ctx, from, to, amtMsat)
	}
	r.routeFound = true
	return result, feeMsat, nil
}

func (r *regolancer) getNodeInfo(ctx context.Context, pk string) (*lnrpc.NodeInfo, error) {
	if nodeInfo, ok := r.nodeCache[pk]; ok {
		return nodeInfo.NodeInfo, nil
	}
	nodeInfo, err := r.lnClient.GetNodeInfo(ctx, &lnrpc.NodeInfoRequest{PubKey: pk})
	if err == nil {
		r.nodeCache[pk] = cachedNodeInfo{
			NodeInfo:  nodeInfo,
			Timestamp: time.Now(),
		}
	}
	return nodeInfo, err
}

func (r *regolancer) printRoute(ctx context.Context, route *lnrpc.Route) {
	if len(route.Hops) == 0 {
		return
	}
	errs := ""
	fmt.Printf("%s %s sat | %s ppm\n", faintWhiteColor("Total fee:"),
		formatFee(route.TotalFeesMsat), formatFeePPM(route.TotalAmtMsat-route.TotalFeesMsat, route.TotalFeesMsat))
	for i, hop := range route.Hops {
		cached := ""
		if params.NodeCacheInfo {
			cached = errColor("x")
			if _, ok := r.nodeCache[hop.PubKey]; ok {
				cached = cyanColor("x")
			}
			cached += "|"
		}
		nodeInfo, err := r.getNodeInfo(ctx, hop.PubKey)
		if err != nil {
			errs = errs + err.Error() + "\n"
			continue
		}
		fee := hiWhiteColorF("%-6s", "")
		if i > 0 {
			fee = hiWhiteColorF("%-6d", route.Hops[i-1].FeeMsat)
		}
		fmt.Printf("%s %s [%s%s|%sch|%ssat|%s]\n", faintWhiteColor(hop.ChanId), fee, cached, cyanColor(nodeInfo.Node.Alias),
			infoColor(nodeInfo.NumChannels), formatAmt(nodeInfo.TotalCapacity), infoColor(nodeInfo.Node.PubKey))
	}
	if errs != "" {
		fmt.Println(errColor(errs))
	}
}

func (r *regolancer) rebuildRoute(ctx context.Context, route *lnrpc.Route, amount int64) (*lnrpc.Route, error) {
	pks := [][]byte{}
	for _, h := range route.Hops {
		pk, _ := hex.DecodeString(h.PubKey)
		pks = append(pks, pk)
	}
	resultRoute, err := r.routerClient.BuildRoute(ctx, &routerrpc.BuildRouteRequest{
		AmtMsat:        amount * 1000,
		OutgoingChanId: route.Hops[0].ChanId,
		HopPubkeys:     pks,
		FinalCltvDelta: 144,
	})
	if err != nil {
		return nil, err
	}
	return resultRoute.Route, err
}

func (r *regolancer) probeRoute(ctx context.Context, route *lnrpc.Route,
	goodAmount, badAmount, amount int64, steps int) (maxAmount int64, err error) {

	defer func() {
		if ctx.Err() == context.DeadlineExceeded && goodAmount > 0 {
			maxAmount = goodAmount
			log.Printf("Probing timed out with value %s", hiWhiteColor(maxAmount))

		}
	}()

	if absoluteDeltaPPM(badAmount, amount) <= params.FailTolerance || absoluteDeltaPPM(amount, goodAmount) <= params.FailTolerance || amount == -goodAmount {
		bestAmount := hiWhiteColor(goodAmount)
		maxAmount = goodAmount
		if goodAmount <= 0 {
			bestAmount = hiWhiteColor("unknown")
			maxAmount = 0
		}
		log.Printf("Best amount is %s", bestAmount)
		return
	}
	probedRoute, err := r.rebuildRoute(ctx, route, amount)
	if err != nil {
		return
	}
	maxFeeMsat, _, err := r.calcFeeMsat(ctx, probedRoute.Hops[0].ChanId,
		probedRoute.Hops[len(probedRoute.Hops)-1].ChanId, amount*1000)
	if err != nil {
		return
	}
	if probedRoute.TotalFeesMsat > maxFeeMsat {
		nextAmount := amount + (badAmount-amount)/2
		log.Printf("%s requires too high fee %s (max allowed is %s), increasing amount to %s",
			hiWhiteColor(amount), formatFee(probedRoute.TotalFeesMsat),
			formatFee(maxFeeMsat), hiWhiteColor(nextAmount))
		// returning negative amount as "good", it's a special case which means
		// this is rather the lower bound and the actual good amount is still
		// unknown
		return r.probeRoute(ctx, route, -amount, badAmount, nextAmount, steps)
	}
	fakeHash := make([]byte, 32)
	rand.Read(fakeHash)
	result, err := r.routerClient.SendToRouteV2(ctx,
		&routerrpc.SendToRouteRequest{
			PaymentHash: fakeHash,
			Route:       probedRoute,
		})
	if err != nil {
		return
	}
	if result.Status == lnrpc.HTLCAttempt_SUCCEEDED {
		return 0, fmt.Errorf("this should never happen")
	}
	if result.Status == lnrpc.HTLCAttempt_FAILED {
		if result.Failure.Code == lnrpc.Failure_INCORRECT_OR_UNKNOWN_PAYMENT_DETAILS { // payment can succeed
			if steps == 1 {
				log.Printf("best amount is %s", hiWhiteColor(amount))
				maxAmount = amount
				return
			}
			nextAmount := amount + (badAmount-amount)/2
			log.Printf("%s is good enough, trying amount %s, %s steps left",
				hiWhiteColor(amount), hiWhiteColor(nextAmount),
				hiWhiteColor(steps-1))
			return r.probeRoute(ctx, route, amount, badAmount, nextAmount,
				steps-1)
		}
		if result.Failure.Code == lnrpc.Failure_TEMPORARY_CHANNEL_FAILURE {
			if steps == 1 {
				maxAmount = goodAmount
				bestAmount := hiWhiteColor(goodAmount)
				if goodAmount <= 0 {
					bestAmount = hiWhiteColor("unknown")
					maxAmount = 0
				}
				log.Printf("%s is too much, best amount is %s",
					hiWhiteColor(amount), bestAmount)
				return
			}
			var nextAmount int64
			if goodAmount >= 0 {
				nextAmount = amount + (goodAmount-amount)/2
			} else {
				nextAmount = amount - (goodAmount+amount)/2
			}
			log.Printf("%s is too much, lowering amount to %s, %s steps left",
				hiWhiteColor(amount), hiWhiteColor(nextAmount),
				hiWhiteColor(steps-1))
			return r.probeRoute(ctx, route, goodAmount, amount, nextAmount,
				steps-1)
		}
		if result.Failure.Code == lnrpc.Failure_FEE_INSUFFICIENT {
			log.Printf("Fee insufficient, retrying...")
			return r.probeRoute(ctx, route, goodAmount, badAmount, amount,
				steps)
		}
	}
	return 0, fmt.Errorf("unknown error: %+v", result)
}

func (r *regolancer) makeNodeList(nodes []string) error {
	for _, nid := range nodes {
		if len(nid) != 66 {
			return fmt.Errorf("invalid node id (%s) length, expected 66 characters, got %d", nid, len(nid))
		}
		pk, err := hex.DecodeString(nid)
		if err != nil {
			return err
		}
		r.excludeNodes = append(r.excludeNodes, pk)
	}
	return nil
}
