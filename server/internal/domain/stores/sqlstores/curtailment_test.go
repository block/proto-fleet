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
)

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
