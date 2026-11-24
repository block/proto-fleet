package main

import (
	"cmp"
	"context"
	"math"
	"slices"

	mdk "github.com/block/proto-fleet/sdk/mdk/generated/mdk/v2"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type simulatedPool struct {
	pool     *mdk.Pool
	password string
}

func (s *ContainerState) initPools() {
	s.pools = make(map[int64]simulatedPool)
	s.defaultPoolRevision = 1
	for _, p := range demoPools() {
		id := int64(p.ID)
		s.pools[id] = simulatedPool{pool: &mdk.Pool{Id: id, Name: p.Name, Url: p.URL, Username: p.Username, Priority: p.Priority, Enabled: p.Enabled}}
		s.nextPoolID = id
	}
}

// Call with the state lock held; callers receive copies without credentials.
func (s *ContainerState) storedPools() []*mdk.Pool {
	pools := make([]*mdk.Pool, 0, len(s.pools))
	for _, p := range s.pools {
		pools = append(pools, proto.CloneOf(p.pool))
	}
	slices.SortFunc(pools, func(a, b *mdk.Pool) int { return cmp.Or(cmp.Compare(a.Priority, b.Priority), cmp.Compare(a.Id, b.Id)) })
	return pools
}

func (s *ContainerState) poolDTOs() []poolDTO {
	pools := make([]poolDTO, 0, len(s.pools))
	for _, p := range s.storedPools() {
		pools = append(pools, poolDTO{ID: int64str(p.Id), Name: p.Name, URL: p.Url, Username: p.Username, Priority: p.Priority, Enabled: p.Enabled})
	}
	return pools
}

func (s *ContainerState) validatePoolWrite(p *mdk.Pool) error {
	if p == nil || p.Name == "" || p.Url == "" || p.Username == "" || p.Priority < 0 {
		return status.Error(codes.InvalidArgument, "pool name, URL, username and nonnegative priority required")
	}
	if p.ShareWeight != nil && (*p.ShareWeight <= 0 || math.IsNaN(*p.ShareWeight) || math.IsInf(*p.ShareWeight, 0)) {
		return status.Error(codes.InvalidArgument, "pool weight must be finite and positive")
	}
	enabled := make([]*mdk.Pool, 0, len(s.pools)+1)
	if p.Enabled {
		enabled = append(enabled, p)
	}
	for id, existing := range s.pools {
		if id == p.Id {
			continue
		}
		if existing.pool.Name == p.Name {
			return status.Error(codes.AlreadyExists, "pool name must be unique")
		}
		if existing.pool.Enabled {
			enabled = append(enabled, existing.pool)
		}
	}
	if hasConflictingEnabledPools(enabled) {
		return status.Error(codes.AlreadyExists, "enabled pool URL and priority must be unique")
	}
	return nil
}

func hasConflictingEnabledPools(enabled []*mdk.Pool) bool {
	weighted := slices.ContainsFunc(enabled, func(pool *mdk.Pool) bool { return pool.ShareWeight != nil })
	type identity struct{ url, username string }
	identities := make(map[identity]bool, len(enabled))
	priorities := make(map[int32]bool, len(enabled))
	for _, pool := range enabled {
		key := identity{url: pool.Url}
		if weighted {
			key.username = pool.Username
		}
		if identities[key] || priorities[pool.Priority] {
			return true
		}
		identities[key] = true
		priorities[pool.Priority] = true
	}
	return false
}

// Legacy pool CRUD edits the built-in Default configuration, including in group mode.
func (s *ContainerState) syncDefaultPools(revisionDelta int64) {
	s.defaultPoolRevision += revisionDelta
	if s.miningGroups == nil {
		return
	}
	s.miningGroups.groups["default"].DefaultPoolRevision = proto.Int64(s.defaultPoolRevision)
	if !s.miningGroups.capability.Enabled {
		return
	}
	var pools []*mdk.MiningGroupPoolInput
	for _, p := range s.storedPools() {
		password := s.pools[p.Id].password
		pools = append(pools, &mdk.MiningGroupPoolInput{Pool: &mdk.MiningGroupPoolRecord{
			Id: int32(p.Id), Name: p.Name, Url: p.Url, Username: p.Username,
			Priority: p.Priority, Enabled: p.Enabled, ShareWeight: p.ShareWeight,
		}, Password: &password})
	}
	s.miningGroups.pools["default"] = pools
	s.applyGroupMining("default")
}

func (a *grpcAPI) ListPools(context.Context, *mdk.ListPoolsRequest) (*mdk.ListPoolsResponse, error) {
	a.state.mu.RLock()
	defer a.state.mu.RUnlock()
	return &mdk.ListPoolsResponse{Pools: a.state.storedPools()}, nil
}

func (a *grpcAPI) CreatePool(ctx context.Context, in *mdk.CreatePoolRequest) (*mdk.CreatePoolResponse, error) {
	if err := ctx.Err(); err != nil {
		return nil, status.FromContextError(err).Err()
	}
	s := a.state
	s.mu.Lock()
	defer s.mu.Unlock()
	p := proto.CloneOf(in.GetPool())
	if p != nil {
		p.Id = 0
	}
	if err := s.validatePoolWrite(p); err != nil {
		return nil, err
	}
	s.nextPoolID++
	p.Id = s.nextPoolID
	p.CreatedAt = timestamppb.New(s.now())
	s.pools[p.Id] = simulatedPool{pool: p, password: in.Password}
	s.syncDefaultPools(1)
	return &mdk.CreatePoolResponse{Pool: proto.CloneOf(p)}, nil
}

func (a *grpcAPI) UpdatePool(ctx context.Context, in *mdk.UpdatePoolRequest) (*mdk.UpdatePoolResponse, error) {
	if err := ctx.Err(); err != nil {
		return nil, status.FromContextError(err).Err()
	}
	s := a.state
	s.mu.Lock()
	defer s.mu.Unlock()
	old, ok := s.pools[in.GetPool().GetId()]
	if !ok {
		return nil, status.Error(codes.NotFound, "pool not found")
	}
	p := proto.CloneOf(old.pool)
	paths := in.GetUpdateMask().GetPaths()
	// Match the pinned firmware's legacy full-record update masks.
	if len(paths) == 0 || (len(paths) == 1 && paths[0] == "share_weight") {
		paths = []string{"name", "url", "username", "priority", "enabled"}
		if in.Pool.ShareWeight != nil || len(in.GetUpdateMask().GetPaths()) != 0 {
			paths = append(paths, "share_weight")
		}
	}
	for _, path := range paths {
		switch path {
		case "id":
		case "name":
			if in.Pool.Name != "" {
				p.Name = in.Pool.Name
			}
		case "url":
			p.Url = in.Pool.Url
		case "username":
			p.Username = in.Pool.Username
		case "priority":
			p.Priority = in.Pool.Priority
		case "enabled":
			p.Enabled = in.Pool.Enabled
		case "share_weight":
			p.ShareWeight = in.Pool.ShareWeight
		default:
			return nil, status.Error(codes.InvalidArgument, "unknown pool update field")
		}
	}
	if err := s.validatePoolWrite(p); err != nil {
		return nil, err
	}
	password := old.password
	if in.Password != "" {
		password = in.Password
	}
	s.pools[p.Id] = simulatedPool{pool: p, password: password}
	s.syncDefaultPools(1)
	return &mdk.UpdatePoolResponse{Pool: proto.CloneOf(p)}, nil
}

func (a *grpcAPI) DeletePool(ctx context.Context, in *mdk.DeletePoolRequest) (*mdk.DeletePoolResponse, error) {
	if err := ctx.Err(); err != nil {
		return nil, status.FromContextError(err).Err()
	}
	s := a.state
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.pools[in.GetId()]; !ok {
		return nil, status.Error(codes.NotFound, "pool not found")
	}
	delete(s.pools, in.Id)
	s.syncDefaultPools(1)
	return &mdk.DeletePoolResponse{}, nil
}
