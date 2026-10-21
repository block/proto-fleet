package hostedaccounts

import (
	"context"
	"encoding/csv"
	"strconv"
	"strings"
	"time"

	"connectrpc.com/connect"
	pb "github.com/block/proto-fleet/server/generated/grpc/hosted/v1"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
)

func (h *Handler) ListAccountActivities(ctx context.Context, request *connect.Request[pb.ListAccountActivitiesRequest]) (*connect.Response[pb.ListAccountActivitiesResponse], error) {
	info, err := principal(ctx, request.Msg.AccountId)
	if err != nil {
		return nil, err
	}
	response, err := h.accounts.Activities(ctx, info.UserID, info.OrganizationID, request.Msg)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(response), nil
}

func (h *Handler) ExportAccountActivities(ctx context.Context, request *connect.Request[pb.ExportAccountActivitiesRequest], stream *connect.ServerStream[pb.ExportAccountActivitiesResponse]) error {
	info, err := principal(ctx, request.Msg.AccountId)
	if err != nil {
		return err
	}
	pageRequest := &pb.ListAccountActivitiesRequest{AccountId: request.Msg.AccountId, Filter: request.Msg.Filter, PageSize: 100}
	first := true
	var authorityFingerprint string
	for {
		// Reauthorize every chunk, including when a caller reuses a cursor after
		// revocation. Nothing is streamed before the initial authorized read.
		page, err := h.accounts.ActivityExportPage(ctx, info.UserID, info.OrganizationID, pageRequest)
		if err != nil {
			return err
		}
		if first {
			authorityFingerprint = page.AuthorityFingerprint
		} else if page.AuthorityFingerprint != authorityFingerprint {
			return fleeterror.NewPlainError("account activity access changed during export; restart the download", connect.CodeAborted)
		}
		var chunk strings.Builder
		writer := csv.NewWriter(&chunk)
		if first {
			_ = writer.Write([]string{"Timestamp", "Type", "Category", "Description", "Result", "Scope", "Count"})
			first = false
		}
		for _, entry := range page.Activities {
			_ = writer.Write([]string{entry.CreatedAt.AsTime().UTC().Format(time.RFC3339), entry.EventType, entry.EventCategory, entry.Description, entry.Result, entry.GetScopeType(), strconv.Itoa(int(entry.ScopeCount))})
		}
		writer.Flush()
		if err = stream.Send(&pb.ExportAccountActivitiesResponse{Chunk: []byte(chunk.String())}); err != nil {
			return fleeterror.NewInternalErrorf("send account activity export: %w", err)
		}
		if page.NextPageToken == "" {
			return nil
		}
		pageRequest.PageToken = page.NextPageToken
	}
}
