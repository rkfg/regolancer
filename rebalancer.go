package main

import (
	"context"
	"encoding/hex"
	"errors"
	"log"
	"time"

	"github.com/lightningnetwork/lnd/lnrpc"
)

type rebalanceResult struct {
	successfulAttempts int
	failedAttempts     int
	successfulAmt      int64
	paidFeeMsat        int64
}

const (
	increaseAmtRapidRebalance string = "increase"
	decreaseAmtRapidRebalance string = "decrease"
)

func (r *regolancer) tryRebalance(ctx context.Context, attempt *int) (err error,
	repeat bool) {
	attemptCtx, attemptCancel := context.WithTimeout(ctx, time.Minute*time.Duration(params.TimeoutAttempt))

	defer attemptCancel()

	from, to, amt, err := r.pickChannelPair(params.Amount, params.MinAmount, params.RelAmountFrom, params.RelAmountTo)
	if err != nil {
		log.Printf(errColor("Error during picking channel: %s"), err)
		return err, false
	}
	routeCtx, routeCtxCancel := context.WithTimeout(attemptCtx, time.Second*time.Duration(params.TimeoutRoute))
	defer routeCtxCancel()
	routes, maxFeeMsat, err := r.getRoutes(routeCtx, from, to, amt*1000)
	if err != nil {
		if routeCtx.Err() == context.DeadlineExceeded {
			log.Print(errColor("Timed out looking for a route"))
			return err, false
		}
		r.addFailedRoute(from, to)
		return err, true
	}
	routeCtxCancel()
	for _, route := range routes {
		log.Printf("Attempt %s, amount: %s (max fee: %s sat | %s ppm )",
			hiWhiteColorF("#%d", *attempt), hiWhiteColor(amt), formatFee(maxFeeMsat), formatFeePPM(amt*1000, maxFeeMsat))
		r.printRoute(attemptCtx, route)
		err = r.pay(attemptCtx, amt, params.MinAmount, maxFeeMsat, route, params.ProbeSteps)
		if err == nil {

			if params.AllowRapidRebalance {
				rebalanceResult, _ := r.tryRapidRebalance(ctx, route)

				if rebalanceResult.successfulAttempts > 0 || rebalanceResult.failedAttempts > 0 {
					log.Printf("%s rapid rebalances were successful, total amount: %s (fee: %s sat | %s ppm) - Failed Attempts: %s\n",
						hiWhiteColor(rebalanceResult.successfulAttempts), hiWhiteColor(rebalanceResult.successfulAmt),
						formatFee(rebalanceResult.paidFeeMsat), formatFeePPM(rebalanceResult.successfulAmt*1000, rebalanceResult.paidFeeMsat),
						hiWhiteColor(rebalanceResult.failedAttempts))
				}
				log.Printf("Finished rapid rebalancing")
			}

			return nil, false
		}
		if retryErr, ok := err.(ErrRetry); ok {
			amt = retryErr.amount
			log.Printf("Trying to rebalance again with %s", hiWhiteColor(amt))
			probedRoute, err := r.rebuildRoute(attemptCtx, route, amt)
			if err != nil {
				log.Printf("Error rebuilding the route for probed payment: %s", errColor(err))
			} else {
				err = r.pay(ctx, amt, 0, maxFeeMsat, probedRoute, 0)
				if err == nil {
					return nil, false
				} else {
					r.invalidateInvoice(amt)
					log.Printf("Probed rebalance failed with error: %s", errColor(err))
				}
			}
		}

		*attempt++
	}
	attemptCancel()
	if attemptCtx.Err() == context.DeadlineExceeded {
		log.Print(errColor("Attempt timed out"))
	}

	return nil, true
}

