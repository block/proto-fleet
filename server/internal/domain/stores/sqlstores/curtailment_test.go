package sqlstores

import (
	"database/sql"
	"encoding/json"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/internal/domain/curtailment/models"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
)

// InsertEventWithTargets still rejects an empty target slice for a non-terminal
// event after the guard was relaxed to permit terminal (vacuously-COMPLETED
// FULL_FLEET) events. The guard returns before any DB access, so a zero-value
// store suffices.
func TestInsertEventWithTargets_RejectsEmptyTargetsForNonTerminalEvent(t *testing.T) {
	t.Parallel()

	store := &SQLCurtailmentStore{}
	_, err := store.InsertEventWithTargets(
		t.Context(),
		models.InsertEventParams{State: models.EventStatePending},
		nil,
	)
	require.Error(t, err)
	assert.True(t, fleeterror.IsInvalidArgumentError(err))
}

func TestHierarchicalScopeSiteIDs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name              string
		event             models.InsertEventParams
		wantSiteIDs       []int64
		wantUsesScopeLock bool
		wantErr           bool
	}{
		{
			name:              "whole org",
			event:             models.InsertEventParams{State: models.EventStateActive, ScopeType: models.ScopeTypeWholeOrg, ScopeJSON: []byte(`{}`)},
			wantUsesScopeLock: true,
		},
		{
			name:              "single site",
			event:             models.InsertEventParams{State: models.EventStateActive, ScopeType: models.ScopeTypeSite, ScopeJSON: []byte(`{"site_id":7}`)},
			wantSiteIDs:       []int64{7},
			wantUsesScopeLock: true,
		},
		{
			name: "site-only mixed",
			event: models.InsertEventParams{
				State:     models.EventStateActive,
				ScopeType: models.ScopeTypeMixed,
				ScopeJSON: []byte(`{"site_ids":[8,7,7],"device_identifiers":null}`),
			},
			wantSiteIDs:       []int64{7, 8},
			wantUsesScopeLock: true,
		},
		{
			name: "mixed with explicit devices",
			event: models.InsertEventParams{
				State:     models.EventStateActive,
				ScopeType: models.ScopeTypeMixed,
				ScopeJSON: []byte(`{"site_ids":[7],"device_identifiers":["miner-a"]}`),
			},
		},
		{
			name:  "terminal site event",
			event: models.InsertEventParams{State: models.EventStateCompleted, ScopeType: models.ScopeTypeSite, ScopeJSON: []byte(`{"site_id":7}`)},
		},
		{
			name: "invalid mixed site id",
			event: models.InsertEventParams{
				State:     models.EventStateActive,
				ScopeType: models.ScopeTypeMixed,
				ScopeJSON: []byte(`{"site_ids":[0],"device_identifiers":null}`),
			},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			gotSiteIDs, gotUsesScopeLock, err := hierarchicalScopeSiteIDs(tc.event)

			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.wantSiteIDs, gotSiteIDs)
			assert.Equal(t, tc.wantUsesScopeLock, gotUsesScopeLock)
		})
	}
}

func TestMapOrgConfigError(t *testing.T) {
	t.Parallel()

	const orgID = int64(42)
	fkErr := &pgconn.PgError{Code: pgErrCodeForeignKeyViolation, ConstraintName: "fk_curtailment_org_config_org"}
	uniqueErr := &pgconn.PgError{Code: "23505", ConstraintName: "curtailment_org_config_pkey"}
	plainErr := errors.New("connection reset by peer")

	t.Run("nil error returns nil", func(t *testing.T) {
		t.Parallel()
		assert.NoError(t, mapOrgConfigError(nil, orgID))
	})

	t.Run("ErrNoRows surfaces as NotFound", func(t *testing.T) {
		t.Parallel()
		// EnsureCurtailmentOrgConfig gates both branches on
		// organization.deleted_at IS NULL, so soft-deleted (and
		// unknown) orgs come through as ErrNoRows rather than an FK
		// violation. Pin the mapping so deleted tenants surface as
		// NotFound, not Internal.
		got := mapOrgConfigError(sql.ErrNoRows, orgID)
		require.Error(t, got)
		assert.True(t, fleeterror.IsNotFoundError(got),
			"ErrNoRows must surface as NotFound; got %v", got)
		assert.Contains(t, got.Error(), "42", "error must echo the orgID")
	})

	t.Run("FK violation surfaces as NotFound", func(t *testing.T) {
		t.Parallel()
		got := mapOrgConfigError(fkErr, orgID)
		require.Error(t, got)
		assert.True(t, fleeterror.IsNotFoundError(got),
			"FK violation must surface as NotFound; got %v", got)
		assert.Contains(t, got.Error(), "42", "error must echo the orgID")
	})

	t.Run("non-FK pg error wraps as Internal", func(t *testing.T) {
		t.Parallel()
		got := mapOrgConfigError(uniqueErr, orgID)
		require.Error(t, got)
		assert.False(t, fleeterror.IsNotFoundError(got),
			"non-FK pg error must not surface as NotFound; got %v", got)
		assert.Contains(t, got.Error(), "failed to get curtailment org config")
	})

	t.Run("plain non-pg error wraps as Internal", func(t *testing.T) {
		t.Parallel()
		got := mapOrgConfigError(plainErr, orgID)
		require.Error(t, got)
		assert.False(t, fleeterror.IsNotFoundError(got),
			"plain error must not surface as NotFound; got %v", got)
		assert.Contains(t, got.Error(), "failed to get curtailment org config")
	})
}

