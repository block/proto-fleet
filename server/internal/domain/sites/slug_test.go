package sites

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestSlugifySiteName(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "spaces and punctuation collapse", in: "North DC #1", want: "north-dc-1"},
		{name: "accents are stripped", in: "São Paulo", want: "sao-paulo"},
		{name: "empty falls back", in: "   !!!   ", want: "site"},
		{name: "numeric falls back", in: "123", want: "site"},
		{name: "reserved route falls back", in: "Dashboard", want: "site"},
		{name: "trims trailing dash after length cap", in: strings.Repeat("a", 62) + " !", want: strings.Repeat("a", 62)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := slugifySiteName(tt.in); got != tt.want {
				t.Fatalf("slugifySiteName(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestGenerateSiteSlugCollisions(t *testing.T) {
	used := map[string]struct{}{
		"north-dc":   {},
		"north-dc-2": {},
	}

	if got := generateSiteSlug("North DC", used); got != "north-dc-3" {
		t.Fatalf("generateSiteSlug collision = %q, want north-dc-3", got)
	}
}

func TestReservedSiteSlugSegmentsFallback(t *testing.T) {
	for slug := range ReservedSiteSlugSegments {
		if got := slugifySiteName(slug); got != "site" {
			t.Fatalf("reserved segment %q slugified to %q, want site", slug, got)
		}
	}
}

func TestReservedSiteSlugSegmentsContainClientScopableRoots(t *testing.T) {
	path := filepath.Join("..", "..", "..", "..", "client", "src", "protoFleet", "routing", "siteScope.tsx")
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read client site scope file: %v", err)
	}
	re := regexp.MustCompile(`SCOPABLE_ROOT_SEGMENTS\s*=\s*new Set\(\[([^\]]+)\]\)`)
	match := re.FindStringSubmatch(string(content))
	if len(match) != 2 {
		t.Fatal("could not find SCOPABLE_ROOT_SEGMENTS in client siteScope.tsx")
	}
	segmentRE := regexp.MustCompile(`"([^"]+)"`)
	for _, m := range segmentRE.FindAllStringSubmatch(match[1], -1) {
		segment := m[1]
		if _, ok := ReservedSiteSlugSegments[segment]; !ok {
			t.Fatalf("client scopable root %q is missing from server reserved site slug segments", segment)
		}
	}
}
