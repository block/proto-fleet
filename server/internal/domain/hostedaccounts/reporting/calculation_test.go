package reporting

import (
	"encoding/json"
	"math/rand"
	"reflect"
	"slices"
	"testing"
	"time"
)

func instant(s string) time.Time {
	t, e := time.Parse(time.RFC3339, s)
	if e != nil {
		panic(e)
	}
	return t
}
func calculationFixture() (Period, Inputs) {
	p := Period{Kind: "monthly", Start: instant("2026-01-01T00:00:00Z"), End: instant("2026-02-01T00:00:00Z"), AsOf: instant("2026-02-02T00:00:00Z"), Timezone: "UTC"}
	c := Configuration{Timezone: "UTC", ReportingYearMonth: 1, ReportingYearDay: 1, ContractRef: "signed-agreement"}
	for _, k := range []string{"G1", "G2", "G3", "G4", "G5"} {
		c.Prerequisites = append(c.Prerequisites, Prerequisite{Key: k, Ready: true, Owner: "owner", SourceRef: "approval:" + k})
	}
	for _, kind := range []string{"coverage", "fees", "obligations"} {
		c.Registers = append(c.Registers, RegisterCompleteness{Kind: kind, Start: p.Start, End: p.End, SourceRef: "complete:" + kind})
	}
	c.Sources = []Source{{ID: "meter", Metric: "meter", Certified: true, SourceRef: "meter-cert"}, {ID: "hash", Metric: "hashrate", Certified: true, SourceRef: "hash-cert"}}
	return p, Inputs{Configuration: c, Orders: []Order{{ID: "a", Name: "A", Start: p.Start.AddDate(-1, 0, 0), SourceRef: "order-a"}}, Coverage: []CoverageSegment{{ID: "c", AllocationIntervalID: 1, Kind: 6, AssetID: 1, SiteID: 1, ContainerID: 10, OrderID: "a", Start: p.Start, CapacityTHS: "100", SourceRef: "coverage"}}}
}
func calculate(t *testing.T, p Period, in Inputs) Result {
	t.Helper()
	r, e := Calculate(p, in)
	if e != nil {
		t.Fatal(e)
	}
	return r
}
func requireValue(t *testing.T, m Metric, v string, s Status) {
	t.Helper()
	if m.Value != v || m.Status != s {
		t.Fatalf("metric %+v; want %s %s", m, v, s)
	}
}
func TestCalculateExactMoneyAndAlternativeBreakdowns(t *testing.T) {
	p, in := calculationFixture()
	in.Orders = append(in.Orders, Order{ID: "b", Name: "B", Start: p.Start, SourceRef: "order-b"})
	c := in.Coverage[0]
	c.ID = "b"
	c.AssetID = 2
	c.AllocationIntervalID = 2
	c.OrderID = "b"
	in.Coverage = append(in.Coverage, c)
	in.Configuration.SharedAllocationRef = "reviewed-shares"
	for _, o := range []string{"a", "b"} {
		in.Meters = append(in.Meters, MeterInterval{ID: o, MeterID: "physical", SourceID: "meter", SiteID: 1, ContainerID: 10, OrderID: o, Start: p.Start, End: p.End, KWh: "100.0000000000000000001", Quality: "measured", AllocationFraction: "0.5", SourceRef: "meter-" + o})
	}
	in.Fees = []FeeLine{{ID: "1", SiteID: 1, ContainerID: 10, OrderID: "a", Start: p.Start, End: p.End, Amount: "0.10000000000000000001", Currency: "USD", SourceRef: "f1"}, {ID: "2", SiteID: 1, ContainerID: 10, OrderID: "a", Start: p.Start, End: p.End, Amount: "0.2", Currency: "USD", SourceRef: "f2"}, {ID: "3", SiteID: 1, ContainerID: 10, OrderID: "b", Start: p.Start, End: p.End, Amount: "2.3", Currency: "EUR", SourceRef: "f3"}}
	r := calculate(t, p, in)
	requireValue(t, r.Metrics["metered_energy_kwh"], "100.0000000000000000001", Authoritative)
	requireValue(t, r.Orders[0].Metrics["metered_energy_kwh"], "50.00000000000000000005", Authoritative)
	want := []Amount{{Currency: "EUR", Value: "2.3", Exact: "23/10"}, {Currency: "USD", Value: "0.30000000000000000001", Exact: "30000000000000000001/100000000000000000000"}}
	if !reflect.DeepEqual(r.Metrics["accrued_hosting_fees"].Amounts, want) {
		t.Fatalf("money %+v", r.Metrics["accrued_hosting_fees"])
	}
	if len(r.Containers) != 1 || len(r.Orders) != 2 {
		t.Fatal("missing breakdowns")
	}
	for _, v := range r.Reconciliations {
		if !v.Matches {
			t.Fatalf("reconciliation %+v", v)
		}
	}
}
func TestCalculatePartialAndUnsupportedMeterAttribution(t *testing.T) {
	p, in := calculationFixture()
	mid := p.Start.AddDate(0, 0, 15)
	in.Meters = []MeterInterval{{ID: "m", MeterID: "m", SourceID: "meter", SiteID: 1, ContainerID: 10, OrderID: "a", Start: p.Start, End: mid, KWh: "12.5", Quality: "measured", SourceRef: "m"}}
	r := calculate(t, p, in)
	requireValue(t, r.Metrics["metered_energy_kwh"], "12.5", Provisional)
	in.Meters[0].End = p.End
	in.Coverage[0].End = mid
	r = calculate(t, p, in)
	requireValue(t, r.Metrics["metered_energy_kwh"], "", Unavailable)
	in.Coverage[0].End = time.Time{}
	in.Meters[0].Start = p.Start.Add(-time.Hour)
	r = calculate(t, p, in)
	requireValue(t, r.Metrics["metered_energy_kwh"], "", Unavailable)
}
func TestCalculateHashrateIsNotOfflineOrContractualUptime(t *testing.T) {
	p, in := calculationFixture()
	in.Configuration.Registers = in.Configuration.Registers[:2]
	in.Hashrates = []HashrateInterval{{ID: "h", AssetID: 1, Kind: 6, SiteID: 1, ContainerID: 10, OrderID: "a", SourceID: "hash", Start: p.Start, End: p.End, HashrateHS: "50000000000000", Complete: true, SourceRef: "h"}}
	r := calculate(t, p, in)
	requireValue(t, r.Metrics["hashrate_performance_hs"], "50000000000000", Authoritative)
	requireValue(t, r.Metrics["hashrate_performance_percent"], "50", Authoritative)
	for _, k := range []string{"offline_units", "monthly_uptime", "annual_service_credit", "termination_section_5_6"} {
		requireValue(t, r.Metrics[k], "", Unavailable)
	}
	in.ExternalResults = []ContractResult{{ID: "ext", Metric: "monthly_uptime", SiteID: 1, Start: p.Start, End: p.End, Value: "99.1234567890123456789", Unit: "percent", SourceRef: "reviewed", CalculationRef: "workbook"}}
	r = calculate(t, p, in)
	requireValue(t, r.Metrics["monthly_uptime"], "99.1234567890123456789", Provisional)
	if r.Metrics["monthly_uptime"].Method != "external" {
		t.Fatal("external not explicit")
	}
}
func TestCalculateHashrateGapsAndConflicts(t *testing.T) {
	p, in := calculationFixture()
	h := HashrateInterval{ID: "h", AssetID: 1, Kind: 6, SiteID: 1, ContainerID: 10, OrderID: "a", SourceID: "hash", Start: p.Start, End: p.Start.Add(p.End.Sub(p.Start) / 2), HashrateHS: "50000000000000", Complete: true, SourceRef: "h"}
	in.Hashrates = []HashrateInterval{h}
	r := calculate(t, p, in)
	requireValue(t, r.Metrics["hashrate_performance_hs"], "25000000000000", Provisional)
	h.ID = "conflict"
	in.Hashrates = append(in.Hashrates, h)
	r = calculate(t, p, in)
	requireValue(t, r.Metrics["hashrate_performance_hs"], "", Unavailable)
}
func TestCalculateUnionsInterruptionsAndRetainsCauses(t *testing.T) {
	p, in := calculationFixture()
	s := p.Start.Add(time.Hour)
	in.Incidents = []Incident{{ID: "a", SiteID: 1, ContainerID: 10, OrderID: "a", Start: s, End: s.Add(2 * time.Hour), AsOf: p.AsOf, Category: "outage", Summary: "A", RootCause: "power", SourceRef: "a"}, {ID: "b", SiteID: 1, ContainerID: 10, OrderID: "a", Start: s.Add(time.Hour), End: s.Add(3 * time.Hour), AsOf: p.AsOf, Category: "outage", Summary: "B", RootCause: "network", SourceRef: "b"}}
	r := calculate(t, p, in)
	requireValue(t, r.Metrics["interruption_duration_hours"], "3", Provisional)
	if len(r.Interruptions) != 2 || r.Interruptions[0].RootCause != "power" || r.Interruptions[1].RootCause != "network" {
		t.Fatal("causes lost")
	}
}
func TestCalculateBacklogCumulativeAgeAndAdministrativeClosure(t *testing.T) {
	p, in := calculationFixture()
	s := p.Start.AddDate(0, 0, -70)
	in.Orders[0].Start = s.AddDate(-1, 0, 0)
	in.Coverage[0].Start = s
	in.Obligations = []ObligationInterval{{ID: "e", EpisodeID: "e", Kind: 6, AssetID: 1, SiteID: 1, ContainerID: 10, OrderID: "a", Start: s, AsOf: p.AsOf, ReceivedAt: s, Qualified: true, Severity: "S1", TicketRef: "ticket", TicketAt: s.Add(time.Hour), Deadline: s.Add(48 * time.Hour), SourceRef: "episode"}}
	r := calculate(t, p, in)
	for _, suffix := range []string{"offline", "ticket_S1", "age_over_7_days", "age_over_30_days", "age_over_60_days", "beyond_deadline"} {
		for _, measure := range []string{"month_end", "daily_average"} {
			requireValue(t, r.Metrics["backlog_"+suffix+"_"+measure], "1", Authoritative)
		}
	}
	in.Obligations[0].ClosedAt = p.Start.Add(time.Hour)
	r = calculate(t, p, in)
	requireValue(t, r.Metrics["backlog_offline_month_end"], "1", Authoritative)
	requireValue(t, r.Metrics["backlog_ticket_S1_month_end"], "0", Authoritative)
	in.Configuration.Prerequisites[0].Ready = false
	r = calculate(t, p, in)
	requireValue(t, r.Metrics["backlog_offline_month_end"], "1", Provisional)
}
func TestCalculateDailyAverageUsesActualDSTDayDuration(t *testing.T) {
	loc, _ := time.LoadLocation("America/New_York")
	p, in := calculationFixture()
	p.Start = time.Date(2026, 3, 1, 0, 0, 0, 0, loc)
	p.End = p.Start.AddDate(0, 1, 0)
	p.AsOf = p.End
	p.Timezone = loc.String()
	in.Configuration.Timezone = loc.String()
	for i := range in.Configuration.Registers {
		in.Configuration.Registers[i].Start = p.Start
		in.Configuration.Registers[i].End = p.End
	}
	in.Coverage[0].Start = p.Start
	s := time.Date(2026, 3, 8, 0, 0, 0, 0, loc)
	in.Obligations = []ObligationInterval{{ID: "e", EpisodeID: "e", Kind: 6, AssetID: 1, SiteID: 1, ContainerID: 10, OrderID: "a", Start: s, End: s.AddDate(0, 0, 1), AsOf: p.End, ReceivedAt: s, Qualified: true, SourceRef: "e"}}
	r := calculate(t, p, in)
	if r.Metrics["backlog_offline_daily_average"].Exact != "1/31" {
		t.Fatalf("DST: %+v", r.Metrics["backlog_offline_daily_average"])
	}
	requireValue(t, r.Metrics["backlog_offline_month_end"], "0", Authoritative)
}
func TestCalculateDueDatesAndFirstYearComparison(t *testing.T) {
	p, in := calculationFixture()
	in.Configuration.Holidays = []string{"2026-02-16"}
	r := calculate(t, p, in)
	if r.Deadlines.Contract.Date != "2026-02-23" || r.Deadlines.Internal.Date != "2026-02-13" || r.Deadlines.Contract.Status != Authoritative {
		t.Fatalf("due %+v", r.Deadlines)
	}
	in.Configuration.Prerequisites[4].Ready = false
	r = calculate(t, p, in)
	if r.Deadlines.Contract.Status != Provisional {
		t.Fatal("unapproved calendar authoritative")
	}
	p.Kind = "annual"
	p.End = p.Start.AddDate(1, 0, 0)
	p.AsOf = p.End
	r = calculate(t, p, in)
	if r.Deadlines.Contract.Date != "2027-01-30" || r.Deadlines.Internal.Date != "2027-01-20" {
		t.Fatalf("annual %+v", r.Deadlines)
	}
	for _, k := range []string{"year_over_year_uptime", "year_over_year_energy", "year_over_year_hashrate", "year_over_year_maintenance"} {
		requireValue(t, r.Metrics[k], "", Unavailable)
	}
}
func TestCalculateCanonicalAndEveryRequiredField(t *testing.T) {
	p, in := calculationFixture()
	for i := range 5 {
		s := p.Start.Add(time.Duration(i) * time.Hour)
		in.Incidents = append(in.Incidents, Incident{ID: string(rune('a' + i)), SiteID: 1, ContainerID: 10, OrderID: "a", Start: s, End: s.Add(time.Hour), AsOf: p.AsOf, Category: "outage", Summary: "incident", SourceRef: string(rune('a' + i))})
	}
	original := calculate(t, p, in)
	want, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	rng := rand.New(rand.NewSource(13))
	for range 10 {
		rng.Shuffle(len(in.Incidents), func(i, j int) { in.Incidents[i], in.Incidents[j] = in.Incidents[j], in.Incidents[i] })
		got, err := json.Marshal(calculate(t, p, in))
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != string(want) {
			t.Fatal("input order changes result")
		}
	}
	for _, k := range RequiredMetricKeys() {
		if _, ok := original.Metrics[k]; !ok {
			t.Fatalf("missing key %s", k)
		}
	}
	for _, m := range original.Metrics {
		if m.Status == Unavailable && (m.Value != "" || m.Exact != "" || len(m.Amounts) > 0) {
			t.Fatalf("unavailable numeric %+v", m)
		}
		if m.Status != Authoritative && len(m.Causes) == 0 {
			t.Fatalf("missing cause %+v", m)
		}
	}
}
func TestCalculateMissingScopeEvidenceAndInvalidInput(t *testing.T) {
	p, in := calculationFixture()
	in.Coverage = nil
	in.Fees = []FeeLine{{ID: "f", SiteID: 1, ContainerID: 10, OrderID: "a", Start: p.Start, End: p.End, Amount: "10", Currency: "USD", SourceRef: "finance"}}
	r := calculate(t, p, in)
	requireValue(t, r.Metrics["accrued_hosting_fees"], "", Unavailable)
	if len(r.Containers) != 1 || len(r.Orders) != 1 {
		t.Fatal("missing scoped gap")
	}
	for _, m := range r.Metrics {
		if m.Status == Authoritative {
			t.Fatalf("no scope: %+v", m)
		}
	}
	p.Timezone = "Africa/Nairobi"
	if _, e := Calculate(p, in); e == nil {
		t.Fatal("mismatched calendar accepted")
	}
}

