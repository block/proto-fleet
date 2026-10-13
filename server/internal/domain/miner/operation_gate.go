package miner

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/commandtype"
	"github.com/block/proto-fleet/server/internal/domain/devicewrite"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/miner/interfaces"
	"github.com/block/proto-fleet/server/internal/domain/miner/models"
	"github.com/block/proto-fleet/server/internal/domain/plugins"
)

func (s *Service) directWriteGate(binding sqlc.GetDeviceWithCredentialsAndIPByDeviceIdentifierRow) interfaces.MinerWriteGate {
	return func(ctx context.Context, kind commandtype.Type, call func(context.Context) error) error {
		if kind == commandtype.Unpair {
			hash, err := devicewrite.DirectBindingHash(binding)
			if err != nil {
				return err
			}
			if handled, err := s.writeService.DispatchDeletedUnpair(ctx, binding.OrgID, binding.ID, hash, call); handled {
				return err
			}
		}
		validate := func(q sqlc.Querier) error {
			fresh, err := q.GetDeviceWithCredentialsAndIPByDeviceIdentifier(ctx, binding.DeviceIdentifier)
			if err != nil {
				s.InvalidateMiner(models.DeviceIdentifier(binding.DeviceIdentifier))
				return operationBindingReadError(ctx, err)
			}
			if fresh != binding {
				s.InvalidateMiner(models.DeviceIdentifier(binding.DeviceIdentifier))
				return changedOperationBinding()
			}
			return nil
		}
		if err := s.writeService.ValidateBinding(ctx, validate); err != nil {
			return err
		}
		return s.writeService.WithHostAttempt(ctx, binding.OrgID, binding.ID, binding.WriteEpoch, kind, func(ctx context.Context) error {
			return s.writeService.WithBindingDispatch(ctx, binding.ID, validate, call)
		})
	}
}

func (s *Service) remoteWriteGate(binding sqlc.GetFleetNodeTelemetryRouteByDeviceIdentifierRow) interfaces.MinerWriteGate {
	return func(ctx context.Context, kind commandtype.Type, call func(context.Context) error) error {
		validate := func(q sqlc.Querier) error {
			fresh, err := q.GetFleetNodeTelemetryRouteByDeviceIdentifier(ctx, binding.DeviceIdentifier)
			// Telemetry's firmware label can change during an ordinary install. It does
			// not change the connection or encrypted identity bound to this handle.
			fresh.FirmwareVersion = binding.FirmwareVersion
			if err != nil {
				s.InvalidateMiner(models.DeviceIdentifier(binding.DeviceIdentifier))
				return operationBindingReadError(ctx, err)
			}
			if fresh != binding {
				s.InvalidateMiner(models.DeviceIdentifier(binding.DeviceIdentifier))
				return changedOperationBinding()
			}
			return nil
		}
		if err := s.writeService.ValidateBinding(ctx, validate); err != nil {
			return err
		}
		return s.writeService.WithHostAttempt(ctx, binding.OrgID, binding.ID, binding.WriteEpoch, kind, func(ctx context.Context) error {
			return s.writeService.WithBindingDispatch(ctx, binding.ID, validate, call)
		})
	}
}

func changedOperationBinding() error {
	return fleeterror.NewFailedPreconditionError("miner connection or identity changed; resolve a fresh handle before dispatch")
}

func operationBindingReadError(ctx context.Context, err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return changedOperationBinding()
	}
	if ctx.Err() != nil {
		return fmt.Errorf("miner connection revalidation canceled: %w", ctx.Err())
	}
	return fleeterror.NewInternalError("miner connection could not be revalidated")
}

// UnpairDeletedMiner uses a single committed deletion permit for the captured
// handle. All ordinary operations still require a live fresh binding.
func (s *Service) UnpairDeletedMiner(ctx context.Context, organizationID int64, handle interfaces.Miner) error {
	captured, ok := handle.(*directMinerHandle)
	if !ok || captured.owner != s || captured.organizationID != organizationID || captured.GetDriverName() != models.DriverNameProto {
		return changedOperationBinding()
	}
	return s.writeService.RunDeletionUnpair(ctx, organizationID, captured.deviceID, captured.bindingHash, captured.Unpair)
}

// Embedding the concrete adapter preserves its optional pool-readback, firmware,
// credential and curtailment interfaces as well as the ordinary Miner methods.
// The captured numeric incarnation cannot be reconstructed from GetID after
// rediscovery, because only live records have unique external identifiers.
type directMinerHandle struct {
	*plugins.PluginMiner
	owner          *Service
	deviceID       int64
	organizationID int64
	bindingHash    string
}
