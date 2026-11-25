package migration

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/miner/interfaces"
	"github.com/block/proto-fleet/server/internal/domain/poolconfigurations"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
	"github.com/block/proto-fleet/server/internal/infrastructure/encrypt"
	"github.com/block/proto-fleet/server/internal/testutil/dbtest"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type snapshotReader func(sqlc.ListPoolDeviceImportSourcesRow) ([]interfaces.MinerStoredPool, error)

func (r snapshotReader) ReadPools(_ context.Context, _ int64, source sqlc.ListPoolDeviceImportSourcesRow) ([]interfaces.MinerStoredPool, error) {
	return r(source)
}

func TestDeviceImportConsolidatesCompleteStacksWithoutChangingHardware(t *testing.T) {
	conn := dbtest.GetTestDB(t)
	ctx := t.Context()
	secrets, err := encrypt.NewService(&encrypt.Config{ServiceMasterKey: base64.StdEncoding.EncodeToString(make([]byte, 32))})
	require.NoError(t, err)
	var org, root, account int64
	require.NoError(t, conn.QueryRowContext(ctx, `INSERT INTO organization(org_id,name) VALUES($1,'Import') RETURNING id`, uuid.NewString()).Scan(&org))
	require.NoError(t, conn.QueryRowContext(ctx, `SELECT id FROM hosted_account WHERE organization_id=$1 AND depth=0`, org).Scan(&root))
	require.NoError(t, conn.QueryRowContext(ctx, `INSERT INTO hosted_account(account_id,organization_id,parent_id,depth,name,capabilities) VALUES($1,$2,$3,1,'Customer',15) RETURNING id`, uuid.NewString(), org, root).Scan(&account))
	ids := make([]int64, 13)
	for i := range ids {
		identifier := uuid.NewString()
		var discovery int64
		require.NoError(t, conn.QueryRowContext(ctx, `INSERT INTO discovered_device(org_id,device_identifier,ip_address,port,url_scheme,driver_name,device_class) VALUES($1,$2,'192.0.2.1','80','http','proto',1) RETURNING id`, org, identifier).Scan(&discovery))
		require.NoError(t, conn.QueryRowContext(ctx, `INSERT INTO device(org_id,device_identifier,mac_address,discovered_device_id,worker_name) VALUES($1,$2,$3,$4,$5) RETURNING id`, org, identifier, fmt.Sprintf("02:00:00:00:01:%02d", i), discovery, fmt.Sprintf("rig-%d", i)).Scan(&ids[i]))
		_, err = conn.ExecContext(ctx, `INSERT INTO device_pairing(device_id,pairing_status) VALUES($1,'PAIRED')`, ids[i])
		require.NoError(t, err)
	}
	_, err = conn.ExecContext(ctx, `INSERT INTO hosted_account_asset(organization_id,kind,asset_id,device_id,miner_id,client_id) VALUES($1,5,$2,$2,$2,$3)`, org, ids[2], account)
	require.NoError(t, err)
	// Only the conflicting device is skipped; unrelated retained writes do not
	// prevent its readback or adoption, and other devices continue importing.
	require.NoError(t, sqlc.New(conn).EnsureDeviceWriteFences(ctx, ids))
	recordAttempt := func(deviceID int64, command string) {
		_, err := conn.ExecContext(ctx, `INSERT INTO device_write_attempt(attempt_id,device_id,organization_id,epoch,owner_instance_id,identity_snapshot,status,finished_at,command_type) VALUES($1,$2,$3,0,$4,'{}','unknown',now(),$5)`, uuid.New(), deviceID, org, uuid.New(), command)
		require.NoError(t, err)
	}
	recordAttempt(ids[6], "UpdateMiningPools")
	recordAttempt(ids[7], "BlinkLED")
	enqueueAdoptionCommand(t, conn, ids[9], "UpdateMiningPools", "PENDING")
	enqueueAdoptionCommand(t, conn, ids[10], "UpdateMiningPools", "PROCESSING")
	enqueueAdoptionCommand(t, conn, ids[11], "BlinkLED", "PENDING")
	password := "private-import-password"
	reads := 0
	reader := snapshotReader(func(source sqlc.ListPoolDeviceImportSourcesRow) ([]interfaces.MinerStoredPool, error) {
		require.NotContains(t, []int64{ids[6], ids[9], ids[10]}, source.ID, "conflicting work is checked before firmware reads")
		reads++
		pools := []interfaces.MinerStoredPool{
			{Priority: 1, URL: "stratum+tcp://backup.example", Username: "backup." + source.WorkerName, Password: &password},
			{Priority: 0, URL: "stratum+tcp://primary.example", Username: "payout." + source.WorkerName, Password: &password},
		}
		if source.ID == ids[1] {
			empty := ""
			pools = append(pools, interfaces.MinerStoredPool{Priority: 2, Password: &empty})
		}
		if source.ID == ids[3] {
			pools[0].Password = nil
		}
		if source.ID == ids[4] {
			return nil, fmt.Errorf("driver error containing %s", password)
		}
		return pools, nil
	})
	snapshots, err := CollectDevices(ctx, sqlc.New(conn), secrets, org, reader)
	require.NoError(t, err)
	require.Equal(t, 10, reads)
	recordAttempt(ids[8], "UpdateMiningPools") // rechecked between readback and commit
	enqueueAdoptionCommand(t, conn, ids[12], "UpdateMiningPools", "PENDING")
	// Ownership and physical identity must still agree at commit, even with all
	// application writers normally stopped during this maintenance command.
	_, err = conn.ExecContext(ctx, `UPDATE device SET worker_name='changed' WHERE id=$1`, ids[5])
	require.NoError(t, err)
	report, err := db.WithTransaction(ctx, conn, func(q sqlc.Querier) (poolconfigurations.ImportReport, error) {
		return Import(ctx, q, secrets, org, snapshots...)
	})
	require.NoError(t, err)
	require.Equal(t, 2, report.CreatedConfigurations)
	require.Len(t, report.AdoptedAssignments, 5)
	require.Len(t, report.Unresolved, 8)
	var first, second, customer string
	for i, destination := range []*string{&first, &second, &customer} {
		require.NoError(t, conn.QueryRowContext(ctx, `SELECT configuration_id FROM pool_configuration_assignment WHERE device_id=$1`, ids[i]).Scan(destination))
	}
	require.Equal(t, first, second)
	require.NotEqual(t, first, customer)
	var slotsJSON []byte
	require.NoError(t, conn.QueryRowContext(ctx, `SELECT slots FROM pool_configuration_revision WHERE configuration_id=$1`, first).Scan(&slotsJSON))
	slots, err := poolconfigurations.DecodeSlots(slotsJSON)
	require.NoError(t, err)
	require.Equal(t, "stratum+tcp://primary.example", slots[0].URL)
	require.Equal(t, "payout", slots[0].Username)
	require.Equal(t, "{miner_name}", slots[0].WorkerTemplate)
	plain, err := secrets.Decrypt(slots[0].PasswordCiphertext)
	require.NoError(t, err)
	require.Equal(t, password, string(plain))
	encoded, err := json.Marshal(report)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), password)
	require.NotContains(t, string(encoded), "ciphertext")
	var operations int
	require.NoError(t, conn.QueryRowContext(ctx, `SELECT count(*) FROM pool_configuration_operation`).Scan(&operations))
	require.Zero(t, operations)
	// Re-import cannot replace an accepted assignment, even if readback changed.
	again, err := db.WithTransaction(ctx, conn, func(q sqlc.Querier) (poolconfigurations.ImportReport, error) {
		return Import(ctx, q, secrets, org, snapshots...)
	})
	require.NoError(t, err)
	require.Zero(t, again.CreatedConfigurations)
	require.Empty(t, again.AdoptedAssignments)
	// A later complete readback joins the existing imported stack, even though
	// already-adopted devices are deliberately excluded from collection.
	snapshots, err = CollectDevices(ctx, sqlc.New(conn), secrets, org, reader)
	require.NoError(t, err)
	later, err := db.WithTransaction(ctx, conn, func(q sqlc.Querier) (poolconfigurations.ImportReport, error) {
		return Import(ctx, q, secrets, org, snapshots...)
	})
	require.NoError(t, err)
	require.Zero(t, later.CreatedConfigurations)
	require.Len(t, later.AdoptedAssignments, 1)
	require.Equal(t, first, later.AdoptedAssignments[0].ConfigurationID)
}

