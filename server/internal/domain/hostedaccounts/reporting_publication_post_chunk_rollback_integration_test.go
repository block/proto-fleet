package hostedaccounts_test

import (
	"fmt"
	"testing"

	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

// The test database is private to setup(t). Its narrowly scoped audit CHECK
// deliberately fails only for this draft's publication. The function queries
// the publication transaction's own writes, so its exception proves the
// source artifact was finalized with actual chunks before the rollback.
func TestHostedReportPublicationRollsBackAfterWrittenChunk(t *testing.T) {
	f, client, publisher, oldDraft := draftReportFixture(t)
	old, err := f.svc.PublishReport(t.Context(), publisher, f.org, client.AccountID, oldDraft.ReportID, oldDraft.Revision, "reviewed exception")
	require.NoError(t, err)
	require.Equal(t, "issued", old.Status)
	oldBytes := map[string][]byte{}
	for _, name := range []string{"source-inputs.json", "report.html", "evidence.zip"} {
		actor := f.user
		if name == "source-inputs.json" {
			actor = publisher
		}
		oldBytes[name] = downloadReportArtifact(t, f, actor, client.AccountID, old.ReportID, name)
		require.NotEmpty(t, oldBytes[name], name)
	}

	var owner string
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT user_id FROM "user" WHERE id=$1`, f.host).Scan(&owner))
	period := oldDraft.Period
	period.Start = period.Start.AddDate(0, -1, 0)
	period.End = oldDraft.Period.Start
	draft, err := f.svc.PrepareReport(t.Context(), f.host, f.org, client.AccountID, hostedaccounts.DraftReportOptions{Period: period, InputRevision: oldDraft.InputRevision, OwnerUserID: owner})
	require.NoError(t, err)
	require.Equal(t, "draft", draft.Status)

	snapshot := func(query string, args ...any) string {
		t.Helper()
		var value string
		require.NoError(t, f.db.QueryRowContext(t.Context(), query, args...).Scan(&value))
		return value
	}
	draftRow := snapshot(`SELECT to_jsonb(r)::text FROM hosted_report r WHERE id=$1`, draft.ID)
	oldRow := snapshot(`SELECT to_jsonb(r)::text FROM hosted_report r WHERE id=$1`, old.ID)
	artifactRows := snapshot(`SELECT COALESCE(jsonb_agg(to_jsonb(a) ORDER BY id),'[]'::jsonb)::text FROM hosted_report_artifact a`)
	chunkRows := snapshot(`SELECT COALESCE(jsonb_agg(to_jsonb(c) ORDER BY artifact_id,sequence),'[]'::jsonb)::text FROM hosted_report_artifact_chunk c`)
	audits := snapshot(`SELECT COALESCE(jsonb_agg(to_jsonb(a) ORDER BY event_id),'[]'::jsonb)::text FROM activity_log a WHERE organization_id=$1 AND event_type='publish_report'`, f.org)
	require.NotEqual(t, "[]", audits, "the genuinely issued older report has a publication audit")
	var artifactsBefore, chunksBefore, draftArtifactsBefore, draftChunksBefore int
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM hosted_report_artifact`).Scan(&artifactsBefore))
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM hosted_report_artifact_chunk`).Scan(&chunksBefore))
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM hosted_report_artifact WHERE report_internal_id=$1`, draft.ID).Scan(&draftArtifactsBefore))
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM hosted_report_artifact_chunk c JOIN hosted_report_artifact a ON a.id=c.artifact_id WHERE a.report_internal_id=$1`, draft.ID).Scan(&draftChunksBefore))
	require.Zero(t, draftArtifactsBefore)
	require.Zero(t, draftChunksBefore)

	// The report and organization selectors prevent this fault from affecting any
	// unrelated publication, even within this test's independently cloned DB.
	_, err = f.db.ExecContext(t.Context(), fmt.Sprintf(`
CREATE FUNCTION test_post_chunk_rollback_probe() RETURNS boolean LANGUAGE plpgsql AS $$
DECLARE chunks bigint; bytes bigint; report_status text;
BEGIN
 SELECT count(*), COALESCE(sum(octet_length(c.data)),0) INTO chunks, bytes
 FROM hosted_report_artifact a JOIN hosted_report_artifact_chunk c ON c.artifact_id=a.id
 WHERE a.report_internal_id=%d AND a.name='source-inputs.json'
   AND a.sha256<>'' AND a.byte_count>0 AND a.chunk_count>0;
 SELECT status INTO report_status FROM hosted_report WHERE id=%d;
 IF chunks<1 OR bytes<1 OR report_status IS DISTINCT FROM 'issued' THEN
  RAISE EXCEPTION 'rollback probe precondition failed: chunks=%% bytes=%% status=%%', chunks, bytes, report_status;
 END IF;
 RAISE EXCEPTION 'rollback probe fired after source-inputs chunks=%% bytes=%% status=%%', chunks, bytes, report_status;
