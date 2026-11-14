package hostedaccounts_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts/reporting"
	"github.com/stretchr/testify/require"
)

func TestReportingSourceStreamLaterChunkFailureRollsBackPublication(t *testing.T) {
	f, account, owner, in, p := reportContextFixture(t)
	c := in.Coverage[0]
	for i := range 100 {
		in.Fees = append(in.Fees, reporting.FeeLine{ID: fmt.Sprintf("fee-%03d", i), SiteID: c.SiteID, ContainerID: c.ContainerID, OrderID: c.OrderID, Start: p.Start, End: p.End, Amount: "1.000000000000000001", Currency: "USD", SourceRef: strings.Repeat("s", 1000)})
	}
	saved, err := saveReportingInputsForTest(f.svc, t.Context(), f.host, f.org, account.AccountID, 0, "source values", in)
	require.NoError(t, err)
	draft, err := f.svc.PrepareReport(t.Context(), f.host, f.org, account.AccountID, hostedaccounts.DraftReportOptions{Period: p, InputRevision: saved.Revision, OwnerUserID: owner})
	require.NoError(t, err)
	_, err = f.db.ExecContext(t.Context(), `CREATE FUNCTION fail_later_source_chunk() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.sequence=1 THEN RAISE EXCEPTION 'later source chunk failed'; END IF; RETURN NEW; END $$; CREATE TRIGGER fail_source_chunk BEFORE INSERT ON hosted_report_artifact_chunk FOR EACH ROW EXECUTE FUNCTION fail_later_source_chunk()`)
	require.NoError(t, err)
	_, err = f.svc.PublishReport(t.Context(), f.host, f.org, account.AccountID, draft.ReportID, draft.Revision, "reviewed source")
	require.ErrorContains(t, err, "later source chunk failed")
	var artifacts, chunks, audits int
	var status string
	var revision int64
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM hosted_report_artifact`).Scan(&artifacts))
	require.Zero(t, artifacts)
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM hosted_report_artifact_chunk`).Scan(&chunks))
	require.Zero(t, chunks)
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM activity_log WHERE event_type='publish_report'`).Scan(&audits))
	require.Zero(t, audits)
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT status,revision FROM hosted_report WHERE id=$1`, draft.ID).Scan(&status, &revision))
	require.Equal(t, "draft", status)
	require.Equal(t, draft.Revision, revision)
	_, err = f.db.ExecContext(t.Context(), `DROP TRIGGER fail_source_chunk ON hosted_report_artifact_chunk`)
	require.NoError(t, err)
	issued, err := f.svc.PublishReport(t.Context(), f.host, f.org, account.AccountID, draft.ReportID, draft.Revision, "reviewed source")
	require.NoError(t, err)
	data := downloadReportArtifact(t, f, f.host, account.AccountID, issued.ReportID, "source-inputs.json")
	require.Greater(t, len(data), 64<<10)
	var envelope struct {
		Inputs reporting.Inputs `json:"inputs"`
	}
	require.NoError(t, json.Unmarshal(data, &envelope))
	require.Equal(t, in.Fees, envelope.Inputs.Fees)
}
