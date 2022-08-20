package main

import (
	"context"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/lightningnetwork/lnd/lnrpc"
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
	fmt.Printf("%s %s\n", faintWhiteColor("Total fee:"), hiWhiteColor((route.TotalFeesMsat-route.Hops[0].FeeMsat)/1000))
	for i, hop := range route.Hops {
		nodeInfo, err := r.getNodeInfo(hop.PubKey)
		if err != nil {
			errs = errs + err.Error() + "\n"
			continue
		}
		fee := hiWhiteColorF("%-6d", hop.FeeMsat)
		if i == 0 {
			fee = hiWhiteColorF("%-6s", "")
		}
		fmt.Printf("%s %s %s\n", faintWhiteColor(hop.ChanId), fee, cyanColor(nodeInfo.Node.Alias))
	}
	if errs != "" {
		fmt.Println(errColor(errs))
	}
}
