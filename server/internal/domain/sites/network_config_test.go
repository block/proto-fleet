package sites

import (
	"strings"
	"testing"
)

func TestCanonicalizeNetworkConfig_emptyAndWhitespace(t *testing.T) {
	cases := []string{
		"",
		"   ",
		"\n\n\n",
	}
	for _, in := range cases {
		got, err := CanonicalizeNetworkConfig(in)
		if err != nil {
			t.Fatalf("expected nil error for input %q, got %v", in, err)
		}
		if got.Canonical != "" {
			t.Fatalf("expected empty canonical for whitespace, got %q", got.Canonical)
		}
		if len(got.Prefixes) != 0 {
			t.Fatalf("expected zero prefixes for whitespace, got %v", got.Prefixes)
		}
	}
}

func TestCanonicalizeNetworkConfig_validCIDRsAreCanonicalized(t *testing.T) {
	got, err := CanonicalizeNetworkConfig("10.0.0.0/24\n  192.168.1.0/24  \n10.0.1.0/24")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"10.0.0.0/24", "10.0.1.0/24", "192.168.1.0/24"}
	for _, w := range want {
		if !strings.Contains(got.Canonical, w) {
			t.Fatalf("canonical %q missing %q", got.Canonical, w)
		}
	}
	if len(got.Prefixes) != 3 {
		t.Fatalf("expected 3 prefixes, got %d", len(got.Prefixes))
	}
}

func TestCanonicalizeNetworkConfig_bareIPBecomesHostPrefix(t *testing.T) {
	got, err := CanonicalizeNetworkConfig("10.0.0.5")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Canonical != "10.0.0.5/32" {
		t.Fatalf("want 10.0.0.5/32, got %q", got.Canonical)
	}
}

func TestCanonicalizeNetworkConfig_invalidEntryRejected(t *testing.T) {
	if _, err := CanonicalizeNetworkConfig("not-an-ip"); err == nil {
		t.Fatal("expected error, got nil")
	}
	if _, err := CanonicalizeNetworkConfig("10.0.0.0/33"); err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestCanonicalizeNetworkConfig_broaderThanCapRejected(t *testing.T) {
	// /16 is broader than /20 cap.
	if _, err := CanonicalizeNetworkConfig("10.0.0.0/16"); err == nil {
		t.Fatal("expected /16 to be rejected, got nil")
	}
	// Exactly /20 is allowed.
	if _, err := CanonicalizeNetworkConfig("10.0.0.0/20"); err != nil {
		t.Fatalf("/20 should be allowed, got %v", err)
	}
	// Narrower (/24) is allowed.
	if _, err := CanonicalizeNetworkConfig("10.0.0.0/24"); err != nil {
		t.Fatalf("/24 should be allowed, got %v", err)
	}
}

func TestCanonicalizeNetworkConfig_withinSiteOverlapRejected(t *testing.T) {
	// /24 nested inside /22 (still within /20 cap).
	if _, err := CanonicalizeNetworkConfig("10.0.0.0/22\n10.0.1.0/24"); err == nil {
		t.Fatal("expected overlap to be rejected, got nil")
	}
	// Exact duplicates also overlap.
	if _, err := CanonicalizeNetworkConfig("10.0.0.0/24\n10.0.0.0/24"); err == nil {
		t.Fatal("expected duplicate to be rejected, got nil")
	}
}

func TestCanonicalizeNetworkConfig_canonicalRoundTrip(t *testing.T) {
	// Save → load should produce the same canonical text.
	first, err := CanonicalizeNetworkConfig("192.168.1.0/24\n10.0.0.0/24")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	second, err := CanonicalizeNetworkConfig(first.Canonical)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if first.Canonical != second.Canonical {
		t.Fatalf("canonical not stable: %q vs %q", first.Canonical, second.Canonical)
	}
}

func TestCrossSiteOverlapWarnings_overlapsProduceWarnings(t *testing.T) {
	subj, err := CanonicalizeNetworkConfig("10.0.0.0/24")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	other, err := CanonicalizeNetworkConfig("10.0.0.0/22")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	warnings := CrossSiteOverlapWarnings(subj.Prefixes, other.Prefixes, "siteB")
	if len(warnings) == 0 {
		t.Fatal("expected at least one warning for overlapping prefixes")
	}
	if !strings.Contains(warnings[0], "siteB") {
		t.Fatalf("warning %q does not name the other site", warnings[0])
	}
}

func TestCrossSiteOverlapWarnings_disjointProducesNone(t *testing.T) {
	subj, err := CanonicalizeNetworkConfig("10.0.0.0/24")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	other, err := CanonicalizeNetworkConfig("192.168.1.0/24")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	warnings := CrossSiteOverlapWarnings(subj.Prefixes, other.Prefixes, "siteB")
	if len(warnings) != 0 {
		t.Fatalf("expected zero warnings for disjoint prefixes, got %v", warnings)
	}
}
