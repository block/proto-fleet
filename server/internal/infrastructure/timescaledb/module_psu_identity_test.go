package timescaledb

import (
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/testutil"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/require"
)

type modulePSUIdentityRig struct {
	db       *sql.DB
	deviceID int64
	orgID    int64
	device   string
	base     time.Time
}

func newModulePSUIdentityRig(t *testing.T) *modulePSUIdentityRig {
	t.Helper()
	database := testutil.NewDatabaseService(t, nil)
	user := database.CreateSuperAdminUser()
	device := database.CreateDevice(user.OrganizationID, "proto")
	_, err := database.DB.ExecContext(t.Context(), "UPDATE discovered_device SET device_class=2 WHERE id=(SELECT discovered_device_id FROM device WHERE id=$1)", device.DatabaseID)
	require.NoError(t, err)
	return &modulePSUIdentityRig{
		db:       database.DB,
		deviceID: device.DatabaseID,
		orgID:    user.OrganizationID,
		device:   device.ID,
		base:     time.Now().UTC().Add(-time.Minute).Truncate(time.Microsecond),
	}
}

func (r *modulePSUIdentityRig) store(t *testing.T, snapshot *sdk.ContainerSnapshot) error {
	t.Helper()
	tx, err := r.db.BeginTx(t.Context(), nil)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback() }()
	if err := storeContainerInventory(t.Context(), sqlc.New(tx), r.device, snapshot); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit module identity poll: %w", err)
	}
	return nil
}

func (r *modulePSUIdentityRig) moduleIDAt(t *testing.T, tank, position int32) int64 {
	t.Helper()
	var id int64
	err := r.db.QueryRowContext(t.Context(), `
		SELECT m.id
		FROM container_module m
		JOIN container_tank t ON t.id=m.tank_id
		WHERE t.device_id=$1 AND t.position=$2 AND m.position=$3 AND m.retired_at IS NULL`,
		r.deviceID, tank, position,
	).Scan(&id)
	require.NoError(t, err)
	return id
}

func (r *modulePSUIdentityRig) boardID(t *testing.T, serial string) int64 {
	t.Helper()
	var id int64
	require.NoError(t, r.db.QueryRowContext(t.Context(), `
		SELECT hashboard_id
		FROM hashboard_identity
		WHERE org_id=$1 AND kind='serial' AND value=$2`,
		r.orgID, serial,
	).Scan(&id))
	return id
}

func modulePSUSnapshot(at time.Time, tanks ...sdk.ContainerTank) *sdk.ContainerSnapshot {
	return &sdk.ContainerSnapshot{ObservedAt: at, TopologyComplete: true, Tanks: tanks}
}

func confirmedModule(at time.Time, position int32, reportedID string, boards ...sdk.ContainerHashboard) sdk.ContainerModule {
	return sdk.ContainerModule{
		Position:          position,
		ReportedID:        reportedID,
		IdentityConfirmed: true,
		State:             sdk.MinerStatusActive,
		ObservedAt:        at,
		Hashboards:        boards,
	}
}

func identifiedBoard(at time.Time, serial string) sdk.ContainerHashboard {
	return sdk.ContainerHashboard{
		Position:   1,
		Occupancy:  "occupied",
		Serial:     serial,
		State:      sdk.MinerStatusActive,
		ObservedAt: at,
	}
}

func TestConfirmedPSUModuleKeepsIDWhenPositionChanges(t *testing.T) {
	r := newModulePSUIdentityRig(t)

	require.NoError(t, r.store(t, modulePSUSnapshot(r.base,
		sdk.ContainerTank{Position: 1, Modules: []sdk.ContainerModule{confirmedModule(r.base, 1, "psu-a")}},
	)))
	original := r.moduleIDAt(t, 1, 1)

	at := r.base.Add(time.Second)
	require.NoError(t, r.store(t, modulePSUSnapshot(at,
		sdk.ContainerTank{Position: 1, Modules: []sdk.ContainerModule{confirmedModule(at, 2, "psu-a")}},
	)))
	require.Equal(t, original, r.moduleIDAt(t, 1, 2), "module identity must follow a PSU to another module position")

	at = r.base.Add(2 * time.Second)
	require.NoError(t, r.store(t, modulePSUSnapshot(at,
		sdk.ContainerTank{Position: 2, Modules: []sdk.ContainerModule{confirmedModule(at, 3, "psu-a")}},
	)))
	require.Equal(t, original, r.moduleIDAt(t, 2, 3), "module identity must follow a PSU to another tank")
}

