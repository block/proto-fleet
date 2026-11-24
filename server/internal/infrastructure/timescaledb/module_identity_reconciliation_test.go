package timescaledb

import (
	"database/sql"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/internal/testutil"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/require"
)

func newModuleIdentityControllers(t *testing.T) (*modulePSUIdentityRig, *modulePSUIdentityRig) {
	t.Helper()
	database := testutil.NewDatabaseService(t, nil)
	user := database.CreateSuperAdminUser()
	base := time.Now().UTC().Add(-5 * time.Minute).Truncate(time.Microsecond)
	var rigs []*modulePSUIdentityRig
	for range 2 {
		device := database.CreateDevice(user.OrganizationID, "proto")
		_, err := database.DB.ExecContext(t.Context(), "UPDATE discovered_device SET device_class=2 WHERE id=(SELECT discovered_device_id FROM device WHERE id=$1)", device.DatabaseID)
		require.NoError(t, err)
		rigs = append(rigs, &modulePSUIdentityRig{db: database.DB, deviceID: device.DatabaseID, device: device.ID, orgID: user.OrganizationID, base: base})
	}
	return rigs[0], rigs[1]
}

func TestStaleMalformedModuleClaimsPreserveNewerEvidence(t *testing.T) {
	for _, kind := range []string{"padded", "duplicate"} {
		t.Run(kind, func(t *testing.T) {
			first, second := newModuleIdentityControllers(t)
			poll := func(r *modulePSUIdentityRig, at time.Time) {
				t.Helper()
				module := confirmedModule(at, 1, "module-a")
				module.Metrics = &sdk.ContainerModuleMetrics{ObservedAt: at, PowerW: new(1000.0)}
				require.NoError(t, r.store(t, modulePSUSnapshot(at, sdk.ContainerTank{Position: 1, Modules: []sdk.ContainerModule{module}})))
			}
			poll(first, first.base)
			movedAt := first.base.Add(3 * time.Minute)
			poll(second, movedAt)
			moduleID := second.moduleIDAt(t, 1, 1)
			read := func() (bool, sql.NullFloat64) {
				t.Helper()
				var confirmed bool
				var power sql.NullFloat64
				require.NoError(t, second.db.QueryRowContext(t.Context(), "SELECT m.identity_confirmed,mm.power_w FROM container_module m JOIN container_module_latest_metrics mm ON mm.module_id=m.id WHERE m.id=$1", moduleID).Scan(&confirmed, &power))
				return confirmed, power
			}
			confirmed, power := read()
			require.True(t, confirmed)
			require.Equal(t, sql.NullFloat64{Float64: 1000, Valid: true}, power)
			malformed := func(at time.Time) *sdk.ContainerSnapshot {
				modules := []sdk.ContainerModule{confirmedModule(at, 1, " module-a ")}
				if kind == "duplicate" {
					modules = []sdk.ContainerModule{confirmedModule(at, 1, "module-a"), confirmedModule(at, 2, "module-a")}
				}
				return modulePSUSnapshot(at, sdk.ContainerTank{Position: 1, Modules: modules})
			}
			// Newer for A's inventory, but older than B's durable module observation.
			require.NoError(t, first.store(t, malformed(first.base.Add(time.Minute))))
			confirmed, power = read()
			require.True(t, confirmed, "stale rejected claims must not withdraw newer command eligibility")
			require.Equal(t, sql.NullFloat64{Float64: 1000, Valid: true}, power)
			require.Equal(t, moduleID, second.moduleIDAt(t, 1, 1))
			// A genuinely newer contradictory claim must still withdraw confirmation.
			require.NoError(t, first.store(t, malformed(movedAt.Add(time.Second))))
			confirmed, power = read()
			require.False(t, confirmed)
			require.False(t, power.Valid)
		})
	}
}

