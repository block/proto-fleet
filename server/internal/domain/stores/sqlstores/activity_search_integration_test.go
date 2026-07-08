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
