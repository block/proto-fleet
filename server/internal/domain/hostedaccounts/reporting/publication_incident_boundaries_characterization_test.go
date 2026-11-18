package reporting

import (
	"bytes"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Incident prose requires an observed interval inside the issued period.
func TestPublicationOmitsStaleIncidentNarrative(t *testing.T) {
	for _, tc := range []struct {
		name       string
		end        func(Period) time.Time
		asOf       func(Period) time.Time
		wantDetail bool
	}{
		{"closed crossing, observed prior", func(p Period) time.Time { return p.Start.Add(time.Hour) }, func(p Period) time.Time { return p.Start.Add(-time.Hour) }, false},
		{"closed crossing, observed at boundary", func(p Period) time.Time { return p.Start.Add(time.Hour) }, func(p Period) time.Time { return p.Start }, false},
		{"stale open", func(Period) time.Time { return time.Time{} }, func(p Period) time.Time { return p.Start.Add(-time.Hour) }, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, in := calculationFixture()
			p.Kind, p.End = "annual", p.Start.AddDate(1, 0, 0)
			p.AsOf = p.End
			prior := p.Start.AddDate(-1, 0, 0)
			in.Orders[0].Start, in.Coverage[0].Start = prior, prior
			in.Configuration.Registers = append(in.Configuration.Registers, RegisterCompleteness{Kind: "incidents", Start: prior, End: p.End, SourceRef: "incident-register"})
			in.Incidents = []Incident{{ID: "stale", SiteID: 1, ContainerID: 10, OrderID: "a", Start: p.Start.Add(-2 * time.Hour), End: tc.end(p), AsOf: tc.asOf(p), Category: "outage", Summary: "UNNEEDED-STALE-SUMMARY", RootCause: "UNNEEDED-STALE-CAUSE", ExclusionClause: "UNNEEDED-STALE-CLAIM", SourceRef: "incident-ref"}}
			result := calculate(t, p, in)
			require.Equal(t, tc.wantDetail, len(result.Interruptions) != 0)
			if tc.wantDetail {
				require.Contains(t, result.Metrics["maintenance_hours"].Causes, "incident_observation_gap")
			}
			other := in
			other.Incidents = slices.Clone(in.Incidents)
			other.Incidents[0].Summary = "different summary"
			other.Incidents[0].RootCause = "different nonempty cause"
			other.Incidents[0].ExclusionClause = "different claim"
			changed := calculate(t, p, other)
			require.True(t, reflect.DeepEqual(result.Metrics, changed.Metrics))
			require.True(t, reflect.DeepEqual(result.Containers, changed.Containers))
			require.True(t, reflect.DeepEqual(result.Orders, changed.Orders))
			selected, err := SelectPublicEvidence(t.Context(), p, in, ObservationSource{})
			require.NoError(t, err)
			require.NoError(t, VerifyPublicEvidenceCalculation(t.Context(), p, selected, result))
			_, _, identity, _ := exportFixture()
			members := scopeEvidenceZIP(t, p, in, result, identity)
			written, err := BuildWrittenReport(result, identity)
			require.NoError(t, err)
			members["standalone.html"] = written
			for _, secret := range []string{"UNNEEDED-STALE-SUMMARY", "UNNEEDED-STALE-CAUSE", "UNNEEDED-STALE-CLAIM"} {
				for member, data := range members {
					require.False(t, bytes.Contains(data, []byte(secret)), "%s leaks %s", member, secret)
				}
				if tc.wantDetail {
					for _, member := range []string{"interruptions.csv", "standalone.html"} {
						require.False(t, bytes.Contains(members[member], []byte(secret)), "%s leaks %s", member, secret)
					}
				}
			}
		})
	}
}
