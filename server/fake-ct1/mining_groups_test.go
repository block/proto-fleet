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

func TestDefaultPoolPasswordRotationInvalidatesCopyAndTransfer(t *testing.T) {
	s := NewContainerState(ContainerConfig{ServerID: "groups", Tanks: 1, ModulesPerTank: 1, SlotsPerModule: 1, Seed: 1})
	api := &grpcAPI{state: s}
	ctx := t.Context()
	if _, err := api.StopMining(ctx, &mdk.StopMiningRequest{}); err != nil {
		t.Fatal(err)
	}
	capability, err := api.GetMiningGroupCapability(ctx, &mdk.GetMiningGroupCapabilityRequest{})
	if err != nil || !capability.GetCapability().GetDefaultPoolWritesSupported() {
		t.Fatalf("Default pool writes unavailable: %v", err)
	}
	if _, err := api.ActivateMiningGroups(ctx, &mdk.ActivateMiningGroupsRequest{
		ExpectedStateRevision: capability.Capability.StateRevision, IdempotencyKey: "activate",
	}); err != nil {
		t.Fatal(err)
	}
	getDefault := func() *mdk.MiningGroupRecord {
		t.Helper()
		response, err := api.GetMiningGroup(ctx, &mdk.GetMiningGroupRequest{Id: "default"})
		if err != nil || response.GetGroup().GetDefaultPoolRevision() <= 0 {
			t.Fatalf("Default pool revision unavailable: %v", err)
		}
		return response.Group
	}
	source := getDefault()
	destination, err := api.CreateMiningGroup(ctx, &mdk.CreateMiningGroupRequest{
		Metadata: &mdk.MiningGroupMetadata{Name: "New worker"}, IdempotencyKey: "create",
	})
	if err != nil {
		t.Fatal(err)
	}
	copyRequest := &mdk.CopyMiningGroupPoolsRequest{
		GroupId: destination.Group.Id, ExpectedRevision: destination.Group.Revision,
		SourceGroupId: "default", SourceExpectedRevision: source.Revision, IdempotencyKey: "copy",
	}
	for _, pool := range s.miningGroups.pools["default"] {
		copyRequest.Pools = append(copyRequest.Pools, &mdk.MiningGroupPoolCopy{Source: proto.CloneOf(pool.Pool), Username: "account.worker"})
	}
	if _, err := api.CopyMiningGroupPools(ctx, copyRequest); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("unguarded Default copy: %v", err)
	}
	rotatePassword := func(password string) {
		t.Helper()
		if _, err := api.UpdatePool(ctx, &mdk.UpdatePoolRequest{
			Pool: &mdk.Pool{Id: int64(copyRequest.Pools[0].Source.Id)}, Password: password,
			UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"id"}},
		}); err != nil {
			t.Fatal(err)
		}
	}
	rotatePassword("first-rotation")
	copyRequest.SourceExpectedDefaultPoolRevision = source.DefaultPoolRevision
	if _, err := api.CopyMiningGroupPools(ctx, copyRequest); status.Code(err) != codes.Aborted {
		t.Fatalf("stale Default copy: %v", err)
	}
	copyRequest.SourceExpectedDefaultPoolRevision = getDefault().DefaultPoolRevision
	copied, err := api.CopyMiningGroupPools(ctx, copyRequest)
	if err != nil {
		t.Fatal(err)
	}
	if s.miningGroups.pools[destination.Group.Id][0].GetPassword() != "first-rotation" {
		t.Fatal("copy lost the rotated password")
	}
	rotatePassword("second-rotation")
	members, err := api.ListMiningGroupMemberships(ctx, &mdk.ListMiningGroupMembershipsRequest{})
	if err != nil {
		t.Fatal(err)
	}
	mac := members.Memberships[0].MacAddress
	transfer := &mdk.RequestMiningGroupTransferRequest{
		SourceGroupId: "default", SourceExpectedRevision: source.Revision,
		SourceExpectedDefaultPoolRevision: copyRequest.SourceExpectedDefaultPoolRevision,
		DestinationGroupId:                copied.Group.Id, DestinationExpectedRevision: copied.Group.Revision,
		MacAddresses: []string{mac}, IdempotencyKey: "transfer",
	}
	if _, err := api.RequestMiningGroupTransfer(ctx, transfer); status.Code(err) != codes.Aborted {
		t.Fatalf("stale Default transfer: %v", err)
	}
	if s.miningGroups.members[mac].GroupId != "default" {
		t.Fatal("stale transfer changed board membership")
	}
}

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
	defaultGroup, err := client.GetMiningGroup(ctx, &mdk.GetMiningGroupRequest{Id: "default"})
	if err != nil {
		t.Fatal(err)
	}
	transfer := &mdk.RequestMiningGroupTransferRequest{SourceGroupId: "default", DestinationGroupId: created.Group.Id, SourceExpectedRevision: defaultGroup.GetGroup().GetRevision(), DestinationExpectedRevision: created.Group.Revision, MacAddresses: []string{selected}, IdempotencyKey: "transfer"}
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
		{Pool: &mdk.MiningGroupPoolRecord{Url: "stratum+tcp://primary.example.com:3333", Username: "worker", Enabled: true}, Password: &primary},
		{Pool: &mdk.MiningGroupPoolRecord{Url: "stratum+tcp://backup.example.com:3333", Username: "backup-worker", Priority: 1, Enabled: true}, Password: &backup},
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

