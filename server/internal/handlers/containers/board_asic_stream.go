package containers

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"connectrpc.com/connect"
	pb "github.com/block/proto-fleet/server/generated/grpc/containers/v1"
	"github.com/block/proto-fleet/server/generated/grpc/containers/v1/containersv1connect"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/command"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/handlers/middleware"
)

// WithBoardASICStreamWriteDeadline gives the board ASIC stream's response
// budget to be written. The stream's lifetime and idle timers end the
// controller's side only, so without this a client that stops reading holds
// the handler in a blocked write after both have passed.
func WithBoardASICStreamWriteDeadline(next http.Handler, budget time.Duration) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == containersv1connect.ContainerServiceStreamBoardASICReadingsProcedure {
			if err := http.NewResponseController(w).SetWriteDeadline(time.Now().Add(budget)); err != nil {
				slog.Warn("board ASIC stream has no write deadline", "error", err)
			}
		}
		next.ServeHTTP(w, r)
	})
}

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
