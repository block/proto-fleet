package hostedaccounts_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/alerts"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/stretchr/testify/require"
)

func notificationRecoveryFixture(t *testing.T, count int) (fixture, hostedaccounts.Account, time.Time) {
	t.Helper()
	f := setup(t)
	client := f.client(t, "Notification recovery", f.user, hostedaccounts.AllCapabilities)
	owner, _ := reportActors(t, f, client.AccountID)
	now := time.Now().UTC().Round(time.Microsecond)
	for range count {
		_, err := f.svc.CreateReportRequest(t.Context(), f.host, f.org, client.AccountID, hostedaccounts.ReportRequest{ReceivedAt: now.Add(-time.Hour), SourceRef: "client request", Requester: "Client", Scope: "Written explanation", OwnerUserID: owner})
		require.NoError(t, err)
	}
	require.NoError(t, hostedaccounts.NewReportingJobs(f.svc, nil).Tick(t.Context(), now))
	return f, client, now
}

func claimNotificationAttempt(t *testing.T, f fixture, now time.Time, priorAttempts int64) sqlc.HostedReportQueue {
	t.Helper()
	_, err := f.db.ExecContext(t.Context(), `UPDATE hosted_report_queue SET attempts=$1,delivered_channel_ids='{11,22}'`, priorAttempts)
	require.NoError(t, err)
	claim, err := sqlc.New(f.db).ClaimHostedReportQueueNotification(t.Context(), sqlc.ClaimHostedReportQueueNotificationParams{NowAt: now, ClaimToken: sql.NullString{String: "lost-process", Valid: true}, LeaseUntil: sql.NullTime{Time: now.Add(time.Minute), Valid: true}})
	require.NoError(t, err)
	require.Equal(t, priorAttempts+1, claim.Attempts)
	return claim
}

type recoveryNotifier struct {
	delivered []int64
	calls     int
}

func (n *recoveryNotifier) DeliverTrackedSkipping(_ context.Context, _ []alerts.Alert, delivered []int64) alerts.DeliveryResult {
	n.calls++
	n.delivered = append([]int64{}, delivered...)
	return alerts.DeliveryResult{Delivered: 1, DeliveredChannelIDs: []int64{33}}
}

