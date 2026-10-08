package ct1

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"sync/atomic"
	"testing"

	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/require"
)

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
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
				dev, err := NewDevice("ct1", sdk.DeviceInfo{Host: u.Hostname(), Port: int32(port), URLScheme: u.Scheme}, sdk.UsernamePassword{})
				require.NoError(t, err)
				err = dev.SetTankMining(t.Context(), sdk.TankMiningRequest{Positions: []int32{1}, Mining: &mining})
				require.Zero(t, writes.Load(), "incomplete slot topology must prevent every write")
				require.ErrorContains(t, err, "slot topology unavailable")
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
				wire := MapResponse{ContainerMap: &ContainerMap{Tanks: []Tank{
					{ID: 1, Modules: []Module{{ID: 1, Slots: []Slot{
						{Occupied: true, Hashboard: &Board{MACAddress: "aa:bb:cc:dd:ee:01", Serial: "selected"}}, {}, {},
					}}}},
					{ID: 2, Modules: []Module{{ID: 1, Slots: []Slot{
						{Occupied: !tc.emptySlot, Hashboard: &tc.other}, {}, {},
					}}}},
				}}}
				var writes atomic.Int32
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
				dev, err := NewDevice("ct1", sdk.DeviceInfo{Host: u.Hostname(), Port: int32(port), URLScheme: u.Scheme}, sdk.UsernamePassword{})
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
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	dev, err := NewDevice("ct1", sdk.DeviceInfo{Host: u.Hostname(), Port: int32(port), URLScheme: u.Scheme}, sdk.UsernamePassword{})
	require.NoError(t, err)
	require.NoError(t, dev.SetTankMining(t.Context(), sdk.TankMiningRequest{Positions: []int32{1}, Mining: new(false)}))
	require.EqualValues(t, 1, writes.Load())
}

func TestTankMiningUsesOneScopedWriteAndReportsPartialBoardFailures(t *testing.T) {
	for _, mining := range []bool{false, true} {
		for _, partial := range []bool{false, true} {
			t.Run(strconv.FormatBool(mining)+"/"+strconv.FormatBool(partial), func(t *testing.T) {
				writes := 0
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
				dev, err := NewDevice("ct1", sdk.DeviceInfo{Host: u.Hostname(), Port: int32(port), URLScheme: u.Scheme}, sdk.UsernamePassword{})
				require.NoError(t, err)
				err = dev.SetTankMining(t.Context(), sdk.TankMiningRequest{Positions: []int32{3, 1}, Mining: &mining})
				if partial {
					require.ErrorContains(t, err, "partially applied")
					require.ErrorContains(t, err, "disconnected")
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
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodPost {
				writes++
				return
			}
			_, _ = w.Write([]byte(`{"containerMap":{"tanks":[{"id":1,"modules":[{"id":1,"slots":[{"occupied":true,"hashboard":{"macAddress":"a"}},{"occupied":false},{"occupied":false}]},{"id":2,"slots":[{"occupied":false},{"occupied":false},{"occupied":false}]}]}]}}`))
		}))
		u, _ := url.Parse(srv.URL)
		port, err := strconv.ParseInt(u.Port(), 10, 32)
		require.NoError(t, err)
		dev, err := NewDevice("ct1", sdk.DeviceInfo{Host: u.Hostname(), Port: int32(port), URLScheme: u.Scheme}, sdk.UsernamePassword{})
		require.NoError(t, err)
		require.Error(t, dev.SetTankMining(t.Context(), sdk.TankMiningRequest{Positions: positions, Mining: new(false)}))
		require.Zero(t, writes)
		srv.Close()
	}
}

func TestTankMiningDoesNotRetryAmbiguousOrRejectedWrites(t *testing.T) {
	for _, response := range []string{`{}`, `{"results":[{"macAddress":"other","applied":true}]}`, `{"results":[{"macAddress":"a","applied":true},{"macAddress":"a","applied":true}]}`} {
		writes := 0
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
		dev, err := NewDevice("ct1", sdk.DeviceInfo{Host: u.Hostname(), Port: int32(port), URLScheme: u.Scheme}, sdk.UsernamePassword{})
		require.NoError(t, err)
		require.Error(t, dev.SetTankMining(t.Context(), sdk.TankMiningRequest{Positions: []int32{1}, Mining: new(false)}))
		require.Equal(t, 1, writes)
		srv.Close()
	}
}
