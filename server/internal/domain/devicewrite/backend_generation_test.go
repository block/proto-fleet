package devicewrite_test

import (
	"context"
	"testing"

	"github.com/block/proto-fleet/server/internal/domain/devicewrite"
	"github.com/block/proto-fleet/server/internal/testutil"
	"github.com/stretchr/testify/require"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestSDKGenerationRejectionDoesNotStrandPhysicalEvidence(t *testing.T) {
	for _, classified := range []bool{true, false} {
		t.Run(map[bool]string{true: "SDK registration rejected", false: "arbitrary adapter failure"}[classified], func(t *testing.T) {
			cfg, err := testutil.GetTestConfig()
			require.NoError(t, err)
			database := testutil.NewDatabaseService(t, cfg)
			user := database.CreateSuperAdminUser()
			dev := database.CreateDevice(user.OrganizationID, "proto")
			service := devicewrite.NewService(database.DB)
			outcome := status.New(codes.FailedPrecondition, "backend failure")
			if classified {
				outcome, err = outcome.WithDetails(&errdetails.ErrorInfo{Reason: "SDK_BACKEND_GENERATION_MISMATCH", Domain: "protofleet.sdk"})
				require.NoError(t, err)
			}
			err = service.RunHost(t.Context(), user.OrganizationID, dev.DatabaseID, 0, func(context.Context) error { return outcome.Err() })
			require.Error(t, err)
			var unresolved int
			require.NoError(t, database.DB.QueryRowContext(t.Context(), "SELECT count(*) FROM device_write_attempt WHERE device_id=$1 AND status NOT IN ('complete', 'resolved')", dev.DatabaseID).Scan(&unresolved))
			if classified {
				require.True(t, devicewrite.IsNotDispatched(err))
				require.Zero(t, unresolved)
				_, err = service.Reserve(t.Context(), user.OrganizationID, dev.DatabaseID)
				require.NoError(t, err)
			} else {
				require.False(t, devicewrite.IsNotDispatched(err))
				require.Equal(t, 1, unresolved)
				_, err = service.Reserve(t.Context(), user.OrganizationID, dev.DatabaseID)
				require.Error(t, err)
			}
		})
	}
}
