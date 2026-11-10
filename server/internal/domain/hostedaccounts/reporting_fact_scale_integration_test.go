package hostedaccounts_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	pb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts/reporting"
	"github.com/stretchr/testify/require"
)

func TestReportingSiteBearingFactsBeyondFormerCeiling(t *testing.T) {
	f := setup(t)
	account := f.client(t, "Site-bearing scale", f.user, hostedaccounts.AllCapabilities)
	reportingBoundaryActors(t, f, account.AccountID)
	var site int64
	require.NoError(t, f.db.QueryRowContext(t.Context(), `INSERT INTO site(org_id,name,slug) VALUES($1,'Scale','scale') RETURNING id`, f.org).Scan(&site))
	device := f.miningDevice(t, pb.DeviceClass_DEVICE_CLASS_CONTAINER)
	_, err := f.db.ExecContext(t.Context(), `UPDATE device SET site_id=$1 WHERE id=$2`, site, device)
	require.NoError(t, err)
	config := reporting.Configuration{Timezone: "UTC", ReportingYearMonth: 1, ReportingYearDay: 1}
	at := time.Now().UTC().Add(-time.Hour).Truncate(time.Microsecond)
	order := reporting.Order{ID: "order", Name: "Original", Start: at, SourceRef: "signed"}
	m, err := f.svc.MutateReportingInputs(t.Context(), f.host, f.org, account.AccountID, 0, "initial", &config, []reporting.FactMutation{reportingUpsertForTest(t, order)})
	require.NoError(t, err)
	const total = 10201
	var slowest time.Duration
	for offset := 0; offset < total; offset += 200 {
		changes := []reporting.FactMutation{}
		for i := offset; i < min(offset+200, total); i++ {
			changes = append(changes, reportingUpsertForTest(t, reporting.FeeLine{ID: fmt.Sprintf("fee-%05d", i), SiteID: site, ContainerID: device, OrderID: order.ID, Start: at, End: at.Add(time.Minute), Amount: "1", Currency: "USD", SourceRef: strings.Repeat("s", 256)}))
		}
		began := time.Now()
		m, err = f.svc.MutateReportingInputs(t.Context(), f.host, f.org, account.AccountID, m.Revision, "site-bearing fees", nil, changes)
		slowest = max(slowest, time.Since(began))
		require.NoError(t, err, "fees before batch: %d", offset)
	}
	t.Logf("%d site-bearing fees: slowest mutation batch=%s", total, slowest)
	began := time.Now()
	m, err = f.svc.MutateReportingInputs(t.Context(), f.host, f.org, account.AccountID, m.Revision, "small unrelated edit", nil, []reporting.FactMutation{reportingUpsertForTest(t, reporting.Order{ID: "unrelated", Name: "Small", Start: at, SourceRef: "reviewed"})})
	require.NoError(t, err)
	t.Logf("unrelated edit=%s", time.Since(began))
	began = time.Now()
	m, err = f.svc.MutateReportingInputs(t.Context(), f.host, f.org, account.AccountID, m.Revision, "small delete", nil, []reporting.FactMutation{{Kind: reporting.FactFee, ID: "fee-00000", Delete: true}})
	require.NoError(t, err)
	t.Logf("single delete=%s", time.Since(began))
	order.Name = "Edited"
	began = time.Now()
	m, err = f.svc.MutateReportingInputs(t.Context(), f.host, f.org, account.AccountID, m.Revision, "edit all dependents", nil, []reporting.FactMutation{reportingUpsertForTest(t, order)})
	require.NoError(t, err)
	t.Logf("order edit validating %d dependents=%s", total-1, time.Since(began))
	require.EqualValues(t, total-1, m.RegisterCounts[reporting.FactFee])
	page, err := f.svc.ReportingFactPage(t.Context(), f.host, f.org, account.AccountID, reporting.FactFee, m.Revision, "", 200)
	require.NoError(t, err)
	require.Len(t, page.Facts, 200, "small facts should use the row budget")
}

