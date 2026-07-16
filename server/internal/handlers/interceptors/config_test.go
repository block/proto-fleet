package interceptors

import (
	"context"
	"net/http"
	"testing"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/generated/grpc/alerts/v1/alertsv1connect"
	"github.com/block/proto-fleet/server/generated/grpc/auth/v1/authv1connect"
	"github.com/block/proto-fleet/server/generated/grpc/curtailment/v1/curtailmentv1connect"
	"github.com/block/proto-fleet/server/generated/grpc/fleetmanagement/v1/fleetmanagementv1connect"
	"github.com/block/proto-fleet/server/generated/grpc/infrastructure/v1/infrastructurev1connect"
	"github.com/block/proto-fleet/server/generated/grpc/sites/v1/sitesv1connect"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
)

func TestUpdateWorkerNamesProcedureIsRedacted(t *testing.T) {
	procedure := fleetmanagementv1connect.FleetManagementServiceUpdateWorkerNamesProcedure

	assert.Contains(t, RedactedRequestProcedures, procedure)
	assert.True(t, SensitiveBodyProcedures[procedure])
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

// API-key auth on AdminTerminateEvent returns PermissionDenied. nil service
// deps are fine — the SessionOnly branch returns before any service is touched.
func TestAuthInterceptor_AdminTerminateEventRejectsApiKeyAuth(t *testing.T) {
	t.Parallel()

	interceptor := NewAuthInterceptor(nil, nil, nil, nil, nil, nil, SessionOnlyProcedures, FleetNodeAuthenticatedProcedures)

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

// UpdateUserRole mutates RBAC state and must reject API-key auth — a leaked
// user API key with user:manage would otherwise be able to escalate or
// reassign roles outside an interactive session.
func TestAuthInterceptor_UpdateUserRoleRejectsApiKeyAuth(t *testing.T) {
	t.Parallel()

	assert.Contains(t, SessionOnlyProcedures, authv1connect.AuthServiceUpdateUserRoleProcedure,
		"UpdateUserRole must be session-only; role mutations should not be reachable via API key")

	interceptor := NewAuthInterceptor(nil, nil, nil, nil, nil, nil, SessionOnlyProcedures, FleetNodeAuthenticatedProcedures)

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
