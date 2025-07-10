package main

import (
	"fmt"
	"os"
	"runtime/debug"
)

func printVersion() {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		fmt.Printf("No build info available")
	} else {
		settings := map[string]string{}
		for _, bs := range info.Settings {
			settings[bs.Key] = bs.Value
		}
		version := info.Main.Version
		if rev, ok := settings["vcs.revision"]; ok && version == "(devel)" {
			version = "git" + rev[:8]
		}
		if settings["vcs.modified"] == "true" {
			version += "-dirty"
		}
		fmt.Printf("Regolancer %s, built with %s\n"+
			"Source: https://github.com/feelancer21/regolancer\n"+
			"Forked from: https://github.com/rkfg/regolancer\n", version, info.GoVersion)
	}
	os.Exit(1)
}
