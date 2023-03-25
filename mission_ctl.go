package main

import (
	"context"
	"encoding/hex"
	"fmt"
	"math"

	"github.com/lightningnetwork/lnd/lnrpc"
)

func (r *regolancer) addFailedChan(fromStr string, toStr string, amount int64) {
	r.mcCache[fromStr+toStr] = amount
}

func (r *regolancer) validateRoute(route *lnrpc.Route) error {
	prevHopPK := r.myPK
	for _, h := range route.Hops {
		hopPK := h.PubKey
		if fp, ok := r.mcCache[prevHopPK+hopPK]; ok && absoluteDeltaPPM(fp, h.AmtToForwardMsat) < params.FailTolerance {
			from, err := hex.DecodeString(prevHopPK)
			if err != nil {
				return err
			}
			to, err := hex.DecodeString(hopPK)
			if err != nil {
				return err
			}
			r.failedPairs = append(r.failedPairs, &lnrpc.NodePair{From: from, To: to})
			return fmt.Errorf("chan %d failed before with %d msat and will not be used anymore during this rebalance, payment attempt with %d msat cancelled", h.ChanId, fp, h.AmtToForwardMsat)
		}
		prevHopPK = hopPK
	}
	return nil
}

func (r *regolancer) maxAmountOnRoute(ctx context.Context, route *lnrpc.Route) (uint64, error) {
	var capAmountMsat uint64 = math.MaxInt64
	for _, h := range route.Hops {
		edge, err := r.getChanInfo(ctx, h.ChanId)
		if err != nil {
			return 0, err
		}

		policyTo := edge.Node1Policy
		if h.PubKey != edge.Node2Pub {
			policyTo = edge.Node2Policy
		}

		if policyTo.MaxHtlcMsat <= 0 {
			continue
		}

		if capAmountMsat > policyTo.MaxHtlcMsat {
			capAmountMsat = policyTo.MaxHtlcMsat
		}
	}

	return capAmountMsat, nil

}
