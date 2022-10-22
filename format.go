package main

import (
	"fmt"

	"github.com/fatih/color"
)

var (
	faintWhiteColor = color.New(color.FgWhite, color.Faint).SprintFunc()
	hiWhiteColor    = color.New(color.FgHiWhite, color.Bold).SprintFunc()
	hiWhiteColorF   = color.New(color.FgHiWhite, color.Bold).SprintfFunc()
	cyanColor       = color.New(color.FgBlue, color.Bold).SprintFunc()
	errColor        = color.New(color.FgHiRed, color.Bold).SprintFunc()
	errColorF       = color.New(color.FgHiRed, color.Bold).SprintfFunc()
	infoColor       = color.New(color.FgHiYellow, color.Bold).SprintFunc()
	infoColorF      = color.New(color.FgHiYellow, color.Bold).SprintfFunc()
)

func formatAmt(amt int64) string {
	btc := amt / COIN
	ms := amt % COIN / 1e6
	ts := amt % 1e6 / 1e3
	s := amt % 1e3
	if btc > 0 {
		return fmt.Sprintf("%s.%s,%s,%s", infoColorF("%d", btc), infoColorF("%02d", ms),
			infoColorF("%03d", ts), infoColorF("%03d", s))
	}
	if ms > 0 {
		return fmt.Sprintf("%s,%s,%s", infoColorF("%d", ms), infoColorF("%03d", ts), infoColorF("%03d", s))
	}
	if ts > 0 {
		return fmt.Sprintf("%s,%s", infoColorF("%d", ts), infoColorF("%03d", s))
	}
	if s >= 0 {
		return infoColorF("%d", s)
	}
	return errColor("error: ", amt)
}

func formatFee(amtMsat int64) string {
	if amtMsat < 1000 {
		return hiWhiteColorF("0.%03d", amtMsat)
	}
	return hiWhiteColor(amtMsat / 1000)
}

func formatFeePPM(amtMsat int64, feeMsat int64) string {

	return hiWhiteColor(int64(float64(feeMsat) / float64(amtMsat) * 1e6))
}
