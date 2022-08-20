package main

import "github.com/fatih/color"

var (
	faintWhiteColor = color.New(color.FgWhite, color.Faint).SprintFunc()
	hiWhiteColor    = color.New(color.FgHiWhite, color.Bold).SprintfFunc()
	cyanColor       = color.New(color.FgBlue, color.Bold).SprintFunc()
	errColor        = color.New(color.FgHiRed, color.Bold).SprintFunc()
)
