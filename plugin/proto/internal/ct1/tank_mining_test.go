package ct1

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"sync/atomic"
	"testing"

	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/require"
)

func TestTankMiningVerifiesContainerIdentityBeforeWriting(t *testing.T) {
	for _, tc := range []struct {
		name           string
		expectedSerial string
		info           string
		status         int
		wantError      bool
	}{
		{"matching", "ct1", `{"info":{"id":"ct1","model":"CT1"}}`, http.StatusOK, false},
		{"normalized", " ct1 ", `{"info":{"id":" ct1 ","model":"CT1"}}`, http.StatusOK, false},
		{"different_container", "ct1", `{"info":{"id":"ct2","model":"CT1"}}`, http.StatusOK, true},
		{"missing_expected_identity", "", `{"info":{"id":"ct1","model":"CT1"}}`, http.StatusOK, true},
		{"missing_live_identity", "ct1", `{"info":{"model":"CT1"}}`, http.StatusOK, true},
		{"blank_live_identity", "ct1", `{"info":{"id":" ","model":"CT1"}}`, http.StatusOK, true},
		{"missing_info", "ct1", `{"info":null}`, http.StatusOK, true},
		{"other_model", "ct1", `{"info":{"id":"ct1","model":"other"}}`, http.StatusOK, true},
		{"malformed_info", "ct1", `{"info":`, http.StatusOK, true},
		{"unavailable_info", "ct1", `{}`, http.StatusServiceUnavailable, true},
	} {
		for _, mining := range []bool{false, true} {
			t.Run(tc.name+"/"+strconv.FormatBool(mining), func(t *testing.T) {
				var writes atomic.Int32
				var mapReads atomic.Int32
				srv := newTankGRPCFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					switch r.URL.Path {
					case "/api/v2/system/info":
						require.Equal(t, http.MethodGet, r.Method)
						w.WriteHeader(tc.status)
						_, _ = w.Write([]byte(tc.info))
					case "/api/v2/container/map":
						mapReads.Add(1)
						_, _ = w.Write([]byte(`{"containerMap":{"tanks":[{"id":1,"modules":[{"id":1,"slots":[{"occupied":true,"hashboard":{"macAddress":"a"}},{"occupied":false},{"occupied":false}]}]}]}}`))
					default:
						require.Equal(t, http.MethodPost, r.Method)
						writes.Add(1)
						_, _ = w.Write([]byte(`{"results":[{"macAddress":"a","applied":true}]}`))
					}
				}))
				defer srv.Close()
				u, err := url.Parse(srv.URL)
				require.NoError(t, err)
				port, err := strconv.ParseInt(u.Port(), 10, 32)
				require.NoError(t, err)
				dev, err := newTankTestDevice(t, "device-id", sdk.DeviceInfo{SerialNumber: tc.expectedSerial, Host: u.Hostname(), Port: 8080, RPCPort: int32(port), URLScheme: u.Scheme})
				require.NoError(t, err)
				err = dev.SetTankMining(t.Context(), sdk.TankMiningRequest{Positions: []int32{1}, Mining: &mining})
				if tc.wantError {
					require.ErrorContains(t, err, "container identity")
					wantReason := sdk.WriteReasonStaleBinding
					if tc.name == "unavailable_info" || tc.name == "malformed_info" {
						wantReason = sdk.WriteReasonReadFailed
					}
					evidence := sdk.WriteEvidence(err)
					require.Equal(t, sdk.WriteNotDispatched, evidence.GetReason())
					require.Equal(t, string(wantReason), evidence.GetMetadata()["reason"])
					require.Zero(t, writes.Load())
					require.Zero(t, mapReads.Load())
				} else {
					require.NoError(t, err)
					require.EqualValues(t, 1, writes.Load())
				}
			})
		}
	}
}

