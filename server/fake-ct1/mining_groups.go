package main

import (
	"context"
	"fmt"
	"maps"
	"math"
	"slices"
	"strings"
	"unicode"

	mdk "github.com/block/proto-fleet/sdk/mdk/generated/mdk/v2"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// The simulator implements Fleet's native mining-group workflow in memory.
// Transfers of connected boards complete synchronously; it does not simulate
// HA owners, physical rails-off proofs, pool connections, or durable receipts.
type simulatedMiningGroups struct {
	capability *mdk.MiningGroupCapability
	groups     map[string]*mdk.MiningGroupRecord
	pools      map[string][]*mdk.MiningGroupPoolInput
	members    map[string]*mdk.MiningGroupMembershipRecord
	operations map[string]*mdk.MiningGroupOperationRecord
	receipts   map[string]miningGroupReceipt
	sequence   int
}
type miningGroupReceipt struct{ request, response proto.Message }

func (s *ContainerState) initMiningGroups() *simulatedMiningGroups {
	if s.miningGroups == nil {
		now := timestamppb.New(s.now())
		defaultGroup := &mdk.MiningGroupRecord{
			Id: "default", Name: "Default", Revision: 1, DefaultPoolRevision: proto.Int64(s.defaultPoolRevision),
			CreatedAt: now, UpdatedAt: now,
		}
		for _, board := range s.eachBoard() {
			defaultGroup.DesiredMining = defaultGroup.DesiredMining || board.isMining()
		}
		s.miningGroups = &simulatedMiningGroups{
			capability: &mdk.MiningGroupCapability{
				Name: "mining-groups-v1", Version: 1, StateRevision: 1,
				MaxGroups: 289, MaxPoolsPerGroup: 3, MaxBoardsPerGroup: 288,
				PoolCopySupported: true, DefaultPoolWritesSupported: proto.Bool(true),
				DefaultPoolWorkerNameUpdateSupported: proto.Bool(true),
				AssignmentGranularity:                "PHYSICAL_HASHBOARD", SupportedPllAlgorithms: []string{"NONE"},
			},
			groups: map[string]*mdk.MiningGroupRecord{"default": defaultGroup}, pools: map[string][]*mdk.MiningGroupPoolInput{}, members: map[string]*mdk.MiningGroupMembershipRecord{}, operations: map[string]*mdk.MiningGroupOperationRecord{}, receipts: map[string]miningGroupReceipt{},
		}
	}
	return s.miningGroups
}

// Serialize both transports and clone messages so callers cannot mutate state.
// Successful retries replay the original receipt before checking revisions.
func miningGroupRPC[T proto.Message](ctx context.Context, a *grpcAPI, key string, request proto.Message, fn func(*ContainerState, *simulatedMiningGroups) (T, error)) (T, error) {
	var zero T
	if err := ctx.Err(); err != nil {
		return zero, status.FromContextError(err).Err()
	}
	s := a.state
	s.mu.Lock()
	defer s.mu.Unlock()
	g := s.initMiningGroups()
	receiptKey := ""
	if request != nil {
		if strings.TrimSpace(key) == "" {
			return zero, status.Error(codes.InvalidArgument, "idempotency key required")
		}
		receiptKey = string(request.ProtoReflect().Descriptor().FullName()) + ":" + key
		if receipt, ok := g.receipts[receiptKey]; ok {
			if !proto.Equal(receipt.request, request) {
				return zero, status.Error(codes.AlreadyExists, "idempotency key reused with different request")
			}
			return proto.Clone(receipt.response).(T), nil
		}
	}
	response, err := fn(s, g)
	if err != nil {
		return zero, err
	}
	if request != nil {
		g.receipts[receiptKey] = miningGroupReceipt{proto.Clone(request), proto.Clone(response)}
	}
	return proto.Clone(response).(T), nil
}
func (g *simulatedMiningGroups) group(id string, revision int64) (*mdk.MiningGroupRecord, error) {
	if !g.capability.Enabled {
		return nil, status.Error(codes.FailedPrecondition, "mining groups are disabled")
	}
	group := g.groups[id]
	if group == nil {
		return nil, status.Error(codes.NotFound, "unknown mining group")
	}
	if revision != 0 && group.Revision != revision {
		return nil, status.Error(codes.Aborted, "mining group revision changed")
	}
	return group, nil
}

func checkDefaultPoolRevision(source *mdk.MiningGroupRecord, expected *int64) error {
	if expected == nil {
		return nil
	}
	if source.GetId() != "default" || *expected <= 0 {
		return status.Error(codes.InvalidArgument, "a positive Default pool revision requires a Default source")
	}
	if source.GetDefaultPoolRevision() != *expected {
		return status.Error(codes.Aborted, "Default pool settings changed")
	}
	return nil
}

func (s *ContainerState) touchGroup(group *mdk.MiningGroupRecord) {
	group.Revision++
	group.UpdatedAt = timestamppb.New(s.now())
}
func (s *ContainerState) applyGroupMining(id string) {
	g := s.miningGroups
	mining := g.groups[id].DesiredMining && !g.capability.Suspended
	hasPool := false
	for _, p := range g.pools[id] {
		hasPool = hasPool || p.Pool.Enabled
	}
	for mac, m := range g.members {
		if m.GroupId != id {
			continue
		}
		b := s.boardsByMAC[strings.ToUpper(mac)]
		if b == nil || b.Isolated || b.State == miningStateDisconnected {
			continue
		}
		b.State = miningStateIdle
		if mining && hasPool && b.canMine() {
			b.State = miningStateMining
		}
	}
}
func (a *grpcAPI) GetMiningGroupCapability(ctx context.Context, _ *mdk.GetMiningGroupCapabilityRequest) (*mdk.GetMiningGroupCapabilityResponse, error) {
	return miningGroupRPC(ctx, a, "", nil, func(_ *ContainerState, g *simulatedMiningGroups) (*mdk.GetMiningGroupCapabilityResponse, error) {
		return &mdk.GetMiningGroupCapabilityResponse{Capability: g.capability}, nil
	})
}

func (a *grpcAPI) GetMiningGroup(ctx context.Context, request *mdk.GetMiningGroupRequest) (*mdk.GetMiningGroupResponse, error) {
	return miningGroupRPC(ctx, a, "", nil, func(_ *ContainerState, groups *simulatedMiningGroups) (*mdk.GetMiningGroupResponse, error) {
		group := groups.groups[request.GetId()]
		if group == nil {
			return nil, status.Error(codes.NotFound, "unknown mining group")
		}
		return &mdk.GetMiningGroupResponse{Group: group}, nil
	})
}

func (a *grpcAPI) DeactivateMiningGroups(ctx context.Context, r *mdk.DeactivateMiningGroupsRequest) (*mdk.DeactivateMiningGroupsResponse, error) {
	return miningGroupRPC(ctx, a, r.IdempotencyKey, r, func(s *ContainerState, g *simulatedMiningGroups) (*mdk.DeactivateMiningGroupsResponse, error) {
		if r.ExpectedStateRevision != g.capability.StateRevision {
			return nil, status.Error(codes.Aborted, "mining group state changed")
		}
		for _, member := range g.members {
			if member.GroupId != "default" {
				return nil, status.Error(codes.FailedPrecondition, "mining groups are still in use")
			}
		}
		for _, operation := range g.operations {
			if operation.Phase != "COMPLETE" && operation.Phase != "FAILED" {
				return nil, status.Error(codes.FailedPrecondition, "mining group operations are unfinished")
			}
		}
		if g.capability.Enabled {
			// Legacy mode takes over the current default policy. Keep board states
			// unchanged so disabling groups does not start or stop mining.
			s.pools = make(map[int64]simulatedPool, len(g.pools["default"]))
			for _, input := range g.pools["default"] {
				p := proto.CloneOf(input.Pool)
				id := int64(p.Id)
				s.pools[id] = simulatedPool{pool: &mdk.Pool{Id: id, Name: p.Name, Url: p.Url, Username: p.Username, Priority: p.Priority, Enabled: p.Enabled, ShareWeight: p.ShareWeight}, password: input.GetPassword()}
				s.nextPoolID = max(s.nextPoolID, id)
			}
			defaultGroup := g.groups["default"]
			clear(g.groups)
			g.groups["default"] = defaultGroup
			clear(g.pools)
			clear(g.members)
			// Retain operation history, receipts and sequence numbers across modes.
		}
		g.capability.Enabled = false
		g.capability.Suspended = false
		g.capability.StateRevision++
		return &mdk.DeactivateMiningGroupsResponse{Capability: g.capability}, nil
	})
}
func (a *grpcAPI) ActivateMiningGroups(ctx context.Context, r *mdk.ActivateMiningGroupsRequest) (*mdk.ActivateMiningGroupsResponse, error) {
	return miningGroupRPC(ctx, a, r.IdempotencyKey, r, func(s *ContainerState, g *simulatedMiningGroups) (*mdk.ActivateMiningGroupsResponse, error) {
		if r.ExpectedStateRevision != g.capability.StateRevision {
			return nil, status.Error(codes.Aborted, "mining group state changed")
		}
		if g.capability.Enabled {
			return nil, status.Error(codes.FailedPrecondition, "mining groups already enabled")
		}
		if g.groups["default"].DesiredMining {
			return nil, status.Error(codes.FailedPrecondition, "stop self mining before activating groups")
		}
		for _, b := range s.eachBoard() {
			if b.isMining() {
				return nil, status.Error(codes.FailedPrecondition, "stop self mining before activating groups")
			}
			if b.Isolated {
				return nil, status.Error(codes.FailedPrecondition, "release isolated boards before activation")
			}
		}
		now := timestamppb.New(s.now())
		for _, p := range s.storedPools() {
			password := s.pools[p.Id].password
			g.pools["default"] = append(g.pools["default"], &mdk.MiningGroupPoolInput{Pool: &mdk.MiningGroupPoolRecord{Id: int32(p.Id), Name: p.Name, Url: p.Url, Username: p.Username, Priority: p.Priority, Enabled: p.Enabled, ShareWeight: p.ShareWeight}, Password: &password})
		}
		for _, b := range s.eachBoard() {
			g.members[strings.ToLower(b.MAC)] = &mdk.MiningGroupMembershipRecord{MacAddress: strings.ToLower(b.MAC), GroupId: "default", Generation: 1, AssignedAt: now}
		}
		g.capability.Enabled = true
		g.capability.StateRevision++
		return &mdk.ActivateMiningGroupsResponse{Capability: g.capability}, nil
	})
}
func (a *grpcAPI) ListMiningGroups(ctx context.Context, _ *mdk.ListMiningGroupsRequest) (*mdk.ListMiningGroupsResponse, error) {
	return miningGroupRPC(ctx, a, "", nil, func(_ *ContainerState, g *simulatedMiningGroups) (*mdk.ListMiningGroupsResponse, error) {
		out := &mdk.ListMiningGroupsResponse{}
		for _, group := range g.groups {
			out.Groups = append(out.Groups, group)
		}
		slices.SortFunc(out.Groups, func(a, b *mdk.MiningGroupRecord) int { return strings.Compare(a.Id, b.Id) })
		return out, nil
	})
}
func (a *grpcAPI) ListMiningGroupMemberships(ctx context.Context, r *mdk.ListMiningGroupMembershipsRequest) (*mdk.ListMiningGroupMembershipsResponse, error) {
	return miningGroupRPC(ctx, a, "", nil, func(_ *ContainerState, g *simulatedMiningGroups) (*mdk.ListMiningGroupMembershipsResponse, error) {
		out := &mdk.ListMiningGroupMembershipsResponse{}
		for _, m := range g.members {
			if r.GetGroupId() == "" || m.GroupId == r.GetGroupId() {
				out.Memberships = append(out.Memberships, m)
			}
		}
		slices.SortFunc(out.Memberships, func(a, b *mdk.MiningGroupMembershipRecord) int { return strings.Compare(a.MacAddress, b.MacAddress) })
		return out, nil
	})
}
func (a *grpcAPI) CreateMiningGroup(ctx context.Context, r *mdk.CreateMiningGroupRequest) (*mdk.CreateMiningGroupResponse, error) {
	return miningGroupRPC(ctx, a, r.IdempotencyKey, r, func(s *ContainerState, g *simulatedMiningGroups) (*mdk.CreateMiningGroupResponse, error) {
		if !g.capability.Enabled {
			return nil, status.Error(codes.FailedPrecondition, "mining groups are disabled")
		}
		if strings.TrimSpace(r.GetMetadata().GetName()) == "" {
			return nil, status.Error(codes.InvalidArgument, "group name required")
		}
		if len(g.groups) >= int(g.capability.MaxGroups) {
			return nil, status.Error(codes.ResourceExhausted, "mining group limit reached")
		}
		g.sequence++
		id := fmt.Sprintf("group-%d", g.sequence)
		now := timestamppb.New(s.now())
		group := &mdk.MiningGroupRecord{Id: id, Name: r.Metadata.Name, ExternalRef: r.Metadata.ExternalRef, Revision: 1, CreatedAt: now, UpdatedAt: now}
		g.groups[id] = group
		return &mdk.CreateMiningGroupResponse{Group: group}, nil
	})
}
func (a *grpcAPI) UpdateMiningGroup(ctx context.Context, r *mdk.UpdateMiningGroupRequest) (*mdk.UpdateMiningGroupResponse, error) {
	return miningGroupRPC(ctx, a, r.IdempotencyKey, r, func(s *ContainerState, g *simulatedMiningGroups) (*mdk.UpdateMiningGroupResponse, error) {
		if r.Id == "default" {
			return nil, status.Error(codes.FailedPrecondition, "cannot update default group metadata")
		}
		if r.ExpectedRevision <= 0 || len(r.GetUpdateMask().GetPaths()) == 0 {
			return nil, status.Error(codes.InvalidArgument, "group revision and metadata mask required")
		}
		for _, path := range r.UpdateMask.Paths {
			switch path {
			case "name":
				if strings.TrimSpace(r.GetMetadata().GetName()) == "" {
					return nil, status.Error(codes.InvalidArgument, "group name required")
				}
			case "external_ref":
			default:
				return nil, status.Error(codes.InvalidArgument, "unsupported metadata field")
			}
		}
		group, err := g.group(r.Id, r.ExpectedRevision)
		if err != nil {
			return nil, err
		}
		for _, path := range r.UpdateMask.Paths {
			switch path {
			case "name":
				group.Name = r.Metadata.Name
			case "external_ref":
				if r.Metadata == nil {
					group.ExternalRef = nil
				} else {
					group.ExternalRef = r.Metadata.ExternalRef
				}
			}
		}
		s.touchGroup(group)
		return &mdk.UpdateMiningGroupResponse{Group: group}, nil
	})
}

func (a *grpcAPI) ListMiningGroupPools(ctx context.Context, r *mdk.ListMiningGroupPoolsRequest) (*mdk.ListMiningGroupPoolsResponse, error) {
	return miningGroupRPC(ctx, a, "", nil, func(_ *ContainerState, g *simulatedMiningGroups) (*mdk.ListMiningGroupPoolsResponse, error) {
		if _, err := g.group(r.GroupId, 0); err != nil {
			return nil, err
		}
		out := &mdk.ListMiningGroupPoolsResponse{}
		if r.GroupId == "default" {
			return out, nil // This API reads hosted pools. Default uses legacy PoolService.
		}
		for _, p := range g.pools[r.GroupId] {
			out.Pools = append(out.Pools, p.Pool)
		}
		return out, nil
	})
}
func (a *grpcAPI) ReplaceMiningGroupPools(ctx context.Context, r *mdk.ReplaceMiningGroupPoolsRequest) (*mdk.ReplaceMiningGroupPoolsResponse, error) {
	return miningGroupRPC(ctx, a, r.IdempotencyKey, r, func(s *ContainerState, g *simulatedMiningGroups) (*mdk.ReplaceMiningGroupPoolsResponse, error) {
		if r.GroupId == "default" {
			return nil, status.Error(codes.FailedPrecondition, "use legacy pool operations for the default group")
		}
		group, err := g.group(r.GroupId, r.ExpectedRevision)
		if err != nil {
			return nil, err
		}
		if r.ExpectedRevision <= 0 || len(r.Pools) > 3 {
			return nil, status.Error(codes.InvalidArgument, "positive revision and at most three pools required")
		}
		ids := map[int32]bool{}
		nextID := s.nextPoolID
		pools := make([]*mdk.MiningGroupPoolInput, 0, len(r.Pools))
		out := &mdk.ReplaceMiningGroupPoolsResponse{Group: group}
		for _, input := range r.Pools {
			p := input.GetPool()
			if p == nil || p.Id < 0 || (p.Id > 0 && ids[p.Id]) || strings.TrimSpace(p.Url) == "" {
				return nil, status.Error(codes.InvalidArgument, "invalid or duplicate pool")
			}
			copy := proto.Clone(input).(*mdk.MiningGroupPoolInput)
			if p.Id == 0 {
				nextID++
				copy.Pool.Id = int32(nextID)
			} else {
				found := false
				for _, old := range g.pools[group.Id] {
					found = found || old.Pool.Id == p.Id
				}
				if !found {
					return nil, status.Error(codes.InvalidArgument, "pool ID is outside this group")
				}
			}
			ids[copy.Pool.Id] = true
			if copy.Password == nil {
				for _, old := range g.pools[group.Id] {
					if old.Pool.Id == p.Id {
						copy.Password = old.Password
					}
				}
			}
			pools = append(pools, copy)
			out.Pools = append(out.Pools, copy.Pool)
		}
		g.pools[group.Id] = pools
		s.nextPoolID = nextID
		s.touchGroup(group)
		s.applyGroupMining(group.Id)
		return out, nil
	})
}
func (a *grpcAPI) CopyMiningGroupPools(ctx context.Context, r *mdk.CopyMiningGroupPoolsRequest) (*mdk.CopyMiningGroupPoolsResponse, error) {
	return miningGroupRPC(ctx, a, r.GetIdempotencyKey(), r, func(s *ContainerState, g *simulatedMiningGroups) (*mdk.CopyMiningGroupPoolsResponse, error) {
		if r.GroupId == "default" || r.GroupId == r.SourceGroupId || r.ExpectedRevision <= 0 ||
			r.SourceExpectedRevision <= 0 || len(r.Pools) == 0 || len(r.Pools) > int(g.capability.MaxPoolsPerGroup) {
			return nil, status.Error(codes.InvalidArgument, "distinct groups, positive revisions and reviewed pools required")
		}
		group, err := g.group(r.GroupId, r.ExpectedRevision)
		if err != nil {
			return nil, err
		}
		sourceGroup, err := g.group(r.SourceGroupId, r.SourceExpectedRevision)
		if err != nil {
			return nil, err
		}
		if sourceGroup.Id == "default" && r.SourceExpectedDefaultPoolRevision == nil {
			return nil, status.Error(codes.InvalidArgument, "Default source pool revision required")
		}
		if err := checkDefaultPoolRevision(sourceGroup, r.SourceExpectedDefaultPoolRevision); err != nil {
			return nil, err
		}
		if len(g.pools[group.Id]) != 0 {
			return nil, status.Error(codes.FailedPrecondition, "destination already has pools")
		}
		for _, member := range g.members {
			if member.GroupId == group.Id {
				return nil, status.Error(codes.FailedPrecondition, "destination already has boards")
			}
		}
		// Transfers in this simulator complete synchronously.
		source := g.pools[r.SourceGroupId]
		if len(source) != len(r.Pools) {
			return nil, status.Error(codes.Aborted, "source pools changed")
		}
		reviewed := make(map[int32]*mdk.MiningGroupPoolCopy, len(r.Pools))
		for _, item := range r.Pools {
			id := item.GetSource().GetId()
			if id <= 0 || reviewed[id] != nil || strings.TrimSpace(item.GetUsername()) == "" {
				return nil, status.Error(codes.InvalidArgument, "distinct reviewed pools and usernames required")
			}
			reviewed[id] = item
		}
		nextID := s.nextPoolID
		pools := make([]*mdk.MiningGroupPoolInput, 0, len(source))
		out := &mdk.CopyMiningGroupPoolsResponse{Group: group}
		for _, saved := range source {
			item := reviewed[saved.Pool.Id]
			if !proto.Equal(saved.Pool, item.GetSource()) {
				return nil, status.Error(codes.Aborted, "source pools changed")
			}
			copied := proto.CloneOf(saved)
			nextID++
			copied.Pool.Id = int32(nextID)
			copied.Pool.Username = item.GetUsername()
			pools = append(pools, copied)
			out.Pools = append(out.Pools, copied.Pool)
		}
		g.pools[group.Id] = pools
		s.nextPoolID = nextID
		s.touchGroup(group)
		return out, nil
	})
}

func (a *grpcAPI) UpdateDefaultMiningGroupWorkerNames(ctx context.Context, r *mdk.UpdateDefaultMiningGroupWorkerNamesRequest) (*mdk.UpdateDefaultMiningGroupWorkerNamesResponse, error) {
	return miningGroupRPC(ctx, a, r.IdempotencyKey, r, func(s *ContainerState, g *simulatedMiningGroups) (*mdk.UpdateDefaultMiningGroupWorkerNamesResponse, error) {
		if r.ExpectedRevision <= 0 || r.ExpectedDefaultPoolRevision <= 0 || len(r.Updates) == 0 || len(r.Updates) > 48 {
			return nil, status.Error(codes.InvalidArgument, "positive revisions and one to 48 worker name updates required")
		}
		group, err := g.group("default", r.ExpectedRevision)
		if err != nil {
			return nil, err
		}
		if g.capability.Suspended {
			return nil, status.Error(codes.FailedPrecondition, "mining groups are suspended")
		}
		if err := checkDefaultPoolRevision(group, &r.ExpectedDefaultPoolRevision); err != nil {
			return nil, err
		}
		for _, operation := range g.operations {
			if (operation.SourceGroupId == group.Id || operation.DestinationGroupId == group.Id) &&
				operation.Phase != "COMPLETE" && operation.Phase != "FAILED" {
				return nil, status.Error(codes.FailedPrecondition, "Default has an unfinished mining group operation")
			}
		}

		updates := make(map[int64]simulatedPool, len(r.Updates))
		var changed int64
		for _, update := range r.Updates {
			id, username := update.GetPoolId(), update.GetUsername()
			if id <= 0 || id > math.MaxInt32 || len(username) == 0 || len(username) > 255 ||
				strings.ContainsFunc(username, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) || r == '\uFEFF' }) {
				return nil, status.Error(codes.InvalidArgument, "positive pool IDs and nonblank usernames without spaces are required")
			}
			if _, duplicate := updates[id]; duplicate {
				return nil, status.Error(codes.InvalidArgument, "worker name updates require distinct pool IDs")
			}
			saved, exists := s.pools[id]
			if !exists {
				return nil, status.Error(codes.InvalidArgument, "unknown Default pool ID")
			}
			if saved.pool.Username != username {
				saved.pool = proto.CloneOf(saved.pool)
				saved.pool.Username = username
				changed++
			}
			updates[id] = saved
		}

		// Check the resulting enabled pools together, so a username swap remains valid.
		enabled := make([]*mdk.Pool, 0, len(s.pools))
		for id, saved := range s.pools {
			if updated, exists := updates[id]; exists {
				saved = updated
			}
			if saved.pool.Enabled {
				enabled = append(enabled, saved.pool)
			}
		}
		if hasConflictingEnabledPools(enabled) {
			return nil, status.Error(codes.InvalidArgument, "enabled pool URL and priority must be unique")
		}
		if changed > 0 {
			maps.Copy(s.pools, updates)
			s.syncDefaultPools(changed)
		}
		return &mdk.UpdateDefaultMiningGroupWorkerNamesResponse{Group: group, Pools: s.storedPools()}, nil
	})
}

