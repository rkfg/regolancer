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

func (r *regolancer) getRoutes(from, to uint64, amtMsat int64, ratio float64) ([]*lnrpc.Route, int64, error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*30)
	defer cancel()
	c, err := r.getChanInfo(ctx, to)
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
	routes, err := r.lnClient.QueryRoutes(ctx, &lnrpc.QueryRoutesRequest{
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

func (r *regolancer) getNodeInfo(pk string) (*lnrpc.NodeInfo, error) {
	if nodeInfo, ok := r.nodeCache[pk]; ok {
		return nodeInfo, nil
	}
	nodeInfo, err := r.lnClient.GetNodeInfo(context.Background(), &lnrpc.NodeInfoRequest{PubKey: pk})
	if err == nil {
		r.nodeCache[pk] = nodeInfo
	}
	return nodeInfo, err
}

func (r *regolancer) printRoute(route *lnrpc.Route) {
	if len(route.Hops) == 0 {
		return
	}
	errs := ""
	fmt.Printf("%s %s\n", faintWhiteColor("Total fee:"), hiWhiteColor(route.TotalFeesMsat/1000))
	for i, hop := range route.Hops {
		nodeInfo, err := r.getNodeInfo(hop.PubKey)
		if err != nil {
			errs = errs + err.Error() + "\n"
			continue
		}
		fee := hiWhiteColorF("%-6s", "")
		if i > 0 {
			hiWhiteColorF("%-6d", route.Hops[i-1].FeeMsat)
		}
		fmt.Printf("%s %s %s\n", faintWhiteColor(hop.ChanId), fee, cyanColor(nodeInfo.Node.Alias))
	}
	if errs != "" {
		fmt.Println(errColor(errs))
	}
}

func (r *regolancer) rebuildRoute(route *lnrpc.Route, amount int64) (*lnrpc.Route, error) {
	pks := [][]byte{}
	for _, h := range route.Hops {
		pk, _ := hex.DecodeString(h.PubKey)
		pks = append(pks, pk)
	}
	resultRoute, err := r.routerClient.BuildRoute(context.Background(), &routerrpc.BuildRouteRequest{
		AmtMsat:        amount * 1000,
		OutgoingChanId: route.Hops[0].ChanId,
		HopPubkeys:     pks,
		FinalCltvDelta: 144,
	})
	return resultRoute.Route, err
}

func (r *regolancer) probeRoute(route *lnrpc.Route, goodAmount, badAmount, amount int64, steps int) (maxAmount int64,
	goodRoute *lnrpc.Route, err error) {
	goodRoute, err = r.rebuildRoute(route, amount)
	if err != nil {
		return 0, nil, err
	}
	fakeHash := make([]byte, 32)
	rand.Read(fakeHash)
	result, err := r.routerClient.SendToRouteV2(context.Background(),
		&routerrpc.SendToRouteRequest{
			PaymentHash: fakeHash,
			Route:       goodRoute,
		})
	if err != nil {
		return
	}
	if result.Status == lnrpc.HTLCAttempt_SUCCEEDED {
		return 0, nil, fmt.Errorf("this should never happen")
	}
	if result.Status == lnrpc.HTLCAttempt_FAILED {
		if result.Failure.Code == lnrpc.Failure_INCORRECT_OR_UNKNOWN_PAYMENT_DETAILS { // payment can succeed
			if steps == 1 || amount == badAmount {
				log.Printf("%s is the best amount", hiWhiteColor(amount))
				return amount, goodRoute, nil
			}
			nextAmount := amount + (badAmount-amount)/2
			log.Printf("%s is good enough, trying amount %s, %s steps left",
				hiWhiteColor(amount), hiWhiteColor(nextAmount), hiWhiteColor(steps-1))
			return r.probeRoute(route, amount, badAmount, nextAmount, steps-1)
		}
		if result.Failure.Code == lnrpc.Failure_TEMPORARY_CHANNEL_FAILURE {
			if steps == 1 {
				bestAmount := hiWhiteColor(goodAmount)
				if goodAmount == 0 {
					bestAmount = hiWhiteColor("unknown")
				}
				log.Printf("%s is too much, best amount is %s", hiWhiteColor(amount), bestAmount)
				return goodAmount, goodRoute, nil
			}
			nextAmount := amount + (goodAmount-amount)/2
			log.Printf("%s is too much, lowering amount to %s, %s steps left",
				hiWhiteColor(amount), hiWhiteColor(nextAmount), hiWhiteColor(steps-1))
			return r.probeRoute(route, goodAmount, amount, nextAmount, steps-1)
		}
		if result.Failure.Code == lnrpc.Failure_FEE_INSUFFICIENT {
			log.Printf("Fee insufficient, retrying...")
			return r.probeRoute(route, goodAmount, badAmount, amount, steps)
		}
	}
	return 0, nil, fmt.Errorf("unknown error: %+v", result)
}
