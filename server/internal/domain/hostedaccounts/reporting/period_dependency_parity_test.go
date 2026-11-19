package reporting

import (
	"reflect"
	"slices"
	"testing"
	"time"
)

func TestPeriodDependencyEarlierEpisodeStart(t *testing.T) {
	p, in := calculationFixture()
	start := p.Start.AddDate(0, 0, -70)
	in.Orders[0].Start = start.AddDate(-1, 0, 0)
	in.Coverage[0].Start = start
	earlier := ObligationInterval{ID: "earlier", EpisodeID: "episode", Kind: 6, AssetID: 1, SiteID: 1, ContainerID: 10, OrderID: "a", Start: start, End: p.Start.AddDate(0, 0, -30), AsOf: p.AsOf, ReceivedAt: start, Qualified: true, SourceRef: "earlier episode"}
	current := earlier
	current.ID, current.Start, current.End, current.SourceRef = "current", p.Start, p.End, "current episode"
	late := earlier
	late.ID, late.Start, late.ReceivedAt, late.SourceRef = "late", start.AddDate(0, 0, -1), p.AsOf.Add(time.Nanosecond), "late evidence"
	in.Obligations = []ObligationInterval{earlier, current, late}

	full := calculate(t, p, in)
	if len(full.Obligations) != 1 || !full.Obligations[0].EpisodeStart.Equal(start) {
		t.Fatalf("true episode start or as-of handling lost: %+v", full.Obligations)
	}
	requireValue(t, full.Metrics["backlog_age_over_60_days_month_end"], "1", Authoritative)
	requireValue(t, full.Metrics["backlog_age_over_60_days_daily_average"], "1", Authoritative)

	narrowed := in
	narrowed.Obligations = []ObligationInterval{current}
	narrow := calculate(t, p, narrowed)
	if len(narrow.Obligations) != 1 || !narrow.Obligations[0].EpisodeStart.Equal(p.Start) {
		t.Fatalf("narrowed result did not expose missing episode context: %+v", narrow.Obligations)
	}
	requireValue(t, narrow.Metrics["backlog_age_over_60_days_month_end"], "0", Authoritative)
	requireValue(t, narrow.Metrics["backlog_age_over_60_days_daily_average"], "0", Authoritative)
}

func TestPeriodDependencyRestoredOpenS3(t *testing.T) {
	for _, tc := range []struct {
		name       string
		stale      bool
		wantValue  string
		wantStatus Status
	}{
		{"observed-through-period", false, "1", Authoritative},
		{"stale-observation", true, "0", Provisional},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, in := calculationFixture()
			start, restored := p.Start.AddDate(0, 0, -3), p.Start.AddDate(0, 0, -1)
			in.Coverage[0].Start = start
			observed := p.End
			if tc.stale {
				observed = restored
			}
			in.Obligations = []ObligationInterval{{ID: "restored", EpisodeID: "episode", Kind: 6, AssetID: 1, SiteID: 1, ContainerID: 10, OrderID: "a", Start: start, End: restored, AsOf: observed, ReceivedAt: start, Qualified: true, Severity: "S3", TicketRef: "ticket", TicketAt: start, RestoredAt: restored, SourceRef: "open ticket"}}

			full := calculate(t, p, in)
			requireValue(t, full.Metrics["open_S3_items"], tc.wantValue, tc.wantStatus)
			if tc.stale && !slices.Contains(full.Metrics["open_S3_items"].Causes, "open_ticket_observation_gap") {
				t.Fatalf("stale open ticket lost its observation gap: %+v", full.Metrics["open_S3_items"])
			}
			narrowed := in
			narrowed.Obligations = nil
			narrow := calculate(t, p, narrowed)
			requireValue(t, narrow.Metrics["open_S3_items"], "0", Authoritative)
			if reflect.DeepEqual(full.Metrics["open_S3_items"], narrow.Metrics["open_S3_items"]) {
				t.Fatal("omitting the pre-period ticket did not change its result")
			}
		})
	}
}

