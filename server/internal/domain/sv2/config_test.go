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
			cfg:     Config{ProxyEnabled: true, ProxyUpstreamURL: "stratum2+tcp://pool:34254"},
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
		ProxyEnabled:        true,
		ProxyMinerURL:       "stratum+tcp://lan:34255",
		ProxyUpstreamURL:    "stratum2+tcp://pool:34254",
		ProxyHealthInterval: 30 * time.Second,
	}
	require.NoError(t, c.Validate())
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
