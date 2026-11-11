package hostedaccounts_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts/reporting"
	"github.com/stretchr/testify/require"
)

func reportingUpsertForTest(t *testing.T, value any) reporting.FactMutation {
	t.Helper()
	f, e := reporting.EncodeFact(value)
	require.NoError(t, e)
	return reporting.FactMutation{Kind: f.Kind, ID: f.ID, Payload: f.Payload}
}

func TestReportingFactMutationsReverseDependenciesAndAtomicRollback(t *testing.T) {
	f := setup(t)
	account := f.client(t, "Dependencies", f.user, hostedaccounts.AllCapabilities)
	reportingBoundaryActors(t, f, account.AccountID)
	input, _, _ := currentBoardReportingInputs(t, f, account.AccountID)
	saved, e := saveReportingInputsForTest(f.svc, t.Context(), f.host, f.org, account.AccountID, 0, "initial", input)
	require.NoError(t, e)
	assertUnchanged := func() {
		t.Helper()
		m, e := f.svc.ReportingInputsMetadata(t.Context(), f.host, f.org, account.AccountID)
		require.NoError(t, e)
		require.Equal(t, saved.Revision, m.Revision)
		require.Equal(t, saved.RegisterCounts, m.RegisterCounts)
		var audits int64
		require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM activity_log WHERE event_type='save_reporting_inputs'`).Scan(&audits))
		require.Equal(t, saved.Revision, audits)
	}
	for i, change := range []reporting.FactMutation{
		{Kind: reporting.FactOrder, ID: input.Orders[0].ID, Delete: true},
		reportingUpsertForTest(t, func() reporting.Order {
			o := input.Orders[0]
			o.End = input.Coverage[0].Start.Add(time.Nanosecond)
			return o
		}()),
		{Kind: reporting.FactCoverage, ID: input.Coverage[0].ID, Delete: true},
		reportingUpsertForTest(t, func() reporting.CoverageSegment {
			c := input.Coverage[0]
			c.End = c.Start.Add(time.Nanosecond)
			return c
		}()),
	} {
		_, e = f.svc.MutateReportingInputs(t.Context(), f.host, f.org, account.AccountID, saved.Revision, "invalid dependency", nil, []reporting.FactMutation{change})
		want := []string{`coverage "board-coverage": order "order" is unavailable`, "attribution exceeds its hosting order", `hashrate "board-hash"`, "commercial coverage"}
		require.ErrorContains(t, e, want[i])
		assertUnchanged()
	}
	// A related correction is judged as one final state, regardless of mutation order.
	deletes := []reporting.FactMutation{{Kind: reporting.FactOrder, ID: input.Orders[0].ID, Delete: true}}
	facts, e := reportingFixtureFacts(input)
	require.NoError(t, e)
	for _, fact := range facts {
		if fact.Kind != reporting.FactOrder {
			deletes = append(deletes, reporting.FactMutation{Kind: fact.Kind, ID: fact.ID, Delete: true})
		}
	}
	final, e := f.svc.MutateReportingInputs(t.Context(), f.host, f.org, account.AccountID, saved.Revision, "remove complete evidence", nil, deletes)
	require.NoError(t, e)
	require.Empty(t, final.SiteIDs)
	require.Empty(t, final.RegisterCounts)
}

func TestReportingFactMutationsSiteCountsAndAuditFailure(t *testing.T) {
	f := setup(t)
	account := f.client(t, "Scope counts", f.user, hostedaccounts.AllCapabilities)
	reportingBoundaryActors(t, f, account.AccountID)
	var a, b int64
	require.NoError(t, f.db.QueryRowContext(t.Context(), `INSERT INTO site(org_id,name,slug) VALUES($1,'A','a') RETURNING id`, f.org).Scan(&a))
	require.NoError(t, f.db.QueryRowContext(t.Context(), `INSERT INTO site(org_id,name,slug) VALUES($1,'B','b') RETURNING id`, f.org).Scan(&b))
	input := siteOnlyReportingInputs(t, f, account.AccountID, a)
	m, e := saveReportingInputsForTest(f.svc, t.Context(), f.host, f.org, account.AccountID, 0, "first", input)
	require.NoError(t, e)
	v := input.ExternalResults[0]
	v.SiteID = b
	next, e := f.svc.MutateReportingInputs(t.Context(), f.host, f.org, account.AccountID, m.Revision, "move", nil, []reporting.FactMutation{reportingUpsertForTest(t, v)})
	require.NoError(t, e)
	require.Equal(t, []int64{b}, next.SiteIDs)
	// A replace within one site has zero net count and cannot remove its scope row.
	replacement := v
	replacement.ID = "replacement"
	next, e = f.svc.MutateReportingInputs(t.Context(), f.host, f.org, account.AccountID, next.Revision, "replace key", nil, []reporting.FactMutation{{Kind: reporting.FactExternalResult, ID: v.ID, Delete: true}, reportingUpsertForTest(t, replacement)})
	require.NoError(t, e)
	require.Equal(t, []int64{b}, next.SiteIDs)
	_, e = f.db.ExecContext(t.Context(), `CREATE FUNCTION reject_fact_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.event_type='save_reporting_inputs' THEN RAISE EXCEPTION 'fact audit rejected'; END IF; RETURN NEW; END $$; CREATE TRIGGER reject_fact_audit BEFORE INSERT ON activity_log FOR EACH ROW EXECUTE FUNCTION reject_fact_audit()`)
	require.NoError(t, e)
	replacement.SiteID = a
	_, e = f.svc.MutateReportingInputs(t.Context(), f.host, f.org, account.AccountID, next.Revision, "rollback scope", nil, []reporting.FactMutation{reportingUpsertForTest(t, replacement)})
	require.ErrorContains(t, e, "fact audit rejected")
	current, e := f.svc.ReportingInputsMetadata(t.Context(), f.host, f.org, account.AccountID)
	require.NoError(t, e)
	require.Equal(t, next.Revision, current.Revision)
	require.Equal(t, []int64{b}, current.SiteIDs)
	var site, count int64
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT site_id,fact_count FROM hosted_reporting_fact_site WHERE account_id=$1`, account.ID).Scan(&site, &count))
	require.Equal(t, b, site)
	require.EqualValues(t, 1, count)
	_, e = f.db.ExecContext(t.Context(), `DROP TRIGGER reject_fact_audit ON activity_log`)
	require.NoError(t, e)
	final, e := f.svc.MutateReportingInputs(t.Context(), f.host, f.org, account.AccountID, next.Revision, "delete last", nil, []reporting.FactMutation{{Kind: reporting.FactExternalResult, ID: replacement.ID, Delete: true}})
	require.NoError(t, e)
	require.Empty(t, final.SiteIDs)
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM hosted_reporting_fact_site WHERE account_id=$1`, account.ID).Scan(&count))
	require.Zero(t, count)
}

func TestReportingFactMutationsConcurrentInitializationAndUpdates(t *testing.T) {
	f := setup(t)
	account := f.client(t, "Concurrent", f.user, hostedaccounts.AllCapabilities)
	reportingBoundaryActors(t, f, account.AccountID)
	config := reporting.Configuration{Timezone: "UTC", ReportingYearMonth: 1, ReportingYearDay: 1}
	for revision := range int64(2) {
		start := make(chan struct{})
		results := make(chan error, 2)
		var wg sync.WaitGroup
		for range 2 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				_, e := f.svc.MutateReportingInputs(t.Context(), f.host, f.org, account.AccountID, revision, "race", &config, nil)
				results <- e
			}()
		}
		close(start)
		wg.Wait()
		close(results)
		success, conflict := 0, 0
		for e := range results {
			if e == nil {
				success++
			} else {
				require.ErrorContains(t, e, "refresh")
				conflict++
			}
		}
		require.Equal(t, 1, success)
		require.Equal(t, 1, conflict)
	}
}

func TestReportingFactMutationsAccumulateBeyondFormerCountAndByteCeilings(t *testing.T) {
	f := setup(t)
	account := f.client(t, "Lifetime evidence", f.user, hostedaccounts.AllCapabilities)
	reportingBoundaryActors(t, f, account.AccountID)
	config := reporting.Configuration{Timezone: "UTC", ReportingYearMonth: 1, ReportingYearDay: 1}
	revision := int64(0)
	at := time.Date(2026, 1, 1, 0, 0, 0, 123, time.UTC)
	const total = 10201
	for offset := 0; offset < total; offset += 200 {
		changes := []reporting.FactMutation{}
		for i := offset; i < min(offset+200, total); i++ {
			changes = append(changes, reportingUpsertForTest(t, reporting.Order{ID: fmt.Sprintf("order-%05d", i), Name: strings.Repeat("n", 120), SourceRef: strings.Repeat("s", 1000), Start: at}))
		}
		wire, e := json.Marshal(changes)
		require.NoError(t, e)
		require.LessOrEqual(t, len(wire), reporting.MaxFactMutationBytes)
		m, e := f.svc.MutateReportingInputs(t.Context(), f.host, f.org, account.AccountID, revision, "accumulate", &config, changes)
		require.NoError(t, e)
		revision = m.Revision
		require.EqualValues(t, min(offset+200, total), m.RegisterCounts[reporting.FactOrder])
	}
	var stored int64
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT sum(payload_bytes) FROM hosted_reporting_fact WHERE account_id=$1`, account.ID).Scan(&stored))
	require.Greater(t, stored, int64(8<<20))
	count := 0
	cursor := ""
	last := ""
	for {
		page, e := f.svc.ReportingFactPage(t.Context(), f.host, f.org, account.AccountID, reporting.FactOrder, revision, cursor, 200)
		require.NoError(t, e)
		wire, e := json.Marshal(page)
		require.NoError(t, e)
		require.LessOrEqual(t, len(wire), hostedaccounts.MaxReportingFactPageBytes)
		require.LessOrEqual(t, len(page.Facts), hostedaccounts.MaxReportingFactPageSize)
		for _, fact := range page.Facts {
			require.Greater(t, fact.ID, last)
			last = fact.ID
			count++
		}
		if page.NextCursor == "" {
			break
		}
		require.NotEqual(t, cursor, page.NextCursor)
		cursor = page.NextCursor
	}
	require.Equal(t, total, count)
	m, e := f.svc.MutateReportingInputs(t.Context(), f.host, f.org, account.AccountID, revision, "small edit after growth", nil, []reporting.FactMutation{reportingUpsertForTest(t, reporting.Order{ID: "order-00000", Name: "changed", SourceRef: "review", Start: at})})
	require.NoError(t, e)
	require.EqualValues(t, total, m.RegisterCounts[reporting.FactOrder])
	_, e = f.svc.ReportingFactPage(t.Context(), f.host, f.org, account.AccountID, reporting.FactOrder, revision, "", 200)
	require.ErrorContains(t, e, "refresh")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, e = f.svc.ReportingFactPage(ctx, f.host, f.org, account.AccountID, reporting.FactOrder, m.Revision, "", 200)
	require.Error(t, e)
	t.Logf("accumulated %d facts and %d stored bytes through legal requests; counted every bounded page", total, stored)
}

