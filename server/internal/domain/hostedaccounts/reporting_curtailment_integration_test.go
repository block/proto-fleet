package hostedaccounts_test

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/curtailment/models"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts/reporting"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
	"github.com/block/proto-fleet/server/migrations"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type controlFixture struct {
	f                 fixture
	store             *sqlstores.SQLCurtailmentStore
	event             *models.InsertEventResult
	device            int64
	identifier, batch string
}

func newControlFixture(t *testing.T, f fixture, device int64) controlFixture {
	t.Helper()
	var identifier string
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT device_identifier FROM device WHERE id=$1`, device).Scan(&identifier))
	store := sqlstores.NewSQLCurtailmentStore(f.db)
	scope, err := json.Marshal(map[string][]string{"device_identifiers": {identifier}})
	require.NoError(t, err)
	event, err := store.InsertEventWithTargets(t.Context(), models.InsertEventParams{
		EventUUID: uuid.New(), OrgID: f.org, State: models.EventStateActive, Mode: models.ModeFixedKw,
		Strategy: models.StrategyLeastEfficientFirst, Level: models.LevelFull, Priority: models.PriorityNormal,
		LoopType: models.LoopTypeOpen, ScopeType: models.ScopeTypeDeviceList, ScopeJSON: scope,
		ModeParamsJSON: []byte(`{"target_kw":1,"tolerance_kw":0}`), RestoreBatchSize: 10, EffectiveBatchSize: 10,
		DecisionSnapshotJSON: []byte(`{}`), SourceActorType: models.SourceActorUser, CreatedByUserID: f.host,
		Reason: "Private host reason; must never reach the client",
	}, []models.InsertTargetParams{{DeviceIdentifier: identifier, State: models.TargetStatePending, DesiredState: models.DesiredStateCurtailed}})
	require.NoError(t, err)
	c := controlFixture{f, store, event, device, identifier, uuid.NewString()}
	c.dispatch(t, models.DesiredStateCurtailed, c.batch)
	return c
}
func (c controlFixture) dispatch(t *testing.T, desired, batch string) {
	t.Helper()
	now := time.Now().UTC()
	require.NoError(t, c.store.UpdateTargetState(t.Context(), c.event.ID, c.identifier, interfaces.UpdateCurtailmentTargetStateParams{
		State: models.TargetStateDispatched, LastDispatchedAt: &now, LastBatchUUID: &batch, ExpectedDesiredState: &desired,
	}))
}
func (c controlFixture) confirm(t *testing.T, phase models.TargetPhase, batch string, bulk bool) time.Time {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Microsecond)
	if bulk {
		eventState := models.EventStateActive
		if phase == models.TargetPhaseRestore {
			eventState = models.EventStateRestoring
		}
		got, err := c.store.BulkConfirmTargets(t.Context(), c.event.ID, eventState, []interfaces.ConfirmationUpdate{{DeviceDatabaseID: c.device,
			DeviceIdentifier: c.identifier, Phase: phase, BatchUUID: batch, SampleAt: now, ObservedAt: now, ConfirmedAt: now}})
		require.NoError(t, err)
		require.Equal(t, 1, got.AppliedCount)
	} else {
		state, desired := models.TargetStateConfirmed, models.DesiredStateCurtailed
		if phase == models.TargetPhaseRestore {
			state, desired = models.TargetStateResolved, models.DesiredStateActive
		}
		dispatched := models.TargetStateDispatched
		require.NoError(t, c.store.UpdateTargetState(t.Context(), c.event.ID, c.identifier, interfaces.UpdateCurtailmentTargetStateParams{
			State: state, ConfirmationSampleAt: &now, ConfirmedAt: &now, ObservedAt: &now, ExpectedDeviceDatabaseID: &c.device, ExpectedState: &dispatched,
			ExpectedDesiredState: &desired, ExpectedDispatchBatchUUID: &batch,
		}))
	}
	return now
}
func controlRows(t *testing.T, f fixture, account int64, start, end, asOf time.Time) []sqlc.ListHostedCurtailmentWindowsRow {
	t.Helper()
	rows, err := sqlc.New(f.db).ListHostedCurtailmentWindows(t.Context(), sqlc.ListHostedCurtailmentWindowsParams{
		OrganizationID: f.org, AccountID: account, PeriodStart: start, PeriodEnd: end, AsOf: asOf, RowLimit: 10001,
	})
	require.NoError(t, err)
	return rows
}
func controlSite(t *testing.T, f fixture, device int64) int64 {
	t.Helper()
	var site int64
	require.NoError(t, f.db.QueryRowContext(t.Context(), `INSERT INTO site(org_id,name,slug) VALUES($1,'Control site',$2) RETURNING id`, f.org, uuid.NewString()).Scan(&site))
	_, err := f.db.ExecContext(t.Context(), `UPDATE device SET site_id=$1 WHERE id=$2`, site, device)
	require.NoError(t, err)
	return site
}

func TestHostedCurtailmentCaptureIsAtomicAndProspective(t *testing.T) {
	for _, bulk := range []bool{false, true} {
		t.Run(fmt.Sprintf("bulk=%t", bulk), func(t *testing.T) {
			f := setup(t)
			client := f.client(t, "Client", f.user, hostedaccounts.AllCapabilities)
			device := f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG)
			controlSite(t, f, device)
			require.NoError(t, f.allocate(t, f.host, f.root, client.AccountID, 1, selected(5, device, 0)))
			c := newControlFixture(t, f, device)
			var count int
			require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM hosted_curtailment_confirmation`).Scan(&count))
			require.Zero(t, count, "requested/dispatched is not confirmation")
			// An evidence failure must roll back the operational promotion too.
			_, err := f.db.ExecContext(t.Context(), `ALTER TABLE hosted_curtailment_confirmation ADD CONSTRAINT test_capture_failure CHECK(false) NOT VALID`)
			require.NoError(t, err)
			now := time.Now().UTC().Truncate(time.Microsecond).Add(789 * time.Nanosecond)
			dispatched := models.TargetStateDispatched
			if bulk {
				_, err = c.store.BulkConfirmTargets(t.Context(), c.event.ID, models.EventStateActive, []interfaces.ConfirmationUpdate{{DeviceDatabaseID: device, DeviceIdentifier: c.identifier, Phase: models.TargetPhaseCurtail, BatchUUID: c.batch, SampleAt: now, ObservedAt: now, ConfirmedAt: now}})
			} else {
				err = c.store.UpdateTargetState(t.Context(), c.event.ID, c.identifier, interfaces.UpdateCurtailmentTargetStateParams{State: models.TargetStateConfirmed, ConfirmationSampleAt: &now, ConfirmedAt: &now, ExpectedDeviceDatabaseID: &device, ExpectedState: &dispatched, ExpectedDispatchBatchUUID: &c.batch})
			}
			require.Error(t, err)
			var state string
			require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT state FROM curtailment_target WHERE curtailment_event_id=$1`, c.event.ID).Scan(&state))
			require.Equal(t, "dispatched", state)
			_, err = f.db.ExecContext(t.Context(), `ALTER TABLE hosted_curtailment_confirmation DROP CONSTRAINT test_capture_failure`)
			require.NoError(t, err)
			// Existing transaction ownership must be honored by both paths.
			tx, err := f.db.BeginTx(t.Context(), nil)
			require.NoError(t, err)
			txCtx := db.WithTxQueries(t.Context(), sqlc.New(tx))
			sample := time.Now().UTC()
			if bulk {
				_, err = c.store.BulkConfirmTargets(txCtx, c.event.ID, models.EventStateActive, []interfaces.ConfirmationUpdate{{DeviceDatabaseID: device, DeviceIdentifier: c.identifier, Phase: models.TargetPhaseCurtail, BatchUUID: c.batch, SampleAt: sample, ObservedAt: sample, ConfirmedAt: sample}})
			} else {
				err = c.store.UpdateTargetState(txCtx, c.event.ID, c.identifier, interfaces.UpdateCurtailmentTargetStateParams{State: models.TargetStateConfirmed, ConfirmationSampleAt: &sample, ConfirmedAt: &sample, ExpectedDeviceDatabaseID: &device, ExpectedState: &dispatched, ExpectedDispatchBatchUUID: &c.batch})
			}
			require.NoError(t, err)
			require.NoError(t, tx.Rollback())
			require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT state FROM curtailment_target WHERE curtailment_event_id=$1`, c.event.ID).Scan(&state))
			require.Equal(t, "dispatched", state)
			confirmed := c.confirm(t, models.TargetPhaseCurtail, c.batch, bulk)
			rows := controlRows(t, f, client.ID, confirmed.Add(-time.Hour), confirmed.Add(time.Hour), time.Now().UTC())
			require.Len(t, rows, 1)
			require.True(t, rows[0].RestoredAt.IsZero())
			// A capture-time cutoff excludes evidence learned after a historical as-of.
			var captured time.Time
			require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT captured_at FROM hosted_curtailment_confirmation WHERE phase='curtail'`).Scan(&captured))
			cutoff := captured.Add(-time.Microsecond)
			require.Empty(t, controlRows(t, f, client.ID, confirmed.Add(-time.Hour), confirmed.Add(time.Hour), cutoff))
			got, err := c.store.BulkConfirmTargets(t.Context(), c.event.ID, models.EventStateActive, []interfaces.ConfirmationUpdate{{DeviceDatabaseID: device, DeviceIdentifier: c.identifier, Phase: models.TargetPhaseCurtail, BatchUUID: c.batch, ConfirmedAt: confirmed, ObservedAt: confirmed}})
			require.NoError(t, err)
			require.Zero(t, got.AppliedCount)
			require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM hosted_curtailment_confirmation`).Scan(&count))
			require.Equal(t, 1, count)
			// Administrative release completes a mutable phase summary but is not proof of restoration.
			require.NoError(t, c.store.UpdateTargetState(t.Context(), c.event.ID, c.identifier, interfaces.UpdateCurtailmentTargetStateParams{State: models.TargetStateReleased}))
			rows = controlRows(t, f, client.ID, confirmed.Add(-time.Hour), confirmed.Add(time.Hour), time.Now().UTC())
			require.Len(t, rows, 1)
			require.True(t, rows[0].RestoredAt.IsZero())
			down, err := migrations.Current.ReadFile("current/001092_internal_hosted_curtailment_observations.down.sql")
			require.NoError(t, err)
			rollback, err := f.db.BeginTx(t.Context(), nil)
			require.NoError(t, err)
			_, err = rollback.ExecContext(t.Context(), string(down))
			require.ErrorContains(t, err, "cannot discard captured curtailment confirmations")
			require.NoError(t, rollback.Rollback())
		})
	}
}

