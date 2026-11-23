package command

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/commandtype"
	"github.com/block/proto-fleet/server/internal/domain/devicewrite"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/control"
	"github.com/block/proto-fleet/server/internal/domain/miner/remotenode"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

const (
	powerFeedControlUnsupportedReason = "Power feed control is not supported by this controller."
	powerFeedControlPendingReason     = "A power feed command is already pending for this container."
	powerFeedControlMappingReason     = "Controls are off because this module is not mapped to exactly one power feed."
	powerFeedControlNoReadingReason   = "Controls are off because the feed has no current reading."
	powerFeedControlStateReason       = "Controls are off because the feed's state is unknown."
	powerFeedControlCycleUnknown      = "Controls are off because the controller's power recovery state is unknown."
	powerFeedControlCycleActive       = "Controls are off while a power recovery cycle is in progress."
	powerFeedControlIdentityReason    = "Module identity is unconfirmed. Refresh the module and check its controller connection before retrying."
	powerFeedControlChangedReason     = "The module moved or was replaced after you confirmed it. Refresh before retrying."
	powerFeedControlUpgradeReason     = "Controls are off until this controller's Fleet Node is upgraded."
	powerFeedControlCheckFailedReason = "Controls are off because their status could not be checked. Refresh to try again."
	powerFeedControlCommandType       = "ControlPowerFeed"
)

var powerFeedControlUpgradeMessage = fmt.Sprintf("power feed control requires Fleet Node command protocol version %d; upgrade Fleet Node before retrying", remotenode.PowerFeedWriteCommandProtocolVersion.Number())

// modulePowerFeedPermission reports whether the caller may control feeds at
// the module's current site. It never fails the read.
type modulePowerFeedPermission func(context.Context, *int64) bool

// PowerFeedControl lists the writes that would change the feed's current
// state and, when they cannot run now, the reason.
type PowerFeedControl struct {
	Actions           []string
	UnavailableReason string
}

// powerFeedControlMaxAge bounds how long after admission a queued feed write
// may still be handed to the controller's device handle. A write left pending
// behind other commands or across a restart is refused rather than cutting
// power long after the operator confirmed it. A Fleet Node's command slot wait
// and the plugin's own reads and write come after this check, so the last
// possible write is later than this bound; together they stay inside the
// dialog's 120 second result window, as containerAlarmMaxAge does.
const powerFeedControlMaxAge = 60 * time.Second

// Authorization scope and admission time stay in Fleet's queue; only Request
// reaches the device.
type modulePowerFeedPayload struct {
	Request    sdk.ContainerPowerFeedRequest `json:"request"`
	OrgID      int64                         `json:"org_id"`
	SiteID     int64                         `json:"site_id"`
	ModuleID   int64                         `json:"module_id"`
	AdmittedAt time.Time                     `json:"admitted_at"`
}

func powerFeedMappingUnique(m sdk.PowerFeedMapping) bool {
	return m.FeedID != "" && (m.Confidence == sdk.PowerFeedMappingWiringVerified || m.Confidence == sdk.PowerFeedMappingConfigured)
}

func (s *Service) powerFeedControlSupported(ctx context.Context, row sqlc.ListModuleCommandTargetsRow) bool {
	return s.pluginCaps != nil && s.pluginCaps.GetRawCapabilitiesForDevice(ctx, row.DriverName, row.Manufacturer.String, row.Model.String)[sdk.CapabilityContainerPowerFeedControl]
}

// powerFeedControl derives the control state from one live inspection. The
// controller re-checks the mapping, reading and recovery cycle under its write
// lock before it writes, so this only decides what the operator is offered.
func (s *Service) powerFeedControl(ctx context.Context, q sqlc.Querier, orgID int64, row sqlc.ListModuleCommandTargetsRow, i sdk.ModulePowerFeedInspection) (*PowerFeedControl, error) {
	out := &PowerFeedControl{Actions: sdk.PowerFeedWritesFor(i.Feed)}
	pending, err := q.HasPendingContainerCommand(ctx, sqlc.HasPendingContainerCommandParams{DeviceID: row.DeviceID, CommandTypes: []string{powerFeedControlCommandType}})
	if err != nil {
		return nil, err
	}
	upgrade, err := s.fleetNodeUpgradeRequired(ctx, orgID, row.DeviceID, row.DeviceIdentifier, remotenode.PowerFeedWriteCommandProtocolVersion, false)
	if err != nil {
		return nil, err
	}
	switch {
	case !s.powerFeedControlSupported(ctx, row):
		out.UnavailableReason = powerFeedControlUnsupportedReason
	case upgrade:
		out.UnavailableReason = powerFeedControlUpgradeReason
	case !row.IdentityConfirmed:
		out.UnavailableReason = powerFeedControlIdentityReason
	case !powerFeedMappingUnique(i.Mapping):
		out.UnavailableReason = powerFeedControlMappingReason
	case i.Feed == nil:
		out.UnavailableReason = powerFeedControlNoReadingReason
	case !i.RecoveryCycleKnown:
		out.UnavailableReason = powerFeedControlCycleUnknown
	case i.RecoveryCycle != nil:
		out.UnavailableReason = powerFeedControlCycleActive
	case pending:
		out.UnavailableReason = powerFeedControlPendingReason
	case len(out.Actions) == 0:
		out.UnavailableReason = powerFeedControlStateReason
	}
	return out, nil
}

