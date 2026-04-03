package apikey_test

import (
	"testing"

	"github.com/alecthomas/assert/v2"
	"github.com/block/proto-fleet/server/generated/sqlc"
	domainApiKey "github.com/block/proto-fleet/server/internal/domain/apikey"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	db2 "github.com/block/proto-fleet/server/internal/infrastructure/db"
	"github.com/block/proto-fleet/server/internal/testutil"
)

func TestServiceExcludesAPIKeysForDeactivatedUsers(t *testing.T) {
	testConfig, err := testutil.GetTestConfig()
	assert.NoError(t, err)

	databaseService := testutil.NewDatabaseService(t, testConfig)
	serviceProvider := testutil.NewServiceProvider(t, databaseService.DB, testConfig)

	testUser := databaseService.CreateSuperAdminUser()

	fullKey, _, err := serviceProvider.ApiKeyService.Create(
		t.Context(),
		testUser.DatabaseID,
		testUser.OrganizationID,
		"external-user-id",
		testUser.Username,
		"deactivated-user-key",
		nil,
	)
	assert.NoError(t, err)

	keys, err := serviceProvider.ApiKeyService.List(t.Context(), testUser.OrganizationID)
	assert.NoError(t, err)
	assert.Equal(t, 1, len(keys))

	validatedKey, err := serviceProvider.ApiKeyService.Validate(t.Context(), fullKey)
	assert.NoError(t, err)
	assert.Equal(t, testUser.DatabaseID, validatedKey.UserID)

	err = db2.WithTransactionNoResult(t.Context(), databaseService.DB, func(q *sqlc.Queries) error {
		return q.SoftDeleteUser(t.Context(), testUser.DatabaseID)
	})
	assert.NoError(t, err)

	keys, err = serviceProvider.ApiKeyService.List(t.Context(), testUser.OrganizationID)
	assert.NoError(t, err)
	assert.Equal(t, 0, len(keys))

	validatedKey, err = serviceProvider.ApiKeyService.Validate(t.Context(), fullKey)
	assert.Error(t, err)
	assert.Equal(t, nil, validatedKey)
	assert.Contains(t, err.Error(), "invalid api key")
}

func TestServiceAllowsNilActivityService(t *testing.T) {
	testConfig, err := testutil.GetTestConfig()
	assert.NoError(t, err)

	databaseService := testutil.NewDatabaseService(t, testConfig)
	testUser := databaseService.CreateSuperAdminUser()

	store := sqlstores.NewSQLApiKeyStore(databaseService.DB)
	service := domainApiKey.NewService(store, nil)

	_, apiKey, err := service.Create(
		t.Context(),
		testUser.DatabaseID,
		testUser.OrganizationID,
		"external-user-id",
		testUser.Username,
		"nil-activity-key",
		nil,
	)
	assert.NoError(t, err)

	err = service.Revoke(
		t.Context(),
		apiKey.KeyID,
		testUser.OrganizationID,
		"external-user-id",
		testUser.Username,
	)
	assert.NoError(t, err)
}
