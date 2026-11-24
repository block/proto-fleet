package reporting

import (
	"bytes"
	"slices"
	"strconv"
	"testing"
	"time"
)

func TestPublicationAnnualPriorIncidentNarrativesAreExplicitlyRedacted(t *testing.T) {
	p, in := calculationFixture()
	p.Kind, p.End = "annual", p.Start.AddDate(1, 0, 0)
	p.AsOf = p.End
	prior := p.Start.AddDate(-1, 0, 0)
	in.Orders[0].Start, in.Coverage[0].Start = prior, prior
	in.Configuration.Registers = append(in.Configuration.Registers, RegisterCompleteness{Kind: "incidents", Start: prior, End: p.End, SourceRef: "incident-register"})
	makeIncident := func(id string, start, end time.Time) Incident {
		return Incident{ID: id, SiteID: 1, ContainerID: 10, OrderID: "a", Start: start, End: end, AsOf: p.AsOf, Category: "outage", Summary: "summary-" + id, RootCause: "cause-" + id, ExclusionClause: "claim-" + id, SourceRef: "incident-" + id}
	}
	in.Incidents = []Incident{
		makeIncident("prior", prior, prior.Add(time.Hour)),
		makeIncident("no-cause", prior.Add(2*time.Hour), prior.Add(3*time.Hour)),
		makeIncident("current", p.Start.Add(time.Hour), p.Start.Add(2*time.Hour)),
		makeIncident("crossing", p.Start.Add(-time.Hour), p.Start.Add(time.Hour)),
	}
	in.Incidents[1].RootCause = ""
	result := calculate(t, p, in)
	selected, err := SelectPublicEvidence(t.Context(), p, in, ObservationSource{})
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyPublicEvidenceCalculation(t.Context(), p, selected, result); err != nil {
		t.Fatal(err)
	}
	_, _, id, _ := exportFixture()
	members := scopeEvidenceZIP(t, p, in, result, id)
	written, err := BuildWrittenReport(result, id)
	if err != nil {
		t.Fatal(err)
	}
	members["standalone.html"] = written
	rows, columns := scopeEvidenceRows(t, members, "incidents")
	index := func(name string) int {
		t.Helper()
		i := slices.Index(columns, name)
		if i < 0 {
			t.Fatalf("missing column %q", name)
		}
		return i
	}
	summary, cause, claim := index("summary"), index("root_cause"), index("exclusion_clause")
	recorded, redacted := index("root_cause_recorded"), index("narrative_redacted")
	for _, key := range []string{"prior", "no-cause"} {
		row := rows[key]
		if row[summary] != "" || row[cause] != "" || row[claim] != "" || row[redacted] != "true" || row[recorded] != strconv.FormatBool(key == "prior") {
			t.Fatalf("incorrect redaction or cause presence for %s: %v", key, row)
		}
		for name, data := range members {
			for _, secret := range []string{"summary-" + key, "cause-" + key, "claim-" + key} {
				if bytes.Contains(data, []byte(secret)) {
					t.Errorf("prior-only incident narrative %q leaked in %s", secret, name)
				}
			}
		}
	}
	for _, key := range []string{"current", "crossing"} {
		row := rows[key]
		if row[summary] != "summary-"+key || row[cause] != "cause-"+key || row[claim] != "claim-"+key || row[redacted] != "false" || row[recorded] != "true" {
			t.Fatalf("current evidence changed for %s: %v", key, row)
		}
		for _, name := range []string{"inputs/incidents.csv", "interruptions.csv", "standalone.html"} {
			for _, narrative := range []string{"summary-" + key, "cause-" + key, "claim-" + key, "incident-" + key} {
				if !bytes.Contains(members[name], []byte(narrative)) {
					t.Errorf("required current explanation %q missing from %s", narrative, name)
				}
			}
		}
	}
	if !bytes.Contains(members["manifest.json"], []byte("Prior-only annual incident narratives")) {
		t.Fatal("manifest must disclose the narrative omission")
	}
	if selected.Incidents[0].Summary != in.Incidents[0].Summary || selected.Incidents[0].RootCause != in.Incidents[0].RootCause {
		t.Fatal("output projection changed calculation inputs")
	}
}