func TestPeriodDependencyOpenS3ChoosesLatestBeforeScope(t *testing.T) {
	p, in := calculationFixture()
	start := p.Start.AddDate(0, 0, -10)
	move := p.Start.AddDate(0, 0, -5)
	in.Coverage[0].Start, in.Coverage[0].End = start, move
	moved := in.Coverage[0]
	moved.ID, moved.AllocationIntervalID, moved.SiteID, moved.ContainerID, moved.Start, moved.End = "moved", 2, 2, 20, move, time.Time{}
	other := in.Coverage[0]
	other.ID, other.AllocationIntervalID, other.AssetID, other.Start, other.End = "other", 3, 2, p.Start, time.Time{}
	in.Coverage = append(in.Coverage, moved, other)
	earlier := ObligationInterval{ID: "earlier", EpisodeID: "episode", Kind: 6, AssetID: 1, SiteID: 1, ContainerID: 10, OrderID: "a", Start: start, End: move, AsOf: p.End, ReceivedAt: start, Qualified: true, Severity: "S3", TicketRef: "ticket", TicketAt: start, SourceRef: "old scope"}
	later := earlier
	later.ID, later.SiteID, later.ContainerID, later.Start, later.End, later.ClosedAt, later.SourceRef = "later", 2, 20, move, p.Start, p.Start, "new scope closed"
	in.Obligations = []ObligationInterval{earlier, later}

	full := calculate(t, p, in)
	if len(full.Containers) != 2 || full.Containers[0].SiteID != 1 {
		t.Fatalf("expected both site breakdowns: %+v", full.Containers)
	}
	requireValue(t, full.Containers[0].Metrics["open_S3_items"], "0", Authoritative)
	narrowed := in
	narrowed.Obligations = []ObligationInterval{earlier}
	narrow := calculate(t, p, narrowed)
	requireValue(t, narrow.Containers[0].Metrics["open_S3_items"], "1", Authoritative)
}

func TestPeriodDependencyPriorReportingYear(t *testing.T) {
	_, in := calculationFixture()
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 3, 9, 0, 0, 0, 0, loc)
	prior := start.AddDate(-1, 0, 0)
	p := Period{Kind: "annual", Start: start.UTC(), End: start.AddDate(1, 0, 0).UTC(), AsOf: start.AddDate(1, 0, 0).UTC(), Timezone: loc.String()}
	in.Configuration.Timezone = loc.String()
	in.Configuration.ReportingYearMonth, in.Configuration.ReportingYearDay = 3, 9
	for i := range in.Configuration.Registers {
		in.Configuration.Registers[i].Start, in.Configuration.Registers[i].End = prior, p.End
	}
	in.Configuration.Registers = append(in.Configuration.Registers, RegisterCompleteness{Kind: "incidents", Start: prior, End: p.End, SourceRef: "complete:incidents"})
	in.Orders[0].Start = prior
	previousCoverage := in.Coverage[0]
	previousCoverage.ID, previousCoverage.Start, previousCoverage.End = "prior", prior, p.Start
	in.Coverage[0].Start = p.Start
	in.Coverage = append([]CoverageSegment{previousCoverage}, in.Coverage...)
	for i, bounds := range [][2]time.Time{{prior, p.Start}, {p.Start, p.End}} {
		id := []string{"prior", "current"}[i]
		in.Meters = append(in.Meters, MeterInterval{ID: id, MeterID: "meter", SourceID: "meter", SiteID: 1, ContainerID: 10, OrderID: "a", Start: bounds[0], End: bounds[1], KWh: "100", Quality: "measured", SourceRef: "meter " + id})
		in.Hashrates = append(in.Hashrates, HashrateInterval{ID: id, Kind: 6, AssetID: 1, SiteID: 1, ContainerID: 10, OrderID: "a", SourceID: "hash", Start: bounds[0], End: bounds[1], HashrateHS: "50000000000000", Complete: true, SourceRef: "hash " + id})
		in.Incidents = append(in.Incidents, Incident{ID: id, SiteID: 1, ContainerID: 10, OrderID: "a", Start: bounds[0].Add(time.Hour), End: bounds[0].Add(2 * time.Hour), AsOf: p.AsOf, Category: "scheduled_maintenance", Summary: "maintenance", RootCause: "planned", SourceRef: "incident " + id})
	}

	full := calculate(t, p, in)
	for _, key := range []string{"year_over_year_energy", "year_over_year_hashrate", "year_over_year_maintenance"} {
		requireValue(t, full.Metrics[key], "0", Provisional)
	}
	withoutPriorMeter := in
	withoutPriorMeter.Meters = in.Meters[1:]
	requireValue(t, calculate(t, p, withoutPriorMeter).Metrics["year_over_year_energy"], "", Unavailable)
	withoutPriorHashrate := in
	withoutPriorHashrate.Hashrates = in.Hashrates[1:]
	requireValue(t, calculate(t, p, withoutPriorHashrate).Metrics["year_over_year_hashrate"], "", Unavailable)
	withoutPriorIncident := in
	withoutPriorIncident.Incidents = in.Incidents[1:]
	// A complete incident register makes a missing prior incident read as zero.
	requireValue(t, calculate(t, p, withoutPriorIncident).Metrics["year_over_year_maintenance"], "1", Provisional)

	narrowed := in
	narrowed.Coverage = in.Coverage[1:]
	narrowed.Meters = in.Meters[1:]
	narrowed.Hashrates = in.Hashrates[1:]
	narrowed.Incidents = in.Incidents[1:]
	narrow := calculate(t, p, narrowed)
	for _, key := range []string{"year_over_year_energy", "year_over_year_hashrate", "year_over_year_maintenance"} {
		requireValue(t, narrow.Metrics[key], "", Unavailable)
		if !slices.Contains(narrow.Metrics[key].Causes, "first_reporting_year_no_prior_evidence") {
			t.Fatalf("%s missing prior-year cause: %+v", key, narrow.Metrics[key])
		}
	}
}

