package devicewrite_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/commandtype"
	"github.com/block/proto-fleet/server/internal/domain/devicewrite"
	"github.com/block/proto-fleet/server/internal/testutil"
	"github.com/stretchr/testify/require"
)

func holdBindingObservation(t *testing.T, database *testutil.DatabaseService, organizationID, deviceID int64) func() {
	t.Helper()
	service := devicewrite.NewService(database.DB)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	t.Cleanup(cancel)
	entered, release := make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- service.WithHostObservation(ctx, organizationID, deviceID, 0, func(ctx context.Context) error {
			return service.WithBindingObservation(ctx, deviceID, func(sqlc.Querier) error { return nil },
				func(context.Context) error { close(entered); <-release; return nil })
		})
	}()
	finish := sync.OnceFunc(func() {
		close(release)
		require.NoError(t, <-done)
	})
	t.Cleanup(finish)
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("durably admitted observation did not start")
	}
	return finish
}

func TestBindingObservationsOverlapOnSameMiner(t *testing.T) {
	cfg, err := testutil.GetTestConfig()
	require.NoError(t, err)
	database := testutil.NewDatabaseService(t, cfg)
	user := database.CreateSuperAdminUser()
	device := createDeletionBindingFixture(t, database, user.OrganizationID)
	first := holdBindingObservation(t, database, user.OrganizationID, device.DatabaseID)
	second := holdBindingObservation(t, database, user.OrganizationID, device.DatabaseID)
	// Both physical reads are still active when the second binding gate enters.
	second()
	first()
}

func TestBindingObservationProtectsMissingChildrenAndEndpoint(t *testing.T) {
	for _, change := range []string{"credentials", "pairing", "route", "endpoint", "existing credentials", "existing pairing", "existing route"} {
		t.Run(change, func(t *testing.T) {
			cfg, err := testutil.GetTestConfig()
			require.NoError(t, err)
			database := testutil.NewDatabaseService(t, cfg)
			user := database.CreateSuperAdminUser()
			device := database.CreateDevice(user.OrganizationID, "proto")
			var nodeID int64
			require.NoError(t, database.DB.QueryRowContext(t.Context(), `INSERT INTO fleet_node(org_id,name,identity_pubkey,encryption_pubkey,enrollment_status)
 VALUES($1,'observation-node',$2,$3,'CONFIRMED') RETURNING id`, user.OrganizationID, []byte("fixture-identity"), []byte("01234567890123456789012345678901")).Scan(&nodeID))
			q := sqlc.New(database.DB)
			switch change {
			case "existing credentials":
				require.NoError(t, q.UpsertMinerCredentials(t.Context(), sqlc.UpsertMinerCredentialsParams{DeviceID: device.DatabaseID, UsernameEnc: "before", PasswordEnc: "before"}))
			case "existing pairing":
				_, err = q.UpsertDevicePairing(t.Context(), sqlc.UpsertDevicePairingParams{DeviceID: device.DatabaseID, PairingStatus: sqlc.PairingStatusEnumPAIRED})
				require.NoError(t, err)
			case "existing route":
				_, err = q.PairDeviceToFleetNode(t.Context(), sqlc.PairDeviceToFleetNodeParams{DeviceID: device.DatabaseID, OrgID: user.OrganizationID, FleetNodeID: nodeID})
				require.NoError(t, err)
			}
			finish := holdBindingObservation(t, database, user.OrganizationID, device.DatabaseID)
			writer, err := database.DB.Conn(t.Context())
			require.NoError(t, err)
			defer func() { finish(); _ = writer.Close() }()
			var pid int
			require.NoError(t, writer.QueryRowContext(t.Context(), "SELECT pg_backend_pid()").Scan(&pid))
			done := make(chan error, 1)
			go func() {
				q := sqlc.New(writer)
				var err error
				switch change {
				case "credentials":
					err = q.UpsertMinerCredentials(t.Context(), sqlc.UpsertMinerCredentialsParams{DeviceID: device.DatabaseID, UsernameEnc: "after", PasswordEnc: "after"})
				case "pairing":
					_, err = q.UpsertDevicePairing(t.Context(), sqlc.UpsertDevicePairingParams{DeviceID: device.DatabaseID, PairingStatus: sqlc.PairingStatusEnumPAIRED})
				case "route":
					_, err = q.PairDeviceToFleetNode(t.Context(), sqlc.PairDeviceToFleetNodeParams{DeviceID: device.DatabaseID, OrgID: user.OrganizationID, FleetNodeID: nodeID})
				case "endpoint":
					_, err = q.UpsertDiscoveredDevice(t.Context(), deletionRediscovery(user.OrganizationID, device.ID))
				case "existing credentials":
					_, err = q.UpdateMinerPassword(t.Context(), sqlc.UpdateMinerPasswordParams{PasswordEnc: "after", DeviceID: device.DatabaseID})
				case "existing pairing":
					err = q.UpdateDevicePairingStatusByIdentifier(t.Context(), sqlc.UpdateDevicePairingStatusByIdentifierParams{PairingStatus: sqlc.PairingStatusEnumUNPAIRED, DeviceIdentifier: device.ID})
				case "existing route":
					_, err = writer.ExecContext(t.Context(), "DELETE FROM fleet_node_device WHERE device_id=$1", device.DatabaseID)
				}
				done <- err
			}()
			waitForDeletionBindingLock(t, t.Context(), database.DB, pid)
			select {
			case err := <-done:
				t.Fatalf("binding changed during observation: %v", err)
			default:
			}
			finish()
			require.NoError(t, <-done)
		})
	}
}

