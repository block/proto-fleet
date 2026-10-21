package interceptors

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/generated/grpc/alerts/v1/alertsv1connect"
	"github.com/block/proto-fleet/server/generated/grpc/auth/v1/authv1connect"
	"github.com/block/proto-fleet/server/generated/grpc/curtailment/v1/curtailmentv1connect"
	"github.com/block/proto-fleet/server/generated/grpc/fleetmanagement/v1/fleetmanagementv1connect"
	fleetnodeadmin "github.com/block/proto-fleet/server/generated/grpc/fleetnodeadmin/v1"
	"github.com/block/proto-fleet/server/generated/grpc/fleetnodeadmin/v1/fleetnodeadminv1connect"
	"github.com/block/proto-fleet/server/generated/grpc/infrastructure/v1/infrastructurev1connect"
	"github.com/block/proto-fleet/server/generated/grpc/instance/v1/instancev1connect"
	"github.com/block/proto-fleet/server/generated/grpc/invitation/v1/invitationv1connect"
	"github.com/block/proto-fleet/server/generated/grpc/sitemap/v1/sitemapv1connect"
	"github.com/block/proto-fleet/server/generated/grpc/sites/v1/sitesv1connect"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
)

func TestUpdateWorkerNamesProcedureIsRedacted(t *testing.T) {
	procedure := fleetmanagementv1connect.FleetManagementServiceUpdateWorkerNamesProcedure

	assert.Contains(t, RedactedRequestProcedures, procedure)
	assert.True(t, SensitiveBodyProcedures[procedure])
}

func TestListFleetNodesResponseIsRedactedFromDebugLogs(t *testing.T) {
	// Arrange
	out := captureSlogWarn(t)
	procedure := fleetnodeadminv1connect.FleetNodeAdminServiceListFleetNodesProcedure
	node := &fleetnodeadmin.FleetNodeSummary{
		IpAddresses: []string{"192.0.2.42", "fd00::42"},
		Version:     "v0.5.1-private-test",
		EnrolledBy:  "operator@example.com",
	}
	logging := NewRequestLoggingInterceptor(slog.LevelDebug, RedactedRequestProcedures, RedactedResponseProcedures)
	handler := connect.NewUnaryHandler(procedure,
		func(context.Context, *connect.Request[fleetnodeadmin.ListFleetNodesRequest]) (*connect.Response[fleetnodeadmin.ListFleetNodesResponse], error) {
			return connect.NewResponse(&fleetnodeadmin.ListFleetNodesResponse{FleetNodes: []*fleetnodeadmin.FleetNodeSummary{node}}), nil
		}, connect.WithInterceptors(logging))
	server := httptest.NewServer(handler)
	defer server.Close()
	client := connect.NewClient[fleetnodeadmin.ListFleetNodesRequest, fleetnodeadmin.ListFleetNodesResponse](server.Client(), server.URL+procedure)

	// Act
	resp, err := client.CallUnary(t.Context(), connect.NewRequest(&fleetnodeadmin.ListFleetNodesRequest{}))

	// Assert
	require.NoError(t, err)
	require.Len(t, resp.Msg.FleetNodes, 1)
	require.Equal(t, node.IpAddresses, resp.Msg.FleetNodes[0].IpAddresses)
	require.Equal(t, node.Version, resp.Msg.FleetNodes[0].Version)
	require.Equal(t, node.EnrolledBy, resp.Msg.FleetNodes[0].EnrolledBy)
	logged := out.String()
	require.Contains(t, logged, procedure)
	require.Contains(t, logged, "result=[REDACTED]")
	for _, value := range append(node.IpAddresses, node.Version, node.EnrolledBy) {
		require.NotContains(t, logged, value)
	}
}

func TestSiteMapCsvProceduresAreSensitiveBody(t *testing.T) {
	t.Parallel()

	assert.Contains(t, RedactedRequestProcedures, sitemapv1connect.SiteMapServiceImportSiteMapCsvProcedure)
	assert.True(t, SensitiveBodyProcedures[sitemapv1connect.SiteMapServiceImportSiteMapCsvProcedure],
		"site-map imports carry CSV topology and miner identity data")
	assert.True(t, SensitiveBodyProcedures[sitemapv1connect.SiteMapServiceExportSiteMapCsvProcedure],
		"site-map export streams carry CSV topology and miner identity data")
}

