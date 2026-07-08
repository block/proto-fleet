package sqlstores_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/internal/domain/activity/models"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/block/proto-fleet/server/internal/testutil"
)

func TestActivityLogs_SearchMatchesDisplayedLabels(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	tc := testutil.InitializeDBServiceInfrastructure(t)
	ctx := t.Context()
	user := tc.DatabaseService.CreateSuperAdminUser()
	orgID := user.OrganizationID
	store := sqlstores.NewSQLActivityStore(tc.ServiceProvider.DB)

	groupScope := "group"
	groupName := "Label Search Group"

	events := []*models.Event{
		{
			Category:       models.CategoryAuth,
			Type:           "login_failed",
			Description:    "Login failed",
			Result:         models.ResultFailure,
			ActorType:      models.ActorUser,
			OrganizationID: &orgID,
		},
		{
			Category:       models.CategoryCollection,
			Type:           "create_collection",
			Description:    "Create group: " + groupName,
			Result:         models.ResultSuccess,
			ActorType:      models.ActorUser,
			ScopeType:      &groupScope,
			ScopeLabel:     &groupName,
			OrganizationID: &orgID,
		},
		{
			Category:       models.CategorySystem,
			Type:           "heartbeat",
			Description:    "Raw search marker",
			Result:         models.ResultSuccess,
			ActorType:      models.ActorSystem,
			OrganizationID: &orgID,
		},
		{
			Category:       models.CategoryPool,
			Type:           "create_pool",
			Description:    "Create pool",
			Result:         models.ResultSuccess,
			ActorType:      models.ActorUser,
			Metadata:       map[string]any{"pool_name": "Label Search Pool"},
			OrganizationID: &orgID,
		},
		{
			Category:       models.CategoryAuth,
			Type:           "create_user",
			Description:    "Create user",
			Result:         models.ResultSuccess,
			ActorType:      models.ActorUser,
			Metadata:       map[string]any{"target_username": "label-search-alice"},
			OrganizationID: &orgID,
		},
		{
			Category:       models.CategoryAuth,
			Type:           "update_user_role",
			Description:    "Update user role",
			Result:         models.ResultSuccess,
			ActorType:      models.ActorUser,
			Metadata:       map[string]any{"target_username": "label-search-bob", "role_name": "Operator"},
			OrganizationID: &orgID,
		},
		{
			Category:       models.CategoryDeviceCommand,
			Type:           "reboot.completed",
			Description:    "Reboot completed: 2 succeeded, 1 failed",
			Result:         models.ResultFailure,
			ActorType:      models.ActorUser,
			Metadata:       map[string]any{"success_count": 2, "failure_count": 1},
			OrganizationID: &orgID,
		},
		{
			Category:       models.CategoryFleetManagement,
			Type:           "site.deleted",
			Description:    "Deleted site 42 (1 building(s), 4 rack(s) unassigned, 9 device(s) unassigned)",
			Result:         models.ResultSuccess,
			ActorType:      models.ActorUser,
			Metadata:       map[string]any{"site_id": 42, "deleted_building_count": 1, "unassigned_rack_count": 4, "unassigned_device_count": 9},
			OrganizationID: &orgID,
		},
		{
			Category:       models.CategoryDeviceCommand,
			Type:           "command_filter_skip",
			Description:    "Command ran with 3 device(s) skipped",
			Result:         models.ResultSuccess,
			ActorType:      models.ActorSystem,
			Metadata:       map[string]any{"skipped_count": 3},
			OrganizationID: &orgID,
		},
		{
			Category:       models.CategorySchedule,
			Type:           "schedule_executed",
			Description:    `Schedule "Night Shift" executed (curtail) on 12 devices`,
			Result:         models.ResultSuccess,
			ActorType:      models.ActorSystem,
			OrganizationID: &orgID,
		},
		{
			Category:       models.CategoryCollection,
			Type:           "assign_devices_to_rack",
			Description:    "Cleared devices from rack",
			Result:         models.ResultSuccess,
			ActorType:      models.ActorUser,
			OrganizationID: &orgID,
		},
	}

	for _, event := range events {
		require.NoError(t, store.Insert(ctx, event))
	}

	cases := []struct {
		name   string
		search string
		want   []string
	}{
		{
			name:   "normalized auth label",
			search: "Couldn't log in",
			want:   []string{"Login failed"},
		},
		{
			name:   "normalized collection label with target",
			search: "Created group: " + groupName,
			want:   []string{"Create group: " + groupName},
		},
		{
			name:   "raw description still works",
			search: "Raw search marker",
			want:   []string{"Raw search marker"},
		},
		{
			name:   "metadata-backed pool label with target",
			search: "Created pool: Label Search Pool",
			want:   []string{"Create pool"},
		},
		{
			name:   "metadata-backed user label with target",
			search: "Created user: label-search-alice",
			want:   []string{"Create user"},
		},
		{
			name:   "metadata-backed role change label with target",
			search: "Updated role for label-search-bob",
			want:   []string{"Update user role"},
		},
		{
			name:   "completed command label with count ratio",
			search: "Rebooted miners: 2/3 miners completed",
			want:   []string{"Reboot completed: 2 succeeded, 1 failed"},
		},
		{
			name:   "site deletion label with unassignment counts",
			search: "Deleted site 42: 1 building, 4 racks unassigned, 9 miners unassigned",
			want:   []string{"Deleted site 42 (1 building(s), 4 rack(s) unassigned, 9 device(s) unassigned)"},
		},
		{
			name:   "skipped command label with miner count",
			search: "Command ran with 3 miners skipped",
			want:   []string{"Command ran with 3 device(s) skipped"},
		},
		{
			name:   "schedule label with quoted name",
			search: "Ran schedule: Night Shift",
			want:   []string{`Schedule "Night Shift" executed (curtail) on 12 devices`},
		},
		{
			name:   "rack clearing keeps clearing wording",
			search: "Cleared miners from rack",
			want:   []string{"Cleared devices from rack"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			filter := models.Filter{
				OrganizationID: orgID,
				SearchText:     tc.search,
				PageSize:       models.MaxPageSize,
			}
			entries, err := store.List(ctx, filter)
			require.NoError(t, err)

			got := make([]string, len(entries))
			for i, entry := range entries {
				got[i] = entry.Description
			}

			count, err := store.Count(ctx, filter)
			require.NoError(t, err)

			assert.ElementsMatch(t, tc.want, got)
			assert.Equal(t, int64(len(tc.want)), count, "CountActivityLogs parity")
		})
	}
}
