package sv2

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfig_ValidateDisabledIsNoop(t *testing.T) {
	c := Config{ProxyEnabled: false}
	require.NoError(t, c.Validate())

	c = Config{ProxyEnabled: false, ProxyMinerURL: "", ProxyUpstreamURL: ""}
	require.NoError(t, c.Validate(), "disabled proxy tolerates empty fields")
}

func TestConfig_ValidateEnabledRequiresBothURLs(t *testing.T) {
	cases := []struct {
		name    string
		cfg     Config
		wantErr string
	}{
		{
			name:    "missing miner url",
			cfg:     Config{ProxyEnabled: true, ProxyUpstreamURL: "stratum2+tcp://pool:34254/UpstreamPubKey"},
			wantErr: "STRATUM_V2_PROXY_MINER_URL",
		},
		{
			name:    "missing upstream url",
			cfg:     Config{ProxyEnabled: true, ProxyMinerURL: "stratum+tcp://lan:34255"},
			wantErr: "STRATUM_V2_PROXY_UPSTREAM_URL",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.cfg.Validate()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantErr)
		})
	}
}

func TestConfig_ValidateEnabledWithBothURLsIsOK(t *testing.T) {
	c := Config{
		ProxyEnabled:         true,
		ProxyMinerURL:        "stratum+tcp://lan:34255",
		ProxyUpstreamURL:     "stratum2+tcp://pool:34254/UpstreamPubKey",
		ProxyHealthCheckAddr: "127.0.0.1:34255",
		ProxyHealthInterval:  30 * time.Second,
	}
	require.NoError(t, c.Validate())
}

func TestConfig_ValidateRejectsBadHealthSettings(t *testing.T) {
	base := Config{
		ProxyEnabled:         true,
		ProxyMinerURL:        "stratum+tcp://lan:34255",
		ProxyUpstreamURL:     "stratum2+tcp://pool:34254/UpstreamPubKey",
		ProxyHealthCheckAddr: "127.0.0.1:34255",
		ProxyHealthInterval:  30 * time.Second,
	}
	cases := []struct {
		name    string
		mutate  func(c *Config)
		wantErr string
	}{
		{
			name:    "non-positive interval",
			mutate:  func(c *Config) { c.ProxyHealthInterval = 0 },
			wantErr: "STRATUM_V2_PROXY_HEALTH_INTERVAL",
		},
		{
			name:    "negative interval",
			mutate:  func(c *Config) { c.ProxyHealthInterval = -time.Second },
			wantErr: "STRATUM_V2_PROXY_HEALTH_INTERVAL",
		},
		{
			name:    "missing health addr",
			mutate:  func(c *Config) { c.ProxyHealthCheckAddr = "" },
			wantErr: "STRATUM_V2_PROXY_HEALTH_ADDR",
		},
		{
			name:    "malformed health addr",
			mutate:  func(c *Config) { c.ProxyHealthCheckAddr = "not-a-host-port" },
			wantErr: "STRATUM_V2_PROXY_HEALTH_ADDR",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := base
			tc.mutate(&c)
			err := c.Validate()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantErr)
		})
	}
}

func TestConfig_ValidateRejectsURLsWithoutPort(t *testing.T) {
	cases := []struct {
		name string
		cfg  Config
	}{
		{
			name: "miner url missing port",
			cfg: Config{
				ProxyEnabled:     true,
				ProxyMinerURL:    "stratum+tcp://lan",
				ProxyUpstreamURL: "stratum2+tcp://pool:34254/UpstreamPubKey",
			},
		},
		{
			name: "upstream url missing port",
			cfg: Config{
				ProxyEnabled:     true,
				ProxyMinerURL:    "stratum+tcp://lan:34255",
				ProxyUpstreamURL: "stratum2+tcp://pool",
			},
		},
		{
			name: "upstream url with trailing slash but no port",
			cfg: Config{
				ProxyEnabled:     true,
				ProxyMinerURL:    "stratum+tcp://lan:34255",
				ProxyUpstreamURL: "stratum2+tcp://pool/PUBKEY",
			},
		},
		{
			name: "upstream url missing authority pubkey suffix",
			cfg: Config{
				ProxyEnabled:     true,
				ProxyMinerURL:    "stratum+tcp://lan:34255",
				ProxyUpstreamURL: "stratum2+tcp://pool:34254",
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.cfg.Validate()
			require.Error(t, err)
		})
	}
}

func TestConfig_ValidateRejectsUnsupportedSchemes(t *testing.T) {
	cases := []struct {
		name    string
		cfg     Config
		wantErr string
	}{
		{
			name: "miner url ssl rejected",
			cfg: Config{
				ProxyEnabled:     true,
				ProxyMinerURL:    "stratum+ssl://lan:34255",
				ProxyUpstreamURL: "stratum2+tcp://pool:34254/UpstreamPubKey",
			},
			wantErr: "STRATUM_V2_PROXY_MINER_URL",
		},
		{
			name: "miner url ws rejected",
			cfg: Config{
				ProxyEnabled:     true,
				ProxyMinerURL:    "stratum+ws://lan:34255",
				ProxyUpstreamURL: "stratum2+tcp://pool:34254/UpstreamPubKey",
			},
			wantErr: "STRATUM_V2_PROXY_MINER_URL",
		},
		{
			name: "upstream url ssl rejected",
			cfg: Config{
				ProxyEnabled:     true,
				ProxyMinerURL:    "stratum+tcp://lan:34255",
				ProxyUpstreamURL: "stratum2+ssl://pool:34254",
			},
			wantErr: "STRATUM_V2_PROXY_UPSTREAM_URL",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.cfg.Validate()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantErr)
		})
	}
}

func TestConfig_RewriterConfig(t *testing.T) {
	c := Config{
		ProxyEnabled:  true,
		ProxyMinerURL: "stratum+tcp://lan:34255",
	}
	rc := c.RewriterConfig()
	assert.True(t, rc.ProxyEnabled)
	assert.Equal(t, "stratum+tcp://lan:34255", rc.MinerURL)

	// Disabled proxy projects straight through.
	c.ProxyEnabled = false
	rc = c.RewriterConfig()
	assert.False(t, rc.ProxyEnabled)
}
