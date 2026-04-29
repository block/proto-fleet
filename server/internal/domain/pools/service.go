package pools

import (
	"context"
	"fmt"
	"strings"
	"time"

	pb "github.com/block/proto-fleet/server/generated/grpc/pools/v1"
	"github.com/block/proto-fleet/server/internal/domain/activity"
	activitymodels "github.com/block/proto-fleet/server/internal/domain/activity/models"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
	"github.com/block/proto-fleet/server/internal/domain/sv2"
	"github.com/block/proto-fleet/server/internal/infrastructure/secrets"
	stratumv1 "github.com/block/proto-fleet/server/internal/infrastructure/stratum/v1"
)

type PoolStatus string

type Service struct {
	poolStore   interfaces.PoolStore
	transactor  interfaces.Transactor
	cfg         Config
	activitySvc *activity.Service
}

func NewService(poolStore interfaces.PoolStore, transactor interfaces.Transactor, cfg Config, activitySvc *activity.Service) *Service {
	return &Service{
		poolStore:   poolStore,
		transactor:  transactor,
		cfg:         cfg,
		activitySvc: activitySvc,
	}
}

func (s *Service) logActivity(ctx context.Context, event activitymodels.Event) {
	if s.activitySvc != nil {
		s.activitySvc.Log(ctx, event)
	}
}

func (s *Service) DeletePool(ctx context.Context, id int64) error {
	info, err := session.GetInfo(ctx)
	if err != nil {
		return err
	}

	pool, poolErr := s.poolStore.GetPool(ctx, info.OrganizationID, id)

	if err := s.transactor.RunInTx(ctx, func(ctx context.Context) error {
		return s.poolStore.SoftDeletePool(ctx, info.OrganizationID, id)
	}); err != nil {
		return err
	}

	if poolErr == nil {
		poolName := pool.GetPoolName()
		s.logActivity(ctx, activitymodels.Event{
			Category:       activitymodels.CategoryPool,
			Type:           "delete_pool",
			Description:    fmt.Sprintf("Delete pool: %s", poolName),
			UserID:         &info.ExternalUserID,
			Username:       &info.Username,
			OrganizationID: &info.OrganizationID,
			Metadata:       map[string]any{"pool_name": poolName},
		})
	}

	return nil
}

func (s *Service) UpdatePool(ctx context.Context, r *pb.UpdatePoolRequest) (*pb.Pool, error) {
	info, err := session.GetInfo(ctx)
	if err != nil {
		return nil, err
	}

	// Callers migrating from the old "empty means unchanged" contract
	// must now omit the field; reject explicit "" (and whitespace-only
	// values, which are equally meaningless to the store) so we never
	// write them.
	if r.PoolName != nil && strings.TrimSpace(r.GetPoolName()) == "" {
		return nil, fleeterror.NewInvalidArgumentError("pool_name cannot be empty; omit the field to leave unchanged")
	}
	if r.Url != nil && strings.TrimSpace(r.GetUrl()) == "" {
		return nil, fleeterror.NewInvalidArgumentError("url cannot be empty; omit the field to leave unchanged")
	}
	if r.Url != nil {
		if err := sv2.ValidatePoolURL(r.GetUrl()); err != nil {
			return nil, err
		}
	}
	if r.Username != nil && strings.TrimSpace(r.GetUsername()) == "" {
		return nil, fleeterror.NewInvalidArgumentError("username cannot be empty; omit the field to leave unchanged")
	}

	// No patch fields set means the caller has nothing to change. Skip
	// the UPDATE and the activity event so an empty patch isn't a write
	// or a row in the activity feed.
	if r.PoolName == nil && r.Url == nil && r.Username == nil && r.Password == nil {
		return s.poolStore.GetPool(ctx, info.OrganizationID, r.PoolId)
	}

	if r.Username != nil {
		existingPool, err := s.poolStore.GetPool(ctx, info.OrganizationID, r.PoolId)
		if err != nil {
			return nil, err
		}

		if r.GetUsername() != existingPool.GetUsername() {
			if err := validatePoolUsername(r.GetUsername()); err != nil {
				return nil, err
			}
		}
	}

	result, err := s.transactor.RunInTxWithResult(ctx, func(ctx context.Context) (any, error) {
		if err := s.poolStore.UpdatePool(ctx, r, info.OrganizationID); err != nil {
			return nil, err
		}

		updatedPool, err := s.poolStore.GetPool(ctx, info.OrganizationID, r.PoolId)
		if err != nil {
			return nil, err
		}

		return updatedPool, nil
	})

	if err != nil {
		return nil, err
	}

	updatedPool, ok := result.(*pb.Pool)
	if !ok {
		return nil, fleeterror.NewInternalErrorf("unexpected result type: %T", result)
	}

	poolName := updatedPool.GetPoolName()
	s.logActivity(ctx, activitymodels.Event{
		Category:       activitymodels.CategoryPool,
		Type:           "update_pool",
		Description:    fmt.Sprintf("Update pool: %s", poolName),
		UserID:         &info.ExternalUserID,
		Username:       &info.Username,
		OrganizationID: &info.OrganizationID,
		Metadata:       map[string]any{"pool_name": poolName},
	})

	return updatedPool, nil
}

