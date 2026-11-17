package hostedaccounts_test

import (
	"testing"
	"time"

	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/block/proto-fleet/server/migrations"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestHostedReportRequestFulfillmentTracksIssuedDeliveryAndRetainsReceipt(t *testing.T) {
	f := setup(t)
	client := f.client(t, "Requested reports", f.user, hostedaccounts.AllCapabilities)
	owner, reviewer := reportActors(t, f, client.AccountID)
	savedReportInputs(t, f, client.AccountID, reviewer, reportingInputs(t, f, client.AccountID, f.host), 0)
	received := time.Now().UTC().Add(-time.Hour)
	request, err := f.svc.CreateReportRequest(t.Context(), f.host, f.org, client.AccountID, hostedaccounts.ReportRequest{ReceivedAt: received, SourceRef: "client request receipt", Requester: "Client representative", Scope: "September package", OwnerUserID: owner, DeliveryRef: "unverified legacy note"})
	require.NoError(t, err)
	requests, err := f.svc.ReportRequests(t.Context(), f.host, f.org, client.AccountID, hostedaccounts.ReportRequestListOptions{})
	require.NoError(t, err)
	require.Nil(t, requests.Requests[0].FulfilledAt, "a reference alone is not confirmed fulfillment")
	p := reportPeriod()
	p.Kind = "ad_hoc"
	p.RequestRef = request.RequestID
	p.RequestReceivedAt = request.ReceivedAt
	report, err := f.svc.PrepareReport(t.Context(), f.host, f.org, client.AccountID, hostedaccounts.DraftReportOptions{Period: p, InputRevision: 1, OwnerUserID: owner})
	require.NoError(t, err)
	report, err = f.svc.HostReport(t.Context(), reviewer, f.org, client.AccountID, report.ReportID)
	require.NoError(t, err)
	report, err = f.svc.PublishReport(t.Context(), reviewer, f.org, client.AccountID, report.ReportID, report.Revision, "reviewed source limitations")
	require.NoError(t, err)
	requests, err = f.svc.ReportRequests(t.Context(), f.host, f.org, client.AccountID, hostedaccounts.ReportRequestListOptions{})
	require.NoError(t, err)
	require.Nil(t, requests.Requests[0].FulfilledAt, "publication is not delivery")
	sent := time.Now().UTC().Round(time.Microsecond)
	evidence := hostedaccounts.DeliveryEvidence{RequestKey: uuid.NewString(), Channel: "agreed channel", Status: "confirmed", SentAt: &sent, SourceRef: "delivery receipt", Evidence: "client representative receipt for the requested package"}
	require.NoError(t, f.svc.RecordReportDelivery(t.Context(), f.host, f.org, client.AccountID, report.ReportID, evidence))
	require.NoError(t, f.svc.RecordReportDelivery(t.Context(), f.host, f.org, client.AccountID, report.ReportID, evidence))
	requests, err = f.svc.ReportRequests(t.Context(), f.host, f.org, client.AccountID, hostedaccounts.ReportRequestListOptions{})
	require.NoError(t, err)
	require.NotNil(t, requests.Requests[0].FulfilledAt)
	require.True(t, sent.Equal(*requests.Requests[0].FulfilledAt))
	require.Equal(t, "delivery receipt", requests.Requests[0].FulfillmentRef)
	require.True(t, request.ReceivedAt.Equal(requests.Requests[0].ReceivedAt))
	require.Equal(t, "unverified legacy note", requests.Requests[0].DeliveryRef)
	jobs := hostedaccounts.NewReportingJobs(f.svc, nil)
	require.NoError(t, jobs.Tick(t.Context(), sent))
	queue, err := f.svc.HostReportQueue(t.Context(), f.host, f.org, 100, "")
	require.NoError(t, err)
	require.Empty(t, queue.Items, "delivered reports and fulfilled requests close active work")
	var status, delivery string
	var complete bool
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT notification_status,projection->>'deliveryStatus',(projection->>'complete')::boolean FROM hosted_report_queue WHERE account_id=$1 AND work_key=$2`, client.ID, "request:"+request.RequestID).Scan(&status, &delivery, &complete))
	require.Equal(t, "confirmed", delivery)
	require.Equal(t, "quiet", status)
	require.True(t, complete)

}

func TestHostedReportRequestManualFulfillmentIsScopedIdempotentAndAppendOnly(t *testing.T) {
	f := setup(t)
	client := f.client(t, "Manual request", f.user, hostedaccounts.AllCapabilities)
	owner, _ := reportActors(t, f, client.AccountID)
	request, err := f.svc.CreateReportRequest(t.Context(), f.host, f.org, client.AccountID, hostedaccounts.ReportRequest{ReceivedAt: time.Now().UTC().Add(-time.Hour), SourceRef: "request", Requester: "Client", Scope: "Reviewed external package", OwnerUserID: owner})
	require.NoError(t, err)
	entry := hostedaccounts.ReportRequestFulfillment{EntryKey: uuid.NewString(), DeliveredAt: time.Now().UTC(), DeliveryRef: "external package delivery", CommunicationRef: "client receipt", Evidence: "Reviewed package and confirmed delivery record"}
	future := entry
	future.EntryKey = uuid.NewString()
	future.DeliveredAt = time.Now().UTC().Add(time.Hour)
	require.Error(t, f.svc.RecordReportRequestFulfillment(t.Context(), f.host, f.org, client.AccountID, request.RequestID, future))
	other := f.client(t, "Other client", f.user, hostedaccounts.AllCapabilities)
	reportActors(t, f, other.AccountID)
	require.Error(t, f.svc.RecordReportRequestFulfillment(t.Context(), f.host, f.org, other.AccountID, request.RequestID, entry))
	require.NoError(t, f.svc.RecordReportRequestFulfillment(t.Context(), f.host, f.org, client.AccountID, request.RequestID, entry))
	require.NoError(t, f.svc.RecordReportRequestFulfillment(t.Context(), f.host, f.org, client.AccountID, request.RequestID, entry))
	entry.Evidence = "different package"
	require.Error(t, f.svc.RecordReportRequestFulfillment(t.Context(), f.host, f.org, client.AccountID, request.RequestID, entry))
	var count int
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM hosted_report_request_fulfillment`).Scan(&count))
	require.Equal(t, 1, count)
	rows, err := f.svc.ReportRequests(t.Context(), f.host, f.org, client.AccountID, hostedaccounts.ReportRequestListOptions{})
	require.NoError(t, err)
	require.NotNil(t, rows.Requests[0].FulfilledAt)
	require.Equal(t, entry.DeliveryRef, rows.Requests[0].FulfillmentRef)
	for _, statement := range []string{`UPDATE hosted_report_request_fulfillment SET evidence='rewrite'`, `DELETE FROM hosted_report_request_fulfillment`, `TRUNCATE hosted_report_request_fulfillment`} {
		_, err = f.db.ExecContext(t.Context(), statement)
		require.Error(t, err)
	}
	down, err := migrations.Current.ReadFile("current/001087_internal_hosted_report_request_fulfillment.down.sql")
	require.NoError(t, err)
	_, err = f.db.ExecContext(t.Context(), string(down))
	require.ErrorContains(t, err, "retained report request fulfillment")
}