func TestHostedCurtailmentMatchesPhysicalDeviceAndCycle(t *testing.T) {
	f := setup(t)
	client := f.client(t, "Client", f.user, hostedaccounts.AllCapabilities)
	device := f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG)
	controlSite(t, f, device)
	require.NoError(t, f.allocate(t, f.host, f.root, client.AccountID, 1, selected(5, device, 0)))
	c := newControlFixture(t, f, device)
	wrong := device + 100000
	now := time.Now().UTC()
	dispatched := models.TargetStateDispatched
	err := c.store.UpdateTargetState(t.Context(), c.event.ID, c.identifier, interfaces.UpdateCurtailmentTargetStateParams{State: models.TargetStateConfirmed, ConfirmedAt: &now, ExpectedDeviceDatabaseID: &wrong, ExpectedState: &dispatched, ExpectedDispatchBatchUUID: &c.batch})
	require.ErrorIs(t, err, interfaces.ErrCurtailmentEventStateRaceLoss)
	start := c.confirm(t, models.TargetPhaseCurtail, c.batch, false)
	_, err = c.store.BeginRestoreTransition(t.Context(), f.org, c.event.EventUUID, interfaces.BeginRestoreTransitionParams{})
	require.NoError(t, err)
	restoreBatch := uuid.NewString()
	c.dispatch(t, models.DesiredStateActive, restoreBatch)
	// An unrelated cycle's restore cannot close this confirmation.
	_, err = f.db.ExecContext(t.Context(), `INSERT INTO hosted_curtailment_confirmation(organization_id,event_id,device_id,curtail_batch,phase,phase_batch,sample_at,confirmed_at) VALUES($1,$2,$3,'unrelated','restore','unrelated',clock_timestamp(),clock_timestamp())`, f.org, c.event.ID, device)
	require.NoError(t, err)
	rows := controlRows(t, f, client.ID, start.Add(-time.Hour), start.Add(time.Hour), time.Now().UTC())
	require.Len(t, rows, 1)
	require.True(t, rows[0].RestoredAt.IsZero())
	restored := c.confirm(t, models.TargetPhaseRestore, restoreBatch, true)
	rows = controlRows(t, f, client.ID, start.Add(-time.Hour), start.Add(time.Hour), time.Now().UTC())
	require.Len(t, rows, 1)
	require.True(t, restored.Equal(rows[0].RestoredAt))
	// Resetting summaries and reusing the identifier cannot rewrite captured endpoints.
	_, err = f.db.ExecContext(t.Context(), `UPDATE curtailment_target SET curtail_completed_at=NULL,restore_completed_at=NULL,confirmed_at=NULL WHERE curtailment_event_id=$1`, c.event.ID)
	require.NoError(t, err)
	rows = controlRows(t, f, client.ID, start.Add(-time.Hour), start.Add(time.Hour), time.Now().UTC())
	require.Len(t, rows, 1)
	require.True(t, start.Equal(rows[0].StartedAt))
	require.True(t, restored.Equal(rows[0].RestoredAt))
}

