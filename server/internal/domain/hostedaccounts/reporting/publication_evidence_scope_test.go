package reporting

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"reflect"

	"slices"
	"strings"
	"testing"
	"time"
)

// These tests exercise the public selector and archive writer independently of
// the service's database capture and authorization responsibilities.
func scopeEvidenceZIP(t *testing.T, p Period, in Inputs, result Result, id Identity) map[string][]byte {
	t.Helper()
	selected, err := SelectPublicEvidence(t.Context(), p, in, ObservationSource{})
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := WriteEvidenceContext(t.Context(), &out, result, selected, id, ObservationSource{}); err != nil {
		t.Fatal(err)
	}
	return unzipExport(t, out.Bytes())
}

func scopeEvidenceTable(t *testing.T, members map[string][]byte, name string) ([][]string, []string) {
	t.Helper()
	data, ok := members["inputs/"+name+".csv"]
	if !ok {
		t.Fatalf("missing %s table", name)
	}
	table, err := csv.NewReader(bytes.NewReader(data)).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(table) == 0 {
		t.Fatalf("missing %s header", name)
	}
	return table[1:], table[0]
}

func scopeEvidenceRows(t *testing.T, members map[string][]byte, name string) (map[string][]string, []string) {
	t.Helper()
	data, columns := scopeEvidenceTable(t, members, name)
	id := slices.Index(columns, "id")
	if id < 0 {
		t.Fatalf("%s table has no id column: %v", name, columns)
	}
	rows := make(map[string][]string, len(data))
	for _, row := range data {
		if len(row) != len(columns) {
			t.Fatalf("%s row has wrong length: %v", name, row)
		}
		key := row[id]
		if _, exists := rows[key]; exists {
			t.Fatalf("duplicate %s row %q", name, key)
		}
		rows[key] = row
	}
	return rows, columns
}