func TestHostedReportingNotificationRecoversExpiredFinalClaim(t *testing.T) {
	for _, status := range []string{"pending", "failed"} {
		t.Run(status, func(t *testing.T) {
			f, client, now := notificationRecoveryFixture(t, 1)
			_, err := f.db.ExecContext(t.Context(), `UPDATE hosted_report_queue SET notification_status=$1`, status)
			require.NoError(t, err)
			claim := claimNotificationAttempt(t, f, now, 4)
			// Recovery also runs when notification delivery is not configured.
			require.NoError(t, hostedaccounts.NewReportingJobs(f.svc, nil).Tick(t.Context(), now.Add(time.Minute)))
			page, err := f.svc.HostReportQueue(t.Context(), f.host, f.org, 100, "")
			require.NoError(t, err)
			require.Len(t, page.Items, 1)
			item := page.Items[0]
			require.Equal(t, "exhausted", item.NotificationStatus)
			require.True(t, item.CanRetry)
			require.Equal(t, claim.Generation, item.Generation)
			require.EqualValues(t, 5, item.Attempts)

			q := sqlc.New(f.db)
			stored, err := q.GetHostedReportQueue(t.Context(), sqlc.GetHostedReportQueueParams{OrganizationID: f.org, AccountID: client.ID, QueueID: claim.QueueID})
			require.NoError(t, err)
			require.False(t, stored.ClaimToken.Valid)
			require.False(t, stored.LeaseUntil.Valid)
			require.Equal(t, []int64{11, 22}, stored.DeliveredChannelIds)
			notifier := &recoveryNotifier{}
			for range 2 {
				require.NoError(t, hostedaccounts.NewReportingJobs(f.svc, notifier).Tick(t.Context(), now.Add(2*time.Minute)))
			}
			require.Zero(t, notifier.calls, "exhaustion must not send a sixth automatic notification")
			var events int
			require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM hosted_report_queue_event WHERE queue_internal_id=$1 AND generation=$2 AND kind='exhausted'`, claim.ID, claim.Generation).Scan(&events))
			require.Equal(t, 1, events)
			// Even a completion carrying its old pre-expiry timestamp loses its token.
			changed, err := q.CompleteHostedReportQueueNotification(t.Context(), sqlc.CompleteHostedReportQueueNotificationParams{ID: claim.ID, Generation: claim.Generation, ClaimToken: claim.ClaimToken, NotificationStatus: "sent", RetryAt: now, NowAt: now})
			require.NoError(t, err)
			require.Zero(t, changed)
			require.NoError(t, f.svc.RetryReportQueueNotification(t.Context(), f.host, f.org, client.AccountID, claim.QueueID, claim.Generation, "destinations reviewed"))
			require.NoError(t, hostedaccounts.NewReportingJobs(f.svc, notifier).Tick(t.Context(), now.Add(3*time.Minute)))
			require.Equal(t, 1, notifier.calls)
			require.Equal(t, []int64{11, 22}, notifier.delivered, "operator retry preserves destinations already accepted")
			stored, err = q.GetHostedReportQueue(t.Context(), sqlc.GetHostedReportQueueParams{OrganizationID: f.org, AccountID: client.ID, QueueID: claim.QueueID})
			require.NoError(t, err)
			require.Equal(t, "sent", stored.NotificationStatus)
			require.EqualValues(t, 1, stored.Attempts)
			require.Equal(t, []int64{11, 22, 33}, stored.DeliveredChannelIds)
		})
	}
}

func TestHostedReportingNotificationKeepsLiveFinalClaimAndRetriesEarlierExpiry(t *testing.T) {
	for _, priorAttempts := range []int64{3, 4} {
		t.Run(map[int64]string{3: "retry fourth claim", 4: "keep live fifth claim"}[priorAttempts], func(t *testing.T) {
			f, client, now := notificationRecoveryFixture(t, 1)
			claim := claimNotificationAttempt(t, f, now, priorAttempts)
			notifier := &recoveryNotifier{}
			tickAt := now.Add(30 * time.Second)
			if priorAttempts == 3 {
				tickAt = now.Add(time.Minute)
			}
			require.NoError(t, hostedaccounts.NewReportingJobs(f.svc, notifier).Tick(t.Context(), tickAt))
			stored, err := sqlc.New(f.db).GetHostedReportQueue(t.Context(), sqlc.GetHostedReportQueueParams{OrganizationID: f.org, AccountID: client.ID, QueueID: claim.QueueID})
			require.NoError(t, err)
			if priorAttempts == 3 {
				require.Equal(t, 1, notifier.calls)
				require.Equal(t, "sent", stored.NotificationStatus)
				require.EqualValues(t, 5, stored.Attempts)
			} else {
				require.Zero(t, notifier.calls)
				require.Equal(t, "pending", stored.NotificationStatus)
				require.Equal(t, claim.ClaimToken, stored.ClaimToken)
				require.Equal(t, claim.LeaseUntil, stored.LeaseUntil)
			}
		})
	}
}

func TestHostedReportingNotificationRecoveryIsBoundedAndConcurrent(t *testing.T) {
	f, _, now := notificationRecoveryFixture(t, 65)
	_, err := f.db.ExecContext(t.Context(), `UPDATE hosted_report_queue SET attempts=5,claim_token='lost-process',lease_until=$1`, now.Add(-time.Second))
	require.NoError(t, err)
	// Hold one recovery batch uncommitted. A competing transaction must make
	// progress on other rows without waiting for those leases to be released.
	first, err := f.db.BeginTx(t.Context(), nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = first.Rollback() })
	firstBatch, err := sqlc.New(first).ExhaustExpiredHostedReportQueueNotifications(t.Context(), now)
	require.NoError(t, err)
	require.Len(t, firstBatch, 32)
	second, err := f.db.BeginTx(t.Context(), nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = second.Rollback() })
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	secondBatch, err := sqlc.New(second).ExhaustExpiredHostedReportQueueNotifications(ctx, now)
	require.NoError(t, err)
	require.Len(t, secondBatch, 32)
	for _, row := range firstBatch {
		require.NotContains(t, secondBatch, row)
	}
	require.NoError(t, second.Rollback())
	require.NoError(t, first.Rollback())
	require.NoError(t, hostedaccounts.NewReportingJobs(f.svc, nil).Tick(t.Context(), now))
	var exhausted int
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM hosted_report_queue WHERE notification_status='exhausted'`).Scan(&exhausted))
	require.Equal(t, 32, exhausted, "one tick recovers at most one bounded batch")
	results := make(chan error, 2)
	for range 2 {
		go func() { results <- hostedaccounts.NewReportingJobs(f.svc, nil).Tick(t.Context(), now) }()
	}
	require.NoError(t, <-results)
	require.NoError(t, <-results)
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM hosted_report_queue WHERE notification_status='exhausted'`).Scan(&exhausted))
	require.Equal(t, 65, exhausted)
	var events int
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM hosted_report_queue_event WHERE kind='exhausted'`).Scan(&events))
	require.Equal(t, 65, events, "competing workers record each terminal transition once")
}

