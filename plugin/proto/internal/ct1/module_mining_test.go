package ct1

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"testing"

	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/require"
)

func TestModuleMiningExactScopeAndIdentity(t *testing.T) {
	for _, tc := range []struct {
		name, identity, response string
		status, wantWrites       int
		wantError                string
	}{
		{"accepted", "module-a", `{"results":[{"macAddress":"a","applied":true}]}`, http.StatusOK, 1, ""},
		{"replacement", "module-new", `{}`, http.StatusOK, 0, "selected module identity unavailable or ambiguous"},
		{"identity unavailable", "", `{}`, http.StatusOK, 0, "selected module identity unavailable or ambiguous"},
		{"partial failure", "module-a", `{"results":[{"macAddress":"a","applied":false,"message":"disconnected"}]}`, http.StatusOK, 1, "module mining partially applied (1/1 boards failed): a: disconnected"},
		{"RPC failure", "module-a", `{}`, http.StatusServiceUnavailable, 1, "module mining outcome unconfirmed:"},
		{"missing acknowledgement", "module-a", `{"results":[]}`, http.StatusOK, 1, "controller returned incomplete board results; outcome unconfirmed"},
		{"unexpected neighbor", "module-a", `{"results":[{"macAddress":"b","applied":true}]}`, http.StatusOK, 1, "controller returned unexpected board results; outcome unconfirmed"},
	} {
		for _, mining := range []bool{false, true} {
			t.Run(tc.name+strconv.FormatBool(mining), func(t *testing.T) {
				writes := 0
				srv := newTankGRPCFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					switch r.URL.Path {
					case "/api/v2/system/info":
						_, _ = w.Write([]byte(`{"info":{"id":"ct1","model":"CT1"}}`))
					case "/api/v2/container/map":
						require.NoError(t, json.NewEncoder(w).Encode(MapResponse{ContainerMap: &ContainerMap{Tanks: []Tank{{ID: 1, Modules: []Module{
							{ID: 1, Slots: []Slot{{Occupied: true, Hashboard: &Board{MACAddress: "A", ModuleID: tc.identity, MiningState: miningStateIdle}}, {}, {}}},
							{ID: 2, Slots: []Slot{{Occupied: true, Hashboard: &Board{MACAddress: "b", ModuleID: "module-b", MiningState: miningStateIdle}}, {}, {}}},
						}}}}}))
					default:
						writes++
						action := "stop"
						if mining {
							action = "start"
						}
						require.Equal(t, "/api/v2/mining/scoped/"+action, r.URL.Path)
						var body struct {
							Scope struct {
								Identifiers []string
								Modules     []struct{ Tank, Module int32 }
							}
						}
						require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
						require.Equal(t, []string{"A"}, body.Scope.Identifiers)
						require.Empty(t, body.Scope.Modules)
						w.WriteHeader(tc.status)
						_, _ = w.Write([]byte(tc.response))
					}
				}))
				defer srv.Close()
				u, err := url.Parse(srv.URL)
				require.NoError(t, err)
				port, err := strconv.ParseInt(u.Port(), 10, 32)
				require.NoError(t, err)
				dev, err := newTankTestDevice(t, "ct1", sdk.DeviceInfo{SerialNumber: "ct1", Host: u.Hostname(), Port: 8080, RPCPort: int32(port), URLScheme: u.Scheme})
				require.NoError(t, err)
				err = dev.SetModuleMining(t.Context(), sdk.ModuleMiningRequest{Mining: &mining, Modules: []sdk.ModuleMiningTarget{{Tank: 1, Module: 1, ReportedID: "module-a"}}})
				if tc.wantError != "" {
					require.ErrorContains(t, err, tc.wantError)
				} else {
					require.NoError(t, err)
				}
				require.Equal(t, tc.wantWrites, writes)
			})
		}
	}
}
