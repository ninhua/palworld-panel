//go:build linux

package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"palpanel/internal/buildinfo"
	"palpanel/internal/panelupdater"
)

func main() {
	os.Exit(run())
}

func run() int {
	requestPath := flag.String("request", "", "path to the pending panel update request")
	showVersion := flag.Bool("version", false, "print version")
	flag.Parse()
	info := buildinfo.Current()
	if *showVersion {
		fmt.Printf("palpanel-updater %s (%s)\n", info.Version, info.Commit)
		return 0
	}
	if os.Geteuid() != 0 {
		fmt.Fprintln(os.Stderr, "palpanel-updater must run as root through palpanel-update.service")
		return 77
	}
	if *requestPath == "" {
		fmt.Fprintln(os.Stderr, "usage: palpanel-updater --request /var/lib/palpanel/panel-update/request.json")
		return 64
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	result := panelupdater.Execute(ctx, *requestPath, panelupdater.DefaultConfig())
	body, _ := json.Marshal(result)
	fmt.Println(string(body))
	if result.Status == "completed" {
		return 0
	}
	if result.RollbackSucceeded {
		return 1
	}
	return 2
}
