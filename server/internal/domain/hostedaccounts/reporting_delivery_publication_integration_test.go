package hostedaccounts_test

import (
	"testing"
	"time"

	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestReportDeliveryCannotPrecedeExactVersionPublication(t *testing.T) {
	f, client, reviewer, draft := draftReportFixture(t)
	original, err := f.svc.PublishReport(t.Context(), reviewer, f.org, client.AccountID, draft.ReportID, draft.Revision, "reviewed incomplete source")
	require.NoError(t, err)
	var owner string
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT user_id FROM "user" WHERE id=$1`, f.host).Scan(&owner))
	correction, err := f.svc.PrepareReport(t.Context(), f.host, f.org, client.AccountID, hostedaccounts.DraftReportOptions{Period: reportPeriod(), InputRevision: 1, OwnerUserID: owner, CorrectionOf: original.ReportID, CorrectionReason: "late evidence"})
	require.NoError(t, err)
	correction, err = f.svc.PublishReport(t.Context(), reviewer, f.org, client.AccountID, correction.ReportID, correction.Revision, "reviewed incomplete source")
	require.NoError(t, err)
	require.True(t, correction.IssuedAt.After(*original.IssuedAt))
	for _, report := range []hostedaccounts.ReportRecord{original, correction} {
		before := report.IssuedAt.Add(-time.Microsecond)
		for _, status := range []string{"attempted", "failed", "confirmed"} {
			evidence := hostedaccounts.DeliveryEvidence{RequestKey: uuid.NewString(), Channel: "email", Status: status, SentAt: &before, SourceRef: "provider receipt", Evidence: "delivery receipt"}
			if status == "failed" {
				evidence.FailureCategory = "unavailable"
			}
			require.ErrorContains(t, f.svc.RecordReportDelivery(t.Context(), f.host, f.org, client.AccountID, report.ReportID, evidence), "cannot precede this report version's publication")
		}
	}
	var rows, audits int
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM hosted_report_delivery`).Scan(&rows))
	require.Zero(t, rows)
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM activity_log WHERE event_type='record_report_delivery'`).Scan(&audits))
	require.Zero(t, audits)
	// The original's actual publication is still too early for its correction.
	earlier := hostedaccounts.DeliveryEvidence{RequestKey: uuid.NewString(), Channel: "email", Status: "confirmed", SentAt: original.IssuedAt, SourceRef: "old version receipt", Evidence: "delivery receipt"}
	require.Error(t, f.svc.RecordReportDelivery(t.Context(), f.host, f.org, client.AccountID, correction.ReportID, earlier))
	for _, report := range []hostedaccounts.ReportRecord{original, correction} {
		evidence := hostedaccounts.DeliveryEvidence{RequestKey: uuid.NewString(), Channel: "email", Status: "confirmed", SentAt: report.IssuedAt, ReceivedAt: report.IssuedAt, SourceRef: "exact publication receipt", Evidence: "delivery receipt"}
		require.NoError(t, f.svc.RecordReportDelivery(t.Context(), f.host, f.org, client.AccountID, report.ReportID, evidence), "equality is a valid actual send time")
		require.NoError(t, f.svc.RecordReportDelivery(t.Context(), f.host, f.org, client.AccountID, report.ReportID, evidence), "idempotent retry does not duplicate history")
	}
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM hosted_report_delivery`).Scan(&rows))
	require.Equal(t, 2, rows)
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM activity_log WHERE event_type='record_report_delivery'`).Scan(&audits))
	require.Equal(t, 2, audits)
}
