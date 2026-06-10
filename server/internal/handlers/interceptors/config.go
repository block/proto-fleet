package interceptors

import (
	"github.com/block/proto-fleet/server/generated/grpc/apikey/v1/apikeyv1connect"
	"github.com/block/proto-fleet/server/generated/grpc/auth/v1/authv1connect"
	"github.com/block/proto-fleet/server/generated/grpc/authz/v1/authzv1connect"
	"github.com/block/proto-fleet/server/generated/grpc/curtailment/v1/curtailmentv1connect"
	"github.com/block/proto-fleet/server/generated/grpc/fleetmanagement/v1/fleetmanagementv1connect"
	"github.com/block/proto-fleet/server/generated/grpc/fleetnodeadmin/v1/fleetnodeadminv1connect"
	"github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1/fleetnodegatewayv1connect"
	"github.com/block/proto-fleet/server/generated/grpc/foremanimport/v1/foremanimportv1connect"
	"github.com/block/proto-fleet/server/generated/grpc/minercommand/v1/minercommandv1connect"
	"github.com/block/proto-fleet/server/generated/grpc/onboarding/v1/onboardingv1connect"
	"github.com/block/proto-fleet/server/generated/grpc/serverlog/v1/serverlogv1connect"
)

// RedactedRequestProcedures lists procedures whose requests contain secrets
// (passwords, pool credentials, provider attestations) and must not be
// logged at debug level.
var RedactedRequestProcedures = []string{
	authv1connect.AuthServiceAuthenticateProcedure,
	authv1connect.AuthServiceUpdatePasswordProcedure,
	authv1connect.AuthServiceVerifyCredentialsProcedure,
	curtailmentv1connect.CurtailmentServiceCreateMqttCurtailmentSourceProcedure,
	curtailmentv1connect.CurtailmentServiceUpdateMqttCurtailmentSourceProcedure,
	curtailmentv1connect.CurtailmentServiceTestMqttCurtailmentSourceConnectionProcedure,
	curtailmentv1connect.CurtailmentServiceIngestCurtailmentSignalProcedure,
	fleetmanagementv1connect.FleetManagementServiceUpdateWorkerNamesProcedure,
	onboardingv1connect.OnboardingServiceCreateAdminLoginProcedure,
	minercommandv1connect.MinerCommandServiceUpdateMiningPoolsProcedure,
	minercommandv1connect.MinerCommandServiceUpdateMinerPasswordProcedure,
	fleetnodegatewayv1connect.FleetNodeGatewayServiceRegisterProcedure,
	fleetnodegatewayv1connect.FleetNodeGatewayServiceBeginAuthHandshakeProcedure,
	fleetnodegatewayv1connect.FleetNodeGatewayServiceCompleteAuthHandshakeProcedure,
	fleetnodegatewayv1connect.FleetNodeGatewayServiceUploadHeartbeatProcedure,
	// PairDiscoveredDevicesOnFleetNode carries miner credentials (username/password)
	// in the request body.
	fleetnodeadminv1connect.FleetNodeAdminServicePairDiscoveredDevicesOnFleetNodeProcedure,
}

// RedactedResponseProcedures lists procedures whose responses contain secrets
// (API keys, temporary passwords) and must not be logged at debug level.
var RedactedResponseProcedures = []string{
	apikeyv1connect.ApiKeyServiceCreateApiKeyProcedure,
	authv1connect.AuthServiceCreateUserProcedure,
	authv1connect.AuthServiceResetUserPasswordProcedure,
	fleetnodegatewayv1connect.FleetNodeGatewayServiceBeginAuthHandshakeProcedure,
	fleetnodegatewayv1connect.FleetNodeGatewayServiceCompleteAuthHandshakeProcedure,
	fleetnodeadminv1connect.FleetNodeAdminServiceCreateEnrollmentCodeProcedure,
	fleetnodeadminv1connect.FleetNodeAdminServiceConfirmFleetNodeProcedure,
	serverlogv1connect.ServerLogServiceListServerLogsProcedure,
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
	authv1connect.AuthServiceUpdateUserRoleProcedure,
	authv1connect.AuthServiceVerifyCredentialsProcedure,
	// AuthzService role management mutates RBAC state — granting, editing,
	// or removing permission bundles assigned to users. A leaked API key
	// with role:manage would otherwise be able to persistently widen its
	// own (or others') effective permissions. Listing surfaces are
	// session-only too: the catalog + role list are only useful inside
	// the browser role editor, and keeping the surface uniform stops the
	// next reviewer from having to think about why mutations are
	// session-only but reads aren't.
	authzv1connect.AuthzServiceListPermissionsProcedure,
	authzv1connect.AuthzServiceListRolesProcedure,
	authzv1connect.AuthzServiceCreateCustomRoleProcedure,
	authzv1connect.AuthzServiceUpdateCustomRoleProcedure,
	authzv1connect.AuthzServiceDeleteCustomRoleProcedure,
	// FleetNodeAdminService mints credentials (enrollment codes, fleet node
	// api_keys) and exposes operator-only fleet metadata. Restrict to
	// interactive browser sessions so a leaked user api_key cannot bootstrap
	// rogue fleet nodes.
	fleetnodeadminv1connect.FleetNodeAdminServiceCreateEnrollmentCodeProcedure,
	fleetnodeadminv1connect.FleetNodeAdminServiceListFleetNodesProcedure,
	fleetnodeadminv1connect.FleetNodeAdminServiceConfirmFleetNodeProcedure,
	fleetnodeadminv1connect.FleetNodeAdminServiceRevokeFleetNodeProcedure,
	fleetnodeadminv1connect.FleetNodeAdminServicePairDeviceToFleetNodeProcedure,
	fleetnodeadminv1connect.FleetNodeAdminServiceUnpairDeviceProcedure,
	fleetnodeadminv1connect.FleetNodeAdminServiceListFleetNodeDevicesProcedure,
	fleetnodeadminv1connect.FleetNodeAdminServiceDiscoverOnFleetNodeProcedure,
	fleetnodeadminv1connect.FleetNodeAdminServiceListFleetNodeDiscoveredDevicesProcedure,
	fleetnodeadminv1connect.FleetNodeAdminServicePairDiscoveredDevicesOnFleetNodeProcedure,
	// AdminTerminateEvent forces a non-terminal event to a terminal state and
	// is session-only. Paired with handler-side requireAdminFromContext in
	// handlers/curtailment/handler.go; neither check alone is sufficient.
	curtailmentv1connect.CurtailmentServiceAdminTerminateEventProcedure,
	// MQTT source credential RPCs carry or exercise broker passwords and should
	// only be reachable from interactive admin sessions, not API keys.
	curtailmentv1connect.CurtailmentServiceCreateMqttCurtailmentSourceProcedure,
	curtailmentv1connect.CurtailmentServiceUpdateMqttCurtailmentSourceProcedure,
	curtailmentv1connect.CurtailmentServiceTestMqttCurtailmentSourceConnectionProcedure,
	serverlogv1connect.ServerLogServiceListServerLogsProcedure,
}

