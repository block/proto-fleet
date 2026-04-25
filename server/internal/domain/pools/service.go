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
	"github.com/block/proto-fleet/server/internal/domain/pools/rewriter"
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

	// Validate URL scheme on every code path that reaches the store,
	// not just CEL — internal callers and any future RPC bypass would
	// otherwise persist an unsupported scheme that dbProtocolFromURL
	// then silently coerces to 'sv1', defeating the SV2 preflight for
	// the imported row.
	if r.Url != nil {
		if _, err := rewriter.ProtocolFromURL(*r.Url); err != nil {
			return nil, fleeterror.NewInvalidArgumentErrorf("invalid pool url: %v", err)
		}
	}

	// Proto3 explicit presence: an absent Username means "leave unchanged".
	// An explicit empty string is always rejected so the patch contract
	// can't be used to dispatch credential-less connections to miners.
	// The separator rule (no '.') is enforced only when the username is
	// actually changing — pools predating the separator restriction can
	// still be edited (rename, etc.) without the legacy-data username
	// being forced through the new contract.
	if r.Username != nil {
		if strings.TrimSpace(*r.Username) == "" {
			return nil, fleeterror.NewInvalidArgumentError(invalidPoolUsernameEmptyMessage)
		}
		existingPool, err := s.poolStore.GetPool(ctx, info.OrganizationID, r.PoolId)
		if err != nil {
			return nil, err
		}
		if *r.Username != existingPool.GetUsername() {
			if err := validatePoolUsername(*r.Username); err != nil {
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

	// Validate URL scheme as well as username so internal callers
	// can't slip a scheme past CEL and end up with a row that
	// dbProtocolFromURL would silently coerce to 'sv1'.
	if _, err := rewriter.ProtocolFromURL(poolConfig.GetUrl()); err != nil {
		return nil, fleeterror.NewInvalidArgumentErrorf("invalid pool url: %v", err)
	}

	if err := validatePoolUsername(poolConfig.GetUsername()); err != nil {
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

// ValidationResult reports what ValidateConnection actually attempted and
// what it observed. It maps 1:1 onto pools.v1.ValidatePoolResponse so the
// handler can pass it through without losing fidelity: the UI renders
// "reachable but credentials unverified" when Reachable &&
// !CredentialsVerified && Mode is an SV2 mode, which is the honest
// description of what a TCP dial actually proves.
type ValidationResult struct {
	Reachable           bool
	CredentialsVerified bool
	Mode                pb.ValidationMode
}

// ValidateConnection probes a pool server, picking the probe style by
// the URL's scheme. SV1 URLs run a full Authenticate (subscribe +
// authorize); SV2 URLs run either a Noise NX handshake (when the
// caller provided the pool's Noise authority pubkey) or a TCP dial
// only. The chosen mode is returned so the UI renders the honest
// outcome rather than inferring from (URL, success).
//
// The SV2 handshake probe proves the pool holds the static key the
// operator supplied — a substantial step up from "something answers
// TCP on this port". It doesn't prove credentials authorise mining;
// that would require a full SetupConnection + OpenStandardMiningChannel
// roundtrip.
func (s *Service) ValidateConnection(
	ctx context.Context,
	url string,
	username string,
	password *secrets.Text,
	poolNoiseKey []byte,
	timeout *time.Duration,
) (ValidationResult, error) {
	protocol, err := rewriter.ProtocolFromURL(url)
	if err != nil {
		return ValidationResult{}, fleeterror.NewInvalidArgumentErrorf("%v", err)
	}

	to := s.cfg.Timeout
	if timeout != nil {
		to = *timeout
	}
	ctx, cancel := context.WithTimeout(ctx, to)
	defer cancel()

	switch protocol {
	case pb.PoolProtocol_POOL_PROTOCOL_SV2:
		// SV2 validation requires the operator to supply the pool's
		// 32-byte Noise authority public key. The key drives a Noise
		// NX handshake probe that pins the pool's identity — no key
		// means we'd be falling back to a bare TCP dial, which both
		// (a) tells the operator "connected" without ever proving
		// they reached the right pool, and (b) turns ValidatePool
		// into a generic TCP scanner against any host:port reachable
		// from the API server. Reject up front and route operators
		// to the docs that explain how to find the key.
		if len(poolNoiseKey) == 0 {
			return ValidationResult{Mode: pb.ValidationMode_VALIDATION_MODE_SV2_HANDSHAKE},
				fleeterror.NewInvalidArgumentError("Stratum V2 pool validation requires the pool's Noise authority public key (32 raw bytes); look it up in the pool operator's docs")
		}
		if len(poolNoiseKey) != 32 {
			return ValidationResult{Mode: pb.ValidationMode_VALIDATION_MODE_SV2_HANDSHAKE},
				fleeterror.NewInvalidArgumentErrorf("noise public key must be 32 raw bytes, got %d", len(poolNoiseKey))
		}
		ok, err := sv2.HandshakeProbe(ctx, url, poolNoiseKey, to)
		if err != nil {
			return ValidationResult{Mode: pb.ValidationMode_VALIDATION_MODE_SV2_HANDSHAKE}, err
		}
		return ValidationResult{
			Reachable: ok,
			Mode:      pb.ValidationMode_VALIDATION_MODE_SV2_HANDSHAKE,
		}, nil
	case pb.PoolProtocol_POOL_PROTOCOL_SV1, pb.PoolProtocol_POOL_PROTOCOL_UNSPECIFIED:
		// UNSPECIFIED is rejected above by ProtocolFromURL when the URL
		// has no recognised scheme; reach this case only when the URL
		// said "stratum+tcp" and the rewriter classified it SV1.
		//
		// Authenticate returns (true, nil) on a clean subscribe+authorize,
		// (false, nil) when the pool returned a JSON-RPC false (typically
		// bad credentials), and (_, err) for transport failures (DNS,
		// refused, RST). The first two cases both mean the host is
		// reachable — the difference is whether credentials checked out —
		// so we only flip Reachable to false when err itself is non-nil.
		// Conflating ok=false with unreachable would mask credential
		// problems as connectivity issues in the UI.
		ok, err := stratumv1.Authenticate(ctx, url, username, password)
		if err != nil {
			return ValidationResult{Mode: pb.ValidationMode_VALIDATION_MODE_SV1_AUTHENTICATE}, err
		}
		return ValidationResult{
			Reachable:           true,
			CredentialsVerified: ok,
			Mode:                pb.ValidationMode_VALIDATION_MODE_SV1_AUTHENTICATE,
		}, nil
	default:
		return ValidationResult{}, fleeterror.NewInvalidArgumentErrorf("unsupported pool protocol: %v", protocol)
	}
}
