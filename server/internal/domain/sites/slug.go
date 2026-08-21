package sites

import (
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

const maxSiteSlugLength = 63

var numericSiteSlugRE = regexp.MustCompile(`^[1-9][0-9]*$`)

// ReservedSiteSlugSegments are root route segments the Fleet client owns.
// They cannot be used as site slugs because scoped routes are first-segment
// paths: /{siteSlug}/dashboard, /{siteSlug}/fleet, etc.
var ReservedSiteSlugSegments = map[string]string{
	"dashboard":       "scopable Dashboard root",
	"fleet":           "scopable Fleet root",
	"groups":          "scopable Groups root",
	"energy":          "scopable Energy root",
	"activity":        "scopable Activity root",
	"settings":        "Settings root",
	"auth":            "Auth root",
	"welcome":         "First-run welcome root",
	"onboarding":      "Onboarding root",
	"miners":          "Miner detail and legacy redirect root",
	"racks":           "Rack detail and legacy redirect root",
	"buildings":       "Building detail root",
	"sites":           "Site detail and legacy redirect root",
	"fleet-down":      "Fleet-down route root",
	"unassigned":      "Reserved non-site scope",
	"update-password": "Password reset root",
}

func generateSiteSlug(name string, used map[string]struct{}) string {
	base := slugifySiteName(name)
	for suffix := 0; ; suffix++ {
		candidate := siteSlugCandidate(base, suffix)
		if _, ok := used[candidate]; !ok {
			return candidate
		}
	}
}

// GenerateSiteSlug returns the first slug candidate for name that is not in
// usedSlugs. It is exported so lower-level stores can protect direct inserts
// from persisting blank slugs when test fixtures bypass the service layer.
func GenerateSiteSlug(name string, usedSlugs []string) string {
	used := make(map[string]struct{}, len(usedSlugs))
	for _, slug := range usedSlugs {
		used[slug] = struct{}{}
	}
	return generateSiteSlug(name, used)
}

func slugifySiteName(name string) string {
	var b strings.Builder
	lastDash := false
	for _, r := range norm.NFKD.String(strings.ToLower(name)) {
		if unicode.Is(unicode.Mn, r) {
			continue
		}
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			if b.Len() < maxSiteSlugLength {
				b.WriteRune(r)
				lastDash = false
			}
			continue
		}
		if b.Len() > 0 && !lastDash && b.Len() < maxSiteSlugLength {
			b.WriteByte('-')
			lastDash = true
		}
	}

	out := strings.Trim(b.String(), "-")
	if out == "" || numericSiteSlugRE.MatchString(out) || isReservedSiteSlug(out) {
		return "site"
	}
	return out
}

func siteSlugCandidate(base string, suffix int) string {
	if suffix == 0 {
		return base
	}
	s := "-" + strconv.Itoa(suffix+1)
	trimmed := strings.TrimRight(base[:min(len(base), maxSiteSlugLength-len(s))], "-")
	if trimmed == "" {
		trimmed = "site"
	}
	return trimmed + s
}

func isReservedSiteSlug(slug string) bool {
	_, ok := ReservedSiteSlugSegments[slug]
	return ok
}
