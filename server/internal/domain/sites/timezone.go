package sites

import "strings"

// usStateToTimezone maps USPS two-letter codes to the IANA timezone
// the bulk of the state observes. Multi-zone states resolve to the
// most populous zone; refine here when a counter-example matters.
var usStateToTimezone = map[string]string{
	"AL": "America/Chicago",
	"AK": "America/Anchorage",
	"AZ": "America/Phoenix",
	"AR": "America/Chicago",
	"CA": "America/Los_Angeles",
	"CO": "America/Denver",
	"CT": "America/New_York",
	"DE": "America/New_York",
	"DC": "America/New_York",
	"FL": "America/New_York",
	"GA": "America/New_York",
	"HI": "Pacific/Honolulu",
	"ID": "America/Boise",
	"IL": "America/Chicago",
	"IN": "America/Indiana/Indianapolis",
	"IA": "America/Chicago",
	"KS": "America/Chicago",
	"KY": "America/New_York",
	"LA": "America/Chicago",
	"ME": "America/New_York",
	"MD": "America/New_York",
	"MA": "America/New_York",
	"MI": "America/Detroit",
	"MN": "America/Chicago",
	"MS": "America/Chicago",
	"MO": "America/Chicago",
	"MT": "America/Denver",
	"NE": "America/Chicago",
	"NV": "America/Los_Angeles",
	"NH": "America/New_York",
	"NJ": "America/New_York",
	"NM": "America/Denver",
	"NY": "America/New_York",
	"NC": "America/New_York",
	"ND": "America/Chicago",
	"OH": "America/New_York",
	"OK": "America/Chicago",
	"OR": "America/Los_Angeles",
	"PA": "America/New_York",
	"RI": "America/New_York",
	"SC": "America/New_York",
	"SD": "America/Chicago",
	"TN": "America/Chicago",
	"TX": "America/Chicago",
	"UT": "America/Denver",
	"VT": "America/New_York",
	"VA": "America/New_York",
	"WA": "America/Los_Angeles",
	"WV": "America/New_York",
	"WI": "America/Chicago",
	"WY": "America/Denver",
}

// caProvinceToTimezone maps Canadian province / territory two-letter
// codes to the IANA timezone the bulk of the province observes.
// Multi-zone provinces (NT, NU, BC's far east) resolve to the most
// populous zone.
var caProvinceToTimezone = map[string]string{
	"AB": "America/Edmonton",
	"BC": "America/Vancouver",
	"MB": "America/Winnipeg",
	"NB": "America/Moncton",
	"NL": "America/St_Johns",
	"NS": "America/Halifax",
	"NT": "America/Yellowknife",
	"NU": "America/Iqaluit",
	"ON": "America/Toronto",
	"PE": "America/Halifax",
	"QC": "America/Toronto",
	"SK": "America/Regina",
	"YT": "America/Whitehorse",
}

// InferTimezone returns an IANA timezone id derived from a (country,
// state) pair. Empty country defaults to "US" (the DB column default).
// Returns "" when nothing matches — caller decides how to render that.
//
// Timezone is intentionally not stored on the site row. Computing on
// read keeps the mapping single-sourced in code; updating an entry
// here immediately affects every site without a backfill.
func InferTimezone(country, state string) string {
	c := strings.ToUpper(strings.TrimSpace(country))
	if c == "" {
		c = "US"
	}
	s := strings.ToUpper(strings.TrimSpace(state))
	switch c {
	case "US":
		return usStateToTimezone[s]
	case "CA":
		return caProvinceToTimezone[s]
	default:
		return ""
	}
}
