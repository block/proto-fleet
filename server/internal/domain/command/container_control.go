package command

import (
	"context"
	"database/sql"
	"errors"
	"time"

	containerpb "github.com/block/proto-fleet/server/generated/grpc/containers/v1"
	pb "github.com/block/proto-fleet/server/generated/grpc/minercommand/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/commandtype"
	"github.com/block/proto-fleet/server/internal/domain/containers"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

// SetContainerControl resolves the current owning controller and site itself.
// Authorization and duplicate detection share the device lock with queue insertion.
func (s *Service) SetContainerControl(ctx context.Context, identifier string, req sdk.ContainerControlRequest, authorize func(context.Context, *int64) error) (*CommandResult, error) {
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
		pending, err := q.HasPendingContainerCommand(txCtx, sqlc.HasPendingContainerCommandParams{DeviceID: target.ID, CommandTypes: []string{"SetContainerControl"}})
		if err != nil {
			return nil, err
		}
		if pending {
			return nil, fleeterror.NewFailedPreconditionError("an equipment command is already pending for this container")
		}
		rows, err := q.ListContainers(txCtx, sqlc.ListContainersParams{OrgID: info.OrganizationID, DeviceIdentifiers: []string{identifier}})
		if err != nil {
			return nil, err
		}
		projected, err := containers.Project(rows, func(*int64) (bool, error) { return true, nil })
		if err != nil {
			return nil, err
		}
		if len(projected) != 1 || !equipmentWritable(projected[0].Controls, req, time.Now()) {
			return nil, fleeterror.NewFailedPreconditionError("equipment control unsupported or current state unavailable; refresh before retrying")
		}
		return s.queueContainerCommand(txCtx, identifier, &Command{commandType: commandtype.SetContainerControl, payload: req}, "set_container_control", "Equipment "+req.Name+" desired state changed")
	})
}

func equipmentWritable(c *containerpb.ContainerControls, req sdk.ContainerControlRequest, now time.Time) bool {
	if c == nil || c.Stale || c.ObservedAt == nil || now.Sub(c.ObservedAt.AsTime()) > 2*time.Minute || c.ObservedAt.AsTime().After(now.Add(5*time.Second)) {
		return false
	}
	if req.Name == "main_circuit_breaker" {
		return c.MainCircuitBreakerSupported != nil && *c.MainCircuitBreakerSupported && c.MainCircuitBreakerClosed != nil
	}
	if c.Supported == nil || !*c.Supported {
		return false
	}
	for _, control := range c.Controls {
		if control.Name == req.Name {
			return control.Enabled != nil
		}
	}
	return false
}

// GetContainerControlResult exposes the existing batch result only after the
// container's current site and the batch's single controller target are checked.
func (s *Service) GetContainerControlResult(ctx context.Context, identifier, batch string, authorize func(context.Context, *int64) error) (*pb.GetCommandBatchDeviceResultsResponse, error) {
	if authorize == nil {
		return nil, fleeterror.NewForbiddenError("authorization required")
	}
	info, err := session.GetInfo(ctx)
	if err != nil {
		return nil, err
	}
	return db.WithTransaction(ctx, s.conn, func(q sqlc.Querier) (*pb.GetCommandBatchDeviceResultsResponse, error) {
		target, err := q.LockContainerCommandTarget(ctx, sqlc.LockContainerCommandTargetParams{DeviceIdentifier: identifier, OrgID: info.OrganizationID})
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
		if err := authorize(ctx, site); err != nil {
			return nil, err
		}
		owns, err := q.ContainerOwnsCommandBatch(ctx, sqlc.ContainerOwnsCommandBatchParams{BatchIdentifier: batch, OrgID: sql.NullInt64{Int64: info.OrganizationID, Valid: true}, DeviceID: target.ID})
		if err != nil {
			return nil, err
		}
		if !owns {
			return nil, fleeterror.NewNotFoundError("equipment command not found")
		}
		return s.GetCommandBatchDeviceResults(ctx, &pb.GetCommandBatchDeviceResultsRequest{BatchIdentifier: batch})
	})
}
