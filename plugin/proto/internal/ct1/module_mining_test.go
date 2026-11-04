package ct1

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
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
					if tc.wantWrites == 0 {
						evidence := sdk.WriteEvidence(err)
						require.Equal(t, sdk.WriteNotDispatched, evidence.GetReason())
						require.Equal(t, string(sdk.WriteReasonStaleBinding), evidence.GetMetadata()["reason"])
					} else {
						require.Nil(t, sdk.WriteEvidence(err), "a dispatched write remains uncertain")
					}
				} else {
					require.NoError(t, err)
				}
				require.Equal(t, tc.wantWrites, writes)
			})
		}
	}
}

func TestModuleMiningRejectsBoardClaimsInUnoccupiedSelectedSlots(t *testing.T) {
	for _, claim := range []string{"module-a", "replacement", ""} {
		for _, mining := range []bool{false, true} {
			t.Run(claim+strconv.FormatBool(mining), func(t *testing.T) {
				writes := 0
				srv := newTankGRPCFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					switch r.URL.Path {
					case "/api/v2/system/info":
						_, _ = w.Write([]byte(`{"info":{"id":"ct1","model":"CT1"}}`))
					case "/api/v2/container/map":
						require.NoError(t, json.NewEncoder(w).Encode(MapResponse{ContainerMap: &ContainerMap{Tanks: []Tank{{ID: 1, Modules: []Module{{ID: 1, Slots: []Slot{
							{Occupied: true, Hashboard: &Board{MACAddress: "a", ModuleID: "module-a"}},
							{Occupied: false, Hashboard: &Board{MACAddress: "b", ModuleID: claim}},
							{},
						}}}}}}}))
					default:
						writes++
						_, _ = w.Write([]byte(`{"results":[{"macAddress":"a","applied":true}]}`))
					}
				}))
				u, err := url.Parse(srv.URL)
				require.NoError(t, err)
				port, err := strconv.ParseInt(u.Port(), 10, 32)
				require.NoError(t, err)
				dev, err := newTankTestDevice(t, "ct1", sdk.DeviceInfo{SerialNumber: "ct1", Host: u.Hostname(), Port: 8080, RPCPort: int32(port), URLScheme: u.Scheme})
				require.NoError(t, err)
				err = dev.SetModuleMining(t.Context(), sdk.ModuleMiningRequest{Mining: &mining, Modules: []sdk.ModuleMiningTarget{{Tank: 1, Module: 1, ReportedID: "module-a"}}})
				require.Error(t, err)
				require.Zero(t, writes)
			})
		}
	}
}

func TestModuleMiningRejectsCrossKindIdentifierCollisionsBeforeWriting(t *testing.T) {
	const selectedMAC = "AA-BB-CC-DD-EE-01"
	for _, tc := range []struct {
		name, selectedSerial, neighborSerial, neighborModuleID string
		emptyNeighbor, wantError                               bool
	}{
		{"neighbor serial matches raw MAC", "selected", selectedMAC, "module-b", false, true},
		{"neighbor serial matches normalized MAC", "selected", "aa:bb:cc:dd:ee:01", "module-b", false, true},
		{"empty slot claims matching serial", "selected", selectedMAC, "module-b", true, true},
		{"same board MAC and serial", selectedMAC, "neighbor", "module-b", false, false},
		{"unoccupied slot claims same module", "selected", "neighbor", "module-a", true, true},
		{"padded duplicate module", "selected", "neighbor", "module-a ", false, true},
		{"Unicode padded duplicate module", "selected", "neighbor", "\u00a0module-a", true, true},
		{"distinct identifiers", "selected", "neighbor", "module-b", false, false},
	} {
		for _, mining := range []bool{false, true} {
			t.Run(tc.name+"/"+strconv.FormatBool(mining), func(t *testing.T) {
				writes := 0
				srv := newTankGRPCFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					switch r.URL.Path {
					case "/api/v2/system/info":
						_, _ = w.Write([]byte(`{"info":{"id":"ct1","model":"CT1"}}`))
					case "/api/v2/container/map":
						require.NoError(t, json.NewEncoder(w).Encode(MapResponse{ContainerMap: &ContainerMap{Tanks: []Tank{{ID: 1, Modules: []Module{
							{ID: 1, Slots: []Slot{{Occupied: true, Hashboard: &Board{MACAddress: selectedMAC, Serial: tc.selectedSerial, ModuleID: "module-a", MiningState: miningStateIdle}}, {}, {}}},
							{ID: 2, Slots: []Slot{{Occupied: !tc.emptyNeighbor, Hashboard: &Board{MACAddress: "aa:bb:cc:dd:ee:02", Serial: tc.neighborSerial, ModuleID: tc.neighborModuleID, MiningState: miningStateIdle}}, {}, {}}},
						}}}}}))
					default:
						writes++
						_, _ = w.Write([]byte(`{"results":[{"macAddress":"aa:bb:cc:dd:ee:01","applied":true}]}`))
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
				if tc.wantError {
					if strings.TrimSpace(tc.neighborModuleID) == "module-a" {
						require.ErrorContains(t, err, "selected module identity unavailable or ambiguous")
					} else {
						require.ErrorContains(t, err, "selected module board identifier ambiguous")
					}
					require.Zero(t, writes)
				} else {
					require.NoError(t, err)
					require.Equal(t, 1, writes)
				}
			})
		}
	}
}

func TestModuleMiningVerifiesEverySelectedBoardIdentity(t *testing.T) {
	for _, tc := range []struct {
		name, identity, wantError string
	}{
		{"missing", "", "identity changed or unavailable"},
		{"blank", " \t", "identity changed or unavailable"},
		{"replacement", "replacement", "selected module identity unavailable or ambiguous"},
		{"padded", "module-a ", "selected module identity unavailable or ambiguous"},
		{"Unicode padded", "\u00a0module-a", "selected module identity unavailable or ambiguous"},
	} {
		for changed := range boardsPerModule {
			for _, mining := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/board-%d/mining-%t", tc.name, changed+1, mining), func(t *testing.T) {
					slots := make([]Slot, boardsPerModule)
					for i := range slots {
						slots[i] = Slot{Occupied: true, Hashboard: &Board{MACAddress: fmt.Sprintf("board-%d", i), ModuleID: "module-a", MiningState: miningStateIdle}}
					}
					slots[changed].Hashboard.ModuleID = tc.identity
					var writes atomic.Int32
					srv := newTankGRPCFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						switch r.URL.Path {
						case "/api/v2/system/info":
							_, _ = w.Write([]byte(`{"info":{"id":"ct1","model":"CT1"}}`))
						case "/api/v2/container/map":
							require.NoError(t, json.NewEncoder(w).Encode(MapResponse{ContainerMap: &ContainerMap{Tanks: []Tank{{ID: 1, Modules: []Module{{ID: 1, Slots: slots}}}}}}))
						default:
							writes.Add(1)
							_, _ = w.Write([]byte(`{"results":[]}`))
						}
					}))
					u, err := url.Parse(srv.URL)
					require.NoError(t, err)
					port, err := strconv.ParseInt(u.Port(), 10, 32)
					require.NoError(t, err)
					dev, err := newTankTestDevice(t, "ct1", sdk.DeviceInfo{SerialNumber: "ct1", Host: u.Hostname(), Port: 8080, RPCPort: int32(port), URLScheme: u.Scheme})
					require.NoError(t, err)
					err = dev.SetModuleMining(t.Context(), sdk.ModuleMiningRequest{Mining: &mining, Modules: []sdk.ModuleMiningTarget{{Tank: 1, Module: 1, ReportedID: "module-a"}}})
					require.ErrorContains(t, err, tc.wantError)
					require.Zero(t, writes.Load())
				})
			}
		}
	}
}

