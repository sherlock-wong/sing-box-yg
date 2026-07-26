package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/sherlock-wong/vps-net-manager/internal/reality"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		return
	}
	switch os.Args[1] {
	case "reality-scan":
		realityScan(os.Args[2:])
	case "targets-validate":
		targetsValidate(os.Args[2:])
	case "version":
		fmt.Println("vpnm development")
	default:
		usage()
		os.Exit(2)
	}
}

func realityScan(arguments []string) {
	flags := flag.NewFlagSet("reality-scan", flag.ExitOnError)
	targetsPath := flags.String("targets-file", "/etc/vps-net-manager/reality-targets.txt", "target hostname file")
	samples := flags.Int("samples", 3, "samples per target")
	top := flags.Int("top", 0, "number of ranked results (0 shows all)")
	timeout := flags.Duration("timeout", 35*time.Second, "whole scan timeout")
	flags.Parse(arguments)

	targets, err := reality.LoadTargets(*targetsPath)
	fatalIf(err)
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	results, err := reality.Scan(ctx, targets, *samples, *top)
	fatalIf(err)
	fatalIf(json.NewEncoder(os.Stdout).Encode(results))
}

func targetsValidate(arguments []string) {
	flags := flag.NewFlagSet("targets-validate", flag.ExitOnError)
	targetsPath := flags.String("targets-file", "/etc/vps-net-manager/reality-targets.txt", "target hostname file")
	flags.Parse(arguments)
	targets, err := reality.LoadTargets(*targetsPath)
	fatalIf(err)
	for _, target := range targets {
		fmt.Println(target)
	}
}

func fatalIf(err error) {
	if err == nil {
		return
	}
	fmt.Fprintln(os.Stderr, "vpnm:", err)
	os.Exit(1)
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: vpnm reality-scan|targets-validate [flags]")
}
