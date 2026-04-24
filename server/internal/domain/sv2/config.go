// Package sv2 holds deployment-level Stratum V2 wiring: the Kong-parsed
// config block that gates the bundled translator proxy and the TCP
// health probe the server runs against it.
//
// The rewriter and preflight (under pools/) are plugged into the command
// service at startup using values from this package, which keeps them
// free of deployment concerns and free of Kong imports.
package sv2

import (
	"fmt"
	"time"

	"github.com/block/proto-fleet/server/internal/domain/pools/rewriter"
)

// Config is the Kong-parsed StratumV2 block. See the "Server config block"
// section of docs/stratum-v2-plan.md for the semantics table. In short:
// ProxyEnabled controls whether the bundled translator proxy participates
// in the deployment — it is NOT a master "SV2 support" gate. Creating an
// SV2 pool and assigning it to a native-SV2 miner works regardless of
// this flag. Only SV2-to-SV1 routing depends on it.
type Config struct {
	ProxyEnabled         bool          `help:"Enable bundled SV2 translation proxy (lets SV1 miners mine SV2 pools). Off by default; operators with native-SV2-only fleets never need to flip it on." default:"false" env:"PROXY_ENABLED"`
	ProxyMinerURL        string        `help:"stratum+tcp URL the SV1 miners on the LAN should be pointed at when their assigned pool is SV2. Pushed by the URL rewriter at commit time." default:"" env:"PROXY_MINER_URL"`
	ProxyUpstreamURL     string        `help:"sv2+* URL the tProxy connects upstream to (the actual SV2 pool). Read once at proxy startup and baked into the tProxy TOML; not consulted by Fleet at dispatch time." default:"" env:"PROXY_UPSTREAM_URL"`
	ProxyHealthCheckAddr string        `help:"host:port Fleet uses to TCP-probe the bundled proxy. Default assumes host-network Compose (fleet-api on the host network reaches tProxy via 127.0.0.1); bridge-network operators override this." default:"127.0.0.1:34255" env:"PROXY_HEALTH_ADDR"`
	ProxyHealthInterval  time.Duration `help:"How often to TCP-probe the proxy for the up/down gauge and activity-log transitions." default:"30s" env:"PROXY_HEALTH_INTERVAL"`
}

// Validate enforces the "if ProxyEnabled, we need upstream + miner URL"
// contract. Called at startup so a misconfigured deployment fails fast
// instead of rejecting pool assignments at commit time. When
// ProxyEnabled is false, every other field is ignored — validation
// passes regardless.
func (c Config) Validate() error {
	if !c.ProxyEnabled {
		return nil
	}
	if c.ProxyMinerURL == "" {
		return fmt.Errorf("STRATUM_V2_PROXY_MINER_URL is required when STRATUM_V2_PROXY_ENABLED=true")
	}
	if c.ProxyUpstreamURL == "" {
		return fmt.Errorf("STRATUM_V2_PROXY_UPSTREAM_URL is required when STRATUM_V2_PROXY_ENABLED=true")
	}
	return nil
}

// RewriterConfig projects this Config onto the struct the pool rewriter
// consumes. Keeps the rewriter free of the Kong-parsed shape so it can
// be tested with bare values.
func (c Config) RewriterConfig() rewriter.ProxyConfig {
	return rewriter.ProxyConfig{
		ProxyEnabled: c.ProxyEnabled,
		MinerURL:     c.ProxyMinerURL,
	}
}
