package minerproxy

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/stretchr/testify/require"
)

func TestDirectContainerProxyPreservesViewsAndMenuActions(t *testing.T) {
	database, orgID, _, h, original := proxyFenceFixture(t)
	_, err := sqlc.New(database.DB).UpsertDiscoveredDevice(t.Context(), sqlc.UpsertDiscoveredDeviceParams{
		OrgID: orgID, DeviceIdentifier: original.deviceIdentifier, DriverName: "proto", DeviceClass: 2,
		IpAddress: original.binding.IpAddress, Port: original.binding.Port, UrlScheme: "http", IsActive: true,
		SerialNumber: original.binding.SerialNumber.String, MacAddress: original.binding.MacAddress,
	})
	require.NoError(t, err)
	transport := &countingProxyTransport{}
	h.httpClient = &http.Client{Transport: transport}
	for _, action := range []struct{ method, path string }{
		{http.MethodGet, "system/info"},
		{http.MethodGet, "mining/status"},
		{http.MethodGet, "mining/target-power"},
		{http.MethodPut, "mining/target-power"},
		{http.MethodPost, "mining/start"},
		{http.MethodPost, "mining/stop"},
	} {
		t.Run(action.method+" "+action.path, func(t *testing.T) {
			source := authenticatedProxyRequest(t, database, h, original)
			source.Method = action.method
			source.SetPathValue("rest", action.path)
			source.Header.Set("Origin", "http://fleet.test")
			if action.method != http.MethodGet {
				source.Body = io.NopCloser(strings.NewReader(`{"watts":100000}`))
			}
			before := transport.calls.Load()
			response := &nodeProxyRecorder{newDeadlineProxyRecorder()}
			h.ServeHTTP(response, source)
			require.Equal(t, http.StatusOK, response.Code, response.Body.String())
			require.JSONEq(t, `{}`, response.Body.String())
			require.Equal(t, before+1, transport.calls.Load(), "direct CT1 actions must reach the controller")
		})
	}
}
