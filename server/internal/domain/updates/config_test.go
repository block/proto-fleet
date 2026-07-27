package updates

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func validConfig() Config {
	return Config{
		CheckInterval:   24 * time.Hour,
		ReleasesAPIURL:  "https://api.github.com/repos/block/proto-fleet",
		DownloadBaseURL: "https://github.com/block/proto-fleet/releases/download",
	}
}

// Scenario 6: both URLs feed a copy-paste shell command, so only https:// may
// pass validation; http:// (or garbage) must fail startup.
func TestConfigValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		mutate  func(*Config)
		wantErr string
	}{
		{
			name:   "defaults pass",
			mutate: func(*Config) {},
		},
		{
			name:    "http releases API URL rejected",
			mutate:  func(c *Config) { c.ReleasesAPIURL = "http://api.github.com/repos/block/proto-fleet" },
			wantErr: "ReleasesAPIURL",
		},
		{
			name:    "http download base URL rejected",
			mutate:  func(c *Config) { c.DownloadBaseURL = "http://github.com/block/proto-fleet/releases/download" },
			wantErr: "DownloadBaseURL",
		},
		{
			name:    "empty releases API URL rejected",
			mutate:  func(c *Config) { c.ReleasesAPIURL = "" },
			wantErr: "ReleasesAPIURL",
		},
		{
			name:    "scheme-less download base URL rejected",
			mutate:  func(c *Config) { c.DownloadBaseURL = "github.com/block/proto-fleet/releases/download" },
			wantErr: "DownloadBaseURL",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg := validConfig()
			tt.mutate(&cfg)
			err := cfg.Validate()
			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			require.Contains(t, err.Error(), tt.wantErr)
		})
	}
}
