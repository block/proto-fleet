// Package updates polls GitHub releases and caches the newest stable and
// release-candidate versions for update notifications.
package updates

import (
	"time"

	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
)

const (
	downloadBaseURL      = "https://github.com/block/proto-fleet/releases/download"
	minimumCheckInterval = 5 * time.Minute
)

// Config contains configuration for the release update checker.
type Config struct {
	CheckInterval   time.Duration `help:"Maximum interval between GitHub release checks" default:"1h" env:"CHECK_INTERVAL"`
	DownloadBaseURL string        `help:"Allowlisted base URL release artifacts are downloaded from" default:"https://github.com/block/proto-fleet/releases/download" env:"DOWNLOAD_BASE_URL"`
	Enabled         bool          `help:"Enable release update checks" default:"true" env:"ENABLED"`
	GitHubToken     string        `help:"Optional GitHub token for authenticated, conditional release checks" default:"" env:"GITHUB_TOKEN"`
}

// Validate validates the configuration. DownloadBaseURL ends up in a
// copy-paste shell command, so it must exactly match the trusted release path.
// The error deliberately omits the configured value because deployment
// configuration can contain sensitive data.
func (c *Config) Validate() error {
	if c.CheckInterval != 0 && c.CheckInterval < minimumCheckInterval {
		return fleeterror.NewInvalidArgumentErrorf("CheckInterval must be zero or at least 5m")
	}
	if c.DownloadBaseURL != downloadBaseURL {
		return fleeterror.NewInvalidArgumentErrorf("DownloadBaseURL must match the allowlisted Proto Fleet release URL")
	}
	return nil
}