func (a *grpcAPI) UpdateMiningGroupIntent(ctx context.Context, r *mdk.UpdateMiningGroupIntentRequest) (*mdk.UpdateMiningGroupIntentResponse, error) {
	return miningGroupRPC(ctx, a, r.IdempotencyKey, r, func(s *ContainerState, g *simulatedMiningGroups) (*mdk.UpdateMiningGroupIntentResponse, error) {
		if r.Id == "default" {
			return nil, status.Error(codes.InvalidArgument, "use legacy mining controls for the default group")
		}
		group, err := g.group(r.Id, r.ExpectedRevision)
		if err != nil {
			return nil, err
		}
		if r.ExpectedRevision <= 0 || !slices.Equal(r.GetUpdateMask().GetPaths(), []string{"desired_mining"}) {
			return nil, status.Error(codes.InvalidArgument, "simulator supports only the desired_mining intent mask")
		}
		group.DesiredMining = r.DesiredMining
		s.touchGroup(group)
		s.applyGroupMining(group.Id)
		return &mdk.UpdateMiningGroupIntentResponse{Group: group}, nil
	})
}
func (a *grpcAPI) RequestMiningGroupTransfer(ctx context.Context, r *mdk.RequestMiningGroupTransferRequest) (*mdk.RequestMiningGroupTransferResponse, error) {
	return miningGroupRPC(ctx, a, r.IdempotencyKey, r, func(s *ContainerState, g *simulatedMiningGroups) (*mdk.RequestMiningGroupTransferResponse, error) {
		destination, err := g.group(r.DestinationGroupId, r.DestinationExpectedRevision)
		if err != nil {
			return nil, err
		}
		if r.DestinationExpectedRevision <= 0 || r.SourceGroupId == r.DestinationGroupId || len(r.MacAddresses) == 0 || len(r.MacAddresses) > 288 {
			return nil, status.Error(codes.InvalidArgument, "invalid transfer")
		}
		var source *mdk.MiningGroupRecord
		if r.SourceGroupId != "" {
			source, err = g.group(r.SourceGroupId, r.SourceExpectedRevision)
			if err != nil {
				return nil, err
			}
			if r.SourceExpectedRevision <= 0 {
				return nil, status.Error(codes.InvalidArgument, "source revision required")
			}
		}
		if err := checkDefaultPoolRevision(source, r.SourceExpectedDefaultPoolRevision); err != nil {
			return nil, err
		}
		seen := map[string]bool{}
		for _, mac := range r.MacAddresses {
			b := s.boardsByMAC[strings.ToUpper(mac)]
			if b == nil || seen[mac] || mac != strings.ToLower(mac) {
				return nil, status.Error(codes.InvalidArgument, "unknown or duplicate board")
			}
			seen[mac] = true
			if g.members[mac].GetGroupId() != r.SourceGroupId {
				return nil, status.Error(codes.FailedPrecondition, "board no longer belongs to source")
			}
			if b.Isolated || b.State == miningStateDisconnected {
				return nil, status.Error(codes.FailedPrecondition, "simulator cannot confirm stop for an isolated or disconnected board")
			}
		}
		now := timestamppb.New(s.now())
		for _, mac := range r.MacAddresses {
			s.boardsByMAC[strings.ToUpper(mac)].State = miningStateIdle
			g.members[mac] = &mdk.MiningGroupMembershipRecord{MacAddress: mac, GroupId: destination.Id, Generation: g.members[mac].GetGeneration() + 1, AssignedAt: now}
		}
		if source != nil {
			s.touchGroup(source)
		}
		s.touchGroup(destination)
		s.applyGroupMining(destination.Id)
		g.sequence++
		operation := &mdk.MiningGroupOperationRecord{Id: fmt.Sprintf("transfer-%d", g.sequence), Kind: "TRANSFER", Phase: "COMPLETE", SourceGroupId: r.SourceGroupId, DestinationGroupId: r.DestinationGroupId, MacAddresses: slices.Clone(r.MacAddresses), RequestedAt: now, UpdatedAt: now, StopBoardsTotal: uint32(len(r.MacAddresses)), StopBoardsConfirmed: uint32(len(r.MacAddresses))}
		g.operations[operation.Id] = operation
		return &mdk.RequestMiningGroupTransferResponse{Operation: operation}, nil
	})
}
func (a *grpcAPI) GetMiningGroupOperation(ctx context.Context, r *mdk.GetMiningGroupOperationRequest) (*mdk.GetMiningGroupOperationResponse, error) {
	return miningGroupRPC(ctx, a, "", nil, func(_ *ContainerState, g *simulatedMiningGroups) (*mdk.GetMiningGroupOperationResponse, error) {
		operation := g.operations[r.Id]
		if operation == nil {
			return nil, status.Error(codes.NotFound, "unknown mining group operation")
		}
		return &mdk.GetMiningGroupOperationResponse{Operation: operation}, nil
	})
}
func (a *grpcAPI) DeleteMiningGroup(ctx context.Context, r *mdk.DeleteMiningGroupRequest) (*mdk.DeleteMiningGroupResponse, error) {
	return miningGroupRPC(ctx, a, r.IdempotencyKey, r, func(_ *ContainerState, g *simulatedMiningGroups) (*mdk.DeleteMiningGroupResponse, error) {
		group, err := g.group(r.Id, r.ExpectedRevision)
		if err != nil {
			return nil, err
		}
		if r.ExpectedRevision <= 0 {
			return nil, status.Error(codes.InvalidArgument, "group revision required")
		}
		if group.Id == "default" {
			return nil, status.Error(codes.FailedPrecondition, "cannot delete default group")
		}
		for _, m := range g.members {
			if m.GroupId == group.Id {
				return nil, status.Error(codes.FailedPrecondition, "group still owns boards")
			}
		}
		delete(g.groups, group.Id)
		delete(g.pools, group.Id)
		return &mdk.DeleteMiningGroupResponse{}, nil
	})
}