func TestConfirmedPSUModulesKeepIDsWhenPositionsAreSwapped(t *testing.T) {
	r := newModulePSUIdentityRig(t)

	require.NoError(t, r.store(t, modulePSUSnapshot(r.base,
		sdk.ContainerTank{Position: 1, Modules: []sdk.ContainerModule{
			confirmedModule(r.base, 1, "psu-a"),
			confirmedModule(r.base, 2, "psu-b"),
		}},
	)))
	psuA := r.moduleIDAt(t, 1, 1)
	psuB := r.moduleIDAt(t, 1, 2)

	at := r.base.Add(time.Second)
	require.NoError(t, r.store(t, modulePSUSnapshot(at,
		sdk.ContainerTank{Position: 1, Modules: []sdk.ContainerModule{
			confirmedModule(at, 1, "psu-b"),
			confirmedModule(at, 2, "psu-a"),
		}},
	)))
	require.Equal(t, psuA, r.moduleIDAt(t, 1, 2))
	require.Equal(t, psuB, r.moduleIDAt(t, 1, 1))
}

func TestHashboardReplacementDoesNotReplaceConfirmedPSUModule(t *testing.T) {
	r := newModulePSUIdentityRig(t)

	boardA := identifiedBoard(r.base, "board-a")
	require.NoError(t, r.store(t, modulePSUSnapshot(r.base,
		sdk.ContainerTank{Position: 1, Modules: []sdk.ContainerModule{confirmedModule(r.base, 1, "psu-a", boardA)}},
	)))
	moduleID := r.moduleIDAt(t, 1, 1)
	boardAID := r.boardID(t, "board-a")

	at := r.base.Add(time.Second)
	boardB := identifiedBoard(at, "board-b")
	require.NoError(t, r.store(t, modulePSUSnapshot(at,
		sdk.ContainerTank{Position: 1, Modules: []sdk.ContainerModule{confirmedModule(at, 1, "psu-a", boardB)}},
	)))
	require.Equal(t, moduleID, r.moduleIDAt(t, 1, 1))
	boardBID := r.boardID(t, "board-b")
	require.NotEqual(t, boardAID, boardBID)

	var currentBoard int64
	require.NoError(t, r.db.QueryRowContext(t.Context(), `
		SELECT hashboard_id FROM hashboard_installation
		WHERE module_id=$1 AND position=1 AND removed_at IS NULL`, moduleID,
	).Scan(&currentBoard))
	require.Equal(t, boardBID, currentBoard)
}

func TestConfirmedPSUReplacementKeepsHashboardIdentity(t *testing.T) {
	r := newModulePSUIdentityRig(t)

	board := identifiedBoard(r.base, "board-a")
	require.NoError(t, r.store(t, modulePSUSnapshot(r.base,
		sdk.ContainerTank{Position: 1, Modules: []sdk.ContainerModule{confirmedModule(r.base, 1, "psu-a", board)}},
	)))
	originalModule := r.moduleIDAt(t, 1, 1)
	originalBoard := r.boardID(t, "board-a")

	at := r.base.Add(time.Second)
	board = identifiedBoard(at, "board-a")
	require.NoError(t, r.store(t, modulePSUSnapshot(at,
		sdk.ContainerTank{Position: 1, Modules: []sdk.ContainerModule{confirmedModule(at, 1, "psu-b", board)}},
	)))
	replacementModule := r.moduleIDAt(t, 1, 1)
	require.NotEqual(t, originalModule, replacementModule)
	require.Equal(t, originalBoard, r.boardID(t, "board-a"))

	var installedModule int64
	require.NoError(t, r.db.QueryRowContext(t.Context(), `
		SELECT module_id FROM hashboard_installation
		WHERE hashboard_id=$1 AND removed_at IS NULL`, originalBoard,
	).Scan(&installedModule))
	require.Equal(t, replacementModule, installedModule)
}

