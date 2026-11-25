package command

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"time"

	containerpb "github.com/block/proto-fleet/server/generated/grpc/containers/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/commandtype"
	"github.com/block/proto-fleet/server/internal/domain/containers"
	"github.com/block/proto-fleet/server/internal/domain/devicewrite"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/control"
	"github.com/block/proto-fleet/server/internal/domain/miner/remotenode"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

// containerAlarmMaxAge bounds how long after admission a queued pulse may still
// be sent. A reset can release a latched trip, so a command left pending by a
// restart or backlog is refused rather than fired long after the operator
// pressed it. It covers one 30 second worker attempt plus queue latency and
// stays inside the UI's 120 second readback window.
const containerAlarmMaxAge = 60 * time.Second

var containerAlarmUpgradeMessage = fmt.Sprintf("container alarm mute and reset require Fleet Node command protocol version %d; upgrade Fleet Node before retrying", remotenode.ContainerAlarmCommandProtocolVersion.Number())

const (
	containerAlarmUnsupportedMessage = "container alarm action unsupported or controller state unavailable; refresh before retrying"
	containerAlarmPendingMessage     = "an equipment command is already pending for this container"
)

// containerEquipmentCommandTypes are the plant writes that exclude each other
// on one container: only one may be queued or running at a time.
var containerEquipmentCommandTypes = []string{"SetContainerControl", "ContainerAlarmAction"}

// containerAlarmPayload pins the controller placement the operator acted on
// and when the command was admitted, so a stale pulse is never sent.
type containerAlarmPayload struct {
	sdk.ContainerAlarmRequest
	OrgID      int64     `json:"org_id"`
	SiteID     int64     `json:"site_id"`
	AdmittedAt time.Time `json:"admitted_at"`
}

// RunContainerAlarmAction queues one plant alarm mute or reset. It resolves
// the owning controller and its current site itself; authorization and the
// pending check share the device lock with queue insertion, so concurrent
// presses queue at most one pulse. The command is never retried and expires
// containerAlarmMaxAge after admission: success only acknowledges the pulse,
// and effect is confirmed from a later observation.
func (s *Service) RunContainerAlarmAction(ctx context.Context, identifier string, req sdk.ContainerAlarmRequest, authorize func(context.Context, *int64) error) (*CommandResult, error) {
	if err := req.Validate(); err != nil {
		return nil, fleeterror.NewInvalidArgumentError(err.Error())
	}
	if authorize == nil {
		return nil, fleeterror.NewForbiddenError("equipment write authorization required")
	}
	info, err := session.GetInfo(ctx)
	if err != nil {
		return nil, err
	}
	return withContainerCommandTransaction(ctx, s, func(txCtx context.Context, q sqlc.Querier) (*CommandResult, error) {
		target, err := q.LockContainerCommandTarget(txCtx, sqlc.LockContainerCommandTargetParams{DeviceIdentifier: identifier, OrgID: info.OrganizationID})
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fleeterror.NewNotFoundError("container not found")
		}
		if err != nil {
			return nil, err
		}
		var site *int64
		if target.SiteID.Valid {
			site = &target.SiteID.Int64
		}
		if err := authorize(txCtx, site); err != nil {
			return nil, err
		}
		pending, err := q.HasPendingContainerCommand(txCtx, sqlc.HasPendingContainerCommandParams{DeviceID: target.ID, CommandTypes: containerEquipmentCommandTypes})
		if err != nil {
			return nil, err
		}
		if pending {
			return nil, fleeterror.NewFailedPreconditionError(containerAlarmPendingMessage)
		}
		upgrade, err := s.fleetNodeUpgradeRequired(db.WithTxQueries(txCtx, q), info.OrganizationID, target.ID, identifier, remotenode.ContainerAlarmCommandProtocolVersion, true)
		if err != nil {
			return nil, err
		}
		if upgrade {
			return nil, fleeterror.NewFailedPreconditionError(containerAlarmUpgradeMessage)
		}
		rows, err := q.ListContainers(txCtx, sqlc.ListContainersParams{OrgID: info.OrganizationID, DeviceIdentifiers: []string{identifier}})
		if err != nil {
			return nil, err
		}
		projected, err := containers.Project(rows, func(*int64) (bool, error) { return true, nil })
		if err != nil {
			return nil, err
		}
		if len(projected) != 1 || !alarmActionWritable(projected[0].Controls, req, time.Now()) {
			return nil, fleeterror.NewFailedPreconditionError(containerAlarmUnsupportedMessage)
		}
		payload := containerAlarmPayload{ContainerAlarmRequest: req, OrgID: info.OrganizationID, SiteID: target.SiteID.Int64, AdmittedAt: time.Now()}
		description := "Container alarm " + req.Action + " requested"
		return s.queueContainerCommand(txCtx, identifier, &Command{commandType: commandtype.ContainerAlarmAction, payload: payload}, "container_alarm_action", description)
	})
}

