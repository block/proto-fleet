package mqttingest

import (
	"database/sql"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	sqlc "github.com/block/proto-fleet/server/generated/sqlc"
)

// A source row that leaves broker_port / staleness / min-duration NULL must
// resolve to the in-code defaults (those defaults live in code, not as DB
// column defaults).
func TestSourceConfigFromRow_NullColumnsUseCodeDefaults(t *testing.T) {
	t.Parallel()

	cfg := sourceConfigFromRow(sqlc.CurtailmentMqttSourceConfig{
		ID:                      1,
		OrganizationID:          7,
		ContractedCurtailmentKw: sql.NullInt32{Int32: 12500, Valid: true},
		// BrokerPort / StalenessThresholdSec / MinCurtailedDurationSec left NULL.
	})

	assert.Equal(t, defaultBrokerPort, cfg.BrokerPort)
	assert.Equal(t, brokerTransportTCP, cfg.BrokerTransport)
	assert.Equal(t, time.Duration(defaultStalenessThresholdSec)*time.Second, cfg.StalenessThreshold)
	assert.Equal(t, time.Duration(defaultMinCurtailedDurationSec)*time.Second, cfg.MinCurtailedDuration)
}

// Explicit column values override the in-code defaults.
func TestSourceConfigFromRow_SetColumnsOverrideDefaults(t *testing.T) {
	t.Parallel()

	cfg := sourceConfigFromRow(sqlc.CurtailmentMqttSourceConfig{
		BrokerPort:              sql.NullInt32{Int32: 8883, Valid: true},
		BrokerTransport:         brokerTransportTLS,
		StalenessThresholdSec:   sql.NullInt32{Int32: 120, Valid: true},
		MinCurtailedDurationSec: sql.NullInt32{Int32: 300, Valid: true},
	})

	assert.Equal(t, int32(8883), cfg.BrokerPort)
	assert.Equal(t, brokerTransportTLS, cfg.BrokerTransport)
	assert.Equal(t, 120*time.Second, cfg.StalenessThreshold)
	assert.Equal(t, 300*time.Second, cfg.MinCurtailedDuration)
}

// scope_type + scope_device_identifiers round-trip through the loader so the
// driver can build the curtailment Scope.
func TestSourceConfigFromRow_CarriesScope(t *testing.T) {
	t.Parallel()

	cfg := sourceConfigFromRow(sqlc.CurtailmentMqttSourceConfig{
		ScopeType:              "device_list",
		ScopeDeviceIdentifiers: []string{"miner-1", "miner-2"},
	})

	assert.Equal(t, "device_list", cfg.ScopeType)
	assert.Equal(t, []string{"miner-1", "miner-2"}, cfg.ScopeDeviceIdentifiers)
}

func TestSourceConfigFromRow_CarriesSiteScope(t *testing.T) {
	t.Parallel()

	siteID := int64(42)
	cfg := sourceConfigFromRow(sqlc.CurtailmentMqttSourceConfig{
		ScopeType:   "site",
		ScopeSiteID: sql.NullInt64{Int64: siteID, Valid: true},
	})

	assert.Equal(t, "site", cfg.ScopeType)
	assert.Equal(t, &siteID, cfg.ScopeSiteID)
}

// curtail_mode round-trips, and a NULL contracted_curtailment_kw (a full_fleet
// source) surfaces as 0 in the domain shape.
func TestSourceConfigFromRow_CarriesCurtailMode(t *testing.T) {
	t.Parallel()

	fixed := sourceConfigFromRow(sqlc.CurtailmentMqttSourceConfig{
		CurtailMode:             "FIXED_KW",
		ContractedCurtailmentKw: sql.NullInt32{Int32: 9000, Valid: true},
	})
	assert.Equal(t, "FIXED_KW", fixed.CurtailMode)
	assert.Equal(t, int32(9000), fixed.ContractedCurtailmentKw)

	full := sourceConfigFromRow(sqlc.CurtailmentMqttSourceConfig{
		CurtailMode: "FULL_FLEET", // contracted_curtailment_kw left NULL
	})
	assert.Equal(t, "FULL_FLEET", full.CurtailMode)
	assert.Zero(t, full.ContractedCurtailmentKw, "NULL contracted kW surfaces as 0 for full_fleet")
}

// last_processed_target rehydrates independently of last_target so the dedup
// guard survives a restart after a debounced flip (last_target=OFF while the
// debounced ON advanced last_processed_target to ON).
func TestSourceStateFromRow_RehydratesProcessedTarget(t *testing.T) {
	t.Parallel()

	st := sourceStateFromRow(sqlc.CurtailmentMqttSourceState{
		LastTarget:          sql.NullString{String: "OFF", Valid: true}, // settled OFF
		LastProcessedTarget: sql.NullString{String: "ON", Valid: true},  // debounced ON
	})

	assert.Equal(t, TargetOff, st.LastTarget)
	assert.Equal(t, TargetOn, st.LastProcessedTarget,
		"processed target survives restart, distinct from the settled target")
}
