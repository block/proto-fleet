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
			device         sqlc.GetPoolConfigurationRuntimeRow
			owner          string
			recoverRemoval bool
		}
		initial, err := db.WithTransaction(ctx, s.conn, func(q sqlc.Querier) (admission, error) {
			if err := lockPoolBindings(ctx, q, scope, []poolconfigurations.AssignmentTarget{target}, true); err != nil {
				return admission{}, err
			}
			owner, err := s.reviewer.authority.AuthorizePoolEquipment(ctx, q, scope, actorID, target)
			if err != nil {
				return admission{}, err
			}
			device, err := q.GetPoolConfigurationRuntime(ctx, sqlc.GetPoolConfigurationRuntimeParams{OrgID: scope.OrganizationID, ID: target.DeviceID})
			if err != nil {
				return admission{}, err
			}
			// A board transition can follow an obsolete whole-container clear.
			// Completed writes may be observed and retired under current authority;
			// unknown writes still retain the device fence.
			retired, err := q.ListRetiredCompletedPoolRemovals(ctx, sqlc.ListRetiredCompletedPoolRemovalsParams{OrganizationID: scope.OrganizationID, TargetID: target.DeviceID})
			if err != nil {
				return admission{}, err
			}
			recoverRemoval := len(retired) > 0
			if !recoverRemoval {
				if err := devicewrite.RequireNoConflictingPoolWrites(ctx, q, scope.OrganizationID, target.DeviceID); err != nil {
					return admission{}, err
				}
			}
			return admission{device: device, owner: owner, recoverRemoval: recoverRemoval}, nil
		})
		if err != nil {
			return err
		}
		if commonpb.DeviceClass(initial.device.DeviceClass) != commonpb.DeviceClass_DEVICE_CLASS_CONTAINER && !initial.recoverRemoval {
			continue
		}
		if s.miners == nil {
			return fleeterror.NewInternalError("container readiness is unavailable")
		}
		err = s.writes.WithHostObservation(ctx, scope.OrganizationID, target.DeviceID, initial.device.Epoch, func(ctx context.Context) error {
			return s.writes.WithBindingTransaction(ctx, func(ctx context.Context, q sqlc.Querier) error {
				if err := lockPoolBindings(ctx, q, scope, []poolconfigurations.AssignmentTarget{target}, true); err != nil {
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
					if initial.recoverRemoval {
						if err := s.recoverRetiredRemovals(ctx, q, scope, target, initial.device.DeviceClass); err != nil {
							return err
						}
					}
					if err := devicewrite.RequireNoConflictingPoolWrites(ctx, q, scope.OrganizationID, target.DeviceID); err != nil {
						return err
					}
					if commonpb.DeviceClass(initial.device.DeviceClass) != commonpb.DeviceClass_DEVICE_CLASS_CONTAINER {
						return nil
					}
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

// Called only under current equipment authority and binding locks. Readback
// settles the obsolete intent, not the new owner's assignment. A nonempty
// result is still a valid observation; supersession never claims clear success.
func (s *Readiness) recoverRetiredRemovals(ctx context.Context, q sqlc.Querier, scope poolconfigurations.Scope, target poolconfigurations.AssignmentTarget, deviceClass int32) error {
	retired, err := q.ListRetiredCompletedPoolRemovals(ctx, sqlc.ListRetiredCompletedPoolRemovalsParams{OrganizationID: scope.OrganizationID, TargetID: target.DeviceID})
	if err != nil {
		return err
	}
	if len(retired) > 0 {
		frozen, err := q.PoolConfigurationWritersFrozen(ctx, scope.OrganizationID)
		if err != nil {
			return err
		}
		if frozen {
			return fleeterror.NewFailedPreconditionError("pool configuration maintenance is in progress")
		}
	}
	for _, removalID := range retired {
		if err := devicewrite.RequireNoConflictingPoolWritesForRemoval(ctx, q, scope.OrganizationID, target.DeviceID, removalID); err != nil {
			return err
		}
		miner, err := s.miners.GetMinerForCommand(ctx, target.DeviceID)
		if err != nil {
			return fleeterror.NewUnavailableErrorf("previous pool removal could not be checked; try again when the equipment is available")
		}
		if _, err := removalPoolsEmpty(ctx, miner, commonpb.DeviceClass(deviceClass) == commonpb.DeviceClass_DEVICE_CLASS_CONTAINER); err != nil {
			return fleeterror.NewUnavailableErrorf("previous pool removal could not be checked; try again when the equipment is available")
		}
		changed, err := q.FinishPoolRemoval(ctx, sqlc.FinishPoolRemovalParams{RemovalID: removalID, State: "superseded", ResultCode: "assignment_changed"})
		if err != nil {
			return err
		}
		if changed != 1 {
			return fleeterror.NewFailedPreconditionError("pool removal changed; review again")
		}
	}
	return nil
}
