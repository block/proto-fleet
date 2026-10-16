package main

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	mdk "github.com/block/proto-fleet/sdk/mdk/generated/mdk/v2"
)

func TestUnlocatedInventoryMap(t *testing.T) {
	for _, tc := range []struct {
		name  string
		count int
		want  int
	}{
		{"default", 0, 0},
		{"mixed", 6, 6},
		{"partial module", 1, 1},
		{"all", -1, 18},
		{"capped at inventory size", 100, 18},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, state := newTestServer(t)
			before := state.miningStatusDTO()
			state.Config.UnlocatedBoards = tc.count
			mux := http.NewServeMux()
			NewRESTApiHandler(state).RegisterRoutes(mux)
			server := httptest.NewServer(mux)
			defer server.Close()
			var rest containerMapResponse
			getJSON(t, server, "/api/v2/container/map", &rest)
			seen := map[string]bool{}
			unlocated := 0
			for _, tank := range rest.ContainerMap.Tanks {
				for _, module := range tank.Modules {
					for _, slot := range module.Slots {
						board := slot.Hashboard
						if board == nil || !slot.Occupied || seen[board.Serial] {
							t.Fatalf("missing or duplicate inventory: %+v", slot)
						}
						seen[board.Serial] = true
						original := state.findBoard(board.Serial)
						if original == nil || original.MAC != board.MACAddress || board.Health.Hashrate <= 0 {
							t.Fatalf("lost board identity or telemetry: %+v", board)
						}
						if tank.ID == 0 {
							unlocated++
							if module.ID != 0 || board.Location != (hashboardLoc{}) || slot.PowerFeedID != "" || slot.BreakerID != "" || board.ModuleID != "" || board.Source != "HASHBOARD_SOURCE_INVENTORY" {
								t.Fatalf("unlocated board claims placement: %+v", slot)
							}
						} else if board.Location.Tank != tank.ID || board.Location.Module != module.ID || board.Location.HashboardPosition < 1 {
							t.Fatalf("mapped board lost placement: %+v", board)
						}
					}
				}
			}
			if unlocated != tc.want || len(seen) != state.BoardCount() {
				t.Fatalf("unlocated=%d inventory=%d, want %d/%d", unlocated, len(seen), tc.want, state.BoardCount())
			}
			if tc.want == state.BoardCount() && (len(rest.ContainerMap.Tanks) != 1 || rest.ContainerMap.Tanks[0].ID != 0) {
				t.Fatal("entirely unlocated inventory must not report physical tanks")
			}
			if !reflect.DeepEqual(before, state.miningStatusDTO()) {
				t.Fatal("location mode changed mining telemetry")
			}
			var repeated containerMapResponse
			getJSON(t, server, "/api/v2/container/map", &repeated)
			if !reflect.DeepEqual(rest, repeated) {
				t.Fatal("location selection changed between reads")
			}
			// Native RPC uses the shared DTO projection and must not restore a
			// default board position when decoding inventory-only locations.
			api := &grpcAPI{state: state}
			rpc, err := api.GetContainerMap(t.Context(), &mdk.GetContainerMapRequest{})
			if err != nil {
				t.Fatal(err)
			}
			rpcBoards, rpcUnlocated := 0, 0
			for _, tank := range rpc.GetContainerMap().GetTanks() {
				for _, module := range tank.GetModules() {
					for _, slot := range module.GetSlots() {
						rpcBoards++
						if tank.Id == 0 {
							rpcUnlocated++
							b := slot.GetHashboard()
							if b.GetLocation().HashboardPosition != nil || b.GetSource() != mdk.HashboardSource_HASHBOARD_SOURCE_INVENTORY {
								t.Fatal("RPC invented location for unlocated board", b)
							}
						}
					}
				}
			}
			if rpcBoards != len(seen) || rpcUnlocated != tc.want {
				t.Fatalf("RPC inventory=%d unlocated=%d, want %d/%d", rpcBoards, rpcUnlocated, len(seen), tc.want)
			}
		})
	}
}

func TestParseUnlocatedBoards(t *testing.T) {
	for _, tc := range []struct {
		value   string
		want    int
		invalid bool
	}{
		{"", 0, false}, {"0", 0, false}, {"6", 6, false}, {"all", -1, false},
		{"-1", 0, true}, {"6.5", 0, true}, {"invalid", 0, true},
	} {
		t.Run(tc.value, func(t *testing.T) {
			got, err := parseUnlocatedBoards(tc.value)
			if got != tc.want || (err != nil) != tc.invalid {
				t.Fatalf("parse %q = %d, %v; want %d, invalid=%t", tc.value, got, err, tc.want, tc.invalid)
			}
		})
	}
}
