package sqlstores_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/internal/domain/activity/models"
	sitesmodels "github.com/block/proto-fleet/server/internal/domain/sites/models"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/block/proto-fleet/server/internal/testutil"
)

// activitySiteFixture seeds the scenario shared by the site-scope filter
// suite. Each row carries a unique Description so the assertions can match on
// it without depending on the store-generated event_id.
//
// Direct (non-batch) events stamp the scalar site_id at write time:
//   - dirA  : site A, category fleet_management (site-shaped)
//   - dirB  : site B, category fleet_management
//   - dirNull: site_id NULL, category fleet_management → unassigned bucket
//   - dirAuth: site_id NULL, category auth (org-level) → all-sites ONLY,
//     never the unassigned bucket (Option B category exclusion)
//
// Command-batch events keep site_id NULL on activity_log; their touched sites
// come from command_on_device_log:
//   - batchAB : codl rows in site A and site B
//   - batchUn : one codl row with site_id NULL (unassigned devices only)
//   - batchMix: codl rows in site A and site_id NULL
//   - batchNone: no codl rows yet (initiated-before-completion) → all-sites only
type activitySiteFixture struct {
	orgID int64
	siteA int64
	siteB int64
	siteC int64
}

const (
	descDirA      = "direct event in site A"
	descDirB      = "direct event in site B"
	descDirNull   = "direct site-shaped unassigned event"
	descDirAuth   = "direct org-level auth event"
	descBatchAB   = "command batch touching sites A and B"
	descBatchUn   = "command batch touching unassigned devices"
	descBatchMix  = "command batch touching site A and unassigned"
	descBatchNone = "command batch with no completed devices yet"
)

func buildActivitySiteFixture(t *testing.T, ctx context.Context, tc *testutil.TestContext) activitySiteFixture {
	t.Helper()
	dbSvc := tc.DatabaseService
	db := tc.ServiceProvider.DB
	siteStore := sqlstores.NewSQLSiteStore(db)
	activityStore := sqlstores.NewSQLActivityStore(db)

	user := dbSvc.CreateSuperAdminUser()
	orgID := user.OrganizationID

	siteA, err := siteStore.CreateSite(ctx, sitesmodels.CreateSiteParams{OrgID: orgID, Name: "Site A"})
	require.NoError(t, err)
	siteB, err := siteStore.CreateSite(ctx, sitesmodels.CreateSiteParams{OrgID: orgID, Name: "Site B"})
	require.NoError(t, err)
	siteC, err := siteStore.CreateSite(ctx, sitesmodels.CreateSiteParams{OrgID: orgID, Name: "Site C"})
	require.NoError(t, err)

	insertDirect := func(desc string, category models.EventCategory, siteID *int64) {
		t.Helper()
		require.NoError(t, activityStore.Insert(ctx, &models.Event{
			Category:       category,
			Type:           "reboot",
			Description:    desc,
			Result:         models.ResultSuccess,
			ActorType:      models.ActorUser,
			OrganizationID: &orgID,
			SiteID:         siteID,
		}))
	}

	insertDirect(descDirA, models.CategoryFleetManagement, &siteA.ID)
	insertDirect(descDirB, models.CategoryFleetManagement, &siteB.ID)
	insertDirect(descDirNull, models.CategoryFleetManagement, nil)
	insertDirect(descDirAuth, models.CategoryAuth, nil)

	// Command-batch events. The activity_log row stamps batch_id + NULL
	// site_id; relevance derives from the per-device command_on_device_log
	// rows seeded below.
	insertBatchEvent := func(desc, batchUUID string) {
		t.Helper()
		require.NoError(t, activityStore.Insert(ctx, &models.Event{
			Category:       models.CategoryDeviceCommand,
			Type:           "reboot",
			Description:    desc,
			Result:         models.ResultSuccess,
			ActorType:      models.ActorUser,
			OrganizationID: &orgID,
			BatchID:        &batchUUID,
		}))
	}

	createBatch := func(uuid string) {
		t.Helper()
		_, err := db.ExecContext(ctx,
			`INSERT INTO command_batch_log (uuid, type, created_by, status, devices_count)
			 VALUES ($1, 'reboot', $2, 'FINISHED', 0)`,
			uuid, user.DatabaseID)
		require.NoError(t, err)
	}

	// seedCodl stamps a per-device command_on_device_log row at completion
	// time with an explicit site_id (nil → NULL, the unassigned bucket).
	seedCodl := func(batchUUID string, siteID *int64) {
		t.Helper()
		d := dbSvc.CreateDevice(orgID, "proto")
		_, err := db.ExecContext(ctx,
			`INSERT INTO command_on_device_log
			   (command_batch_log_id, device_id, status, org_id, site_id)
			 SELECT cbl.id, $2, 'SUCCESS', $3, $4
			 FROM command_batch_log cbl WHERE cbl.uuid = $1`,
			batchUUID, d.DatabaseID, orgID, siteID)
		require.NoError(t, err)
	}

	createBatch("batch-ab")
	seedCodl("batch-ab", &siteA.ID)
	seedCodl("batch-ab", &siteB.ID)
	insertBatchEvent(descBatchAB, "batch-ab")

	createBatch("batch-un")
	seedCodl("batch-un", nil)
	insertBatchEvent(descBatchUn, "batch-un")

	createBatch("batch-mix")
	seedCodl("batch-mix", &siteA.ID)
	seedCodl("batch-mix", nil)
	insertBatchEvent(descBatchMix, "batch-mix")

	createBatch("batch-none")
	insertBatchEvent(descBatchNone, "batch-none")

	return activitySiteFixture{orgID: orgID, siteA: siteA.ID, siteB: siteB.ID, siteC: siteC.ID}
}