func (r *regolancer) tryRapidRebalance(ctx context.Context, route *lnrpc.Route) (result rebalanceResult, err error) {

	var (
		amt  int64  = (route.TotalAmtMsat - route.TotalFeesMsat) / 1000
		from uint64 = getSource(route)
		to   uint64 = getTarget(route)

		// Need to save the route and amount locally because we are changing it via the accelerator
		// In case we reuse the route it will lead to a situation where no route is found
		// the route variable will be overwritten and we are loosing the information
		routeLocal           *lnrpc.Route
		amtLocal             int64 = amt
		accelerator          int64 = 1
		hittingTheWall       bool
		exitEarly            bool
		capReached           bool
		maxAmountOnRouteMsat uint64
		minAmount            uint64
		rebalanceStrategy    string = increaseAmtRapidRebalance
	)

	result.successfulAttempts = 0
	result.failedAttempts = 0
	// Include Initial Rebalance
	result.successfulAmt = amt
	result.paidFeeMsat = route.TotalFeesMsat

	maxAmountOnRouteMsat, err = r.maxAmountOnRoute(ctx, route)
	if err != nil {
		return result, err
	}

	if params.MinAmount > 0 {
		minAmount = uint64(params.MinAmount)
	} else {
		minAmount = 10000
	}

Loop:
	for {
		switch rebalanceStrategy {
		case increaseAmtRapidRebalance:
			if hittingTheWall {
				accelerator >>= 1
				// In case we encounter that we are already constrained
				// by the liquidity on the channels we are waiting for
				// the accelerator to go below this amount to save
				// already failed rebalances
				if amtLocal < accelerator*amt && amtLocal > int64(minAmount) {
					continue
				}
			} else if !capReached {
				// we only increase the amount if the max Amount on the
				// route is still not reached
				accelerator <<= 1
			}

			if uint64(accelerator*amt) < maxAmountOnRouteMsat/1000 {
				amtLocal = accelerator * amt
			} else if !capReached {
				capReached = true
				log.Printf("Max amount on route reached capping amount at %s sats "+
					"| max amount on route (max htlc size) %s sats\n", infoColor(amtLocal), infoColor(maxAmountOnRouteMsat/1000))
			}
			// We reached the initial amount again.
			// now we switch to the decreasing strategy.
			// We half the amount on every step we go down.
			if accelerator < 1 {
				accelerator = 2
				rebalanceStrategy = decreaseAmtRapidRebalance
				amtLocal = amt / accelerator
				if amtLocal < int64(minAmount) {
					break Loop
				}
			}

		case decreaseAmtRapidRebalance:
			accelerator <<= 1
			if amtLocal < amt/accelerator {
				continue
			}

			amtLocal = amt / accelerator
			if amtLocal < int64(minAmount) {
				break Loop
			}
		}

		if exitEarly {
			break Loop
		}

		log.Printf("Rapid rebalance attempt %s, amount: %s\n", hiWhiteColor(result.successfulAttempts+1), hiWhiteColor(amtLocal))

		cTo, err := r.getChanInfo(ctx, to)

		if err != nil {
			logErrorF("Error fetching target channel: %s", err)
			return result, err
		}
		cFrom, err := r.getChanInfo(ctx, from)

		if err != nil {
			logErrorF("Error fetching source channel: %s", err)
			return result, err
		}

		fromPeer, _ := hex.DecodeString(cFrom.Node1Pub)
		if cFrom.Node1Pub == r.myPK {
			fromPeer, _ = hex.DecodeString(cFrom.Node2Pub)
		}
		fromChan, err := r.lnClient.ListChannels(ctx, &lnrpc.ListChannelsRequest{ActiveOnly: true, PublicOnly: true, Peer: fromPeer})

		if err != nil {
			logErrorF("Error fetching source channel: %s", err)
			return result, err

		}
		toPeer, _ := hex.DecodeString(cTo.Node1Pub)
		if cTo.Node1Pub == r.myPK {
			toPeer, _ = hex.DecodeString(cTo.Node2Pub)
		}

		toChan, err := r.lnClient.ListChannels(ctx, &lnrpc.ListChannelsRequest{ActiveOnly: true, PublicOnly: true, Peer: toPeer})

		if err != nil {
			logErrorF("Error fetching target channel: %s", err)
			return result, err
		}

		for k := range r.fromChannelId {
			delete(r.fromChannelId, k)
		}
		r.fromChannelId = makeChanSet([]uint64{from})

		for k := range r.toChannelId {
			delete(r.toChannelId, k)
		}
		r.toChannelId = makeChanSet([]uint64{to})

		r.channels = r.channels[:0]
		r.fromChannels = r.fromChannels[:0]
		r.toChannels = r.toChannels[:0]

		r.channels = append(append(r.channels, toChan.Channels...),
			fromChan.Channels...)

		for k := range r.failureCache {
			delete(r.failureCache, k)
		}

		for k := range r.channelPairs {
			delete(r.channelPairs, k)
		}

		err = r.getChannelCandidates(params.FromPerc, params.ToPerc, amtLocal)

		if err != nil {
			logErrorF("Error selecting channel candidates: %s", err)
			return result, err
		}

		amtLocalTemp := amtLocal
		_, _, amtLocal, err = r.pickChannelPair(amtLocal, params.MinAmount, params.RelAmountFrom, params.RelAmountTo)

		if err != nil {
			log.Printf(errColor("Error during picking channel: %s"), err)
			hittingTheWall = true
			// We are not returning an error here because
			// in we still could rebalance an amount in the
			// decreasing strategy.
			// return result, err
			continue
		}

		if amtLocalTemp > amtLocal {
			log.Printf("Rapid fire starting with actual amount: %s (could be lower than the attempted amount in case there is less liquidity available on the channel)", hiWhiteColor(amtLocal))
			// We are already using maximum available liquidity so we can begin decreasing amounts again.
			hittingTheWall = true
			// This is needed so we do not test further amounts
			// when in the decreasing strategy.
			if rebalanceStrategy == decreaseAmtRapidRebalance {
				exitEarly = true
			}
		}

		routeLocal, err = r.rebuildRoute(ctx, route, amtLocal)

		if err != nil {
			log.Printf(errColor("Error building route: %s"), err)
			return result, err
		}

		attemptCtx, attemptCancel := context.WithTimeout(ctx, time.Minute*time.Duration(params.TimeoutAttempt))

		defer attemptCancel()

		// make sure we account for fees when increasing the rebalance amount
		maxFeeMsat, _, err := r.calcFeeMsat(ctx, from, to, amtLocal*1000)

		if err != nil {
			log.Printf(errColor("Error calculating fee: %s"), err)
			return result, err
		}

		err = r.pay(attemptCtx, amtLocal, params.MinAmount, maxFeeMsat, routeLocal, 0)

		// In case we are already decreasing the amount we can exit early because
		// for even smaller amounts the fee will be higher (reason is the basefee).
		if rebalanceStrategy == decreaseAmtRapidRebalance && errors.Is(err, ErrFeeExceeded) {
			exitEarly = true
		}

		attemptCancel()

		if attemptCtx.Err() == context.DeadlineExceeded {
			log.Print(errColor("Rapid rebalance attempt timed out"))
			return result, attemptCtx.Err()
		}

		if err != nil {
			log.Printf("Rebalance failed with %s", err)
			log.Println()
			result.failedAttempts++
			hittingTheWall = true
		} else {
			result.successfulAttempts++
			result.successfulAmt += amtLocal
			result.paidFeeMsat += routeLocal.TotalFeesMsat
		}
	}
	return result, nil
}
