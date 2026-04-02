// Package types contains shared types used across the Antminer plugin
package types

import (
	"os"
	"strconv"
	"time"

	"github.com/block/proto-fleet/plugin/antminer/pkg/antminer"
)

// ClientFactory is a function type for creating Antminer clients
// This allows for dependency injection and easier testing
type ClientFactory func(host string, rpcPort, webPort int32, urlScheme string) (antminer.AntminerClient, error)

// WebPort returns the web API port, reading from ANTMINER_WEB_PORT env var
// with a default of 80.
func WebPort() int32 {
	if v := os.Getenv("ANTMINER_WEB_PORT"); v != "" {
		if port, err := strconv.ParseInt(v, 10, 32); err == nil {
			return int32(port)
		}
	}
	return 80
}

// StatusCacheTTL returns the status cache TTL, reading from ANTMINER_STATUS_CACHE_TTL
// env var (Go duration string) with a default of 5s.
func StatusCacheTTL() time.Duration {
	if v := os.Getenv("ANTMINER_STATUS_CACHE_TTL"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return 5 * time.Second
}
