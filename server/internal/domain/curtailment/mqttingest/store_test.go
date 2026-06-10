package mqttingest

import (
	"database/sql"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	sqlc "github.com/block/proto-fleet/server/generated/sqlc"
)

// A source row that leaves broker_port / staleness NULL must resolve to the
// in-code defaults (those defaults live in code, not as DB column defaults).
func TestSourceConfigFromRow_NullColumnsUseCodeDefaults(t *testing.T) {
	t.Parallel()

	cfg := sourceConfigFromRow(sqlc.CurtailmentMqttSourceConfig{
		ID:             1,
		OrganizationID: 7,
		// BrokerPort / StalenessThresholdSec left NULL.
	})

	assert.Equal(t, defaultBrokerPort, cfg.BrokerPort)
	assert.Equal(t, brokerTransportTCP, cfg.BrokerTransport)
	assert.Equal(t, time.Duration(defaultStalenessThresholdSec)*time.Second, cfg.StalenessThreshold)
}

// Explicit column values override the in-code defaults.
func TestSourceConfigFromRow_SetColumnsOverrideDefaults(t *testing.T) {
	t.Parallel()

	cfg := sourceConfigFromRow(sqlc.CurtailmentMqttSourceConfig{
		BrokerPort:            sql.NullInt32{Int32: 8883, Valid: true},
		BrokerTransport:       brokerTransportTLS,
		StalenessThresholdSec: sql.NullInt32{Int32: 120, Valid: true},
	})

	assert.Equal(t, int32(8883), cfg.BrokerPort)
	assert.Equal(t, brokerTransportTLS, cfg.BrokerTransport)
	assert.Equal(t, 120*time.Second, cfg.StalenessThreshold)
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