func TestHostedCurtailmentSharedCohortAndFrozenReport(t *testing.T) {
	f := setup(t)
	client := f.client(t, "Client", f.user, hostedaccounts.AllCapabilities)
	other := f.client(t, "Private other client", f.user, hostedaccounts.AllCapabilities)
	owner, reviewer := reportActors(t, f, client.AccountID)
	module, device := f.miningModule(t)
	site := controlSite(t, f, device)
	f.observeModuleAtCurrentSite(t, module, device)
	var boards []int64
	rows, err := f.db.QueryContext(t.Context(), `SELECT hashboard_id FROM hashboard_installation WHERE module_id=$1 AND removed_at IS NULL ORDER BY position`, module)
	require.NoError(t, err)
	defer func() { require.NoError(t, rows.Close()) }()
	for rows.Next() {
		var id int64
		require.NoError(t, rows.Scan(&id))
		boards = append(boards, id)
	}
	require.NoError(t, rows.Err())
	require.NoError(t, rows.Close())
	require.Len(t, boards, 3)
	require.NoError(t, f.allocate(t, f.host, f.root, client.AccountID, 1, selected(7, boards[0], 0)))
	require.NoError(t, f.allocate(t, f.host, f.root, other.AccountID, 1, selected(7, boards[1], 0)))
	c := newControlFixture(t, f, device)
	start := c.confirm(t, models.TargetPhaseCurtail, c.batch, true)
	// Later hardware assignments never inherit the earlier curtail confirmation.
	require.NoError(t, f.allocate(t, f.host, f.root, client.AccountID, 1, selected(7, boards[2], 0)))
	scoped := controlRows(t, f, client.ID, start.Add(-time.Hour), start.Add(time.Hour), time.Now().UTC())
	require.Len(t, scoped, 1)
	require.Equal(t, boards[0], scoped[0].AssetID)
	otherRows := controlRows(t, f, other.ID, start.Add(-time.Hour), start.Add(time.Hour), time.Now().UTC())
	require.Len(t, otherRows, 1)
	require.Equal(t, boards[1], otherRows[0].AssetID)
	savedReportInputs(t, f, client.AccountID, reviewer, reportingInputs(t, f, client.AccountID, f.host), 0)
	request, err := f.svc.CreateReportRequest(t.Context(), f.host, f.org, client.AccountID, hostedaccounts.ReportRequest{ReceivedAt: start.Add(-time.Hour), SourceRef: "client receipt", Requester: "Client", Scope: "Control evidence", OwnerUserID: owner})
	require.NoError(t, err)
	periodEnd := time.Now().UTC().Truncate(time.Microsecond)
	opts := hostedaccounts.DraftReportOptions{Period: reporting.Period{Kind: "ad_hoc", Start: start.Add(-time.Hour), End: periodEnd, AsOf: time.Now().UTC(), Timezone: "UTC", RequestRef: request.RequestID, RequestReceivedAt: request.ReceivedAt}, InputRevision: 1, OwnerUserID: owner}
	report, err := f.svc.PrepareReport(t.Context(), f.host, f.org, client.AccountID, opts)
	require.NoError(t, err)
	require.Equal(t, []int64{site}, report.SiteIDs)
	require.Len(t, report.Result.Interruptions, 1)
	event := report.Result.Interruptions[0]
	require.Equal(t, "curtailment_control", event.Category)
	require.True(t, event.Open)
	require.Empty(t, event.ClaimedExclusion)
	require.Equal(t, reporting.Unavailable, event.ExclusionStatus)
	expected, err := reporting.Calculate(opts.Period, reportingInputs(t, f, client.AccountID, f.host))
	require.NoError(t, err)
	require.Equal(t, expected.Metrics, report.Result.Metrics, "control observations cannot alter reviewed or contractual metrics")
	frozen, err := json.Marshal(report.Result)
	require.NoError(t, err)
	_, err = f.svc.PublishReport(t.Context(), f.host, f.org, client.AccountID, report.ReportID, report.Revision, "Synthetic control fixture; contractual definitions unavailable")
	require.NoError(t, err)
	// A later restoration is available for future preparation, never injected into the issued snapshot.
	_, err = c.store.BeginRestoreTransition(t.Context(), f.org, c.event.EventUUID, interfaces.BeginRestoreTransitionParams{})
	require.NoError(t, err)
	restoreBatch := uuid.NewString()
	c.dispatch(t, models.DesiredStateActive, restoreBatch)
	c.confirm(t, models.TargetPhaseRestore, restoreBatch, false)
	got, err := f.svc.AccountReport(t.Context(), f.user, f.org, client.AccountID, report.ReportID)
	require.NoError(t, err)
	encoded, err := json.Marshal(got.Result)
	require.NoError(t, err)
	require.JSONEq(t, string(frozen), string(encoded))
	require.NotContains(t, string(encoded), "Private host reason")
	require.NotContains(t, string(encoded), c.identifier)
	// Frozen source sites participate in normal read authorization.
	require.NoError(t, f.svc.SetReportingAuthority(t.Context(), f.host, f.org, client.AccountID, hostedaccounts.ReportingAuthority{UserID: owner, CanCreate: true, SiteIDs: []int64{}}, 1))
	_, err = f.svc.HostReport(t.Context(), f.host, f.org, client.AccountID, report.ReportID)
	require.Error(t, err)
}