END $$;
ALTER TABLE activity_log ADD CONSTRAINT test_post_chunk_rollback_check CHECK (
 CASE WHEN organization_id=%d AND event_type='publish_report' AND metadata->>'report_id'=%s
 THEN test_post_chunk_rollback_probe() ELSE true END
) NOT VALID`, draft.ID, draft.ID, f.org, pq.QuoteLiteral(draft.ReportID)))
	require.NoError(t, err)
	// Drop even when an assertion fails; the database harness also drops the
	// private test database after this cleanup.
	t.Cleanup(func() {
		_, cleanupErr := f.db.Exec(`ALTER TABLE activity_log DROP CONSTRAINT IF EXISTS test_post_chunk_rollback_check; DROP FUNCTION IF EXISTS test_post_chunk_rollback_probe()`)
		if cleanupErr != nil {
			t.Errorf("remove publication fault: %v", cleanupErr)
		}
	})

	_, err = f.svc.PublishReport(t.Context(), publisher, f.org, client.AccountID, draft.ReportID, draft.Revision, "reviewed exception")
	require.Error(t, err)
	require.Regexp(t, `rollback probe fired after source-inputs chunks=[1-9][0-9]* bytes=[1-9][0-9]* status=issued`, err.Error())
	t.Logf("deterministic failure: %v", err)
	require.Equal(t, draftRow, snapshot(`SELECT to_jsonb(r)::text FROM hosted_report r WHERE id=$1`, draft.ID), "the entire draft row must roll back")
	require.Equal(t, oldRow, snapshot(`SELECT to_jsonb(r)::text FROM hosted_report r WHERE id=$1`, old.ID))
	require.Equal(t, artifactRows, snapshot(`SELECT COALESCE(jsonb_agg(to_jsonb(a) ORDER BY id),'[]'::jsonb)::text FROM hosted_report_artifact a`))
	require.Equal(t, chunkRows, snapshot(`SELECT COALESCE(jsonb_agg(to_jsonb(c) ORDER BY artifact_id,sequence),'[]'::jsonb)::text FROM hosted_report_artifact_chunk c`))
	require.Equal(t, audits, snapshot(`SELECT COALESCE(jsonb_agg(to_jsonb(a) ORDER BY event_id),'[]'::jsonb)::text FROM activity_log a WHERE organization_id=$1 AND event_type='publish_report'`, f.org))
	var artifactsAfter, chunksAfter, draftArtifactsAfter, draftChunksAfter int
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM hosted_report_artifact`).Scan(&artifactsAfter))
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM hosted_report_artifact_chunk`).Scan(&chunksAfter))
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM hosted_report_artifact WHERE report_internal_id=$1`, draft.ID).Scan(&draftArtifactsAfter))
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM hosted_report_artifact_chunk c JOIN hosted_report_artifact a ON a.id=c.artifact_id WHERE a.report_internal_id=$1`, draft.ID).Scan(&draftChunksAfter))
	require.Equal(t, artifactsBefore, artifactsAfter)
	require.Equal(t, chunksBefore, chunksAfter)
	require.Equal(t, draftArtifactsBefore, draftArtifactsAfter)
	require.Equal(t, draftChunksBefore, draftChunksAfter)
	for name, before := range oldBytes {
		actor := f.user
		if name == "source-inputs.json" {
			actor = publisher
		}
		require.Equal(t, before, downloadReportArtifact(t, f, actor, client.AccountID, old.ReportID, name), name)
	}

	// The same revision can publish after removing the fault; a rejection before
	// the probe cannot be mistaken for successful rollback coverage.
	_, err = f.db.ExecContext(t.Context(), `ALTER TABLE activity_log DROP CONSTRAINT test_post_chunk_rollback_check; DROP FUNCTION test_post_chunk_rollback_probe()`)
	require.NoError(t, err)
	issued, err := f.svc.PublishReport(t.Context(), publisher, f.org, client.AccountID, draft.ReportID, draft.Revision, "reviewed exception")
	require.NoError(t, err)
	require.Equal(t, "issued", issued.Status)
	for name, before := range oldBytes {
		actor := f.user
		if name == "source-inputs.json" {
			actor = publisher
		}
		require.Equal(t, before, downloadReportArtifact(t, f, actor, client.AccountID, old.ReportID, name), name)
	}
}
