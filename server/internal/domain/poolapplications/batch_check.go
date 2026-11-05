package poolapplications

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"sync"
	"time"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/poolconfigurations"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

// ReadDevicePools shares only an in-request observation, never cached evidence.
// Each controller has its own admission and transaction so a failed controller
// cannot discard another controller's results. Results retain request order.
func (c *Checker) ReadDevicePools(ctx context.Context, p poolconfigurations.Principal, targets []poolconfigurations.AssignmentTarget) []poolconfigurations.CheckResult {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	results := make([]poolconfigurations.CheckResult, len(targets))
	grouped := make(map[int64][]int)
	for i, target := range targets {
		results[i] = poolconfigurations.CheckResult{State: codeUnavailable, ResultCode: "check_unavailable"}
		if target.DeviceID > 0 {
			grouped[target.DeviceID] = append(grouped[target.DeviceID], i)
		}
	}
	jobs := make(chan []int, len(grouped))
	devices := make([]int64, 0, len(grouped))
	for device := range grouped {
		devices = append(devices, device)
	}
	slices.Sort(devices)
	for _, device := range devices {
		jobs <- grouped[device]
	}
	close(jobs)
	var workers sync.WaitGroup
	for range min(poolconfigurations.MaxReadbackControllers, len(grouped)) {
		workers.Go(func() {
			for indices := range jobs {
				if ctx.Err() != nil {
					return
				}
				selection := make([]poolconfigurations.AssignmentTarget, len(indices))
				for i, index := range indices {
					selection[i] = targets[index]
				}
				readings := c.readControllerPoolsSafely(ctx, p, selection)
				for i, reading := range readings {
					results[indices[i]] = reading
				}
			}
		})
	}
	workers.Wait()
	return results
}

// Recover per controller so one faulty driver cannot terminate fleetd or stop
// sibling jobs. Never expose driver messages, panic values, or partial results.
func (c *Checker) readControllerPoolsSafely(ctx context.Context, p poolconfigurations.Principal, targets []poolconfigurations.AssignmentTarget) (readings []poolconfigurations.CheckResult) {
	deviceID := targets[0].DeviceID
	defer func() {
		if recovered := recover(); recovered != nil {
			readings = nil
			slog.ErrorContext(ctx, "panic reading device pools", "device_id", deviceID, "error_type", fmt.Sprintf("%T", recovered))
		}
	}()
	readings, err := c.readControllerPools(ctx, p, targets)
	if err != nil {
		slog.WarnContext(ctx, "failed to read device pools", "device_id", deviceID, "error_type", fmt.Sprintf("%T", err))
		return nil
	}
	return readings
}

type devicePoolBinding struct {
	owner string
	board BoardBinding
}

// authorizeDevicePoolRead retains the same physical and ownership locks used by
// explicit verification. Assignment, location and ownership are checked again
// in the non-retrying I/O transaction; an ineligible target never gains access
// merely because a sibling on its controller is authorized.
func (c *Checker) authorizeDevicePoolRead(ctx context.Context, q sqlc.Querier, p poolconfigurations.Principal, targets []poolconfigurations.AssignmentTarget) (sqlc.GetPoolConfigurationRuntimeRow, map[int]devicePoolBinding, error) {
	var runtime sqlc.GetPoolConfigurationRuntimeRow
	current, err := c.library.AuthorizePoolConfiguration(ctx, q, p.AccountID, poolconfigurations.Observe)
	if err != nil {
		return runtime, nil, err
	}
	if current.Scope != p.Scope || current.ActorID != p.ActorID {
		return runtime, nil, fleeterror.NewForbiddenError("equipment access changed")
	}
	if err := lockPoolBindings(ctx, q, p.Scope, targets, true); err != nil {
		return runtime, nil, err
	}
	owners, err := c.reviewer.authority.AuthorizePoolEquipmentBatch(ctx, q, p.Scope, p.ActorID, targets)
	if err != nil {
		return runtime, nil, err
	}
	var deviceIDs, installations []int64
	for _, target := range targets {
		if target.Kind == poolconfigurations.TargetInstallation {
			installations = append(installations, target.ID)
		} else {
			deviceIDs = append(deviceIDs, target.ID)
		}
	}
	assignments, err := q.ListActivePoolAssignmentsForTargets(ctx, sqlc.ListActivePoolAssignmentsForTargetsParams{OrganizationID: p.Scope.OrganizationID, DeviceIds: deviceIDs, InstallationIds: installations})
	if err != nil {
		return runtime, nil, err
	}
	assigned := make(map[poolconfigurations.AssignmentTarget]bool, len(assignments))
	for _, assignment := range assignments {
		assigned[poolconfigurations.AssignmentTarget{Kind: assignment.TargetKind, ID: assignment.TargetID}] = true
	}
	bindings := make(map[int]devicePoolBinding, len(owners))
	for i, owner := range owners {
		if !assigned[poolconfigurations.AssignmentTarget{Kind: targets[i].Kind, ID: targets[i].ID}] {
			bindings[i] = devicePoolBinding{owner: owner}
		}
	}
	boards, err := q.ListPoolSelectionBoards(ctx, sqlc.ListPoolSelectionBoardsParams{OrgID: p.Scope.OrganizationID, InstallationIds: installations})
	if err != nil {
		return runtime, nil, err
	}
	byID := make(map[int64]BoardBinding, len(boards))
	for _, board := range boards {
		if board.Ready && board.DeviceID == targets[0].DeviceID {
			byID[board.InstallationID] = BoardBinding{InstallationID: board.InstallationID, HashboardID: board.HashboardID, ModuleID: board.ModuleID, Position: board.Position, MAC: board.MacAddress}
		}
	}
	for i, binding := range bindings {
		if targets[i].Kind == poolconfigurations.TargetInstallation {
			board, ok := byID[targets[i].ID]
			if !ok {
				delete(bindings, i)
				continue
			}
			binding.board = board
			bindings[i] = binding
		}
	}
	runtime, err = q.GetPoolConfigurationRuntime(ctx, sqlc.GetPoolConfigurationRuntimeParams{OrgID: p.Scope.OrganizationID, ID: targets[0].DeviceID})
	return runtime, bindings, err
}

