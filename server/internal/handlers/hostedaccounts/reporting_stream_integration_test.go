package hostedaccounts_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"connectrpc.com/authn"
	"connectrpc.com/connect"
	pb "github.com/block/proto-fleet/server/generated/grpc/hosted/v1"
	"github.com/block/proto-fleet/server/generated/grpc/hosted/v1/hostedv1connect"
	domain "github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts/reporting"
	"github.com/block/proto-fleet/server/internal/domain/session"
	hostedhandler "github.com/block/proto-fleet/server/internal/handlers/hostedaccounts"
	"github.com/block/proto-fleet/server/internal/handlers/interceptors"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestHostedReportStreamRechecksRevocationBeforeEveryStoredChunk(t *testing.T) {
	for _, test := range []struct {
		name, change, file string
		host, initial      bool
	}{
		{name: "viewer membership revoked", change: "membership"},
		{name: "viewer site policy narrowed", change: "policy"},
		{name: "viewer evidence ZIP membership revoked", file: "evidence.zip", change: "membership"},
		{name: "viewer evidence ZIP site policy narrowed", file: "evidence.zip", change: "policy"},
		{name: "host reporting grant narrowed", host: true, change: "grant"},
		{name: "viewer initially lacks one frozen site", initial: true, change: "policy"},
		{name: "host initially lacks one frozen site", host: true, initial: true, change: "grant"},
		{name: "source inputs denied to viewer", file: "source-inputs.json", initial: true},
		{name: "source inputs host grant revoked midstream", file: "source-inputs.json", host: true, change: "grant"},
	} {
		t.Run(test.name, func(t *testing.T) {
			file := test.file
			if file == "" {
				file = "report.html"
			}
			f := newRPCFixture(t)
			account := f.account(t, "Report stream", f.host, domain.AllCapabilities)
			rootCtx, err := domain.WithRequestAccountID(t.Context(), f.root)
			require.NoError(t, err)
			viewer := f.addUser(t, "report-stream-"+uuid.NewString(), false)
			require.NoError(t, f.accounts.SetMembership(rootCtx, f.host, f.org, account.AccountID, viewer, domain.RoleViewer))
			reviewer := f.addUser(t, "report-reviewer-"+uuid.NewString(), true)
			var reviewerID string
			require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT user_id FROM "user" WHERE id=$1`, reviewer).Scan(&reviewerID))
			require.NoError(t, f.accounts.SetReportingAuthority(t.Context(), f.host, f.org, account.AccountID, domain.ReportingAuthority{UserID: reviewerID, CanCreate: true, AllSites: true}, 0))
			sites := make([]int64, 2)
			for i := range sites {
				require.NoError(t, f.db.QueryRowContext(t.Context(), `INSERT INTO site(name,org_id,slug) VALUES('Frozen report site ' || $2,$1,$2) RETURNING id`, f.org, uuid.NewString()).Scan(&sites[i]))
			}
			// The fixture inserts a two-site frozen artifact to isolate streaming admission
			// from rendering; publication and its exact digest have separate real-DB proof.
			var reportInternal int64
			reportID := uuid.NewString()
			var input int64
			require.NoError(t, f.db.QueryRowContext(t.Context(), `INSERT INTO hosted_reporting_inputs(organization_id,account_id,revision,updated_by,source_ref,configuration,site_ids) VALUES($1,$2,1,$3,'fixture','{}',ARRAY[$4::bigint,$5::bigint]) RETURNING revision`, f.org, account.ID, f.host, sites[0], sites[1]).Scan(&input))
			require.NoError(t, f.db.QueryRowContext(t.Context(), `INSERT INTO hosted_report(report_id,organization_id,account_id,series_id,number,kind,period_start,period_end,as_of,timezone,period,input_revision,result,site_ids,created_by,owner_id,status,evidence_cutoff) VALUES($1,$2,$3,$1,1,'monthly','2026-09-01','2026-10-01',now(),'UTC','{}',$4,'{}',ARRAY[$6::bigint,$7::bigint],$5,$5,'draft',now()) RETURNING id`, reportID, f.org, account.ID, input, f.host, sites[0], sites[1]).Scan(&reportInternal))
			var artifact int64
			media := "application/json"
			if file == "report.html" {
				media = "text/html; charset=utf-8"
				_, err = f.db.ExecContext(t.Context(), `INSERT INTO hosted_report_artifact(report_internal_id,name,media_type,sha256,byte_count,chunk_count) VALUES($1,'evidence.zip',$2,repeat('2',64),0,0)`, reportInternal, reporting.PublicEvidenceMediaType)
				require.NoError(t, err)
			} else if file == "evidence.zip" {
				media = reporting.PublicEvidenceMediaType
			}
			require.NoError(t, f.db.QueryRowContext(t.Context(), `INSERT INTO hosted_report_artifact(report_internal_id,name,media_type) VALUES($1,$2,$3) RETURNING id`, reportInternal, file, media).Scan(&artifact))
			_, err = f.db.ExecContext(t.Context(), `INSERT INTO hosted_report_artifact_chunk(artifact_id,sequence,data) VALUES($1,0,'first authorized bytes'::bytea),($1,1,'must never arrive after revocation'::bytea)`, artifact)
			require.NoError(t, err)
			_, err = f.db.ExecContext(t.Context(), `UPDATE hosted_report_artifact SET sha256=repeat('1',64),byte_count=53,chunk_count=2 WHERE id=$1`, artifact)
			require.NoError(t, err)
			_, err = f.db.ExecContext(t.Context(), `UPDATE hosted_report SET status='issued',issued_at=now(),issued_by=$2 WHERE id=$1`, reportInternal, reviewer)
			require.NoError(t, err)
			changeAccess := func() {
				switch test.change {
				case "membership":
					require.NoError(t, f.accounts.RevokeMembership(rootCtx, f.host, f.org, account.AccountID, viewer))
				case "policy":
					require.NoError(t, f.accounts.ConfigureSites(rootCtx, f.host, f.org, account.AccountID, true, sites[:1]))
				case "grant":
					require.NoError(t, f.accounts.SetReportingAuthority(t.Context(), f.host, f.org, account.AccountID, domain.ReportingAuthority{UserID: reviewerID, CanCreate: true, SiteIDs: sites[:1]}, 1))
				}
				if test.change != "membership" {
					access, err := f.accounts.Resolve(t.Context(), viewer, f.org, account.AccountID)
					require.NoError(t, err)
					require.True(t, access.Has(domain.View), "site narrowing must preserve viewer membership")
				}
			}
			if test.initial {
				changeAccess()
			}
			entered, proceed := make(chan struct{}), make(chan struct{})
			release := sync.OnceFunc(func() { close(proceed) })
			_, handler := hostedv1connect.NewHostedAccountServiceHandler(hostedhandler.NewHandler(f.accounts, nil, nil, nil, nil), connect.WithInterceptors(interceptors.NewErrorMappingInterceptor()))
			actor := viewer
			if test.host {
				actor = reviewer
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				ctx := authn.SetInfo(r.Context(), &session.Info{UserID: actor, OrganizationID: f.org, AuthMethod: session.AuthMethodSession})
				if !test.host {
					var scopeErr error
					ctx, scopeErr = domain.WithRequestAccountID(ctx, account.AccountID)
					if scopeErr != nil {
						panic(scopeErr)
					}
				}
				if !test.initial {
					w = &heldActivityExportWriter{ResponseWriter: w, entered: entered, release: proceed}
				}
				handler.ServeHTTP(w, r.WithContext(ctx))
			}))
			t.Cleanup(server.Close)
			t.Cleanup(release)
			client := hostedv1connect.NewHostedAccountServiceClient(server.Client(), server.URL)
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			var receive func() bool
			var chunk func() []byte
			var streamErr func() error
			if test.host {
				stream, err := client.ExportHostReport(ctx, connect.NewRequest(&pb.ExportHostReportRequest{AccountId: account.AccountID, ReportId: reportID, Name: file}))
				require.NoError(t, err)
				receive, chunk, streamErr = stream.Receive, func() []byte { return stream.Msg().Chunk }, stream.Err
			} else {
				stream, err := client.ExportAccountReport(ctx, connect.NewRequest(&pb.ExportAccountReportRequest{AccountId: account.AccountID, ReportId: reportID, Name: file}))
				require.NoError(t, err)
				receive, chunk, streamErr = stream.Receive, func() []byte { return stream.Msg().Chunk }, stream.Err
			}
			if !test.initial {
				select {
				case <-entered:
				case <-ctx.Done():
					t.Fatal("artifact stream did not send first chunk")
				}
				require.True(t, receive())
				require.Equal(t, "first authorized bytes", string(chunk()))
				changeAccess()
				release()
			}
			require.False(t, receive(), "a multi-site artifact requires authority for every frozen site before any further bytes")
			require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(streamErr()))
		})
	}
}