func powerFeedRequest(row sqlc.ListModuleCommandTargetsRow, action, feedID string) sdk.ContainerPowerFeedRequest {
	return sdk.ContainerPowerFeedRequest{
		Action: action, FeedID: feedID,
		Module: sdk.ContainerPowerFeedModule{Tank: row.TankPosition, Module: row.Position, ReportedID: row.ReportedID},
	}
}

func moduleCommandTarget(ctx context.Context, q sqlc.Querier, orgID, moduleID int64) (sqlc.ListModuleCommandTargetsRow, error) {
	rows, err := q.ListModuleCommandTargets(ctx, sqlc.ListModuleCommandTargetsParams{ModuleIds: []int64{moduleID}, OrgID: orgID})
	if err != nil {
		return sqlc.ListModuleCommandTargetsRow{}, err
	}
	if len(rows) != 1 {
		return sqlc.ListModuleCommandTargetsRow{}, fleeterror.NewNotFoundError("module not found")
	}
	return rows[0], nil
}

// ControlModulePowerFeed queues one write to the feed the operator confirmed,
// on the controller they inspected. Feed IDs repeat across containers, so a
// module re-homed since the inspection is refused here.
// The controller refuses it unless that feed is still the module's unique
// feed in the state the action changes, with no recovery cycle pending.
func (s *Service) ControlModulePowerFeed(ctx context.Context, moduleID int64, deviceIdentifier, action, feedID string, authorize modulePowerFeedAuthorization) (*CommandResult, string, error) {
	if moduleID <= 0 {
		return nil, "", fleeterror.NewInvalidArgumentError("module ID must be positive")
	}
	if !sdk.IsContainerPowerFeedWrite(action) {
		return nil, "", fleeterror.NewInvalidArgumentError("unsupported power feed action")
	}
	if authorize == nil {
		return nil, "", fleeterror.NewForbiddenError("power feed authorization required")
	}
	info, err := session.GetInfo(ctx)
	if err != nil {
		return nil, "", err
	}
	var identifier string
	result, err := withContainerCommandTransaction(ctx, s, func(txCtx context.Context, q sqlc.Querier) (*CommandResult, error) {
		row, err := moduleCommandTarget(txCtx, q, info.OrganizationID, moduleID)
		if err != nil {
			return nil, err
		}
		if row.DeviceIdentifier != deviceIdentifier {
			return nil, fleeterror.NewFailedPreconditionError(powerFeedControlChangedReason)
		}
		target, err := q.LockContainerCommandTarget(txCtx, sqlc.LockContainerCommandTargetParams{DeviceIdentifier: row.DeviceIdentifier, OrgID: info.OrganizationID})
		if err != nil {
			return nil, fleeterror.NewNotFoundError("module controller unavailable")
		}
		// Re-resolve under the controller ownership lock, including site.
		row, err = moduleCommandTarget(txCtx, q, info.OrganizationID, moduleID)
		if err != nil {
			return nil, err
		}
		if row.DeviceID != target.ID {
			return nil, fleeterror.NewFailedPreconditionError(powerFeedControlChangedReason)
		}
		var site *int64
		if row.SiteID.Valid {
			site = &row.SiteID.Int64
		}
		if err := authorize(txCtx, site); err != nil {
			return nil, err
		}
		if !s.powerFeedControlSupported(txCtx, row) {
			return nil, fleeterror.NewFailedPreconditionError(powerFeedControlUnsupportedReason)
		}
		if !row.IdentityConfirmed {
			return nil, fleeterror.NewFailedPreconditionError(powerFeedControlIdentityReason)
		}
		req := powerFeedRequest(row, action, feedID)
		if err := req.Validate(); err != nil {
			return nil, fleeterror.NewInvalidArgumentError(err.Error())
		}
		upgrade, err := s.fleetNodeUpgradeRequired(db.WithTxQueries(txCtx, q), info.OrganizationID, row.DeviceID, row.DeviceIdentifier, remotenode.PowerFeedWriteCommandProtocolVersion, true)
		if err != nil {
			return nil, err
		}
		if upgrade {
			return nil, fleeterror.NewFailedPreconditionError(powerFeedControlUpgradeMessage)
		}
		pending, err := q.HasPendingContainerCommand(txCtx, sqlc.HasPendingContainerCommandParams{DeviceID: row.DeviceID, CommandTypes: []string{powerFeedControlCommandType}})
		if err != nil {
			return nil, err
		}
		if pending {
			return nil, fleeterror.NewFailedPreconditionError(powerFeedControlPendingReason)
		}
		identifier = row.DeviceIdentifier
		payload := modulePowerFeedPayload{Request: req, OrgID: info.OrganizationID, SiteID: row.SiteID.Int64, ModuleID: row.ModuleID, AdmittedAt: time.Now()}
		description := fmt.Sprintf("%s tank %d/module %d: power feed %s %s", row.ContainerName, row.TankPosition, row.Position, feedID, strings.ReplaceAll(action, "_", " "))
		return s.queueContainerCommand(txCtx, row.DeviceIdentifier, &Command{commandType: commandtype.ControlPowerFeed, payload: payload}, "control_power_feed", description)
	})
	if err != nil {
		return nil, "", err
	}
	return result, identifier, nil
}

