package sqlstores_test

import (
	"database/sql"
	"testing"

	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/block/proto-fleet/server/internal/testutil"
	"github.com/stretchr/testify/require"
)

func TestUpdateDeviceCustomNamesRollsBackShortWrites(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	testContext := testutil.InitializeDBServiceInfrastructure(t)
	user := testContext.DatabaseService.CreateSuperAdminUser()
	deviceIDs := testContext.DatabaseService.CreateTestMiners(user.OrganizationID, 1, "https://172.17.0.1:80")
	store := sqlstores.NewSQLDeviceStore(testContext.ServiceProvider.DB)

	err := store.UpdateDeviceCustomNames(t.Context(), user.OrganizationID, map[string]string{
		deviceIDs[0]:                "Changed",
		"missing-device-identifier": "Should fail",
	})
	require.Error(t, err)

	var customName sql.NullString
	require.NoError(t, testContext.ServiceProvider.DB.QueryRowContext(
		t.Context(),
		"SELECT custom_name FROM device WHERE org_id = $1 AND device_identifier = $2",
		user.OrganizationID,
		deviceIDs[0],
	).Scan(&customName))
	require.False(t, customName.Valid, "valid row rename should roll back when a batch update is short")
}