func TestPeriodDependencyNextPeriodMeter(t *testing.T) {
	p, in := calculationFixture()
	in.Configuration.SharedAllocationRef = "host allocation worksheet"
	meter := MeterInterval{ID: "used", MeterID: "raw-meter", SourceID: "meter", SiteID: 1, ContainerID: 10, OrderID: "a", Start: p.Start, End: p.Start.Add(time.Hour), KWh: "100.000000000000000001", AllocationFraction: "0.25", Quality: "measured", SourceRef: "host original"}
	other := meter
	other.ID = "future"
	other.Start = p.End
	other.End = p.End.Add(time.Hour)
	in.Meters = []MeterInterval{meter, other}
	in.PoolObservations = []PoolObservation{{ID: "pool", Kind: 6, AssetID: 1, SourceID: "pool", SourceAt: p.Start, ReceivedAt: p.Start, State: "unknown", SourceRef: "pool evidence"}}

	full := calculate(t, p, in)
	narrowed := in
	narrowed.Meters = []MeterInterval{meter}
	if !reflect.DeepEqual(full, calculate(t, p, narrowed)) {
		t.Fatal("a meter interval in the next period changed the calculated result")
	}
	narrowed.Meters = []MeterInterval{other}
	if reflect.DeepEqual(full, calculate(t, p, narrowed)) {
		t.Fatal("omitting the meter interval in the period did not change the result")
	}
}

func TestPeriodDependencyEqualSourceTimePoolConflict(t *testing.T) {
	p, in := calculationFixture()
	in.Coverage[0].Start = p.Start.AddDate(0, 0, -1)
	at := p.Start.Add(-time.Hour)
	reporting := PoolObservation{ID: "a", Kind: 6, AssetID: 1, SourceID: "pool", SourceAt: at, ReceivedAt: p.End, State: "reporting", SourceRef: "pool reporting"}
	conflict := reporting
	conflict.ID, conflict.State, conflict.ReceivedAt, conflict.SourceRef = "b", "failed", p.AsOf, "pool failed"
	late := conflict
	late.ID, late.ReceivedAt, late.SourceRef = "late", p.AsOf.Add(time.Nanosecond), "pool late"
	atEnd := reporting
	atEnd.ID, atEnd.SourceAt, atEnd.ReceivedAt, atEnd.SourceRef = "at-end", p.End, p.End, "pool at end"
	in.PoolObservations = []PoolObservation{reporting, conflict, late, atEnd}

	full := calculate(t, p, in)
	requireValue(t, full.Metrics["pool_unknown_units"], "1", Provisional)
	requireValue(t, full.Metrics["pool_reporting_units"], "0", Provisional)
	if !slices.Contains(full.Metrics["pool_unknown_units"].Causes, "conflicting_pool_observations") {
		t.Fatalf("same-time conflict not reported: %+v", full.Metrics["pool_unknown_units"])
	}
	narrowed := in
	narrowed.PoolObservations = []PoolObservation{reporting}
	narrow := calculate(t, p, narrowed)
	requireValue(t, narrow.Metrics["pool_unknown_units"], "0", Provisional)
	requireValue(t, narrow.Metrics["pool_reporting_units"], "1", Provisional)
	if slices.Contains(narrow.Metrics["pool_unknown_units"].Causes, "conflicting_pool_observations") {
		t.Fatal("narrowed single observation unexpectedly conflicts")
	}
}
