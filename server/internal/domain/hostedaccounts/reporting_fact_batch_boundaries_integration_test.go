package hostedaccounts_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts/reporting"
	"github.com/stretchr/testify/require"
)

func TestReportingModuleWitnessBatchKeepsDistinctPhysicalModules(t *testing.T) {
	f := setup(t)
	account := f.client(t, "Distinct modules", f.user, hostedaccounts.AllCapabilities)
	reportingBoundaryActors(t, f, account.AccountID)
	input := reviewedModuleInputs(t, f, account.AccountID)
	other := reviewedModuleInputs(t, f, account.AccountID)
	for i := range other.Coverage {
		other.Coverage[i].ID = "other-" + other.Coverage[i].ID
	}
	other.ModuleCoverage[0].ID = "other-module"
	for i := range other.ModuleCoverage[0].BoardCoverageIDs {
		other.ModuleCoverage[0].BoardCoverageIDs[i] = "other-" + other.ModuleCoverage[0].BoardCoverageIDs[i]
	}
	input.Coverage = append(input.Coverage, other.Coverage...)
	input.ModuleCoverage = append(input.ModuleCoverage, other.ModuleCoverage...)
	input.Obligations = nil
	saved, err := saveReportingInputsForTest(f.svc, t.Context(), f.host, f.org, account.AccountID, 0, "distinct physical witnesses", input)
	require.NoError(t, err)
	require.EqualValues(t, 2, saved.RegisterCounts[reporting.FactModuleCoverage])
	last := other.ModuleCoverage[0]
	third, _ := f.miningModule(t)
	var unreferenced int64
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT hashboard_id FROM hashboard_installation WHERE module_id=$1 AND position=1 AND removed_at IS NULL`, third).Scan(&unreferenced))
	_, err = f.db.ExecContext(t.Context(), `INSERT INTO hashboard_installation(hashboard_id,module_id,position,site_id,device_id,tank_id,module_position,installed_at,last_seen_at,removed_at)
 SELECT $1,module_id,position,site_id,device_id,tank_id,module_position,$2,$3,$3 FROM hashboard_installation WHERE module_id=$4 AND position=1 AND removed_at IS NULL`, unreferenced, last.Start.Add(time.Microsecond), last.End, last.ModuleID)
	require.NoError(t, err)
	_, err = f.svc.MutateReportingInputs(t.Context(), f.host, f.org, account.AccountID, saved.Revision, "second module drift", nil, []reporting.FactMutation{reportingUpsertForTest(t, reporting.Order{ID: "unrelated", Name: "Unrelated", Start: last.Start, SourceRef: "signed"})})
	require.ErrorContains(t, err, `module coverage "other-module" requires unambiguous historical board installations`)
}

func TestReportingReferencePayloadPagesPreserveOrderDependencies(t *testing.T) {
	f := setup(t)
	account := f.client(t, "Reference payload pages", f.user, hostedaccounts.AllCapabilities)
	reportingBoundaryActors(t, f, account.AccountID)
	input, _, _ := currentBoardReportingInputs(t, f, account.AccountID)
	c := input.Coverage[0]
	input.Coverage, input.Hashrates, input.PoolObservations = nil, nil, nil
	input.Orders = nil
	for i := range 64 {
		id := fmt.Sprintf("order-%03d", i)
		input.Orders = append(input.Orders, reporting.Order{ID: id, Name: strings.Repeat("n", 2000), SourceRef: strings.Repeat("s", 2000), Start: c.Start})
		input.Fees = append(input.Fees, reporting.FeeLine{ID: fmt.Sprintf("fee-%03d", i), SiteID: c.SiteID, ContainerID: c.ContainerID, OrderID: id, Start: c.Start, End: c.End, Amount: "1", Currency: "USD", SourceRef: "invoice"})
	}
	saved, err := saveReportingInputsForTest(f.svc, t.Context(), f.host, f.org, account.AccountID, 0, "large distinct references", input)
	require.NoError(t, err)
	require.EqualValues(t, 64, saved.RegisterCounts[reporting.FactFee])
	last := input.Orders[len(input.Orders)-1]
	last.End = c.Start.Add(time.Nanosecond)
	changes := []reporting.FactMutation{reportingUpsertForTest(t, last)}
	for _, fee := range input.Fees {
		changes = append(changes, reportingUpsertForTest(t, fee))
	}
	// The failing request must also fetch all references across byte pages.
	_, err = f.svc.MutateReportingInputs(t.Context(), f.host, f.org, account.AccountID, saved.Revision, "truncate last referenced order", nil, changes)
	require.ErrorContains(t, err, "attribution exceeds its hosting order")
	current, err := f.svc.ReportingInputsMetadata(t.Context(), f.host, f.org, account.AccountID)
	require.NoError(t, err)
	require.Equal(t, saved.Revision, current.Revision)
}

func TestReportingCoverageBatchKeepsCommercialAndModuleSupportDistinct(t *testing.T) {
	f := setup(t)
	account := f.client(t, "Distinct coverage support", f.user, hostedaccounts.AllCapabilities)
	reportingBoundaryActors(t, f, account.AccountID)
	input := reviewedModuleInputs(t, f, account.AccountID)
	saved, err := saveReportingInputsForTest(f.svc, t.Context(), f.host, f.org, account.AccountID, 0, "module-supported obligation", input)
	require.NoError(t, err)
	m := input.ModuleCoverage[0]
	for _, dependent := range []any{
		reporting.HashrateInterval{ID: "module-hash", Kind: reporting.AssetKindModule, AssetID: m.ModuleID, SiteID: m.SiteID, ContainerID: m.ContainerID, OrderID: m.OrderID, Start: m.Start, End: m.End, HashrateHS: "1", SourceID: "fleet", SourceRef: "reviewed readings", Complete: true},
		reporting.PoolObservation{ID: "module-pool", Kind: reporting.AssetKindModule, AssetID: m.ModuleID, SourceAt: m.Start, ReceivedAt: m.End, State: "unknown", SourceID: "pool", SourceRef: "reviewed observation"},
	} {
		change := reportingUpsertForTest(t, dependent)
		_, err = f.svc.MutateReportingInputs(t.Context(), f.host, f.org, account.AccountID, saved.Revision, "mixed coverage requirements", nil, []reporting.FactMutation{reportingUpsertForTest(t, input.Obligations[0]), change})
		require.ErrorContains(t, err, fmt.Sprintf("%s %q", change.Kind, change.ID))
		require.ErrorContains(t, err, "commercial coverage")
		current, err := f.svc.ReportingInputsMetadata(t.Context(), f.host, f.org, account.AccountID)
		require.NoError(t, err)
		require.Equal(t, saved.Revision, current.Revision)
		require.Equal(t, saved.RegisterCounts, current.RegisterCounts)
	}
}