func TestModuleMoveClosesUnobservedBoardInstallation(t *testing.T) {
	for _, placement := range []string{"unchanged", "position", "tank", "controller"} {
		for _, evidence := range []string{"absent", "unknown", "rejected"} {
			t.Run(placement+"/"+evidence, func(t *testing.T) {
				first, second := newModuleIdentityControllers(t)
				board := identifiedBoard(first.base, "board-a")
				board.HashrateHS = new(100e12)
				require.NoError(t, first.store(t, modulePSUSnapshot(first.base, sdk.ContainerTank{Position: 1, Modules: []sdk.ContainerModule{confirmedModule(first.base, 1, "module-a", board)}})))
				moduleID := first.moduleIDAt(t, 1, 1)
				boardID := first.boardID(t, "board-a")
				var installationID, originalTank int64
				require.NoError(t, first.db.QueryRowContext(t.Context(), "SELECT id,tank_id FROM hashboard_installation WHERE hashboard_id=$1 AND removed_at IS NULL", boardID).Scan(&installationID, &originalTank))
				destination, tank, position := first, int32(1), int32(1)
				switch placement {
				case "position":
					position = 2
				case "tank":
					tank = 2
				case "controller":
					destination = second
				}
				movedAt := first.base.Add(3 * time.Minute)
				module := confirmedModule(movedAt, position, "module-a")
				if evidence != "absent" {
					reason := ""
					if evidence == "rejected" {
						reason = sdk.HashboardIdentityConflict
					}
					module.Hashboards = []sdk.ContainerHashboard{sdk.UnavailableHashboard(1, reason)}
				}
				require.NoError(t, destination.store(t, modulePSUSnapshot(movedAt, sdk.ContainerTank{Position: tank, Modules: []sdk.ContainerModule{module}})))
				require.Equal(t, moduleID, destination.moduleIDAt(t, tank, position))
				var deviceID, tankID int64
				var modulePosition int32
				var removedAt sql.NullTime
				require.NoError(t, first.db.QueryRowContext(t.Context(), "SELECT device_id,tank_id,module_position,removed_at FROM hashboard_installation WHERE id=$1", installationID).Scan(&deviceID, &tankID, &modulePosition, &removedAt))
				require.Equal(t, first.deviceID, deviceID)
				require.Equal(t, originalTank, tankID)
				require.EqualValues(t, 1, modulePosition)
				if placement == "unchanged" {
					require.False(t, removedAt.Valid, "unknown evidence alone cannot end an installation")
					return
				}
				require.True(t, removedAt.Valid, "a confirmed module move ends the previous installation")
				require.True(t, movedAt.Equal(removedAt.Time))
				var open int
				require.NoError(t, first.db.QueryRowContext(t.Context(), "SELECT count(*) FROM hashboard_installation WHERE module_id=$1 AND removed_at IS NULL", moduleID).Scan(&open))
				require.Zero(t, open, "do not invent a destination installation without observing the board")
				// Reappearance creates fresh placement evidence while preserving old history.
				at := movedAt.Add(time.Second)
				board = identifiedBoard(at, "board-a")
				board.HashrateHS = new(100e12)
				require.NoError(t, destination.store(t, modulePSUSnapshot(at, sdk.ContainerTank{Position: tank, Modules: []sdk.ContainerModule{confirmedModule(at, position, "module-a", board)}})))
				var newInstallation int64
				require.NoError(t, first.db.QueryRowContext(t.Context(), "SELECT id,device_id,module_position FROM hashboard_installation WHERE hashboard_id=$1 AND removed_at IS NULL", boardID).Scan(&newInstallation, &deviceID, &modulePosition))
				require.NotEqual(t, installationID, newInstallation)
				require.Equal(t, destination.deviceID, deviceID)
				require.Equal(t, position, modulePosition)
				var oldSamples, newSamples int
				require.NoError(t, first.db.QueryRowContext(t.Context(), "SELECT count(*) FILTER (WHERE installation_id=$1),count(*) FILTER (WHERE installation_id=$2) FROM hashboard_metrics WHERE hashboard_id=$3", installationID, newInstallation, boardID).Scan(&oldSamples, &newSamples))
				require.Equal(t, 1, oldSamples)
				require.Equal(t, 1, newSamples)
			})
		}
	}
}
