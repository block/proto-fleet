package hostedaccounts_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"testing"
	"time"

	"connectrpc.com/connect"
	pb "github.com/block/proto-fleet/server/generated/grpc/hosted/v1"
	domain "github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts/reporting"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestReportingRPCCreatorPublishesScopedReadAndFrozenDownload(t *testing.T) {
	f := newRPCFixture(t)
	account := f.account(t, "Report customer", f.host, domain.AllCapabilities)
	viewerName := "report-viewer-" + uuid.NewString()
	viewer := f.addUser(t, viewerName, false)
	require.NoError(t, f.accounts.SetMembership(t.Context(), f.host, f.org, account.AccountID, viewer, domain.RoleViewer))
	cookie := f.login(t, viewerName).Header().Get("Set-Cookie")
	var owner string
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT user_id FROM "user" WHERE id=$1`, f.host).Scan(&owner))
	_, err := f.client.SetHostReportingAuthority(t.Context(), rpcRequest(&pb.SetHostReportingAuthorityRequest{AccountId: account.AccountID, Authority: &pb.HostReportingAuthorityGrant{UserId: owner, CanCreate: true, AllSites: true}}, f.cookie, ""))
	require.NoError(t, err)
	config := &pb.ReportingConfiguration{Timezone: "UTC", ReportingYearMonth: 1, ReportingYearDay: 1}
	proposal := &pb.MutateHostReportingInputsRequest{AccountId: account.AccountID, SourceRef: "signed evidence inputs", Configuration: config}
	_, err = f.client.MutateHostReportingInputs(t.Context(), rpcRequest(proposal, cookie, ""))
	require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err))
	proposed, err := f.client.MutateHostReportingInputs(t.Context(), rpcRequest(proposal, f.cookie, ""))
	require.NoError(t, err)
	require.EqualValues(t, 1, proposed.Msg.Input.Revision)
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	prepared, err := f.client.PrepareHostReport(t.Context(), rpcRequest(&pb.PrepareHostReportRequest{AccountId: account.AccountID, InputRevision: 1, OwnerUserId: owner, Period: &pb.ReportingPeriod{Kind: "monthly", Start: timestamppb.New(start), End: timestamppb.New(start.AddDate(0, 1, 0)), AsOf: timestamppb.Now(), Timezone: "UTC"}}, f.cookie, ""))
	require.NoError(t, err)
	report := prepared.Msg.Report.Report
	_, err = f.client.GetAccountReport(t.Context(), rpcRequest(&pb.GetAccountReportRequest{AccountId: account.AccountID, ReportId: report.ReportId}, cookie, account.AccountID))
	require.Error(t, err, "draft is not client visible")
	publish := &pb.PublishHostReportRequest{AccountId: account.AccountID, ReportId: report.ReportId, ExpectedRevision: report.Revision, Exception: "Incomplete interim report pending contract and source certification"}
	_, err = f.client.PublishHostReport(t.Context(), rpcRequest(publish, cookie, ""))
	require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err))
	issued, err := f.client.PublishHostReport(t.Context(), rpcRequest(publish, f.cookie, ""))
	require.NoError(t, err)
	require.Equal(t, "issued", issued.Msg.Report.Report.Status)
	for _, selected := range []string{"", uuid.NewString()} {
		_, err = f.client.ListAccountReports(t.Context(), rpcRequest(&pb.ListAccountReportsRequest{AccountId: account.AccountID}, cookie, selected))
		require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err))
	}
	listed, err := f.client.ListAccountReports(t.Context(), rpcRequest(&pb.ListAccountReportsRequest{AccountId: account.AccountID}, cookie, account.AccountID))
	require.NoError(t, err)
	require.Len(t, listed.Msg.Reports, 1)
	require.False(t, listed.Msg.Reports[0].Complete)
	detail, err := f.client.GetAccountReport(t.Context(), rpcRequest(&pb.GetAccountReportRequest{AccountId: account.AccountID, ReportId: report.ReportId}, cookie, account.AccountID))
	require.NoError(t, err)
	require.NotNil(t, detail.Msg.Report.Result)
	require.Equal(t, "unavailable", detail.Msg.Report.Result.Metrics["monthly_uptime"].Status)
	stream, err := f.client.ExportAccountReport(t.Context(), rpcRequest(&pb.ExportAccountReportRequest{AccountId: account.AccountID, ReportId: report.ReportId, Name: "evidence.zip"}, cookie, account.AccountID))
	require.NoError(t, err)
	var data bytes.Buffer
	var digest string
	for stream.Receive() {
		data.Write(stream.Msg().Chunk)
		digest = stream.Msg().Sha256
		require.Equal(t, reporting.PublicEvidenceMediaType, stream.Msg().MediaType)
	}
	require.NoError(t, stream.Err())
	actual := sha256.Sum256(data.Bytes())
	require.Equal(t, hex.EncodeToString(actual[:]), digest)
	denied, err := f.client.ExportHostReport(t.Context(), rpcRequest(&pb.ExportHostReportRequest{AccountId: account.AccountID, ReportId: report.ReportId, Name: "evidence.zip"}, f.cookie, account.AccountID))
	require.NoError(t, err)
	require.False(t, denied.Receive())
	require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(denied.Err()))
	require.NoError(t, f.accounts.RevokeMembership(t.Context(), f.host, f.org, account.AccountID, viewer))
	revoked, err := f.client.ExportAccountReport(t.Context(), rpcRequest(&pb.ExportAccountReportRequest{AccountId: account.AccountID, ReportId: report.ReportId, Name: "report.html"}, cookie, account.AccountID))
	require.NoError(t, err)
	require.False(t, revoked.Receive())
	require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(revoked.Err()))
}