func TestReportingFactMutationsExactCoverageUnionAcrossPages(t *testing.T) {
	f := setup(t)
	account := f.client(t, "Exact union", f.user, hostedaccounts.AllCapabilities)
	reportingBoundaryActors(t, f, account.AccountID)
	input, _, _ := currentBoardReportingInputs(t, f, account.AccountID)
	base := input.Coverage[0]
	input.Coverage = nil
	input.PoolObservations = nil
	// All timestamps round into the same PostgreSQL microsecond, and the support
	// stream crosses both row and conservative byte-prefix page boundaries.
	for i := range 221 {
		c := base
		c.ID = fmt.Sprintf("segment-%03d", 220-i)
		c.Start = base.Start.Add(time.Duration(i) * time.Nanosecond)
		c.End = c.Start.Add(time.Nanosecond)
		input.Coverage = append(input.Coverage, c)
	}
	input.Hashrates[0].Start = base.Start
	input.Hashrates[0].End = base.Start.Add(221 * time.Nanosecond)
	saved, e := saveReportingInputsForTest(f.svc, t.Context(), f.host, f.org, account.AccountID, 0, "adjacent exact segments", input)
	require.NoError(t, e)
	_, e = f.svc.MutateReportingInputs(t.Context(), f.host, f.org, account.AccountID, saved.Revision, "gap", nil, []reporting.FactMutation{{Kind: reporting.FactCoverage, ID: input.Coverage[100].ID, Delete: true}})
	require.ErrorContains(t, e, "commercial coverage")
	h := input.Hashrates[0]
	h.End = input.Coverage[100].Start
	next, e := f.svc.MutateReportingInputs(t.Context(), f.host, f.org, account.AccountID, saved.Revision, "related shortening", nil, []reporting.FactMutation{{Kind: reporting.FactCoverage, ID: input.Coverage[100].ID, Delete: true}, reportingUpsertForTest(t, h)})
	require.NoError(t, e)
	require.Equal(t, saved.Revision+1, next.Revision)
}

