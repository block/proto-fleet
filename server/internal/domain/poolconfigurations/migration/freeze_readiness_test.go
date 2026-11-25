package migration

import (
	"database/sql"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/miner/interfaces"
	"github.com/block/proto-fleet/server/internal/domain/poolconfigurations"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
	"github.com/stretchr/testify/require"
)

func requireFreezeRejected(t *testing.T, conn *sql.DB, org int64, kind string) {
	t.Helper()
	err := db.WithTransactionNoResult(t.Context(), conn, func(q sqlc.Querier) error {
		return FreezeCutover(t.Context(), q, org)
	})
	require.ErrorContains(t, err, kind)
	require.False(t, cutoverMarkerExists(t, conn, org), "rejected freeze leaves normal assignment and cleanup available")
}

// cutoverMarkerExists reports whether the organization has a cutover marker.
func cutoverMarkerExists(t *testing.T, conn *sql.DB, org int64) bool {
	t.Helper()
	var exists bool
	require.NoError(t, conn.QueryRowContext(t.Context(), `SELECT EXISTS(SELECT 1 FROM pool_configuration_cutover WHERE organization_id=$1)`, org).Scan(&exists))
	return exists
}

func TestCutoverIgnoresSoftDeletedPools(t *testing.T) {
	conn, cipher, org := newCutoverFixture(t)
	ctx, q := t.Context(), sqlc.New(conn)
	deleted, err := q.CreatePool(ctx, sqlc.CreatePoolParams{OrgID: org, PoolName: "Deleted", Url: "stratum+tcp://deleted.example", Username: "worker", CreatedAt: time.Now()})
	require.NoError(t, err)
	require.NoError(t, q.SoftDeletePool(ctx, sqlc.SoftDeletePoolParams{OrgID: org, ID: deleted}))
	active, err := q.CreatePool(ctx, sqlc.CreatePoolParams{OrgID: org, PoolName: "Active", Url: "stratum+tcp://active.example", Username: "worker", CreatedAt: time.Now()})
	require.NoError(t, err)
	inspection, err := InspectCutover(ctx, q, org)
	require.NoError(t, err)
	require.Len(t, inspection.Gaps, 1, "only the active library record requires import")
	require.Equal(t, "legacy_library", inspection.Gaps[0].Source.Kind)
	requireFreezeRejected(t, conn, org, "legacy_library")
	imported, err := db.WithTransaction(ctx, conn, func(q sqlc.Querier) (poolconfigurations.ImportReport, error) {
		return Import(ctx, q, cipher, org)
	})
	require.NoError(t, err)
	require.Len(t, imported.Imported, 1)
	require.Empty(t, imported.Unresolved)
	freezeCutoverFixture(t, conn, org)
	completed, err := db.WithTransaction(ctx, conn, func(q sqlc.Querier) (CutoverReport, error) {
		return CompleteCutover(ctx, q, org)
	})
	require.NoError(t, err)
	require.True(t, completed.Complete)
	var deletedAt sql.NullTime
	require.NoError(t, conn.QueryRowContext(ctx, `SELECT deleted_at FROM pool WHERE id=$1`, deleted).Scan(&deletedAt))
	require.True(t, deletedAt.Valid, "cutover preserves deleted history without importing it")
	var importedSource int64
	require.NoError(t, conn.QueryRowContext(ctx, `SELECT source_id::bigint FROM pool_configuration_import WHERE organization_id=$1 AND source_kind='operator_pool'`, org).Scan(&importedSource))
	require.Equal(t, active, importedSource)
}

