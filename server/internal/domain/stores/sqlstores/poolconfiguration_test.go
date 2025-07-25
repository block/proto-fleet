package sqlstores_test

import (
	"database/sql"
	"testing"

	"github.com/btc-mining/proto-fleet/server/internal/domain/stores/sqlstores"

	pb "github.com/btc-mining/proto-fleet/server/generated/grpc/pools/v1"
	"github.com/btc-mining/proto-fleet/server/generated/sqlc"
	"github.com/btc-mining/proto-fleet/server/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSQLPoolConfigurationStore_GetPoolConfigurationsWithPools(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping test in short mode")
	}

	db := testutil.GetTestDB(t)
	store := sqlstores.NewSQLPoolConfigurationStore(db)
	ctx := t.Context()

	queries := sqlc.New(db)
	orgName := "Test Organization"
	orgResult, err := queries.CreateOrganization(ctx, sqlc.CreateOrganizationParams{
		Name: orgName,
	})
	require.NoError(t, err)

	orgID, err := orgResult.LastInsertId()
	require.NoError(t, err)
	require.Positive(t, orgID)

	configName := "Test Pool Config"
	configDesc := "Test Description"

	err = store.UpsertPoolConfiguration(ctx, orgID, &pb.PoolConfigurationBase{
		Name:        configName,
		Description: configDesc,
	})
	require.NoError(t, err)

	configID, err := store.GetPoolConfigurationIDByOrg(ctx, orgID)
	require.NoError(t, err)
	require.Positive(t, configID)

	pools := []*struct {
		name     string
		url      string
		username string
		priority int32
	}{
		{
			name:     "Pool 1",
			url:      "stratum+tcp://pool1.example.com:3333",
			username: "user1.worker1",
			priority: 0,
		},
		{
			name:     "Pool 2",
			url:      "stratum+tcp://pool2.example.com:3333",
			username: "user2.worker2",
			priority: 1,
		},
	}

	for _, p := range pools {
		poolResult, err := queries.CreatePool(ctx, sqlc.CreatePoolParams{
			OrgID:     orgID,
			PoolName:  p.name,
			Url:       p.url,
			Username:  p.username,
			IsDefault: sql.NullBool{Bool: false, Valid: true},
		})
		require.NoError(t, err)

		poolID, err := poolResult.LastInsertId()
		require.NoError(t, err)

		err = store.AddPoolToConfiguration(ctx, configID, poolID, p.priority)
		require.NoError(t, err)
	}

	result, err := store.ListPoolConfigurations(ctx, orgID)

	require.NoError(t, err)
	require.Len(t, result, 1)

	config := result[0]
	assert.Equal(t, configID, config.Configuration.Id)
	assert.Equal(t, configName, config.Configuration.Name)
	assert.Equal(t, configDesc, config.Configuration.Description)

	require.Len(t, config.Pools, len(pools))

	poolMap := make(map[string]*pb.PoolWithPriority)
	for _, p := range config.Pools {
		poolMap[p.Pool.PoolName] = p
	}

	for _, expectedPool := range pools {
		pool, exists := poolMap[expectedPool.name]
		require.True(t, exists, "Pool %s not found in results", expectedPool.name)

		assert.Equal(t, expectedPool.url, pool.Pool.Url)
		assert.Equal(t, expectedPool.username, pool.Pool.Username)
		assert.Equal(t, expectedPool.priority, pool.Priority)
		assert.False(t, pool.Pool.IsDefault)
	}

	emptyResult, err := store.ListPoolConfigurations(ctx, 12345)
	require.NoError(t, err)
	assert.Empty(t, emptyResult, "Expected empty result for non-existent org ID")

	err = store.DeletePoolConfigurationPools(ctx, configID)
	require.NoError(t, err)

	for _, p := range config.Pools {
		err = queries.DeletePool(ctx, p.Pool.PoolId)
		require.NoError(t, err)
	}

	err = store.DeletePoolConfiguration(ctx, orgID, configID)
	require.NoError(t, err)

	// Delete the test organization
	err = queries.DeleteOrganization(ctx, orgID)
	require.NoError(t, err)
}