func TestNormalizeListCandidatesParams(t *testing.T) {
	t.Parallel()

	empty := normalizeListCandidatesParams(interfaces.ListCandidatesParams{
		OrgID:             7,
		DeviceIdentifiers: []string{},
		SiteIDs:           []int64{},
	})
	assert.Nil(t, empty.DeviceIdentifiers, "empty slices must bind as SQL NULL so they match whole-org")
	assert.Nil(t, empty.SiteIDs, "empty site slices must bind as SQL NULL so they match whole-org")

	nonEmpty := normalizeListCandidatesParams(interfaces.ListCandidatesParams{
		OrgID:             7,
		DeviceIdentifiers: []string{"miner-1"},
		SiteIDs:           []int64{3},
	})
	assert.Equal(t, []string{"miner-1"}, nonEmpty.DeviceIdentifiers)
	assert.Equal(t, []int64{3}, nonEmpty.SiteIDs)
}

func TestResponseProfileSiteIDsForLock(t *testing.T) {
	t.Parallel()

	siteA := int64(7)
	siteB := int64(3)

	assert.Equal(t, []int64{3, 7}, responseProfileSiteIDsForLock(&siteA, nil, &siteB, &siteA))
	assert.Empty(t, responseProfileSiteIDsForLock(nil))
}

// TestBuildBulkTargetPayload pins the JSON contract consumed by
// BulkInsertCurtailmentTargets via jsonb_to_recordset:
//   - nil baseline_power_w marshals to JSON null so the NUMERIC column
//     receives SQL NULL,
//   - empty selector_rationale_jsonb is omitted entirely (omitempty)
//     because json.RawMessage's MarshalJSON would otherwise reject an
//     empty []byte and the recordset treats a missing key as NULL,
//   - populated rationale rides through verbatim as a JSON object.
//
// The recordset cast itself runs in Postgres, but we'd rather catch a
// shape regression here than via an integration-test failure that needs
// docker-compose to reproduce.
func TestBuildBulkTargetPayload(t *testing.T) {
	t.Parallel()

	baseline := 1234.567
	rationale := []byte(`{"reason":"least_efficient","rank":3}`)
	targets := []models.InsertTargetParams{
		{
			DeviceIdentifier:      "miner-1",
			TargetType:            "miner",
			State:                 models.TargetStatePending,
			DesiredState:          "curtailed",
			BaselinePowerW:        &baseline,
			SelectorRationaleJSON: rationale,
		},
		{
			DeviceIdentifier:      "miner-2",
			TargetType:            "miner",
			State:                 models.TargetStatePending,
			DesiredState:          "curtailed",
			BaselinePowerW:        nil,
			SelectorRationaleJSON: nil,
		},
	}

	raw, err := buildBulkTargetPayload(targets)
	require.NoError(t, err)

	var decoded []map[string]any
	require.NoError(t, json.Unmarshal(raw, &decoded))
	require.Len(t, decoded, 2)

	first := decoded[0]
	assert.Equal(t, "miner-1", first["device_identifier"])
	assert.Equal(t, "miner", first["target_type"])
	assert.Equal(t, "pending", first["state"])
	assert.Equal(t, "curtailed", first["desired_state"])
	baselineField, ok := first["baseline_power_w"].(float64)
	require.True(t, ok, "baseline_power_w must marshal as a JSON number")
	assert.InDelta(t, baseline, baselineField, 1e-9)
	rationaleField, ok := first["selector_rationale_jsonb"].(map[string]any)
	require.True(t, ok, "populated rationale must marshal as a nested JSON object, got %T", first["selector_rationale_jsonb"])
	assert.Equal(t, "least_efficient", rationaleField["reason"])

	second := decoded[1]
	assert.Equal(t, "miner-2", second["device_identifier"])
	require.Contains(t, second, "baseline_power_w", "nil pointer must serialize as JSON null, not be omitted, so jsonb_to_recordset returns NULL rather than skipping the column")
	assert.Nil(t, second["baseline_power_w"], "nil *float64 must marshal to JSON null")
	assert.NotContains(t, second, "selector_rationale_jsonb", "empty rationale must be omitted so jsonb_to_recordset sees no key and falls back to NULL")
}