func TestFreezeRequiresDeviceImportToFinish(t *testing.T) {
	for _, scenario := range []string{"remote", "empty readback", "missing password", "complete"} {
		t.Run(scenario, func(t *testing.T) {
			conn, cipher, org := newCutoverFixture(t)
			ctx, q := t.Context(), sqlc.New(conn)
			device := newCutoverDevice(t, conn, org)
			_, err := q.UpsertDevicePairing(ctx, sqlc.UpsertDevicePairingParams{DeviceID: device, PairingStatus: sqlc.PairingStatusEnumPAIRED})
			require.NoError(t, err)
			if scenario == "remote" {
				var node int64
				require.NoError(t, conn.QueryRowContext(ctx, `INSERT INTO fleet_node(org_id,name,identity_pubkey,encryption_pubkey,enrollment_status) VALUES($1,'Node',$2,$3,'CONFIRMED') RETURNING id`, org, []byte("import-node"), make([]byte, 32)).Scan(&node))
				_, err = conn.ExecContext(ctx, `INSERT INTO fleet_node_device(fleet_node_id,device_id,org_id) VALUES($1,$2,$3)`, node, device, org)
				require.NoError(t, err)
			}
			snapshots, err := CollectDevices(ctx, q, cipher, org, snapshotReader(func(sqlc.ListPoolDeviceImportSourcesRow) ([]interfaces.MinerStoredPool, error) {
				require.NotEqual(t, "remote", scenario, "offline import must not read Fleet Node equipment")
				if scenario == "empty readback" {
					return nil, nil
				}
				password := "private-password"
				pool := interfaces.MinerStoredPool{URL: "stratum+tcp://pool.example", Username: "worker", Password: &password}
				if scenario == "missing password" {
					pool.Password = nil
				}
				return []interfaces.MinerStoredPool{pool}, nil
			}))
			require.NoError(t, err)
			requireFreezeRejected(t, conn, org, "unmanaged_device")
			report, err := db.WithTransaction(ctx, conn, func(q sqlc.Querier) (poolconfigurations.ImportReport, error) {
				return Import(ctx, q, cipher, org, snapshots...)
			})
			require.NoError(t, err)
			if scenario != "complete" {
				require.Empty(t, report.AdoptedAssignments)
				// Empty readback is skipped by collection without an import error. Freeze
				// must still inspect persisted gaps rather than trust an empty report.
				requireFreezeRejected(t, conn, org, "unmanaged_device")
				return
			}
			require.Empty(t, report.Unresolved)
			require.Len(t, report.AdoptedAssignments, 1)
			freezeCutoverFixture(t, conn, org)
			completed, err := db.WithTransaction(ctx, conn, func(q sqlc.Querier) (CutoverReport, error) {
				return CompleteCutover(ctx, q, org)
			})
			require.NoError(t, err)
			require.True(t, completed.Complete)
		})
	}
}

func TestFreezeRechecksChangesAfterImport(t *testing.T) {
	conn, cipher, org := newCutoverFixture(t)
	ctx, q := t.Context(), sqlc.New(conn)
	library, err := q.CreatePool(ctx, sqlc.CreatePoolParams{OrgID: org, PoolName: "Library", Url: "stratum+tcp://pool.example", Username: "worker", CreatedAt: time.Now()})
	require.NoError(t, err)
	importLibrary := func() {
		t.Helper()
		report, err := db.WithTransaction(ctx, conn, func(q sqlc.Querier) (poolconfigurations.ImportReport, error) {
			return Import(ctx, q, cipher, org)
		})
		require.NoError(t, err)
		require.Empty(t, report.Unresolved)
	}
	importLibrary()
	ready, err := InspectCutover(ctx, q, org)
	require.NoError(t, err)
	require.Empty(t, ready.Gaps)
	// A writer committed after the import/inspection. Freeze must inspect the
	// current version under locks, not trust an earlier successful import report.
	_, err = conn.ExecContext(ctx, `UPDATE pool SET username='changed',updated_at=updated_at+interval '1 second' WHERE id=$1`, library)
	require.NoError(t, err)
	requireFreezeRejected(t, conn, org, "legacy_library")
	importLibrary()
	freezeCutoverFixture(t, conn, org)
}