func TestModuleMiningScopesEverySelectedBoardAcrossTanks(t *testing.T) {
	module := func(id int32, reportedID string, macs ...string) Module {
		m := Module{ID: id}
		for _, mac := range macs {
			m.Slots = append(m.Slots, Slot{Occupied: true, Hashboard: &Board{MACAddress: mac, ModuleID: reportedID, MiningState: miningStateIdle}})
		}
		return m
	}
	wire := MapResponse{ContainerMap: &ContainerMap{Tanks: []Tank{
		{ID: 1, Modules: []Module{module(1, "module-a", "a1", "a2", "a3"), module(2, "neighbor", "n1", "n2", "n3")}},
		{ID: 2, Modules: []Module{module(2, "module-b", "b1", "b2", "b3")}},
	}}}
	for _, mining := range []bool{false, true} {
		t.Run(strconv.FormatBool(mining), func(t *testing.T) {
			var writes atomic.Int32
			srv := newTankGRPCFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/v2/system/info":
					_, _ = w.Write([]byte(`{"info":{"id":"ct1","model":"CT1"}}`))
				case "/api/v2/container/map":
					require.NoError(t, json.NewEncoder(w).Encode(wire))
				default:
					writes.Add(1)
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
					require.Equal(t, []string{"a1", "a2", "a3", "b1", "b2", "b3"}, body.Scope.Identifiers)
					require.Empty(t, body.Scope.Modules)
					_, _ = w.Write([]byte(`{"results":[{"macAddress":"a1","applied":true},{"macAddress":"a2","applied":true},{"macAddress":"a3","applied":true},{"macAddress":"b1","applied":true},{"macAddress":"b2","applied":true},{"macAddress":"b3","applied":true}]}`))
				}
			}))
			u, err := url.Parse(srv.URL)
			require.NoError(t, err)
			port, err := strconv.ParseInt(u.Port(), 10, 32)
			require.NoError(t, err)
			dev, err := newTankTestDevice(t, "ct1", sdk.DeviceInfo{SerialNumber: "ct1", Host: u.Hostname(), Port: 8080, RPCPort: int32(port), URLScheme: u.Scheme})
			require.NoError(t, err)
			err = dev.SetModuleMining(t.Context(), sdk.ModuleMiningRequest{Mining: &mining, Modules: []sdk.ModuleMiningTarget{
				{Tank: 2, Module: 2, ReportedID: "module-b"},
				{Tank: 1, Module: 1, ReportedID: "module-a"},
			}})
			require.NoError(t, err)
			require.EqualValues(t, 1, writes.Load())
		})
	}
}