func TestReportingFactMutationsBoardAndMappingReverseDependencies(t *testing.T) {
	f := setup(t)
	account := f.client(t, "Mapped module", f.user, hostedaccounts.AllCapabilities)
	reportingBoundaryActors(t, f, account.AccountID)
	input := reviewedModuleInputs(t, f, account.AccountID)
	saved, e := saveReportingInputsForTest(f.svc, t.Context(), f.host, f.org, account.AccountID, 0, "mapped evidence", input)
	require.NoError(t, e)
	for i, change := range []reporting.FactMutation{{Kind: reporting.FactCoverage, ID: input.Coverage[0].ID, Delete: true}, {Kind: reporting.FactModuleCoverage, ID: input.ModuleCoverage[0].ID, Delete: true}} {
		_, e = f.svc.MutateReportingInputs(t.Context(), f.host, f.org, account.AccountID, saved.Revision, "remove support", nil, []reporting.FactMutation{change})
		want := []string{`module_coverage "module": coverage "board-coverage" is unavailable`, `obligation "episode"`}
		require.ErrorContains(t, e, want[i])
		metadata, e := f.svc.ReportingInputsMetadata(t.Context(), f.host, f.org, account.AccountID)
		require.NoError(t, e)
		require.Equal(t, saved.Revision, metadata.Revision)
	}
}