func TestCalculateLateEpisodeFactDoesNotChangeAsOfSnapshot(t *testing.T) {
	p, in := calculationFixture()
	v := ObligationInterval{ID: "current", EpisodeID: "same", Kind: 6, AssetID: 1, SiteID: 1, ContainerID: 10, OrderID: "a", Start: p.Start, AsOf: p.AsOf, ReceivedAt: p.Start, Qualified: true, SourceRef: "current"}
	in.Obligations = []ObligationInterval{v}
	want := calculate(t, p, in)
	late := v
	late.ID = "late"
	late.Start = p.Start.AddDate(0, 0, -60)
	late.ReceivedAt = p.AsOf.Add(time.Hour)
	late.SourceRef = "late"
	in.Obligations = append(in.Obligations, late)
	got := calculate(t, p, in)
	a, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	if string(a) != string(b) {
		t.Fatal("fact received after report as-of changed the snapshot")
	}
}
func TestCalculatePoolEvidenceRespectsCoverageAndConflictingObservations(t *testing.T) {
	p, in := calculationFixture()
	in.PoolObservations = []PoolObservation{{ID: "old", Kind: 6, AssetID: 1, SourceID: "pool", SourceAt: p.Start.Add(-time.Hour), ReceivedAt: p.Start, State: "reporting", SourceRef: "previous-attribution"}}
	r := calculate(t, p, in)
	requireValue(t, r.Metrics["pool_unknown_units"], "1", Provisional)
	if slicesContains(r.Sources, "previous-attribution") {
		t.Fatal("observation outside commercial coverage exposed")
	}
	in.PoolObservations[0].SourceAt = p.End.Add(-time.Minute)
	in.PoolObservations[0].ReceivedAt = p.End
	conflict := in.PoolObservations[0]
	conflict.ID = "conflict"
	conflict.State = "failed"
	conflict.SourceRef = "conflicting"
	in.PoolObservations = append(in.PoolObservations, conflict)
	r = calculate(t, p, in)
	requireValue(t, r.Metrics["pool_unknown_units"], "1", Provisional)
}
func TestCalculateExternalAnnualWindowAndMissingApproval(t *testing.T) {
	p, in := calculationFixture()
	p.Start = p.End
	p.End = p.Start.AddDate(0, 1, 0)
	p.AsOf = p.End
	in.ExternalResults = []ContractResult{{ID: "annual", Metric: "annual_service_credit", SiteID: 1, Start: instant("2026-01-01T00:00:00Z"), End: p.End, Value: "12.001", Unit: "USD", SourceRef: "reviewed-credit", CalculationRef: "external"}}
	in.Configuration.Prerequisites[0].Ready = false
	r := calculate(t, p, in)
	requireValue(t, r.Metrics["annual_service_credit"], "12.001", Provisional)
	if !slicesContains(r.Metrics["annual_service_credit"].Causes, "missing_G1") {
		t.Fatal("unapproved formula not qualified")
	}
}
func TestCalculateMoveHasIndependentReconciliationAndCertifiedSourceGates(t *testing.T) {
	p, in := calculationFixture()
	mid := p.Start.AddDate(0, 0, 15)
	in.Coverage[0].End = mid
	in.Orders = append(in.Orders, Order{ID: "b", Name: "B", Start: mid, SourceRef: "order-b"})
	c := in.Coverage[0]
	c.ID = "moved"
	c.AllocationIntervalID = 2
	c.Start = mid
	c.End = time.Time{}
	c.OrderID = "b"
	c.ContainerID = 20
	in.Coverage = append(in.Coverage, c)
	in.Meters = []MeterInterval{{ID: "a", MeterID: "a", SourceID: "meter", SiteID: 1, ContainerID: 10, OrderID: "a", Start: p.Start, End: mid, KWh: "20", Quality: "measured", SourceRef: "a"}, {ID: "b", MeterID: "b", SourceID: "meter", SiteID: 1, ContainerID: 20, OrderID: "b", Start: mid, End: p.End, KWh: "30", Quality: "measured", SourceRef: "b"}}
	r := calculate(t, p, in)
	requireValue(t, r.Metrics["metered_energy_kwh"], "50", Authoritative)
	for _, v := range r.Reconciliations {
		if !v.Matches {
			t.Fatal(v)
		}
	}
	in.Configuration.Prerequisites[1].Ready = false
	r = calculate(t, p, in)
	requireValue(t, r.Metrics["metered_energy_kwh"], "50", Provisional)
}
func slicesContains(ss []string, s string) bool {
	for _, v := range ss {
		if v == s {
			return true
		}
	}
	return false
}

