package sites

import "testing"

func TestInferTimezone_USStates(t *testing.T) {
	cases := []struct {
		state string
		want  string
	}{
		{"CA", "America/Los_Angeles"},
		{"NY", "America/New_York"},
		{"TX", "America/Chicago"},
		{"AZ", "America/Phoenix"},
		{"HI", "Pacific/Honolulu"},
		{"AK", "America/Anchorage"},
		{"IN", "America/Indiana/Indianapolis"},
	}
	for _, c := range cases {
		if got := InferTimezone("US", c.state); got != c.want {
			t.Errorf("InferTimezone(\"US\", %q) = %q, want %q", c.state, got, c.want)
		}
	}
}

func TestInferTimezone_NormalizesWhitespaceAndCase(t *testing.T) {
	cases := []struct {
		country, state string
	}{
		{"us", "ca"},
		{"  US ", "  Ca  "},
		{"US", "cA"},
	}
	const want = "America/Los_Angeles"
	for _, c := range cases {
		if got := InferTimezone(c.country, c.state); got != want {
			t.Errorf("InferTimezone(%q, %q) = %q, want %q", c.country, c.state, got, want)
		}
	}
}

func TestInferTimezone_EmptyCountryDefaultsToUS(t *testing.T) {
	if got := InferTimezone("", "CA"); got != "America/Los_Angeles" {
		t.Errorf("empty country should default to US: got %q", got)
	}
	if got := InferTimezone("   ", "CA"); got != "America/Los_Angeles" {
		t.Errorf("whitespace country should default to US: got %q", got)
	}
}

func TestInferTimezone_NonUSCountryReturnsEmpty(t *testing.T) {
	cases := []struct{ country, state string }{
		{"CA", "ON"}, // Canada / Ontario — out of scope today
		{"MX", "DF"},
		{"GB", ""},
	}
	for _, c := range cases {
		if got := InferTimezone(c.country, c.state); got != "" {
			t.Errorf("InferTimezone(%q, %q) = %q, want empty", c.country, c.state, got)
		}
	}
}

func TestInferTimezone_UnknownStateReturnsEmpty(t *testing.T) {
	cases := []string{"", "ZZ", "XX", "123"}
	for _, s := range cases {
		if got := InferTimezone("US", s); got != "" {
			t.Errorf("InferTimezone(\"US\", %q) = %q, want empty", s, got)
		}
	}
}

func TestInferTimezone_CoversAllFiftyStatesPlusDC(t *testing.T) {
	// Guard against accidental deletions from the lookup table. If a
	// future change drops a state, this test fails noisily instead of
	// silently regressing every site in that state to empty timezone.
	if got, want := len(usStateToTimezone), 51; got != want {
		t.Errorf("usStateToTimezone has %d entries, want %d (50 states + DC)", got, want)
	}
}