func TestTankMiningRejectsIncompleteModuleSlotsBeforeWriting(t *testing.T) {
	for _, module := range []struct {
		name string
		wire string
	}{
		{"missing", `{"id":2}`},
		{"null", `{"id":2,"slots":null}`},
		{"empty", `{"id":2,"slots":[]}`},
		{"truncated_empty", `{"id":2,"slots":[{"occupied":false}]}`},
		{"truncated_populated", `{"id":2,"slots":[{"occupied":true,"hashboard":{"macAddress":"b"}},{"occupied":false}]}`},
		{"extra_slots", `{"id":2,"slots":[{"occupied":false},{"occupied":false},{"occupied":false},{"occupied":false}]}`},
	} {
		for _, mining := range []bool{false, true} {
			t.Run(module.name+"/"+strconv.FormatBool(mining), func(t *testing.T) {
				var writes atomic.Int32
				srv := newTankGRPCFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path == "/api/v2/system/info" {
						_, _ = w.Write([]byte(`{"info":{"id":"ct1","model":"CT1"}}`))
						return
					}
					if r.Method == http.MethodPost {
						writes.Add(1)
						_, _ = w.Write([]byte(`{"results":[{"macAddress":"a","applied":true},{"macAddress":"b","applied":true}]}`))
						return
					}
					_, _ = w.Write([]byte(`{"containerMap":{"tanks":[{"id":1,"modules":[{"id":1,"slots":[{"occupied":true,"hashboard":{"macAddress":"a"}},{"occupied":false},{"occupied":false}]},` + module.wire + `]}]}}`))
				}))
				defer srv.Close()
				u, err := url.Parse(srv.URL)
				require.NoError(t, err)
				port, err := strconv.ParseInt(u.Port(), 10, 32)
				require.NoError(t, err)
				dev, err := newTankTestDevice(t, "ct1", sdk.DeviceInfo{SerialNumber: "ct1", Host: u.Hostname(), Port: 8080, RPCPort: int32(port), URLScheme: u.Scheme})
				require.NoError(t, err)
				err = dev.SetTankMining(t.Context(), sdk.TankMiningRequest{Positions: []int32{1}, Mining: &mining})
				require.Zero(t, writes.Load(), "incomplete slot topology must prevent every write")
				require.ErrorContains(t, err, "slot topology unavailable")
				evidence := sdk.WriteEvidence(err)
				require.Equal(t, sdk.WriteNotDispatched, evidence.GetReason())
				require.Equal(t, string(sdk.WriteReasonStaleBinding), evidence.GetMetadata()["reason"])
			})
		}
	}
}

func TestTankMiningRejectsAmbiguousBoardIdentitiesBeforeWriting(t *testing.T) {
	for _, tc := range []struct {
		name      string
		other     Board
		positions []int32
		emptySlot bool
	}{
		{"neighbor_mac", Board{MACAddress: "aa:bb:cc:dd:ee:01", Serial: "other"}, []int32{1}, false},
		{"neighbor_formatted_mac", Board{MACAddress: "AA-BB-CC-DD-EE-01", Serial: "other"}, []int32{1}, false},
		{"selected_formatted_mac", Board{MACAddress: "aabb.ccdd.ee01", Serial: "other"}, []int32{1, 2}, false},
		{"neighbor_serial", Board{MACAddress: "aa:bb:cc:dd:ee:02", Serial: " selected "}, []int32{1}, false},
		{"selected_serial", Board{MACAddress: "aa:bb:cc:dd:ee:02", Serial: "selected"}, []int32{1, 2}, false},
		{"empty_slot_identity_claim", Board{MACAddress: "AA-BB-CC-DD-EE-01"}, []int32{1}, true},
	} {
		for _, mining := range []bool{false, true} {
			t.Run(tc.name+"/"+strconv.FormatBool(mining), func(t *testing.T) {
				tc.other.MiningState = "MINING_STATE_IDLE"
				wire := MapResponse{ContainerMap: &ContainerMap{Tanks: []Tank{
					{ID: 1, Modules: []Module{{ID: 1, Slots: []Slot{
						{Occupied: true, Hashboard: &Board{MACAddress: "aa:bb:cc:dd:ee:01", Serial: "selected", MiningState: "MINING_STATE_IDLE"}}, {}, {},
					}}}},
					{ID: 2, Modules: []Module{{ID: 1, Slots: []Slot{
						{Occupied: !tc.emptySlot, Hashboard: &tc.other}, {}, {},
					}}}},
				}}}
				var writes atomic.Int32
				srv := newTankGRPCFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path == "/api/v2/system/info" {
						_, _ = w.Write([]byte(`{"info":{"id":"ct1","model":"CT1"}}`))
						return
					}
					if r.Method == http.MethodPost {
						writes.Add(1)
						_, _ = w.Write([]byte(`{"results":[{"macAddress":"aa:bb:cc:dd:ee:01","applied":true}]}`))
						return
					}
					require.NoError(t, json.NewEncoder(w).Encode(wire))
				}))
				defer srv.Close()
				u, err := url.Parse(srv.URL)
				require.NoError(t, err)
				port, err := strconv.ParseInt(u.Port(), 10, 32)
				require.NoError(t, err)
				dev, err := newTankTestDevice(t, "ct1", sdk.DeviceInfo{SerialNumber: "ct1", Host: u.Hostname(), Port: 8080, RPCPort: int32(port), URLScheme: u.Scheme})
				require.NoError(t, err)
				err = dev.SetTankMining(t.Context(), sdk.TankMiningRequest{Positions: tc.positions, Mining: &mining})
				require.Zero(t, writes.Load(), "ambiguous placement must prevent every write")
				require.ErrorContains(t, err, "board identity unavailable or ambiguous")
			})
		}
	}
}