func TestCalculateReviewedRegistersDistinguishKnownZeroAndMissingFacts(t *testing.T) {
	p, in := calculationFixture()
	in.Configuration.Registers = append(in.Configuration.Registers, RegisterCompleteness{Kind: "incidents", Start: p.Start, End: p.End, SourceRef: "incident-register"})
	r := calculate(t, p, in)
	requireValue(t, r.Metrics["backlog_offline_month_end"], "0", Authoritative)
	requireValue(t, r.Metrics["interruption_duration_hours"], "0", Authoritative)
	requireValue(t, r.Metrics["accrued_hosting_fees"], "0", Authoritative)
	in.Configuration.Registers = in.Configuration.Registers[:1]
	r = calculate(t, p, in)
	requireValue(t, r.Metrics["backlog_offline_month_end"], "", Unavailable)
	requireValue(t, r.Metrics["accrued_hosting_fees"], "", Unavailable)
	requireValue(t, r.Metrics["interruption_duration_hours"], "", Unavailable)
}
func TestCalculateAdHocReceiptIsReasonableEffortsTarget(t *testing.T) {
	p, in := calculationFixture()
	p.Kind = "ad_hoc"
	p.RequestReceivedAt = instant("2026-01-15T16:30:00Z")
	p.RequestRef = "client-request"
	r := calculate(t, p, in)
	if r.Deadlines.Contract.Date != "2026-02-14" || r.Deadlines.Contract.Status != Provisional || !r.Deadlines.Contract.ReasonableEfforts || !slicesContains(r.Deadlines.Contract.Sources, "client-request") {
		t.Fatalf("ad hoc target: %+v", r.Deadlines.Contract)
	}
}
func TestCalculateOpenS3SurvivesRestorationUntilAdministrativeClose(t *testing.T) {
	p, in := calculationFixture()
	s := p.Start.Add(time.Hour)
	restored := s.Add(24 * time.Hour)
	in.Obligations = []ObligationInterval{{ID: "e", EpisodeID: "e", Kind: 6, AssetID: 1, SiteID: 1, ContainerID: 10, OrderID: "a", Start: s, End: restored, AsOf: p.AsOf, ReceivedAt: s, Qualified: true, Severity: "S3", TicketRef: "ticket", TicketAt: s, RestoredAt: restored, SourceRef: "reviewed-ticket"}}
	r := calculate(t, p, in)
	requireValue(t, r.Metrics["backlog_offline_month_end"], "0", Authoritative)
	requireValue(t, r.Metrics["open_S3_items"], "1", Authoritative)
}
func TestCalculateLateDifferentScopeDoesNotCreateBreakdown(t *testing.T) {
	p, in := calculationFixture()
	want := calculate(t, p, in)
	in.Obligations = []ObligationInterval{{ID: "late", EpisodeID: "late", Kind: 6, AssetID: 2, SiteID: 1, ContainerID: 20, OrderID: "a", Start: p.Start, AsOf: p.AsOf, ReceivedAt: p.AsOf.Add(time.Hour), Qualified: true, SourceRef: "late"}}
	got := calculate(t, p, in)
	a, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	if string(a) != string(b) {
		t.Fatal("late fact created a new scope")
	}
}