func TestReportingFactMutationsSharedMeterExactSharesAndConfigurationRemoval(t *testing.T) {
	f := setup(t)
	account := f.client(t, "Shared energy", f.user, hostedaccounts.AllCapabilities)
	reportingBoundaryActors(t, f, account.AccountID)
	input, _, _ := currentBoardReportingInputs(t, f, account.AccountID)
	c := input.Coverage[0]
	input.Coverage = nil
	input.Hashrates = nil
	input.PoolObservations = nil
	input.Configuration.SharedAllocationRef = "allocation approval"
	second := input.Orders[0]
	second.ID = "other-order"
	input.Orders = append(input.Orders, second)
	meter := reporting.MeterInterval{ID: "left", MeterID: "physical", SourceID: "meter", SiteID: c.SiteID, ContainerID: c.ContainerID, OrderID: c.OrderID, Start: c.Start, End: c.End, KWh: "1", Quality: "measured", SourceRef: "reading", AllocationFraction: "0.1"}
	right := meter
	right.ID = "right"
	right.OrderID = second.ID
	right.AllocationFraction = "0.9"
	input.Meters = []reporting.MeterInterval{meter, right}
	saved, e := saveReportingInputsForTest(f.svc, t.Context(), f.host, f.org, account.AccountID, 0, "exactly one", input)
	require.NoError(t, e)
	right.AllocationFraction = "0.9000000000000000000000000000000000000001"
	_, e = f.svc.MutateReportingInputs(t.Context(), f.host, f.org, account.AccountID, saved.Revision, "over one", nil, []reporting.FactMutation{reportingUpsertForTest(t, right)})
	require.ErrorContains(t, e, "exceeds one")
	right.AllocationFraction = "0.9"
	right.Start = right.Start.Add(time.Nanosecond)
	_, e = f.svc.MutateReportingInputs(t.Context(), f.host, f.org, account.AccountID, saved.Revision, "partial overlap", nil, []reporting.FactMutation{reportingUpsertForTest(t, right)})
	require.ErrorContains(t, e, "matching evidence")
	config := input.Configuration
	config.SharedAllocationRef = ""
	_, e = f.svc.MutateReportingInputs(t.Context(), f.host, f.org, account.AccountID, saved.Revision, "remove approval", &config, nil)
	require.ErrorContains(t, e, "allocation evidence")
	right.Start = meter.Start
	right.AllocationFraction = ""
	next, e := f.svc.MutateReportingInputs(t.Context(), f.host, f.org, account.AccountID, saved.Revision, "remove sharing atomically", &config, []reporting.FactMutation{{Kind: reporting.FactMeter, ID: meter.ID, Delete: true}, reportingUpsertForTest(t, right)})
	require.NoError(t, e)
	require.Equal(t, saved.Revision+1, next.Revision)
}