func TestDuplicateConfirmedPSUWithholdsIdentityAndMetrics(t *testing.T) {
	r := newModulePSUIdentityRig(t)
	powerA, powerB := 1200.0, 2400.0
	first := confirmedModule(r.base, 1, "duplicate-psu")
	first.Metrics = &sdk.ContainerModuleMetrics{ObservedAt: r.base, PowerW: &powerA}
	second := confirmedModule(r.base, 2, "duplicate-psu")
	second.Metrics = &sdk.ContainerModuleMetrics{ObservedAt: r.base, PowerW: &powerB}

	require.NoError(t, r.store(t, modulePSUSnapshot(r.base,
		sdk.ContainerTank{Position: 1, Modules: []sdk.ContainerModule{first, second}},
	)))

	var modules, confirmed, identified, samples int
	require.NoError(t, r.db.QueryRowContext(t.Context(), "SELECT count(*) FROM container_module WHERE retired_at IS NULL").Scan(&modules))
	require.NoError(t, r.db.QueryRowContext(t.Context(), "SELECT count(*) FROM container_module WHERE identity_confirmed").Scan(&confirmed))
	require.NoError(t, r.db.QueryRowContext(t.Context(), "SELECT count(*) FROM container_module WHERE reported_id='duplicate-psu'").Scan(&identified))
	require.NoError(t, r.db.QueryRowContext(t.Context(), "SELECT count(*) FROM container_module_metrics").Scan(&samples))
	require.Equal(t, 2, modules, "ambiguous PSU identity must not merge distinct physical positions")
	require.Zero(t, confirmed)
	require.Zero(t, identified)
	require.Zero(t, samples, "metrics cannot be attributed when PSU identity is ambiguous")
}

func TestConfirmedPSUIdentityIsScopedToOrganization(t *testing.T) {
	database := testutil.NewDatabaseService(t, nil)
	firstUser := database.CreateSuperAdminUser()
	secondUser := database.CreateSuperAdminUser2()
	firstDevice := database.CreateDevice(firstUser.OrganizationID, "proto")
	secondDevice := database.CreateDevice(secondUser.OrganizationID, "proto")
	_, err := database.DB.ExecContext(t.Context(), `
		UPDATE discovered_device SET device_class=2
		WHERE id IN (SELECT discovered_device_id FROM device WHERE id IN ($1,$2))`,
		firstDevice.DatabaseID, secondDevice.DatabaseID,
	)
	require.NoError(t, err)
	base := time.Now().UTC().Add(-time.Minute).Truncate(time.Microsecond)
	store := func(identifier string) {
		t.Helper()
		tx, err := database.DB.BeginTx(t.Context(), nil)
		require.NoError(t, err)
		defer func() { _ = tx.Rollback() }()
		require.NoError(t, storeContainerInventory(t.Context(), sqlc.New(tx), identifier, modulePSUSnapshot(base,
			sdk.ContainerTank{Position: 1, Modules: []sdk.ContainerModule{confirmedModule(base, 1, "shared-psu")}},
		)))
		require.NoError(t, tx.Commit())
	}
	store(firstDevice.ID)
	store(secondDevice.ID)

	moduleID := func(deviceID int64) int64 {
		t.Helper()
		var id int64
		require.NoError(t, database.DB.QueryRowContext(t.Context(), `
			SELECT m.id FROM container_module m
			JOIN container_tank t ON t.id=m.tank_id
			WHERE t.device_id=$1 AND m.reported_id='shared-psu'
			  AND m.identity_confirmed AND m.retired_at IS NULL`, deviceID,
		).Scan(&id))
		return id
	}
	require.NotEqual(t, moduleID(firstDevice.DatabaseID), moduleID(secondDevice.DatabaseID))
}

func TestDelayedPollCannotUndoConfirmedPSUMove(t *testing.T) {
	r := newModulePSUIdentityRig(t)

	require.NoError(t, r.store(t, modulePSUSnapshot(r.base,
		sdk.ContainerTank{Position: 1, Modules: []sdk.ContainerModule{confirmedModule(r.base, 1, "psu-a")}},
	)))
	original := r.moduleIDAt(t, 1, 1)

	movedAt := r.base.Add(2 * time.Second)
	require.NoError(t, r.store(t, modulePSUSnapshot(movedAt,
		sdk.ContainerTank{Position: 2, Modules: []sdk.ContainerModule{confirmedModule(movedAt, 3, "psu-a")}},
	)))
	delayedAt := r.base.Add(time.Second)
	require.NoError(t, r.store(t, modulePSUSnapshot(delayedAt,
		sdk.ContainerTank{Position: 1, Modules: []sdk.ContainerModule{confirmedModule(delayedAt, 1, "psu-a")}},
	)))

	require.Equal(t, original, r.moduleIDAt(t, 2, 3))
	var staleCurrent int
	require.NoError(t, r.db.QueryRowContext(t.Context(), `
		SELECT count(*) FROM container_module m
		JOIN container_tank t ON t.id=m.tank_id
		WHERE t.device_id=$1 AND t.position=1 AND m.position=1 AND m.retired_at IS NULL`, r.deviceID,
	).Scan(&staleCurrent))
	require.Zero(t, staleCurrent)
}
