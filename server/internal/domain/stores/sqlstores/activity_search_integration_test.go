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
