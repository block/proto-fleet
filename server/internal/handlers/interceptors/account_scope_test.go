package interceptors

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"connectrpc.com/authn"
	"connectrpc.com/connect"
	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	"github.com/block/proto-fleet/server/generated/grpc/invitation/v1/invitationv1connect"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/stretchr/testify/require"
)

const selectedAccount = "32b42406-c1b1-4a02-8af6-9dfb55bd1f43"

type accountScopeRequest struct {
	connect.AnyRequest
	procedure string
}

func TestRequestAccountBinding(t *testing.T) {
	ctx, err := hostedaccounts.WithRequestAccountID(t.Context(), selectedAccount)
	require.NoError(t, err)
	id, ok := hostedaccounts.RequestAccountID(ctx)
	require.True(t, ok)
	require.Equal(t, selectedAccount, id)
	require.NoError(t, hostedaccounts.RequireRequestAccount(ctx, selectedAccount))
	require.NoError(t, hostedaccounts.RequireRequestAccount(t.Context(), selectedAccount))
	otherAccount := "9a3d1744-4079-46ec-84c4-0a718e9748ba"
	require.True(t, fleeterror.IsForbiddenError(hostedaccounts.RequireRequestAccount(ctx, otherAccount)))
	_, err = hostedaccounts.WithRequestAccountID(ctx, otherAccount)
	require.True(t, fleeterror.IsForbiddenError(err))
	for _, invalid := range []string{"", "invalid", "00000000-0000-0000-0000-000000000000", "32b42406c1b14a028af69dfb55bd1f43", "urn:uuid:" + selectedAccount} {
		require.True(t, fleeterror.IsInvalidArgumentError(hostedaccounts.RequireRequestAccount(ctx, invalid)))
	}
}

func (r accountScopeRequest) Spec() connect.Spec { return connect.Spec{Procedure: r.procedure} }

func TestSelectedAccountCannotFallBackToHostRPC(t *testing.T) {
	const legacyProcedure = "/fleetmanagement.v1.FleetManagementService/ListMinerStateSnapshots"
	const streamProcedure = "/telemetry.v1.TelemetryService/StreamCombinedMetricUpdates"
	// Even a host administrator cannot use the host transport in customer mode.
	ctx := authn.SetInfo(t.Context(), &session.Info{UserID: 1, OrganizationID: 1, Role: "ADMIN"})
	interceptor := NewAuthInterceptor(nil, nil, nil, nil, nil, nil, []string{legacyProcedure, streamProcedure}, nil, []string{legacyProcedure, streamProcedure})
	header := http.Header{}
	header.Set("X-Fleet-Account-Id", selectedAccount)

	t.Run("unary", func(t *testing.T) {
		called := false
		wrapped := interceptor.WrapUnary(func(context.Context, connect.AnyRequest) (connect.AnyResponse, error) {
			called = true
			return connect.NewResponse(&commonpb.ResourceRef{}), nil
		})
		request := connect.NewRequest(&commonpb.ResourceRef{})
		request.Header().Set("X-Fleet-Account-Id", selectedAccount)
		_, err := wrapped(ctx, accountScopeRequest{AnyRequest: request, procedure: legacyProcedure})
		require.True(t, fleeterror.IsForbiddenError(err))
		require.False(t, called)
	})
	t.Run("stream", func(t *testing.T) {
		called := false
		wrapped := interceptor.WrapStreamingHandler(func(context.Context, connect.StreamingHandlerConn) error {
			called = true
			return nil
		})
		err := wrapped(ctx, fleetNodeStreamingConn{procedure: streamProcedure, header: header})
		require.True(t, fleeterror.IsForbiddenError(err))
		require.False(t, called)
	})
	t.Run("raw HTTP", func(t *testing.T) {
		_, err := interceptor.AuthenticateRequest(ctx, header)
		require.True(t, fleeterror.IsForbiddenError(err))
	})
}

