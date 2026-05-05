package interceptors

import (
	"github.com/block/proto-fleet/server/generated/grpc/agentadmin/v1/agentadminv1connect"
	"github.com/block/proto-fleet/server/generated/grpc/agentgateway/v1/agentgatewayv1connect"
	"github.com/block/proto-fleet/server/generated/grpc/apikey/v1/apikeyv1connect"
	"github.com/block/proto-fleet/server/generated/grpc/auth/v1/authv1connect"
	"github.com/block/proto-fleet/server/generated/grpc/fleetmanagement/v1/fleetmanagementv1connect"
	"github.com/block/proto-fleet/server/generated/grpc/foremanimport/v1/foremanimportv1connect"
	"github.com/block/proto-fleet/server/generated/grpc/minercommand/v1/minercommandv1connect"
	"github.com/block/proto-fleet/server/generated/grpc/onboarding/v1/onboardingv1connect"
)

// RedactedRequestProcedures lists procedures whose requests contain secrets
// (passwords, pool credentials) and must not be logged at debug level.
var RedactedRequestProcedures = []string{
	authv1connect.AuthServiceAuthenticateProcedure,
	authv1connect.AuthServiceUpdatePasswordProcedure,
	authv1connect.AuthServiceVerifyCredentialsProcedure,
	fleetmanagementv1connect.FleetManagementServiceUpdateWorkerNamesProcedure,
	onboardingv1connect.OnboardingServiceCreateAdminLoginProcedure,
	minercommandv1connect.MinerCommandServiceUpdateMiningPoolsProcedure,
	minercommandv1connect.MinerCommandServiceUpdateMinerPasswordProcedure,
	agentgatewayv1connect.AgentGatewayServiceRegisterProcedure,
	agentgatewayv1connect.AgentGatewayServiceBeginAuthHandshakeProcedure,
	agentgatewayv1connect.AgentGatewayServiceCompleteAuthHandshakeProcedure,
	agentgatewayv1connect.AgentGatewayServiceUploadHeartbeatProcedure,
}

// RedactedResponseProcedures lists procedures whose responses contain secrets
// (API keys, temporary passwords) and must not be logged at debug level.
var RedactedResponseProcedures = []string{
	apikeyv1connect.ApiKeyServiceCreateApiKeyProcedure,
	authv1connect.AuthServiceCreateUserProcedure,
	authv1connect.AuthServiceResetUserPasswordProcedure,
	agentgatewayv1connect.AgentGatewayServiceCompleteAuthHandshakeProcedure,
	agentadminv1connect.AgentAdminServiceCreateEnrollmentCodeProcedure,
	agentadminv1connect.AgentAdminServiceConfirmAgentProcedure,
}

// SessionOnlyProcedures lists procedures that require session-cookie auth and
// must reject API-key auth. This covers all credential and user management
// endpoints to prevent a leaked API key from escalating to interactive
// credentials or modifying user accounts.
var SessionOnlyProcedures = []string{
	// API key lifecycle — prevents self-replication from a leaked key
	apikeyv1connect.ApiKeyServiceCreateApiKeyProcedure,
	apikeyv1connect.ApiKeyServiceListApiKeysProcedure,
	apikeyv1connect.ApiKeyServiceRevokeApiKeyProcedure,
	// Auth and user management endpoints remain session-only to keep interactive
	// account management scoped to an authenticated browser session.
	// Note: Logout is NOT listed here — it has its own FailedPrecondition guard
	// in the handler that returns a more actionable error message.
	authv1connect.AuthServiceGetUserAuditInfoProcedure,
	authv1connect.AuthServiceUpdatePasswordProcedure,
	authv1connect.AuthServiceUpdateUsernameProcedure,
	authv1connect.AuthServiceCreateUserProcedure,
	authv1connect.AuthServiceListUsersProcedure,
	authv1connect.AuthServiceResetUserPasswordProcedure,
	authv1connect.AuthServiceDeactivateUserProcedure,
	authv1connect.AuthServiceVerifyCredentialsProcedure,
	// AgentAdminService mints credentials (enrollment codes, agent api_keys)
	// and exposes operator-only fleet metadata. Restrict to interactive
	// browser sessions so a leaked user api_key cannot bootstrap rogue
	// agents.
	agentadminv1connect.AgentAdminServiceCreateEnrollmentCodeProcedure,
	agentadminv1connect.AgentAdminServiceListAgentsProcedure,
	agentadminv1connect.AgentAdminServiceConfirmAgentProcedure,
}

var UnauthenticatedProcedures = []string{
	"/health",
	"/grpc.reflection.v1alpha.ServerReflection/ServerReflectionInfo",
	authv1connect.AuthServiceAuthenticateProcedure,
	onboardingv1connect.OnboardingServiceCreateAdminLoginProcedure,
	onboardingv1connect.OnboardingServiceGetFleetInitStatusProcedure,
	// Bootstrap RPCs: the agent has no session_token yet. Register validates
	// an enrollment_token in the body; the handshake validates an api_key.
	agentgatewayv1connect.AgentGatewayServiceRegisterProcedure,
	agentgatewayv1connect.AgentGatewayServiceBeginAuthHandshakeProcedure,
	agentgatewayv1connect.AgentGatewayServiceCompleteAuthHandshakeProcedure,
}

// AgentAuthenticatedProcedures lists procedures gated by AgentAuthInterceptor
// (Authorization: Bearer <session_token>). The user-session AuthInterceptor
// short-circuits these so the two interceptors don't fight over the same
// procedure.
var AgentAuthenticatedProcedures = []string{
	agentgatewayv1connect.AgentGatewayServiceUploadTelemetryProcedure,
	agentgatewayv1connect.AgentGatewayServiceUploadEventsProcedure,
	agentgatewayv1connect.AgentGatewayServiceUploadHeartbeatProcedure,
	agentgatewayv1connect.AgentGatewayServiceControlStreamProcedure,
}

// SensitiveBodyProcedures lists RPCs whose request/response bodies must not be
// logged, even at debug level, because they contain secrets (e.g., API keys).
// For streaming RPCs, this also suppresses individual message bodies in
// loggingStreamingHandlerConn.
var SensitiveBodyProcedures = map[string]bool{
	foremanimportv1connect.ForemanImportServiceImportFromForemanProcedure:     true,
	foremanimportv1connect.ForemanImportServiceCompleteImportProcedure:        true,
	authv1connect.AuthServiceAuthenticateProcedure:                            true,
	authv1connect.AuthServiceVerifyCredentialsProcedure:                       true,
	fleetmanagementv1connect.FleetManagementServiceUpdateWorkerNamesProcedure: true,
	agentgatewayv1connect.AgentGatewayServiceControlStreamProcedure:           true,
	agentgatewayv1connect.AgentGatewayServiceUploadTelemetryProcedure:         true,
	agentgatewayv1connect.AgentGatewayServiceUploadEventsProcedure:            true,
}
