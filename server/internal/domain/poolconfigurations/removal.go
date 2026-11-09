package poolconfigurations

import (
	"context"
	"fmt"
	"strings"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
)

// RemovalReviewTarget freezes removal preflight alongside the shared review target.
type RemovalReviewTarget = reviewedTarget

type Removal struct {
	ID                 string
	Target             AssignmentTarget
	VerificationState  string
	ClearSupported     bool
	State, ResultCode  string
	KeepDeviceSettings bool
}

type RemovalManager interface {
	ReviewRemoval(ctx context.Context, principal Principal, target AssignmentTarget) (Removal, error)
	EnqueueRemoval(ctx context.Context, q sqlc.Querier, principal Principal, id string, keep bool) error
	ValidateRemoval(ctx context.Context, q sqlc.Querier, principal Principal, row sqlc.GetPoolRemovalRow, allowOwn bool) error
	RecoverRemoval(ctx context.Context, principal Principal, id string) (Removal, error)
	GetRemoval(ctx context.Context, principal Principal, id string) (Removal, error)
}

func (s *Service) removalPrincipal(ctx context.Context, accountID string) (Principal, RemovalManager, error) {
	p, err := withAuthority(ctx, s, accountID, Read, func(_ sqlc.Querier, p Principal) (Principal, error) { return p, nil })
	manager, ok := s.checker.(RemovalManager)
	if err == nil && !ok {
		err = fleeterror.NewUnavailableErrorf("pool removal is unavailable")
	}
	return p, manager, err
}

func (s *Service) ReviewRemoval(ctx context.Context, accountID string, target AssignmentTarget) (Removal, error) {
	if target.ID <= 0 || (target.Kind != TargetDevice && target.Kind != TargetInstallation) {
		return Removal{}, fleeterror.NewInvalidArgumentError("select valid equipment")
	}
	p, manager, err := s.removalPrincipal(ctx, accountID)
	if err != nil {
		return Removal{}, err
	}
	return manager.ReviewRemoval(ctx, p, target)
}

func (s *Service) GetRemoval(ctx context.Context, accountID, id string) (Removal, error) {
	p, manager, err := s.removalPrincipal(ctx, accountID)
	if err != nil {
		return Removal{}, err
	}
	return manager.GetRemoval(ctx, p, id)
}

func RecordRemovalAudit(ctx context.Context, q sqlc.Querier, p Principal, removalID, configID string, keep bool) error {
	event := "clear_device_pools_and_unassign"
	if keep {
		event = "unassign_pool_configuration"
	}
	return recordConfigurationAudit(ctx, q, p, event, map[string]string{"configuration_id": configID, "removal_id": removalID})
}

// ClearedPoolIntent is distinct from every configuration/revision identity and
// partitions empty groups by account. It never names a library configuration.
func ClearedPoolIntent(accountID int64) string { return fmt.Sprintf("pool-free:%d", accountID) }
func IsClearedPoolIntent(id string) bool       { return strings.HasPrefix(id, "pool-free:") }
