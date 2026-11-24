package hostedaccounts_test

import (
	"context"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts/reporting"
	"github.com/stretchr/testify/require"
)

func TestHostedReportWorkDoesNotBlockAccountReaders(t *testing.T) {
	for _, operation := range []string{"prepare", "publish"} {
		t.Run(operation, func(t *testing.T) {
			f, client, reviewer, draft := draftReportFixture(t)
			barrier, err := f.db.BeginTx(t.Context(), nil)
			require.NoError(t, err)
			defer func() { _ = barrier.Rollback() }()
			_, err = barrier.ExecContext(t.Context(), `SELECT account_id FROM hosted_reporting_inputs WHERE account_id=$1 FOR NO KEY UPDATE`, client.ID)
			require.NoError(t, err)
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			done := make(chan error, 1)
			go func() {
				defer close(done)
				if operation == "publish" {
					_, err := f.svc.PublishReport(ctx, reviewer, f.org, client.AccountID, draft.ReportID, draft.Revision, "reviewed limitations")
					done <- err
					return
				}
				var owner string
				err := f.db.QueryRowContext(ctx, `SELECT user_id FROM "user" WHERE id=$1`, f.host).Scan(&owner)
				if err == nil {
					_, err = f.svc.PrepareReport(ctx, reviewer, f.org, client.AccountID, hostedaccounts.DraftReportOptions{Period: draft.Period, InputRevision: draft.InputRevision, OwnerUserID: owner, ReportID: draft.ReportID, ExpectedRevision: draft.Revision})
				}
				done <- err
			}()
			defer func() { cancel(); <-done }()
			require.Eventually(t, func() bool {
				var waiting bool
				err := f.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE '-- name: LockReportingInputsForReport%')`).Scan(&waiting)
				return err == nil && waiting
			}, 2*time.Second, 10*time.Millisecond)
			readCtx, stop := context.WithTimeout(t.Context(), time.Second)
			defer stop()
			_, err = f.svc.MiningSummary(readCtx, f.user, f.org, client.AccountID)
			require.NoError(t, err)
			_, err = f.svc.HostReport(readCtx, reviewer, f.org, client.AccountID, draft.ReportID)
			require.NoError(t, err)
			_, err = f.svc.HostReports(readCtx, reviewer, f.org, client.AccountID, hostedaccounts.ReportListOptions{})
			require.NoError(t, err)
			_, err = f.svc.ReportRequests(readCtx, reviewer, f.org, client.AccountID, hostedaccounts.ReportRequestListOptions{})
			require.NoError(t, err)
			_, err = f.svc.HostReportingSetup(readCtx, reviewer, f.org, client.AccountID)
			require.NoError(t, err)
			_, err = f.svc.ReportingInputsMetadata(readCtx, reviewer, f.org, client.AccountID)
			require.NoError(t, err)
			_, err = f.svc.ReportingFactPage(readCtx, reviewer, f.org, client.AccountID, reporting.FactOrder, draft.InputRevision, "", 200)
			require.NoError(t, err)
			require.NoError(t, barrier.Commit())
			require.NoError(t, <-done)
		})
	}
}

func TestHostedReportIssuedReadsDoNotLockReportRows(t *testing.T) {
	f, client, reviewer, draft := draftReportFixture(t)
	issued, err := f.svc.PublishReport(t.Context(), reviewer, f.org, client.AccountID, draft.ReportID, draft.Revision, "reviewed limitations")
	require.NoError(t, err)
	barrier, err := f.db.BeginTx(t.Context(), nil)
	require.NoError(t, err)
	defer func() { _ = barrier.Rollback() }()
	_, err = barrier.ExecContext(t.Context(), `SELECT id FROM hosted_report WHERE id=$1 FOR UPDATE`, issued.ID)
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	_, err = f.svc.HostReport(ctx, reviewer, f.org, client.AccountID, issued.ReportID)
	require.NoError(t, err)
	_, err = f.svc.AccountReport(ctx, f.user, f.org, client.AccountID, issued.ReportID)
	require.NoError(t, err)
	for _, host := range []bool{false, true} {
		actor := f.user
		if host {
			actor = reviewer
		}
		_, err = f.svc.ReportArtifactPage(ctx, actor, f.org, client.AccountID, issued.ReportID, "evidence.zip", 0, host)
		require.NoError(t, err)
	}
}

func TestHostedReportConcurrentOriginalsRemainUnique(t *testing.T) {
	f := setup(t)
	client := f.client(t, "Concurrent originals", f.user, hostedaccounts.AllCapabilities)
	owner, reviewer := reportActors(t, f, client.AccountID)
	savedReportInputs(t, f, client.AccountID, reviewer, reportingInputs(t, f, client.AccountID, reviewer), 0)
	opts := hostedaccounts.DraftReportOptions{Period: reportPeriod(), InputRevision: 1, OwnerUserID: owner}
	start, done := make(chan struct{}), make(chan error, 2)
	for _, actor := range []int64{f.host, reviewer} {
		go func() {
			<-start
			_, err := f.svc.PrepareReport(t.Context(), actor, f.org, client.AccountID, opts)
			done <- err
		}()
	}
	close(start)
	first, second := <-done, <-done
	if first == nil {
		require.True(t, fleeterror.IsFailedPreconditionError(second))
	} else {
		require.True(t, fleeterror.IsFailedPreconditionError(first))
		require.NoError(t, second)
	}
	var count int
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM hosted_report WHERE account_id=$1`, client.ID).Scan(&count))
	require.Equal(t, 1, count)
}
