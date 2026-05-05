package interceptors

import (
	"context"
	"net/http"
	"testing"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/generated/grpc/curtailment/v1/curtailmentv1connect"
	"github.com/block/proto-fleet/server/generated/grpc/fleetmanagement/v1/fleetmanagementv1connect"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
)

func TestUpdateWorkerNamesProcedureIsRedacted(t *testing.T) {
	procedure := fleetmanagementv1connect.FleetManagementServiceUpdateWorkerNamesProcedure

	assert.Contains(t, RedactedRequestProcedures, procedure)
	assert.True(t, SensitiveBodyProcedures[procedure])
}

// Curtailment write and admin RPCs must reject API-key auth. A leaked API
// key would otherwise be able to mass-stop a fleet, abort a live event, or
// force-recover a non-terminal event via AdminTransitionEvent.
func TestCurtailmentWriteProceduresAreSessionOnly(t *testing.T) {
	t.Parallel()

	sessionOnly := []string{
		curtailmentv1connect.CurtailmentServiceStartCurtailmentProcedure,
		curtailmentv1connect.CurtailmentServiceStopCurtailmentProcedure,
		curtailmentv1connect.CurtailmentServiceUpdateCurtailmentEventProcedure,
		curtailmentv1connect.CurtailmentServiceAdminTransitionEventProcedure,
	}

	for _, procedure := range sessionOnly {
		assert.Contains(t, SessionOnlyProcedures, procedure,
			"%s must be in SessionOnlyProcedures so a leaked API key cannot invoke it",
			procedure)
	}
}

// Curtailment read RPCs must remain reachable via API-key auth so monitoring
// dashboards and fleet-health probes can call them without an interactive
// session.
func TestCurtailmentReadProceduresStayApiKeyAccessible(t *testing.T) {
	t.Parallel()

	apiKeyAccessible := []string{
		curtailmentv1connect.CurtailmentServicePreviewCurtailmentPlanProcedure,
		curtailmentv1connect.CurtailmentServiceGetActiveCurtailmentProcedure,
		curtailmentv1connect.CurtailmentServiceListCurtailmentEventsProcedure,
	}

	for _, procedure := range apiKeyAccessible {
		assert.NotContains(t, SessionOnlyProcedures, procedure,
			"%s is a read RPC and must remain API-key-accessible",
			procedure)
	}
}

// API-key auth on SessionOnly curtailment procedures returns PermissionDenied.
// nil service deps are fine — the SessionOnly branch returns before any
// service is touched.
func TestAuthInterceptor_SessionOnlyRejectsApiKeyAuth(t *testing.T) {
	t.Parallel()

	interceptor := NewAuthInterceptor(nil, nil, nil, nil, nil, SessionOnlyProcedures)

	cases := []struct {
		name      string
		procedure string
	}{
		{"StartCurtailment", curtailmentv1connect.CurtailmentServiceStartCurtailmentProcedure},
		{"StopCurtailment", curtailmentv1connect.CurtailmentServiceStopCurtailmentProcedure},
		{"UpdateCurtailmentEvent", curtailmentv1connect.CurtailmentServiceUpdateCurtailmentEventProcedure},
		{"AdminTransitionEvent", curtailmentv1connect.CurtailmentServiceAdminTransitionEventProcedure},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			header := http.Header{}
			header.Set("Authorization", "Bearer fleet_test_some_key")

			_, err := interceptor.authenticate(context.Background(), tc.procedure, header)

			require.Error(t, err)
			var fleetErr fleeterror.FleetError
			require.ErrorAs(t, err, &fleetErr)
			assert.Equal(t, connect.CodePermissionDenied, fleetErr.GRPCCode)
		})
	}
}