func TestCalculateStaleOpenObligationObservationGapAcrossPeriodStart(t *testing.T) {
	for _, offset := range []time.Duration{-time.Hour, 0, time.Hour} {
		t.Run(offset.String(), func(t *testing.T) {
			p, in := calculationFixture()
			start := p.Start.AddDate(0, 0, -7)
			in.Coverage[0].Start = start
			in.Obligations = []ObligationInterval{{ID: "stale", EpisodeID: "stale", Kind: 6, AssetID: 1, SiteID: 1, ContainerID: 10, OrderID: "a", Start: start, AsOf: p.Start.Add(offset), ReceivedAt: start, Qualified: true, SourceRef: "last observed open"}}
			r := calculate(t, p, in)
			for _, key := range []string{"backlog_offline_month_end", "backlog_offline_daily_average", "backlog_unknown_coverage_month_end", "backlog_unknown_coverage_daily_average"} {
				metric := r.Metrics[key]
				if metric.Status == Authoritative || !slicesContains(metric.Causes, "obligation_observation_gap") {
					t.Fatalf("%s must retain stale open observation uncertainty: %+v", key, metric)
				}
			}
			requireValue(t, r.Metrics["backlog_unknown_coverage_month_end"], "1", Provisional)
			if offset <= 0 {
				requireValue(t, r.Metrics["backlog_unknown_coverage_daily_average"], "1", Provisional)
			}
		})
	}
}

