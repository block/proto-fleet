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

func boardIdentifyMap(boardMAC, moduleID string) MapResponse {
	return MapResponse{ContainerMap: &ContainerMap{Tanks: []Tank{{ID: 1, Modules: []Module{
		{ID: 1, Slots: []Slot{
			{Occupied: true, Hashboard: &Board{MACAddress: "AA:00:00:00:00:01", ModuleID: moduleID, MiningState: miningStateIdle}},
			{Occupied: true, Hashboard: &Board{MACAddress: boardMAC, ModuleID: moduleID, MiningState: miningStateIdle}},
			{},
		}},
		{ID: 2, Slots: []Slot{{Occupied: true, Hashboard: &Board{MACAddress: "aa:00:00:00:00:09", ModuleID: "module-b", MiningState: miningStateIdle}}, {}, {}}},
	}}}}}
}

func TestBoardIdentifyAddressesOnlyTheConfirmedBoard(t *testing.T) {
	target := sdk.BoardIdentifyTarget{Tank: 1, Module: 1, Slot: 2, ModuleReportedID: "module-a", MACAddress: "aa:00:00:00:00:02"}
	for _, tc := range []struct {
		name, boardMAC, moduleID, response string
		status, wantWrites                 int
		wantError                          string
		notDispatched                      bool
	}{
		{"blink confirmed", "AA:00:00:00:00:02", "module-a", `{"boardsUpdated":1}`, http.StatusOK, 1, "", false},
		{"board replaced", "aa:00:00:00:00:07", "module-a", `{}`, http.StatusOK, 0, "slot 2 board was replaced", true},
		{"module replaced", "aa:00:00:00:00:02", "module-new", `{}`, http.StatusOK, 0, "identity changed or unavailable", true},
		{"slot emptied", "", "module-a", `{}`, http.StatusOK, 0, "slot 2 no longer has a board", true},
		{"not applied", "aa:00:00:00:00:02", "module-a", `{"boardsUpdated":0}`, http.StatusOK, 1, "controller updated 0 boards", false},
		{"RPC failure", "aa:00:00:00:00:02", "module-a", `{}`, http.StatusServiceUnavailable, 1, "board identify outcome unconfirmed", false},
	} {
		for _, mode := range []sdk.BoardIdentifyMode{sdk.BoardIdentifyBlink, sdk.BoardIdentifyOff} {
			t.Run(tc.name+"/"+string(mode), func(t *testing.T) {
				writes := 0
				srv := newTankGRPCFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					switch r.URL.Path {
					case "/api/v2/system/info":
						_, _ = w.Write([]byte(`{"info":{"id":"ct1","model":"CT1"}}`))
					case "/api/v2/container/map":
						wire := boardIdentifyMap(tc.boardMAC, tc.moduleID)
						if tc.boardMAC == "" {
							wire.ContainerMap.Tanks[0].Modules[0].Slots[1] = Slot{}
						}
						require.NoError(t, json.NewEncoder(w).Encode(wire))
					default:
						writes++
						require.Equal(t, "/api/v2/hashboards/identify-led", r.URL.Path)
						var body struct {
							Scope struct {
								Identifiers []string
								Modules     []struct{ Tank, Module int32 }
							}
							Mode string
						}
						require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
						require.Equal(t, []string{tc.boardMAC}, body.Scope.Identifiers, "only the verified board is addressed")
						require.Empty(t, body.Scope.Modules)
						want := "HASHBOARD_IDENTIFY_LED_MODE_OFF"
						if mode == sdk.BoardIdentifyBlink {
							want = "HASHBOARD_IDENTIFY_LED_MODE_BLINK"
						}
						require.Equal(t, want, body.Mode)
						w.WriteHeader(tc.status)
						_, _ = w.Write([]byte(tc.response))
					}
				}))
				u, err := url.Parse(srv.URL)
				require.NoError(t, err)
				port, err := strconv.ParseInt(u.Port(), 10, 32)
				require.NoError(t, err)
				dev, err := newTankTestDevice(t, "ct1", sdk.DeviceInfo{SerialNumber: "ct1", Host: u.Hostname(), Port: 8080, RPCPort: int32(port), URLScheme: u.Scheme})
				require.NoError(t, err)
				err = dev.SetBoardIdentify(t.Context(), sdk.BoardIdentifyRequest{Board: target, Mode: mode})
				require.Equal(t, tc.wantWrites, writes, "%v", err)
				if tc.wantError == "" {
					require.NoError(t, err)
					return
				}
				require.ErrorContains(t, err, tc.wantError)
				if tc.notDispatched {
					evidence := sdk.WriteEvidence(err)
					require.Equal(t, sdk.WriteNotDispatched, evidence.GetReason())
					require.Equal(t, string(sdk.WriteReasonStaleBinding), evidence.GetMetadata()["reason"])
				} else {
					require.Nil(t, sdk.WriteEvidence(err), "a dispatched write remains uncertain")
				}
			})
		}
	}
}

func TestBoardIdentifySelectorRejectsAmbiguousIdentity(t *testing.T) {
	target := sdk.BoardIdentifyTarget{Tank: 1, Module: 1, Slot: 2, ModuleReportedID: "module-a", MACAddress: "aa:00:00:00:00:02"}
	wire := boardIdentifyMap("aa:00:00:00:00:02", "module-a")
	got, err := identifyBoardSelector(wire, target)
	require.NoError(t, err)
	require.Equal(t, "aa:00:00:00:00:02", got)

	wire.ContainerMap.Tanks[0].Modules[1].Slots[1] = Slot{Occupied: true, Hashboard: &Board{MACAddress: "AA:00:00:00:00:02", ModuleID: "module-b", MiningState: miningStateIdle}}
	_, err = identifyBoardSelector(wire, target)
	require.ErrorContains(t, err, "ambiguous")

	_, err = identifyBoardSelector(boardIdentifyMap("aa:00:00:00:00:02", "module-a"), sdk.BoardIdentifyTarget{Tank: 1, Module: 3, Slot: 1, ModuleReportedID: "module-a", MACAddress: "aa:00:00:00:00:02"})
	require.ErrorContains(t, err, "identity changed or unavailable")
}

func TestBoardIdentifyRejectsInvalidRequestWithoutDispatch(t *testing.T) {
	dev := &Device{}
	err := dev.SetBoardIdentify(t.Context(), sdk.BoardIdentifyRequest{Board: sdk.BoardIdentifyTarget{Tank: 1, Module: 1, Slot: 4, ModuleReportedID: "m", MACAddress: "a"}, Mode: sdk.BoardIdentifyBlink})
	require.Error(t, err)
	require.Equal(t, sdk.WriteNotDispatched, sdk.WriteEvidence(err).GetReason())
}
