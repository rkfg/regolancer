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

func calcFeeMsat(amtMsat int64, policy *lnrpc.RoutingPolicy) float64 {
	return float64(policy.FeeBaseMsat+amtMsat*policy.FeeRateMilliMsat) / 1e6
}

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

func (r *regolancer) getRoutes(ctx context.Context, from, to uint64, amtMsat int64, ratio float64) ([]*lnrpc.Route, int64, error) {
	routeCtx, cancel := context.WithTimeout(ctx, time.Second*30)
	defer cancel()
	c, err := r.getChanInfo(routeCtx, to)
	if err != nil {
		return nil, 0, err
	}
	lastPKstr := c.Node1Pub
	policy := c.Node2Policy
	if lastPKstr == r.myPK {
		lastPKstr = c.Node2Pub
		policy = c.Node1Policy
	}
	feeMsat := int64(calcFeeMsat(amtMsat, policy) * ratio)
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
	})
	if err != nil {
		return nil, 0, err
	}
	return routes.Routes, feeMsat, nil
}

func (r *regolancer) getNodeInfo(ctx context.Context, pk string) (*lnrpc.NodeInfo, error) {
	if nodeInfo, ok := r.nodeCache[pk]; ok {
		return nodeInfo, nil
	}
	nodeInfo, err := r.lnClient.GetNodeInfo(ctx, &lnrpc.NodeInfoRequest{PubKey: pk})
	if err == nil {
		r.nodeCache[pk] = nodeInfo
	}
	return nodeInfo, err
}

func (r *regolancer) printRoute(ctx context.Context, route *lnrpc.Route) {
	if len(route.Hops) == 0 {
		return
	}
	errs := ""
	fmt.Printf("%s %s\n", faintWhiteColor("Total fee:"), hiWhiteColor(route.TotalFeesMsat/1000))
	for i, hop := range route.Hops {
		nodeInfo, err := r.getNodeInfo(ctx, hop.PubKey)
		if err != nil {
			errs = errs + err.Error() + "\n"
			continue
		}
		fee := hiWhiteColorF("%-6s", "")
		if i > 0 {
			fee = hiWhiteColorF("%-6d", route.Hops[i-1].FeeMsat)
		}
		fmt.Printf("%s %s %s\n", faintWhiteColor(hop.ChanId), fee, cyanColor(nodeInfo.Node.Alias))
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

func (r *regolancer) probeRoute(ctx context.Context, route *lnrpc.Route, goodAmount, badAmount, amount int64,
	steps int) (maxAmount int64, err error) {
	probedRoute, err := r.rebuildRoute(ctx, route, amount)
	if err != nil {
		return 0, err
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
			if steps == 1 || amount == badAmount {
				log.Printf("%s is the best amount", hiWhiteColor(amount))
				return amount, nil
			}
			nextAmount := amount + (badAmount-amount)/2
			log.Printf("%s is good enough, trying amount %s, %s steps left",
				hiWhiteColor(amount), hiWhiteColor(nextAmount), hiWhiteColor(steps-1))
			return r.probeRoute(ctx, route, amount, badAmount, nextAmount, steps-1)
		}
		if result.Failure.Code == lnrpc.Failure_TEMPORARY_CHANNEL_FAILURE {
			if steps == 1 {
				bestAmount := hiWhiteColor(goodAmount)
				if goodAmount == 0 {
					bestAmount = hiWhiteColor("unknown")
				}
				log.Printf("%s is too much, best amount is %s", hiWhiteColor(amount), bestAmount)
				return goodAmount, nil
			}
			nextAmount := amount + (goodAmount-amount)/2
			log.Printf("%s is too much, lowering amount to %s, %s steps left",
				hiWhiteColor(amount), hiWhiteColor(nextAmount), hiWhiteColor(steps-1))
			return r.probeRoute(ctx, route, goodAmount, amount, nextAmount, steps-1)
		}
		if result.Failure.Code == lnrpc.Failure_FEE_INSUFFICIENT {
			log.Printf("Fee insufficient, retrying...")
			return r.probeRoute(ctx, route, goodAmount, badAmount, amount, steps)
		}
	}
	return 0, fmt.Errorf("unknown error: %+v", result)
}

func (r *regolancer) addFailedRoute(from, to uint64) {
	t := time.Now().Add(time.Hour)
	r.failureCache[fmt.Sprintf("%d-%d", from, to)] = &t
	for k, v := range r.failureCache {
		if v.Before(time.Now()) {
			delete(r.failureCache, k)
		}
	}
}

func (r *regolancer) isFailedRoute(from, to uint64) bool {
	_, ok := r.failureCache[fmt.Sprintf("%d-%d", from, to)]
	return ok
}
