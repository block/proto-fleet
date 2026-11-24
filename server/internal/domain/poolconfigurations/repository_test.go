package poolconfigurations

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
	"github.com/block/proto-fleet/server/internal/testutil/dbtest"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestRepositoryRevisionHistoryAndMetadataOnlyEdits(t *testing.T) {
	conn := dbtest.GetTestDB(t)
	ctx := t.Context()
	var org int64
	require.NoError(t, conn.QueryRowContext(ctx, `INSERT INTO organization(org_id,name) VALUES($1,'Pools') RETURNING id`, uuid.NewString()).Scan(&org))
	scope := Scope{OrganizationID: org}
	repo := NewRepository(testCipher(t))
	password := "private-password"
	options := RevisionOptions{Name: "Production", Slots: []SlotOptions{{URL: "stratum+tcp://pool.example", Username: "payout", Password: &password}}}
	first, err := db.WithTransaction(ctx, conn, func(q sqlc.Querier) (Configuration, error) {
		return repo.Create(ctx, q, scope, options)
	})
	require.NoError(t, err)
	require.NotEmpty(t, first.Slots[0].ID)
	public, err := json.Marshal(first)
	require.NoError(t, err)
	require.NotContains(t, string(public), password)
	require.NotContains(t, string(public), "ciphertext")
	options.Slots[0].ID = first.Slots[0].ID
	options.Slots[0].Password = nil
	options.Name = "Renamed"
	save := func(ctx context.Context, scope Scope, version int64, options RevisionOptions) (SaveResult, error) {
		return db.WithTransaction(ctx, conn, func(q sqlc.Querier) (SaveResult, error) {
			return repo.Revise(ctx, q, scope, first.ID, version, options)
		})
	}
	renamed, err := save(ctx, scope, first.Version, options)
	require.NoError(t, err)
	require.False(t, renamed.SettingsChanged)
	require.Equal(t, first.RevisionID, renamed.Configuration.RevisionID)
	require.Equal(t, "Renamed", renamed.Configuration.Name)
	options.Slots[0].Password = &password
	same, err := save(ctx, scope, renamed.Configuration.Version, options)
	require.NoError(t, err)
	require.False(t, same.SettingsChanged, "re-encryption of the same password is not a configuration change")
	require.Equal(t, renamed.Configuration.Version, same.Configuration.Version)
	options.Slots[0].URL = "stratum+tcp://new.example"
	updated, err := save(ctx, scope, renamed.Configuration.Version, options)
	require.NoError(t, err)
	require.True(t, updated.SettingsChanged)
	require.NotEqual(t, first.RevisionID, updated.Configuration.RevisionID)
	_, err = save(ctx, scope, renamed.Configuration.Version, options)
	require.True(t, fleeterror.IsFailedPreconditionError(err))
	var oldURL string
	require.NoError(t, conn.QueryRowContext(ctx, `SELECT slots->0->>'url' FROM pool_configuration_revision WHERE revision_id=$1`, first.RevisionID).Scan(&oldURL))
	require.Equal(t, "stratum+tcp://pool.example", oldURL)
	var count int
	require.NoError(t, conn.QueryRowContext(ctx, `SELECT count(*) FROM pool_configuration_revision WHERE configuration_id=$1`, first.ID).Scan(&count))
	require.Equal(t, 2, count)
	_, err = save(ctx, Scope{OrganizationID: org, AccountID: 123}, updated.Configuration.Version, options)
	require.Error(t, err, "an account scope cannot revise an organization configuration")
}

func TestConfigurationCreationRollsBackWithCallerTransaction(t *testing.T) {
	conn := dbtest.GetTestDB(t)
	ctx := t.Context()
	var org int64
	require.NoError(t, conn.QueryRowContext(ctx, `INSERT INTO organization(org_id,name) VALUES($1,'Pools') RETURNING id`, uuid.NewString()).Scan(&org))
	repo := NewRepository(testCipher(t))
	err := db.WithTransactionNoResult(ctx, conn, func(q sqlc.Querier) error {
		_, err := repo.Create(ctx, q, Scope{OrganizationID: org}, RevisionOptions{Name: "Pools", Slots: []SlotOptions{{URL: "stratum+tcp://pool.example", Username: "payout"}}})
		if err != nil {
			return err
		}
		return fleeterror.NewFailedPreconditionError("review became stale")
	})
	require.ErrorContains(t, err, "review became stale")
	var count int
	require.NoError(t, conn.QueryRowContext(ctx, `SELECT count(*) FROM pool_configuration WHERE organization_id=$1`, org).Scan(&count))
	require.Zero(t, count)
}
