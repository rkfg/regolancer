package main

import (
	"encoding/hex"
	"fmt"

	"github.com/lightningnetwork/lnd/lnrpc"
)

func (r *regolancer) addFailedChan(fromStr string, toStr string, amount int64) {
	r.mcCache[fromStr+toStr] = amount
}

func (r *regolancer) validateRoute(route *lnrpc.Route) error {
	targetHop := route.Hops[len(route.Hops)-2]
	if int64(float64(targetHop.FeeMsat)/float64(targetHop.AmtToForwardMsat)*1e6) > params.FeeLastHopPPM && params.FeeLastHopPPM != 0 {
		from, err := hex.DecodeString(targetHop.PubKey)
		if err != nil {
			return err
		}
		to, err := hex.DecodeString(r.myPK)
		if err != nil {
			return err
		}
		r.failedPairs = append(r.failedPairs, &lnrpc.NodePair{From: from, To: to})
		return fmt.Errorf("last hop with chan %d exceeds our limit: %s ppm allowed, wanted %s ppm", route.Hops[len(route.Hops)-1].ChanId, hiWhiteColor(params.FeeLastHopPPM), formatFeePPM(targetHop.AmtToForwardMsat, targetHop.FeeMsat))
	}

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
