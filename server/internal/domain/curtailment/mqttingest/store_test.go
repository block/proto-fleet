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
		ContractedCurtailmentKw: 12500,
		// BrokerPort / StalenessThresholdSec / MinCurtailedDurationSec left NULL.
	})

	assert.Equal(t, defaultBrokerPort, cfg.BrokerPort)
	assert.Equal(t, time.Duration(defaultStalenessThresholdSec)*time.Second, cfg.StalenessThreshold)
	assert.Equal(t, time.Duration(defaultMinCurtailedDurationSec)*time.Second, cfg.MinCurtailedDuration)
}

// Explicit column values override the in-code defaults.
func TestSourceConfigFromRow_SetColumnsOverrideDefaults(t *testing.T) {
	t.Parallel()

	cfg := sourceConfigFromRow(sqlc.CurtailmentMqttSourceConfig{
		BrokerPort:              sql.NullInt32{Int32: 8883, Valid: true},
		StalenessThresholdSec:   sql.NullInt32{Int32: 120, Valid: true},
		MinCurtailedDurationSec: sql.NullInt32{Int32: 300, Valid: true},
	})

	assert.Equal(t, int32(8883), cfg.BrokerPort)
	assert.Equal(t, 120*time.Second, cfg.StalenessThreshold)
	assert.Equal(t, 300*time.Second, cfg.MinCurtailedDuration)
}