func TestPublicationEvidenceScopeRelevantHistoryAndConflicts(t *testing.T) {
	p, in := calculationFixture()
	prior := p.Start.AddDate(0, 0, -90)
	in.Orders[0].Start = prior.AddDate(-1, 0, 0)
	oldCoverage := in.Coverage[0]
	oldCoverage.ID, oldCoverage.Start, oldCoverage.End = "support-before-period", prior, p.Start.Add(-24*time.Hour)
	currentCoverage := in.Coverage[0]
	currentCoverage.ID, currentCoverage.Start = "current-coverage", oldCoverage.End
	unrelatedCoverage := oldCoverage
	unrelatedCoverage.ID, unrelatedCoverage.AssetID, unrelatedCoverage.AllocationIntervalID = "unrelated-coverage", 2, 2
	unrelatedCoverage.Start, unrelatedCoverage.End = prior.AddDate(0, 0, -60), prior.AddDate(0, 0, -30)
	in.Coverage = []CoverageSegment{unrelatedCoverage, oldCoverage, currentCoverage}

	earlier := ObligationInterval{ID: "episode-earlier", EpisodeID: "relevant", Kind: 6, AssetID: 1, SiteID: 1, ContainerID: 10, OrderID: "a", Start: prior, End: p.Start.AddDate(0, 0, -30), AsOf: p.AsOf, ReceivedAt: prior, Qualified: true, SourceRef: "earlier episode"}
	current := earlier
	current.ID, current.Start, current.End, current.SourceRef = "episode-current", p.Start, p.End, "current episode"
	unrelated := earlier
	unrelated.ID, unrelated.EpisodeID, unrelated.Start, unrelated.End = "episode-unrelated", "closed-unrelated", prior.AddDate(0, 0, 10), prior.AddDate(0, 0, 20)
	unrelated.AsOf, unrelated.ClosedAt = unrelated.End, unrelated.End
	in.Obligations = []ObligationInterval{earlier, current, unrelated}

	crossing := HashrateInterval{ID: "crossing-hashrate", Kind: 6, AssetID: 1, SiteID: 1, ContainerID: 10, OrderID: "a", SourceID: "hash", Start: p.Start.AddDate(0, 0, -10), End: p.Start.AddDate(0, 0, 10), HashrateHS: "50000000000000", Complete: true, SourceRef: "crossing hash"}
	pastHash := crossing
	pastHash.ID, pastHash.Start, pastHash.End = "past-hash", prior.AddDate(0, 0, 1), prior.AddDate(0, 0, 2)
	futureHash := crossing
	futureHash.ID, futureHash.Start, futureHash.End = "future-hash", p.End, p.End.Add(time.Hour)
	in.Hashrates = []HashrateInterval{crossing, pastHash, futureHash}

	currentFee := FeeLine{ID: "current-fee", SiteID: 1, ContainerID: 10, OrderID: "a", Start: p.Start, End: p.End, Amount: "1", Currency: "USD", SourceRef: "current fee"}
	pastFee := currentFee
	pastFee.ID, pastFee.Start, pastFee.End = "past-fee", prior.Add(time.Hour), prior.Add(2*time.Hour)
	futureFee := currentFee
	futureFee.ID, futureFee.Start, futureFee.End = "future-fee", p.End, p.End.Add(time.Hour)
	in.Fees = []FeeLine{pastFee, currentFee, futureFee}

	// A pre-period source point can determine the period-end state. Ties must
	// remain visible; late-arriving and period-end points are not eligible.
	pool := PoolObservation{ID: "pool-reporting", Kind: 6, AssetID: 1, SourceID: "pool", SourceAt: p.Start.Add(-12 * time.Hour), ReceivedAt: p.End, State: "reporting", SourceRef: "pool reporting"}
	conflict := pool
	conflict.ID, conflict.State, conflict.ReceivedAt, conflict.SourceRef = "pool-failed", "failed", p.AsOf, "pool failed"
	late := conflict
	late.ID, late.ReceivedAt = "pool-late", p.AsOf.Add(time.Nanosecond)
	atEnd := pool
	atEnd.ID, atEnd.SourceAt, atEnd.ReceivedAt = "pool-at-end", p.End, p.End
	in.PoolObservations = []PoolObservation{pool, conflict, late, atEnd}

	result := calculate(t, p, in)
	selectedForParity, err := SelectPublicEvidence(t.Context(), p, in, ObservationSource{})
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyPublicEvidenceCalculation(t.Context(), p, selectedForParity, result); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(result.Metrics["pool_unknown_units"].Causes, "conflicting_pool_observations") {
		t.Fatal("fixture does not produce a pool conflict")
	}
	_, _, id, _ := exportFixture()
	members := scopeEvidenceZIP(t, p, in, result, id)
	for _, tc := range []struct {
		name string
		want []string
		not  []string
	}{
		{"coverage", []string{"support-before-period", "current-coverage"}, []string{"unrelated-coverage"}},
		{"obligations", []string{"episode-earlier", "episode-current"}, []string{"episode-unrelated"}},
		{"hashrates", []string{"crossing-hashrate"}, []string{"past-hash", "future-hash"}},
		{"fees", []string{"current-fee"}, []string{"past-fee", "future-fee"}},
		{"pool_observations", []string{"pool-reporting", "pool-failed"}, []string{"pool-late", "pool-at-end"}},
	} {
		rows, _ := scopeEvidenceRows(t, members, tc.name)
		for _, key := range tc.want {
			if _, ok := rows[key]; !ok {
				t.Errorf("%s missing required row %q", tc.name, key)
			}
		}
		for _, key := range tc.not {
			if _, ok := rows[key]; ok {
				t.Errorf("%s retained irrelevant row %q", tc.name, key)
			}
		}
	}
}

func TestPublicationEvidenceScopeCurrentAttributedMetersNoProration(t *testing.T) {
	p, in := calculationFixture()
	in.Coverage[0].Start = p.Start.Add(-24 * time.Hour)
	in.Configuration.SharedAllocationRef = "reviewed allocation"
	current := MeterInterval{ID: "meter-current", MeterID: "meter-a", SourceID: "meter", SiteID: 1, ContainerID: 10, OrderID: "a", Start: p.Start, End: p.Start.Add(24 * time.Hour), KWh: "98765.4321", AllocationFraction: "0.25", Quality: "measured", SourceRef: "current evidence"}
	crossing := current
	crossing.ID, crossing.MeterID, crossing.Start, crossing.End, crossing.KWh = "meter-crossing", "meter-b", p.Start.Add(-time.Hour), p.Start.Add(time.Hour), "76543.1234"
	in.Meters = []MeterInterval{current, crossing}
	r, _, id, _ := exportFixture()
	r.Period = p
	members := scopeEvidenceZIP(t, p, in, r, id)
	rows, columns := scopeEvidenceRows(t, members, "meters")
	amount := slices.Index(columns, "attributed_kwh")
	if amount < 0 {
		t.Fatalf("missing attributed consumption column: %v", columns)
	}
	if row, ok := rows["meter-current"]; !ok || fmt.Sprint(row[amount]) != "24691.358025" {
		t.Errorf("current meter: unexpected attributed amount %v", row)
	}
	if row, ok := rows["meter-crossing"]; ok && fmt.Sprint(row[amount]) != "" {
		t.Errorf("boundary crossing was prorated or attributed: %v", row)
	}
	for _, forbidden := range []string{"kwh", "allocation_fraction", "allocationFraction"} {
		if slices.Contains(columns, forbidden) {
			t.Errorf("private meter column %q exported", forbidden)
		}
	}
	for name, data := range members {
		for _, private := range []string{"98765.4321", "76543.1234", "allocation_fraction", "allocationFraction"} {
			if bytes.Contains(data, []byte(private)) {
				t.Errorf("private raw reading or allocation field %q in %s", private, name)
			}
		}
	}
}

