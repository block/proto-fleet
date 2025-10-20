// Package types contains shared types used across the Antminer plugin
package types

import "github.com/btc-mining/proto-fleet/plugin/antminer/pkg/antminer"

// ClientFactory is a function type for creating Antminer clients
// This allows for dependency injection and easier testing
type ClientFactory func(host string, rpcPort, webPort int32, urlScheme string) (antminer.AntminerClient, error)
