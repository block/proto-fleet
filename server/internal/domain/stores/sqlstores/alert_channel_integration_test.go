package sqlstores_test

import (
	"context"
	"database/sql"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/internal/domain/alerts"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/block/proto-fleet/server/internal/testutil"
)

func newAlertChannelStore(t *testing.T) (*sqlstores.SQLAlertChannelStore, *sql.DB) {
	t.Helper()
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}
	db := testutil.GetTestDB(t)
	return sqlstores.NewSQLAlertChannelStore(db), db
}

func TestAlertChannelStoreCRUD(t *testing.T) {
	store, db := newAlertChannelStore(t)
	ctx := context.Background()

	created, err := store.Insert(ctx, alerts.ChannelRecord{
		OrganizationID:  7,
		Name:            "oncall",
		Kind:            alerts.ChannelKindSlack,
		EncryptedConfig: "cipher-blob-1",
		ValidationState: alerts.ValidationPending,
	})
	require.NoError(t, err)
	require.NotZero(t, created.ID)

	got, err := store.Get(ctx, 7, created.ID)
	require.NoError(t, err)
	assert.Equal(t, "oncall", got.Name)
	assert.Equal(t, "cipher-blob-1", got.EncryptedConfig)
	assert.Equal(t, alerts.ChannelKindSlack, got.Kind)

	// Org scoping: another org can't read it.
	_, err = store.Get(ctx, 8, created.ID)
	require.ErrorIs(t, err, alerts.ErrNotFound)

	byName, err := store.GetByName(ctx, 7, "oncall")
	require.NoError(t, err)
	assert.Equal(t, created.ID, byName.ID)

	updated, err := store.Update(ctx, alerts.ChannelRecord{
		ID:              created.ID,
		OrganizationID:  7,
		Name:            "oncall-renamed",
		Kind:            alerts.ChannelKindSlack,
		EncryptedConfig: "cipher-blob-2",
		ValidationState: alerts.ValidationOK,
	})
	require.NoError(t, err)
	assert.Equal(t, "oncall-renamed", updated.Name)
	assert.Equal(t, "cipher-blob-2", updated.EncryptedConfig)

	list, err := store.List(ctx, 7)
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Equal(t, "oncall-renamed", list[0].Name)

	require.NoError(t, store.SoftDelete(ctx, 7, created.ID))
	_, err = store.Get(ctx, 7, created.ID)
	require.ErrorIs(t, err, alerts.ErrNotFound)
	require.ErrorIs(t, store.SoftDelete(ctx, 7, created.ID), alerts.ErrNotFound)

	// Soft-delete clears the encrypted secret, so a deleted channel retains no webhook URL/bearer.
	var deletedConfig string
	require.NoError(t, db.QueryRowContext(ctx, `SELECT encrypted_config FROM alert_channel WHERE id = $1`, created.ID).Scan(&deletedConfig))
	assert.Empty(t, deletedConfig)

	// The soft-deleted name is free to reuse.
	_, err = store.Insert(ctx, alerts.ChannelRecord{OrganizationID: 7, Name: "oncall-renamed", Kind: alerts.ChannelKindWebhook, EncryptedConfig: "c", ValidationState: alerts.ValidationPending})
	require.NoError(t, err)
}

func TestAlertChannelStoreUpdateForeignOrgNotFound(t *testing.T) {
	store, _ := newAlertChannelStore(t)
	ctx := context.Background()
	created, err := store.Insert(ctx, alerts.ChannelRecord{OrganizationID: 7, Name: "c", Kind: alerts.ChannelKindSlack, EncryptedConfig: "x", ValidationState: alerts.ValidationPending})
	require.NoError(t, err)

	_, err = store.Update(ctx, alerts.ChannelRecord{ID: created.ID, OrganizationID: 9, Name: "c", Kind: alerts.ChannelKindSlack, EncryptedConfig: "y", ValidationState: alerts.ValidationPending})
	require.ErrorIs(t, err, alerts.ErrNotFound)
}

func TestAlertChannelStoreDeviceIdentities(t *testing.T) {
	store, db := newAlertChannelStore(t)
	ctx := context.Background()

	orgID := seedOrg(t, db, "device-identities-org")
	// custom_name wins over manufacturer/model; the second device has no custom name.
	seedDevice(t, db, orgID, "dev-uuid-1", "aa:bb:cc:dd:ee:01", "Bitmain", "Antminer S19", "Rig One")
	seedDevice(t, db, orgID, "dev-uuid-2", "aa:bb:cc:dd:ee:02", "Bitmain", "Antminer S21", "")

	got, err := store.DeviceIdentities(ctx, orgID, []string{"dev-uuid-1", "dev-uuid-2", "missing"})
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, alerts.DeviceIdentity{Name: "Rig One", MAC: "aa:bb:cc:dd:ee:01"}, got["dev-uuid-1"])
	assert.Equal(t, alerts.DeviceIdentity{Name: "Bitmain Antminer S21", MAC: "aa:bb:cc:dd:ee:02"}, got["dev-uuid-2"])

	empty, err := store.DeviceIdentities(ctx, orgID, nil)
	require.NoError(t, err)
	assert.Empty(t, empty)
}

func seedOrg(t *testing.T, db *sql.DB, name string) int64 {
	t.Helper()
	var id int64
	require.NoError(t, db.QueryRowContext(context.Background(),
		`INSERT INTO organization (org_id, name) VALUES ($1, $2) RETURNING id`,
		name, name).Scan(&id))
	return id
}

func seedDevice(t *testing.T, db *sql.DB, orgID int64, identifier, mac, manufacturer, model, customName string) {
	t.Helper()
	ctx := context.Background()
	var discoveredID int64
	require.NoError(t, db.QueryRowContext(ctx,
		`INSERT INTO discovered_device (org_id, device_identifier, manufacturer, model, driver_name, ip_address, port, url_scheme)
		 VALUES ($1, $2, $3, $4, 'antminer', '10.0.0.1', '80', 'http') RETURNING id`,
		orgID, identifier, manufacturer, model).Scan(&discoveredID))
	_, err := db.ExecContext(ctx,
		`INSERT INTO device (device_identifier, mac_address, org_id, discovered_device_id, custom_name)
		 VALUES ($1, $2, $3, $4, NULLIF($5, ''))`,
		identifier, mac, orgID, discoveredID, customName)
	require.NoError(t, err)
}
