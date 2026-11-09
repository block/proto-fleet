package poolapplications

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"time"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/devicewrite"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/miner/dto"
	"github.com/block/proto-fleet/server/internal/domain/poolconfigurations"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
	"github.com/block/proto-fleet/server/internal/infrastructure/encrypt"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

const stateApplying = "applying"
const codeUnavailable = "unavailable"

type Checker struct {
	boardRemovals BoardRemovalCoordinator
	conn          *sql.DB
	reviewer      *Reviewer
	miners        MinerGetter
	secrets       *encrypt.Service
	library       poolconfigurations.Authority
	writes        *devicewrite.Service
}

func NewChecker(conn *sql.DB, reviewer *Reviewer, miners MinerGetter, secrets *encrypt.Service, library poolconfigurations.Authority) *Checker {
	return &Checker{conn: conn, reviewer: reviewer, miners: miners, secrets: secrets, library: library, writes: devicewrite.NewService(conn)}
}

// CheckConfiguration observes current hardware without changing settings or
// resolving write attempts. Authority and the exact assignment are held through
// I/O; evidence cannot be attached to a replacement board or newer assignment.
func (c *Checker) CheckConfiguration(ctx context.Context, p poolconfigurations.Principal, configID string, target poolconfigurations.AssignmentTarget) (poolconfigurations.CheckResult, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	var result poolconfigurations.CheckResult
	inheritedContainerPools := false
	var version int64
	authorize := func(q sqlc.Querier) (sqlc.GetPoolConfigurationAssignmentRow, sqlc.GetPoolConfigurationRuntimeRow, error) {
		var runtime sqlc.GetPoolConfigurationRuntimeRow
		current, err := c.library.AuthorizePoolConfiguration(ctx, q, p.AccountID, poolconfigurations.Observe)
		if err != nil {
			return sqlc.GetPoolConfigurationAssignmentRow{}, runtime, err
		}
		if current.Scope != p.Scope || current.ActorID != p.ActorID {
			return sqlc.GetPoolConfigurationAssignmentRow{}, runtime, fleeterror.NewForbiddenError("equipment access changed")
		}
		if err := lockPoolBindings(ctx, q, p.Scope, []poolconfigurations.AssignmentTarget{target}, true); err != nil {
			return sqlc.GetPoolConfigurationAssignmentRow{}, runtime, err
		}
		if _, err := c.reviewer.authority.AuthorizePoolEquipment(ctx, q, p.Scope, p.ActorID, target); err != nil {
			return sqlc.GetPoolConfigurationAssignmentRow{}, runtime, err
		}
		a, err := q.GetPoolConfigurationAssignment(ctx, sqlc.GetPoolConfigurationAssignmentParams{OrganizationID: p.Scope.OrganizationID, TargetKind: target.Kind, TargetID: target.ID})
		if configID == "" {
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return a, runtime, err
			}
			if err == nil && !a.RetiredAt.Valid {
				return a, runtime, fleeterror.NewFailedPreconditionError("assignment changed; refresh its status")
			}
			runtime, err = q.GetPoolConfigurationRuntime(ctx, sqlc.GetPoolConfigurationRuntimeParams{OrgID: p.Scope.OrganizationID, ID: target.DeviceID})
			return sqlc.GetPoolConfigurationAssignmentRow{}, runtime, err
		}
		if err != nil {
			return a, runtime, err
		}
		if a.RetiredAt.Valid || a.ConfigurationID != configID || a.AccountID.Int64 != p.Scope.AccountID || a.DeviceID != target.DeviceID || version != 0 && a.Version != version {
			return a, runtime, fleeterror.NewFailedPreconditionError("assignment changed; refresh its status")
		}
		runtime, err = q.GetPoolConfigurationRuntime(ctx, sqlc.GetPoolConfigurationRuntimeParams{OrgID: p.Scope.OrganizationID, ID: target.DeviceID})
		return a, runtime, err
	}
	runtime, err := db.WithTransaction(ctx, c.conn, func(q sqlc.Querier) (sqlc.GetPoolConfigurationRuntimeRow, error) {
		a, r, err := authorize(q)
		version = a.Version
		return r, err
	})
	if err != nil {
		return result, err
	}
	err = c.writes.WithHostObservation(ctx, p.Scope.OrganizationID, target.DeviceID, runtime.Epoch, func(ctx context.Context) error {
		return c.writes.WithBindingTransaction(ctx, func(ctx context.Context, q sqlc.Querier) error {
			a, runtime, err := authorize(q)
			if err != nil {
				return err
			}
			if a.OperationID.Valid {
				work, err := q.GetPoolConfigurationWork(ctx, sqlc.GetPoolConfigurationWorkParams{OrganizationID: p.Scope.OrganizationID, OperationID: a.OperationID.String, TargetKind: target.Kind, TargetID: target.ID})
				if err != nil {
					return err
				}
				if work.State == "pending" || work.State == stateApplying {
					return fleeterror.NewFailedPreconditionError("the pool configuration update is still running")
				}
			}
			var desired []dto.MiningPool
			if configID != "" {
				revision, err := q.GetPoolConfigurationRevision(ctx, sqlc.GetPoolConfigurationRevisionParams{OrganizationID: p.Scope.OrganizationID, AccountID: a.AccountID, ConfigurationID: a.ConfigurationID, RevisionID: a.RevisionID})
				if err != nil {
					return err
				}
				slots, err := poolconfigurations.DecodeSlots(revision.Slots)
				if err != nil {
					return err
				}
				desired = make([]dto.MiningPool, len(slots))
				for i, slot := range slots {
					username, err := poolconfigurations.RenderWorker(slot.Username, slot.WorkerTemplate, runtime.DeviceIdentifier, runtime.WorkerName)
					if err != nil {
						return err
					}
					password, err := c.secrets.Decrypt(slot.PasswordCiphertext)
					if err != nil {
						return fleeterror.NewInternalError("configuration credential unavailable")
					}
					desired[i] = dto.MiningPool{Priority: uint32(i), URL: slot.URL, Username: username, Password: string(password)}                  // #nosec G115 -- configuration has at most three pools.
					result.Expected = append(result.Expected, poolconfigurations.PoolReading{Priority: int32(i), URL: slot.URL, Username: username}) // #nosec G115 -- at most three pools.
				}
			}
			err = c.writes.WithBindingObservation(ctx, target.DeviceID, func(sqlc.Querier) error { return nil }, func(ctx context.Context) error {
				result.State, result.ResultCode = resultUnverified, codeReadbackUnavailable
				miner, err := c.miners.GetMinerForCommand(ctx, target.DeviceID)
				if err != nil {
					return nil //nolint:nilerr // Persist unavailable readback; driver errors may contain credentials.
				}
				if target.Kind == poolconfigurations.TargetDevice {
					if commonpb.DeviceClass(runtime.DeviceClass) == commonpb.DeviceClass_DEVICE_CLASS_CONTAINER {
						groups, ok := miner.(sdk.DeviceMiningGroups)
						if !ok {
							return nil
						}
						observed, err := groups.MiningGroups(ctx, sdk.MiningGroupRequest{Action: "observe"})
						if err != nil || !observed.Supported || observed.Enabled || observed.Pending || observed.Suspended {
							return nil //nolint:nilerr // Persist the unavailable observation without exposing driver errors.
						}
					}
					if configID == "" {
						pools, readErr := miner.GetMiningPools(ctx)
						if readErr == nil {
							result.ReadbackAvailable = true
							for _, pool := range pools {
								result.Reported = append(result.Reported, poolconfigurations.PoolReading{Priority: pool.Priority, URL: pool.URL, Username: pool.Username})
							}
							slices.SortFunc(result.Reported, func(a, b poolconfigurations.PoolReading) int { return int(a.Priority) - int(b.Priority) })
						}
					} else {
						result.State, result.ResultCode, result.Reported = readPools(ctx, miner, desired)
						result.ReadbackAvailable = result.State != resultUnverified || len(result.Reported) > 0
					}
					return nil
				}
				boards, err := q.ListPoolSelectionBoards(ctx, sqlc.ListPoolSelectionBoardsParams{OrgID: p.Scope.OrganizationID, InstallationIds: []int64{target.ID}})
				if err != nil {
					return err
				}
				if len(boards) != 1 || !boards[0].Ready || boards[0].DeviceID != target.DeviceID {
					return fleeterror.NewFailedPreconditionError("hashboard location is unavailable; check the controller's module mapping")
				}
				groups, ok := miner.(sdk.DeviceMiningGroups)
				if !ok {
					return nil
				}
				observed, err := groups.MiningGroups(ctx, sdk.MiningGroupRequest{Action: "observe"})
				if err != nil {
					return nil //nolint:nilerr // Persist the unavailable observation without exposing driver errors.
				}
				if configID == "" {
					readback := reportedBoardPools(observed, boards[0].MacAddress)
					if wholeContainerPoolMode(observed) {
						readback = readCurrentPoolStack(ctx, miner)
						inheritedContainerPools = readback.Available
					}
					result.Reported, result.ReadbackAvailable = readback.Pools, readback.Available
				} else {
					result.State, result.ResultCode, result.Reported = checkBoardPools(observed, boards[0].MacAddress, result.Expected)
					result.ReadbackAvailable = result.State != resultUnverified
				}
				return nil
			})
			if err != nil {
				return err
			}
			if configID == "" {
				result.State, result.ResultCode = codeUnavailable, "check_unavailable"
				if result.ReadbackAvailable {
					result.State, result.ResultCode = "", ""
					if inheritedContainerPools {
						result.State = stateContainerConfiguration
					} else if len(result.Reported) > 0 {
						result.State = "device_configuration"
					}
				}
				result.CheckedAt = time.Now()
				return nil
			}
			conflict := devicewrite.RequireNoConflictingPoolWrites(ctx, q, p.Scope.OrganizationID, target.DeviceID)
			if conflict != nil {
				if !fleeterror.IsFailedPreconditionError(conflict) {
					return conflict
				}
				if result.State != resultDifferent {
					result.State = resultUnverified
				}
				result.ResultCode = "write_uncertain"
			}
			if result.ResultCode == codeReadbackUnavailable {
				result.ResultCode = "check_unavailable"
			}
			if err := q.SavePoolConfigurationObservation(ctx, sqlc.SavePoolConfigurationObservationParams{OrganizationID: p.Scope.OrganizationID, TargetKind: target.Kind, TargetID: target.ID, AssignmentVersion: a.Version, State: result.State, ResultCode: result.ResultCode}); err != nil {
				return err
			}
			// Return the persisted evidence timestamp so assignment polling does
			// not make this same check look like a newer, separate observation.
			observation, err := q.GetPoolConfigurationObservation(ctx, sqlc.GetPoolConfigurationObservationParams{OrganizationID: p.Scope.OrganizationID, TargetKind: target.Kind, TargetID: target.ID, AssignmentVersion: a.Version})
			if err != nil {
				return err
			}
			result.CheckedAt = observation.ObservedAt
			return nil
		})
	})
	return result, err
}