func TestPublicationEvidenceScopeAnnualAttributedMeters(t *testing.T) {
	p, in := calculationFixture()
	p.Kind, p.End = "annual", p.Start.AddDate(1, 0, 0)
	p.AsOf = p.End
	prior := p.Start.AddDate(-1, 0, 0)
	in.Orders[0].Start = prior.AddDate(-1, 0, 0)
	in.Coverage[0].Start = prior
	in.Configuration.SharedAllocationRef = "reviewed allocation"
	previous := MeterInterval{ID: "prior-meter", MeterID: "meter-prior", SourceID: "meter", SiteID: 1, ContainerID: 10, OrderID: "a", Start: prior, End: p.Start, KWh: "98765.4321", AllocationFraction: "0.25", Quality: "measured", SourceRef: "prior evidence"}
	current := previous
	current.ID, current.MeterID, current.Start, current.End, current.KWh, current.AllocationFraction = "current-meter", "meter-current", p.Start, p.End, "12345.6789", "0.4"
	crossing := previous
	crossing.ID, crossing.MeterID, crossing.Start, crossing.End, crossing.KWh = "boundary-meter", "meter-boundary", p.Start.Add(-time.Hour), p.Start.Add(time.Hour), "76543.1234"
	past := previous
	past.ID, past.MeterID, past.Start, past.End = "past-meter", "meter-past", prior.AddDate(0, 0, -2), prior
	future := previous
	future.ID, future.MeterID, future.Start, future.End = "future-meter", "meter-future", p.End, p.End.Add(time.Hour)
	in.Meters = []MeterInterval{previous, current, crossing, past, future}

	r, _, id, _ := exportFixture()
	r.Period = p
	members := scopeEvidenceZIP(t, p, in, r, id)
	rows, columns := scopeEvidenceRows(t, members, "meters")
	amount := slices.Index(columns, "attributed_kwh")
	if amount < 0 {
		t.Fatalf("missing attributed consumption column: %v", columns)
	}
	for key, want := range map[string]string{"prior-meter": "24691.358025", "current-meter": "4938.27156"} {
		row, ok := rows[key]
		if !ok || fmt.Sprint(row[amount]) != want {
			t.Errorf("%s: attributed amount = %v; want %s", key, row, want)
		}
	}
	label := slices.Index(columns, "comparison_window")
	if label < 0 {
		t.Fatal("missing comparison window label")
	}
	for key, want := range map[string]string{"prior-meter": "prior_year", "current-meter": "current"} {
		if row, ok := rows[key]; !ok || fmt.Sprint(row[label]) != want {
			t.Errorf("%s: comparison label = %v; want %s", key, row, want)
		}
	}
	for _, forbidden := range []string{"kwh", "allocation_fraction", "allocationFraction"} {
		if slices.Contains(columns, forbidden) {
			t.Errorf("private meter column %q exported", forbidden)
		}
	}
	for _, key := range []string{"past-meter", "future-meter"} {
		if _, ok := rows[key]; ok {
			t.Errorf("irrelevant meter %q exported", key)
		}
	}
	if row, ok := rows["boundary-meter"]; ok && fmt.Sprint(row[amount]) != "" {
		t.Errorf("boundary crossing was prorated or attributed: %v", row)
	}
	for name, data := range members {
		for _, private := range []string{"98765.4321", "12345.6789", "76543.1234", "allocation_fraction", "allocationFraction"} {
			if bytes.Contains(data, []byte(private)) {
				t.Errorf("private raw reading or allocation field %q in %s", private, name)
			}
		}
	}
}

