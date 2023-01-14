package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/lightningnetwork/lnd/lnrpc"
	"github.com/mattn/go-runewidth"
)

func printBooleanOption(name string, value bool) {
	v := "disabled"
	if value {
		v = "enabled"
	}
	fmt.Printf("%s: %s\n", name, hiWhiteColor(v))
}

func (r *regolancer) printChannelInfo(ctx context.Context, channel *lnrpc.Channel) error {
	nodeInfo, err := r.getNodeInfo(ctx, channel.RemotePubkey)
	if err != nil {
		return err
	}
	alias := runewidth.FillRight(runewidth.Truncate(nodeInfo.Node.Alias, 25, ""), 25)
	balance := runewidth.FillRight(strings.Repeat("|", int(channel.LocalBalance*15/channel.Capacity)), 15)
	balancePct := channel.LocalBalance * 100 / channel.Capacity
	fmt.Printf("%s [%s] %d%%    ", alias, cyanColor(balance), balancePct)
	return nil
}

func (r *regolancer) info(ctx context.Context) error {
	fromIdx := 0
	toIdx := 0
	sep := strings.Repeat("—", 98)
	fmt.Printf("%s\nFrom %s channels %33s To %s channels\n%s\n", sep, hiWhiteColor(len(r.fromChannels)), "", hiWhiteColor(len(r.toChannels)), sep)
	for {
		if fromIdx < len(r.fromChannels) {
			channel := r.fromChannels[fromIdx]
			err := r.printChannelInfo(ctx, channel)
			if err != nil {
				return err
			}
			fromIdx++
		} else {
			fmt.Print(strings.Repeat(" ", 51))
		}
		if toIdx < len(r.toChannels) {
			channel := r.toChannels[toIdx]
			err := r.printChannelInfo(ctx, channel)
			if err != nil {
				return err
			}
			toIdx++
		}
		fmt.Println()
		if fromIdx >= len(r.fromChannels) && toIdx >= len(r.toChannels) {
			break
		}
	}
	fmt.Println(sep)
	fmt.Printf("Min amount: %s sat\n", formatAmt(params.MinAmount))
	if params.Amount > 0 {
		fmt.Printf("Amount: %s sat\n", formatAmt(params.Amount))
	} else {
		if params.RelAmountFrom > 0 {
			fmt.Printf("Relative amount from: %s%%\n", formatAmt(int64(params.RelAmountFrom*100)))
		}
		if params.RelAmountTo > 0 {
			fmt.Printf("Relative amount to: %s%%\n", formatAmt(int64(params.RelAmountTo*100)))
		}
	}
	if params.FeeLimitPPM > 0 {
		fmt.Printf("Max fee: %s ppm", formatAmt(int64(params.FeeLimitPPM)))
	} else if params.EconRatio > 0 {
		fmt.Printf("Max fee: %s%% of target channel ppm", formatAmt(int64(params.EconRatio*100)))
		if params.EconRatioMaxPPM > 0 {
			fmt.Printf(" (but <= %s ppm)", formatAmt(int64(params.EconRatioMaxPPM)))
		}
	}
	fmt.Println()
	if params.ExcludeChannelAge != 0 {
		fmt.Printf("Channel age needs to be >= %s blocks\n", hiWhiteColor(params.ExcludeChannelAge))
	}
	fmt.Printf("Fail tolerance: %s ppm\n", formatAmt(int64(params.FailTolerance)))
	printBooleanOption("Rapid rebalance", params.AllowRapidRebalance)
	printBooleanOption("Lost profit accounting", params.LostProfit)
	if params.ProbeSteps > 0 {
		fmt.Printf("Probing steps: %s\n", hiWhiteColor(params.ProbeSteps))
	}
	fmt.Printf("Node cache size: %s records, life time: %s days %s hours %s minutes\n", hiWhiteColor(len(r.nodeCache)), hiWhiteColor(params.NodeCacheLifetime/1440), hiWhiteColor(params.NodeCacheLifetime%1440/60), hiWhiteColor(params.NodeCacheLifetime%60))
	printBooleanOption("Show node cache hits", params.NodeCacheInfo)
	fmt.Printf("Total rebalance timeout: %s hours %s minutes\n", hiWhiteColor(params.TimeoutRebalance/60), hiWhiteColor(params.TimeoutRebalance%60))
	fmt.Printf("Single attempt timeout: %s minutes\n", hiWhiteColor(params.TimeoutAttempt))
	fmt.Printf("Info query timeout: %s seconds\n", hiWhiteColor(params.TimeoutInfo))
	fmt.Printf("Route query timeout: %s seconds\n", hiWhiteColor(params.TimeoutRoute))
	return nil
}