func TestHostedCurtailmentTransferAndRedispatchBoundOriginalCohort(t *testing.T) {
	f := setup(t)
	client := f.client(t, "First owner", f.user, hostedaccounts.AllCapabilities)
	other := f.client(t, "Later owner", f.user, hostedaccounts.AllCapabilities)
	child, err := f.svc.CreateChild(t.Context(), f.user, f.org, client.AccountID, "Subclient", f.user, hostedaccounts.DefaultCapabilities)
	require.NoError(t, err)
	device := f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG)
	controlSite(t, f, device)
	require.NoError(t, f.allocate(t, f.host, f.root, client.AccountID, 1, selected(5, device, 0)))
	require.NoError(t, f.allocate(t, f.user, client.AccountID, child.AccountID, 1, selected(5, device, 1)))
	c := newControlFixture(t, f, device)
	start := c.confirm(t, models.TargetPhaseCurtail, c.batch, true)
	scoped := controlRows(t, f, child.ID, start.Add(-time.Hour), start.Add(time.Hour), time.Now().UTC())
	require.Len(t, scoped, 1)
	require.Empty(t, controlRows(t, f, other.ID, start.Add(-time.Hour), start.Add(time.Hour), time.Now().UTC()))
	// A redispatch is new evidence, not an extension reconstructed from reset fields.
	newBatch := uuid.NewString()
	c.dispatch(t, models.DesiredStateCurtailed, newBatch)
	second := c.confirm(t, models.TargetPhaseCurtail, newBatch, false)
	scoped = controlRows(t, f, child.ID, start.Add(-time.Hour), start.Add(time.Hour), time.Now().UTC())
	require.Len(t, scoped, 2)
	require.True(t, second.Equal(scoped[0].AttributionEnd))
	require.True(t, scoped[0].RestoredAt.IsZero())
	require.Len(t, controlRows(t, f, child.ID, second, second.Add(time.Hour), time.Now().UTC()), 1, "superseded confirmation does not remain open in later periods")
	// Recall to the host then assign to a different client. Earlier evidence stays
	// with the original customer/subclient, ending at the old allocation boundary.
	require.NoError(t, f.allocate(t, f.host, f.root, client.AccountID, 2, selected(5, device, 2)))
	require.NoError(t, f.allocate(t, f.host, f.root, other.AccountID, 1, selected(5, device, 3)))
	scoped = controlRows(t, f, child.ID, start.Add(-time.Hour), start.Add(time.Hour), time.Now().UTC())
	require.Len(t, scoped, 2)
	require.False(t, scoped[1].AttributionEnd.IsZero())
	require.Empty(t, controlRows(t, f, child.ID, time.Now().UTC(), start.Add(time.Hour), time.Now().UTC()))
	require.Empty(t, controlRows(t, f, other.ID, start.Add(-time.Hour), start.Add(time.Hour), time.Now().UTC()), "new owner cannot see old observations")
}

