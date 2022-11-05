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
