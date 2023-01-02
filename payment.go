package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/lightningnetwork/lnd/lnrpc"
	"github.com/lightningnetwork/lnd/lnrpc/routerrpc"
)

type ErrRetry struct {
	amount int64
}

func (e ErrRetry) Error() string {
	return fmt.Sprintf("retry payment with %d sats", e.amount)
}

var ErrProbeFailed = fmt.Errorf("probe failed")

func (r *regolancer) createInvoice(ctx context.Context, amount int64) (result *lnrpc.AddInvoiceResponse, err error) {
	var ok bool
	if result, ok = r.invoiceCache[amount]; ok {
		return
	}
	result, err = r.lnClient.AddInvoice(ctx, &lnrpc.Invoice{Value: amount,
		Memo:   "Rebalance attempt",
		Expiry: int64(time.Hour.Seconds() * 24)})
	r.invoiceCache[amount] = result

	return
}

func (r *regolancer) invalidateInvoice(amount int64) {
	delete(r.invoiceCache, amount)
}

func (r *regolancer) pay(ctx context.Context, amount int64, minAmount int64, maxFeeMsat int64,
	route *lnrpc.Route, probeSteps int) error {
	fmt.Println()
	defer fmt.Println()

	if route.TotalFeesMsat > maxFeeMsat {
		log.Printf("fee on the route exceeds our limits: %s ppm (max fee %s ppm)", formatFeePPM(amount*1000, route.TotalFeesMsat), formatFeePPM(amount*1000, maxFeeMsat))
		return fmt.Errorf("fee-limit exceeded")
	}

	invoice, err := r.createInvoice(ctx, amount)
	if err != nil {
		log.Printf("Error creating invoice: %s", err)
		return err
	}
	defer func() {
		if ctx.Err() == context.DeadlineExceeded {
			r.invalidateInvoice(amount)
		}
	}()
	lastHop := route.Hops[len(route.Hops)-1]
	lastHop.MppRecord = &lnrpc.MPPRecord{
		PaymentAddr:  invoice.PaymentAddr,
		TotalAmtMsat: amount * 1000,
	}

	result, err := r.routerClient.SendToRouteV2(ctx,
		&routerrpc.SendToRouteRequest{
			PaymentHash: invoice.RHash,
			Route:       route,
		})
	if err != nil {
		logErrorF("error sending payment %s", err)
		return err
	}
	if result.Status == lnrpc.HTLCAttempt_FAILED {
		if result.Failure.FailureSourceIndex >= uint32(len(route.Hops)) {
			logErrorF("%s (unexpected hop index %d, should be less than %d)", result.Failure.Code.String(),
				result.Failure.FailureSourceIndex, len(route.Hops))
			return fmt.Errorf("error: %s @ %d", result.Failure.Code.String(),
				result.Failure.FailureSourceIndex)
		}
		if result.Failure.FailureSourceIndex == 0 {
			logErrorF("%s (unexpected hop index %d, should be greater than 0)", result.Failure.Code.String(),
				result.Failure.FailureSourceIndex)
			return fmt.Errorf("error: %s @ %d", result.Failure.Code.String(),
				result.Failure.FailureSourceIndex)
		}

		prevHop := route.Hops[result.Failure.FailureSourceIndex-1]
		failedHop := route.Hops[result.Failure.FailureSourceIndex]
		nodeCtx, cancel := context.WithTimeout(ctx, time.Second*time.Duration(params.TimeoutInfo))
		defer cancel()
		node1, err := r.getNodeInfo(nodeCtx, prevHop.PubKey)
		node1name := ""
		node2name := ""
		if err != nil {
			node1name = fmt.Sprintf("node%d", result.Failure.FailureSourceIndex-1)
		} else {
			node1name = node1.Node.Alias
		}
		node2, err := r.getNodeInfo(nodeCtx, failedHop.PubKey)
		if err != nil {
			node2name = fmt.Sprintf("node%d", result.Failure.FailureSourceIndex)
		} else {
			node2name = node2.Node.Alias
		}
		log.Printf("%s %s ⇒ %s", faintWhiteColor(result.Failure.Code.String()), cyanColor(node1name), cyanColor(node2name))

		if result.Failure.Code == lnrpc.Failure_FEE_INSUFFICIENT || result.Failure.Code == lnrpc.Failure_INCORRECT_CLTV_EXPIRY {
			failedHop := route.Hops[result.Failure.FailureSourceIndex-1]
			route, err = r.rebuildRoute(ctx, route, amount)
			updatedHop := route.Hops[result.Failure.FailureSourceIndex-1]
			if err == nil {
				// compare hops to make sure we do not loop endlessly
				if !compareHops(failedHop, updatedHop) {
					log.Printf("received channelupdate after failure, trying again with amt %s and fee %s ppm",
						hiWhiteColor(amount), formatFeePPM(amount*1000, route.TotalFeesMsat))
					return r.pay(ctx, amount, minAmount, maxFeeMsat, route, probeSteps)
				}
			}
		}
		if result.Failure.Code == lnrpc.Failure_TEMPORARY_CHANNEL_FAILURE {
			r.addFailedChan(node1.Node.PubKey, node2.Node.PubKey, prevHop.
				AmtToForwardMsat)
		}
		if probeSteps > 0 && int(result.Failure.FailureSourceIndex) == len(route.Hops)-2 &&
			result.Failure.Code == lnrpc.Failure_TEMPORARY_CHANNEL_FAILURE {
			fmt.Println("Probing route...")
			min := int64(0)
			start := amount / 2
			if minAmount > 0 && minAmount < amount {
				// need to use -1 so we do not fail the first probing attempt
				min = -minAmount - 1
				start = minAmount
			}
			maxAmount, err := r.probeRoute(ctx, route, min, amount, start,
				probeSteps)

			if err != nil {
				logErrorF("Probe error: %s", err)
				return err
			}
			if maxAmount == 0 {
				return ErrProbeFailed
			}
			return ErrRetry{amount: maxAmount}
		}
		return fmt.Errorf("error: %s @ %d", result.Failure.Code.String(), result.Failure.FailureSourceIndex)
	} else {
		log.Printf("Success! Paid %s in fees, %s ppm",
			formatFee(result.Route.TotalFeesMsat), formatFeePPM(result.Route.TotalAmtMsat-result.Route.TotalFeesMsat, result.Route.TotalFeesMsat))
		if r.statFilename != "" {

			l := lock()
			err := l.Lock()
			defer l.Unlock()

			if err != nil {
				return fmt.Errorf("error taking exclusive lock on file %s: %s", r.statFilename, err)
			}

			_, err = os.Stat(r.statFilename)
			f, ferr := os.OpenFile(r.statFilename, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
			if ferr != nil {
				logErrorF("Error saving rebalance stats to %s: %s", r.statFilename, ferr)
				return nil
			}
			defer f.Close()
			if os.IsNotExist(err) {
				f.WriteString("timestamp,from_channel,to_channel,amount_msat,fees_msat\n")
			}
			f.Write([]byte(fmt.Sprintf("%d,%d,%d,%d,%d\n", time.Now().Unix(), route.Hops[0].ChanId,
				lastHop.ChanId, route.TotalAmtMsat-route.TotalFeesMsat, route.TotalFeesMsat)))
		}
		// Necessary for Rapid Rebalancing
		r.invalidateInvoice(amount)
		return nil
	}
}