// listDescriptions runs ListActivityLogs with the given site scope and returns
// the set of Descriptions, plus the CountActivityLogs total for parity checks.
func listActivityDescriptions(
	t *testing.T, ctx context.Context, store *sqlstores.SQLActivityStore,
	orgID int64, siteIDs []int64, includeUnassigned bool,
) (map[string]struct{}, int64) {
	t.Helper()
	filter := models.Filter{
		OrganizationID:    orgID,
		SiteIDs:           siteIDs,
		IncludeUnassigned: includeUnassigned,
		PageSize:          models.MaxPageSize,
	}
	entries, err := store.List(ctx, filter)
	require.NoError(t, err)
	got := make(map[string]struct{}, len(entries))
	for _, e := range entries {
		got[e.Description] = struct{}{}
	}
	count, err := store.Count(ctx, filter)
	require.NoError(t, err)
	return got, count
}

func TestActivityLogs_SiteScopeFilter(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}
	tc := testutil.InitializeDBServiceInfrastructure(t)
	ctx := t.Context()
	fx := buildActivitySiteFixture(t, ctx, tc)
	store := sqlstores.NewSQLActivityStore(tc.ServiceProvider.DB)

	cases := []struct {
		name              string
		siteIDs           []int64
		includeUnassigned bool
		want              []string
	}{
		{
			name:    "all sites (no filter) returns every org row",
			siteIDs: nil,
			want: []string{
				descDirA, descDirB, descDirNull, descDirAuth,
				descBatchAB, descBatchUn, descBatchMix, descBatchNone,
			},
		},
		{
			name:    "single site A: direct A + batches touching A",
			siteIDs: []int64{fx.siteA},
			want:    []string{descDirA, descBatchAB, descBatchMix},
		},
		{
			name:    "single site B: direct B + batch touching B (not the A-only mix)",
			siteIDs: []int64{fx.siteB},
			want:    []string{descDirB, descBatchAB},
		},
		{
			name:    "multi site A+B: OR across both",
			siteIDs: []int64{fx.siteA, fx.siteB},
			want:    []string{descDirA, descDirB, descBatchAB, descBatchMix},
		},
		{
			name:    "site C: nothing touched it",
			siteIDs: []int64{fx.siteC},
			want:    []string{},
		},
		{
			name:              "unassigned bucket: site-shaped NULL + unassigned batches, excludes auth",
			includeUnassigned: true,
			want:              []string{descDirNull, descBatchUn, descBatchMix},
		},
		{
			name:              "site A + unassigned: union of both branches",
			siteIDs:           []int64{fx.siteA},
			includeUnassigned: true,
			want:              []string{descDirA, descDirNull, descBatchAB, descBatchUn, descBatchMix},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, count := listActivityDescriptions(t, ctx, store, fx.orgID, tc.siteIDs, tc.includeUnassigned)

			want := make(map[string]struct{}, len(tc.want))
			for _, d := range tc.want {
				want[d] = struct{}{}
			}
			assert.Equal(t, want, got)
			// Count must match the filtered list cardinality exactly so the
			// pagination total never disagrees with the rendered feed/CSV.
			assert.Equal(t, int64(len(tc.want)), count, "CountActivityLogs parity")
		})
	}
}
