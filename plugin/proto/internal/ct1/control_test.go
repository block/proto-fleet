package ct1

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
	"time"

	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/require"
)

func TestEquipmentControlRequiresLiveSupportAndExplicitState(t *testing.T) {
	for _, tc := range []struct {
		name, controls, control string
		enabled                 *bool
		reject                  bool
		wantWrites              int
	}{
		{"enable", `{"supported":true,"controls":[{"name":"dry_cooler","stateKnown":true}]}`, "dry_cooler", new(true), false, 1},
		{"disable", `{"supported":true,"controls":[{"name":"dry_cooler","stateKnown":true,"enabled":true}]}`, "dry_cooler", new(false), false, 1},
		{"unsupported", `{"supported":false}`, "dry_cooler", new(true), true, 0},
		{"unknown state", `{"supported":true,"controls":[{"name":"dry_cooler"}]}`, "dry_cooler", new(true), true, 0},
		{"missing target", `{}`, "dry_cooler", nil, true, 0},
		{"arbitrary name", `{}`, "../../alarm/reset", new(true), true, 0},
		{"breaker missing capability", `{"supported":true,"mainCircuitBreakerClosed":false}`, "main_circuit_breaker", new(true), true, 0},
		{"breaker", `{"mainCircuitBreakerSupported":true,"mainCircuitBreakerClosed":false}`, "main_circuit_breaker", new(true), false, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			writes := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodGet {
					_, _ = w.Write([]byte(tc.controls))
					return
				}
				writes++
				var body map[string]bool
				require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
				key := "enabled"
				if tc.control == "main_circuit_breaker" {
					key = "closed"
					require.Equal(t, "/api/v2/power/container/main-circuit-breaker", r.URL.Path)
				} else {
					require.Equal(t, "/api/v2/power/container/controls/"+tc.control, r.URL.Path)
				}
				require.Equal(t, map[string]bool{key: *tc.enabled}, body)
				if key == "closed" {
					_, _ = w.Write([]byte(`{"closed":true}`))
				} else {
					require.NoError(t, json.NewEncoder(w).Encode(map[string]any{"control": map[string]any{"name": tc.control, "enabled": *tc.enabled}}))
				}
			}))
			defer server.Close()
			u, _ := url.Parse(server.URL)
			port, err := strconv.ParseInt(u.Port(), 10, 32)
			require.NoError(t, err)
			dev, err := NewDevice("ct1", sdk.DeviceInfo{Host: u.Hostname(), Port: int32(port), URLScheme: u.Scheme}, sdk.UsernamePassword{})
			require.NoError(t, err)
			err = dev.SetContainerControl(t.Context(), sdk.ContainerControlRequest{Name: tc.control, Enabled: tc.enabled})
			if tc.reject {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, tc.wantWrites, writes)
		})
	}
}

func TestEquipmentWriteFailureIsNotRetried(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		status     int
		hang       bool
	}{
		{name: "rejected", status: http.StatusConflict},
		{name: "missing off acknowledgment", status: http.StatusOK, body: `{"control":{"name":"dry_cooler"}}`},
		{name: "wrong acknowledgment", status: http.StatusOK, body: `{"control":{"name":"dry_cooler","enabled":true}}`},
		{name: "timeout", hang: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			writes := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodGet {
					_, _ = w.Write([]byte(`{"supported":true,"controls":[{"name":"dry_cooler","stateKnown":true,"enabled":true}]}`))
					return
				}
				writes++
				if tc.hang {
					select {
					case <-r.Context().Done():
					case <-time.After(200 * time.Millisecond):
					}
					return
				}
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			u, _ := url.Parse(server.URL)
			port, err := strconv.ParseInt(u.Port(), 10, 32)
			require.NoError(t, err)
			dev, err := NewDevice("ct1", sdk.DeviceInfo{Host: u.Hostname(), Port: int32(port), URLScheme: u.Scheme}, sdk.UsernamePassword{})
			require.NoError(t, err)
			ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
			defer cancel()
			err = dev.SetContainerControl(ctx, sdk.ContainerControlRequest{Name: "dry_cooler", Enabled: new(false)})
			require.Error(t, err)
			require.Equal(t, 1, writes)
		})
	}
}
