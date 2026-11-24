package proto

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	mdk "github.com/block/proto-fleet/sdk/mdk/generated/mdk/v2"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

func TestClearPoolsDurableMDKV2(t *testing.T) {
	for _, scenario := range []string{"last pool", "multiple pools", "already empty", "v1 only", "malformed readback", "duplicate IDs", "ignored delete", "lost reply", "partial delete", "runtime override", "runtime remains", "active pool remains", "readback unavailable"} {
		t.Run(scenario, func(t *testing.T) {
			pools := []*mdk.Pool{{Id: 9, Name: "primary", Url: "stratum+tcp://pool.example:3333", Username: "worker", Enabled: true}}
			if scenario == "multiple pools" || scenario == "partial delete" {
				pools = append(pools, &mdk.Pool{Id: 1234567890123, Name: "backup", Url: "stratum+tcp://backup.example:3333", Priority: 1})
			}
			if scenario == "duplicate IDs" {
				pools = append(pools, proto.CloneOf(pools[0]))
			}
			if scenario == "already empty" {
				pools = nil
			}
			deletes := 0
			send := func(w http.ResponseWriter, value proto.Message) {
				data, err := protojson.Marshal(value)
				require.NoError(t, err)
				_, _ = w.Write(data)
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.True(t, strings.HasPrefix(r.URL.Path, "/api/v2/pools"), "must never fall back to legacy mutation")
				if scenario == "v1 only" {
					http.NotFound(w, r)
					return
				}
				switch {
				case r.Method == http.MethodGet && r.URL.Path == "/api/v2/pools":
					if scenario == "malformed readback" {
						_, _ = w.Write([]byte(`{"pools":[{"id":"private-secret"}]}`))
						return
					}
					if scenario == "readback unavailable" && deletes > 0 {
						http.Error(w, "private-secret", http.StatusInternalServerError)
						return
					}
					send(w, &mdk.ListPoolsResponse{Pools: pools})
				case r.Method == http.MethodGet && r.URL.Path == "/api/v2/pools/diagnostics":
					response := &mdk.GetPoolDiagnosticsResponse{}
					for _, pool := range pools {
						if pool.Enabled {
							response.Pools = append(response.Pools, &mdk.PoolConnectionDiagnostics{PoolId: pool.Id, PoolName: pool.Name})
						}
					}
					if scenario == "runtime override" || scenario == "runtime remains" && deletes > 0 {
						response.Pools = append(response.Pools, &mdk.PoolConnectionDiagnostics{PoolId: 88, PoolName: "override"})
					}
					if scenario == "active pool remains" && deletes > 0 {
						response.ActivePoolId = 88
					}
					send(w, response)
				case r.Method == http.MethodDelete:
					deletes++
					id, err := strconv.ParseInt(strings.TrimPrefix(r.URL.Path, "/api/v2/pools/"), 10, 64)
					require.NoError(t, err)
					if scenario == "partial delete" && deletes == 2 {
						http.Error(w, "private-secret", http.StatusInternalServerError)
						return
					}
					if scenario != "ignored delete" {
						for i, pool := range pools {
							if pool.Id == id {
								pools = append(pools[:i], pools[i+1:]...)
								break
							}
						}
					}
					if scenario == "lost reply" {
						hijacker, ok := w.(http.Hijacker)
						require.True(t, ok)
						conn, _, err := hijacker.Hijack()
						require.NoError(t, err)
						_ = conn.Close()
						return
					}
					_, _ = w.Write([]byte(`{}`))
				default:
					t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			client := newTestClient(t, server)
			err := client.UpdatePools(t.Context(), nil)
			switch scenario {
			case "last pool", "multiple pools", "already empty":
				require.NoError(t, err)
				stored, err := client.GetStoredMiningPools(t.Context())
				require.NoError(t, err)
				require.Empty(t, stored)
				require.Equal(t, map[string]int{"last pool": 1, "multiple pools": 2, "already empty": 0}[scenario], deletes)
			default:
				require.Error(t, err)
				require.NotContains(t, fmt.Sprintf("%+v", err), "private-secret")
				if scenario == "v1 only" || scenario == "malformed readback" || scenario == "duplicate IDs" || scenario == "runtime override" {
					require.Zero(t, deletes)
					require.Equal(t, sdk.WriteNotDispatched, sdk.WriteEvidence(err).Reason)
				} else {
					require.Nil(t, sdk.WriteEvidence(err), "a dispatched clear must not be classified as safe to retry")
				}
			}
		})
	}
}