func checkBoardPools(observed sdk.MiningGroupResult, mac string, expected []poolconfigurations.PoolReading) (string, string, []poolconfigurations.PoolReading) {
	readback := reportedBoardPools(observed, mac)
	if !readback.Available {
		return resultUnverified, codeReadbackUnavailable, nil
	}
	if readback.UnsupportedPolicy {
		return resultDifferent, codeSettingsDiffer, readback.Pools
	}
	if !slices.Equal(readback.Pools, expected) {
		return resultDifferent, codeSettingsDiffer, readback.Pools
	}
	return resultVerified, "", readback.Pools
}

type boardPoolReadback struct {
	Pools             []poolconfigurations.PoolReading
	Available         bool
	UnsupportedPolicy bool
}

func reportedBoardPools(observed sdk.MiningGroupResult, mac string) boardPoolReadback {
	var result boardPoolReadback
	if !observed.Supported || !observed.Enabled || observed.Pending || observed.Suspended {
		return result
	}
	found := 0
	for _, group := range observed.Groups {
		if !slices.Contains(group.MACs, mac) {
			continue
		}
		found++
		result.UnsupportedPolicy = result.UnsupportedPolicy || group.CustomPolicy
		for _, pool := range group.Pools {
			// Preserve all readable settings for display, even when the firmware policy
			// cannot represent an ordered configuration for verification.
			result.UnsupportedPolicy = result.UnsupportedPolicy || !pool.Enabled || pool.ShareWeight != nil
			result.Pools = append(result.Pools, poolconfigurations.PoolReading{Priority: pool.Priority, URL: pool.URL, Username: pool.Username})
		}
	}
	slices.SortFunc(result.Pools, func(a, b poolconfigurations.PoolReading) int { return int(a.Priority) - int(b.Priority) })
	result.Available = found == 1
	return result
}