func (s *Service) CreatePool(ctx context.Context, poolConfig *pb.PoolConfig) (*pb.Pool, error) {
	info, err := session.GetInfo(ctx)
	if err != nil {
		return nil, err
	}

	if err := validatePoolUsername(poolConfig.GetUsername()); err != nil {
		return nil, err
	}
	if err := sv2.ValidatePoolURL(poolConfig.GetUrl()); err != nil {
		return nil, err
	}

	result, err := s.transactor.RunInTxWithResult(ctx, func(ctx context.Context) (any, error) {
		poolID, err := s.poolStore.CreatePool(ctx, poolConfig, info.OrganizationID)
		if err != nil {
			return nil, fleeterror.NewInternalErrorf("error saving pool for org_id: %d, pool_name: %s: %v", info.OrganizationID, poolConfig.PoolName, err)
		}

		pool, err := s.poolStore.GetPool(ctx, info.OrganizationID, poolID)
		if err != nil {
			return nil, fleeterror.NewInternalErrorf("error getting created pool for org_id: %d, pool_id: %d: %v", info.OrganizationID, poolID, err)
		}

		return pool, nil
	})

	if err != nil {
		return nil, err
	}

	pool, ok := result.(*pb.Pool)
	if !ok {
		return nil, fleeterror.NewInternalErrorf("unexpected result type: %T", result)
	}

	poolName := pool.GetPoolName()
	s.logActivity(ctx, activitymodels.Event{
		Category:       activitymodels.CategoryPool,
		Type:           "create_pool",
		Description:    fmt.Sprintf("Create pool: %s", poolName),
		UserID:         &info.ExternalUserID,
		Username:       &info.Username,
		OrganizationID: &info.OrganizationID,
		Metadata:       map[string]any{"pool_name": poolName},
	})

	return pool, nil
}

func (s *Service) ListPools(ctx context.Context) ([]*pb.Pool, error) {
	info, err := session.GetInfo(ctx)
	if err != nil {
		return nil, err
	}

	pools, err := s.poolStore.ListPools(ctx, info.OrganizationID)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("error listing pools: %v", err)
	}

	return pools, nil
}

// ValidateConnection probes a pool server. SV1 URLs run a full
// mining.subscribe + authorize. SV2 URLs run a Noise NX handshake
// against the authority pubkey embedded in the URL path, which
// confirms the pool speaks SV2 and presents the operator-pinned
// static key. The protocol has no equivalent of SV1's credential
// check — credentials are deferred to the worker connection.
func (s *Service) ValidateConnection(ctx context.Context, url string, username string, password *secrets.Text, timeout *time.Duration) (bool, error) {
	to := s.cfg.Timeout
	if timeout != nil {
		to = *timeout
	}
	ctx, cancel := context.WithTimeout(ctx, to)
	defer cancel()

	if sv2.IsSV2URL(url) {
		key, err := sv2.PoolNoiseKeyFromURL(url)
		if err != nil {
			return false, fleeterror.NewInvalidArgumentErrorf("%v", err)
		}
		return sv2.HandshakeProbe(ctx, url, key, to)
	}
	return stratumv1.Authenticate(ctx, url, username, password)
}

