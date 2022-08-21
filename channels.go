package main

import (
	"context"
	"math"
	"math/rand"
	"time"

	"github.com/lightningnetwork/lnd/lnrpc"
)

func (r *regolancer) getChannels(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, time.Second*30)
	defer cancel()
	channels, err := r.lnClient.ListChannels(ctx, &lnrpc.ListChannelsRequest{ActiveOnly: true, PublicOnly: true})
	if err != nil {
		return err
	}
	r.channels = channels.Channels
	return nil
}

func makeChanSet(chanIds []uint64) (result map[uint64]struct{}) {
	result = map[uint64]struct{}{}
	for _, cid := range chanIds {
		result[cid] = struct{}{}
	}
	return
}

func (r *regolancer) getChannelCandidates(fromPerc, toPerc, amount int64) error {
	for _, c := range r.channels {
		if _, ok := r.excludeBoth[c.ChanId]; ok {
			continue
		}
		if c.LocalBalance < c.Capacity*toPerc/100 && c.LocalBalance+amount < c.Capacity/2 {
			if _, ok := r.excludeIn[c.ChanId]; ok {
				continue
			}
			r.toChannels = append(r.toChannels, c)
		}
		if c.RemoteBalance < c.Capacity*fromPerc/100 && c.RemoteBalance-amount < c.Capacity/2 {
			if _, ok := r.excludeOut[c.ChanId]; ok {
				continue
			}
			r.fromChannels = append(r.fromChannels, c)
		}
	}
	return nil
}

func min(args ...int64) (result int64) {
	result = math.MaxInt64
	for _, a := range args {
		if a < result {
			result = a
		}
	}
	return
}

func (r *regolancer) pickChannelPair(ctx context.Context, amount int64) (from uint64, to uint64, maxAmount int64, err error) {
	var fromChan, toChan *lnrpc.Channel
	for {
		select {
		case <-ctx.Done():
			return 0, 0, 0, ctx.Err()
		default:
		}
		fromIdx := rand.Int31n(int32(len(r.fromChannels)))
		toIdx := rand.Int31n(int32(len(r.toChannels)))
		fromChan = r.fromChannels[fromIdx]
		toChan = r.toChannels[toIdx]
		if !r.isFailedRoute(fromChan.ChanId, toChan.ChanId) {
			break
		}
	}
	maxFrom := fromChan.Capacity/2 - fromChan.RemoteBalance
	maxTo := toChan.Capacity/2 - toChan.LocalBalance
	if amount == 0 {
		maxAmount = min(maxFrom, maxTo)
	} else {
		maxAmount = min(maxFrom, maxTo, amount)
	}
	return fromChan.ChanId, toChan.ChanId, maxAmount, nil
}