func TestMqttSettingsPasswordProceduresAreRedacted(t *testing.T) {
	t.Parallel()

	procedures := []string{
		curtailmentv1connect.CurtailmentServiceCreateMqttCurtailmentSourceProcedure,
		curtailmentv1connect.CurtailmentServiceUpdateMqttCurtailmentSourceProcedure,
		curtailmentv1connect.CurtailmentServiceTestMqttCurtailmentSourceConnectionProcedure,
	}
	for _, procedure := range procedures {
		assert.Contains(t, RedactedRequestProcedures, procedure)
	}
}

// Infrastructure device bodies carry driver_config — the OT control
// network map (endpoint IPs, unit IDs, register addresses) — and must
// never land in debug logs.
func TestInfrastructureProceduresAreSensitiveBody(t *testing.T) {
	t.Parallel()

	procedures := []string{
		infrastructurev1connect.InfrastructureServiceListInfrastructureDevicesProcedure,
		infrastructurev1connect.InfrastructureServiceGetInfrastructureDeviceProcedure,
		infrastructurev1connect.InfrastructureServiceCreateInfrastructureDeviceProcedure,
		infrastructurev1connect.InfrastructureServiceUpdateInfrastructureDeviceProcedure,
		infrastructurev1connect.InfrastructureServiceDeleteInfrastructureDeviceProcedure,
	}
	for _, procedure := range procedures {
		assert.True(t, SensitiveBodyProcedures[procedure],
			"%s carries driver_config (OT network topology) and must suppress body logging",
			procedure)
	}
}

// Infrastructure devices are the OT control surface (writes change which
// physical fans curtailment drives; manage-level reads expose the OT network
// map), so all five procedures must reject API-key auth.
func TestInfrastructureProceduresAreSessionOnly(t *testing.T) {
	t.Parallel()

	procedures := []string{
		infrastructurev1connect.InfrastructureServiceListInfrastructureDevicesProcedure,
		infrastructurev1connect.InfrastructureServiceGetInfrastructureDeviceProcedure,
		infrastructurev1connect.InfrastructureServiceCreateInfrastructureDeviceProcedure,
		infrastructurev1connect.InfrastructureServiceUpdateInfrastructureDeviceProcedure,
		infrastructurev1connect.InfrastructureServiceDeleteInfrastructureDeviceProcedure,
	}
	for _, procedure := range procedures {
		assert.Contains(t, SessionOnlyProcedures, procedure,
			"%s must be session-only; the OT control surface should not be reachable via API key",
			procedure)
	}
}

func TestInfrastructureControlSubnetProceduresAreSensitiveAndSessionOnly(t *testing.T) {
	t.Parallel()

	getProcedure := sitesv1connect.SiteServiceGetInfrastructureControlSubnetsProcedure
	setProcedure := sitesv1connect.SiteServiceSetInfrastructureControlSubnetsProcedure

	for _, procedure := range []string{getProcedure, setProcedure} {
		assert.Contains(t, SessionOnlyProcedures, procedure,
			"%s exposes OT topology and must reject API-key auth", procedure)
		assert.True(t, SensitiveBodyProcedures[procedure],
			"%s exposes OT topology and must suppress body logging", procedure)
	}
	assert.Contains(t, RedactedRequestProcedures, setProcedure,
		"commissioning replacement carries OT topology in its request body")
}

func TestMqttSettingsPasswordProceduresAreSessionOnly(t *testing.T) {
	t.Parallel()

	procedures := []string{
		curtailmentv1connect.CurtailmentServiceCreateMqttCurtailmentSourceProcedure,
		curtailmentv1connect.CurtailmentServiceUpdateMqttCurtailmentSourceProcedure,
		curtailmentv1connect.CurtailmentServiceTestMqttCurtailmentSourceConnectionProcedure,
	}
	for _, procedure := range procedures {
		assert.Contains(t, SessionOnlyProcedures, procedure,
			"%s must reject API-key auth because it carries or exercises MQTT broker credentials",
			procedure)
	}
}

// The alert surface is uniformly session-only; rule mutations persist Grafana
// rule evaluations and must not be reachable from a leaked API key.
func TestAlertRuleMutationProceduresAreSessionOnly(t *testing.T) {
	t.Parallel()

	procedures := []string{
		alertsv1connect.RuleServiceCreateRuleProcedure,
		alertsv1connect.RuleServiceUpdateRuleProcedure,
		alertsv1connect.RuleServiceDeleteRuleProcedure,
		alertsv1connect.RuleServiceSetRuleRoutingProcedure,
	}
	for _, procedure := range procedures {
		assert.Contains(t, SessionOnlyProcedures, procedure,
			"%s must reject API-key auth like the rest of the alert-management surface", procedure)
	}
}