func TestReportingHashrateDependentsAtScale(t *testing.T) {
	f := setup(t)
	account := f.client(t, "Hashrate dependents", f.user, hostedaccounts.AllCapabilities)
	reportingBoundaryActors(t, f, account.AccountID)
	input, _, _ := currentBoardReportingInputs(t, f, account.AccountID)
	at := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	input.Orders[0].Start = at
	input.Coverage[0].Start, input.Coverage[0].End = at, at.Add(365*24*time.Hour)
	_, err := f.db.ExecContext(t.Context(), `UPDATE hosted_account_asset_interval SET started_at=$1 WHERE id=$2`, at, input.Coverage[0].AllocationIntervalID)
	require.NoError(t, err)
	input.Hashrates, input.PoolObservations = nil, nil
	saved, err := saveReportingInputsForTest(f.svc, t.Context(), f.host, f.org, account.AccountID, 0, "historical allocation", input)
	require.NoError(t, err)
	m := hostedaccounts.ReportingInputsMetadata{Revision: saved.Revision}
	c := input.Coverage[0]
	const total = 10201
	step := c.End.Sub(c.Start) / total
	for offset := 0; offset < total; offset += 200 {
		changes := []reporting.FactMutation{}
		for i := offset; i < min(offset+200, total); i++ {
			start := c.Start.Add(time.Duration(i) * step)
			changes = append(changes, reportingUpsertForTest(t, reporting.HashrateInterval{ID: fmt.Sprintf("reading-%05d", i), Kind: c.Kind, AssetID: c.AssetID, SiteID: c.SiteID, ContainerID: c.ContainerID, OrderID: c.OrderID, Start: start, End: start.Add(step), HashrateHS: "100000000000000", SourceID: "fleet", SourceRef: "reviewed readings", Complete: true}))
		}
		m, err = f.svc.MutateReportingInputs(t.Context(), f.host, f.org, account.AccountID, m.Revision, "legal hashrate batch", nil, changes)
		require.NoError(t, err, "readings before batch: %d", offset)
	}
	order := input.Orders[0]
	changes := []reporting.FactMutation{reportingUpsertForTest(t, order)}
	order.Name = "Edited name"
	changes = append(changes, reportingUpsertForTest(t, order))
	c.SourceRef = "corrected source evidence"
	changes = append(changes, reportingUpsertForTest(t, c))
	c.End = c.End.Add(time.Second)
	changes = append(changes, reportingUpsertForTest(t, c))
	for i, change := range changes {
		began := time.Now()
		m, err = f.svc.MutateReportingInputs(t.Context(), f.host, f.org, account.AccountID, m.Revision, "edit with retained dependents", nil, []reporting.FactMutation{change})
		require.NoError(t, err, "edit %d", i)
		t.Logf("edit %d with %d hashrate dependents=%s", i, total, time.Since(began))
		require.EqualValues(t, total, m.RegisterCounts[reporting.FactHashrate])
	}
	// Revalidation must reach the final dependent and reject an exact-time gap.
	c.End = c.Start.Add(total*step - time.Nanosecond)
	_, err = f.svc.MutateReportingInputs(t.Context(), f.host, f.org, account.AccountID, m.Revision, "truncate final reading", nil, []reporting.FactMutation{reportingUpsertForTest(t, c)})
	require.ErrorContains(t, err, `hashrate "reading-10200"`)
	require.ErrorContains(t, err, "commercial coverage")
	current, err := f.svc.ReportingInputsMetadata(t.Context(), f.host, f.org, account.AccountID)
	require.NoError(t, err)
	require.Equal(t, m.Revision, current.Revision)
}

func TestReportingModuleFactsAtScale(t *testing.T) {
	f := setup(t)
	account := f.client(t, "Module scale", f.user, hostedaccounts.AllCapabilities)
	reportingBoundaryActors(t, f, account.AccountID)
	input := reviewedModuleInputs(t, f, account.AccountID)
	base := input.ModuleCoverage[0]
	const total = 2000
	start := base.Start.Add(time.Microsecond)
	step := base.End.Sub(start) / total
	require.Positive(t, step)
	for i := range input.Coverage {
		input.Coverage[i].Start = start
		input.Coverage[i].End = base.End
	}
	input.ModuleCoverage, input.Obligations = nil, nil
	saved, err := saveReportingInputsForTest(f.svc, t.Context(), f.host, f.org, account.AccountID, 0, "board support", input)
	require.NoError(t, err)
	revision := saved.Revision
	for offset := 0; offset < total; offset += 200 {
		changes := []reporting.FactMutation{}
		for i := offset; i < min(offset+200, total); i++ {
			m := base
			m.ID = fmt.Sprintf("mapped-%05d", i)
			m.Start = start.Add(time.Duration(i) * step)
			m.End = m.Start.Add(step)
			changes = append(changes, reportingUpsertForTest(t, m))
		}
		began := time.Now()
		m, err := f.svc.MutateReportingInputs(t.Context(), f.host, f.org, account.AccountID, revision, "valid module mappings", nil, changes)
		t.Logf("module mappings before=%d add=%d elapsed=%s", offset, len(changes), time.Since(began))
		require.NoError(t, err)
		revision = m.Revision
	}
	began := time.Now()
	m, err := f.svc.MutateReportingInputs(t.Context(), f.host, f.org, account.AccountID, revision, "unrelated edit at scale", nil, []reporting.FactMutation{reportingUpsertForTest(t, reporting.Order{ID: "new-order", Name: "Unrelated", Start: base.Start, SourceRef: "source"})})
	require.NoError(t, err)
	t.Logf("%d module mappings: unrelated edit=%s", total, time.Since(began))
	began = time.Now()
	m, err = f.svc.MutateReportingInputs(t.Context(), f.host, f.org, account.AccountID, m.Revision, "delete at scale", nil, []reporting.FactMutation{{Kind: reporting.FactModuleCoverage, ID: "mapped-00000", Delete: true}})
	require.NoError(t, err)
	t.Logf("single module delete=%s", time.Since(began))
	require.EqualValues(t, total-1, m.RegisterCounts[reporting.FactModuleCoverage])
	// Full witness validation must still reach the last page after batching.
	_, err = f.db.ExecContext(t.Context(), `UPDATE hashboard_installation SET removed_at=$1 WHERE module_id=$2 AND position=1`, start.Add((total-1)*step), base.ModuleID)
	require.NoError(t, err)
	_, err = f.svc.MutateReportingInputs(t.Context(), f.host, f.org, account.AccountID, m.Revision, "drift at scale", nil, []reporting.FactMutation{reportingUpsertForTest(t, reporting.Order{ID: "new-order", Name: "Changed", Start: base.Start, SourceRef: "source"})})
	require.ErrorContains(t, err, "unambiguous historical board installations")
}
