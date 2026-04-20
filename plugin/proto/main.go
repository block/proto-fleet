// Package main implements a Proto miner plugin for the Fleet mining system.
//
// This plugin demonstrates:
//   - SDK v1 interface implementation
//   - Device discovery and management
//   - Authentication and secure communication
//   - Telemetry collection and reporting
//   - Error handling and logging
//
// For full documentation, see docs/getting-started.md
package main

import (
	"log"
	"os"
	"strconv"

	"github.com/block/proto-fleet/plugin/proto/internal/driver"
	"github.com/block/proto-fleet/server/sdk/v1"
	"github.com/hashicorp/go-plugin"
)

const (
	requiredPort = 443
)

func main() {
	port, err := strconv.Atoi(os.Getenv("PROTO_MINER_PORT"))
	if err != nil || port <= 0 {
		port = requiredPort
	}

	// Create the plugin driver
	protoDriver, err := driver.New(port)
	if err != nil {
		log.Fatalf("Failed to create proto driver: %v", err)
	}

	// Serve the plugin using the Fleet SDK
	plugin.Serve(&plugin.ServeConfig{
		HandshakeConfig: sdk.HandshakeConfig,
		Plugins: map[string]plugin.Plugin{
			"driver": &sdk.DriverPlugin{Impl: protoDriver},
		},
		GRPCServer: plugin.DefaultGRPCServer,
	})
}
