package hostedaccounts_test

import (
	"testing"

	pb "github.com/block/proto-fleet/server/generated/grpc/hosted/v1"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/stretchr/testify/require"
)

func TestAccountAnalysisGenerationPermissionPreservesSavedLibrary(t *testing.T) {
	f := setup(t)
	a := f.client(t, "Generation policy", f.user, hostedaccounts.AllCapabilities)
	ctx := analysisContext(t, f, f.user, a.AccountID)
	request := analysisRequest(a.AccountID, "energy_usage")
	generated, err := f.svc.GenerateAnalysis(ctx, f.user, f.org, request)
	require.NoError(t, err)
	require.NoError(t, f.svc.ConfigureChild(t.Context(), f.host, f.org, a.AccountID, hostedaccounts.AllCapabilities&^hostedaccounts.GenerateReports, false))
	_, err = f.svc.GenerateAnalysis(ctx, f.user, f.org, request)
	require.Error(t, err, "revocation also blocks a generation retry")
	_, err = f.svc.GenerateAnalysis(ctx, f.user, f.org, analysisRequest(a.AccountID, "energy_usage"))
	require.Error(t, err, "administrator membership alone cannot generate")
	access, err := f.svc.Resolve(ctx, f.user, f.org, a.AccountID)
	require.NoError(t, err)
	require.False(t, access.Has(hostedaccounts.GenerateReports))
	require.True(t, access.Has(hostedaccounts.ManageMembers))
	listed, err := f.svc.AccountAnalyses(ctx, f.user, f.org, &pb.ListAccountAnalysesRequest{AccountId: a.AccountID})
	require.NoError(t, err)
	require.Len(t, listed.Analyses, 1)
	_, err = f.svc.AccountAnalysis(ctx, f.user, f.org, a.AccountID, generated.AnalysisId)
	require.NoError(t, err)
	_, err = f.svc.AnalysisArtifactPage(ctx, f.user, f.org, a.AccountID, generated.AnalysisId, 0)
	require.NoError(t, err)
	require.NoError(t, f.svc.DeleteAnalysis(ctx, f.user, f.org, a.AccountID, generated.AnalysisId), "generation permission does not gate cleanup")
	require.NoError(t, f.svc.ConfigureChild(t.Context(), f.host, f.org, a.AccountID, hostedaccounts.AllCapabilities, false))
	_, err = f.svc.GenerateAnalysis(ctx, f.user, f.org, analysisRequest(a.AccountID, "energy_usage"))
	require.NoError(t, err, "explicit restoration permits new generation")
}

func TestAccountAnalysisGenerationPermissionFollowsParentCeiling(t *testing.T) {
	f := setup(t)
	parent := f.client(t, "Parent", f.user, hostedaccounts.AllCapabilities)
	child, err := f.svc.CreateChild(t.Context(), f.user, f.org, parent.AccountID, "Child", f.user, hostedaccounts.AllCapabilities)
	require.NoError(t, err)
	ctx := analysisContext(t, f, f.user, child.AccountID)
	_, err = f.svc.GenerateAnalysis(ctx, f.user, f.org, analysisRequest(child.AccountID, "energy_usage"))
	require.NoError(t, err)
	require.NoError(t, f.svc.ConfigureChild(t.Context(), f.host, f.org, parent.AccountID, hostedaccounts.AllCapabilities&^hostedaccounts.GenerateReports, false))
	_, err = f.svc.GenerateAnalysis(ctx, f.user, f.org, analysisRequest(child.AccountID, "energy_usage"))
	require.Error(t, err, "a child cannot bypass its parent's generation ceiling")
	children, err := f.svc.ListChildren(t.Context(), f.user, f.org, parent.AccountID)
	require.NoError(t, err)
	require.Len(t, children, 1)
	require.Zero(t, children[0].EffectiveCapabilities&hostedaccounts.GenerateReports)
	require.Error(t, f.svc.ConfigureChild(t.Context(), f.user, f.org, child.AccountID, hostedaccounts.AllCapabilities, false), "a client cannot restore a capability excluded by its parent")
	require.NoError(t, f.svc.ConfigureChild(t.Context(), f.host, f.org, parent.AccountID, hostedaccounts.AllCapabilities, false))
	_, err = f.svc.GenerateAnalysis(ctx, f.user, f.org, analysisRequest(child.AccountID, "energy_usage"))
	require.NoError(t, err)
}
