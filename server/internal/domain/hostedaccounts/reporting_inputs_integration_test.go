package hostedaccounts_test

import (
	"testing"
	"time"

	"github.com/lib/pq"

	pb "github.com/block/proto-fleet/server/generated/grpc/hosted/v1"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts/reporting"
	"github.com/stretchr/testify/require"
)

func TestReportingAssetKindsMatchMiningContract(t *testing.T) {
	require.Equal(t, int32(pb.MiningAssetKind_MINING_ASSET_KIND_MINER), reporting.AssetKindMiner)
	require.Equal(t, int32(pb.MiningAssetKind_MINING_ASSET_KIND_MODULE), reporting.AssetKindModule)
	require.Equal(t, int32(pb.MiningAssetKind_MINING_ASSET_KIND_HASHBOARD), reporting.AssetKindHashboard)
}

func reportingInputs(t *testing.T, f fixture, account string, actor int64) reporting.Inputs {
	t.Helper()
	return reporting.Inputs{Configuration: reporting.Configuration{Timezone: "UTC", ReportingYearMonth: 1, ReportingYearDay: 1}}
}

func TestReportingInputsSaveImmediatelyWithCreatePermission(t *testing.T) {
	f := setup(t)
	account := f.client(t, "Reporting client", f.user, hostedaccounts.AllCapabilities)
	input := reportingInputs(t, f, account.AccountID, f.host)
	_, err := saveReportingInputsForTest(f.svc, t.Context(), f.host, f.org, account.AccountID, 0, "signed inputs", input)
	require.Error(t, err, "administration alone does not grant report creation")
	var actor string
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT user_id FROM "user" WHERE id=$1`, f.host).Scan(&actor))
	require.NoError(t, f.svc.SetReportingAuthority(t.Context(), f.host, f.org, account.AccountID, hostedaccounts.ReportingAuthority{UserID: actor, CanCreate: true, AllSites: true}, 0))
	saved, err := saveReportingInputsForTest(f.svc, t.Context(), f.host, f.org, account.AccountID, 0, "signed inputs", input)
	require.NoError(t, err)
	require.EqualValues(t, 1, saved.Revision)
	require.Equal(t, f.host, saved.UpdatedBy)
	current, err := reportingInputsForTest(f.svc, t.Context(), f.host, f.org, account.AccountID)
	require.NoError(t, err)
	require.Equal(t, saved.Revision, current.Revision)
	require.Equal(t, "UTC", current.Inputs.Configuration.Timezone)
	setup, err := f.svc.HostReportingSetup(t.Context(), f.host, f.org, account.AccountID)
	require.NoError(t, err)
	require.NotNil(t, setup.Input, "the saved revision is immediately available without another actor")
	_, err = saveReportingInputsForTest(f.svc, t.Context(), f.host, f.org, account.AccountID, 0, "stale edit", input)
	require.Error(t, err, "stale revisions cannot overwrite saved history")
}

func TestReportingInputAuditRollbackAndSingleCurrentRecord(t *testing.T) {
	f := setup(t)
	client := f.client(t, "Reporting client", f.user, hostedaccounts.AllCapabilities)
	var hostUUID string
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT user_id FROM "user" WHERE id=$1`, f.host).Scan(&hostUUID))
	require.NoError(t, f.svc.SetReportingAuthority(t.Context(), f.host, f.org, client.AccountID, hostedaccounts.ReportingAuthority{UserID: hostUUID, CanCreate: true, AllSites: true}, 0))
	_, err := f.db.ExecContext(t.Context(), `CREATE FUNCTION fail_reporting_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.event_type='save_reporting_inputs' THEN RAISE EXCEPTION 'reporting audit failed'; END IF; RETURN NEW; END $$; CREATE TRIGGER reporting_audit_failure BEFORE INSERT ON activity_log FOR EACH ROW EXECUTE FUNCTION fail_reporting_audit()`)
	require.NoError(t, err)
	_, err = saveReportingInputsForTest(f.svc, t.Context(), f.host, f.org, client.AccountID, 0, "source", reportingInputs(t, f, client.AccountID, f.host))
	require.Error(t, err)
	var count int
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM hosted_reporting_inputs`).Scan(&count))
	require.Zero(t, count)
	_, err = f.db.ExecContext(t.Context(), `DROP TRIGGER reporting_audit_failure ON activity_log`)
	require.NoError(t, err)
	revision, err := saveReportingInputsForTest(f.svc, t.Context(), f.host, f.org, client.AccountID, 0, "source", reportingInputs(t, f, client.AccountID, f.host))
	require.NoError(t, err)
	require.EqualValues(t, 1, revision.Revision)
	input := reportingInputs(t, f, client.AccountID, f.host)
	input.Configuration.DeliveryChannel = "updated channel"
	updated, err := saveReportingInputsForTest(f.svc, t.Context(), f.host, f.org, client.AccountID, revision.Revision, "updated source", input)
	require.NoError(t, err)
	require.EqualValues(t, 2, updated.Revision)
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM hosted_reporting_inputs`).Scan(&count))
	require.Equal(t, 1, count, "saving replaces the current record instead of appending history")
	_, err = f.db.ExecContext(t.Context(), `CREATE TRIGGER reporting_audit_failure BEFORE INSERT ON activity_log FOR EACH ROW EXECUTE FUNCTION fail_reporting_audit()`)
	require.NoError(t, err)
	input.Configuration.DeliveryChannel = "must roll back"
	_, err = saveReportingInputsForTest(f.svc, t.Context(), f.host, f.org, client.AccountID, 2, "failed update", input)
	require.Error(t, err)
	current, err := reportingInputsForTest(f.svc, t.Context(), f.host, f.org, client.AccountID)
	require.NoError(t, err)
	require.EqualValues(t, 2, current.Revision)
	require.Equal(t, "updated channel", current.Inputs.Configuration.DeliveryChannel)
	_, err = f.db.ExecContext(t.Context(), `DROP TRIGGER reporting_audit_failure ON activity_log`)
	require.NoError(t, err)

	_, err = saveReportingInputsForTest(f.svc, t.Context(), f.host, f.org, client.AccountID, revision.Revision, "stale source", input)
	require.Error(t, err)

}

