// Package poolapplications translates reviewed pool assignments into device work.
package poolapplications

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"time"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/devicewrite"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/poolconfigurations"
	"github.com/block/proto-fleet/server/internal/domain/sv2"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

type Authority interface {
	AuthorizePoolEquipmentBatch(ctx context.Context, q sqlc.Querier, scope poolconfigurations.Scope, actorID int64, targets []poolconfigurations.AssignmentTarget) (map[int]string, error)
	VerifyPoolEquipmentCredentials(ctx context.Context, accountID, password string) (poolconfigurations.CredentialProof, error)
	AuthorizePoolEquipment(ctx context.Context, q sqlc.Querier, scope poolconfigurations.Scope, actorID int64, target poolconfigurations.AssignmentTarget) (string, error)
}

type Capabilities interface {
	GetRawCapabilitiesForDevice(ctx context.Context, driverName, manufacturer, model string) sdk.Capabilities
}

type Reviewer struct {
	authority    Authority
	capabilities Capabilities
}

func NewReviewer(authority Authority, capabilities Capabilities) *Reviewer {
	return &Reviewer{authority: authority, capabilities: capabilities}
}

// Binding is private dispatch context. It deliberately excludes passwords and
// observation timestamps: refreshing unchanged telemetry must not invalidate a
// review. Ownership generations, physical identity, mode and worker inputs do.
type Binding struct {
	Version      int
	Label        string
	Ownership    string
	Device       sqlc.GetPoolConfigurationRuntimeRow
	Usernames    []string
	Board        *BoardBinding
	MiningGroups bool
}

type BoardBinding struct {
	InstallationID, HashboardID, ModuleID int64
	Position                              int32
	MAC                                   string
}

func (r *Reviewer) ReviewTargets(ctx context.Context, q sqlc.Querier, scope poolconfigurations.Scope, actorID int64, targets []poolconfigurations.AssignmentTarget, slots []poolconfigurations.Endpoint) ([]poolconfigurations.TargetApproval, error) {
	return r.reviewTargets(ctx, q, scope, actorID, targets, slots, true)
}

// ReviewBindings rechecks authority, physical identity and configuration inputs
// without requiring a recent controller observation. Execution must check live
// controller mode under its physical admission before dispatching any mutation.
func (r *Reviewer) ReviewBindings(ctx context.Context, q sqlc.Querier, scope poolconfigurations.Scope, actorID int64, targets []poolconfigurations.AssignmentTarget, slots []poolconfigurations.Endpoint) ([]poolconfigurations.TargetApproval, error) {
	return r.reviewTargets(ctx, q, scope, actorID, targets, slots, false)
}

