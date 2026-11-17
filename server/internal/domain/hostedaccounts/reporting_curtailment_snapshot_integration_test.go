package hostedaccounts_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	"github.com/block/proto-fleet/server/internal/domain/curtailment/models"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts/reporting"
	"github.com/stretchr/testify/require"
)

func TestHostedCurtailmentCaptureUsesOneSnapshotForAtomicRestoration(t *testing.T) {
	f := setup(t)
	client := f.client(t, "Control snapshot client", f.user, hostedaccounts.AllCapabilities)
	owner, _ := reportActors(t, f, client.AccountID)
	device := f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG)
	controlSite(t, f, device)
	require.NoError(t, f.allocate(t, f.host, f.root, client.AccountID, 1, selected(5, device, 0)))
	c := newControlFixture(t, f, device)
	start := c.confirm(t, models.TargetPhaseCurtail, c.batch, true)
	addSnapshotControlConfirmations(t, f, 200)
	saved, err := saveReportingInputsForTest(f.svc, t.Context(), f.host, f.org, client.AccountID, 0, "Control configuration", reportingInputs(t, f, client.AccountID, f.host))
	require.NoError(t, err)
	request, err := f.svc.CreateReportRequest(t.Context(), f.host, f.org, client.AccountID, hostedaccounts.ReportRequest{ReceivedAt: start.Add(-time.Hour), SourceRef: "Client receipt", Requester: "Client", Scope: "Atomic control cohort", OwnerUserID: owner})
	require.NoError(t, err)

	var first int64
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT min(id) FROM hosted_curtailment_confirmation WHERE phase='curtail'`).Scan(&first))
	// A volatile view projection pauses the first control SELECT after its MVCC
	// snapshot exists. It only acquires a lock and returns the unchanged site;
	// it never reads data using a separate snapshot or adds a production hook.
	_, err = f.db.ExecContext(t.Context(), fmt.Sprintf(`
 ALTER TABLE hosted_curtailment_asset RENAME TO snapshot_curtailment_asset;
 CREATE FUNCTION snapshot_control_site(confirmation bigint, site bigint) RETURNS bigint LANGUAGE plpgsql VOLATILE AS $$
 BEGIN
  IF confirmation=%d THEN PERFORM pg_advisory_xact_lock(10490,1); END IF;
  RETURN site;
 END $$;
 CREATE VIEW hosted_curtailment_asset AS
 SELECT confirmation_id,interval_id,installation_id,organization_id,client_id,subclient_id,
 snapshot_control_site(confirmation_id,site_id) AS site_id,kind,asset_id FROM snapshot_curtailment_asset`, first))
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	gate, err := f.db.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer gate.Rollback()
	_, err = gate.ExecContext(ctx, `SELECT pg_advisory_xact_lock(10490,1)`)
	require.NoError(t, err)
	restore, err := f.db.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer restore.Rollback()
	// Cancel blocked report queries before transaction cleanup on assertion failure.
	defer cancel()
	// All 201 restores belong to one uncommitted transaction and were captured
	// before the report as-of. Their visibility must remain atomic in the report.
	_, err = restore.ExecContext(ctx, `INSERT INTO hosted_curtailment_confirmation(organization_id,event_id,device_id,curtail_batch,phase,phase_batch,sample_at,confirmed_at)
 SELECT organization_id,event_id,device_id,curtail_batch,'restore','restore-'||curtail_batch,clock_timestamp(),clock_timestamp()
 FROM hosted_curtailment_confirmation WHERE phase='curtail'`)
	require.NoError(t, err)
	var asOf time.Time
	require.NoError(t, f.db.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&asOf))
	// PrepareReport rounds as-of to PostgreSQL microsecond precision. Reading the
	// database clock after the restore INSERT keeps every capture before it.
	opts := hostedaccounts.DraftReportOptions{Period: reporting.Period{Kind: "ad_hoc", Start: start.Add(-time.Hour), End: asOf, AsOf: asOf, Timezone: "UTC", RequestRef: request.RequestID, RequestReceivedAt: request.ReceivedAt}, InputRevision: saved.Revision, OwnerUserID: owner}
	type prepared struct {
		report hostedaccounts.ReportRecord
		err    error
	}
	finished := make(chan prepared, 1)
	go func() {
		report, prepareErr := f.svc.PrepareReport(ctx, f.host, f.org, client.AccountID, opts)
		finished <- prepared{report, prepareErr}
	}()
	require.Eventually(t, func() bool {
		var waiting bool
		err := f.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM pg_locks WHERE locktype='advisory' AND classid=10490 AND objid=1 AND NOT granted AND database=(SELECT oid FROM pg_database WHERE datname=current_database()))`).Scan(&waiting)
		return err == nil && waiting
	}, 5*time.Second, 10*time.Millisecond, "first control SELECT must reach the snapshot barrier")
	require.NoError(t, restore.Commit())
	require.NoError(t, gate.Commit())
	var captured hostedaccounts.ReportRecord
	select {
	case got := <-finished:
		require.NoError(t, got.err)
		captured = got.report
	case <-ctx.Done():
		t.Fatal("report did not complete after releasing the control snapshot barrier")
	}
	require.Len(t, captured.Result.Interruptions, 201)
	open := 0
	for _, observation := range captured.Result.Interruptions {
		if observation.Open {
			open++
		}
	}
	t.Logf("captured atomic restoration cohort: open=%d restored=%d", open, len(captured.Result.Interruptions)-open)
	require.Equal(t, 201, open, "the control statement began before the atomic restoration committed; no later page may freeze a conflicting view")
	frozen, err := f.svc.HostReport(ctx, f.host, f.org, client.AccountID, captured.ReportID)
	require.NoError(t, err)
	require.Equal(t, captured.Result, frozen.Result, "later restoration visibility cannot rewrite the saved control snapshot")
	opts.ReportID, opts.ExpectedRevision = captured.ReportID, captured.Revision
	regenerated, err := f.svc.PrepareReport(ctx, f.host, f.org, client.AccountID, opts)
	require.NoError(t, err)
	require.Len(t, regenerated.Result.Interruptions, 201)
	for _, observation := range regenerated.Result.Interruptions {
		require.False(t, observation.Open, "a new capture sees the whole committed restoration")
		require.False(t, observation.End.IsZero())
	}
}

