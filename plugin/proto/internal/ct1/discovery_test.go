package ct1

import (
	"context"
	"testing"

	mdk "github.com/block/proto-fleet/sdk/mdk/generated/mdk/v2"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestDiscoveryOptionalHAErrors(t *testing.T) {
	for _, code := range []codes.Code{
		codes.Unimplemented,
		codes.PermissionDenied,
		codes.Unavailable,
	} {
		t.Run(code.String(), func(t *testing.T) {
			s := startHAServer(t, "a", "b")
			s.haError = status.Error(code, "HA diagnostics unavailable")
			info := sdk.DeviceInfo{Host: s.endpoint.Host, Port: s.endpoint.Port, URLScheme: s.endpoint.URLScheme, RPCPort: s.endpoint.RPCPort}
			c, err := NewClient(info, sdk.UsernamePassword{})
			require.NoError(t, err)
			defer c.Close()
			got, err := c.Discover(t.Context(), info)
			require.NoError(t, err)
			require.Equal(t, sdk.DeviceClassContainer, got.DeviceClass)
			require.Equal(t, "a", got.SerialNumber)
			require.Empty(t, got.Endpoints, "missing HA diagnostics cannot establish a pair")
			info.SerialNumber = "replacement"
			_, err = c.Discover(t.Context(), info)
			require.ErrorContains(t, err, "controller identity mismatch")
		})
	}
}

func TestDiscoveryHonorsCancellationDuringHARead(t *testing.T) {
	s := startHAServer(t, "a", "b")
	s.haEntered = make(chan struct{})
	info := sdk.DeviceInfo{Host: s.endpoint.Host, Port: s.endpoint.Port, URLScheme: s.endpoint.URLScheme, RPCPort: s.endpoint.RPCPort}
	c, err := NewClient(info, sdk.UsernamePassword{})
	require.NoError(t, err)
	defer c.Close()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	completed := make(chan error, 1)
	go func() {
		_, err := c.Discover(ctx, info)
		completed <- err
	}()
	select {
	case <-s.haEntered:
	case err := <-completed:
		t.Fatalf("discovery returned before the optional RPC started: %v", err)
	}
	cancel()
	err = <-completed
	require.Equal(t, codes.Canceled, status.Code(err))
}

func TestDiscoveryUsesConfirmedClusterIdentity(t *testing.T) {
	for _, tc := range []struct {
		name    string
		change  func(*mdk.HAStatus)
		grouped bool
		invalid bool
	}{
		{"confirmed", func(*mdk.HAStatus) {}, true, false},
		{"initializing", func(h *mdk.HAStatus) {
			h.ClusterId = ""
			h.IdentityState = mdk.HAIdentityState_HA_IDENTITY_STATE_UNAVAILABLE
		}, false, false},
		{"retained but unconfirmed", func(h *mdk.HAStatus) { h.IdentityState = mdk.HAIdentityState_HA_IDENTITY_STATE_UNAVAILABLE }, false, false},
		{"inventory not loaded", func(h *mdk.HAStatus) { h.InventoryReady = false }, false, false},
		{"standalone", func(h *mdk.HAStatus) { h.Enabled = false }, false, false},
		{"conflicted", func(h *mdk.HAStatus) { h.IdentityState = mdk.HAIdentityState_HA_IDENTITY_STATE_CONFLICTED }, false, true},
		{"conflict details", func(h *mdk.HAStatus) { h.Conflicts = []string{"peer disagrees"} }, false, true},
		{"wrong reporter", func(h *mdk.HAStatus) { h.ReportingServerId = "other" }, false, true},
		{"empty peer", func(h *mdk.HAStatus) { h.PeerServerId = "" }, false, true},
		{"self peer", func(h *mdk.HAStatus) { h.PeerServerId = h.ReportingServerId }, false, true},
		{"missing peer", func(h *mdk.HAStatus) { h.Members = h.Members[:1] }, true, false},
		{"duplicate member", func(h *mdk.HAStatus) { h.Members = append(h.Members, h.Members[0]) }, true, false},
		{"invalid cluster", func(h *mdk.HAStatus) { h.ClusterId = "not-a-uuid" }, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := startHAServer(t, "a", "b")
			s.haOverride = tc.change
			info := sdk.DeviceInfo{Host: s.endpoint.Host, Port: s.endpoint.Port, URLScheme: s.endpoint.URLScheme, RPCPort: s.endpoint.RPCPort}
			c, err := NewClient(info, sdk.UsernamePassword{})
			require.NoError(t, err)
			defer c.Close()
			got, err := c.Discover(t.Context(), info)
			if tc.invalid {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			if tc.grouped {
				require.Len(t, got.Endpoints, 1)
				require.Equal(t, "ct1:"+testClusterID, got.Endpoints[0].DeviceKey)
				require.ElementsMatch(t, []string{"a", "b"}, got.Endpoints[0].MemberIDs)
			} else {
				require.Empty(t, got.Endpoints)
			}
		})
	}
}