func TestReportingCoverageUsesHistoricalAccountAndContainerIdentity(t *testing.T) {
	f := setup(t)
	client := f.client(t, "Reporting client", f.user, hostedaccounts.AllCapabilities)
	var hostUUID string
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT user_id FROM "user" WHERE id=$1`, f.host).Scan(&hostUUID))
	require.NoError(t, f.svc.SetReportingAuthority(t.Context(), f.host, f.org, client.AccountID, hostedaccounts.ReportingAuthority{UserID: hostUUID, CanCreate: true, AllSites: true}, 0))
	module, controller := f.miningModule(t)
	_, other := f.miningModule(t)
	var site int64
	require.NoError(t, f.db.QueryRowContext(t.Context(), `INSERT INTO site(org_id,name,slug) VALUES($1,'Report site','report-site') RETURNING id`, f.org).Scan(&site))
	_, err := f.db.ExecContext(t.Context(), `UPDATE device SET site_id=$1 WHERE id=ANY($2::bigint[])`, site, pq.Array([]int64{controller, other}))
	require.NoError(t, err)
	f.observeModuleAtCurrentSite(t, module, controller)
	require.NoError(t, f.allocate(t, f.host, f.root, client.AccountID, 1, selected(6, module, 0)))
	f.legacyModuleHistory(t)
	_, err = f.db.ExecContext(t.Context(), `UPDATE hosted_account_asset_interval SET started_at=now()-interval '48 hours',ended_at=now()-interval '24 hours' WHERE kind=6 AND asset_id=$1`, module)
	require.NoError(t, err)
	var id int64
	var start, end time.Time
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT id,started_at,ended_at FROM hosted_account_asset_interval WHERE kind=6 AND asset_id=$1`, module).Scan(&id, &start, &end))
	input := reportingInputs(t, f, client.AccountID, f.host)
	input.Orders = []reporting.Order{{ID: "order", Name: "Signed order", Start: start, SourceRef: "order"}}
	input.Coverage = []reporting.CoverageSegment{{ID: "scope", AllocationIntervalID: id, Kind: 6, AssetID: module, SiteID: site, ContainerID: other, OrderID: "order", Start: start, End: end, CapacityTHS: "100", SourceRef: "commissioning"}}
	_, err = saveReportingInputsForTest(f.svc, t.Context(), f.host, f.org, client.AccountID, 0, "source", input)
	require.Error(t, err, "a different controller is not this module's historical container")
	input.Coverage[0].ContainerID = controller
	_, err = saveReportingInputsForTest(f.svc, t.Context(), f.host, f.org, client.AccountID, 0, "source", input)
	require.NoError(t, err)
	input.Coverage[0].Start = start.Add(-time.Hour)
	input.Orders[0].Start = input.Coverage[0].Start
	_, err = saveReportingInputsForTest(f.svc, t.Context(), f.host, f.org, client.AccountID, 1, "source", input)
	require.Error(t, err, "current allocations cannot establish past ownership")
}

func TestReportingHostWorkspaceDoesNotGrantPreviewOrUnassignedInputAccess(t *testing.T) {
	f := setup(t)
	client := f.client(t, "Reporting client", f.user, hostedaccounts.AllCapabilities)
	workspace, err := f.svc.HostReportingSetup(t.Context(), f.host, f.org, client.AccountID)
	require.NoError(t, err)
	require.True(t, workspace.CanAdminister)
	require.False(t, workspace.CanCreate)
	require.False(t, workspace.CanCreate)
	require.Nil(t, workspace.Input)
	scoped, err := hostedaccounts.WithRequestAccountID(t.Context(), client.AccountID)
	require.NoError(t, err)
	_, err = f.svc.HostReportingSetup(scoped, f.host, f.org, client.AccountID)
	require.Error(t, err, "client preview cannot become a host mutation workspace")
	accounts, err := f.svc.HostReportingAccounts(t.Context(), f.user, f.org)
	require.Error(t, err, "discovery requires live host permissions")
	require.Empty(t, accounts, "client membership is not host reporting authority")
}