func TestHostedCurtailmentInstallationRemovalAndReusedIdentifier(t *testing.T) {
	f := setup(t)
	client := f.client(t, "Client", f.user, hostedaccounts.AllCapabilities)
	module, device := f.miningModule(t)
	controlSite(t, f, device)
	f.observeModuleAtCurrentSite(t, module, device)
	var board, installation int64
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT hashboard_id,id FROM hashboard_installation WHERE module_id=$1 AND position=1 AND removed_at IS NULL`, module).Scan(&board, &installation))
	require.NoError(t, f.allocate(t, f.host, f.root, client.AccountID, 1, selected(7, board, 0)))
	c := newControlFixture(t, f, device)
	start := c.confirm(t, models.TargetPhaseCurtail, c.batch, true)
	removed := time.Now().UTC().Truncate(time.Microsecond)
	_, err := f.db.ExecContext(t.Context(), `UPDATE hashboard_installation SET removed_at=$2 WHERE id=$1`, installation, removed)
	require.NoError(t, err)
	scoped := controlRows(t, f, client.ID, start.Add(-time.Hour), start.Add(time.Hour), time.Now().UTC())
	require.Len(t, scoped, 1)
	require.True(t, removed.Equal(scoped[0].AttributionEnd))
	// A newly live device with the same identifier cannot receive an old sampled confirmation.
	_, err = f.db.ExecContext(t.Context(), `UPDATE device SET deleted_at=clock_timestamp() WHERE id=$1`, device)
	require.NoError(t, err)
	replacement := f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_CONTAINER)
	_, err = f.db.ExecContext(t.Context(), `UPDATE device SET device_identifier=$1 WHERE id=$2`, c.identifier, replacement)
	require.NoError(t, err)
	batch := uuid.NewString()
	c.dispatch(t, models.DesiredStateCurtailed, batch)
	now := time.Now().UTC()
	dispatched := models.TargetStateDispatched
	err = c.store.UpdateTargetState(t.Context(), c.event.ID, c.identifier, interfaces.UpdateCurtailmentTargetStateParams{
		State: models.TargetStateConfirmed, ConfirmationSampleAt: &now, ConfirmedAt: &now, ExpectedDeviceDatabaseID: &device, ExpectedState: &dispatched, ExpectedDispatchBatchUUID: &batch,
	})
	require.ErrorIs(t, err, interfaces.ErrCurtailmentEventStateRaceLoss)
	got, err := c.store.BulkConfirmTargets(t.Context(), c.event.ID, models.EventStateActive, []interfaces.ConfirmationUpdate{{DeviceDatabaseID: device, DeviceIdentifier: c.identifier, Phase: models.TargetPhaseCurtail, BatchUUID: batch, ConfirmedAt: now, ObservedAt: now}})
	require.NoError(t, err)
	require.Zero(t, got.AppliedCount)
}

func TestHostedCurtailmentRequiresPostDispatchSampleAndOriginalAllocation(t *testing.T) {
	for _, stale := range []bool{true, false} {
		t.Run(fmt.Sprintf("stale=%t", stale), func(t *testing.T) {
			f := setup(t)
			client := f.client(t, "Client", f.user, hostedaccounts.AllCapabilities)
			device := f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG)
			controlSite(t, f, device)
			c := newControlFixture(t, f, device)
			sampled := time.Now().UTC().Truncate(time.Microsecond)
			if stale {
				sampled = sampled.Add(-time.Hour)
			}
			// This customer allocation starts after sampling in both cases.
			require.NoError(t, f.allocate(t, f.host, f.root, client.AccountID, 1, selected(5, device, 0)))
			confirmed := time.Now().UTC().Truncate(time.Microsecond)
			got, err := c.store.BulkConfirmTargets(t.Context(), c.event.ID, models.EventStateActive, []interfaces.ConfirmationUpdate{{DeviceDatabaseID: device, DeviceIdentifier: c.identifier, Phase: models.TargetPhaseCurtail, BatchUUID: c.batch, SampleAt: sampled, ObservedAt: confirmed, ConfirmedAt: confirmed}})
			require.NoError(t, err)
			require.Equal(t, 1, got.AppliedCount, "reporting admission does not change operational confirmation")
			require.Empty(t, controlRows(t, f, client.ID, confirmed.Add(-time.Hour), confirmed.Add(time.Hour), time.Now().UTC()))
			var count int
			require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM hosted_curtailment_confirmation`).Scan(&count))
			if stale {
				require.Zero(t, count, "pre-dispatch telemetry cannot create new historical evidence")
			} else {
				require.Equal(t, 1, count, "valid control confirmation is retained without attributing it to a later allocation")
			}
		})
	}
}