func TestDeviceImportNeverGuessesMissingCredentialsOrWorkerSuffix(t *testing.T) {
	secrets, err := encrypt.NewService(&encrypt.Config{ServiceMasterKey: base64.StdEncoding.EncodeToString(make([]byte, 32))})
	require.NoError(t, err)
	empty := ""
	slots, reason := importDeviceSlots(secrets, "rig", []interfaces.MinerStoredPool{{URL: "stratum+tcp://pool.example", Username: "account.fixed", Password: &empty}})
	require.Empty(t, reason)
	require.Equal(t, "account.fixed", slots[0].Username)
	require.Empty(t, slots[0].WorkerTemplate)
	plain, err := secrets.Decrypt(slots[0].PasswordCiphertext)
	require.NoError(t, err)
	require.Empty(t, plain)
	_, reason = importDeviceSlots(secrets, "rig", []interfaces.MinerStoredPool{{URL: "stratum+tcp://pool.example", Username: "account.rig"}})
	require.Contains(t, reason, "unknown")
	_, reason = importDeviceSlots(secrets, "rig", []interfaces.MinerStoredPool{{Priority: 0, Password: &empty}, {Priority: 0, Password: &empty}})
	require.Contains(t, reason, "ambiguous")
}

func TestDeviceImportTrimsOnlyKnownEmptyTrailingBackups(t *testing.T) {
	secrets, err := encrypt.NewService(&encrypt.Config{ServiceMasterKey: base64.StdEncoding.EncodeToString(make([]byte, 32))})
	require.NoError(t, err)
	empty, secret := "", "secret"
	primary := interfaces.MinerStoredPool{Priority: 0, URL: "stratum+tcp://primary.example", Username: "payout", Password: &secret}
	backup := interfaces.MinerStoredPool{Priority: 1, URL: "stratum+tcp://backup.example", Username: "backup", Password: &empty}
	emptyOne := interfaces.MinerStoredPool{Priority: 1, Password: &empty}
	emptyTwo := interfaces.MinerStoredPool{Priority: 2, Password: &empty}
	for _, tc := range []struct {
		name    string
		pools   []interfaces.MinerStoredPool
		count   int
		invalid bool
		reason  string
	}{
		{name: "two empty backups", pools: []interfaces.MinerStoredPool{primary, emptyOne, emptyTwo}, count: 1},
		{name: "sort before trimming", pools: []interfaces.MinerStoredPool{emptyTwo, primary, backup}, count: 2},
		{name: "populated backup", pools: []interfaces.MinerStoredPool{primary, backup}, count: 2},
		{name: "unknown trailing password", pools: []interfaces.MinerStoredPool{primary, {Priority: 1}}, reason: "unknown"},
		{name: "remaining username", pools: []interfaces.MinerStoredPool{primary, {Priority: 1, Username: "payout", Password: &empty}}, count: 2, invalid: true},
		{name: "remaining password", pools: []interfaces.MinerStoredPool{primary, {Priority: 1, Password: &secret}}, count: 2, invalid: true},
		{name: "interior empty slot", pools: []interfaces.MinerStoredPool{primary, emptyOne, {Priority: 2, URL: backup.URL, Username: backup.Username, Password: &empty}}, count: 3, invalid: true},
		{name: "all empty", pools: []interfaces.MinerStoredPool{{Priority: 0, Password: &empty}, emptyOne, emptyTwo}, count: 1, invalid: true},
		{name: "duplicate empty priorities", pools: []interfaces.MinerStoredPool{primary, emptyOne, emptyOne}, reason: "ambiguous"},
		{name: "negative empty priority", pools: []interfaces.MinerStoredPool{{Priority: -1, Password: &empty}, primary}, reason: "ambiguous"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			original := append([]interfaces.MinerStoredPool(nil), tc.pools...)
			slots, reason := importDeviceSlots(secrets, "", tc.pools)
			require.Equal(t, original, tc.pools, "readback input is not modified")
			if tc.reason != "" {
				require.Contains(t, reason, tc.reason)
				return
			}
			require.Empty(t, reason)
			require.Len(t, slots, tc.count)
			options := poolconfigurations.RevisionOptions{Name: "Imported"}
			for _, slot := range slots {
				plain, err := secrets.Decrypt(slot.PasswordCiphertext)
				require.NoError(t, err)
				password := string(plain)
				options.Slots = append(options.Slots, poolconfigurations.SlotOptions{URL: slot.URL, Username: slot.Username, WorkerTemplate: slot.WorkerTemplate, Password: &password})
			}
			_, err := poolconfigurations.Validate(options)
			if tc.invalid {
				require.Error(t, err, "incomplete slots must remain invalid for adoption")
			} else {
				require.NoError(t, err)
				require.Equal(t, primary.URL, slots[0].URL)
				require.Equal(t, secret, *options.Slots[0].Password)
			}
		})
	}
}
