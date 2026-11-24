package proto

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestGetSystemInfoUptime(t *testing.T) {
	for _, tt := range []struct {
		name        string
		uptimeField string
		present     bool
		wantUptime  uint64
		wantErr     bool
	}{
		{name: "absent"},
		{name: "null", uptimeField: `,"uptime_seconds":null`},
		{name: "zero", uptimeField: `,"uptime_seconds":0`, present: true},
		{name: "positive", uptimeField: `,"uptime_seconds":12345`, present: true, wantUptime: 12345},
		{name: "negative", uptimeField: `,"uptime_seconds":-1`, wantErr: true},
		{name: "fractional", uptimeField: `,"uptime_seconds":1.5`, wantErr: true},
		{name: "string", uptimeField: `,"uptime_seconds":"123"`, wantErr: true},
		{name: "overflow", uptimeField: `,"uptime_seconds":18446744073709551616`, wantErr: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			requestReceivedAt := make(chan time.Time, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requestReceivedAt <- time.Now()
				require.Equal(t, "/api/v1/system", r.URL.Path)
				w.Header().Set("Content-Type", "application/json")
				_, _ = fmt.Fprintf(w, `{"system-info":{"os":{"version":"2.0.0"}%s}}`, tt.uptimeField)
			}))
			t.Cleanup(server.Close)
			client := newTestClient(t, server)
			t.Cleanup(func() { _ = client.Close() })

			beforeRequest := time.Now()
			info, err := client.GetSystemInfo(t.Context())
			if tt.wantErr {
				require.Error(t, err)
				require.Nil(t, info.UptimeSeconds)
				require.Zero(t, info.UptimeObservedAt)
				return
			}
			require.NoError(t, err)
			require.Equal(t, "2.0.0", info.FirmwareVersion)
			if !tt.present {
				require.Nil(t, info.UptimeSeconds)
				require.Zero(t, info.UptimeObservedAt)
				return
			}
			require.NotNil(t, info.UptimeSeconds)
			require.Equal(t, tt.wantUptime, *info.UptimeSeconds)
			require.False(t, info.UptimeObservedAt.Before(beforeRequest))
			require.True(t, info.UptimeObservedAt.Before(<-requestReceivedAt), "uptime must retain the request-start timestamp")
		})
	}
}
