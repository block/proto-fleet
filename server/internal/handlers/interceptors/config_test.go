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

// AdminTerminateEvent is the operator-of-last-resort recovery RPC and must
// reject API-key auth. The other curtailment write RPCs remain API-key-
// accessible so external integrations can drive curtailment via the public API.
func TestCurtailmentAdminProcedureIsSessionOnly(t *testing.T) {
	t.Parallel()

	assert.Contains(t, SessionOnlyProcedures,
		curtailmentv1connect.CurtailmentServiceAdminTerminateEventProcedure,
		"AdminTerminateEvent must be session-only; recovery escape hatch should not be reachable via API key")
}

// Every other curtailment RPC must remain reachable via API-key auth so
// integrations and monitoring callers can drive the public surface.
func TestCurtailmentNonAdminProceduresStayApiKeyAccessible(t *testing.T) {
	t.Parallel()

	apiKeyAccessible := []string{
		curtailmentv1connect.CurtailmentServicePreviewCurtailmentPlanProcedure,
		curtailmentv1connect.CurtailmentServiceStartCurtailmentProcedure,
		curtailmentv1connect.CurtailmentServiceUpdateCurtailmentEventProcedure,
		curtailmentv1connect.CurtailmentServiceStopCurtailmentProcedure,
		curtailmentv1connect.CurtailmentServiceGetActiveCurtailmentProcedure,
		curtailmentv1connect.CurtailmentServiceListCurtailmentEventsProcedure,
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

	interceptor := NewAuthInterceptor(nil, nil, nil, nil, nil, SessionOnlyProcedures)

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