func TestRequestAccountHeaderValidation(t *testing.T) {
	const procedure = "/auth.v1.AuthService/GetUserAuditInfo"
	interceptor := NewAuthInterceptor(nil, nil, nil, nil, nil, nil, []string{procedure}, nil, nil)
	for name, header := range map[string]http.Header{
		"empty":              {hostedaccounts.RequestAccountHeader: {""}},
		"empty field":        {hostedaccounts.RequestAccountHeader: {}},
		"malformed":          {hostedaccounts.RequestAccountHeader: {"invalid"}},
		"comma joined":       {hostedaccounts.RequestAccountHeader: {selectedAccount + "," + selectedAccount}},
		"duplicate values":   {hostedaccounts.RequestAccountHeader: {selectedAccount, selectedAccount}},
		"duplicate casing":   {hostedaccounts.RequestAccountHeader: {selectedAccount}, "x-fleet-account-id": {selectedAccount}},
		"zero UUID":          {hostedaccounts.RequestAccountHeader: {"00000000-0000-0000-0000-000000000000"}},
		"leading whitespace": {hostedaccounts.RequestAccountHeader: {" " + selectedAccount}},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := interceptor.authenticate(t.Context(), procedure, header)
			require.True(t, fleeterror.IsInvalidArgumentError(err))
		})
	}
	ctx, err := interceptor.authenticate(t.Context(), procedure, http.Header{hostedaccounts.RequestAccountHeader: {selectedAccount}})
	require.NoError(t, err)
	accountID, selected := hostedaccounts.RequestAccountID(ctx)
	require.True(t, selected)
	require.Equal(t, selectedAccount, accountID)
}

func TestRequestAccountCapturedIndependentlyOfHeaders(t *testing.T) {
	const procedure = "/auth.v1.AuthService/VerifyCredentials"
	interceptor := NewAuthInterceptor(nil, nil, nil, nil, nil, nil, []string{procedure}, nil, nil)
	header := http.Header{hostedaccounts.RequestAccountHeader: {selectedAccount}}
	ctx, err := interceptor.authenticate(t.Context(), procedure, header)
	require.NoError(t, err)
	header.Set(hostedaccounts.RequestAccountHeader, "9a3d1744-4079-46ec-84c4-0a718e9748ba")
	id, ok := hostedaccounts.RequestAccountID(ctx)
	require.True(t, ok)
	require.Equal(t, selectedAccount, id)
}

func TestSelectedAccountRejectsOtherAuthAndUnknownHostedMethods(t *testing.T) {
	for _, procedure := range []string{
		"/auth.v1.AuthService/ListUsers",
		"/auth.v1.AuthService/UpdateUsername",
		"/auth.v1.AuthService/Authenticate",
		"/auth.v1.AuthService/ResetUserPassword",
		invitationv1connect.InvitationServicePreviewInvitationProcedure,
		invitationv1connect.InvitationServiceRegisterInvitedUserProcedure,
		invitationv1connect.InvitationServiceAcceptInvitationProcedure,
		invitationv1connect.InvitationServiceCreateTeamInvitationProcedure,
		hostedAccountProcedurePrefix + "FutureMethod",
	} {
		t.Run(procedure, func(t *testing.T) {
			interceptor := NewAuthInterceptor(nil, nil, nil, nil, nil, nil, []string{procedure}, nil, []string{procedure})
			_, err := interceptor.authenticate(t.Context(), procedure, http.Header{hostedaccounts.RequestAccountHeader: {selectedAccount}})
			require.True(t, fleeterror.IsForbiddenError(err))
		})
	}
}

func TestHostedAdministrationRejectsOrganizationAPIKeys(t *testing.T) {
	for _, procedure := range SessionOnlyProcedures {
		if !strings.HasPrefix(procedure, hostedAccountProcedurePrefix) {
			continue
		}
		t.Run(procedure, func(t *testing.T) {
			// Even an accidental exemption must not let an organization key in.
			interceptor := NewAuthInterceptor(nil, nil, nil, nil, nil, nil, []string{procedure}, nil, []string{procedure})
			_, err := interceptor.authenticate(t.Context(), procedure, http.Header{"Authorization": {"Bearer organization-key"}})
			require.True(t, fleeterror.IsForbiddenError(err))
		})
	}
}