// executeModulePowerFeed sends one queued feed write. Every failure is
// terminal: a write older than powerFeedControlMaxAge, a refusal, or a failure
// before dispatch is reported as not sent, and every other failure is an
// unconfirmed outcome. The queue never sends it later.
func (es *ExecutionService) executeModulePowerFeed(ctx context.Context, deviceID, orgID int64, payload modulePowerFeedPayload, controller sdk.DeviceContainerPowerFeed) error {
	if payload.OrgID <= 0 || payload.OrgID != orgID || payload.SiteID < 0 || payload.ModuleID <= 0 || payload.Request.Validate() != nil || payload.Request.ReadOnly() {
		return fleeterror.NewFailedPreconditionError("invalid power feed write or authorization scope")
	}
	err := es.executeContainerWrite(ctx, containerWriteScope{
		deviceID: deviceID, orgID: orgID, siteID: payload.SiteID,
		noun: "module", action: "power feed", upgradeMessage: powerFeedControlUpgradeMessage,
	}, func(ctx context.Context, q sqlc.Querier) error {
		row, err := moduleCommandTarget(ctx, q, orgID, payload.ModuleID)
		if fleeterror.IsNotFoundError(err) {
			return fleeterror.NewFailedPreconditionError(powerFeedControlChangedReason)
		}
		if err != nil {
			return err
		}
		if row.DeviceID != deviceID || row.SiteID.Int64 != payload.SiteID || !row.IdentityConfirmed ||
			powerFeedRequest(row, payload.Request.Action, payload.Request.FeedID) != payload.Request {
			return fleeterror.NewFailedPreconditionError(powerFeedControlChangedReason)
		}
		return nil
	}, func(ctx context.Context) error {
		// Checked after the placement lock and write gate, right before the write.
		if payload.AdmittedAt.IsZero() || time.Since(payload.AdmittedAt) > powerFeedControlMaxAge {
			return devicewrite.NotDispatched(fleeterror.NewFailedPreconditionError("power feed command expired before it was sent; check the feed before retrying"))
		}
		result, err := controller.ContainerPowerFeed(ctx, payload.Request)
		if err == nil {
			err = result.Validate(payload.Request)
		}
		return powerFeedNotRetried(err)
	})
	if err == nil || fleeterror.IsFailedPreconditionError(err) {
		return err
	}
	// Only a failure before dispatch reaches here (preparation failed). Make it
	// terminal instead of letting the queue send the write on a later retry.
	slog.WarnContext(ctx, "power feed command not sent", "device_id", deviceID, "action", payload.Request.Action, "error", err)
	return powerFeedNotSent()
}

// powerFeedNotRetried makes every refusal final. A queued retry could write
// long after the operator confirmed, against a feed they no longer see. The
// controller's own refusal arrives as FailedPrecondition, directly or through
// a Fleet Node, and keeps its reason.
func powerFeedNotRetried(err error) error {
	if err == nil || !devicewrite.IsNotDispatched(err) || fleeterror.IsFailedPreconditionError(err) || errors.Is(err, control.ErrCommandProtocolUpgradeRequired) {
		return err
	}
	return powerFeedNotSent()
}

func powerFeedNotSent() error {
	return devicewrite.NotDispatched(fleeterror.NewFailedPreconditionError("power feed command was not sent; refresh before retrying"))
}