func TestBindingMutationLoginLocksExclusivelyBeforeRead(t *testing.T) {
	cfg, err := testutil.GetTestConfig()
	require.NoError(t, err)
	database := testutil.NewDatabaseService(t, cfg)
	user := database.CreateSuperAdminUser()
	device := createDeletionBindingFixture(t, database, user.OrganizationID)
	service := devicewrite.NewService(database.DB)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	loginEntered, releaseLogin := make(chan struct{}), make(chan struct{})
	release := sync.OnceFunc(func() { close(releaseLogin) })
	t.Cleanup(release)
	first := make(chan error, 1)
	go func() {
		first <- service.WithHostAttempt(ctx, user.OrganizationID, device.DatabaseID, 0, commandtype.StartMining, func(ctx context.Context) error {
			return service.WithBindingTransaction(ctx, func(ctx context.Context, _ sqlc.Querier) error {
				if err := service.WithBindingObservation(ctx, device.DatabaseID, func(sqlc.Querier) error { return nil },
					func(context.Context) error { close(loginEntered); <-releaseLogin; return nil }); err != nil {
					return err
				}
				return service.WithBindingDispatch(ctx, device.DatabaseID, func(sqlc.Querier) error { return nil }, func(context.Context) error { return nil })
			})
		})
	}()
	select {
	case <-loginEntered:
	case <-ctx.Done():
		t.Fatal("mutation login did not begin")
	}
	secondEntered := make(chan struct{}, 1)
	second := make(chan error, 1)
	go func() {
		second <- service.WithHostObservation(ctx, user.OrganizationID, device.DatabaseID, 0, func(ctx context.Context) error {
			return service.WithBindingObservation(ctx, device.DatabaseID, func(sqlc.Querier) error { return nil },
				func(context.Context) error { secondEntered <- struct{}{}; return nil })
		})
	}()
	require.Eventually(t, func() bool {
		var waiting bool
		err := database.DB.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity
 WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE '%SELECT discovered_device_id FROM device%FOR SHARE%')`).Scan(&waiting)
		return err == nil && waiting
	}, 5*time.Second, 10*time.Millisecond, "a second read must wait while a mutation attempt is still in login")
	select {
	case <-secondEntered:
		t.Fatal("mutation login acquired shared locks, allowing a competing observation")
	default:
	}
	release()
	require.NoError(t, <-first, "subsequent mutation must not upgrade a shared parent lock")
	require.NoError(t, <-second)
}

func TestUndispatchedDirectMutationRetiresAdmission(t *testing.T) {
	database, organizationID, deviceID, _, _ := queuedFenceFixture(t)
	service := devicewrite.NewService(database.DB)
	outcome := errors.New("rejected before transport")
	err := service.WithHostAttempt(t.Context(), organizationID, deviceID, 0, commandtype.StartMining, func(context.Context) error {
		return devicewrite.NotDispatched(outcome)
	})
	require.ErrorIs(t, err, outcome)
	var count int
	require.NoError(t, database.DB.QueryRowContext(t.Context(), "SELECT count(*) FROM device_write_attempt WHERE device_id=$1", deviceID).Scan(&count))
	require.Zero(t, count, "undispatched direct mutations are transient admissions, just like completed observations")
}

func TestBindingObservationBlocksMutationUntilReadReturns(t *testing.T) {
	cfg, err := testutil.GetTestConfig()
	require.NoError(t, err)
	database := testutil.NewDatabaseService(t, cfg)
	user := database.CreateSuperAdminUser()
	device := createDeletionBindingFixture(t, database, user.OrganizationID)
	finish := holdBindingObservation(t, database, user.OrganizationID, device.DatabaseID)
	service := devicewrite.NewService(database.DB)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	entered, done := make(chan struct{}, 1), make(chan error, 1)
	go func() {
		done <- service.WithHostAttempt(ctx, user.OrganizationID, device.DatabaseID, 0, commandtype.StartMining, func(ctx context.Context) error {
			return service.WithBindingDispatch(ctx, device.DatabaseID, func(sqlc.Querier) error { return nil },
				func(context.Context) error { entered <- struct{}{}; return nil })
		})
	}()
	require.Eventually(t, func() bool {
		var waiting bool
		err := database.DB.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity
 WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE '%SELECT discovered_device_id FROM device%FOR NO KEY UPDATE%')`).Scan(&waiting)
		return err == nil && waiting
	}, 5*time.Second, 10*time.Millisecond, "the mutation must wait on the active read's parent binding lock")
	select {
	case <-entered:
		t.Fatal("mutation reached the device while a same-miner read was still active")
	default:
	}
	finish()
	require.NoError(t, <-done)
}