func TestHostedReportingNotificationRecoveryRollsBackWithoutEvent(t *testing.T) {
	f, client, now := notificationRecoveryFixture(t, 1)
	claim := claimNotificationAttempt(t, f, now, 4)
	_, err := f.db.ExecContext(t.Context(), `CREATE FUNCTION reject_exhausted_event() RETURNS trigger LANGUAGE plpgsql AS $$
	BEGIN IF NEW.kind='exhausted' THEN RAISE EXCEPTION 'injected exhaustion event failure'; END IF; RETURN NEW; END $$;
	CREATE TRIGGER fail_exhausted_event BEFORE INSERT ON hosted_report_queue_event FOR EACH ROW EXECUTE FUNCTION reject_exhausted_event()`)
	require.NoError(t, err)
	err = hostedaccounts.NewReportingJobs(f.svc, nil).Tick(t.Context(), now.Add(time.Minute))
	require.ErrorContains(t, err, "injected exhaustion event failure")
	stored, err := sqlc.New(f.db).GetHostedReportQueue(t.Context(), sqlc.GetHostedReportQueueParams{OrganizationID: f.org, AccountID: client.ID, QueueID: claim.QueueID})
	require.NoError(t, err)
	require.Equal(t, "pending", stored.NotificationStatus)
	require.Equal(t, claim.ClaimToken, stored.ClaimToken)
	require.Equal(t, claim.LeaseUntil, stored.LeaseUntil)
	_, err = f.db.ExecContext(t.Context(), `DROP TRIGGER fail_exhausted_event ON hosted_report_queue_event`)
	require.NoError(t, err)
	require.NoError(t, hostedaccounts.NewReportingJobs(f.svc, nil).Tick(t.Context(), now.Add(time.Minute)))
	page, err := f.svc.HostReportQueue(t.Context(), f.host, f.org, 100, "")
	require.NoError(t, err)
	require.Equal(t, "exhausted", page.Items[0].NotificationStatus)
}

func TestHostedReportingNotificationRecoveryPreservesNewGeneration(t *testing.T) {
	f, client, now := notificationRecoveryFixture(t, 1)
	claim := claimNotificationAttempt(t, f, now, 4)
	q := sqlc.New(f.db)
	current, err := q.UpsertHostedReportQueue(t.Context(), sqlc.UpsertHostedReportQueueParams{QueueID: claim.QueueID, OrganizationID: f.org, AccountID: client.ID, WorkKey: claim.WorkKey, OwnerID: claim.OwnerID, SiteIds: claim.SiteIds, Projection: claim.Projection, Fingerprint: claim.Fingerprint + ":new-generation", NotificationStatus: "pending", NowAt: now})
	require.NoError(t, err)
	require.Equal(t, claim.Generation+1, current.Generation)
	recovered, err := q.ExhaustExpiredHostedReportQueueNotifications(t.Context(), now.Add(time.Minute))
	require.NoError(t, err)
	require.Empty(t, recovered, "the prior generation's fifth attempt does not exhaust fresh work")
	newClaim, err := q.ClaimHostedReportQueueNotification(t.Context(), sqlc.ClaimHostedReportQueueNotificationParams{NowAt: now, ClaimToken: sql.NullString{String: "new-process", Valid: true}, LeaseUntil: sql.NullTime{Time: now.Add(2 * time.Minute), Valid: true}})
	require.NoError(t, err)
	require.Equal(t, current.Generation, newClaim.Generation)
	require.EqualValues(t, 1, newClaim.Attempts)
	changed, err := q.CompleteHostedReportQueueNotification(t.Context(), sqlc.CompleteHostedReportQueueNotificationParams{ID: claim.ID, Generation: claim.Generation, ClaimToken: claim.ClaimToken, NotificationStatus: "sent", RetryAt: now, NowAt: now})
	require.NoError(t, err)
	require.Zero(t, changed)
	stored, err := q.GetHostedReportQueue(t.Context(), sqlc.GetHostedReportQueueParams{OrganizationID: f.org, AccountID: client.ID, QueueID: claim.QueueID})
	require.NoError(t, err)
	require.Equal(t, newClaim.Generation, stored.Generation)
	require.Equal(t, newClaim.ClaimToken, stored.ClaimToken)
	require.Equal(t, "pending", stored.NotificationStatus)
}
