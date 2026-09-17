// Package updates polls GitHub releases and caches the newest stable and
// release-candidate versions for update notifications.
package updates

import (
	"path/filepath"
	"time"

	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/releaseinfo"
)

const (
	minimumCheckInterval = 5 * time.Minute
)

// Config contains configuration for the release update checker.
type Config struct {
	CheckInterval time.Duration `help:"Maximum interval between GitHub release checks" default:"1h" env:"CHECK_INTERVAL"`
	// DownloadBaseURL remains only to reject stale or unsafe legacy configuration.
	// Release URLs are always derived from the embedded build identity.
	DownloadBaseURL   string `help:"Deprecated; release URLs are derived from the build identity" hidden:"" env:"DOWNLOAD_BASE_URL"`
	Enabled           bool   `help:"Enable release update checks" default:"true" env:"ENABLED"`
	UpdaterSocketPath string `help:"Unix socket exposed by the optional host updater" default:"/run/proto-fleet-updater/updater.sock" env:"UPDATER_SOCKET_PATH"`
}

// Validate checks the embedded repository and any legacy URL against that source.
// The error deliberately omits the configured value because deployment
// configuration can contain sensitive data.
func (c *Config) Validate() error {
	if c.CheckInterval != 0 && c.CheckInterval < minimumCheckInterval {
		return fleeterror.NewInvalidArgumentErrorf("CheckInterval must be zero or at least 5m")
	}
	repository := releaseinfo.Repository
	if err := releaseinfo.ValidateRepository(repository); err != nil {
		return fleeterror.NewInvalidArgumentErrorf("embedded release repository must be a valid GitHub owner/repo value")
	}
	if c.DownloadBaseURL != "" && c.DownloadBaseURL != releaseinfo.DownloadBaseURL(repository) {
		return fleeterror.NewInvalidArgumentErrorf("DownloadBaseURL must match the embedded release repository")
	}
	c.DownloadBaseURL = releaseinfo.DownloadBaseURL(repository)
	if c.UpdaterSocketPath != "" && !filepath.IsAbs(c.UpdaterSocketPath) {
		return fleeterror.NewInvalidArgumentErrorf("UpdaterSocketPath %q must be absolute", c.UpdaterSocketPath)
	}
	return nil
}
