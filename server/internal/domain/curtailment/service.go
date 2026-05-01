package curtailment

import (
	"context"
	"time"

	capabilitiespb "github.com/block/proto-fleet/server/generated/grpc/capabilities/v1"
	pb "github.com/block/proto-fleet/server/generated/grpc/curtailment/v1"
	pairingpb "github.com/block/proto-fleet/server/generated/grpc/pairing/v1"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
)

const (
	defaultCandidateMinPowerW = 1500
	defaultPostEventCooldown  = 10 * time.Minute
)

type CapabilitiesProvider interface {
	GetMinerCapabilitiesForDevice(ctx context.Context, device *pairingpb.Device) *capabilitiespb.MinerCapabilities
}

type Config struct {
	CandidateMinPowerW float64       `help:"Minimum current and recent average power for a miner to be eligible for curtailment preview." default:"1500" env:"CANDIDATE_MIN_POWER_W"`
	PostEventCooldown  time.Duration `help:"Cooldown after a miner resolves or restore-fails before normal-priority previews can select it again." default:"10m" env:"POST_EVENT_COOLDOWN"`
}

type Service struct {
	store                interfaces.CurtailmentStore
	capabilitiesProvider CapabilitiesProvider
	config               Config
	now                  func() time.Time
}

func NewService(store interfaces.CurtailmentStore, capabilitiesProvider CapabilitiesProvider, config Config) *Service {
	return &Service{
		store:                store,
		capabilitiesProvider: capabilitiesProvider,
		config:               config.withDefaults(),
		now:                  time.Now,
	}
}

func (c Config) withDefaults() Config {
	if c.CandidateMinPowerW <= 0 {
		c.CandidateMinPowerW = defaultCandidateMinPowerW
	}
	if c.PostEventCooldown <= 0 {
		c.PostEventCooldown = defaultPostEventCooldown
	}
	return c
}

func (s *Service) PreviewCurtailmentPlan(ctx context.Context, req *pb.PreviewCurtailmentPlanRequest) (*pb.PreviewCurtailmentPlanResponse, error) {
	if req == nil {
		return nil, fleeterror.NewInvalidArgumentError("preview request is required")
	}
	if s.store == nil {
		return nil, fleeterror.NewInternalError("curtailment store is not configured")
	}

	info, err := session.GetInfo(ctx)
	if err != nil {
		return nil, err
	}

	normalized, err := normalizePreviewRequest(req)
	if err != nil {
		return nil, err
	}
	if err := s.ensureDeviceSetsResolved(ctx, info.OrganizationID, normalized); err != nil {
		return nil, err
	}

	params, requestedIdentifiers, err := normalized.storeParams(info.OrganizationID, s.now().Add(-s.config.PostEventCooldown))
	if err != nil {
		return nil, err
	}

	devices, err := s.store.ListPreviewDevices(ctx, params)
	if err != nil {
		return nil, err
	}
	if err := ensureExplicitDevicesResolved(requestedIdentifiers, devices); err != nil {
		return nil, err
	}

	selector := Selector{
		candidateMinPowerW:   s.config.CandidateMinPowerW,
		capabilitiesProvider: s.capabilitiesProvider,
	}
	plan, err := selector.BuildPlan(ctx, normalized, devices)
	if err != nil {
		return nil, err
	}

	return plan.toResponse(normalized), nil
}

func (s *Service) ensureDeviceSetsResolved(ctx context.Context, orgID int64, req normalizedPreviewRequest) error {
	if req.scopeType != interfaces.CurtailmentScopeDeviceSets {
		return nil
	}

	validIDs, err := s.store.ListValidDeviceSetIDs(ctx, orgID, req.deviceSetIDs)
	if err != nil {
		return err
	}
	return ensureRequestedDeviceSetsResolved(req.deviceSetIDs, validIDs)
}

func ensureRequestedDeviceSetsResolved(requested []int64, resolved []int64) error {
	if len(requested) == 0 {
		return nil
	}

	found := make(map[int64]struct{}, len(resolved))
	for _, id := range resolved {
		found[id] = struct{}{}
	}

	checked := make(map[int64]struct{}, len(requested))
	for _, id := range requested {
		if _, ok := checked[id]; ok {
			continue
		}
		checked[id] = struct{}{}
		if _, ok := found[id]; !ok {
			return fleeterror.NewInvalidArgumentErrorf("device_set_id %d is not in the caller organization or does not exist", id)
		}
	}
	return nil
}