func TestCalculateAnnualComparisonUsesContractTimezoneAfterUTCDecode(t *testing.T) {
	_, in := calculationFixture()
	loc, _ := time.LoadLocation("America/New_York")
	start := time.Date(2026, 3, 9, 0, 0, 0, 0, loc)
	priorStart := start.AddDate(-1, 0, 0)
	p := Period{Kind: "annual", Start: start.UTC(), End: start.AddDate(1, 0, 0).UTC(), AsOf: start.AddDate(1, 0, 0).UTC(), Timezone: loc.String()}
	in.Configuration.Timezone = loc.String()
	in.Configuration.ReportingYearMonth, in.Configuration.ReportingYearDay = 3, 9
	for i := range in.Configuration.Registers {
		in.Configuration.Registers[i].Start, in.Configuration.Registers[i].End = priorStart, p.End
	}
	in.Orders[0].Start = priorStart
	in.Coverage[0].Start = priorStart
	for i, bounds := range [][2]time.Time{{priorStart, p.Start}, {p.Start, p.End}} {
		id := string(rune('a' + i))
		in.Meters = append(in.Meters, MeterInterval{ID: id, MeterID: "physical", SourceID: "meter", SiteID: 1, ContainerID: 10, OrderID: "a", Start: bounds[0], End: bounds[1], KWh: "100", Quality: "measured", SourceRef: "whole year " + id})
		in.Hashrates = append(in.Hashrates, HashrateInterval{ID: id, AssetID: 1, Kind: 6, SiteID: 1, ContainerID: 10, OrderID: "a", SourceID: "hash", Start: bounds[0], End: bounds[1], HashrateHS: "50000000000000", Complete: true, SourceRef: "constant hashrate " + id})
	}
	r := calculate(t, p, in)
	requireValue(t, r.Metrics["year_over_year_energy"], "0", Provisional)
	requireValue(t, r.Metrics["year_over_year_hashrate"], "0", Provisional)
	for _, key := range []string{"year_over_year_energy", "year_over_year_hashrate"} {
		if slicesContains(r.Metrics[key].Causes, "commercial_coverage_register_completeness_unverified") || slicesContains(r.Metrics[key].Causes, "hashrate_observation_gap") {
			t.Fatalf("calendar aligned complete evidence has a fabricated gap: %+v", r.Metrics[key])
		}
	}
}

