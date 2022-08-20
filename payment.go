package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/lightningnetwork/lnd/lnrpc"
	"github.com/lightningnetwork/lnd/lnrpc/routerrpc"
)

func (r *regolancer) createInvoice(from, to uint64, amount int64) (*lnrpc.AddInvoiceResponse, error) {
	return r.lnClient.AddInvoice(context.Background(), &lnrpc.Invoice{Value: amount,
		Memo:   fmt.Sprintf("Rebalance %d ⇒ %d", from, to),
		Expiry: int64(time.Hour.Seconds() * 24)})
}

func (r *regolancer) pay(invoice *lnrpc.AddInvoiceResponse, amount int64, route *lnrpc.Route) error {
	lastHop := route.Hops[len(route.Hops)-1]
	lastHop.MppRecord = &lnrpc.MPPRecord{
		PaymentAddr:  invoice.PaymentAddr,
		TotalAmtMsat: amount * 1000,
	}
	result, err := r.routerClient.SendToRouteV2(context.Background(),
		&routerrpc.SendToRouteRequest{
			PaymentHash: invoice.RHash,
			Route:       route,
			SkipTempErr: true,
		})
	if err != nil {
		return err
	}
	if result.Status == lnrpc.HTLCAttempt_FAILED {
		node1, err := r.getNodeInfo(route.Hops[result.Failure.FailureSourceIndex-1].PubKey)
		node1name := ""
		node2name := ""
		if err != nil {
			node1name = fmt.Sprintf("node%d", result.Failure.FailureSourceIndex-1)
		} else {
			node1name = node1.Node.Alias
		}
		node2, err := r.getNodeInfo(route.Hops[result.Failure.FailureSourceIndex].PubKey)
		if err != nil {
			node2name = fmt.Sprintf("node%d", result.Failure.FailureSourceIndex)
		} else {
			node2name = node2.Node.Alias
		}
		fmt.Printf("\n%s %s ⇒ %s\n\n", faintWhiteColor(result.Failure.Code.String()),
			cyanColor(node1name), cyanColor(node2name))
		return fmt.Errorf("error: %s @ %d", result.Failure.Code.String(), result.Failure.FailureSourceIndex)
	} else {
		log.Printf("Success! Paid %s in fees", hiWhiteColor(result.Route.TotalFeesMsat/1000))
		return nil
	}
}