func TestTankMiningMatchesNormalizedBoardResults(t *testing.T) {
	var writes atomic.Int32
	srv := newTankGRPCFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v2/system/info" {
			_, _ = w.Write([]byte(`{"info":{"id":"ct1","model":"CT1"}}`))
			return
		}
		if r.Method == http.MethodGet {
			_, _ = w.Write([]byte(`{"containerMap":{"tanks":[{"id":1,"modules":[{"id":1,"slots":[{"occupied":true,"hashboard":{"macAddress":" AA-BB-CC-DD-EE-01 "}},{"occupied":false},{"occupied":false}]}]}]}}`))
			return
		}
		writes.Add(1)
		_, _ = w.Write([]byte(`{"results":[{"macAddress":"aabb.ccdd.ee01","applied":true}]}`))
	}))
	defer srv.Close()
	u, err := url.Parse(srv.URL)
	require.NoError(t, err)
	port, err := strconv.ParseInt(u.Port(), 10, 32)
	require.NoError(t, err)
	dev, err := newTankTestDevice(t, "ct1", sdk.DeviceInfo{SerialNumber: "ct1", Host: u.Hostname(), Port: 8080, RPCPort: int32(port), URLScheme: u.Scheme})
	require.NoError(t, err)
	require.NoError(t, dev.SetTankMining(t.Context(), sdk.TankMiningRequest{Positions: []int32{1}, Mining: new(false)}))
	require.EqualValues(t, 1, writes.Load())
}

func TestTankMiningUsesOneScopedWriteAndReportsPartialBoardFailures(t *testing.T) {
	for _, mining := range []bool{false, true} {
		for _, partial := range []bool{false, true} {
			t.Run(strconv.FormatBool(mining)+"/"+strconv.FormatBool(partial), func(t *testing.T) {
				writes := 0
				srv := newTankGRPCFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path == "/api/v2/system/info" {
						_, _ = w.Write([]byte(`{"info":{"id":"ct1","model":"CT1"}}`))
						return
					}
					require.Equal(t, "proto-fleet", r.Header.Get("X-Frigate-Actor"))
					switch r.URL.Path {
					case "/api/v2/container/map":
						require.Equal(t, http.MethodGet, r.Method)
						_, _ = w.Write([]byte(`{"containerMap":{"tanks":[{"id":1,"modules":[{"id":1,"slots":[{"occupied":true,"hashboard":{"macAddress":"a"}},{"occupied":false},{"occupied":false}]},{"id":2,"slots":[{"occupied":false},{"occupied":false},{"occupied":false}]}]},{"id":2,"modules":[{"id":1,"slots":[{"occupied":true,"hashboard":{"macAddress":"neighbor"}},{"occupied":false},{"occupied":false}]}]},{"id":3,"modules":[{"id":4,"slots":[{"occupied":true,"hashboard":{"macAddress":"b"}},{"occupied":false},{"occupied":false}]}]}]}}`))
					default:
						writes++
						action := "stop"
						if mining {
							action = "start"
						}
						require.Equal(t, "/api/v2/mining/scoped/"+action, r.URL.Path)
						var body map[string]any
						require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
						require.Equal(t, map[string]any{"scope": map[string]any{"modules": []any{map[string]any{"tank": float64(1), "module": float64(1)}, map[string]any{"tank": float64(3), "module": float64(4)}}}}, body)
						require.NoError(t, json.NewEncoder(w).Encode(map[string]any{"results": []any{map[string]any{"macAddress": "a", "applied": true}, map[string]any{"macAddress": "b", "applied": !partial, "message": "disconnected"}}}))
					}
				}))
				defer srv.Close()
				u, _ := url.Parse(srv.URL)
				port, err := strconv.ParseInt(u.Port(), 10, 32)
				require.NoError(t, err)
				dev, err := newTankTestDevice(t, "ct1", sdk.DeviceInfo{SerialNumber: "ct1", Host: u.Hostname(), Port: 8080, RPCPort: int32(port), URLScheme: u.Scheme})
				require.NoError(t, err)
				err = dev.SetTankMining(t.Context(), sdk.TankMiningRequest{Positions: []int32{3, 1}, Mining: &mining})
				if partial {
					require.EqualError(t, err, "tank mining partially applied (1/2 boards failed): b: disconnected")
				} else {
					require.NoError(t, err)
				}
				require.Equal(t, 1, writes)
			})
		}
	}
}

