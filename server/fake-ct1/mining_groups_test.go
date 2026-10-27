package main

import (
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	mdk "github.com/block/proto-fleet/sdk/mdk/generated/mdk/v2"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
)

func TestMiningGroupsWorkflow(t *testing.T) {
	s := NewContainerState(ContainerConfig{ServerID: "groups", Tanks: 1, ModulesPerTank: 2, SlotsPerModule: 3, Seed: 1})
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := newGRPCServer(s)
	t.Cleanup(server.Stop)
	go func() { _ = server.Serve(listener) }()
	conn, err := grpc.NewClient(listener.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	client := mdk.NewMiningGroupServiceClient(conn)
	ctx := t.Context()
	mux := http.NewServeMux()
	NewRESTApiHandler(s).RegisterRoutes(mux)
	control := func(path string) {
		t.Helper()
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/v2/mining/"+path, nil))
		if response.Code != http.StatusOK {
			t.Fatal(response.Body.String())
		}
	}
	capability, err := client.GetMiningGroupCapability(ctx, &mdk.GetMiningGroupCapabilityRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if capability.Capability.Enabled || capability.Capability.Name != "mining-groups-v1" || capability.Capability.AssignmentGranularity != "PHYSICAL_HASHBOARD" {
		t.Fatal(capability)
	}
	activate := &mdk.ActivateMiningGroupsRequest{ExpectedStateRevision: capability.Capability.StateRevision, IdempotencyKey: "activate"}
	if _, err = client.ActivateMiningGroups(ctx, activate); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("activation while mining: %v", err)
	}
	mining := mdk.NewMiningServiceClient(conn)
	if _, err := mining.StopMining(ctx, &mdk.StopMiningRequest{}); err != nil {
		t.Fatal(err)
	}
	stopped, err := mining.GetMiningStatus(ctx, &mdk.GetMiningStatusRequest{})
	if err != nil || stopped.GetMining() || stopped.GetActiveBoards() != 0 {
		t.Fatalf("native stop status: %v, %v", stopped, err)
	}
	activated, err := client.ActivateMiningGroups(ctx, activate)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := client.ActivateMiningGroups(ctx, activate)
	if err != nil || !proto.Equal(activated, replay) {
		t.Fatalf("activation replay: %v", err)
	}
	changed := proto.Clone(activate).(*mdk.ActivateMiningGroupsRequest)
	changed.ExpectedStateRevision++
	if _, err = client.ActivateMiningGroups(ctx, changed); status.Code(err) != codes.AlreadyExists {
		t.Fatalf("changed retry: %v", err)
	}
	members, err := client.ListMiningGroupMemberships(ctx, &mdk.ListMiningGroupMembershipsRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if len(members.Memberships) != 6 {
		t.Fatal(members)
	}
	for _, m := range members.Memberships {
		if m.GroupId != "default" {
			t.Fatal(m)
		}
	}
	create := &mdk.CreateMiningGroupRequest{Metadata: &mdk.MiningGroupMetadata{Name: "Customer"}, IdempotencyKey: "create"}
	created, err := client.CreateMiningGroup(ctx, create)
	if err != nil {
		t.Fatal(err)
	}
	createdAgain, err := client.CreateMiningGroup(ctx, create)
	if err != nil || !proto.Equal(created, createdAgain) {
		t.Fatalf("create replay: %v", err)
	}
	if created.Group.DesiredMining {
		t.Fatal("new group must be stopped")
	}
	selected := members.Memberships[0].MacAddress
	transfer := &mdk.RequestMiningGroupTransferRequest{SourceGroupId: "default", DestinationGroupId: created.Group.Id, SourceExpectedRevision: 1, DestinationExpectedRevision: created.Group.Revision, MacAddresses: []string{selected}, IdempotencyKey: "transfer"}
	invalid := proto.Clone(transfer).(*mdk.RequestMiningGroupTransferRequest)
	invalid.IdempotencyKey = "invalid"
	invalid.MacAddresses = append(invalid.MacAddresses, "missing")
	if _, err = client.RequestMiningGroupTransfer(ctx, invalid); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("unknown board: %v", err)
	}
	moved, err := client.RequestMiningGroupTransfer(ctx, transfer)
	if err != nil {
		t.Fatal(err)
	}
	if moved.Operation.Phase != "COMPLETE" || moved.Operation.StopBoardsConfirmed != 1 {
		t.Fatal(moved)
	}
	movedAgain, err := client.RequestMiningGroupTransfer(ctx, transfer)
	if err != nil || !proto.Equal(moved, movedAgain) {
		t.Fatalf("transfer replay: %v", err)
	}
	operation, err := client.GetMiningGroupOperation(ctx, &mdk.GetMiningGroupOperationRequest{Id: moved.Operation.Id})
	if err != nil || !proto.Equal(operation.GetOperation(), moved.Operation) {
		t.Fatalf("operation receipt: %v", err)
	}
	started, err := client.UpdateMiningGroupIntent(ctx, &mdk.UpdateMiningGroupIntentRequest{Id: created.Group.Id, ExpectedRevision: 2, DesiredMining: true, UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"desired_mining"}}, IdempotencyKey: "start"})
	if err != nil {
		t.Fatal(err)
	}
	if s.boardsByMAC[strings.ToUpper(selected)].isMining() {
		t.Fatal("group without pools fell back to default pools")
	}
	primary, backup := "primary-secret", "backup-secret"
	replace := &mdk.ReplaceMiningGroupPoolsRequest{GroupId: created.Group.Id, ExpectedRevision: started.Group.Revision, IdempotencyKey: "pools", Pools: []*mdk.MiningGroupPoolInput{
		{Pool: &mdk.MiningGroupPoolRecord{Id: 1, Url: "stratum+tcp://primary.example.com:3333", Username: "worker", Enabled: true}, Password: &primary},
		{Pool: &mdk.MiningGroupPoolRecord{Id: 2, Url: "stratum+tcp://backup.example.com:3333", Username: "backup-worker", Priority: 1, Enabled: true}, Password: &backup},
	}}
	configured, err := client.ReplaceMiningGroupPools(ctx, replace)
	if err != nil {
		t.Fatal(err)
	}
	configuredAgain, err := client.ReplaceMiningGroupPools(ctx, replace)
	if err != nil || !proto.Equal(configured, configuredAgain) {
		t.Fatalf("pool retry: %v", err)
	}
	pools, err := client.ListMiningGroupPools(ctx, &mdk.ListMiningGroupPoolsRequest{GroupId: created.Group.Id})
	if err != nil {
		t.Fatal(err)
	}
	wire, err := protojson.Marshal(pools)
	if err != nil {
		t.Fatal(err)
	}
	if len(pools.Pools) != 2 || pools.Pools[1].Priority != 1 || strings.Contains(string(wire), "secret") {
		t.Fatalf("invalid pool readback: %s", wire)
	}
	stale := proto.Clone(replace).(*mdk.ReplaceMiningGroupPoolsRequest)
	stale.IdempotencyKey = "stale"
	if _, err = client.ReplaceMiningGroupPools(ctx, stale); status.Code(err) != codes.Aborted {
		t.Fatalf("stale write: %v", err)
	}
	for _, b := range s.eachBoard() {
		if b.isMining() != (strings.ToLower(b.MAC) == selected) {
			t.Fatalf("pool assignment affected wrong board %s", b.MAC)
		}
	}
	if _, err := mining.StartMining(ctx, &mdk.StartMiningRequest{}); err != nil {
		t.Fatal(err)
	}
	for _, b := range s.eachBoard() {
		if !b.isMining() {
			t.Fatalf("default start omitted %s", b.MAC)
		}
	}
	control("stop")
	for _, b := range s.eachBoard() {
		if b.isMining() != (strings.ToLower(b.MAC) == selected) {
			t.Fatalf("default stop affected hosted board %s", b.MAC)
		}
	}
	if s.miningStatusDTO().Mining {
		t.Fatal("container mining toggle must describe default intent")
	}
	if _, err = client.DeleteMiningGroup(ctx, &mdk.DeleteMiningGroupRequest{Id: created.Group.Id, ExpectedRevision: configured.Group.Revision, IdempotencyKey: "delete-nonempty"}); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("deleted occupied group: %v", err)
	}
	groups, err := client.ListMiningGroups(ctx, &mdk.ListMiningGroupsRequest{})
	if err != nil {
		t.Fatal(err)
	}
	var defaultRevision int64
	for _, g := range groups.Groups {
		if g.Id == "default" {
			defaultRevision = g.Revision
		}
	}
	if _, err = client.RequestMiningGroupTransfer(ctx, &mdk.RequestMiningGroupTransferRequest{SourceGroupId: created.Group.Id, DestinationGroupId: "default", SourceExpectedRevision: configured.Group.Revision, DestinationExpectedRevision: defaultRevision, MacAddresses: []string{selected}, IdempotencyKey: "return"}); err != nil {
		t.Fatal(err)
	}
	if _, err = client.DeleteMiningGroup(ctx, &mdk.DeleteMiningGroupRequest{Id: created.Group.Id, ExpectedRevision: configured.Group.Revision + 1, IdempotencyKey: "delete-empty"}); err != nil {
		t.Fatal(err)
	}
	if s.boardsByMAC[strings.ToUpper(selected)].isMining() {
		t.Fatal("transfer ignored stopped destination intent")
	}
}