func TestCalculateStaleOpenIncidentObservationGapAcrossPeriodStart(t *testing.T) {
	for _, offset := range []time.Duration{-time.Hour, 0, time.Hour} {
		t.Run(offset.String(), func(t *testing.T) {
			p, in := calculationFixture()
			in.Configuration.Registers = append(in.Configuration.Registers, RegisterCompleteness{Kind: "incidents", Start: p.Start, End: p.End, SourceRef: "complete:incidents"})
			start := p.Start.AddDate(0, 0, -7)
			in.Coverage[0].Start = start
			in.Incidents = []Incident{{ID: "stale", SiteID: 1, ContainerID: 10, OrderID: "a", Start: start, AsOf: p.Start.Add(offset), Category: "outage", Summary: "Still open at last observation", RootCause: "power", SourceRef: "last observed open"}}
			r := calculate(t, p, in)
			metric := r.Metrics["interruption_duration_hours"]
			if metric.Status == Authoritative || !slicesContains(metric.Causes, "incident_observation_gap") {
				t.Fatalf("stale open observation must retain uncertainty: %+v", metric)
			}
			want := "0"
			if offset > 0 {
				want = "1"
			}
			requireValue(t, metric, want, Provisional)
			if len(r.Interruptions) != 1 || !r.Interruptions[0].ObservedThrough.Equal(in.Incidents[0].AsOf) || !r.Interruptions[0].Open {
				t.Fatalf("open interruption must retain its last observation: %+v", r.Interruptions)
			}
		})
	}
}