func TestReportingFactMutationsLegalEnvelopeBoundsAndNoPartialWrites(t *testing.T) {
	f := setup(t)
	account := f.client(t, "Request bounds", f.user, hostedaccounts.AllCapabilities)
	reportingBoundaryActors(t, f, account.AccountID)
	input, _, _ := currentBoardReportingInputs(t, f, account.AccountID)
	saved, e := saveReportingInputsForTest(f.svc, t.Context(), f.host, f.org, account.AccountID, 0, "initial", input)
	require.NoError(t, e)
	c := input.Coverage[0]
	pool := reporting.PoolObservation{ID: "large", Kind: c.Kind, AssetID: c.AssetID, SourceAt: c.Start, ReceivedAt: c.End, SourceID: "pool", SourceRef: "reviewed", State: "unknown"}
	change := reportingUpsertForTest(t, pool)
	base, e := json.Marshal([]reporting.FactMutation{change})
	require.NoError(t, e)
	pool.Reason = strings.Repeat("x", reporting.MaxFactMutationBytes-len(base))
	change = reportingUpsertForTest(t, pool)
	wire, e := json.Marshal([]reporting.FactMutation{change})
	require.NoError(t, e)
	require.Len(t, wire, reporting.MaxFactMutationBytes)
	m, e := f.svc.MutateReportingInputs(t.Context(), f.host, f.org, account.AccountID, saved.Revision, "maximum envelope", nil, []reporting.FactMutation{change})
	require.NoError(t, e)
	page, e := f.svc.ReportingFactPage(t.Context(), f.host, f.org, account.AccountID, reporting.FactPoolObservation, m.Revision, "", 200)
	require.NoError(t, e)
	require.NotEmpty(t, page.Facts)
	encoded, e := json.Marshal(page)
	require.NoError(t, e)
	require.LessOrEqual(t, len(encoded), hostedaccounts.MaxReportingFactPageBytes)
	pool.Reason += "x"
	_, e = f.svc.MutateReportingInputs(t.Context(), f.host, f.org, account.AccountID, m.Revision, "one byte over", nil, []reporting.FactMutation{reportingUpsertForTest(t, pool)})
	require.ErrorContains(t, e, "byte budget")
	tooMany := make([]reporting.FactMutation, 201)
	_, e = f.svc.MutateReportingInputs(t.Context(), f.host, f.org, account.AccountID, m.Revision, "too many", nil, tooMany)
	require.ErrorContains(t, e, "mutation count")
	_, e = f.svc.MutateReportingInputs(t.Context(), f.host, f.org, account.AccountID, m.Revision, "absent delete", nil, []reporting.FactMutation{{Kind: reporting.FactOrder, ID: "absent", Delete: true}})
	require.ErrorContains(t, e, "absent")
	_, e = f.svc.MutateReportingInputs(t.Context(), f.host, f.org, account.AccountID, m.Revision, "no operations", nil, nil)
	require.ErrorContains(t, e, "at least one")
	config := input.Configuration
	config.ContractRef = strings.Repeat("x", reporting.MaxReportingConfigurationBytes)
	_, e = f.svc.MutateReportingInputs(t.Context(), f.host, f.org, account.AccountID, m.Revision, "large config", &config, nil)
	require.ErrorContains(t, e, "configuration exceeds")
	current, e := f.svc.ReportingInputsMetadata(t.Context(), f.host, f.org, account.AccountID)
	require.NoError(t, e)
	require.Equal(t, m.Revision, current.Revision)
	require.Equal(t, m.RegisterCounts, current.RegisterCounts)
}

