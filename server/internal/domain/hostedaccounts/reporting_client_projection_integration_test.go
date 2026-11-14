package hostedaccounts_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts/reporting"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestHostedReportClientReadsOmitPrivateTicketAndDeliveryFields(t *testing.T) {
	f, client, reviewer, draft := draftReportFixture(t)
	// Isolate projection from calculation by adding a private reference to the stored draft fixture.
	draft.Result.Obligations = []reporting.ObligationDetail{{TicketRef: "host-only-ticket", EpisodeID: "reviewed episode"}}
	result, err := json.Marshal(draft.Result)
	require.NoError(t, err)
	_, err = f.db.ExecContext(t.Context(), `UPDATE hosted_report SET result=$1 WHERE id=$2`, result, draft.ID)
	require.NoError(t, err)
	issueStoredReport(t, f, draft, reviewer)
	issued, err := f.svc.HostReport(t.Context(), reviewer, f.org, client.AccountID, draft.ReportID)
	require.NoError(t, err)
	sent := time.Now().UTC()
	require.NoError(t, f.svc.RecordReportDelivery(t.Context(), f.host, f.org, client.AccountID, issued.ReportID, hostedaccounts.DeliveryEvidence{RequestKey: uuid.NewString(), Channel: "private-channel", Status: "confirmed", SentAt: &sent, SourceRef: "private-delivery-source", Evidence: "private-delivery-evidence"}))
	require.NoError(t, f.svc.RecordReportDelivery(t.Context(), f.host, f.org, client.AccountID, issued.ReportID, hostedaccounts.DeliveryEvidence{RequestKey: uuid.NewString(), Channel: "private-channel", Status: "failed", SourceRef: "later-send-log", FailureCategory: "unavailable"}))
	one, err := f.svc.AccountReport(t.Context(), f.user, f.org, client.AccountID, issued.ReportID)
	require.NoError(t, err)
	listed, err := f.svc.AccountReports(t.Context(), f.user, f.org, client.AccountID, hostedaccounts.ReportListOptions{})
	require.NoError(t, err)
	require.Len(t, listed.Reports, 1)
	require.Len(t, one.Result.Obligations, 1)
	require.Empty(t, one.Result.Obligations[0].TicketRef)
	require.Empty(t, listed.Reports[0].Result.Obligations, "list responses carry metadata only")
	for _, record := range []hostedaccounts.ReportRecord{one, listed.Reports[0]} {
		require.Zero(t, record.ID)
		require.Zero(t, record.InputRevision)
		require.Zero(t, record.CreatedBy)
		require.Zero(t, record.OwnerID)
		require.Zero(t, record.IssuedBy)
		require.Empty(t, record.Deliveries)
		require.Equal(t, "confirmed", record.DeliveryStatus)
		encoded, err := json.Marshal(record)
		require.NoError(t, err)
		for _, private := range []string{"host-only-ticket", "private-channel", "private-delivery-source", "private-delivery-evidence"} {
			require.NotContains(t, string(encoded), private)
		}
	}
	host, err := f.svc.HostReport(t.Context(), f.host, f.org, client.AccountID, issued.ReportID)
	require.NoError(t, err)
	require.Equal(t, "host-only-ticket", host.Result.Obligations[0].TicketRef)
	require.Len(t, host.Deliveries, 2)
	require.Equal(t, "failed", host.Deliveries[1].Status)
	require.Equal(t, "confirmed", host.DeliveryStatus)
	require.Equal(t, "private-delivery-evidence", host.Deliveries[0].Evidence)
}
