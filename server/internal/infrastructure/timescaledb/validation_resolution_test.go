package timescaledb

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/block/proto-fleet/server/internal/domain/telemetry/models"
)

func TestSelectDataSourceForQuery_RespectsExplicitValidationResolution(t *testing.T) {
	start := time.Date(2026, time.July, 15, 12, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)

	assert.Equal(t, dataSourceRaw, selectDataSourceForQuery(models.CombinedMetricsQuery{
		Resolution: models.CombinedMetricsResolutionRaw,
	}, start, end))
	assert.Equal(t, dataSourceHourly, selectDataSourceForQuery(models.CombinedMetricsQuery{
		Resolution: models.CombinedMetricsResolutionHourly,
	}, start, end))
	assert.Equal(t, dataSourceDaily, selectDataSourceForQuery(models.CombinedMetricsQuery{
		Resolution: models.CombinedMetricsResolutionDaily,
	}, start, end))
	assert.Equal(t, dataSourceRaw, selectDataSourceForQuery(models.CombinedMetricsQuery{
		DeviceIDs:  []models.DeviceIdentifier{"miner-1"},
		Resolution: models.CombinedMetricsResolutionAuto,
	}, start, end))
}