func TestTankMiningNeverBroadensMissingOrInvalidScope(t *testing.T) {
	for _, positions := range [][]int32{nil, {0}, {1, 1}, {2}, {1, 2}} {
		writes := 0
		srv := newTankGRPCFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/api/v2/system/info" {
				_, _ = w.Write([]byte(`{"info":{"id":"ct1","model":"CT1"}}`))
				return
			}
			if r.Method == http.MethodPost {
				writes++
				return
			}
			_, _ = w.Write([]byte(`{"containerMap":{"tanks":[{"id":1,"modules":[{"id":1,"slots":[{"occupied":true,"hashboard":{"macAddress":"a"}},{"occupied":false},{"occupied":false}]},{"id":2,"slots":[{"occupied":false},{"occupied":false},{"occupied":false}]}]}]}}`))
		}))
		u, _ := url.Parse(srv.URL)
		port, err := strconv.ParseInt(u.Port(), 10, 32)
		require.NoError(t, err)
		dev, err := newTankTestDevice(t, "ct1", sdk.DeviceInfo{SerialNumber: "ct1", Host: u.Hostname(), Port: 8080, RPCPort: int32(port), URLScheme: u.Scheme})
		require.NoError(t, err)
		require.Error(t, dev.SetTankMining(t.Context(), sdk.TankMiningRequest{Positions: positions, Mining: new(false)}))
		require.Zero(t, writes)
		srv.Close()
	}
}

func TestTankMiningDoesNotRetryAmbiguousOrRejectedWrites(t *testing.T) {
	for _, response := range []string{`{}`, `{"results":[{"macAddress":"other","applied":true}]}`, `{"results":[{"macAddress":"a","applied":true},{"macAddress":"a","applied":true}]}`} {
		writes := 0
		srv := newTankGRPCFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/api/v2/system/info" {
				_, _ = w.Write([]byte(`{"info":{"id":"ct1","model":"CT1"}}`))
				return
			}
			if r.Method == http.MethodGet {
				_, _ = w.Write([]byte(`{"containerMap":{"tanks":[{"id":1,"modules":[{"id":1,"slots":[{"occupied":true,"hashboard":{"macAddress":"a"}},{"occupied":false},{"occupied":false}]},{"id":2,"slots":[{"occupied":false},{"occupied":false},{"occupied":false}]}]}]}}`))
				return
			}
			writes++
			_, _ = w.Write([]byte(response))
		}))
		u, _ := url.Parse(srv.URL)
		port, err := strconv.ParseInt(u.Port(), 10, 32)
		require.NoError(t, err)
		dev, err := newTankTestDevice(t, "ct1", sdk.DeviceInfo{SerialNumber: "ct1", Host: u.Hostname(), Port: 8080, RPCPort: int32(port), URLScheme: u.Scheme})
		require.NoError(t, err)
		require.Error(t, dev.SetTankMining(t.Context(), sdk.TankMiningRequest{Positions: []int32{1}, Mining: new(false)}))
		require.Equal(t, 1, writes)
		srv.Close()
	}
}

// Identity failures are exercised at control time on an already-created handle.
func newTankTestDevice(t *testing.T, id string, info sdk.DeviceInfo) (*Device, error) {
	t.Helper()
	c, err := NewClient(info, sdk.UsernamePassword{})
	if err != nil {
		return nil, err
	}
	t.Cleanup(func() { _ = c.Close() })
	return &Device{id: id, info: info, clients: []*Client{c}}, nil
}

func TestTankMiningSkipsUnoccupiedSlotsWithResidualBoards(t *testing.T) {
	for _, mining := range []bool{false, true} {
		t.Run(strconv.FormatBool(mining), func(t *testing.T) {
			writes := 0
			srv := newTankGRPCFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/v2/system/info":
					_, _ = w.Write([]byte(`{"info":{"id":"ct1","model":"CT1"}}`))
				case "/api/v2/container/map":
					_, _ = w.Write([]byte(`{"containerMap":{"tanks":[{"id":1,"modules":[{"id":1,"slots":[{"occupied":true,"hashboard":{"macAddress":"a"}},{"occupied":false,"hashboard":{"macAddress":"old"}},{"occupied":false}]}]}]}}`))
				default:
					writes++
					_, _ = w.Write([]byte(`{"results":[{"macAddress":"a","applied":true}]}`))
				}
			}))
			defer srv.Close()
			u, err := url.Parse(srv.URL)
			require.NoError(t, err)
			port, err := strconv.ParseInt(u.Port(), 10, 32)
			require.NoError(t, err)
			dev, err := newTankTestDevice(t, "ct1", sdk.DeviceInfo{SerialNumber: "ct1", Host: u.Hostname(), Port: 8080, RPCPort: int32(port), URLScheme: u.Scheme})
			require.NoError(t, err)
			require.NoError(t, dev.SetTankMining(t.Context(), sdk.TankMiningRequest{Positions: []int32{1}, Mining: &mining}))
			require.Equal(t, 1, writes)
		})
	}
}
