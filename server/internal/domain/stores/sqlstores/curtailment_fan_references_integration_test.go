package sqlstores_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	infradb "github.com/block/proto-fleet/server/internal/infrastructure/db"
	"github.com/block/proto-fleet/server/internal/testutil"
)

func TestSQLCurtailmentFanReferences_ProfileOverlapUsesCallerTransaction(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping database integration test in short mode")
	}

	testContext := testutil.InitializeDBServiceInfrastructure(t)
	user := testContext.DatabaseService.CreateSuperAdminUser()
	otherUser := testContext.DatabaseService.CreateSuperAdminUser2()
	database := testContext.DatabaseService.DB
	ctx := t.Context()
	siteA, fanA := seedCurtailmentFacilityFan(t, database, user.OrganizationID, "reference-fan-a")
	siteB, fanB := seedCurtailmentFacilityFan(t, database, user.OrganizationID, "reference-fan-b")
	otherSite, otherFan := seedCurtailmentFacilityFan(t, database, otherUser.OrganizationID, "reference-other-org-fan")

	tx, err := database.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer func() { require.NoError(t, tx.Rollback()) }()
	_, err = tx.ExecContext(ctx, `
		INSERT INTO curtailment_response_profile (
			org_id, profile_name, mode, scope_json, facility_fan_device_ids,
			authorization_envelope_jsonb
		) VALUES
			($1, 'Both selected fans', 'FULL_FLEET', '{"whole_org":true}', ARRAY[$3::bigint, $4::bigint],
			 jsonb_build_object('schema_version', 1, 'selected_resource_site_ids', '[]'::jsonb,
			 'current_member_site_ids', '[]'::jsonb, 'miner_scope_unbounded', true,
			 'facility_fan_site_ids', jsonb_build_array($6::bigint, $7::bigint), 'facility_fan_scope_unbounded', false)),
			($1, 'One selected fan', 'FULL_FLEET', '{"whole_org":true}', ARRAY[$4::bigint],
			 jsonb_build_object('schema_version', 1, 'selected_resource_site_ids', '[]'::jsonb,
			 'current_member_site_ids', '[]'::jsonb, 'miner_scope_unbounded', true,
			 'facility_fan_site_ids', jsonb_build_array($7::bigint), 'facility_fan_scope_unbounded', false)),
			($2, 'Other organization fan', 'FULL_FLEET', '{"whole_org":true}', ARRAY[$5::bigint],
			 jsonb_build_object('schema_version', 1, 'selected_resource_site_ids', '[]'::jsonb,
			 'current_member_site_ids', '[]'::jsonb, 'miner_scope_unbounded', true,
			 'facility_fan_site_ids', jsonb_build_array($8::bigint), 'facility_fan_scope_unbounded', false))
	`, user.OrganizationID, otherUser.OrganizationID, fanA, fanB, otherFan, siteA, siteB, otherSite)
	require.NoError(t, err)

	txCtx := infradb.WithTxQueries(ctx, sqlc.New(tx))
	sites := sqlstores.NewSQLSiteStore(database)
	devices := sqlstores.NewSQLInfrastructureDeviceStore(database)
	for _, selection := range []struct {
		name string
		org  int64
		ids  []int64
		want int64
	}{
		{"empty selection", user.OrganizationID, nil, 0},
		{"one matching fan", user.OrganizationID, []int64{fanA}, 1},
		{"any overlap counts each profile once", user.OrganizationID, []int64{fanA, fanB, fanB, otherFan}, 2},
		{"other organization isolated", otherUser.OrganizationID, []int64{fanA, fanB}, 0},
		{"other organization owns its profile", otherUser.OrganizationID, []int64{fanA, fanB, otherFan}, 1},
	} {
		t.Run(selection.name, func(t *testing.T) {
			count, err := sites.CountResponseProfilesByInfrastructureDevices(txCtx, selection.org, selection.ids)
			require.NoError(t, err)
			assert.Equal(t, selection.want, count)
		})
	}
	count, err := devices.CountResponseProfilesByInfrastructureDevice(txCtx, user.OrganizationID, fanA)
	require.NoError(t, err)
	assert.Equal(t, int64(1), count, "single-device lookup must use the same transaction and singleton overlap")
	count, err = sites.CountResponseProfilesByInfrastructureDevices(ctx, user.OrganizationID, []int64{fanA, fanB})
	require.NoError(t, err)
	assert.Zero(t, count, "uncommitted profiles must remain invisible outside the caller transaction")
}
