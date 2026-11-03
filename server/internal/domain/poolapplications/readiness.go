package poolapplications

import (
	"context"
	"database/sql"
	"encoding/json"
	"slices"
	"time"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/devicewrite"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/poolconfigurations"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

// Readiness refreshes controller mode using the selected account's existing
// Change pools authority. It is an observation, never a mining-mode mutation.
type Readiness struct {
	conn     *sql.DB
	reviewer *Reviewer
	miners   MinerGetter
	writes   *devicewrite.Service
}

func NewReadiness(conn *sql.DB, reviewer *Reviewer, miners MinerGetter) *Readiness {
	return &Readiness{conn: conn, reviewer: reviewer, miners: miners, writes: devicewrite.NewService(conn)}
}

func (s *Readiness) RefreshTargets(ctx context.Context, scope poolconfigurations.Scope, actorID int64, targets []poolconfigurations.AssignmentTarget) (resultErr error) {
	// Bound the entire refresh, including lock acquisition and Fleet Node replies.
	// Keep the binding locks until the observation returns and unwinds normally.
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	defer func() {
		if resultErr != nil && ctx.Err() == context.DeadlineExceeded {
			resultErr = fleeterror.NewUnavailableErrorf("container readiness timed out; try reviewing again when connected: %w", context.DeadlineExceeded)
		}
	}()
	targets = slices.Clone(targets)
	slices.SortFunc(targets, func(a, b poolconfigurations.AssignmentTarget) int {
		if a.DeviceID < b.DeviceID {
			return -1
		}
		if a.DeviceID > b.DeviceID {
			return 1
		}
		return 0
	})
	seen := map[int64]bool{}
	for _, target := range targets {
		if seen[target.DeviceID] {
			continue
		}
		seen[target.DeviceID] = true
		type admission struct {
			device sqlc.GetPoolConfigurationRuntimeRow
			owner  string
		}
		initial, err := db.WithTransaction(ctx, s.conn, func(q sqlc.Querier) (admission, error) {
			if err := lockReviewBindings(ctx, q, scope, []poolconfigurations.AssignmentTarget{target}); err != nil {
				return admission{}, err
			}
			owner, err := s.reviewer.authority.AuthorizePoolEquipment(ctx, q, scope, actorID, target)
			if err != nil {
				return admission{}, err
			}
			device, err := q.GetPoolConfigurationRuntime(ctx, sqlc.GetPoolConfigurationRuntimeParams{OrgID: scope.OrganizationID, ID: target.DeviceID})
			return admission{device: device, owner: owner}, err
		})
		if err != nil {
			return err
		}
		if commonpb.DeviceClass(initial.device.DeviceClass) != commonpb.DeviceClass_DEVICE_CLASS_CONTAINER {
			continue
		}
		if s.miners == nil {
			return fleeterror.NewInternalError("container readiness is unavailable")
		}
		err = s.writes.WithHostObservation(ctx, scope.OrganizationID, target.DeviceID, initial.device.Epoch, func(ctx context.Context) error {
			return s.writes.WithBindingTransaction(ctx, func(ctx context.Context, q sqlc.Querier) error {
				if err := lockReviewBindings(ctx, q, scope, []poolconfigurations.AssignmentTarget{target}); err != nil {
					return err
				}
				owner, err := s.reviewer.authority.AuthorizePoolEquipment(ctx, q, scope, actorID, target)
				if err != nil {
					return err
				}
				if owner != initial.owner {
					return fleeterror.NewFailedPreconditionError("equipment ownership changed; review again")
				}
				return s.writes.WithBindingObservation(ctx, target.DeviceID, func(sqlc.Querier) error { return nil }, func(ctx context.Context) error {
					if err := q.EnsureMiningGroupState(ctx, sqlc.EnsureMiningGroupStateParams{DeviceID: target.DeviceID, OrganizationID: scope.OrganizationID}); err != nil {
						return err
					}
					if _, err := q.LockMiningGroupState(ctx, sqlc.LockMiningGroupStateParams{DeviceID: target.DeviceID, OrganizationID: scope.OrganizationID}); err != nil {
						return err
					}
					miner, err := s.miners.GetMinerForCommand(ctx, target.DeviceID)
					if err != nil {
						return fleeterror.NewUnavailableErrorf("container is unavailable; try reviewing again when connected")
					}
					groups, ok := miner.(sdk.DeviceMiningGroups)
					if !ok {
						return fleeterror.NewFailedPreconditionError("container mining-group mode is unavailable")
					}
					observed, err := groups.MiningGroups(ctx, sdk.MiningGroupRequest{Action: "observe"})
					if err != nil {
						return fleeterror.NewUnavailableErrorf("container readiness could not be refreshed")
					}
					encoded, err := json.Marshal(observed)
					if err != nil {
						return fleeterror.NewInternalError("encode container readiness failed")
					}
					return q.RefreshPoolConfigurationReadiness(ctx, sqlc.RefreshPoolConfigurationReadinessParams{OrganizationID: scope.OrganizationID, DeviceID: target.DeviceID, Observed: encoded})
				})
			})
		})
		if err != nil {
			return err
		}
	}
	return nil
}