func TestHostedCurtailmentSingleConfirmationNormalizesTimestampPrecision(t *testing.T) {
	f := setup(t)
	client := f.client(t, "Client", f.user, hostedaccounts.AllCapabilities)
	device := f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG)
	controlSite(t, f, device)
	require.NoError(t, f.allocate(t, f.host, f.root, client.AccountID, 1, selected(5, device, 0)))
	c := newControlFixture(t, f, device)
	now := time.Now().UTC().Truncate(time.Microsecond).Add(789 * time.Nanosecond)
	original := now
	dispatched := models.TargetStateDispatched
	require.NoError(t, c.store.UpdateTargetState(t.Context(), c.event.ID, c.identifier, interfaces.UpdateCurtailmentTargetStateParams{
		State: models.TargetStateConfirmed, ConfirmationSampleAt: &now, ConfirmedAt: &now, ExpectedDeviceDatabaseID: &device, ExpectedState: &dispatched, ExpectedDispatchBatchUUID: &c.batch,
	}))
	require.Equal(t, original, now, "normalization must not mutate caller-owned timestamps")
	rows := controlRows(t, f, client.ID, now.Add(-time.Hour), now.Add(time.Hour), time.Now().UTC())
	require.Len(t, rows, 1)
	require.True(t, now.Truncate(time.Microsecond).Equal(rows[0].StartedAt))
	var confirmed time.Time
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT confirmed_at FROM curtailment_target WHERE curtailment_event_id=$1`, c.event.ID).Scan(&confirmed))
	require.True(t, confirmed.Equal(rows[0].StartedAt), "operational promotion and captured evidence must agree exactly")
}

func TestHostedCurtailmentTopologyRestoreClosesWindowBeforeGlobalRestore(t *testing.T) {
	for _, parent := range []models.EventState{models.EventStatePending, models.EventStateActive} {
		for _, bulk := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/bulk=%t", parent, bulk), func(t *testing.T) {
				f := setup(t)
				client := f.client(t, "Client", f.user, hostedaccounts.AllCapabilities)
				device := f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG)
				controlSite(t, f, device)
				require.NoError(t, f.allocate(t, f.host, f.root, client.AccountID, 1, selected(5, device, 0)))
				c := newControlFixture(t, f, device)
				start := c.confirm(t, models.TargetPhaseCurtail, c.batch, bulk)
				// Topology reconciliation can restore an individual target while the event
				// continues controlling other targets. Ownership and physical placement stay put.
				_, err := f.db.ExecContext(t.Context(), `UPDATE curtailment_event SET state=$2 WHERE id=$1`, c.event.ID, string(parent))
				require.NoError(t, err)
				_, err = f.db.ExecContext(t.Context(), `UPDATE curtailment_target SET desired_state='active',state='restore_pending' WHERE curtailment_event_id=$1`, c.event.ID)
				require.NoError(t, err)
				batch := uuid.NewString()
				c.dispatch(t, models.DesiredStateActive, batch)
				restored := time.Now().UTC().Truncate(time.Microsecond)
				if bulk {
					applied, err := c.store.BulkConfirmTargets(t.Context(), c.event.ID, parent, []interfaces.ConfirmationUpdate{{DeviceDatabaseID: device, DeviceIdentifier: c.identifier, Phase: models.TargetPhaseRestore, BatchUUID: batch, SampleAt: restored, ObservedAt: restored, ConfirmedAt: restored}})
					require.NoError(t, err)
					require.Equal(t, 1, applied.AppliedCount)
				} else {
					dispatched := models.TargetStateDispatched
					desired := models.DesiredStateActive
					require.NoError(t, c.store.UpdateTargetState(t.Context(), c.event.ID, c.identifier, interfaces.UpdateCurtailmentTargetStateParams{State: models.TargetStateResolved, ConfirmationSampleAt: &restored, ConfirmedAt: &restored, ExpectedDeviceDatabaseID: &device, ExpectedState: &dispatched, ExpectedDesiredState: &desired, ExpectedDispatchBatchUUID: &batch}))
				}
				rows := controlRows(t, f, client.ID, start.Add(-time.Hour), start.Add(time.Hour), time.Now().UTC())
				require.Len(t, rows, 1)
				require.True(t, restored.Equal(rows[0].RestoredAt), "positive per-target restore telemetry must close the captured control window")
			})
		}
	}
}

func TestHostedCurtailmentPagesTiedObservationsAndIgnoresRetainedInputHistory(t *testing.T) {
	f := setup(t)
	client := f.client(t, "Paged control client", f.user, hostedaccounts.AllCapabilities)
	owner, _ := reportActors(t, f, client.AccountID)
	device := f.miningDevice(t, commonpb.DeviceClass_DEVICE_CLASS_RIG)
	controlSite(t, f, device)
	require.NoError(t, f.allocate(t, f.host, f.root, client.AccountID, 1, selected(5, device, 0)))
	c := newControlFixture(t, f, device)
	start := c.confirm(t, models.TargetPhaseCurtail, c.batch, true)
	_, err := f.db.ExecContext(t.Context(), `WITH added AS (
 INSERT INTO hosted_curtailment_confirmation(organization_id,event_id,device_id,curtail_batch,phase,phase_batch,sample_at,confirmed_at)
 SELECT organization_id,event_id,device_id,'page-'||n,'curtail','page-'||n,sample_at,confirmed_at
 FROM hosted_curtailment_confirmation CROSS JOIN generate_series(1,200) n WHERE phase='curtail' RETURNING id
 ) INSERT INTO hosted_curtailment_asset(confirmation_id,interval_id,installation_id,organization_id,client_id,subclient_id,site_id,kind,asset_id)
 SELECT added.id,a.interval_id,a.installation_id,a.organization_id,a.client_id,a.subclient_id,a.site_id,a.kind,a.asset_id
 FROM added CROSS JOIN hosted_curtailment_asset a`)
	require.NoError(t, err)
	saved, err := saveReportingInputsForTest(f.svc, t.Context(), f.host, f.org, client.AccountID, 0, "Control configuration", reportingInputs(t, f, client.AccountID, f.host))
	require.NoError(t, err)
	request, err := f.svc.CreateReportRequest(t.Context(), f.host, f.org, client.AccountID, hostedaccounts.ReportRequest{ReceivedAt: start.Add(-time.Hour), SourceRef: "Client receipt", Requester: "Client", Scope: "Paged controls", OwnerUserID: owner})
	require.NoError(t, err)
	asOf := time.Now().UTC().Truncate(time.Microsecond)
	opts := hostedaccounts.DraftReportOptions{Period: reporting.Period{Kind: "ad_hoc", Start: start.Add(-time.Hour), End: asOf, AsOf: asOf, Timezone: "UTC", RequestRef: request.RequestID, RequestReceivedAt: request.ReceivedAt}, InputRevision: saved.Revision, OwnerUserID: owner}
	baseline, err := f.svc.PrepareReport(t.Context(), f.host, f.org, client.AccountID, opts)
	require.NoError(t, err)
	require.Len(t, baseline.Result.Interruptions, 201, "same-start confirmations cross the page cursor without omissions")
	ids := map[string]bool{}
	for _, v := range baseline.Result.Interruptions {
		require.False(t, ids[v.ID])
		ids[v.ID] = true
	}
	revision := saved.Revision
	for offset := 0; offset < 201; offset += 200 {
		changes := []reporting.FactMutation{}
		for i := offset; i < min(offset+200, 201); i++ {
			changes = append(changes, reportingUpsertForTest(t, reporting.Order{ID: fmt.Sprintf("retained-%03d", i), Name: "Historical order", Start: start.AddDate(-2, 0, 0), End: start.AddDate(-1, 0, 0), SourceRef: "Retained history"}))
		}
		updated, err := f.svc.MutateReportingInputs(t.Context(), f.host, f.org, client.AccountID, revision, "Retained historical orders", nil, changes)
		require.NoError(t, err)
		revision = updated.Revision
	}
	opts.ReportID, opts.ExpectedRevision, opts.InputRevision = baseline.ReportID, baseline.Revision, revision
	regenerated, err := f.svc.PrepareReport(t.Context(), f.host, f.org, client.AccountID, opts)
	require.NoError(t, err)
	require.Equal(t, baseline.Result, regenerated.Result, "irrelevant current-register growth preserves frozen control membership and result")
	_, err = f.svc.PublishReport(t.Context(), f.host, f.org, client.AccountID, regenerated.ReportID, regenerated.Revision, "Synthetic fixture; contractual definitions unavailable")
	require.NoError(t, err)
}