func TestReportingFactMutationsOpenCoverageRetainsLastNanoseconds(t *testing.T) {
	f := setup(t)
	account := f.client(t, "Open bounds", f.user, hostedaccounts.AllCapabilities)
	reportingBoundaryActors(t, f, account.AccountID)
	input, _, _ := currentBoardReportingInputs(t, f, account.AccountID)
	input.Hashrates = nil
	input.PoolObservations = nil
	c := input.Coverage[0]
	c.Start = time.Date(9999, 12, 31, 23, 59, 59, 999999900, time.UTC)
	c.End = time.Time{}
	input.Coverage = []reporting.CoverageSegment{c}
	saved, e := saveReportingInputsForTest(f.svc, t.Context(), f.host, f.org, account.AccountID, 0, "open final second", input)
	require.NoError(t, e)
	overlap := c
	overlap.ID = "overlap"
	overlap.Start = overlap.Start.Add(50 * time.Nanosecond)
	_, e = f.svc.MutateReportingInputs(t.Context(), f.host, f.org, account.AccountID, saved.Revision, "overlap at final nanoseconds", nil, []reporting.FactMutation{reportingUpsertForTest(t, overlap)})
	require.ErrorContains(t, e, "overlapping")
}

func TestReportingFactMutationsSharedMeterGroupCarriesAcrossPages(t *testing.T) {
	f := setup(t)
	account := f.client(t, "Paged shares", f.user, hostedaccounts.AllCapabilities)
	reportingBoundaryActors(t, f, account.AccountID)
	input, _, _ := currentBoardReportingInputs(t, f, account.AccountID)
	c := input.Coverage[0]
	input.Coverage = nil
	input.Hashrates = nil
	input.PoolObservations = nil
	input.Orders = nil
	input.Configuration.SharedAllocationRef = "approved"
	for i := range 71 {
		order := fmt.Sprintf("order-%03d", i)
		input.Orders = append(input.Orders, reporting.Order{ID: order, Name: order, Start: c.Start, SourceRef: "order"})
		input.Meters = append(input.Meters, reporting.MeterInterval{ID: fmt.Sprintf("share-%03d", i), MeterID: "physical", SourceID: "source", OrderID: order, SiteID: c.SiteID, ContainerID: c.ContainerID, Start: c.Start, End: c.End, KWh: "1", Quality: "measured", AllocationFraction: "0.01", SourceRef: "reading"})
	}
	saved, e := saveReportingInputsForTest(f.svc, t.Context(), f.host, f.org, account.AccountID, 0, "paged exact shares", input)
	require.NoError(t, e)
	m := input.Meters[len(input.Meters)-1]
	m.AllocationFraction = "0.30"
	next, e := f.svc.MutateReportingInputs(t.Context(), f.host, f.org, account.AccountID, saved.Revision, "exactly one", nil, []reporting.FactMutation{reportingUpsertForTest(t, m)})
	require.NoError(t, e)
	m.AllocationFraction = "0.3000000000000000000000000000000000000001"
	_, e = f.svc.MutateReportingInputs(t.Context(), f.host, f.org, account.AccountID, next.Revision, "epsilon over one", nil, []reporting.FactMutation{reportingUpsertForTest(t, m)})
	require.ErrorContains(t, e, "exceeds one")
}