func TestPublicationEvidenceScopeAnnualPoolInstants(t *testing.T) {
	p, in := calculationFixture()
	p.Kind, p.End = "annual", p.Start.AddDate(1, 0, 0)
	p.AsOf = p.End
	prior := p.Start.AddDate(-1, 0, 0)
	in.Coverage[0].Start = prior.AddDate(-1, 0, 0)
	point := PoolObservation{ID: "prior", Kind: in.Coverage[0].Kind, AssetID: in.Coverage[0].AssetID, SourceID: "pool", SourceAt: p.Start.Add(-time.Hour), ReceivedAt: p.End, State: "reporting", SourceRef: "prior"}
	conflict := point
	conflict.ID, conflict.SourceAt, conflict.State, conflict.SourceRef = "prior-tie", point.SourceAt.In(time.FixedZone("offset", -5*60*60)), "failed", "tie"
	current := point
	current.ID, current.SourceAt, current.SourceRef = "current", p.End.Add(-time.Hour), "current"
	tooOld := point
	tooOld.ID, tooOld.SourceAt = "before-prior", prior.Add(-time.Hour)
	late := point
	late.ID, late.ReceivedAt = "late", p.AsOf.Add(time.Nanosecond)
	for _, pool := range [][]PoolObservation{{tooOld, point, conflict, current, late}, {late, current, conflict, point, tooOld}} {
		in.PoolObservations = pool
		selected, err := SelectPublicEvidence(t.Context(), p, in, ObservationSource{})
		if err != nil {
			t.Fatal(err)
		}
		ids := map[string]bool{}
		for _, v := range selected.PoolObservations {
			ids[v.ID] = true
		}
		for _, id := range []string{"current"} {
			if !ids[id] {
				t.Errorf("missing pool %s in %v", id, ids)
			}
		}
		for _, id := range []string{"prior", "prior-tie", "before-prior", "late"} {
			if ids[id] {
				t.Errorf("unexpected pool %s in %v", id, ids)
			}
		}
	}
	// A pre-current observation remains relevant when it is the latest at the
	// current end, including equal instants written with different offsets.
	in.PoolObservations = []PoolObservation{conflict, point, late}
	selected, err := SelectPublicEvidence(t.Context(), p, in, ObservationSource{})
	if err != nil {
		t.Fatal(err)
	}
	if len(selected.PoolObservations) != 2 {
		t.Fatalf("current-end pre-period tie was lost: %+v", selected.PoolObservations)
	}
}

func TestPublicationEvidenceScopeObservationProviderFailClosed(t *testing.T) {
	p, in := calculationFixture()
	c := in.Coverage[0]
	valid := Observation{IntervalID: c.AllocationIntervalID, Kind: c.Kind, AssetID: c.AssetID, SiteID: c.SiteID, Bucket: p.Start}
	for _, tc := range []struct {
		name string
		rows []Observation
		want string
	}{
		{"duplicate", []Observation{valid, valid}, "without repeats"},
		{"missing coverage", []Observation{{IntervalID: c.AllocationIntervalID + 1000, Bucket: p.Start}}, "no supporting commercial coverage"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := SelectPublicEvidence(t.Context(), p, in, ObservationSource{Hours: func(yield func(Observation) error) error {
				for _, v := range tc.rows {
					_ = yield(v)
				}
				return nil // deliberately ignore callback errors
			}})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v, want %q", err, tc.want)
			}
		})
	}
}

func TestPublicationEvidenceScopeRetainsSuppressionWitness(t *testing.T) {
	p, in := calculationFixture()
	early := ObligationInterval{ID: "earlier", EpisodeID: "episode", Kind: 6, AssetID: 1, SiteID: 1, ContainerID: 10, OrderID: "a", Start: p.Start.AddDate(0, 0, -10), End: p.Start.AddDate(0, 0, -5), AsOf: p.End, ReceivedAt: p.Start.AddDate(0, 0, -10), Qualified: true, Severity: "S3", TicketRef: "ticket", TicketAt: p.Start.AddDate(0, 0, -10), SourceRef: "old scope"}
	later := early
	later.ID, later.SiteID, later.ContainerID, later.Start, later.End, later.ClosedAt, later.SourceRef = "closed", 2, 20, early.End, p.Start, p.Start, "closed elsewhere"
	unrelated := later
	unrelated.ID, unrelated.EpisodeID = "unrelated", "another episode"
	in.Obligations = []ObligationInterval{early, later, unrelated}
	selected, err := SelectPublicEvidence(t.Context(), p, in, ObservationSource{})
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]bool{}
	for _, o := range selected.Obligations {
		ids[o.ID] = true
	}
	if !ids["earlier"] || !ids["closed"] || ids["unrelated"] {
		t.Fatalf("incorrect suppression context: %v", ids)
	}
}

