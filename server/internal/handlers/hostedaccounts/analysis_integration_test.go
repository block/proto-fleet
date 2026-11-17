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
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestAccountAnalysisRPCSharedLibraryAndFrozenCSV(t *testing.T) {
	f := newRPCFixture(t)
	account := f.account(t, "Analysis customer", f.host, domain.AllCapabilities)
	name := "analysis-viewer-" + uuid.NewString()
	viewer := f.addUser(t, name, false)
	require.NoError(t, f.accounts.SetMembership(t.Context(), f.host, f.org, account.AccountID, viewer, domain.RoleViewer))
	cookie := f.login(t, name).Header().Get("Set-Cookie")
	end := time.Now().UTC().Truncate(time.Second).Add(-time.Minute)
	request := &pb.GenerateAccountAnalysisRequest{RequestId: uuid.NewString(), AccountId: account.AccountID, Name: "Energy analysis", Kind: "energy_usage", Start: timestamppb.New(end.Add(-24 * time.Hour)), End: timestamppb.New(end), Timezone: "UTC"}
	for _, selected := range []string{"", uuid.NewString()} {
		_, err := f.client.GenerateAccountAnalysis(t.Context(), rpcRequest(request, cookie, selected))
		require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err))
	}
	_, err := f.client.GenerateAccountAnalysis(t.Context(), rpcRequest(request, cookie, account.AccountID))
	require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err), "viewers cannot generate")
	created, err := f.client.GenerateAccountAnalysis(t.Context(), rpcRequest(request, f.cookie, account.AccountID))
	require.NoError(t, err)
	require.NotEmpty(t, created.Msg.Analysis.AnalysisId)
	retried, err := f.client.GenerateAccountAnalysis(t.Context(), rpcRequest(request, f.cookie, account.AccountID))
	require.NoError(t, err)
	require.True(t, proto.Equal(created.Msg.Analysis, retried.Msg.Analysis))
	require.Equal(t, "Unavailable", created.Msg.Analysis.Tables[0].Rows[0].Cells[1])
	require.NoError(t, f.accounts.ConfigureChild(t.Context(), f.host, f.org, account.AccountID, domain.AllCapabilities&^domain.GenerateReports, false))
	_, err = f.client.GenerateAccountAnalysis(t.Context(), rpcRequest(request, f.cookie, account.AccountID))
	require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err), "an admin cannot generate after capability revocation")
	listed, err := f.client.ListAccountAnalyses(t.Context(), rpcRequest(&pb.ListAccountAnalysesRequest{AccountId: account.AccountID}, cookie, account.AccountID))
	require.NoError(t, err)
	require.Len(t, listed.Msg.Analyses, 1)
	require.Empty(t, listed.Msg.Analyses[0].Tables)
	loaded, err := f.client.GetAccountAnalysis(t.Context(), rpcRequest(&pb.GetAccountAnalysisRequest{AccountId: account.AccountID, AnalysisId: created.Msg.Analysis.AnalysisId}, cookie, account.AccountID))
	require.NoError(t, err)
	require.True(t, proto.Equal(created.Msg.Analysis, loaded.Msg.Analysis))
	stream, err := f.client.ExportAccountAnalysis(t.Context(), rpcRequest(&pb.ExportAccountAnalysisRequest{AccountId: account.AccountID, AnalysisId: created.Msg.Analysis.AnalysisId}, cookie, account.AccountID))
	require.NoError(t, err)
	var data bytes.Buffer
	var digest string
	for stream.Receive() {
		data.Write(stream.Msg().Chunk)
		digest = stream.Msg().Sha256
		require.Equal(t, "text/csv; charset=utf-8", stream.Msg().MediaType)
	}
	require.NoError(t, stream.Err())
	actual := sha256.Sum256(data.Bytes())
	require.Equal(t, hex.EncodeToString(actual[:]), digest)
	require.Contains(t, data.String(), "No recorded meter inputs")
	deleteRequest := &pb.DeleteAccountAnalysisRequest{AccountId: account.AccountID, AnalysisId: created.Msg.Analysis.AnalysisId}
	_, err = f.client.DeleteAccountAnalysis(t.Context(), rpcRequest(deleteRequest, cookie, account.AccountID))
	require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err), "viewers cannot delete")
	_, err = f.client.DeleteAccountAnalysis(t.Context(), rpcRequest(deleteRequest, f.cookie, ""))
	require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err), "deletion requires selected account")
	for range 2 {
		_, err = f.client.DeleteAccountAnalysis(t.Context(), rpcRequest(deleteRequest, f.cookie, account.AccountID))
		require.NoError(t, err, "admin deletion and retry succeed")
	}
	require.NoError(t, f.accounts.ConfigureChild(t.Context(), f.host, f.org, account.AccountID, domain.AllCapabilities, false))
	_, err = f.client.GenerateAccountAnalysis(t.Context(), rpcRequest(request, f.cookie, account.AccountID))
	require.Equal(t, connect.CodeFailedPrecondition, connect.CodeOf(err), "generation retry cannot resurrect deleted contents")

	require.NoError(t, f.accounts.RevokeMembership(t.Context(), f.host, f.org, account.AccountID, viewer))
	_, err = f.client.GetAccountAnalysis(t.Context(), rpcRequest(&pb.GetAccountAnalysisRequest{AccountId: account.AccountID, AnalysisId: created.Msg.Analysis.AnalysisId}, cookie, account.AccountID))
	require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err))
}
