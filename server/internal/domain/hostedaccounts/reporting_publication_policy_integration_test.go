package hostedaccounts_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts/reporting"
	"github.com/stretchr/testify/require"
)

func TestHostedReportPrepareAndPublishBusyDeviceAreAtomic(t *testing.T) {
	for _, operation := range []string{"prepare", "publish"} {
		t.Run(operation, func(t *testing.T) {
			f := setup(t)
			client := f.client(t, "Busy publication device", f.user, hostedaccounts.AllCapabilities)
			owner, publisher := reportActors(t, f, client.AccountID)
			input, _, _ := currentBoardReportingInputs(t, f, client.AccountID)
			savedReportInputs(t, f, client.AccountID, f.host, input, 0)
			c := input.Coverage[0]
			p := reporting.Period{Kind: "ad_hoc", Start: c.Start, End: c.End, AsOf: c.End, Timezone: "UTC"}
			request, err := f.svc.CreateReportRequest(t.Context(), f.host, f.org, client.AccountID, hostedaccounts.ReportRequest{ReceivedAt: c.Start, SourceRef: "recorded client request", Requester: "Client", Scope: "Busy-device report", OwnerUserID: owner})
			require.NoError(t, err)
			p.RequestRef, p.RequestReceivedAt = request.RequestID, request.ReceivedAt
			opts := hostedaccounts.DraftReportOptions{Period: p, InputRevision: 1, OwnerUserID: owner}
			var draft hostedaccounts.ReportRecord
			if operation == "publish" {
				draft, err = f.svc.PrepareReport(t.Context(), f.host, f.org, client.AccountID, opts)
				require.NoError(t, err)
			}
			snapshot := func() string {
				var value string
				require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT jsonb_build_object(
 'reports',(SELECT COALESCE(jsonb_agg(to_jsonb(r) ORDER BY id),'[]') FROM hosted_report r),
 'artifacts',(SELECT COALESCE(jsonb_agg(to_jsonb(a) ORDER BY id),'[]') FROM hosted_report_artifact a),
 'chunks',(SELECT COALESCE(jsonb_agg(to_jsonb(c) ORDER BY artifact_id,sequence),'[]') FROM hosted_report_artifact_chunk c),
 'audit',(SELECT COALESCE(jsonb_agg(to_jsonb(a) ORDER BY event_id),'[]') FROM activity_log a WHERE event_type IN ('prepare_report','publish_report'))
 )::text`).Scan(&value))
				return value
			}
			before := snapshot()
			tx, err := f.db.BeginTx(t.Context(), nil)
			require.NoError(t, err)
			defer tx.Rollback()
			_, err = tx.ExecContext(t.Context(), `SELECT id FROM device WHERE id=$1 FOR UPDATE`, c.ContainerID)
			require.NoError(t, err)
			ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
			defer cancel()
			if operation == "prepare" {
				_, err = f.svc.PrepareReport(ctx, f.host, f.org, client.AccountID, opts)
			} else {
				_, err = f.svc.PublishReport(ctx, publisher, f.org, client.AccountID, draft.ReportID, draft.Revision, "reviewed limitations")
			}
			require.ErrorContains(t, err, "reporting history changed")
			require.NoError(t, ctx.Err(), "device NOWAIT must finish before the caller deadline")
			require.Equal(t, before, snapshot(), "failed admission cannot alter publication state")
			require.NoError(t, tx.Rollback())
			if operation == "prepare" {
				_, err = f.svc.PrepareReport(t.Context(), f.host, f.org, client.AccountID, opts)
			} else {
				_, err = f.svc.PublishReport(t.Context(), publisher, f.org, client.AccountID, draft.ReportID, draft.Revision, "reviewed limitations")
			}
			require.NoError(t, err, "same revision retries after physical writer releases the device")
		})
	}
}

func TestHostedReportPreviousPolicyHTMLAndZIPRemainFrozenAndHostOnly(t *testing.T) {
	f, client, publisher, draft := draftReportFixture(t)
	q := sqlc.New(f.db)
	old := map[string][]byte{"report.html": []byte("<p>PRIVATE-OLD-CONTROL</p>"), "evidence.zip": []byte("previous policy archive")}
	for name, data := range old {
		media := "text/html; charset=utf-8"
		if name == "evidence.zip" {
			media = `application/zip; profile="hosted-report-evidence/v3"`
		}
		artifact, err := q.CreateHostedReportArtifact(t.Context(), sqlc.CreateHostedReportArtifactParams{ReportInternalID: draft.ID, Name: name, MediaType: media})
		require.NoError(t, err)
		require.NoError(t, q.AppendHostedReportArtifactChunk(t.Context(), sqlc.AppendHostedReportArtifactChunkParams{ArtifactID: artifact.ID, Sequence: 0, Data: data}))
		digest := sha256.Sum256(data)
		_, err = q.FinalizeHostedReportArtifact(t.Context(), sqlc.FinalizeHostedReportArtifactParams{ID: artifact.ID, Sha256: hex.EncodeToString(digest[:]), ByteCount: int64(len(data)), ChunkCount: 1})
		require.NoError(t, err)
	}
	issueStoredReport(t, f, draft, publisher)
	for name, data := range old {
		_, err := f.svc.ReportArtifactPage(t.Context(), f.user, f.org, client.AccountID, draft.ReportID, name, 0, false)
		require.ErrorContains(t, err, "Prepare and publish a correction")
		host, err := f.svc.ReportArtifactPage(t.Context(), f.host, f.org, client.AccountID, draft.ReportID, name, 0, true)
		require.NoError(t, err)
		require.Equal(t, data, host.Data, "legacy bytes remain unchanged")
	}
}