// alarmActionWritable applies the same freshness rule as equipment writes and
// requires the controller to currently advertise the specific action.
func alarmActionWritable(c *containerpb.ContainerControls, req sdk.ContainerAlarmRequest, now time.Time) bool {
	if c == nil || c.Stale || c.ObservedAt == nil || now.Sub(c.ObservedAt.AsTime()) > 2*time.Minute || c.ObservedAt.AsTime().After(now.Add(5*time.Second)) {
		return false
	}
	switch req.Action {
	case sdk.ContainerAlarmActionMute:
		return c.AlarmMuteSupported != nil && *c.AlarmMuteSupported
	case sdk.ContainerAlarmActionReset:
		return c.AlarmResetSupported != nil && *c.AlarmResetSupported
	default:
		return false
	}
}

// executeContainerAlarm runs under the same placement lock as other container
// writes. Every failure is terminal, so the pulse is never sent later than the
// operator pressed it: a refusal or a failure before dispatch proves nothing
// was written and is reported with fixed text, and every other failure is an
// unconfirmed outcome. A command older than containerAlarmMaxAge is refused
// without being sent.
func (es *ExecutionService) executeContainerAlarm(ctx context.Context, deviceID, orgID int64, payload containerAlarmPayload, controller sdk.DeviceContainerAlarmController) error {
	if err := payload.Validate(); err != nil {
		return fleeterror.NewFailedPreconditionError("invalid container alarm payload")
	}
	if payload.OrgID != orgID {
		return fleeterror.NewFailedPreconditionError("container alarm command ownership or site changed")
	}
	notSent := func(err error) error {
		slog.WarnContext(ctx, "container alarm not sent", "device_id", deviceID, "action", payload.Action, "error", err)
		return devicewrite.NotDispatched(fleeterror.NewFailedPreconditionErrorf("container alarm %s was not sent; refresh the container before retrying", payload.Action))
	}
	err := es.executeContainerWrite(ctx, containerWriteScope{
		deviceID: deviceID, orgID: orgID, siteID: payload.SiteID,
		noun: "plant", action: "alarm " + payload.Action, upgradeMessage: containerAlarmUpgradeMessage,
	}, nil, func(ctx context.Context) error {
		// Checked after the placement lock and write gate, right before the pulse.
		if age := time.Since(payload.AdmittedAt); payload.AdmittedAt.IsZero() || age > containerAlarmMaxAge {
			return devicewrite.NotDispatched(fleeterror.NewFailedPreconditionErrorf("container alarm %s expired before it was sent; check the current alarm state before retrying", payload.Action))
		}
		err := controller.RunContainerAlarmAction(ctx, payload.ContainerAlarmRequest)
		if err == nil || !devicewrite.IsNotDispatched(err) || errors.Is(err, control.ErrCommandProtocolUpgradeRequired) {
			return err
		}
		// The controller's refusal text is logged, not stored in the command
		// result, which anyone who can read the result would see.
		return notSent(err)
	})
	if err == nil || fleeterror.IsFailedPreconditionError(err) {
		return err
	}
	// Only a failure before dispatch reaches here (preparation failed). Make it
	// terminal instead of letting the queue send the pulse on a later retry.
	return notSent(err)
}