func TestReportingFactMutationCancellationAfterProvisionalInsertRollsBack(t *testing.T) {
	f := setup(t)
	account := f.client(t, "Cancel provisional", f.user, hostedaccounts.AllCapabilities)
	reportingBoundaryActors(t, f, account.AccountID)
	input := reportingInputs(t, f, account.AccountID, f.host)
	saved, e := saveReportingInputsForTest(f.svc, t.Context(), f.host, f.org, account.AccountID, 0, "initial", input)
	require.NoError(t, e)
	_, e = f.db.ExecContext(t.Context(), `CREATE FUNCTION gate_provisional_fact() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN PERFORM pg_advisory_xact_lock(19762,1); RETURN NEW; END $$; CREATE TRIGGER gate_provisional_fact AFTER INSERT ON hosted_reporting_fact FOR EACH ROW EXECUTE FUNCTION gate_provisional_fact()`)
	require.NoError(t, e)
	gate, e := f.db.BeginTx(t.Context(), nil)
	require.NoError(t, e)
	defer func() { _ = gate.Rollback() }()
	_, e = gate.ExecContext(t.Context(), `SELECT pg_advisory_xact_lock(19762,1)`)
	require.NoError(t, e)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	result := make(chan error, 1)
	go func() {
		_, e := f.svc.MutateReportingInputs(ctx, f.host, f.org, account.AccountID, saved.Revision, "cancel midway", nil, []reporting.FactMutation{reportingUpsertForTest(t, reporting.Order{ID: "cancelled", Name: "Cancelled", Start: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), SourceRef: "reviewed"})})
		result <- e
	}()
	require.Eventually(t, func() bool {
		var blocked bool
		e := f.db.QueryRowContext(t.Context(), `SELECT EXISTS(SELECT 1 FROM pg_locks WHERE locktype='advisory' AND classid=19762 AND objid=1 AND NOT granted AND database=(SELECT oid FROM pg_database WHERE datname=current_database()))`).Scan(&blocked)
		return e == nil && blocked
	}, 3*time.Second, 10*time.Millisecond)
	cancel()
	select {
	case e := <-result:
		require.Error(t, e)
	case <-time.After(3 * time.Second):
		t.Fatal("cancellation failed to stop provisional mutation")
	}
	require.NoError(t, gate.Commit())
	m, e := f.svc.ReportingInputsMetadata(t.Context(), f.host, f.org, account.AccountID)
	require.NoError(t, e)
	require.Equal(t, saved.Revision, m.Revision)
	require.Empty(t, m.RegisterCounts)
	var count int
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM hosted_reporting_fact`).Scan(&count))
	require.Zero(t, count)
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM activity_log WHERE event_type='save_reporting_inputs'`).Scan(&count))
	require.Equal(t, 1, count)
}

func TestReportingFactMutationAuditRecordsActualChanges(t *testing.T) {
	f := setup(t)
	account := f.client(t, "Actual changes", f.user, hostedaccounts.AllCapabilities)
	reportingBoundaryActors(t, f, account.AccountID)
	config := reporting.Configuration{Timezone: "UTC", ReportingYearMonth: 1, ReportingYearDay: 1}
	order := reporting.Order{ID: "order", Name: "Order", Start: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), SourceRef: "signed"}
	change := reportingUpsertForTest(t, order)
	initial, e := f.svc.MutateReportingInputs(t.Context(), f.host, f.org, account.AccountID, 0, "initial", &config, []reporting.FactMutation{change})
	require.NoError(t, e)
	config.Sources = []reporting.Source{}
	config.Representatives = []reporting.Representative{}
	config.Registers = []reporting.RegisterCompleteness{}
	config.Holidays = []string{}
	config.Prerequisites = []reporting.Prerequisite{}
	next, e := f.svc.MutateReportingInputs(t.Context(), f.host, f.org, account.AccountID, initial.Revision, "review unchanged", &config, []reporting.FactMutation{change})
	require.NoError(t, e)
	require.Equal(t, initial.Revision+1, next.Revision)
	var sections string
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT (metadata->'changed_sections')::text FROM activity_log WHERE event_type='save_reporting_inputs' ORDER BY id DESC LIMIT 1`).Scan(&sections))
	require.JSONEq(t, `[]`, sections)
	order.Name = "Updated"
	_, e = f.svc.MutateReportingInputs(t.Context(), f.host, f.org, account.AccountID, next.Revision, "edit name", nil, []reporting.FactMutation{reportingUpsertForTest(t, order)})
	require.NoError(t, e)
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT (metadata->'changed_sections')::text FROM activity_log WHERE event_type='save_reporting_inputs' ORDER BY id DESC LIMIT 1`).Scan(&sections))
	require.JSONEq(t, `["orders"]`, sections)
}