func TestPublicationEvidenceScopeCapturedCoverageBoundaries(t *testing.T) {
	p, in := calculationFixture()
	c := in.Coverage[0]
	c.Start, c.End, c.ID = p.Start.Add(-2*time.Hour), p.Start.Add(-time.Hour), "earlier"
	next := c
	next.ID, next.Start, next.End = "adjacent", c.End, p.Start
	current := c
	current.ID, current.Start, current.End = "current", p.Start, time.Time{}
	in.Coverage = []CoverageSegment{c, next, current}
	rows := []Observation{
		{IntervalID: c.AllocationIntervalID, Kind: c.Kind, AssetID: c.AssetID, SiteID: c.SiteID, Bucket: p.Start},
		{IntervalID: c.AllocationIntervalID, Kind: c.Kind, AssetID: c.AssetID, SiteID: c.SiteID, Bucket: p.Start.Add(time.Hour), CaptureReason: "gap"},
	}
	selected, err := SelectPublicEvidence(t.Context(), p, in, ObservationSource{Hours: func(yield func(Observation) error) error {
		for _, v := range rows {
			if err := yield(v); err != nil {
				return err
			}
		}
		return nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]bool{}
	for _, v := range selected.Coverage {
		ids[v.ID] = true
	}
	// Rows are matched within the report period. The one-microsecond widening
	// can conservatively include the segment that ends at the period start; the
	// segment covering the rows must never be lost, and earlier ones are not
	// pulled in by a row.
	if !ids["current"] || ids["earlier"] {
		t.Errorf("incorrect captured coverage: %v", ids)
	}
}

func TestPublicEvidenceCalculationReconciliation(t *testing.T) {
	p, in := calculationFixture()
	result := calculate(t, p, in)
	persisted, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	var loaded Result
	if err = json.Unmarshal(persisted, &loaded); err != nil {
		t.Fatal(err)
	}
	selected, err := SelectPublicEvidence(t.Context(), p, in, ObservationSource{})
	if err != nil {
		t.Fatal(err)
	}
	if err = VerifyPublicEvidenceCalculation(t.Context(), p, selected, loaded); err != nil {
		t.Fatal(err)
	}
	selected.Coverage = nil
	if err = VerifyPublicEvidenceCalculation(t.Context(), p, selected, loaded); err == nil {
		t.Fatal("dropped evidence must fail closed")
	}
}

func TestVerifySavedReviewedCalculationPreservesFrozenControls(t *testing.T) {
	p, in := calculationFixture()
	in.Incidents = []Incident{{ID: "reviewed", SiteID: 1, ContainerID: 10, OrderID: "a", Start: p.Start, End: p.End, AsOf: p.AsOf, Category: "curtailment_control", Summary: "reviewed incident", SourceRef: "reviewed"}}
	baseline := calculate(t, p, in)
	if len(baseline.Interruptions) != 1 {
		t.Fatal("reviewed incident missing")
	}
	if err := VerifySavedReviewedCalculation(t.Context(), baseline, baseline); err != nil {
		t.Fatal(err)
	}
	saved := baseline
	saved.Interruptions = append(slices.Clone(saved.Interruptions), Interruption{ID: "control", Category: "curtailment_control", SourceRef: "curtailment-confirmation:1"})
	saved.Sources = uniqueStrings(append(slices.Clone(saved.Sources), "curtailment-confirmation:1"))
	if err := VerifySavedReviewedCalculation(t.Context(), baseline, saved); err != nil {
		t.Fatal(err)
	}
	saved.Metrics = map[string]Metric{}
	if err := VerifySavedReviewedCalculation(t.Context(), baseline, saved); err == nil {
		t.Fatal("changed saved metrics must fail")
	}
}

func TestPublicationEvidenceScopeConfigurationDependencies(t *testing.T) {
	for _, kind := range []string{"monthly", "annual"} {
		t.Run(kind, func(t *testing.T) {
			p, in := calculationFixture()
			p.Kind = kind
			if kind == "annual" {
				p.End = p.Start.AddDate(1, 0, 0)
				p.AsOf = p.End
			}
			prior := p.Start.AddDate(-1, 0, 0)
			in.Orders[0].Start = prior.AddDate(-1, 0, 0)
			in.Coverage[0].Start = prior
			in.Configuration.SharedAllocationRef = "token=unused-allocation"
			in.Configuration.Prerequisites[3].SourceRef = "token=unused-G4"
			in.Configuration.Holidays = []string{"2020-02-03", "2026-02-02", "2026-02-07", "2027-02-01"}
			in.Configuration.Sources = append(in.Configuration.Sources, Source{ID: "unused", Metric: "meter", Certified: true, SourceRef: "token=unused-source"})
			in.Configuration.Registers = append(in.Configuration.Registers,
				RegisterCompleteness{Kind: "coverage", Start: prior.AddDate(-5, 0, 0), End: prior.AddDate(-4, 0, 0), SourceRef: "token=old-register"},
				RegisterCompleteness{Kind: "coverage", Start: prior, End: p.Start, SourceRef: "prior-coverage-register"},
				RegisterCompleteness{Kind: "incidents", Start: prior, End: p.Start, SourceRef: "prior-incident-register"},
				RegisterCompleteness{Kind: "fees", Start: prior, End: p.Start, SourceRef: "token=prior-fee-register"})
			in.Meters = []MeterInterval{{ID: "current-meter", MeterID: "physical", SourceID: "meter", SiteID: 1, ContainerID: 10, OrderID: "a", Start: p.Start, End: p.End, KWh: "10", Quality: "measured", SourceRef: "meter-reading"}}
			in.Fees = []FeeLine{
				{ID: "prior-fee", SiteID: 1, ContainerID: 10, OrderID: "a", Start: prior, End: p.Start, Amount: "1234567", Currency: "USD", SourceRef: "token=prior-fee"},
				{ID: "current-fee", SiteID: 1, ContainerID: 10, OrderID: "a", Start: p.Start, End: p.End, Amount: "2", Currency: "USD", SourceRef: "current-fee"},
			}
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
			for name, data := range members {
				for _, forbidden := range []string{"token=", "1234567", "2020-02-03", "2027-02-01", "2026-02-07"} {
					if bytes.Contains(data, []byte(forbidden)) {
						t.Errorf("unrelated %q exposed in %s", forbidden, name)
					}
				}
			}
			fees, _ := scopeEvidenceRows(t, members, "fees")
			if _, ok := fees["current-fee"]; !ok {
				t.Error("current fee absent")
			}
			if _, ok := fees["prior-fee"]; ok {
				t.Error("prior-only fee disclosed")
			}
			registers := string(members["inputs/register_completeness.csv"])
			for _, ref := range []string{"prior-coverage-register", "prior-incident-register"} {
				if got := strings.Contains(registers, ref); got != (kind == "annual") {
					t.Errorf("%s presence %v for %s", ref, got, kind)
				}
			}
			if !bytes.Contains(members["inputs/source_qualifications.csv"], []byte("meter-cert")) {
				t.Error("meter qualification absent")
			}
			holidays := string(members["inputs/holidays.csv"])
			if got := strings.Contains(holidays, "2026-02-02"); got != (kind == "monthly") {
				t.Errorf("deadline holiday presence %v for %s", got, kind)
			}
			projected, err := publicEvidenceConfiguration(t.Context(), result, selected)
			if err != nil {
				t.Fatal(err)
			}
			// The public configuration intentionally omits gate owners. Replay
			// deadline arithmetic with the selected calendar and original gates.
			calendar := in.Configuration
			calendar.Holidays = projected.Holidays
			if got, want := deadlines(p, calendar), deadlines(p, in.Configuration); !reflect.DeepEqual(got, want) {
				t.Errorf("projected deadline = %+v; want %+v", got, want)
			}
		})
	}
}

func TestPublicationEvidenceScopeAnnualPriorOnlyDependencies(t *testing.T) {
	p, in := calculationFixture()
	p.Kind, p.End = "annual", p.Start.AddDate(1, 0, 0)
	p.AsOf = p.End
	prior := p.Start.AddDate(-1, 0, 0)
	in.Orders[0].Start = prior.AddDate(-1, 0, 0)
	in.Coverage[0].Start = prior
	in.Orders = append(in.Orders, Order{ID: "unused-prior-order", Name: "Unused", Start: prior, End: p.Start, SourceRef: "unused prior order"})
	in.ExternalResults = []ContractResult{{ID: "prior-result", Metric: "annual_uptime", SiteID: 1, Start: prior, End: p.Start, Value: "95", Unit: "percent", SourceRef: "unused prior result", CalculationRef: "unused prior calculation"}}
	in.Obligations = []ObligationInterval{{ID: "closed-prior", EpisodeID: "closed", Kind: 6, AssetID: 1, SiteID: 1, ContainerID: 10, OrderID: "a", Start: prior, End: p.Start, AsOf: p.Start, ReceivedAt: prior, ClosedAt: p.Start, SourceRef: "unused prior episode"}}
	in.Incidents = []Incident{{ID: "prior-outage", SiteID: 1, ContainerID: 10, OrderID: "a", Start: prior, End: prior.Add(time.Hour), AsOf: p.Start, Category: "outage", Summary: "reviewed", SourceRef: "prior-outage"},
		{ID: "prior-maintenance", SiteID: 1, ContainerID: 10, OrderID: "a", Start: prior.Add(2 * time.Hour), End: prior.Add(3 * time.Hour), AsOf: p.Start, Category: "maintenance", Summary: "maintenance", RootCause: "known", SourceRef: "prior-maintenance"}}
	in.Configuration.Registers = append(in.Configuration.Registers, RegisterCompleteness{Kind: "incidents", Start: prior, End: p.End, SourceRef: "incidents complete"})
	full := calculate(t, p, in)
	selected, err := SelectPublicEvidence(t.Context(), p, in, ObservationSource{})
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyPublicEvidenceCalculation(t.Context(), p, selected, full); err != nil {
		t.Fatal(err)
	}
	if len(selected.ExternalResults) != 0 || len(selected.Obligations) != 0 || len(selected.Orders) != 1 || len(selected.Incidents) != 2 {
		t.Fatalf("incorrect prior dependencies: results=%+v obligations=%+v orders=%+v incidents=%+v", selected.ExternalResults, selected.Obligations, selected.Orders, selected.Incidents)
	}
	if !slices.Contains(full.Metrics["year_over_year_maintenance"].Causes, "root_cause_missing") {
		t.Fatalf("prior outage missing maintenance status evidence: %+v", full.Metrics["year_over_year_maintenance"])
	}
}

func TestPublicationEvidenceScopeCrossingMeterDoesNotExposeQualification(t *testing.T) {
	p, in := calculationFixture()
	p.Kind, p.End = "annual", p.Start.AddDate(1, 0, 0)
	p.AsOf = p.End
	in.Coverage[0].Start = p.Start.AddDate(-1, 0, 0)
	in.Orders[0].Start = in.Coverage[0].Start
	in.Configuration.SharedAllocationRef = "token=unused-shared"
	in.Configuration.Sources = append(in.Configuration.Sources, Source{ID: "crossing-only", Metric: "meter", Certified: true, SourceRef: "token=unused-certification"})
	in.Meters = []MeterInterval{{ID: "crossing", MeterID: "meter", SourceID: "crossing-only", SiteID: 1, ContainerID: 10, OrderID: "a", Start: p.Start.Add(-time.Hour), End: p.Start.Add(time.Hour), KWh: "123", Quality: "measured", AllocationFraction: "0.5", SourceRef: "boundary-evidence"}}
	full := calculate(t, p, in)
	selected, err := SelectPublicEvidence(t.Context(), p, in, ObservationSource{})
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyPublicEvidenceCalculation(t.Context(), p, selected, full); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(full.Metrics["metered_energy_kwh"].Causes, "meter_period_boundary_without_interval_evidence") {
		t.Fatal("crossing meter boundary cause missing")
	}
	_, _, id, _ := exportFixture()
	members := scopeEvidenceZIP(t, p, in, full, id)
	for name, data := range members {
		if bytes.Contains(data, []byte("token=")) {
			t.Errorf("unused crossing-meter qualification or allocation leaked in %s", name)
		}
	}
}

func TestPublicationEvidenceConfigurationUnusedGatesAndQualifications(t *testing.T) {
	t.Run("uncovered ad hoc", func(t *testing.T) {
		p, in := calculationFixture()
		p.Kind = "ad_hoc"
		in.Coverage = nil
		in.Orders = nil
		in.Configuration.Prerequisites[2].SourceRef = "token=unused-g3"
		in.Configuration.Prerequisites[4].SourceRef = "token=unused-g5"
		full := calculate(t, p, in)
		_, _, id, _ := exportFixture()
		members := scopeEvidenceZIP(t, p, in, full, id)
		for name, data := range members {
			if bytes.Contains(data, []byte("token=")) {
				t.Errorf("unused gate leaked in %s", name)
			}
		}
	})
	t.Run("overlap without certification", func(t *testing.T) {
		p, in := calculationFixture()
		in.Configuration.Sources = append(in.Configuration.Sources,
			Source{ID: "overlap-a", Metric: "hashrate", Certified: true, SourceRef: "token=unused-a"},
			Source{ID: "overlap-b", Metric: "hashrate", Certified: true, SourceRef: "token=unused-b"})
		h := HashrateInterval{ID: "hash-a", Kind: 6, AssetID: 1, SiteID: 1, ContainerID: 10, OrderID: "a", SourceID: "overlap-a", Start: p.Start, End: p.End, HashrateHS: "42", Complete: true, SourceRef: "hash-a"}
		other := h
		other.ID, other.SourceID, other.SourceRef = "hash-b", "overlap-b", "hash-b"
		in.Hashrates = []HashrateInterval{h, other}
		full := calculate(t, p, in)
		if !slices.Contains(full.Metrics["hashrate_performance_hs"].Causes, "overlapping_hashrate_evidence") {
			t.Fatal("fixture did not produce overlapping hashrate cause")
		}
		_, _, id, _ := exportFixture()
		members := scopeEvidenceZIP(t, p, in, full, id)
		for name, data := range members {
			if bytes.Contains(data, []byte("token=")) {
				t.Errorf("unused qualification leaked in %s", name)
			}
		}
		rows, _ := scopeEvidenceRows(t, members, "hashrates")
		if len(rows) != 2 {
			t.Fatalf("overlap evidence missing: %v", rows)
		}
	})
	t.Run("unapproved gate cause without unused reference", func(t *testing.T) {
		p, in := calculationFixture()
		in.Configuration.Prerequisites[2].Ready = false
		in.Configuration.Prerequisites[2].SourceRef = "token=unused-approval"
		full := calculate(t, p, in)
		_, _, id, _ := exportFixture()
		members := scopeEvidenceZIP(t, p, in, full, id)
		if !bytes.Contains(members["inputs/prerequisites.csv"], []byte("G3")) || bytes.Contains(members["inputs/prerequisites.csv"], []byte("token=")) {
			t.Fatalf("unapproved gate not safely represented: %s", members["inputs/prerequisites.csv"])
		}
	})
}

// Publication verifies the selection the archive returns, so the archive must
// read the frozen observations once and select exactly what selection alone does.
func TestWriteSelectedEvidenceReturnsSelectionFromOnePass(t *testing.T) {
	p, in := calculationFixture()
	result := calculate(t, p, in)
	_, _, identity, _ := exportFixture()
	c := in.Coverage[0]
	hours := []Observation{
		{IntervalID: c.AllocationIntervalID, AssetID: c.AssetID, SiteID: c.SiteID, Kind: c.Kind, Bucket: p.Start, Revision: 1, CapturedAt: p.Start, CaptureReason: "closed", WindowStart: p.Start, WindowEnd: p.Start.Add(time.Hour)},
		{IntervalID: c.AllocationIntervalID, AssetID: c.AssetID, SiteID: c.SiteID, Kind: c.Kind, Bucket: p.Start.Add(time.Hour), Revision: 2, CapturedAt: p.Start, CaptureReason: "closed", WindowStart: p.Start.Add(time.Hour), WindowEnd: p.Start.Add(2 * time.Hour)},
	}
	passes := 0
	source := ObservationSource{Hours: func(yield func(Observation) error) error {
		passes++
		for _, v := range hours {
			if err := yield(v); err != nil {
				return err
			}
		}
		return nil
	}}
	want, err := SelectPublicEvidence(t.Context(), p, in, source)
	if err != nil {
		t.Fatal(err)
	}
	passes = 0
	var out bytes.Buffer
	got, err := WriteSelectedEvidenceContext(t.Context(), &out, result, in, identity, source)
	if err != nil {
		t.Fatal(err)
	}
	if passes != 1 {
		t.Fatalf("archive read the observations %d times, want 1", passes)
	}
	if !reflect.DeepEqual(want, got) {
		t.Fatalf("archive selection differs from public selection:\nwant %+v\n got %+v", want, got)
	}
	if err := VerifyPublicEvidenceCalculation(t.Context(), p, got, result); err != nil {
		t.Fatal(err)
	}
}