// AdminTerminateEvent is the operator-of-last-resort recovery RPC and must
// reject API-key auth.
func TestCurtailmentAdminProcedureIsSessionOnly(t *testing.T) {
	t.Parallel()

	assert.Contains(t, SessionOnlyProcedures,
		curtailmentv1connect.CurtailmentServiceAdminTerminateEventProcedure,
		"AdminTerminateEvent must be session-only; recovery escape hatch should not be reachable via API key")
	assert.Contains(t, SessionOnlyProcedures,
		curtailmentv1connect.CurtailmentServiceForceReleaseCurtailmentOwnershipProcedure,
		"ForceReleaseCurtailmentOwnership must be session-only; recovery escape hatch should not be reachable via API key")
}

// Public curtailment control/read RPCs must remain reachable via API-key auth
// so integrations and monitoring callers can drive the public surface.
func TestCurtailmentNonAdminProceduresStayApiKeyAccessible(t *testing.T) {
	t.Parallel()

	apiKeyAccessible := []string{
		curtailmentv1connect.CurtailmentServicePreviewCurtailmentPlanProcedure,
		curtailmentv1connect.CurtailmentServiceStartCurtailmentProcedure,
		curtailmentv1connect.CurtailmentServiceUpdateCurtailmentEventProcedure,
		curtailmentv1connect.CurtailmentServiceStopCurtailmentProcedure,
		curtailmentv1connect.CurtailmentServiceListActiveCurtailmentsProcedure,
		curtailmentv1connect.CurtailmentServiceListCurtailmentEventsProcedure,
		curtailmentv1connect.CurtailmentServiceGetCurtailmentEventProcedure,
		curtailmentv1connect.CurtailmentServiceListCurtailmentAutomationRulesProcedure,
		curtailmentv1connect.CurtailmentServiceGetCurtailmentAutomationRuleProcedure,
		curtailmentv1connect.CurtailmentServiceCreateCurtailmentAutomationRuleProcedure,
		curtailmentv1connect.CurtailmentServiceUpdateCurtailmentAutomationRuleProcedure,
		curtailmentv1connect.CurtailmentServiceSetCurtailmentAutomationRuleEnabledProcedure,
		curtailmentv1connect.CurtailmentServiceDeleteCurtailmentAutomationRuleProcedure,
	}

	for _, procedure := range apiKeyAccessible {
		assert.NotContains(t, SessionOnlyProcedures, procedure,
			"%s must remain API-key-accessible for public-API integrations",
			procedure)
	}
}

// The updates surface is uniformly session-only: an API key can neither read
// the instance's patch level nor flip the release channel. Membership is
// asserted AND exercised through the interceptor so a procedure rename cannot
// silently drop the gate (nil service deps are fine — the SessionOnly branch
// returns before any service is touched).
func TestInstanceUpdateProceduresAreSessionOnly(t *testing.T) {
	t.Parallel()

	interceptor := NewAuthInterceptor(nil, nil, nil, nil, nil, nil, nil, SessionOnlyProcedures, FleetNodeAuthenticatedProcedures)

	procedures := []string{
		instancev1connect.InstanceUpdateServiceGetUpdateStatusProcedure,
		instancev1connect.InstanceUpdateServiceSetReleaseChannelProcedure,
		instancev1connect.InstanceUpdateServiceTriggerUpgradeProcedure,
		instancev1connect.InstanceUpdateServiceGetUpgradeStatusProcedure,
		instancev1connect.InstanceUpdateServiceAcknowledgeUpgradeProcedure,
	}
	for _, procedure := range procedures {
		assert.Contains(t, SessionOnlyProcedures, procedure,
			"%s must reject API-key auth like the rest of the instance-administration surface", procedure)

		header := http.Header{}
		header.Set("Authorization", "Bearer fleet_test_some_key")
		_, err := interceptor.authenticate(context.Background(), procedure, header)
		require.Error(t, err)
		var fleetErr fleeterror.FleetError
		require.ErrorAs(t, err, &fleetErr)
		assert.Equal(t, connect.CodePermissionDenied, fleetErr.GRPCCode)
	}
}

func TestInstanceUpdateResponsesAreRedacted(t *testing.T) {
	t.Parallel()

	procedures := []string{
		instancev1connect.InstanceUpdateServiceGetUpdateStatusProcedure,
		instancev1connect.InstanceUpdateServiceTriggerUpgradeProcedure,
		instancev1connect.InstanceUpdateServiceGetUpgradeStatusProcedure,
		instancev1connect.InstanceUpdateServiceAcknowledgeUpgradeProcedure,
	}
	for _, procedure := range procedures {
		assert.Contains(t, RedactedResponseProcedures, procedure,
			"update responses expose host operational metadata that should not land in debug logs")
	}
}

