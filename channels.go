package main

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"math"
	"math/rand"
	"strconv"
	"strings"
	"time"

	"github.com/lightningnetwork/lnd/lnrpc"
	"github.com/lightningnetwork/lnd/lnwire"
)

func formatChannelPair(a, b uint64) string {
	return fmt.Sprintf("%d-%d", a, b)
}

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

func parseNodeChannelIDs(ids []string) (chans map[uint64]struct{}, nodes [][]byte, err error) {
	chanIdStr := []string{}
	nodePKStr := []string{}
	for _, id := range ids {
		if len(id) == 66 {
			nodePKStr = append(nodePKStr, id)
		} else {
			chanIdStr = append(chanIdStr, id)
		}
	}
	chans = makeChanSet(convertChanStringToInt(chanIdStr))
	for _, pk := range nodePKStr {
		nodePK, err := hex.DecodeString(pk)
		if err != nil {
			return nil, nil, err
		}
		nodes = append(nodes, nodePK)
	}
	return
}

func (r *regolancer) getChannelCandidates(fromPerc, toPerc, amount int64) error {

	for _, c := range r.channels {
		if _, ok := r.excludeBoth[c.ChanId]; ok {
			continue
		}
		if c.LocalBalance < c.Capacity*toPerc/100 && (params.AllowUnbalanceTo ||
			c.LocalBalance+amount < c.Capacity/2) {
			if _, ok := r.excludeIn[c.ChanId]; ok {
				continue
			}
			if _, ok := r.toChannelId[c.ChanId]; ok || len(r.toChannelId) == 0 {
				r.toChannels = append(r.toChannels, c)
			}
		}
		if c.RemoteBalance < c.Capacity*fromPerc/100 &&
			(params.AllowUnbalanceFrom ||
				c.RemoteBalance+amount < c.Capacity/2) {
			if _, ok := r.excludeOut[c.ChanId]; ok {
				continue
			}
			if _, ok := r.fromChannelId[c.ChanId]; ok || len(r.fromChannelId) == 0 {
				r.fromChannels = append(r.fromChannels, c)
			}
		}
	}
	for _, fc := range r.fromChannels {
		for _, tc := range r.toChannels {
			if fc.RemotePubkey != tc.RemotePubkey {
				pair := [2]*lnrpc.Channel{fc, tc}
				r.channelPairs[formatChannelPair(pair[0].ChanId, pair[1].ChanId)] = pair
			}
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

func (r *regolancer) pickChannelPair(amount, minAmount int64,
	relFromAmount, relToAmount float64) (from uint64, to uint64, maxAmount int64, err error) {
	if len(r.channelPairs) == 0 {
		if !r.routeFound || len(r.failureCache) == 0 {
			return 0, 0, 0, errors.New("no routes")
		}
		log.Print(errColor("No channel pairs left, expiring all failed routes"))
		// expire all failed routes
		for k, v := range r.failureCache {
			r.channelPairs[k] = v.channelPair
			delete(r.failureCache, k)
		}

		r.routeFound = false

	}
	var fromChan, toChan *lnrpc.Channel

	idx := rand.Int31n(int32(len(r.channelPairs)))

	var pair [2]*lnrpc.Channel
	for _, pair = range r.channelPairs {
		if idx == 0 {
			break
		}
		idx--
	}
	fromChan = pair[0]
	toChan = pair[1]
	maxFrom := fromChan.Capacity/2 - fromChan.RemoteBalance
	if params.AllowUnbalanceFrom {
		maxFrom = fromChan.LocalBalance
	}
	if relFromAmount > 0 {
		maxFrom = min(maxFrom, int64(float64(fromChan.Capacity)*relFromAmount)-fromChan.RemoteBalance)
	}
	maxTo := toChan.Capacity/2 - toChan.LocalBalance
	if params.AllowUnbalanceTo {
		maxTo = toChan.RemoteBalance
	}
	if relToAmount > 0 {
		maxTo = min(maxTo, int64(float64(toChan.Capacity)*relToAmount)-toChan.LocalBalance)
	}
	if amount == 0 {
		maxAmount = min(maxFrom, maxTo)
	} else {
		maxAmount = min(maxFrom, maxTo, amount)
	}
	if maxAmount < minAmount {
		r.addFailedRoute(fromChan.ChanId, toChan.ChanId)
		return r.pickChannelPair(amount, minAmount, relFromAmount, relToAmount)
	}
	for k, v := range r.failureCache {
		if v.expiration.Before(time.Now()) {
			r.channelPairs[k] = v.channelPair
			delete(r.failureCache, k)
		}
	}
	return fromChan.ChanId, toChan.ChanId, maxAmount, nil
}

func (r *regolancer) addFailedRoute(from, to uint64) {
	t := time.Now().Add(time.Minute * 5)
	k := formatChannelPair(from, to)
	r.failureCache[k] = failedRoute{channelPair: r.channelPairs[k], expiration: &t}
	delete(r.channelPairs, k)
}

func parseScid(chanId string) int64 {

	elements := strings.Split(strings.ToLower(chanId), "x")

	blockHeight, err := strconv.ParseInt(elements[0], 10, 24)
	if err != nil {
		log.Fatalf("error: not able to parse Blockheight of ShortChannelID %s, %s ", chanId, err)
	}
	txIndex, err := strconv.ParseInt(elements[1], 10, 24)
	if err != nil {
		log.Fatalf("error: not able to parse TxIndex of ShortChannelID %s, %s ", chanId, err)

	}
	txPosition, err := strconv.ParseInt(elements[2], 10, 32)

	if err != nil {
		log.Fatalf("error: not able to parse txPosition of ShortChannelID %s, %s ", chanId, err)

	}

	var scId lnwire.ShortChannelID
	scId.BlockHeight = uint32(blockHeight)
	scId.TxIndex = uint32(txIndex)
	scId.TxPosition = uint16(txPosition)

	return int64(scId.ToUint64())

}
