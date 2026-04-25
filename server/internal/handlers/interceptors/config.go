package interceptors

import (
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
}

// RedactedResponseProcedures lists procedures whose responses contain secrets
// (API keys, temporary passwords) and must not be logged at debug level.
var RedactedResponseProcedures = []string{
	apikeyv1connect.ApiKeyServiceCreateApiKeyProcedure,
	authv1connect.AuthServiceCreateUserProcedure,
	authv1connect.AuthServiceResetUserPasswordProcedure,
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
}

var UnauthenticatedProcedures = []string{
	"/health",
	"/grpc.reflection.v1alpha.ServerReflection/ServerReflectionInfo",
	authv1connect.AuthServiceAuthenticateProcedure,
	onboardingv1connect.OnboardingServiceCreateAdminLoginProcedure,
	onboardingv1connect.OnboardingServiceGetFleetInitStatusProcedure,
}

// SensitiveBodyProcedures lists RPCs whose request/response bodies must not be
// logged, even at debug level, because they contain secrets (e.g., API keys).
var SensitiveBodyProcedures = map[string]bool{
	foremanimportv1connect.ForemanImportServiceImportFromForemanProcedure:     true,
	foremanimportv1connect.ForemanImportServiceCompleteImportProcedure:        true,
	authv1connect.AuthServiceAuthenticateProcedure:                            true,
	authv1connect.AuthServiceVerifyCredentialsProcedure:                       true,
	fleetmanagementv1connect.FleetManagementServiceUpdateWorkerNamesProcedure: true,
}