// API-key auth on AdminTerminateEvent returns PermissionDenied. nil service
// deps are fine — the SessionOnly branch returns before any service is touched.
func TestAuthInterceptor_AdminTerminateEventRejectsApiKeyAuth(t *testing.T) {
	t.Parallel()

	interceptor := NewAuthInterceptor(nil, nil, nil, nil, nil, nil, nil, SessionOnlyProcedures, FleetNodeAuthenticatedProcedures)

	header := http.Header{}
	header.Set("Authorization", "Bearer fleet_test_some_key")

	_, err := interceptor.authenticate(
		context.Background(),
		curtailmentv1connect.CurtailmentServiceAdminTerminateEventProcedure,
		header,
	)

	require.Error(t, err)
	var fleetErr fleeterror.FleetError
	require.ErrorAs(t, err, &fleetErr)
	assert.Equal(t, connect.CodePermissionDenied, fleetErr.GRPCCode)
}

// Team invitations mint login access: management and signed-in acceptance
// reject API keys, and only the bearer-code redemption RPCs are public.
func TestInvitationProceduresAreSessionOnlyAndRedacted(t *testing.T) {
	t.Parallel()

	interceptor := NewAuthInterceptor(nil, nil, nil, nil, nil, nil, nil, SessionOnlyProcedures, FleetNodeAuthenticatedProcedures)
	for _, procedure := range []string{
		invitationv1connect.InvitationServiceCreateTeamInvitationProcedure,
		invitationv1connect.InvitationServiceListTeamInvitationsProcedure,
		invitationv1connect.InvitationServiceRevokeTeamInvitationProcedure,
		invitationv1connect.InvitationServiceDeleteTeamInvitationProcedure,
		invitationv1connect.InvitationServiceAcceptInvitationProcedure,
	} {
		assert.Contains(t, SessionOnlyProcedures, procedure)
		assert.NotContains(t, UnauthenticatedProcedures, procedure)
		header := http.Header{}
		header.Set("Authorization", "Bearer fleet_test_some_key")
		_, err := interceptor.authenticate(context.Background(), procedure, header)
		require.Error(t, err)
		var fleetErr fleeterror.FleetError
		require.ErrorAs(t, err, &fleetErr)
		assert.Equal(t, connect.CodePermissionDenied, fleetErr.GRPCCode, procedure)
	}
	for _, procedure := range []string{
		invitationv1connect.InvitationServicePreviewInvitationProcedure,
		invitationv1connect.InvitationServiceRegisterInvitedUserProcedure,
		invitationv1connect.InvitationServiceAcceptInvitationProcedure,
	} {
		assert.Contains(t, RedactedRequestProcedures, procedure, "requests carry the bearer code")
		assert.True(t, SensitiveBodyProcedures[procedure], procedure)
	}
	assert.Contains(t, UnauthenticatedProcedures, invitationv1connect.InvitationServicePreviewInvitationProcedure)
	assert.Contains(t, UnauthenticatedProcedures, invitationv1connect.InvitationServiceRegisterInvitedUserProcedure)
	assert.Contains(t, RedactedResponseProcedures, invitationv1connect.InvitationServiceCreateTeamInvitationProcedure, "responses carry the bearer code")
	assert.True(t, SensitiveBodyProcedures[invitationv1connect.InvitationServiceCreateTeamInvitationProcedure])
}

// UpdateUserRole mutates RBAC state and must reject API-key auth — a leaked
// user API key with user:manage would otherwise be able to escalate or
// reassign roles outside an interactive session.
func TestAuthInterceptor_UpdateUserRoleRejectsApiKeyAuth(t *testing.T) {
	t.Parallel()

	assert.Contains(t, SessionOnlyProcedures, authv1connect.AuthServiceUpdateUserRoleProcedure,
		"UpdateUserRole must be session-only; role mutations should not be reachable via API key")

	interceptor := NewAuthInterceptor(nil, nil, nil, nil, nil, nil, nil, SessionOnlyProcedures, FleetNodeAuthenticatedProcedures)

	header := http.Header{}
	header.Set("Authorization", "Bearer fleet_test_some_key")

	_, err := interceptor.authenticate(
		context.Background(),
		authv1connect.AuthServiceUpdateUserRoleProcedure,
		header,
	)

	require.Error(t, err)
	var fleetErr fleeterror.FleetError
	require.ErrorAs(t, err, &fleetErr)
	assert.Equal(t, connect.CodePermissionDenied, fleetErr.GRPCCode)
}
