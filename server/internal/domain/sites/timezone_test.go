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

func TestInferTimezone_CAProvinces(t *testing.T) {
	cases := []struct {
		province string
		want     string
	}{
		{"ON", "America/Toronto"},
		{"QC", "America/Toronto"},
		{"BC", "America/Vancouver"},
		{"AB", "America/Edmonton"},
		{"SK", "America/Regina"},
		{"NL", "America/St_Johns"},
		{"YT", "America/Whitehorse"},
		{"NU", "America/Iqaluit"},
		{"PE", "America/Halifax"},
	}
	for _, c := range cases {
		if got := InferTimezone("CA", c.province); got != c.want {
			t.Errorf("InferTimezone(\"CA\", %q) = %q, want %q", c.province, got, c.want)
		}
	}
}

func TestInferTimezone_NormalizesWhitespaceAndCase(t *testing.T) {
	cases := []struct {
		country, state, want string
	}{
		{"us", "ca", "America/Los_Angeles"},
		{"  US ", "  Ca  ", "America/Los_Angeles"},
		{"US", "cA", "America/Los_Angeles"},
		{"ca", "on", "America/Toronto"},
		{"  Ca  ", " ON ", "America/Toronto"},
	}
	for _, c := range cases {
		if got := InferTimezone(c.country, c.state); got != c.want {
			t.Errorf("InferTimezone(%q, %q) = %q, want %q", c.country, c.state, got, c.want)
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

func TestInferTimezone_UnsupportedCountryReturnsEmpty(t *testing.T) {
	cases := []struct{ country, state string }{
		{"MX", "DF"},
		{"GB", ""},
		{"DE", "BY"},
	}
	for _, c := range cases {
		if got := InferTimezone(c.country, c.state); got != "" {
			t.Errorf("InferTimezone(%q, %q) = %q, want empty", c.country, c.state, got)
		}
	}
}

func TestInferTimezone_UnknownStateReturnsEmpty(t *testing.T) {
	cases := []struct{ country, state string }{
		{"US", ""},
		{"US", "ZZ"},
		{"US", "XX"},
		{"US", "123"},
		{"CA", ""},
		{"CA", "ZZ"},
		{"CA", "ON1"},
	}
	for _, c := range cases {
		if got := InferTimezone(c.country, c.state); got != "" {
			t.Errorf("InferTimezone(%q, %q) = %q, want empty", c.country, c.state, got)
		}
	}
}

func TestInferTimezone_CoversAllFiftyStatesPlusDC(t *testing.T) {
	if got, want := len(usStateToTimezone), 51; got != want {
		t.Errorf("usStateToTimezone has %d entries, want %d (50 states + DC)", got, want)
	}
}

func TestInferTimezone_CoversAllCanadianProvincesAndTerritories(t *testing.T) {
	if got, want := len(caProvinceToTimezone), 13; got != want {
		t.Errorf("caProvinceToTimezone has %d entries, want %d (10 provinces + 3 territories)", got, want)
	}
}
