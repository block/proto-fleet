package hostedaccounts_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts/reporting"
	"github.com/stretchr/testify/require"
)

func TestHostedReportCancellationDuringContextSelectionIsAtomic(t *testing.T) {
	f, account, owner, in, p := reportContextFixture(t)
	saved, err := saveReportingInputsForTest(f.svc, t.Context(), f.host, f.org, account.AccountID, 0, "reviewed baseline", in)
	require.NoError(t, err)
	// Input selection reads this relation after admission, starting with the
	// device lookup that precedes device locks and before any report is created.
	// Hold selection there.
	gate, err := f.db.BeginTx(t.Context(), nil)
	require.NoError(t, err)
	defer func() { _ = gate.Rollback() }()
	_, err = gate.ExecContext(t.Context(), `LOCK TABLE hosted_reporting_fact IN ACCESS EXCLUSIVE MODE`)
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	result := make(chan error, 1)
	opts := hostedaccounts.DraftReportOptions{Period: p, InputRevision: saved.Revision, OwnerUserID: owner}
	go func() { _, err := f.svc.PrepareReport(ctx, f.host, f.org, account.AccountID, opts); result <- err }()
	require.Eventually(t, func() bool {
		var blocked bool
		err := f.db.QueryRowContext(t.Context(), `SELECT EXISTS(SELECT 1 FROM pg_locks l JOIN pg_stat_activity a USING(pid) WHERE l.database=(SELECT oid FROM pg_database WHERE datname=current_database()) AND l.relation='hosted_reporting_fact'::regclass AND NOT l.granted AND a.query LIKE '%ListReportingFactDevices%')`).Scan(&blocked)
		return err == nil && blocked
	}, 3*time.Second, 10*time.Millisecond)
	cancel()
	select {
	case err := <-result:
		require.Error(t, err)
	case <-time.After(3 * time.Second):
		t.Fatal("cancellation did not stop report context selection")
	}
	require.NoError(t, gate.Commit())
	var reports, artifacts, audits int
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM hosted_report`).Scan(&reports))
	require.Zero(t, reports)
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM hosted_report_artifact`).Scan(&artifacts))
	require.Zero(t, artifacts)
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM activity_log WHERE event_type IN ('prepare_report','publish_report')`).Scan(&audits))
	require.Zero(t, audits)
	metadata, err := f.svc.ReportingInputsMetadata(t.Context(), f.host, f.org, account.AccountID)
	require.NoError(t, err)
	require.Equal(t, saved.ReportingInputsMetadata, metadata)
	_, err = f.svc.PrepareReport(t.Context(), f.host, f.org, account.AccountID, opts)
	require.NoError(t, err, "same input revision remains usable after cancellation")
}

func TestHostedReportCancellationAfterSourceChunkIsAtomic(t *testing.T) {
	f, account, owner, in, p := reportContextFixture(t)
	c := in.Coverage[0]
	for i := range 100 {
		in.Fees = append(in.Fees, reporting.FeeLine{ID: fmt.Sprintf("fee-%03d", i), SiteID: c.SiteID, ContainerID: c.ContainerID, OrderID: c.OrderID, Start: p.Start, End: p.End, Amount: "1", Currency: "USD", SourceRef: strings.Repeat("s", 1000)})
	}
	saved, err := saveReportingInputsForTest(f.svc, t.Context(), f.host, f.org, account.AccountID, 0, "source context", in)
	require.NoError(t, err)
	draft, err := f.svc.PrepareReport(t.Context(), f.host, f.org, account.AccountID, hostedaccounts.DraftReportOptions{Period: p, InputRevision: saved.Revision, OwnerUserID: owner})
	require.NoError(t, err)
	snapshot := func() string {
		var value string
		require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT to_jsonb(r)::text FROM hosted_report r WHERE id=$1`, draft.ID).Scan(&value))
		return value
	}
	before := snapshot()
	_, err = f.db.ExecContext(t.Context(), `CREATE FUNCTION gate_second_source_chunk() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.sequence=1 THEN IF NOT EXISTS(SELECT 1 FROM hosted_report_artifact_chunk WHERE artifact_id=NEW.artifact_id AND sequence=0 AND octet_length(data)=65536) THEN RAISE EXCEPTION 'first source chunk was not written'; END IF; PERFORM pg_advisory_xact_lock(19763,1); END IF; RETURN NEW; END $$; CREATE TRIGGER gate_source_chunk BEFORE INSERT ON hosted_report_artifact_chunk FOR EACH ROW EXECUTE FUNCTION gate_second_source_chunk()`)
	require.NoError(t, err)
	gate, err := f.db.BeginTx(t.Context(), nil)
	require.NoError(t, err)
	defer func() { _ = gate.Rollback() }()
	_, err = gate.ExecContext(t.Context(), `SELECT pg_advisory_xact_lock(19763,1)`)
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	result := make(chan error, 1)
	go func() {
		_, err := f.svc.PublishReport(ctx, f.host, f.org, account.AccountID, draft.ReportID, draft.Revision, "reviewed limitations")
		result <- err
	}()
	require.Eventually(t, func() bool {
		var blocked bool
		err := f.db.QueryRowContext(t.Context(), `SELECT EXISTS(SELECT 1 FROM pg_locks WHERE locktype='advisory' AND classid=19763 AND objid=1 AND NOT granted AND database=(SELECT oid FROM pg_database WHERE datname=current_database()))`).Scan(&blocked)
		return err == nil && blocked
	}, 3*time.Second, 10*time.Millisecond)
	cancel()
	select {
	case err := <-result:
		require.Error(t, err)
	case <-time.After(3 * time.Second):
		t.Fatal("cancellation did not stop source artifact writing")
	}
	require.NoError(t, gate.Commit())
	require.Equal(t, before, snapshot(), "cancellation preserves the exact reviewed draft")
	var artifacts, chunks, audits int
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM hosted_report_artifact`).Scan(&artifacts))
	require.Zero(t, artifacts)
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM hosted_report_artifact_chunk`).Scan(&chunks))
	require.Zero(t, chunks)
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM activity_log WHERE event_type='publish_report'`).Scan(&audits))
	require.Zero(t, audits)
	_, err = f.db.ExecContext(t.Context(), `DROP TRIGGER gate_source_chunk ON hosted_report_artifact_chunk`)
	require.NoError(t, err)
	issued, err := f.svc.PublishReport(t.Context(), f.host, f.org, account.AccountID, draft.ReportID, draft.Revision, "reviewed limitations")
	require.NoError(t, err)
	require.Equal(t, "issued", issued.Status)
}
