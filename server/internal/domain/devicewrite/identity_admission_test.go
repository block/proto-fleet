package devicewrite_test

import (
	"context"
	"testing"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/commandtype"
	"github.com/block/proto-fleet/server/internal/domain/devicewrite"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/block/proto-fleet/server/internal/testutil"
	"github.com/stretchr/testify/require"
)

func TestIdentityAdmissionReuseRequiresExactDispatchScope(t *testing.T) {
	cfg, err := testutil.GetTestConfig()
	require.NoError(t, err)
	database := testutil.NewDatabaseService(t, cfg)
	user := database.CreateSuperAdminUser()
	device := database.CreateDevice(user.OrganizationID, "proto")
	other := database.CreateDevice(user.OrganizationID, "proto")
	service := devicewrite.NewService(database.DB)
	attempt, err := service.BeginHost(t.Context(), user.OrganizationID, device.DatabaseID, 0)
	require.NoError(t, err)
	defer func() { require.NoError(t, attempt.Finish(t.Context(), nil)) }()
	ctx := attempt.Context(t.Context())
	reused, err := devicewrite.RecheckIdentityAdmission(ctx, sqlc.New(database.DB), user.OrganizationID, []int64{device.DatabaseID})
	require.NoError(t, err)
	require.False(t, reused, "a committed attempt alone cannot bypass review locking")
	require.NoError(t, service.WithBindingTransaction(ctx, func(ctx context.Context, q sqlc.Querier) error {
		for _, scenario := range []struct {
			name    string
			q       sqlc.Querier
			org     int64
			devices []int64
			reuse   bool
		}{
			{"matching dispatch", q, user.OrganizationID, []int64{device.DatabaseID}, true},
			{"different query connection", sqlc.New(database.DB), user.OrganizationID, []int64{device.DatabaseID}, false},
			{"different organization", q, user.OrganizationID + 1, []int64{device.DatabaseID}, false},
			{"different device", q, user.OrganizationID, []int64{other.DatabaseID}, false},
			{"batch", q, user.OrganizationID, []int64{device.DatabaseID, other.DatabaseID}, false},
			{"empty", q, user.OrganizationID, nil, false},
		} {
			t.Run(scenario.name, func(t *testing.T) {
				reused, err := devicewrite.RecheckIdentityAdmission(ctx, scenario.q, scenario.org, scenario.devices)
				require.NoError(t, err)
				require.Equal(t, scenario.reuse, reused)
			})
		}
		return nil
	}))
	// Neither a binding transaction without admission nor an admitted read
	// confers permission to omit a review's identity locking.
	checkNotReusable := func(ctx context.Context) error {
		return service.WithBindingTransaction(ctx, func(ctx context.Context, q sqlc.Querier) error {
			reused, err := devicewrite.RecheckIdentityAdmission(ctx, q, user.OrganizationID, []int64{device.DatabaseID})
			require.NoError(t, err)
			require.False(t, reused)
			return nil
		})
	}
	require.NoError(t, checkNotReusable(t.Context()))
	require.NoError(t, service.WithHostObservation(t.Context(), user.OrganizationID, device.DatabaseID, 0, checkNotReusable))

	otherDatabase := testutil.NewDatabaseService(t, cfg)
	require.NoError(t, devicewrite.NewService(otherDatabase.DB).WithBindingTransaction(ctx, func(ctx context.Context, q sqlc.Querier) error {
		reused, err := devicewrite.RecheckIdentityAdmission(ctx, q, user.OrganizationID, []int64{device.DatabaseID})
		require.NoError(t, err)
		require.False(t, reused, "an attempt from another database must not authorize dispatch")
		return nil
	}))
}