func TestMiningGroupPoolClearDiagnostics(t *testing.T) {
	s := NewContainerState(ContainerConfig{ServerID: "clear", Tanks: 1, ModulesPerTank: 1, SlotsPerModule: 3, Seed: 1})
	api := &grpcAPI{state: s}
	ctx := t.Context()
	_, err := api.StopMining(ctx, &mdk.StopMiningRequest{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = api.ActivateMiningGroups(ctx, &mdk.ActivateMiningGroupsRequest{ExpectedStateRevision: 1, IdempotencyKey: "activate"})
	if err != nil {
		t.Fatal(err)
	}
	group, err := api.CreateMiningGroup(ctx, &mdk.CreateMiningGroupRequest{Metadata: &mdk.MiningGroupMetadata{Name: "Cleared"}, IdempotencyKey: "create"})
	if err != nil {
		t.Fatal(err)
	}
	members, err := api.ListMiningGroupMemberships(ctx, &mdk.ListMiningGroupMembershipsRequest{})
	if err != nil {
		t.Fatal(err)
	}
	mac := members.Memberships[0].MacAddress
	defaultGroup, err := api.GetMiningGroup(ctx, &mdk.GetMiningGroupRequest{Id: "default"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = api.RequestMiningGroupTransfer(ctx, &mdk.RequestMiningGroupTransferRequest{SourceGroupId: "default", DestinationGroupId: group.Group.Id, SourceExpectedRevision: defaultGroup.GetGroup().GetRevision(), DestinationExpectedRevision: group.Group.Revision, MacAddresses: []string{mac}, IdempotencyKey: "move"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = api.ReplaceMiningGroupPools(ctx, &mdk.ReplaceMiningGroupPoolsRequest{GroupId: group.Group.Id, ExpectedRevision: 2, IdempotencyKey: "clear"})
	if err != nil {
		t.Fatal(err)
	}
	result, err := api.GetMiningGroupPoolDiagnostics(ctx, &mdk.GetMiningGroupPoolDiagnosticsRequest{GroupId: group.Group.Id})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Pools) != 0 {
		t.Fatal("pools remain")
	}
	if len(result.ObservedMacAddresses) != 1 || result.ObservedMacAddresses[0] != mac {
		t.Fatal(result)
	}
	if s.boardsByMAC[strings.ToUpper(mac)].isMining() {
		t.Fatal("cleared board is mining")
	}
	s.boardsByMAC[strings.ToUpper(mac)].State = miningStateDisconnected
	result, err = api.GetMiningGroupPoolDiagnostics(ctx, &mdk.GetMiningGroupPoolDiagnosticsRequest{GroupId: group.Group.Id})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ObservedMacAddresses) != 0 {
		t.Fatal("disconnected board reported observed")
	}
	_, err = api.GetMiningGroupPoolDiagnostics(ctx, &mdk.GetMiningGroupPoolDiagnosticsRequest{GroupId: "default"})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatal(err)
	}
}

func TestDefaultGroupUsesLegacyMutationAPIs(t *testing.T) {
	s := NewContainerState(ContainerConfig{ServerID: "default-contract", Tanks: 1, ModulesPerTank: 1, SlotsPerModule: 1, Seed: 1})
	api := &grpcAPI{state: s}
	ctx := t.Context()
	if _, err := api.StopMining(ctx, &mdk.StopMiningRequest{}); err != nil {
		t.Fatal(err)
	}
	if _, err := api.ActivateMiningGroups(ctx, &mdk.ActivateMiningGroupsRequest{ExpectedStateRevision: 1, IdempotencyKey: "activate"}); err != nil {
		t.Fatal(err)
	}
	revision := s.miningGroups.groups["default"].Revision
	if _, err := api.UpdateMiningGroup(ctx, &mdk.UpdateMiningGroupRequest{Id: "default", ExpectedRevision: revision, IdempotencyKey: "metadata", Metadata: &mdk.MiningGroupMetadata{Name: "Renamed"}, UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"name"}}}); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("default metadata update: %v", err)
	}
	if _, err := api.ReplaceMiningGroupPools(ctx, &mdk.ReplaceMiningGroupPoolsRequest{GroupId: "default", ExpectedRevision: revision, IdempotencyKey: "pools"}); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("default pool replace: %v", err)
	}
	if _, err := api.UpdateMiningGroupIntent(ctx, &mdk.UpdateMiningGroupIntentRequest{Id: "default", ExpectedRevision: revision, IdempotencyKey: "intent", DesiredMining: true, UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"desired_mining"}}}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("default intent update: %v", err)
	}
}
