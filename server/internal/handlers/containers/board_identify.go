package containers

import (
	"context"

	"connectrpc.com/connect"
	pb "github.com/block/proto-fleet/server/generated/grpc/containers/v1"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/command"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/handlers/middleware"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

// authorizeBoardIdentify requires the miner locator-LED permission plus the
// reads needed to see the board, all at the board's current site.
func authorizeBoardIdentify(ctx context.Context, site *int64) error {
	for _, permission := range []string{authz.PermSiteRead, authz.PermFleetRead, authz.PermMinerRead, authz.PermMinerBlinkLED} {
		if _, err := middleware.RequirePermission(ctx, permission, authz.ResourceContext{SiteID: site}); err != nil {
			return err
		}
	}
	return nil
}

func (h *Handler) GetBoardIdentifyOptions(ctx context.Context, req *connect.Request[pb.GetBoardIdentifyOptionsRequest]) (*connect.Response[pb.GetBoardIdentifyOptionsResponse], error) {
	if h.commands == nil {
		return nil, fleeterror.NewUnimplementedError("board commands unavailable")
	}
	result, err := h.commands.GetBoardIdentifyOptions(ctx, req.Msg.ModuleIds, req.Msg.HashboardIds, authorizeBoardIdentify)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(result), nil
}

func (h *Handler) SetBoardIdentify(ctx context.Context, req *connect.Request[pb.SetBoardIdentifyRequest]) (*connect.Response[pb.SetBoardIdentifyResponse], error) {
	if h.commands == nil {
		return nil, fleeterror.NewUnimplementedError("board commands unavailable")
	}
	var mode sdk.BoardIdentifyMode
	switch req.Msg.Mode {
	case pb.BoardIdentifyMode_BOARD_IDENTIFY_MODE_BLINK:
		mode = sdk.BoardIdentifyBlink
	case pb.BoardIdentifyMode_BOARD_IDENTIFY_MODE_OFF:
		mode = sdk.BoardIdentifyOff
	case pb.BoardIdentifyMode_BOARD_IDENTIFY_MODE_UNSPECIFIED:
		return nil, fleeterror.NewInvalidArgumentError("identify mode must be blink or off")
	default:
		return nil, fleeterror.NewInvalidArgumentError("identify mode must be blink or off")
	}
	targets := make([]command.BoardIdentifyTarget, 0, len(req.Msg.Boards))
	for _, board := range req.Msg.Boards {
		targets = append(targets, command.BoardIdentifyTarget{HashboardID: board.GetHashboardId(), InstallationID: board.GetInstallationId()})
	}
	result, err := h.commands.SetBoardIdentify(ctx, targets, mode, authorizeBoardIdentify)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(result), nil
}
