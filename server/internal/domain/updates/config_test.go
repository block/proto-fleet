package updates

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func validConfig() Config {
	return Config{
		CheckInterval:   time.Hour,
		DownloadBaseURL: downloadBaseURL,
	}
}

// The download base feeds a copy-paste command, so only the canonical release
// path may pass validation.
func TestConfigValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		mutate     func(*Config)
		wantErr    string
		wantAbsent string
	}{
		{
			name:   "defaults pass",
			mutate: func(*Config) {},
		},
		{
			name:    "tiny polling interval rejected",
			mutate:  func(c *Config) { c.CheckInterval = time.Nanosecond },
			wantErr: "CheckInterval",
		},
		{
			name:    "negative polling interval rejected",
			mutate:  func(c *Config) { c.CheckInterval = -time.Second },
			wantErr: "CheckInterval",
		},
		{
			name:   "zero polling interval uses runtime default",
			mutate: func(c *Config) { c.CheckInterval = 0 },
		},
		{
			name:   "minimum polling interval accepted",
			mutate: func(c *Config) { c.CheckInterval = minimumCheckInterval },
		},
		{
			name: "http download base URL rejected",
			mutate: func(c *Config) {
				c.DownloadBaseURL = "http://github.com/block/proto-fleet/releases/download?token=super-secret"
			},
			wantErr:    "DownloadBaseURL",
			wantAbsent: "super-secret",
		},
		{
			name:    "scheme-less download base URL rejected",
			mutate:  func(c *Config) { c.DownloadBaseURL = "github.com/block/proto-fleet/releases/download" },
			wantErr: "DownloadBaseURL",
		},
		{
			name:    "alternate https download host rejected",
			mutate:  func(c *Config) { c.DownloadBaseURL = "https://example.com/releases/download" },
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
			if tt.wantAbsent != "" {
				require.NotContains(t, err.Error(), tt.wantAbsent)
			}
		})
	}
}

func TestConfigValidateRejectsShellSyntaxInDownloadBaseURL(t *testing.T) {
	t.Parallel()

	for _, value := range []string{
		"https://github.com/block/proto-fleet/releases/download/$(touch /tmp/pwned)",
		"https://github.com/block/proto-fleet/releases/download/`touch /tmp/pwned`",
		`https://github.com/block/proto-fleet/releases/download/"quoted"`,
		`https://github.com/block/proto-fleet/releases/download/\backslash`,
		"https://github.com/block/proto-fleet/releases/download/with space",
	} {
		t.Run(value, func(t *testing.T) {
			t.Parallel()

			cfg := validConfig()
			cfg.DownloadBaseURL = value
			err := cfg.Validate()
			require.Error(t, err)
			require.Contains(t, err.Error(), "DownloadBaseURL")
		})
	}
}
