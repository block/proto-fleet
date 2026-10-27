package ct1

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/metadata"
)

// Opt-in, read-only probe of the full Frigate scripts/ha-faults lab.
// The test supplies the lab credential through its context; production
// discovery still does not read or send a shared token to scanned endpoints.
func TestFrigateHALab(t *testing.T) {
	root := os.Getenv("CT1_HA_LAB_STATE")
	if root == "" {
		t.Skip("set CT1_HA_LAB_STATE to a running ha-faults lab with identity-probe/summary.json")
	}
	data, err := os.ReadFile(filepath.Join(root, "identity-probe", "summary.json"))
	require.NoError(t, err)
	var summary struct {
		Controllers []struct {
			HTTP string `json:"http"`
			GRPC string `json:"grpc"`
			Info struct {
				ID string `json:"id"`
			} `json:"info"`
		} `json:"controllers"`
	}
	require.NoError(t, json.Unmarshal(data, &summary))
	require.Len(t, summary.Controllers, 2)
	token, err := os.ReadFile(filepath.Join(root, "mdk.token"))
	require.NoError(t, err)
	ctx := metadata.AppendToOutgoingContext(t.Context(), "authorization", "Bearer "+strings.TrimSpace(string(token)))
	var endpoints []sdk.DeviceEndpoint
	for _, c := range summary.Controllers {
		host, rpcText, err := net.SplitHostPort(c.GRPC)
		require.NoError(t, err)
		require.Equal(t, "127.0.0.1", host, "this test sends the lab token only to loopback")
		_, webText, err := net.SplitHostPort(c.HTTP)
		require.NoError(t, err)
		rpc, err := sdk.ParsePort(rpcText)
		require.NoError(t, err)
		web, err := sdk.ParsePort(webText)
		require.NoError(t, err)
		endpoints = append(endpoints, sdk.DeviceEndpoint{ID: c.Info.ID, Host: host, RPCPort: rpc, Port: web, URLScheme: "http"})
	}
	// A degraded lab may have only one reachable controller. Use its confirmed
	// inventory to verify the fixed addresses from the local lab manifest.
	var identity sdk.DeviceEndpoint
	for _, endpoint := range endpoints {
		info := sdk.DeviceInfo{Host: endpoint.Host, RPCPort: endpoint.RPCPort, Port: endpoint.Port, URLScheme: endpoint.URLScheme}
		client, err := NewClient(info, sdk.UsernamePassword{})
		require.NoError(t, err)
		verified, err := client.Discover(ctx, info)
		require.NoError(t, client.Close())
		if err == nil && len(verified.Endpoints) == 1 {
			identity = verified.Endpoints[0]
			break
		}
	}
	require.NotEmpty(t, identity.DeviceKey, "the lab must expose a confirmed HA cluster ID")
	for i := range endpoints {
		endpoints[i].DeviceKey = identity.DeviceKey
	}
	info := sdk.DeviceInfo{Host: endpoints[0].Host, Port: endpoints[0].Port, URLScheme: "http", RPCPort: endpoints[0].RPCPort, SerialNumber: endpoints[0].ID, Endpoints: endpoints}
	d, err := NewDevice(ctx, "ct1-ha-lab", info, sdk.UsernamePassword{})
	require.NoError(t, err)
	defer d.Close(ctx)
	expected := os.Getenv("CT1_HA_EXPECT_DEGRADED") == "true"
	var selected *Client
	require.Eventually(t, func() bool {
		metrics, err := d.Status(ctx)
		if err != nil || metrics.Container == nil || metrics.Container.HA == nil {
			return false
		}
		selected, err = d.selectClient(ctx, true)
		return err == nil && metrics.Container.HA.Degraded == expected
	}, time.Minute, time.Second)
	if want := os.Getenv("CT1_HA_EXPECT_CONTROLLER"); want != "" {
		require.Equal(t, want, selected.endpointID(endpoints))
	}
	t.Logf("read succeeded; write-ready controller=%s; HA degraded=%t", selected.endpointID(endpoints), expected)
}
