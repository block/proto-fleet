package containers

import (
	"context"

	"connectrpc.com/connect"
	pb "github.com/block/proto-fleet/server/generated/grpc/containers/v1"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/command"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/handlers/middleware"
)

// authorizeBoardRead requires the reads needed to see the board's container,
// all at the board's current site.
func authorizeBoardRead(ctx context.Context, site *int64) error {
	for _, permission := range []string{authz.PermSiteRead, authz.PermFleetRead, authz.PermMinerRead} {
		if _, err := middleware.RequirePermission(ctx, permission, authz.ResourceContext{SiteID: site}); err != nil {
			return err
		}
	}
	return nil
}

func (h *Handler) StreamBoardASICReadings(ctx context.Context, req *connect.Request[pb.StreamBoardASICReadingsRequest], stream *connect.ServerStream[pb.StreamBoardASICReadingsResponse]) error {
	if h.commands == nil {
		return fleeterror.NewUnimplementedError("board readings unavailable")
	}
	board := req.Msg.GetBoard()
	target := command.BoardIdentifyTarget{HashboardID: board.GetHashboardId(), InstallationID: board.GetInstallationId()}
	return h.commands.StreamBoardASICReadings(ctx, target, authorizeBoardRead, stream.Send)
}