func (r *Reviewer) reviewTargets(ctx context.Context, q sqlc.Querier, scope poolconfigurations.Scope, actorID int64, targets []poolconfigurations.AssignmentTarget, slots []poolconfigurations.Endpoint, requireReadiness bool) ([]poolconfigurations.TargetApproval, error) {
	if r.authority == nil || r.capabilities == nil {
		return nil, fleeterror.NewInternalError("equipment review is unavailable")
	}
	if len(slots) < 1 || len(slots) > 3 {
		return nil, fleeterror.NewInvalidArgumentError("pool configurations require 1 to 3 pools")
	}
	if err := lockReviewBindings(ctx, q, scope, targets); err != nil {
		return nil, err
	}
	approvals := make([]poolconfigurations.TargetApproval, 0, len(targets))
	for _, target := range targets {
		owner, err := r.authority.AuthorizePoolEquipment(ctx, q, scope, actorID, target)
		if err != nil {
			return nil, err
		}
		device, err := q.GetPoolConfigurationRuntime(ctx, sqlc.GetPoolConfigurationRuntimeParams{OrgID: scope.OrganizationID, ID: target.DeviceID})
		if err != nil {
			return nil, err
		}
		if device.State != "host" || len(device.IdentityKeys) == 0 {
			return nil, fleeterror.NewFailedPreconditionError("equipment is reserved for another operation")
		}
		caps := r.capabilities.GetRawCapabilitiesForDevice(ctx, device.DriverName, device.Manufacturer, device.Model)
		if !caps[sdk.CapabilityPoolConfig] || len(slots) > 1 && !caps[sdk.CapabilityPoolPriority] {
			return nil, fleeterror.NewFailedPreconditionError("equipment does not support this ordered pool configuration")
		}
		for _, slot := range slots {
			if sv2.IsSV2URL(slot.URL) && !caps[sdk.CapabilityNativeStratumV2] {
				return nil, fleeterror.NewFailedPreconditionError("equipment does not support native Stratum V2 pool destinations")
			}
		}
		binding := Binding{Version: 1, Label: device.WorkerName, Ownership: owner, Device: device, Usernames: make([]string, len(slots))}
		if commonpb.DeviceClass(device.DeviceClass) == commonpb.DeviceClass_DEVICE_CLASS_CONTAINER {
			state, err := q.LockMiningGroupState(ctx, sqlc.LockMiningGroupStateParams{DeviceID: target.DeviceID, OrganizationID: scope.OrganizationID})
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return nil, err
			}
			if err != nil {
				return nil, fleeterror.NewFailedPreconditionError("container mining-group mode is unknown")
			}
			if state.Status == "pending" || state.Status == stateApplying || state.Status == "blocked" {
				return nil, fleeterror.NewFailedPreconditionError("container has an unfinished mining-group operation")
			}
			// The reviewed target fixes the required mode. Execution validates it
			// against fresh hardware state, rather than an expiring cached read.
			binding.MiningGroups = target.Kind == poolconfigurations.TargetInstallation
			if requireReadiness {
				if !state.ObservedAt.Valid || time.Since(state.ObservedAt.Time) > 2*time.Minute || time.Until(state.ObservedAt.Time) > 5*time.Second {
					return nil, fleeterror.NewFailedPreconditionError("refresh container mining-group readiness before reviewing pools")
				}
				var observed sdk.MiningGroupResult
				if json.Unmarshal(state.Observed, &observed) != nil || !observed.Supported {
					return nil, fleeterror.NewFailedPreconditionError("container mining-group mode is unknown")
				}
				if observed.Pending || observed.Suspended {
					return nil, fleeterror.NewFailedPreconditionError("container has an unfinished mining-group operation")
				}
				if observed.Enabled != binding.MiningGroups {
					return nil, fleeterror.NewFailedPreconditionError("select whole containers with mining groups disabled, or installed hashboards with mining groups enabled")
				}
			}
			if target.Kind == poolconfigurations.TargetInstallation {
				boards, err := q.ListPoolSelectionBoards(ctx, sqlc.ListPoolSelectionBoardsParams{OrgID: scope.OrganizationID, InstallationIds: []int64{target.ID}})
				if err != nil {
					return nil, err
				}
				if len(boards) != 1 || !boards[0].Ready || boards[0].DeviceID != target.DeviceID {
					return nil, fleeterror.NewFailedPreconditionError("selected board installation changed or is stale")
				}
				board := boards[0]
				binding.Board = &BoardBinding{InstallationID: board.InstallationID, HashboardID: board.HashboardID, ModuleID: board.ModuleID, Position: board.Position, MAC: board.MacAddress}
				binding.Label = fmt.Sprintf("%s · Hashboard %d", board.ModuleName, board.Position)
			}
			seen := map[string]bool{}
			for _, slot := range slots {
				parsed, err := url.Parse(slot.URL)
				key := strings.ToLower(slot.URL)
				if err != nil || (parsed.Scheme != "stratum+tcp" && parsed.Scheme != "stratum+ssl") || seen[key] {
					return nil, fleeterror.NewFailedPreconditionError("container requires distinct TCP or TLS pool destinations")
				}
				if binding.MiningGroups && parsed.Scheme != "stratum+tcp" {
					return nil, fleeterror.NewFailedPreconditionError("mining groups require TCP pool destinations")
				}
				seen[key] = true
			}
		} else if target.Kind != poolconfigurations.TargetDevice {
			return nil, fleeterror.NewFailedPreconditionError("hashboard assignments require mining-group support")
		}
		for i, slot := range slots {
			binding.Usernames[i], err = poolconfigurations.RenderWorker(slot.Username, slot.WorkerTemplate, device.DeviceIdentifier, device.WorkerName)
			if err != nil {
				return nil, err
			}
		}
		encoded, err := json.Marshal(binding)
		if err != nil {
			return nil, fleeterror.NewInternalError("encode equipment review failed")
		}
		approvals = append(approvals, poolconfigurations.TargetApproval{Target: target, Binding: string(encoded)})
	}
	return approvals, nil
}