var UnauthenticatedProcedures = []string{
	"/health",
	"/grpc.reflection.v1alpha.ServerReflection/ServerReflectionInfo",
	authv1connect.AuthServiceAuthenticateProcedure,
	onboardingv1connect.OnboardingServiceCreateAdminLoginProcedure,
	onboardingv1connect.OnboardingServiceGetFleetInitStatusProcedure,
	// Bootstrap RPCs: the fleet node has no session_token yet. Register
	// validates an enrollment_token in the body; the handshake validates an
	// api_key.
	fleetnodegatewayv1connect.FleetNodeGatewayServiceRegisterProcedure,
	fleetnodegatewayv1connect.FleetNodeGatewayServiceBeginAuthHandshakeProcedure,
	fleetnodegatewayv1connect.FleetNodeGatewayServiceCompleteAuthHandshakeProcedure,
}

// FleetNodeAuthenticatedProcedures lists procedures gated by FleetNodeAuthInterceptor
// (Authorization: Bearer <session_token>). The user-session AuthInterceptor
// short-circuits these so the two interceptors don't fight over the same
// procedure.
var FleetNodeAuthenticatedProcedures = []string{
	fleetnodegatewayv1connect.FleetNodeGatewayServiceUploadTelemetryProcedure,
	fleetnodegatewayv1connect.FleetNodeGatewayServiceUploadEventsProcedure,
	fleetnodegatewayv1connect.FleetNodeGatewayServiceUploadHeartbeatProcedure,
	fleetnodegatewayv1connect.FleetNodeGatewayServiceControlStreamProcedure,
	fleetnodegatewayv1connect.FleetNodeGatewayServiceReportDiscoveredDevicesProcedure,
	fleetnodegatewayv1connect.FleetNodeGatewayServiceReportPairedDevicesProcedure,
}

// SensitiveBodyProcedures lists RPCs whose request/response bodies must not be
// logged, even at debug level, because they contain secrets (e.g., API keys).
// For streaming RPCs, this also suppresses individual message bodies in
// loggingStreamingHandlerConn.
var SensitiveBodyProcedures = map[string]bool{
	foremanimportv1connect.ForemanImportServiceImportFromForemanProcedure:             true,
	foremanimportv1connect.ForemanImportServiceCompleteImportProcedure:                true,
	authv1connect.AuthServiceAuthenticateProcedure:                                    true,
	authv1connect.AuthServiceVerifyCredentialsProcedure:                               true,
	fleetmanagementv1connect.FleetManagementServiceUpdateWorkerNamesProcedure:         true,
	fleetnodegatewayv1connect.FleetNodeGatewayServiceControlStreamProcedure:           true,
	fleetnodegatewayv1connect.FleetNodeGatewayServiceUploadTelemetryProcedure:         true,
	fleetnodegatewayv1connect.FleetNodeGatewayServiceUploadEventsProcedure:            true,
	fleetnodegatewayv1connect.FleetNodeGatewayServiceReportDiscoveredDevicesProcedure: true,
	// ReportPairedDevices carries pair result error strings that can echo credential
	// fragments from plugin/miner error responses.
	fleetnodegatewayv1connect.FleetNodeGatewayServiceReportPairedDevicesProcedure: true,
	fleetnodeadminv1connect.FleetNodeAdminServiceDiscoverOnFleetNodeProcedure:     true,
	// PairDiscoveredDevicesOnFleetNode carries credentials in the request; response
	// error strings from plugins/nodes can also echo secrets.
	fleetnodeadminv1connect.FleetNodeAdminServicePairDiscoveredDevicesOnFleetNodeProcedure: true,
}