func TestInvitationOnboardingRemainsUnscoped(t *testing.T) {
	interceptor := NewAuthInterceptor(nil, nil, nil, nil, nil, nil, UnauthenticatedProcedures, SessionOnlyProcedures, nil)
	for _, procedure := range []string{invitationv1connect.InvitationServicePreviewInvitationProcedure, invitationv1connect.InvitationServiceRegisterInvitedUserProcedure} {
		ctx, err := interceptor.authenticate(t.Context(), procedure, http.Header{})
		require.NoError(t, err)
		_, selected := hostedaccounts.RequestAccountID(ctx)
		require.False(t, selected)
	}
	for _, procedure := range []string{invitationv1connect.InvitationServiceAcceptInvitationProcedure, invitationv1connect.InvitationServiceCreateTeamInvitationProcedure} {
		_, err := interceptor.authenticate(t.Context(), procedure, http.Header{})
		require.True(t, fleeterror.IsAuthenticationError(err))
	}
}

func TestSelectedAccountCannotControlHostCurtailment(t *testing.T) {
	for _, procedure := range []string{
		"/curtailment.v1.CurtailmentService/StartCurtailment",
		"/curtailment.v1.CurtailmentService/StopCurtailment",
		"/curtailment.v1.CurtailmentService/UpdateCurtailmentEvent",
	} {
		t.Run(procedure, func(t *testing.T) {
			interceptor := NewAuthInterceptor(nil, nil, nil, nil, nil, nil, []string{procedure}, nil, []string{procedure})
			request := connect.NewRequest(&commonpb.ResourceRef{})
			request.Header().Set(hostedaccounts.RequestAccountHeader, selectedAccount)
			called := false
			wrapped := interceptor.WrapUnary(func(context.Context, connect.AnyRequest) (connect.AnyResponse, error) { called = true; return nil, nil })
			_, err := wrapped(t.Context(), accountScopeRequest{AnyRequest: request, procedure: procedure})
			require.True(t, fleeterror.IsForbiddenError(err))
			require.False(t, called)
		})
	}
}

func TestMiningMethodsAreExplicitlySessionOnlyAndAccountScoped(t *testing.T) {
	for _, method := range []string{"ListAccountActivities", "ExportAccountActivities", "GetAccountDashboard", "GetAccountAssetDetail", "GetAccountMiningSummary", "ListAccountAssets", "AssignAccountAssets", "ReplaceAccountHashboard"} {
		procedure := hostedAccountProcedurePrefix + method
		require.True(t, requiresSelectedAccount(procedure))
		require.True(t, allowsSelectedAccount(procedure))
		require.False(t, allowsAccountKey(procedure))
		require.Contains(t, SessionOnlyProcedures, procedure)
	}
}

func TestReportingMethodsKeepClientScopeAndHostAuthoritySeparate(t *testing.T) {
	for _, method := range []string{"ListAccountReports", "GetAccountReport", "ExportAccountReport", "GenerateAccountAnalysis", "DeleteAccountAnalysis", "ListAccountAnalyses", "GetAccountAnalysis", "ExportAccountAnalysis"} {
		procedure := hostedAccountProcedurePrefix + method
		require.True(t, requiresSelectedAccount(procedure))
		require.True(t, allowsSelectedAccount(procedure))
		require.False(t, allowsAccountKey(procedure))
		require.Contains(t, SessionOnlyProcedures, procedure)
		require.True(t, SensitiveBodyProcedures[procedure])
	}
	for _, method := range []string{"ListHostReportingAccounts", "GetHostReportingSetup", "SetHostReportingAuthority", "MutateHostReportingInputs", "ListHostReportingFacts", "PrepareHostReport", "PublishHostReport", "ListHostReports", "GetHostReport", "RecordHostReportDelivery", "ExportHostReport", "CreateHostReportRequest", "RecordHostReportRequestFulfillment", "ListHostReportRequests", "ListHostReportQueue", "AcknowledgeHostReportQueue", "RetryHostReportQueueNotification"} {
		procedure := hostedAccountProcedurePrefix + method
		require.False(t, requiresSelectedAccount(procedure))
		require.False(t, allowsSelectedAccount(procedure))
		require.False(t, allowsAccountKey(procedure))
		require.Contains(t, SessionOnlyProcedures, procedure)
		require.True(t, SensitiveBodyProcedures[procedure])
	}
}
