package devicewrite_test

import (
	"context"
	"errors"
	"testing"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/commandtype"
	"github.com/block/proto-fleet/server/internal/domain/devicewrite"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
	"github.com/block/proto-fleet/server/internal/testutil"
	"github.com/stretchr/testify/require"
)

func TestPoolReadinessIgnoresOnlyUnrelatedWrites(t *testing.T) {
	cfg, err := testutil.GetTestConfig()
	require.NoError(t, err)
	database := testutil.NewDatabaseService(t, cfg)
	user := database.CreateSuperAdminUser()
	for _, tc := range []struct {
		kind   commandtype.Type
		blocks bool
	}{
		{commandtype.BlinkLED, false}, {commandtype.DownloadLogs, false},
		{commandtype.SetCoolingMode, false}, {commandtype.SetPowerTarget, false},
		{commandtype.UpdateMiningPools, true}, {commandtype.Reboot, true},
		{commandtype.FirmwareUpdate, true}, {commandtype.Unpair, true},
		{commandtype.ApplyCurtailmentConfig, true}, {commandtype.Curtail, true},
		{commandtype.ContainerMining, true},
		{commandtype.UpdateMinerPassword, true}, {commandtype.Type(-1), true},
	} {
		for _, crashed := range []bool{false, true} {
			name := tc.kind.String() + "/unknown"
			if crashed {
				name = tc.kind.String() + "/running"
			}
			t.Run(name, func(t *testing.T) {
				device := database.CreateDevice(user.OrganizationID, "proto")
				service := devicewrite.NewService(database.DB)
				call := func() error {
					return service.WithHostAttempt(t.Context(), user.OrganizationID, device.DatabaseID, 0, tc.kind, func(ctx context.Context) error {
						return devicewrite.Dispatch(ctx, func(context.Context) error {
							if crashed {
								panic("interrupted writer")
							}
							return devicewrite.UncertainDelivery(errors.New("lost response"))
						})
					})
				}
				if crashed {
					require.Panics(t, func() { _ = call() })
				} else {
					require.Error(t, call())
				}
				err := db.WithTransactionNoResult(t.Context(), database.DB, func(q sqlc.Querier) error {
					_, err := q.LockDeviceWriteFences(t.Context(), []int64{device.DatabaseID})
					if err != nil {
						return err
					}
					return devicewrite.RequireNoConflictingPoolWrites(t.Context(), q, user.OrganizationID, device.DatabaseID)
				})
				if tc.blocks {
					require.Error(t, err)
				} else {
					require.NoError(t, err)
				}
				var retained int
				require.NoError(t, database.DB.QueryRowContext(t.Context(), `SELECT count(*) FROM device_write_attempt WHERE device_id=$1 AND status IN ('running','unknown')`, device.DatabaseID).Scan(&retained))
				require.Equal(t, 1, retained, "pool readiness must not erase ownership recovery evidence")
			})
		}
	}
}

func TestMayAffectPoolsPreservesObservationAndUnknownCommandPolicy(t *testing.T) {
	require.False(t, devicewrite.MayAffectPools("observation", "UnknownCommand"))
	require.True(t, devicewrite.MayAffectPools("mutation", "UnknownCommand"))
	require.False(t, devicewrite.MayAffectPools("mutation", "BlinkLED"))
	require.True(t, devicewrite.MayAffectPools("mutation", "UpdateMiningPools"))
}
