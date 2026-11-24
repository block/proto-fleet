package ct1

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestDryCoolerTemperatureIsDedicatedOptionalReading(t *testing.T) {
	now := time.Now().UTC()
	for _, tc := range []struct {
		body string
		want *float64
	}{
		{`{"plant":{"inletTemperatureC":45,"dryCoolerInletTemperatureC":0}}`, new(0.0)},
		{`{"plant":{"inletTemperatureC":45,"dryCoolerInletTemperatureC":48.2}}`, new(48.2)},
		{`{"plant":{"inletTemperatureC":45}}`, nil},
	} {
		var response plantResponse
		require.NoError(t, json.Unmarshal([]byte(tc.body), &response))
		response.Timestamp = now
		require.Equal(t, tc.want, response.observation(now).DryCoolerInletTemperatureC)
		response.Timestamp = now.Add(-3 * time.Minute)
		require.Nil(t, response.observation(now))
	}
}