// Reviews take organization -> identities -> physical binding -> fence locks.
// Acquire the union of stripes once so batches cannot reverse their order.
// Dispatch reuses its committed identity admission, keeping only organization
// and device-specific locks through I/O so unrelated devices sharing a bucket
// can still be admitted. Recheck that admission after locking the binding.
func lockReviewBindings(ctx context.Context, q sqlc.Querier, scope poolconfigurations.Scope, targets []poolconfigurations.AssignmentTarget) error {
	return lockPoolBindings(ctx, q, scope, targets, false)
}

func lockPoolBindings(ctx context.Context, q sqlc.Querier, scope poolconfigurations.Scope, targets []poolconfigurations.AssignmentTarget, observation bool) error {
	if _, err := q.LockPoolEquipmentOrganization(ctx, scope.OrganizationID); err != nil {
		return err
	}
	devices := make([]int64, 0, len(targets))
	for _, target := range targets {
		devices = append(devices, target.DeviceID)
	}
	slices.Sort(devices)
	devices = slices.Compact(devices)
	keys := []string{}
	byDevice := map[int64][]string{}
	identities, err := q.ListPoolEquipmentIdentities(ctx, sqlc.ListPoolEquipmentIdentitiesParams{OrgID: scope.OrganizationID, DeviceIds: devices})
	if err != nil {
		return err
	}
	if len(identities) != len(devices) {
		return fleeterror.NewForbiddenError("equipment access denied")
	}
	for _, device := range identities {
		if len(device.IdentityKeys) == 0 {
			return fleeterror.NewFailedPreconditionError("equipment physical identity is unavailable")
		}
		keys = append(keys, device.IdentityKeys...)
		byDevice[device.ID] = device.IdentityKeys
	}
	admitted, err := devicewrite.RecheckIdentityAdmission(ctx, q, scope.OrganizationID, devices)
	if err != nil {
		return err
	}
	if !admitted {
		if err := q.LockDeviceWriteIdentities(ctx, keys); err != nil {
			return err
		}
	}
	drained, err := q.IsRetiredDeviceWriteDrained(ctx, keys)
	if err != nil {
		return err
	}
	if !drained {
		return fleeterror.NewFailedPreconditionError("equipment has an unresolved prior write")
	}
	for _, id := range devices {
		discovery, err := q.LockMinerBindingDevice(ctx, id)
		if err != nil {
			return err
		}
		if _, err := q.LockMinerBindingDiscovery(ctx, discovery); err != nil {
			return err
		}
		if _, err := q.LockMinerBindingPairing(ctx, id); err != nil {
			return err
		}
		if _, err := q.LockMinerBindingCredentials(ctx, id); err != nil {
			return err
		}
		if _, err := q.LockMinerBindingRoutes(ctx, id); err != nil {
			return err
		}
		current, err := q.GetDeviceWriteIdentityKeys(ctx, id)
		if err != nil {
			return err
		}
		if !slices.Equal(current, byDevice[id]) {
			return fleeterror.NewFailedPreconditionError("equipment binding changed; refresh and review again")
		}
	}
	if err := q.EnsureDeviceWriteFences(ctx, devices); err != nil {
		return err
	}
	fences, err := q.LockDeviceWriteFences(ctx, devices)
	if err != nil {
		return err
	}
	if len(fences) != len(devices) {
		return fleeterror.NewFailedPreconditionError("equipment admission changed")
	}
	for _, fence := range fences {
		if !observation {
			if err := devicewrite.RequireNoConflictingPoolWrites(ctx, q, scope.OrganizationID, fence.DeviceID); err != nil {
				return err
			}
		}
		if fence.OrganizationID != scope.OrganizationID {
			return fleeterror.NewForbiddenError("equipment access denied")
		}
	}
	if admitted {
		_, err := devicewrite.RecheckIdentityAdmission(ctx, q, scope.OrganizationID, devices)
		return err
	}
	return nil
}

func (r *Reviewer) VerifyPoolEquipmentCredentials(ctx context.Context, accountID, password string) (poolconfigurations.CredentialProof, error) {
	return r.authority.VerifyPoolEquipmentCredentials(ctx, accountID, password)
}