func (c *Checker) readControllerPools(ctx context.Context, p poolconfigurations.Principal, targets []poolconfigurations.AssignmentTarget) ([]poolconfigurations.CheckResult, error) {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	var initial map[int]devicePoolBinding
	runtime, err := db.WithTransaction(ctx, c.conn, func(q sqlc.Querier) (sqlc.GetPoolConfigurationRuntimeRow, error) {
		r, bindings, err := c.authorizeDevicePoolRead(ctx, q, p, targets)
		initial = bindings
		return r, err
	})
	if err != nil {
		return nil, err
	}
	if len(initial) == 0 {
		return nil, nil
	}
	results := make([]poolconfigurations.CheckResult, len(targets))
	for i := range results {
		results[i] = poolconfigurations.CheckResult{State: codeUnavailable, ResultCode: "check_unavailable"}
	}
	err = c.writes.WithHostObservation(ctx, p.Scope.OrganizationID, targets[0].DeviceID, runtime.Epoch, func(ctx context.Context) error {
		return c.writes.WithBindingTransaction(ctx, func(ctx context.Context, q sqlc.Querier) error {
			runtime, bindings, err := c.authorizeDevicePoolRead(ctx, q, p, targets)
			if err != nil {
				return err
			}
			for i, binding := range bindings {
				if previous, ok := initial[i]; !ok || binding != previous {
					delete(bindings, i)
				}
			}
			if len(bindings) == 0 {
				return nil
			}
			return c.writes.WithBindingObservation(ctx, targets[0].DeviceID, func(sqlc.Querier) error { return nil }, func(ctx context.Context) error {
				miner, err := c.miners.GetMinerForCommand(ctx, targets[0].DeviceID)
				if err != nil {
					return err
				}
				var observed sdk.MiningGroupResult
				if commonpb.DeviceClass(runtime.DeviceClass) == commonpb.DeviceClass_DEVICE_CLASS_CONTAINER {
					groups, ok := miner.(sdk.DeviceMiningGroups)
					if !ok {
						return nil
					}
					observed, err = groups.MiningGroups(ctx, sdk.MiningGroupRequest{Action: "observe"})
					if err != nil {
						return err
					}
				}
				checkedAt := time.Now()
				for i, binding := range bindings {
					var reading boardPoolReadback
					if targets[i].Kind == poolconfigurations.TargetInstallation {
						reading = reportedBoardPools(observed, binding.board.MAC)
					} else {
						if commonpb.DeviceClass(runtime.DeviceClass) == commonpb.DeviceClass_DEVICE_CLASS_CONTAINER && (!observed.Supported || observed.Enabled || observed.Pending || observed.Suspended) {
							continue
						}
						pools, err := miner.GetMiningPools(ctx)
						if err != nil {
							return err
						}
						reading.Available = true
						for _, pool := range pools {
							reading.Pools = append(reading.Pools, poolconfigurations.PoolReading{Priority: pool.Priority, URL: pool.URL, Username: pool.Username})
						}
						slices.SortFunc(reading.Pools, func(a, b poolconfigurations.PoolReading) int { return int(a.Priority) - int(b.Priority) })
					}
					if !reading.Available {
						continue
					}
					results[i] = poolconfigurations.CheckResult{ReadbackAvailable: true, Reported: reading.Pools, CheckedAt: checkedAt}
					if len(reading.Pools) > 0 {
						results[i].State = "device_configuration"
					}
				}
				return nil
			})
		})
	})
	return results, err
}
