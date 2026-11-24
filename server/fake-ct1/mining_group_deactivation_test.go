package main

import (
	"fmt"
	"testing"

	mdk "github.com/block/proto-fleet/sdk/mdk/generated/mdk/v2"
	"google.golang.org/protobuf/proto"
)

func replaceDefaultPoolsLegacy(t testing.TB, api *grpcAPI, pools []*mdk.MiningGroupPoolInput) {
	t.Helper()
	current, err := api.ListPools(t.Context(), &mdk.ListPoolsRequest{})
	if err != nil {
		t.Fatal(err)
	}
	for _, pool := range current.Pools {
		if _, err = api.DeletePool(t.Context(), &mdk.DeletePoolRequest{Id: pool.Id}); err != nil {
			t.Fatal(err)
		}
	}
	for _, input := range pools {
		pool := input.GetPool()
		if _, err = api.CreatePool(t.Context(), &mdk.CreatePoolRequest{Pool: &mdk.Pool{
			Name: pool.Name, Url: pool.Url, Username: pool.Username, Priority: pool.Priority,
			Enabled: pool.Enabled, ShareWeight: pool.ShareWeight,
		}, Password: input.GetPassword()}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestMiningGroupModeCyclesPreserveDefaultPolicy(t *testing.T) {
	for _, tc := range []struct {
		name    string
		changed bool
		empty   bool
		mining  bool
	}{
		{name: "unchanged stopped"},
		{name: "unchanged mining", mining: true},
		{name: "changed stopped", changed: true},
		{name: "changed mining", changed: true, mining: true},
		{name: "empty pools", changed: true, empty: true, mining: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := NewContainerState(ContainerConfig{ServerID: "mode-cycle", Tanks: 1, ModulesPerTank: 1, SlotsPerModule: 3, Seed: 1})
			api := &grpcAPI{state: s}
			ctx := t.Context()
			if _, err := api.StopMining(ctx, &mdk.StopMiningRequest{}); err != nil {
				t.Fatal(err)
			}
			activate := &mdk.ActivateMiningGroupsRequest{ExpectedStateRevision: s.initMiningGroups().capability.StateRevision, IdempotencyKey: "activate"}
			activated, err := api.ActivateMiningGroups(ctx, activate)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := api.CreateMiningGroup(ctx, &mdk.CreateMiningGroupRequest{Metadata: &mdk.MiningGroupMetadata{Name: "Unused"}, IdempotencyKey: "unused-group"}); err != nil {
				t.Fatal(err)
			}
			if tc.changed {
				var pools []*mdk.MiningGroupPoolInput
				if !tc.empty {
					password, weight := "current-secret", 2.0
					pools = []*mdk.MiningGroupPoolInput{{Pool: &mdk.MiningGroupPoolRecord{Name: "Current", Url: "stratum+tcp://current.example:3333", Username: "current.worker", Priority: 2, Enabled: true, ShareWeight: &weight}, Password: &password}}
				}
				replaceDefaultPoolsLegacy(t, api, pools)
			}
			if tc.mining {
				if _, err := api.StartMining(ctx, &mdk.StartMiningRequest{}); err != nil {
					t.Fatal(err)
				}
			}
			expected := make([]*mdk.MiningGroupPoolInput, 0, len(s.miningGroups.pools["default"]))
			for _, pool := range s.miningGroups.pools["default"] {
				expected = append(expected, proto.CloneOf(pool))
			}
			states := make(map[string]string)
			for _, board := range s.eachBoard() {
				states[board.MAC] = board.State
				if board.isMining() != (tc.mining && !tc.empty) {
					t.Fatal("fixture did not establish the requested mining state")
				}
			}
			deactivate := &mdk.DeactivateMiningGroupsRequest{ExpectedStateRevision: s.miningGroups.capability.StateRevision, IdempotencyKey: "deactivate"}
			disabled, err := api.DeactivateMiningGroups(ctx, deactivate)
			if err != nil {
				t.Fatal(err)
			}
			for _, board := range s.eachBoard() {
				if board.State != states[board.MAC] {
					t.Fatal("deactivation changed a board's mining state")
				}
			}
			legacy, err := api.ListPools(ctx, &mdk.ListPoolsRequest{})
			if err != nil {
				t.Fatal(err)
			}
			if len(legacy.Pools) != len(expected) {
				t.Fatalf("disabled mode has %d pools, want %d", len(legacy.Pools), len(expected))
			}
			for i, input := range expected {
				p := input.Pool
				want := &mdk.Pool{Id: int64(p.Id), Name: p.Name, Url: p.Url, Username: p.Username, Priority: p.Priority, Enabled: p.Enabled, ShareWeight: p.ShareWeight}
				if !proto.Equal(legacy.Pools[i], want) || s.pools[int64(p.Id)].password != input.GetPassword() {
					t.Fatal("deactivation lost current pool settings or credentials")
				}
			}
			// An old successful activation must remain replayable without enabling
			// the new legacy mode or overwriting its pools.
			replay, err := api.ActivateMiningGroups(ctx, activate)
			if err != nil || !proto.Equal(replay, activated) || s.miningGroups.capability.Enabled {
				t.Fatalf("activation receipt changed or replay mutated mode: %v", err)
			}
			for cycle := range 2 {
				if _, err := api.StopMining(ctx, &mdk.StopMiningRequest{}); err != nil {
					t.Fatal(err)
				}
				if _, err := api.ActivateMiningGroups(ctx, &mdk.ActivateMiningGroupsRequest{ExpectedStateRevision: s.miningGroups.capability.StateRevision, IdempotencyKey: fmt.Sprintf("reactivate-%d", cycle)}); err != nil {
					t.Fatal(err)
				}
				if len(s.miningGroups.groups) != 1 || len(s.miningGroups.members) != len(states) {
					t.Fatal("reactivation retained old groups or memberships")
				}
				actual := s.miningGroups.pools["default"]
				if len(actual) != len(expected) {
					t.Fatalf("reactivation has %d pools, want %d", len(actual), len(expected))
				}
				for i := range expected {
					if !proto.Equal(actual[i], expected[i]) {
						t.Fatal("reactivation changed pool settings or credentials")
					}
				}
				revision := s.miningGroups.capability.StateRevision
				replayed, err := api.DeactivateMiningGroups(ctx, deactivate)
				if err != nil || !proto.Equal(replayed, disabled) || !s.miningGroups.capability.Enabled || s.miningGroups.capability.StateRevision != revision {
					t.Fatalf("deactivation receipt changed or replay mutated mode: %v", err)
				}
				if _, err := api.DeactivateMiningGroups(ctx, &mdk.DeactivateMiningGroupsRequest{ExpectedStateRevision: revision, IdempotencyKey: fmt.Sprintf("disable-%d", cycle)}); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}
