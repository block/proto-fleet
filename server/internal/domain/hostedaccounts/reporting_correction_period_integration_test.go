package hostedaccounts_test

import (
	"testing"
	"time"

	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/stretchr/testify/require"
)

func TestHostedReportAdHocCorrectionNormalizesPeriodPrecision(t *testing.T) {
	f, client, owner, in, p := reportContextFixture(t)
	saved, err := saveReportingInputsForTest(f.svc, t.Context(), f.host, f.org, client.AccountID, 0, "reviewed inputs", in)
	require.NoError(t, err)
	p.Kind = "ad_hoc"
	p.Start = p.Start.Add(789 * time.Nanosecond)
	p.End = p.End.Add(123 * time.Nanosecond)
	p.RequestReceivedAt = p.End.Add(987 * time.Nanosecond)
	request, err := f.svc.CreateReportRequest(t.Context(), f.host, f.org, client.AccountID, hostedaccounts.ReportRequest{ReceivedAt: p.RequestReceivedAt, SourceRef: "request/email", Requester: "contract representative", Scope: "correction precision", OwnerUserID: owner})
	require.NoError(t, err)
	p.RequestRef = request.RequestID
	opts := hostedaccounts.DraftReportOptions{Period: p, InputRevision: saved.Revision, OwnerUserID: owner}
	draft, err := f.svc.PrepareReport(t.Context(), f.host, f.org, client.AccountID, opts)
	require.NoError(t, err)
	require.Equal(t, p.Start.Round(time.Microsecond), draft.Period.Start)
	require.Equal(t, p.End.Round(time.Microsecond), draft.Period.End)
	require.Equal(t, p.RequestReceivedAt.Round(time.Microsecond), draft.Period.RequestReceivedAt)
	issued, err := f.svc.PublishReport(t.Context(), f.host, f.org, client.AccountID, draft.ReportID, draft.Revision, "reviewed incomplete sources")
	require.NoError(t, err)
	opts.CorrectionOf = issued.ReportID
	opts.CorrectionReason = "late evidence"
	correction, err := f.svc.PrepareReport(t.Context(), f.host, f.org, client.AccountID, opts)
	require.NoError(t, err)
	opts.Period = correction.Period
	opts.ReportID, opts.ExpectedRevision = correction.ReportID, correction.Revision
	opts.CorrectionOf, opts.CorrectionReason = "", ""
	regenerated, err := f.svc.PrepareReport(t.Context(), f.host, f.org, client.AccountID, opts)
	require.NoError(t, err)
	_, err = f.svc.PublishReport(t.Context(), f.host, f.org, client.AccountID, regenerated.ReportID, regenerated.Revision, "reviewed correction")
	require.NoError(t, err)
}

func TestHostedReportCorrectionRegenerationRetainsParentPeriod(t *testing.T) {
	f, client, reviewer, draft := draftReportFixture(t)
	original, err := f.svc.PublishReport(t.Context(), reviewer, f.org, client.AccountID, draft.ReportID, draft.Revision, "reviewed incomplete source")
	require.NoError(t, err)
	var owner string
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT user_id FROM "user" WHERE id=$1`, f.host).Scan(&owner))
	options := hostedaccounts.DraftReportOptions{Period: reportPeriod(), InputRevision: 1, OwnerUserID: owner, CorrectionOf: original.ReportID, CorrectionReason: "late evidence"}
	correction, err := f.svc.PrepareReport(t.Context(), f.host, f.org, client.AccountID, options)
	require.NoError(t, err)
	options.CorrectionOf = ""
	options.CorrectionReason = ""
	options.ReportID = correction.ReportID
	options.ExpectedRevision = correction.Revision
	for _, candidate := range []struct {
		kind       string
		start, end time.Time
	}{
		{"monthly", time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)},
		{"quarterly", time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)},
	} {
		options.Period.Kind = candidate.kind
		options.Period.Start = candidate.start
		options.Period.End = candidate.end
		_, err = f.svc.PrepareReport(t.Context(), f.host, f.org, client.AccountID, options)
		require.ErrorContains(t, err, "correction must retain the original report period")
	}
	unchanged, err := f.svc.HostReport(t.Context(), reviewer, f.org, client.AccountID, correction.ReportID)
	require.NoError(t, err)
	require.Equal(t, correction.Revision, unchanged.Revision)
	require.Equal(t, correction.Period, unchanged.Period)
	require.Equal(t, correction.CorrectionOf, unchanged.CorrectionOf)
}
