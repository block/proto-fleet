package hostedaccounts_test

import (
	"context"
	"testing"
	"time"

	"encoding/json"

	"github.com/block/proto-fleet/server/internal/domain/alerts"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/stretchr/testify/require"
)

type reviewNotifier struct {
	result  alerts.DeliveryResult
	calls   int
	skipped [][]int64
}

func (n *reviewNotifier) DeliverTrackedSkipping(_ context.Context, _ []alerts.Alert, skipped []int64) alerts.DeliveryResult {
	n.calls++
	n.skipped = append(n.skipped, append([]int64{}, skipped...))
	return n.result
}

func notificationReviewFixture(t *testing.T) (fixture, hostedaccounts.Account, hostedaccounts.ReportRecord, int64) {
	t.Helper()
	f := setup(t)
	client := f.client(t, "Notification review", f.user, hostedaccounts.AllCapabilities)
	owner, reviewer := reportActors(t, f, client.AccountID)
	savedReportInputs(t, f, client.AccountID, reviewer, reportingInputs(t, f, client.AccountID, f.host), 0)
	report, err := f.svc.PrepareReport(t.Context(), f.host, f.org, client.AccountID, hostedaccounts.DraftReportOptions{Period: reportPeriod(), InputRevision: 1, OwnerUserID: owner})
	require.NoError(t, err)
	return f, client, report, reviewer
}

func TestReportingNotificationNoRouteStopsUntilExplicitRetry(t *testing.T) {
	f, client, _, _ := notificationReviewFixture(t)
	notifier := &reviewNotifier{}
	jobs := hostedaccounts.NewReportingJobs(f.svc, notifier)
	now := time.Now().UTC().Round(time.Microsecond)
	require.NoError(t, jobs.Tick(t.Context(), now))
	require.NoError(t, jobs.Tick(t.Context(), now.Add(2*time.Hour)))
	require.Equal(t, 1, notifier.calls)
	var status, queueID string
	var generation int64
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT notification_status,queue_id,generation FROM hosted_report_queue WHERE account_id=$1`, client.ID).Scan(&status, &queueID, &generation))
	require.Equal(t, "unrouted", status)
	require.NoError(t, f.svc.RetryReportQueueNotification(t.Context(), f.host, f.org, client.AccountID, queueID, generation, "routing configured"))
	notifier.result = alerts.DeliveryResult{Delivered: 1, DeliveredChannelIDs: []int64{42}}
	require.NoError(t, jobs.Tick(t.Context(), now.Add(3*time.Hour)))
	require.Equal(t, 2, notifier.calls)
}

func TestReportingNotificationRetriesAreBoundedAndRememberDestinations(t *testing.T) {
	f, client, _, _ := notificationReviewFixture(t)
	notifier := &reviewNotifier{result: alerts.DeliveryResult{Delivered: 1, Failed: 1, DeliveredChannelIDs: []int64{42}}}
	jobs := hostedaccounts.NewReportingJobs(f.svc, notifier)
	now := time.Now().UTC().Round(time.Microsecond)
	require.NoError(t, jobs.Tick(t.Context(), now))
	notifier.result = alerts.DeliveryResult{Failed: 1}
	for _, after := range []time.Duration{2 * time.Minute, 10 * time.Minute, 30 * time.Minute, 2 * time.Hour, 4 * time.Hour, 5 * time.Hour} {
		require.NoError(t, jobs.Tick(t.Context(), now.Add(after)))
	}
	require.Equal(t, 5, notifier.calls)
	for _, skipped := range notifier.skipped[1:] {
		require.Equal(t, []int64{42}, skipped)
	}
	var status string
	var attempts int64
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT notification_status,attempts FROM hosted_report_queue WHERE account_id=$1`, client.ID).Scan(&status, &attempts))
	require.Equal(t, "exhausted", status)
	require.EqualValues(t, 5, attempts)
}

func TestReportingDeliveredPeriodClosesDespiteMissingEvidence(t *testing.T) {
	f, client, report, reviewer := notificationReviewFixture(t)
	require.False(t, report.Result.Complete)
	report, err := f.svc.HostReport(t.Context(), reviewer, f.org, client.AccountID, report.ReportID)
	require.NoError(t, err)
	_, err = f.svc.PublishReport(t.Context(), reviewer, f.org, client.AccountID, report.ReportID, report.Revision, "reviewed missing evidence")
	require.NoError(t, err)
	sent := time.Now().UTC().Round(time.Microsecond)
	require.NoError(t, f.svc.RecordReportDelivery(t.Context(), f.host, f.org, client.AccountID, report.ReportID, hostedaccounts.DeliveryEvidence{RequestKey: report.ReportID, Channel: "agreed channel", Status: "confirmed", SentAt: &sent, SourceRef: "receipt", Evidence: "received"}))
	require.NoError(t, hostedaccounts.NewReportingJobs(f.svc, nil).Tick(t.Context(), sent))
	var status string
	var reasons []byte
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT notification_status,projection->'reasons' FROM hosted_report_queue WHERE account_id=$1`, client.ID).Scan(&status, &reasons))
	require.Equal(t, "quiet", status)
	var remaining []string
	require.NoError(t, json.Unmarshal(reasons, &remaining))
	require.Empty(t, remaining)
	visible, err := f.svc.AccountReport(analysisContext(t, f, f.user, client.AccountID), f.user, f.org, client.AccountID, report.ReportID)
	require.NoError(t, err)
	require.False(t, visible.Result.Complete, "closing work never changes frozen data-quality qualifications")
}
