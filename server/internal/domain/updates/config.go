// Package updates polls GitHub releases and caches the newest stable and
// release-candidate versions for update notifications.
package updates

import (
	"net/url"
	"time"

	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
)

// Config contains configuration for the release update checker.
type Config struct {
	CheckInterval   time.Duration `help:"Interval between GitHub release checks" default:"24h" env:"CHECK_INTERVAL"`
	ReleasesAPIURL  string        `help:"GitHub API base URL of the repository releases are fetched from" default:"https://api.github.com/repos/block/proto-fleet" env:"RELEASES_API_URL"`
	DownloadBaseURL string        `help:"Base URL release artifacts are downloaded from" default:"https://github.com/block/proto-fleet/releases/download" env:"DOWNLOAD_BASE_URL"`
	Disabled        bool          `help:"Disable release update checks" env:"DISABLED"`
}

// Validate validates the configuration. Both URLs end up inside a copy-paste
// upgrade command shown to operators, so an http:// base would hand a network
// MITM a rewritable shell command; only https:// is accepted, and a violation
// should fail startup.
func (c *Config) Validate() error {
	for _, field := range []struct {
		name, value string
	}{
		{"ReleasesAPIURL", c.ReleasesAPIURL},
		{"DownloadBaseURL", c.DownloadBaseURL},
	} {
		u, err := url.Parse(field.value)
		if err != nil {
			return fleeterror.NewInvalidArgumentErrorf("%s %q is not a valid URL: %v", field.name, field.value, err)
		}
		if u.Scheme != "https" || u.Host == "" {
			return fleeterror.NewInvalidArgumentErrorf("%s %q must be an https:// URL", field.name, field.value)
		}
	}
	return nil
}
