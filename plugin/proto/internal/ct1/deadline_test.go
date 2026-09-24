package ct1

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
	"time"

	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/require"
)

func TestOptionalPlantTimeoutPreservesCoreMetrics(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/container/map":
			_, _ = w.Write([]byte(`{"containerMap":{"tanks":[{"id":1,"modules":[]}]}}`))
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
	client, err := NewClient(sdk.DeviceInfo{Host: u.Hostname(), Port: int32(port), URLScheme: u.Scheme}, sdk.UsernamePassword{})
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 1200*time.Millisecond)
	defer cancel()
	metrics, err := client.Collect(ctx, "ct1")
	require.NoError(t, err)
	require.NoError(t, ctx.Err(), "optional request must finish before the outer deadline")
	require.Equal(t, 100e12, metrics.HashrateHS.Value)
	require.Equal(t, 3000.0, metrics.PowerW.Value)
	require.Len(t, metrics.Container.Tanks, 1)
	require.Nil(t, metrics.Container.Plant)
	cancel()
	_, err = client.Collect(ctx, "ct1")
	require.Error(t, err, "parent cancellation still aborts collection")
}