func addSnapshotControlConfirmations(t *testing.T, f fixture, additional int) {
	t.Helper()
	_, err := f.db.ExecContext(t.Context(), `WITH added AS (
 INSERT INTO hosted_curtailment_confirmation(organization_id,event_id,device_id,curtail_batch,phase,phase_batch,sample_at,confirmed_at)
 SELECT organization_id,event_id,device_id,'snapshot-'||n,'curtail','snapshot-'||n,sample_at,confirmed_at
 FROM hosted_curtailment_confirmation CROSS JOIN generate_series(1,$1::int) n WHERE phase='curtail' RETURNING id
 ) INSERT INTO hosted_curtailment_asset(confirmation_id,interval_id,installation_id,organization_id,client_id,subclient_id,site_id,kind,asset_id)
 SELECT added.id,a.interval_id,a.installation_id,a.organization_id,a.client_id,a.subclient_id,a.site_id,a.kind,a.asset_id
 FROM added CROSS JOIN hosted_curtailment_asset a`, additional)
	require.NoError(t, err)
}

func TestHostedCurtailmentCaptureBudgetsFailWithoutSavingPartialReport(t *testing.T) {
	for _, tc := range []struct {
		name    string
		windows int
		error   string
	}{
		{"capture_memory", 32769, "curtailment capture exceeds report memory budget"},
		{"result_bytes", 12000, "curtailment evidence exceeds report byte budget"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := setup(t)
			client := f.client(t, "Bounded control client", f.user, hostedaccounts.AllCapabilities)
			owner, _ := reportActors(t, f, client.AccountID)
			device := f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG)
			controlSite(t, f, device)
			require.NoError(t, f.allocate(t, f.host, f.root, client.AccountID, 1, selected(5, device, 0)))
			c := newControlFixture(t, f, device)
			start := c.confirm(t, models.TargetPhaseCurtail, c.batch, true)
			addSnapshotControlConfirmations(t, f, tc.windows-1)
			saved, err := saveReportingInputsForTest(f.svc, t.Context(), f.host, f.org, client.AccountID, 0, "Control configuration", reportingInputs(t, f, client.AccountID, f.host))
			require.NoError(t, err)
			request, err := f.svc.CreateReportRequest(t.Context(), f.host, f.org, client.AccountID, hostedaccounts.ReportRequest{ReceivedAt: start.Add(-time.Hour), SourceRef: "Client receipt", Requester: "Client", Scope: "Control resource budgets", OwnerUserID: owner})
			require.NoError(t, err)
			var asOf time.Time
			require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT clock_timestamp()`).Scan(&asOf))
			opts := hostedaccounts.DraftReportOptions{Period: reporting.Period{Kind: "ad_hoc", Start: start.Add(-time.Hour), End: asOf, AsOf: asOf, Timezone: "UTC", RequestRef: request.RequestID, RequestReceivedAt: request.ReceivedAt}, InputRevision: saved.Revision, OwnerUserID: owner}
			_, err = f.svc.PrepareReport(t.Context(), f.host, f.org, client.AccountID, opts)
			require.ErrorContains(t, err, tc.error)
			var reports int
			require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM hosted_report`).Scan(&reports))
			require.Zero(t, reports, "resource exhaustion cannot save a truncated control cohort")
		})
	}
}
