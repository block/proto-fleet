package reporting

import (
	"fmt"
	"slices"
	"testing"
	"time"
)

func mappedModuleFixture() (Period, Inputs) {
	p, in := calculationFixture()
	c := in.Coverage[0]
	in.Coverage = nil
	refs := []string{}
	for i := int64(1); i <= 3; i++ {
		b := c
		b.Kind = 7
		b.AssetID = 10 + i
		b.ID = fmt.Sprintf("board-%d", i)
		b.AllocationIntervalID = i
		in.Coverage = append(in.Coverage, b)
		refs = append(refs, b.ID)
	}
	in.ModuleCoverage = []ModuleCoverageSegment{{ID: "mapped-module", ModuleID: 1, SiteID: c.SiteID, ContainerID: c.ContainerID, OrderID: c.OrderID, Start: p.Start, End: p.End, BoardCoverageIDs: refs, WholeModule: true, SourceRef: "reviewed historical membership"}}
	in.Obligations = []ObligationInterval{{ID: "repair", EpisodeID: "episode", Kind: 6, AssetID: 1, SiteID: c.SiteID, ContainerID: c.ContainerID, OrderID: c.OrderID, Start: p.Start, AsOf: p.End, ReceivedAt: p.End, Qualified: true, Severity: "S3", TicketRef: "ticket", TicketAt: p.Start, Deadline: p.Start.Add(7 * 24 * time.Hour), SourceRef: "reviewed outage"}}
	return p, in
}

func TestModuleMappingsCountModulesOnceWithoutAddingPerformanceCapacity(t *testing.T) {
	p, in := mappedModuleFixture()
	other := in.Obligations[0]
	other.ID = "second-repair"
	other.EpisodeID = "second-episode"
	other.TicketRef = "other-ticket"
	in.Obligations = append(in.Obligations, other)
	for _, c := range in.Coverage {
		in.Hashrates = append(in.Hashrates, HashrateInterval{ID: c.ID, Kind: c.Kind, AssetID: c.AssetID, SiteID: c.SiteID, ContainerID: c.ContainerID, OrderID: c.OrderID, Start: p.Start, End: p.End, HashrateHS: "100000000000000", Complete: true, SourceID: "hash", SourceRef: "board performance"})
	}
	r := calculate(t, p, in)
	requireValue(t, r.Metrics["backlog_offline_month_end"], "1", Authoritative)
	requireValue(t, r.Metrics["backlog_offline_daily_average"], "1", Authoritative)
	requireValue(t, r.Metrics["open_S3_items"], "2", Authoritative)
	requireValue(t, r.Metrics["ticket_history"], "2", Authoritative)
	requireValue(t, r.Metrics["hashrate_performance_percent"], "100", Authoritative)
	// Both view breakdowns use the same module identity, not three board units.
	requireValue(t, r.Orders[0].Metrics["backlog_offline_month_end"], "1", Authoritative)
	requireValue(t, r.Containers[0].Metrics["backlog_offline_month_end"], "1", Authoritative)
}

func TestPartialModuleMappingPreservesIndependentTickets(t *testing.T) {
	p, in := mappedModuleFixture()
	in.Coverage = in.Coverage[:1]
	in.ModuleCoverage[0].BoardCoverageIDs = in.ModuleCoverage[0].BoardCoverageIDs[:1]
	in.ModuleCoverage[0].WholeModule = false
	r := calculate(t, p, in)
	if r.Metrics["offline_units"].Status != Unavailable {
		t.Fatal("partial association inferred a whole-module count")
	}
	requireValue(t, r.Metrics["open_S3_items"], "1", Authoritative)
	requireValue(t, r.Metrics["ticket_history"], "1", Authoritative)
	in.Obligations[0].AsOf = p.End.Add(-time.Hour)
	r = calculate(t, p, in)
	requireValue(t, r.Metrics["open_S3_items"], "0", Provisional)
	if !slices.Contains(r.Metrics["open_S3_items"].Causes, "open_ticket_observation_gap") {
		t.Fatal("stale ticket treated as complete")
	}
}

func TestModuleMappingQualifiesUnmappedPopulationAndWindow(t *testing.T) {
	for _, extraBoard := range []bool{false, true} {
		p, in := mappedModuleFixture()
		if extraBoard {
			c := in.Coverage[0]
			c.ID = "unmapped"
			c.AssetID = 99
			c.AllocationIntervalID = 99
			in.Coverage = append(in.Coverage, c)
		} else {
			in.ModuleCoverage[0].End = p.Start.Add(p.End.Sub(p.Start) / 2)
		}
		r := calculate(t, p, in)
		expected := "0"
		if extraBoard {
			expected = "1"
		}
		requireValue(t, r.Metrics["backlog_offline_month_end"], expected, Provisional)
		if !slices.Contains(r.Metrics["backlog_offline_month_end"].Causes, "module_coverage_incomplete") {
			t.Fatal("missing mapping qualification")
		}
		if r.Metrics["backlog_unknown_coverage_month_end"].Status != Unavailable {
			t.Fatal("unknown board population converted into a numeric module count")
		}
		requireValue(t, r.Metrics["ticket_history"], "1", Authoritative)
	}
}

func TestWholeModuleMappingDoesNotInferOutageFromBoardPerformance(t *testing.T) {
	p, in := mappedModuleFixture()
	in.Obligations = nil
	r := calculate(t, p, in)
	requireValue(t, r.Metrics["offline_units"], "0", Authoritative)
	in.Configuration.Registers = slices.DeleteFunc(in.Configuration.Registers, func(r RegisterCompleteness) bool { return r.Kind == "obligations" })
	r = calculate(t, p, in)
	if r.Metrics["offline_units"].Status != Unavailable {
		t.Fatal("missing register inferred zero outages")
	}
	if r.Metrics["ticket_history"].Status != Unavailable {
		t.Fatal("missing register inferred zero episodes")
	}
}

func TestUnmappedBoardsDoNotBorrowARepairUnitFromConventionalMiners(t *testing.T) {
	p, in := mappedModuleFixture()
	in.ModuleCoverage = nil
	in.Obligations = nil
	c := in.Coverage[0]
	c.ID = "conventional"
	c.Kind = 5
	c.AssetID = 100
	c.AllocationIntervalID = 100
	in.Coverage = append(in.Coverage, c)
	r := calculate(t, p, in)
	if r.Metrics["offline_units"].Status != Unavailable {
		t.Fatal("conventional miner supplied a whole-module repair basis")
	}
}

func TestPartialEpisodeDoesNotEraseSupportedZeroForOtherModules(t *testing.T) {
	p, in := mappedModuleFixture()
	c := in.Coverage[0]
	c.ID = "partial-board"
	c.AssetID = 100
	c.AllocationIntervalID = 100
	in.Coverage = append(in.Coverage, c)
	partial := in.ModuleCoverage[0]
	partial.ID = "partial-module"
	partial.ModuleID = 2
	partial.WholeModule = false
	partial.BoardCoverageIDs = []string{c.ID}
	in.ModuleCoverage = append(in.ModuleCoverage, partial)
	in.Obligations[0].AssetID = 2
	r := calculate(t, p, in)
	requireValue(t, r.Metrics["offline_units"], "0", Provisional)
	requireValue(t, r.Metrics["ticket_history"], "1", Authoritative)
	if r.Metrics["backlog_unknown_coverage_month_end"].Status != Unavailable {
		t.Fatal("unknown modules became zero")
	}
}
