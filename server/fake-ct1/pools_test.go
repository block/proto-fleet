package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	mdk "github.com/block/proto-fleet/sdk/mdk/generated/mdk/v2"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
)

func TestPoolWritesPreserveCredentialsAndActivate(t *testing.T) {
	s := NewContainerState(ContainerConfig{ServerID: "pools", Tanks: 1, ModulesPerTank: 1, SlotsPerModule: 3, Seed: 1})
	api := &grpcAPI{state: s}
	ctx := t.Context()
	created, err := api.CreatePool(ctx, &mdk.CreatePoolRequest{Pool: &mdk.Pool{Name: "Custom", Url: "stratum+tcp://custom.example:3333", Username: "account.worker", Priority: 3, Enabled: true}, Password: "fixture-secret"})
	if err != nil {
		t.Fatal(err)
	}
	id := created.Pool.Id
	updated, err := api.UpdatePool(ctx, &mdk.UpdatePoolRequest{Pool: &mdk.Pool{Id: id, Enabled: false}, UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"enabled"}}})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Pool.Url != created.Pool.Url || updated.Pool.Enabled || s.pools[id].password != "fixture-secret" {
		t.Fatal("partial update lost settings or credentials")
	}
	if _, err := api.UpdatePool(ctx, &mdk.UpdatePoolRequest{Pool: &mdk.Pool{Id: id, Enabled: true}, UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"enabled"}}}); err != nil {
		t.Fatal(err)
	}
	// Returned messages cannot mutate simulator state.
	created.Pool.Url = "changed"
	mux := http.NewServeMux()
	NewRESTApiHandler(s).RegisterRoutes(mux)
	result := httptest.NewRecorder()
	mux.ServeHTTP(result, httptest.NewRequest(http.MethodGet, "/api/v2/pools", nil))
	var listed listPoolsResponse
	if err := json.Unmarshal(result.Body.Bytes(), &listed); err != nil {
		t.Fatal(err)
	}
	if len(listed.Pools) != 4 || listed.Pools[3].URL != "stratum+tcp://custom.example:3333" {
		t.Fatal("REST did not reflect native write")
	}
	if _, err := api.StopMining(ctx, &mdk.StopMiningRequest{}); err != nil {
		t.Fatal(err)
	}
	cap, err := api.GetMiningGroupCapability(ctx, &mdk.GetMiningGroupCapabilityRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := api.ActivateMiningGroups(ctx, &mdk.ActivateMiningGroupsRequest{ExpectedStateRevision: cap.Capability.StateRevision, IdempotencyKey: "activation"}); err != nil {
		t.Fatal(err)
	}
	pools := s.miningGroups.pools["default"]
	if len(pools) != 4 || pools[3].Pool.Url != updated.Pool.Url || pools[3].GetPassword() != "fixture-secret" {
		t.Fatal("activation lost configured pools or credentials")
	}
	if len(s.miningGroups.groups) != 1 || len(s.miningGroups.members) != 3 {
		t.Fatal("activation did not create one group for all boards")
	}
	if _, err := api.UpdatePool(ctx, &mdk.UpdatePoolRequest{Pool: &mdk.Pool{Id: id, Username: "updated.worker"}, UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"username"}}}); err != nil {
		t.Fatal(err)
	}
	if s.miningGroups.pools["default"][3].Pool.Username != "updated.worker" || s.pools[id].password != "fixture-secret" {
		t.Fatal("Default edit lost settings or credentials")
	}
	added, err := api.CreatePool(ctx, &mdk.CreatePoolRequest{Pool: &mdk.Pool{Name: "Extra", Url: "stratum+tcp://extra:3333", Username: "worker", Priority: 4, Enabled: true}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := api.DeletePool(ctx, &mdk.DeletePoolRequest{Id: added.Pool.Id}); err != nil {
		t.Fatal(err)
	}
	if len(s.miningGroups.pools["default"]) != 4 || len(s.miningGroups.members) != 3 {
		t.Fatal("Default pool CRUD changed board assignments")
	}
}

func TestPoolWriteValidation(t *testing.T) {
	s := NewContainerState(ContainerConfig{ServerID: "pools", Tanks: 1, ModulesPerTank: 1, SlotsPerModule: 1, Seed: 1})
	api := &grpcAPI{state: s}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := api.DeletePool(ctx, &mdk.DeletePoolRequest{Id: 1}); status.Code(err) != codes.Canceled {
		t.Fatal(err)
	}
	if _, err := api.CreatePool(t.Context(), &mdk.CreatePoolRequest{}); status.Code(err) != codes.InvalidArgument {
		t.Fatal(err)
	}
	if _, err := api.UpdatePool(t.Context(), &mdk.UpdatePoolRequest{Pool: &mdk.Pool{Id: 1}, UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"unknown"}}}); status.Code(err) != codes.InvalidArgument {
		t.Fatal(err)
	}
	if len(s.pools) != 3 {
		t.Fatal("rejected writes changed pools")
	}
}

func TestWeightedPoolsMayShareAURLWithDifferentUsernames(t *testing.T) {
	s := NewContainerState(ContainerConfig{ServerID: "weighted", Tanks: 1, ModulesPerTank: 1, SlotsPerModule: 1, Seed: 1})
	api := &grpcAPI{state: s}
	weight := 2.0
	created, err := api.CreatePool(t.Context(), &mdk.CreatePoolRequest{Pool: &mdk.Pool{
		Name: "Weighted", Url: defaultActivePool, Username: "another.worker", Priority: 3, Enabled: true, ShareWeight: &weight,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := api.CreatePool(t.Context(), &mdk.CreatePoolRequest{Pool: &mdk.Pool{
		Name: "Duplicate", Url: defaultActivePool, Username: defaultPoolUsername, Priority: 4, Enabled: true, ShareWeight: &weight,
	}}); status.Code(err) != codes.AlreadyExists {
		t.Fatalf("duplicate weighted identity: %v", err)
	}
	if _, ok := s.pools[created.Pool.Id]; !ok {
		t.Fatal("accepted weighted pool was not saved")
	}
}

func TestLegacyPoolRemovalUpdatesDefaultMiningState(t *testing.T) {
	s := NewContainerState(ContainerConfig{ServerID: "default-pools", Tanks: 1, ModulesPerTank: 1, SlotsPerModule: 1, Seed: 1})
	api := &grpcAPI{state: s}
	ctx := t.Context()
	if _, err := api.StopMining(ctx, &mdk.StopMiningRequest{}); err != nil {
		t.Fatal(err)
	}
	if _, err := api.ActivateMiningGroups(ctx, &mdk.ActivateMiningGroupsRequest{ExpectedStateRevision: 1, IdempotencyKey: "activate"}); err != nil {
		t.Fatal(err)
	}
	if _, err := api.StartMining(ctx, &mdk.StartMiningRequest{}); err != nil {
		t.Fatal(err)
	}
	for _, pool := range s.storedPools() {
		if _, err := api.DeletePool(ctx, &mdk.DeletePoolRequest{Id: pool.Id}); err != nil {
			t.Fatal(err)
		}
	}
	if s.eachBoard()[0].isMining() {
		t.Fatal("Default kept mining after its last enabled pool was removed")
	}
}
