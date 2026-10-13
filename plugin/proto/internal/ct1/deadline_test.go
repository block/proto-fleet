package ct1

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"testing"
	"time"

	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/require"
)

func TestOptionalPlantTimeoutPreservesCoreMetrics(t *testing.T) {
	now := time.Now().UTC()
	server := newGRPCFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/container/map":
			require.NoError(t, json.NewEncoder(w).Encode(pollingCorrectionsMap(now)))
		case "/api/v2/mining/status":
			_, _ = w.Write([]byte(`{"hashrate":100,"power":3000,"mining":true}`))
		default:
			<-r.Context().Done()
		}
	}))
	defer server.Close()
	u, err := url.Parse(server.URL)
	require.NoError(t, err)
	port, err := strconv.ParseInt(u.Port(), 10, 32)
	require.NoError(t, err)
	client, err := NewClient(sdk.DeviceInfo{Host: u.Hostname(), Port: 8080, RPCPort: int32(port), URLScheme: u.Scheme}, sdk.UsernamePassword{})
	require.NoError(t, err)
	defer client.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 1200*time.Millisecond)
	defer cancel()
	metrics, err := client.Collect(ctx, "ct1")
	require.NoError(t, err)
	require.NoError(t, ctx.Err(), "optional request must finish before the outer deadline")
	require.Equal(t, 100e12, metrics.HashrateHS.Value)
	require.Nil(t, metrics.PowerW, "failed facility read cannot fall back to mining power")
	require.Nil(t, metrics.EfficiencyJH)
	require.Len(t, metrics.Container.Tanks, 2)
	require.Nil(t, metrics.Container.Plant)
	cancel()
	_, err = client.Collect(ctx, "ct1")
	require.Error(t, err, "parent cancellation still aborts collection")
}

func TestMissingContainerTotalsPreservesModuleObservation(t *testing.T) {
	server := newGRPCFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v2/container/map" {
			_, _ = w.Write([]byte(`{"containerMap":{"tanks":[{"id":1,"modules":[{"id":1,"slots":[]}]}]}}`))
			return
		}
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()
	u, err := url.Parse(server.URL)
	require.NoError(t, err)
	port, err := strconv.ParseInt(u.Port(), 10, 32)
	require.NoError(t, err)
	client, err := NewClient(sdk.DeviceInfo{Host: u.Hostname(), Port: 8080, RPCPort: int32(port), URLScheme: u.Scheme}, sdk.UsernamePassword{})
	require.NoError(t, err)
	defer client.Close()
	m, err := client.Collect(t.Context(), "ct1")
	require.NoError(t, err)
	require.False(t, m.Container.TopologyComplete, "a partial map does not prove absent equipment")
	require.Len(t, m.Container.Tanks[0].Modules, 1)
	require.Nil(t, m.HashrateHS)
	require.Nil(t, m.PowerW)
}
