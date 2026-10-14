package command

import (
	"context"
	"database/sql"
	"fmt"
	"slices"
	"strings"
	"time"

	containerpb "github.com/block/proto-fleet/server/generated/grpc/containers/v1"
	gatewaypb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/commandtype"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

type miningScope uint8

const moduleIdentityUnavailableReason = "Module identity is unavailable or ambiguous. Check the module's board status and controller connection before retrying."

const (
	tankScope miningScope = iota
	moduleScope
)

type miningCommandTarget struct {
	ID, DeviceID                                int64
	Position                                    int32
	LastSeenAt, ObservedAt                      time.Time
	DeviceIdentifier, ContainerName, DriverName string
	SiteID                                      sql.NullInt64
	Manufacturer, Model                         sql.NullString
	Module                                      *sdk.ModuleMiningTarget
	ModuleIdentityConfirmed                     bool
}

func (row miningCommandTarget) identityConfirmed() bool {
	return row.Module == nil || (row.ModuleIdentityConfirmed && row.Module.Validate() == nil)
}

type miningOptions struct {
	CanStart, CanStop bool
	UnavailableReason string
}
type miningCommand struct {
	DeviceIdentifier, ContainerName, BatchIdentifier string
	TargetIDs                                        []int64
}

func (scope miningScope) noun() string {
	if scope == moduleScope {
		return "module"
	}
	return "tank"
}
func (scope miningScope) controlsUnavailableReason() string {
	noun := "Tank"
	if scope == moduleScope {
		noun = "Module"
	}
	return noun + " controls unsupported or topology stale; refresh before retrying."
}

func (scope miningScope) targetUnavailableReason(row miningCommandTarget) string {
	if !row.identityConfirmed() {
		return moduleIdentityUnavailableReason
	}
	return scope.controlsUnavailableReason()
}

func (scope miningScope) controlsUnsupportedReason() string {
	return scope.noun() + " mining controls are not supported by this controller"
}
func (scope miningScope) capability() string {
	if scope == moduleScope {
		return sdk.CapabilityModuleMining
	}
	return sdk.CapabilityTankMining
}
func (scope miningScope) commandType() commandtype.Type {
	if scope == moduleScope {
		return commandtype.SetModuleMining
	}
	return commandtype.SetTankMining
}
func (scope miningScope) eventType() string { return "set_" + scope.noun() + "_mining" }
func (scope miningScope) protocol() gatewaypb.CommandProtocolVersion {
	if scope == moduleScope {
		return gatewaypb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V5
	}
	return gatewaypb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V4
}
func (scope miningScope) upgradeMessage() string {
	return fmt.Sprintf("%s mining requires Fleet Node command protocol version %d; upgrade Fleet Node before retrying", scope.noun(), scope.protocol())
}
func (scope miningScope) ids(ids []int64) ([]int64, error) {
	if len(ids) == 0 || len(ids) > 100 {
		return nil, fleeterror.NewInvalidArgumentError(fmt.Sprintf("select 1–100 %ss", scope.noun()))
	}
	ids = slices.Clone(ids)
	slices.Sort(ids)
	ids = slices.Compact(ids)
	if ids[0] <= 0 {
		return nil, fleeterror.NewInvalidArgumentError(scope.noun() + " IDs must be positive")
	}
	return ids, nil
}
func (scope miningScope) resolve(ctx context.Context, q sqlc.Querier, ids []int64, orgID int64) ([]miningCommandTarget, error) {
	if scope == tankScope {
		return resolveTankCommandTargets(ctx, q, ids, orgID)
	}
	rows, err := q.ListModuleCommandTargets(ctx, sqlc.ListModuleCommandTargetsParams{ModuleIds: ids, OrgID: orgID})
	if err != nil {
		return nil, err
	}
	if len(rows) != len(ids) {
		return nil, fleeterror.NewNotFoundError("selected module unavailable")
	}
	targets := make([]miningCommandTarget, len(rows))
	for i, row := range rows {
		targets[i] = miningCommandTarget{
			ID: row.ModuleID, DeviceID: row.DeviceID, Position: row.TankPosition,
			LastSeenAt: row.LastSeenAt, ObservedAt: row.ObservedAt,
			DeviceIdentifier: row.DeviceIdentifier, SiteID: row.SiteID, ContainerName: row.ContainerName,
			Manufacturer: row.Manufacturer, Model: row.Model, DriverName: row.DriverName,
			Module:                  &sdk.ModuleMiningTarget{Tank: row.TankPosition, Module: row.Position, ReportedID: row.ReportedID},
			ModuleIdentityConfirmed: row.IdentityConfirmed,
		}
	}
	return targets, nil
}

func (p scopedMiningPayload) ids() []int64 {
	if len(p.ModuleIDs) > 0 {
		return p.ModuleIDs
	}
	return p.TankIDs
}
func (p scopedMiningPayload) activityDescription(containerName string) string {
	if len(p.ModuleIDs) > 0 {
		coordinates := make([]string, len(p.Modules))
		for i, module := range p.Modules {
			coordinates[i] = fmt.Sprintf("tank %d/module %d", module.Tank, module.Module)
		}
		return fmt.Sprintf("%s modules [%s]: mining=%t", containerName, strings.Join(coordinates, ", "), *p.Mining)
	}
	return fmt.Sprintf("%s tanks %v: mining=%t", containerName, p.Positions, *p.Mining)
}
func (p scopedMiningPayload) Validate() error {
	if len(p.ModuleIDs) > 0 {
		if len(p.TankIDs) > 0 || len(p.Positions) > 0 || len(p.Modules) != len(p.ModuleIDs) {
			return fmt.Errorf("invalid module scope")
		}
		return (sdk.ModuleMiningRequest{Modules: p.Modules, Mining: p.Mining}).Validate()
	}
	if len(p.Modules) > 0 || len(p.TankIDs) != len(p.Positions) {
		return fmt.Errorf("invalid tank scope")
	}
	return p.TankMiningRequest.Validate()
}
func (p scopedMiningPayload) matches(row miningCommandTarget, i int) bool {
	if len(p.ModuleIDs) > 0 {
		return row.Module != nil && *row.Module == p.Modules[i]
	}
	return row.Position == p.Positions[i]
}

func (s *Service) GetModuleMiningOptions(ctx context.Context, ids []int64, authorize miningAuthorization) (*containerpb.GetModuleMiningOptionsResponse, error) {
	r, err := s.getMiningOptions(ctx, ids, authorize, moduleScope)
	if err != nil {
		return nil, err
	}
	return &containerpb.GetModuleMiningOptionsResponse{CanStart: r.CanStart, CanStop: r.CanStop, UnavailableReason: r.UnavailableReason}, nil
}
func (s *Service) SetModuleMining(ctx context.Context, ids []int64, mining *bool, authorize miningAuthorization) (*containerpb.SetModuleMiningResponse, error) {
	r, err := s.setMining(ctx, ids, mining, authorize, moduleScope)
	if err != nil {
		return nil, err
	}
	result := &containerpb.SetModuleMiningResponse{}
	for _, c := range r {
		result.Commands = append(result.Commands, &containerpb.ModuleMiningCommand{DeviceIdentifier: c.DeviceIdentifier, ContainerName: c.ContainerName, ModuleIds: c.TargetIDs, BatchIdentifier: c.BatchIdentifier})
	}
	return result, nil
}
func (es *ExecutionService) executeModuleMining(ctx context.Context, deviceID, orgID int64, payload scopedMiningPayload, controller sdk.DeviceModuleMiner) error {
	return es.executeScopedMining(ctx, deviceID, orgID, payload, moduleScope, func(ctx context.Context) error {
		return controller.SetModuleMining(ctx, sdk.ModuleMiningRequest{Modules: payload.Modules, Mining: payload.Mining})
	})
}
