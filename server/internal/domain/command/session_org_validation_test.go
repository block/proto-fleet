package command

import (
	"context"
	"errors"
	"testing"

	"connectrpc.com/authn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pb "github.com/block/proto-fleet/server/generated/grpc/minercommand/v1"
	"github.com/block/proto-fleet/server/internal/domain/commandtype"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/session"
)

// sessionCtxWithOrg builds a request context carrying the minimum session.Info
// the validation path reads. Inlined to avoid testutil (same-package test can
// hit an import cycle since testutil depends on command).
func sessionCtxWithOrg(orgID int64) context.Context {
	return authn.SetInfo(context.Background(), &session.Info{
		SessionID:      "test",
		UserID:         42,
		OrganizationID: orgID,
	})
}

// Guards the invariant added in R14/M3: a session that reached the command
// service must carry a real organization_id. Without this check we'd write
// a command_batch_log row with organization_id=NULL, which is invisible to
// GetCommandBatchDeviceResults and would also block the planned NOT NULL
// migration on command_batch_log.organization_id.

func TestProcessCommand_RejectsMissingOrgID(t *testing.T) {
	// Constructing a Service struct directly (same-package test) so we never
	// reach the DB layer -- validation must short-circuit before then. The
	// executionService is pre-marked running so the IsRunning() check passes
	// and control reaches the validation we care about.
	es := &ExecutionService{queueProcessorRunning: true}
	svc := &Service{config: &Config{}, executionService: es}

	cases := []struct {
		name  string
		orgID int64
	}{
		{"zero orgID", 0},
		{"negative orgID", -1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := sessionCtxWithOrg(tc.orgID)
			_, _, err := svc.processCommand(ctx, &Command{
				commandType:    commandtype.Reboot,
				deviceSelector: &pb.DeviceSelector{},
			})
			require.Error(t, err)
			var fleetErr fleeterror.FleetError
			require.True(t, errors.As(err, &fleetErr), "expected FleetError, got %T", err)
			assert.Contains(t, err.Error(), "session missing organization_id")
		})
	}
}

func TestReapplyCurrentPoolsWithWorkerNames_RejectsMissingOrgID(t *testing.T) {
	es := &ExecutionService{queueProcessorRunning: true}
	svc := &Service{config: &Config{}, executionService: es}

	ctx := sessionCtxWithOrg(0)
	_, err := svc.ReapplyCurrentPoolsWithWorkerNames(ctx, map[string]string{
		"device-1": "worker-1",
	})
	require.Error(t, err)
	var fleetErr fleeterror.FleetError
	require.True(t, errors.As(err, &fleetErr), "expected FleetError, got %T", err)
	assert.Contains(t, err.Error(), "session missing organization_id")
}
