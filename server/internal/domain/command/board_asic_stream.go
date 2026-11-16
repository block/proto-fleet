package command

import (
	"context"
	"errors"
	"log/slog"
	"time"

	containerpb "github.com/block/proto-fleet/server/generated/grpc/containers/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// boardASICStreamLifetime ends each stream so a reopened stream re-resolves
// the caller's permissions and the board's placement.
// boardASICStreamIdleTimeout ends a stream whose controller has gone silent,
// so the caller reconnects and the plugin can select a controller again. The
// controller sends about one frame per second. Both are variables for tests.
var (
	boardASICStreamLifetime    = 5 * time.Minute
	boardASICStreamIdleTimeout = 15 * time.Second
)

var errBoardASICStreamIdle = errors.New("board ASIC stream idle")

// BoardASICStreamWriteBudget is how long one stream's response may take to
// write: its lifetime, plus the idle timeout for the last write to finish.
func BoardASICStreamWriteBudget() time.Duration {
	return boardASICStreamLifetime + boardASICStreamIdleTimeout
}

const (
	boardASICStreamChangedReason     = "This board moved or was replaced. Reopen it from its module."
	boardASICStreamIdentityReason    = "Board identity is unavailable. Refresh the module and check its controller connection."
	boardASICStreamUnsupportedReason = "Live ASIC readings are not supported by this controller."
	boardASICStreamFleetNodeReason   = "Live ASIC readings are not yet available for containers reached through a Fleet Node."
	boardASICStreamStoppedReason     = "The controller stopped sending ASIC readings."
)

// StreamBoardASICReadings relays one installed board's live ASIC readings to
// send. It returns nil only when the lifetime cap passes. A controller that
// ends the stream or goes silent is unavailable, so the caller backs off, and
// the caller's cancellation or send error is returned as is.
func (s *Service) StreamBoardASICReadings(ctx context.Context, target BoardIdentifyTarget, authorize boardIdentifyAuthorization, send func(*containerpb.StreamBoardASICReadingsResponse) error) error {
	if target.HashboardID <= 0 || target.InstallationID <= 0 {
		return fleeterror.NewInvalidArgumentError("a board with a known installation is required")
	}
	if authorize == nil {
		return fleeterror.NewForbiddenError("board read authorization required")
	}
	info, err := session.GetInfo(ctx)
	if err != nil {
		return err
	}
	row, err := db.WithTransaction(ctx, s.conn, func(q sqlc.Querier) (sqlc.ListBoardIdentifyTargetsRow, error) {
		rows, err := resolveBoardIdentifyTargets(ctx, q, info.OrganizationID, nil, []int64{target.HashboardID})
		if fleeterror.IsNotFoundError(err) {
			return sqlc.ListBoardIdentifyTargetsRow{}, fleeterror.NewFailedPreconditionError(boardASICStreamChangedReason)
		}
		if err != nil {
			return sqlc.ListBoardIdentifyTargetsRow{}, err
		}
		row := rows[0]
		if err := authorize(ctx, boardIdentifySite(row)); err != nil {
			return sqlc.ListBoardIdentifyTargetsRow{}, err
		}
		if row.InstallationID != target.InstallationID {
			return sqlc.ListBoardIdentifyTargetsRow{}, fleeterror.NewFailedPreconditionError(boardASICStreamChangedReason)
		}
		return row, nil
	})
	if err != nil {
		return err
	}
	board := boardIdentifyRequest(row, sdk.BoardIdentifyOff).Board
	if !row.IdentityConfirmed || !row.SlotMatches || row.Occupancy.String != "occupied" || board.Validate() != nil {
		return fleeterror.NewFailedPreconditionError(boardASICStreamIdentityReason)
	}
	if s.pluginCaps == nil || !s.pluginCaps.GetRawCapabilitiesForDevice(ctx, row.DriverName, row.Manufacturer.String, row.Model.String)[sdk.CapabilityBoardASICStream] {
		return fleeterror.NewUnimplementedError(boardASICStreamUnsupportedReason)
	}
	if s.executionService == nil || s.executionService.minerService == nil {
		return fleeterror.NewUnimplementedError("board readings unavailable")
	}
	miner, err := s.executionService.minerService.GetMinerForCommand(ctx, row.DeviceID)
	if err != nil {
		return err
	}
	// Fleet Node routed miners do not relay the stream yet.
	streamer, ok := miner.(sdk.DeviceBoardASICStreamer)
	if !ok {
		return fleeterror.NewFailedPreconditionError(boardASICStreamFleetNodeReason)
	}

	lifetimeCtx, cancel := context.WithTimeout(ctx, boardASICStreamLifetime)
	defer cancel()
	streamCtx, stop := context.WithCancelCause(lifetimeCtx)
	defer stop(nil)
	idle := time.AfterFunc(boardASICStreamIdleTimeout, func() { stop(errBoardASICStreamIdle) })
	defer idle.Stop()
	relay := boardASICRelay{hashboardID: row.HashboardID}
	var sendErr error
	err = streamer.StreamBoardASICReadings(streamCtx, board, func(frame sdk.BoardASICFrame) error {
		idle.Reset(boardASICStreamIdleTimeout)
		sendErr = send(relay.response(frame))
		return sendErr
	})
	switch {
	case sendErr != nil:
		return sendErr
	case ctx.Err() != nil:
		return fleeterror.NewCanceledError()
	case lifetimeCtx.Err() != nil:
		return nil
	case errors.Is(context.Cause(streamCtx), errBoardASICStreamIdle), err == nil:
		// A silent controller, or one that ended the stream itself.
		return fleeterror.NewUnavailableErrorf(boardASICStreamStoppedReason)
	}
	return boardASICStreamError(row.DeviceID, err)
}

// boardASICRelay joins the latest layout onto every frame, because the
// controller sends the layout only on the first frame and when it changes.
type boardASICRelay struct {
	hashboardID   int64
	asicsPerStack int32
	positions     map[int32]*containerpb.BoardASICPosition
}

func (r *boardASICRelay) response(frame sdk.BoardASICFrame) *containerpb.StreamBoardASICReadingsResponse {
	if frame.Layout != nil {
		r.asicsPerStack = frame.Layout.ASICsPerStack
		r.positions = make(map[int32]*containerpb.BoardASICPosition, len(frame.Layout.Positions))
		for _, p := range frame.Layout.Positions {
			r.positions[p.Index] = &containerpb.BoardASICPosition{StackId: p.StackID, StackIndex: p.StackIndex, X: p.X, Y: p.Y}
		}
	}
	out := &containerpb.StreamBoardASICReadingsResponse{HashboardId: r.hashboardID, AsicsPerStack: r.asicsPerStack, Asics: make([]*containerpb.BoardASICReading, 0, len(frame.ASICs))}
	if !frame.ObservedAt.IsZero() {
		out.ObservedAt = timestamppb.New(frame.ObservedAt)
	}
	for _, a := range frame.ASICs {
		out.Asics = append(out.Asics, &containerpb.BoardASICReading{
			Index: a.Index, TemperatureC: a.TemperatureC, HashrateGhs: a.HashrateGHs, VoltageV: a.VoltageV,
			FrequencyMhz: a.FrequencyMHz, PassRatePercent: a.PassRatePercent, ThermalQuarantined: a.ThermalQuarantined,
			Position: r.positions[a.Index],
		})
	}
	return out
}

// boardASICStreamError keeps controller and transport details out of the
// customer message; the server log has them.
func boardASICStreamError(deviceID int64, err error) error {
	var fleetErr fleeterror.FleetError
	if errors.As(err, &fleetErr) {
		return err
	}
	if sdk.IsBackendGenerationRejected(err) {
		return fleeterror.NewUnavailableErrorf("the controller connection changed; reconnecting")
	}
	//nolint:exhaustive // Every other controller outcome is reported as unavailable.
	switch status.Code(err) {
	case codes.FailedPrecondition:
		// The controller no longer places the confirmed board at its slot.
		return fleeterror.NewFailedPreconditionError(boardASICStreamIdentityReason)
	case codes.Unimplemented:
		return fleeterror.NewUnimplementedError(boardASICStreamUnsupportedReason)
	case codes.Canceled:
		return fleeterror.NewCanceledError()
	default:
		slog.Warn("board ASIC stream failed", "device_id", deviceID, "code", status.Code(err).String(), "error", err)
		return fleeterror.NewUnavailableErrorf(boardASICStreamStoppedReason)
	}
}
