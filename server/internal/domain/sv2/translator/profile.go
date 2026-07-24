package translator

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/block/proto-fleet/server/internal/domain/sv2"
)

type Endpoint string

func (e Endpoint) String() string {
	return string(e)
}

type Upstream struct {
	URL      string `json:"url"`
	Username string `json:"username"`
}

type Profile struct {
	Upstreams []Upstream `json:"upstreams"`
}

func NormalizeProfile(profile Profile) (Profile, error) {
	if len(profile.Upstreams) == 0 {
		return Profile{}, fmt.Errorf("translator profile has no upstreams")
	}

	normalized := Profile{Upstreams: make([]Upstream, 0, len(profile.Upstreams))}
	for _, upstream := range profile.Upstreams {
		upstream.URL = strings.TrimSpace(upstream.URL)
		upstream.Username = strings.TrimSpace(upstream.Username)
		if err := sv2.ValidatePoolURL(upstream.URL); err != nil {
			return Profile{}, err
		}
		parsed, err := url.Parse(upstream.URL)
		if err != nil {
			return Profile{}, fmt.Errorf("parse translator upstream: %w", err)
		}
		if parsed.Hostname() == "" || parsed.Port() == "" {
			return Profile{}, fmt.Errorf("translator upstream %q requires a host and port", upstream.URL)
		}
		normalized.Upstreams = append(normalized.Upstreams, upstream)
	}
	return normalized, nil
}

func ProfilesEqual(left, right Profile) bool {
	if len(left.Upstreams) != len(right.Upstreams) {
		return false
	}
	for i := range left.Upstreams {
		if left.Upstreams[i] != right.Upstreams[i] {
			return false
		}
	}
	return true
}
