// Package main implements a Virtual miner plugin for the Fleet mining system.
//
// This plugin provides simulated miners for testing and demonstration purposes.
// Virtual miners don't require any network hardware and can be configured via JSON.
//
// Usage:
//  1. Place config.json in the same directory as the plugin binary
//  2. Use IP List discovery mode with IPs from the 10.255.x.x range
//  3. Pair discovered virtual miners (any credentials work)
package main

import (
	"log"
	"os"
	"path/filepath"

	"github.com/block/proto-fleet/plugin/virtual/internal/driver"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/hashicorp/go-plugin"
)

func main() {
	// Config file is in the same directory as the plugin binary.
	// Use os.Args[0] (set by go-plugin to the absolute binary path) because
	// os.Executable() reads /proc/self/exe which resolves to the ELF interpreter
	// (/lib/ld-linux-aarch64.so.1) on Alpine+gcompat, giving the wrong directory.
	execPath := os.Args[0]
	if !filepath.IsAbs(execPath) {
		var err error
		execPath, err = os.Executable()
		if err != nil {
			log.Fatalf("Failed to get executable path: %v", err)
		}
	}
	configPath := filepath.Join(filepath.Dir(execPath), "config.json")

	// Create the plugin driver
	virtualDriver, err := driver.New(configPath)
	if err != nil {
		log.Fatalf("Failed to create virtual driver: %v", err)
	}

	// Serve the plugin using the Fleet SDK
	plugin.Serve(&plugin.ServeConfig{
		HandshakeConfig: sdk.HandshakeConfig,
		Plugins: map[string]plugin.Plugin{
			"driver": &sdk.DriverPlugin{Impl: virtualDriver},
		},
		GRPCServer: plugin.DefaultGRPCServer,
	})
}