// Runtime group diagnostics cover only locally connected boards. Empty pool
// records alone must not certify a missing board as cleared.
func (a *grpcAPI) GetMiningGroupPoolDiagnostics(ctx context.Context, r *mdk.GetMiningGroupPoolDiagnosticsRequest) (*mdk.GetMiningGroupPoolDiagnosticsResponse, error) {
	return miningGroupRPC(ctx, a, "", nil, func(s *ContainerState, g *simulatedMiningGroups) (*mdk.GetMiningGroupPoolDiagnosticsResponse, error) {
		if r.GroupId == "default" {
			return nil, status.Error(codes.InvalidArgument, "hosted group ID is required")
		}
		if _, err := g.group(r.GroupId, 0); err != nil {
			return nil, err
		}
		out := &mdk.GetMiningGroupPoolDiagnosticsResponse{ObservedAt: timestamppb.New(s.now())}
		for mac, member := range g.members {
			board := s.boardsByMAC[strings.ToUpper(mac)]
			if member.GroupId == r.GroupId && board != nil && board.State != miningStateDisconnected {
				out.ObservedMacAddresses = append(out.ObservedMacAddresses, mac)
			}
		}
		slices.Sort(out.ObservedMacAddresses)
		for _, p := range g.pools[r.GroupId] {
			out.Pools = append(out.Pools, &mdk.PoolConnectionDiagnostics{PoolId: int64(p.Pool.Id), PoolName: p.Pool.Name})
		}
		return out, nil
	})
}