func TestCurrentBoardPerformanceDoesNotInferContractualModuleRepairCounts(t *testing.T) {
	for _, mixed := range []bool{false, true} {
		t.Run(map[bool]string{false: "boards", true: "mixed_historical_units"}[mixed], func(t *testing.T) {
			p, in := calculationFixture()
			board := in.Coverage[0]
			board.Kind = 7
			if mixed {
				board.ID = "board"
				board.AllocationIntervalID = 2
				in.Coverage = append(in.Coverage, board)
			} else {
				in.Coverage[0] = board
			}
			in.Hashrates = []HashrateInterval{{ID: "board-performance", Kind: 7, AssetID: board.AssetID, SiteID: board.SiteID, ContainerID: board.ContainerID, OrderID: board.OrderID, Start: p.Start, End: p.End, HashrateHS: "100000000000000", Complete: true, SourceID: "hash", SourceRef: "reviewed board telemetry"}}
			r := calculate(t, p, in)
			if !mixed {
				requireValue(t, r.Metrics["hashrate_performance_hs"], "100000000000000", Authoritative)
			}
			for _, key := range []string{"backlog_offline_month_end", "backlog_offline_daily_average", "offline_units"} {
				metric := r.Metrics[key]
				if !slices.Contains(metric.Causes, "module_coverage_incomplete") {
					t.Fatalf("%s lost mapping qualification: %+v", key, metric)
				}
				if mixed {
					requireValue(t, metric, "0", Provisional)
				} else if metric.Status != Unavailable || metric.Value != "" {
					t.Fatalf("%s inferred modules: %+v", key, metric)
				}
			}
			requireValue(t, r.Metrics["open_S3_items"], "0", Authoritative)
			requireValue(t, r.Metrics["ticket_history"], "0", Authoritative)

		})
	}
}

func TestCalculateRejectsSingleSiteExternalResultForMultisiteOrder(t *testing.T) {
	p, in := calculationFixture()
	other := in.Coverage[0]
	other.ID, other.AssetID, other.AllocationIntervalID, other.SiteID, other.ContainerID = "other-site", 2, 2, 2, 20
	in.Coverage = append(in.Coverage, other)
	in.ExternalResults = []ContractResult{{ID: "site-result", Metric: "monthly_uptime", SiteID: 1, OrderID: "a", Start: p.Start, End: p.End, Value: "99", Unit: "percent", SourceRef: "site-one-only", CalculationRef: "workbook"}}
	r := calculate(t, p, in)
	requireValue(t, r.Orders[0].Metrics["monthly_uptime"], "", Unavailable)
	// An explicitly attributed single-site order still accepts reviewed results.
	in.Coverage = in.Coverage[:1]
	r = calculate(t, p, in)
	requireValue(t, r.Orders[0].Metrics["monthly_uptime"], "99", Provisional)
}