func TestIdentityAdmissionReuseExcludesSpecialMutationPermits(t *testing.T) {
	for _, scenario := range []string{"curtailment", "deletion"} {
		t.Run(scenario, func(t *testing.T) {
			database, org, device, processing, _ := queuedFenceFixture(t)
			service := devicewrite.NewService(database.DB)
			check := func(ctx context.Context) error {
				return service.WithBindingTransaction(ctx, func(ctx context.Context, q sqlc.Querier) error {
					reused, err := devicewrite.RecheckIdentityAdmission(ctx, q, org, []int64{device})
					require.NoError(t, err)
					require.False(t, reused, "special permits must not substitute for ordinary mutation admission")
					return nil
				})
			}
			if scenario == "curtailment" {
				processing.CommandType = commandtype.Curtail
				_, err := database.DB.ExecContext(t.Context(), `UPDATE queue_message SET command_type=$1 WHERE id=$2`, processing.CommandType.String(), processing.ID)
				require.NoError(t, err)
				attempt, err := service.BeginQueuedHost(t.Context(), processing)
				require.NoError(t, err)
				require.NoError(t, check(attempt.Context(t.Context())))
				require.NoError(t, attempt.Finish(t.Context(), nil))
			} else {
				_, err := database.DB.ExecContext(t.Context(), `INSERT INTO device_pairing(device_id,pairing_status,paired_at) VALUES($1,'PAIRED',now())`, device)
				require.NoError(t, err)
				identifier, err := sqlc.New(database.DB).GetDeviceIdentifierByID(t.Context(), device)
				require.NoError(t, err)
				deleted, err := sqlstores.NewSQLDeviceStore(database.DB).SoftDeleteDevices(t.Context(), []string{identifier}, org)
				require.NoError(t, err)
				require.EqualValues(t, 1, deleted)
				var hash string
				require.NoError(t, database.DB.QueryRowContext(t.Context(), `SELECT binding_hash FROM device_deletion_unpair_permit WHERE device_id=$1`, device).Scan(&hash))
				require.NoError(t, service.RunDeletionUnpair(t.Context(), org, device, hash, check))
			}
		})
	}
}

func TestIdentityAdmissionReuseRejectsStaleAttempts(t *testing.T) {
	cfg, err := testutil.GetTestConfig()
	require.NoError(t, err)
	database := testutil.NewDatabaseService(t, cfg)
	user := database.CreateSuperAdminUser()
	service := devicewrite.NewService(database.DB)
	for _, scenario := range []struct {
		name, mutation string
	}{
		{"completed", `UPDATE device_write_attempt SET status='complete',finished_at=now() WHERE device_id=$1`},
		{"unknown outcome", `UPDATE device_write_attempt SET status='unknown',finished_at=now() WHERE device_id=$1`},
		{"new epoch", `UPDATE device_write_fence SET epoch=epoch+1 WHERE device_id=$1`},
		{"reserved", `UPDATE device_write_fence SET state='reserved' WHERE device_id=$1`},
		{"new identity", `UPDATE device SET serial_number='replacement' WHERE id=$1`},
		{"deleted", `UPDATE device SET deleted_at=now() WHERE id=$1`},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			device := database.CreateDevice(user.OrganizationID, "proto")
			attempt, err := service.BeginHost(t.Context(), user.OrganizationID, device.DatabaseID, 0)
			require.NoError(t, err)
			require.NoError(t, service.WithBindingTransaction(attempt.Context(t.Context()), func(ctx context.Context, q sqlc.Querier) error {
				reused, err := devicewrite.RecheckIdentityAdmission(ctx, q, user.OrganizationID, []int64{device.DatabaseID})
				require.NoError(t, err)
				require.True(t, reused)
				_, err = database.DB.ExecContext(t.Context(), scenario.mutation, device.DatabaseID)
				require.NoError(t, err)
				reused, err = devicewrite.RecheckIdentityAdmission(ctx, q, user.OrganizationID, []int64{device.DatabaseID})
				require.ErrorIs(t, err, devicewrite.ErrStaleBinding)
				require.False(t, reused)
				return nil
			}))
		})
	}
}
