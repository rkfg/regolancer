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

func (r *regolancer) createInvoice(ctx context.Context, from, to uint64, amount int64) (*lnrpc.AddInvoiceResponse, error) {
	return r.lnClient.AddInvoice(ctx, &lnrpc.Invoice{Value: amount,
		Memo:   fmt.Sprintf("Rebalance %d ⇒ %d", from, to),
		Expiry: int64(time.Hour.Seconds() * 24)})
}

func (r *regolancer) pay(ctx context.Context, invoice *lnrpc.AddInvoiceResponse, amount int64,
	minAmount int64, route *lnrpc.Route, probeSteps int) error {
	fmt.Println()
	defer fmt.Println()
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
		return err
	}
	if result.Status == lnrpc.HTLCAttempt_FAILED {
		nodeCtx, cancel := context.WithTimeout(ctx, time.Minute)
		defer cancel()
		node1, err := r.getNodeInfo(nodeCtx, route.Hops[result.Failure.FailureSourceIndex-1].PubKey)
		node1name := ""
		node2name := ""
		if err != nil {
			node1name = fmt.Sprintf("node%d", result.Failure.FailureSourceIndex-1)
		} else {
			node1name = node1.Node.Alias
		}
		node2, err := r.getNodeInfo(nodeCtx, route.Hops[result.Failure.FailureSourceIndex].PubKey)
		if err != nil {
			node2name = fmt.Sprintf("node%d", result.Failure.FailureSourceIndex)
		} else {
			node2name = node2.Node.Alias
		}
		log.Printf("%s %s ⇒ %s", faintWhiteColor(result.Failure.Code.String()),
			cyanColor(node1name), cyanColor(node2name))
		if probeSteps > 0 && int(result.Failure.FailureSourceIndex) == len(route.Hops)-2 &&
			result.Failure.Code == lnrpc.Failure_TEMPORARY_CHANNEL_FAILURE {
			fmt.Println("Probing route...")
			min := int64(0)
			start := amount / 2
			if minAmount > 0 && minAmount < amount {
				min = -minAmount - 1
				start = minAmount
			}
			maxAmount, err := r.probeRoute(ctx, route, min, amount, start, probeSteps, params.EconRatio)
			if err != nil {
				return err
			}
			if maxAmount == 0 {
				return ErrProbeFailed
			}
			return ErrRetry{amount: maxAmount}
		}
		return fmt.Errorf("error: %s @ %d", result.Failure.Code.String(), result.Failure.FailureSourceIndex)
	} else {
		log.Printf("Success! Paid %s in fees", hiWhiteColor(result.Route.TotalFeesMsat/1000))
		if r.statFilename != "" {
			_, err := os.Stat(r.statFilename)
			f, ferr := os.OpenFile(r.statFilename, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
			if ferr != nil {
				log.Print(errColorF("Error saving rebalance stats to %s: %s", r.statFilename, ferr))
				return nil
			}
			defer f.Close()
			if os.IsNotExist(err) {
				f.WriteString("timestamp,from_channel,to_channel,amount_msat,fees_msat\n")
			}
			f.Write([]byte(fmt.Sprintf("%d,%d,%d,%d,%d\n", time.Now().Unix(), route.Hops[0].ChanId,
				lastHop.ChanId, route.TotalAmtMsat-route.TotalFeesMsat, route.TotalFeesMsat)))
		}
		return nil
	}
}
