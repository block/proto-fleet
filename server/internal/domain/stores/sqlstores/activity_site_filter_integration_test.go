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
	descDirA       = "direct event in site A"
	descDirB       = "direct event in site B"
	descDirNull    = "direct site-shaped unassigned event"
	descDirAuth    = "direct org-level auth event"
	descCollA      = "site-stamped collection event in site A"
	descPoolOrg    = "org-level pool config event"
	descSchedOrg   = "org-level schedule event"
	descCurtOrg    = "org-level curtailment event"
	descCmdOrg     = "org-level non-batch command audit event"
	descFleetMS    = "multi-device fleet event spanning sites A and B"
	descFleetMixed = "multi-device fleet event spanning site A and unassigned"
	descBatchAB    = "command batch touching sites A and B"
	descBatchUn    = "command batch touching unassigned devices"
	descBatchMix   = "command batch touching site A and unassigned"
	descBatchNone  = "command batch with no completed devices yet"
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
	// A site-scoped collection event that DOES stamp site_id (e.g. the
	// rack-slot emitters): belongs to its site, never the unassigned bucket.
	insertDirect(descCollA, models.CategoryCollection, &siteA.ID)
	// Org-level categories with NULL site_id: pool/schedule/curtailment have
	// no single-site concept, so they surface only in the all-sites feed and
	// must be excluded from the unassigned bucket (Option B category list).
	insertDirect(descPoolOrg, models.CategoryPool, nil)
	insertDirect(descSchedOrg, models.CategorySchedule, nil)
	insertDirect(descCurtOrg, models.CategoryCurtailment, nil)
	// A non-batch device-command audit (preflight-blocked / filter-skip): no
	// batch_id, no site_id. device_command is org-level for the direct branch,
	// so it must be excluded from the unassigned bucket (batch command rows are
	// handled separately by the codl join, exercised by the descBatch* rows).
	insertDirect(descCmdOrg, models.CategoryDeviceCommand, nil)

	// Multi-device fleet event (#538) whose touched device set spans sites A
	// and B: no single site_id, so it carries multi_site = true with site_id
	// NULL and one activity_log_site row per touched site. It surfaces under
	// /{siteA} AND /{siteB} (via membership) and in the all-sites feed, but
	// NOT the /unassigned bucket (no NULL-site membership row). A SINGLE-site
	// fleet batch instead stamps its scalar site_id (represented by descDirA).
	require.NoError(t, activityStore.Insert(ctx, &models.Event{
		Category:       models.CategoryFleetManagement,
		Type:           "rename_miners",
		Description:    descFleetMS,
		Result:         models.ResultSuccess,
		ActorType:      models.ActorUser,
		OrganizationID: &orgID,
		MultiSite:      true,
		MemberSiteIDs:  []int64{siteA.ID, siteB.ID},
	}))

	// Multi-device fleet event spanning site A AND site-less devices: membership
	// records site A plus a NULL-site row, so it surfaces under /{siteA}, the
	// all-sites feed, AND the /unassigned bucket — the mixed-scope case the
	// scalar-only model couldn't express.
	require.NoError(t, activityStore.Insert(ctx, &models.Event{
		Category:          models.CategoryFleetManagement,
		Type:              "unpair_miners",
		Description:       descFleetMixed,
		Result:            models.ResultSuccess,
		ActorType:         models.ActorUser,
		OrganizationID:    &orgID,
		MultiSite:         true,
		MemberSiteIDs:     []int64{siteA.ID},
		TouchesUnassigned: true,
	}))

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
				descCollA, descPoolOrg, descSchedOrg, descCurtOrg, descCmdOrg,
				descFleetMS, descFleetMixed,
				descBatchAB, descBatchUn, descBatchMix, descBatchNone,
			},
		},
		{
			name:    "single site A: direct A + batches touching A + multi-site events with A membership (#538)",
			siteIDs: []int64{fx.siteA},
			want:    []string{descDirA, descCollA, descBatchAB, descBatchMix, descFleetMS, descFleetMixed},
		},
		{
			name:    "single site B: direct B + batch touching B + the A/B multi-site event (not the A/unassigned one)",
			siteIDs: []int64{fx.siteB},
			want:    []string{descDirB, descBatchAB, descFleetMS},
		},
		{
			name:    "multi site A+B: OR across both, multi-site events surface once",
			siteIDs: []int64{fx.siteA, fx.siteB},
			want:    []string{descDirA, descDirB, descCollA, descBatchAB, descBatchMix, descFleetMS, descFleetMixed},
		},
		{
			name:    "site C: nothing touched it",
			siteIDs: []int64{fx.siteC},
			want:    []string{},
		},
		{
			name:              "unassigned bucket: site-shaped NULL + unassigned batches + multi-site events that touched site-less devices; excludes org-level and pure cross-site events (#538)",
			includeUnassigned: true,
			want:              []string{descDirNull, descBatchUn, descBatchMix, descFleetMixed},
		},
		{
			name:              "site A + unassigned: union of both branches incl. multi-site membership",
			siteIDs:           []int64{fx.siteA},
			includeUnassigned: true,
			want:              []string{descDirA, descCollA, descDirNull, descBatchAB, descBatchUn, descBatchMix, descFleetMS, descFleetMixed},
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

// TestActivityLogs_SiteDeleteCascadesMembership pins the FK referential action
// chosen for #538: a hard site delete CASCADE-removes that site's membership
// row (rather than nulling it), so a cross-site event stays attributed to its
// surviving site and is NOT misread as having touched unassigned devices. A
// NULL-site (SET NULL) action would alias the "touches unassigned" row and
// leak the event into /unassigned.
func TestActivityLogs_SiteDeleteCascadesMembership(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}
	tc := testutil.InitializeDBServiceInfrastructure(t)
	ctx := t.Context()
	db := tc.ServiceProvider.DB
	siteStore := sqlstores.NewSQLSiteStore(db)
	store := sqlstores.NewSQLActivityStore(db)

	user := tc.DatabaseService.CreateSuperAdminUser()
	orgID := user.OrganizationID
	siteA, err := siteStore.CreateSite(ctx, sitesmodels.CreateSiteParams{OrgID: orgID, Name: "Site A"})
	require.NoError(t, err)
	siteB, err := siteStore.CreateSite(ctx, sitesmodels.CreateSiteParams{OrgID: orgID, Name: "Site B"})
	require.NoError(t, err)

	const desc = "cross-site event surviving a site delete"
	require.NoError(t, store.Insert(ctx, &models.Event{
		Category:       models.CategoryFleetManagement,
		Type:           "rename_miners",
		Description:    desc,
		Result:         models.ResultSuccess,
		ActorType:      models.ActorUser,
		OrganizationID: &orgID,
		MultiSite:      true,
		MemberSiteIDs:  []int64{siteA.ID, siteB.ID},
	}))

	// Hard-delete site B (sites are normally soft-deleted; this exercises the
	// FK referential action directly).
	_, err = db.ExecContext(ctx, `DELETE FROM site WHERE id = $1 AND org_id = $2`, siteB.ID, orgID)
	require.NoError(t, err)

	// Site B's membership row cascaded away; only site A's remains.
	var memberRows int
	require.NoError(t, db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM activity_log_site als
		 JOIN activity_log a ON a.id = als.activity_log_id
		 WHERE a.organization_id = $1 AND a.description = $2`,
		orgID, desc).Scan(&memberRows))
	assert.Equal(t, 1, memberRows, "deleted site's membership row must cascade away, leaving only site A")

	// The event still surfaces under /{siteA} and must NOT have leaked into
	// the /unassigned bucket.
	gotA, _ := listActivityDescriptions(t, ctx, store, orgID, []int64{siteA.ID}, false)
	assert.Contains(t, gotA, desc, "event stays attributed to its surviving site")
	gotUnassigned, _ := listActivityDescriptions(t, ctx, store, orgID, nil, true)
	assert.NotContains(t, gotUnassigned, desc, "deleting a site must not leak the event into /unassigned")
}
