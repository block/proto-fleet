package interceptors

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/block/proto-fleet/server/generated/grpc/fleetmanagement/v1/fleetmanagementv1connect"
)

func TestUpdateWorkerNamesProcedureIsRedacted(t *testing.T) {
	procedure := fleetmanagementv1connect.FleetManagementServiceUpdateWorkerNamesProcedure

	assert.Contains(t, RedactedRequestProcedures, procedure)
	assert.True(t, SensitiveBodyProcedures[procedure])
}
