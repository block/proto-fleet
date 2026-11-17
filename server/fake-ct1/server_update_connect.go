package main

import (
	"context"
	"errors"
	"net/http"
	"slices"

	"connectrpc.com/connect"
	mdk "github.com/block/proto-fleet/sdk/mdk/generated/mdk/v2"
	"github.com/block/proto-fleet/sdk/mdk/generated/mdk/v2/mdkv2connect"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

// connectServerUpdate serves a pair member's ServerUpdateService over Connect,
// as Frigate's Connect gateway relays it to the browser. Every unary method is
// available; the client-streaming StageServerUpdate stays Unimplemented, since
// browsers stage with Begin/Append/Commit.
type connectServerUpdate struct {
	mdkv2connect.UnimplementedServerUpdateServiceHandler
	api *serverUpdateAPI
}

// connectMetadata carries the two headers Frigate's gateway forwards to the
// native service: the exact-instance selector and the audit actor.
func connectMetadata(ctx context.Context, header http.Header) context.Context {
	md := metadata.MD{}
	for _, key := range []string{serverIDHeader, actorHeader} {
		if values := header.Values(key); len(values) > 0 {
			md.Set(key, values...)
		}
	}
	return metadata.NewIncomingContext(ctx, md)
}

// connectStatus converts a native status to a Connect error and keeps its
// details, so the Frigate ErrorInfo refusal reason reaches the browser.
func connectStatus(err error) error {
	st, ok := status.FromError(err)
	if !ok {
		return err
	}
	converted := connect.NewError(connect.Code(st.Code()), errors.New(st.Message()))
	for _, detail := range st.Details() {
		message, ok := detail.(proto.Message)
		if !ok {
			continue
		}
		if wire, detailErr := connect.NewErrorDetail(message); detailErr == nil {
			converted.AddDetail(wire)
		}
	}
	return converted
}

func serveServerUpdate[Req, Res any](c *connectServerUpdate, ctx context.Context, procedure string, req *connect.Request[Req], call func(context.Context, *Req) (*Res, error)) (*connect.Response[Res], error) {
	if c.api.sim.inMaintenance() && !slices.Contains(maintenanceServed, procedure) {
		return nil, connect.NewError(connect.CodeUnavailable, errors.New(maintenanceMessage))
	}
	res, err := call(connectMetadata(ctx, req.Header()), req.Msg)
	if err != nil {
		return nil, connectStatus(err)
	}
	return connect.NewResponse(res), nil
}

func (c *connectServerUpdate) GetServerUpdateSupport(ctx context.Context, req *connect.Request[mdk.GetServerUpdateSupportRequest]) (*connect.Response[mdk.GetServerUpdateSupportResponse], error) {
	return serveServerUpdate(c, ctx, mdkv2connect.ServerUpdateServiceGetServerUpdateSupportProcedure, req, c.api.GetServerUpdateSupport)
}

func (c *connectServerUpdate) BeginServerUpdateStage(ctx context.Context, req *connect.Request[mdk.BeginServerUpdateStageRequest]) (*connect.Response[mdk.BeginServerUpdateStageResponse], error) {
	return serveServerUpdate(c, ctx, mdkv2connect.ServerUpdateServiceBeginServerUpdateStageProcedure, req, c.api.BeginServerUpdateStage)
}

func (c *connectServerUpdate) AppendServerUpdateStage(ctx context.Context, req *connect.Request[mdk.AppendServerUpdateStageRequest]) (*connect.Response[mdk.AppendServerUpdateStageResponse], error) {
	return serveServerUpdate(c, ctx, mdkv2connect.ServerUpdateServiceAppendServerUpdateStageProcedure, req, c.api.AppendServerUpdateStage)
}

func (c *connectServerUpdate) CommitServerUpdateStage(ctx context.Context, req *connect.Request[mdk.CommitServerUpdateStageRequest]) (*connect.Response[mdk.CommitServerUpdateStageResponse], error) {
	return serveServerUpdate(c, ctx, mdkv2connect.ServerUpdateServiceCommitServerUpdateStageProcedure, req, c.api.CommitServerUpdateStage)
}

func (c *connectServerUpdate) AbortServerUpdateStage(ctx context.Context, req *connect.Request[mdk.AbortServerUpdateStageRequest]) (*connect.Response[mdk.AbortServerUpdateStageResponse], error) {
	return serveServerUpdate(c, ctx, mdkv2connect.ServerUpdateServiceAbortServerUpdateStageProcedure, req, c.api.AbortServerUpdateStage)
}

func (c *connectServerUpdate) StartServerUpdate(ctx context.Context, req *connect.Request[mdk.StartServerUpdateRequest]) (*connect.Response[mdk.StartServerUpdateResponse], error) {
	return serveServerUpdate(c, ctx, mdkv2connect.ServerUpdateServiceStartServerUpdateProcedure, req, c.api.StartServerUpdate)
}

func (c *connectServerUpdate) GetServerUpdateStatus(ctx context.Context, req *connect.Request[mdk.GetServerUpdateStatusRequest]) (*connect.Response[mdk.GetServerUpdateStatusResponse], error) {
	return serveServerUpdate(c, ctx, mdkv2connect.ServerUpdateServiceGetServerUpdateStatusProcedure, req, c.api.GetServerUpdateStatus)
}

func (c *connectServerUpdate) RetryServerUpdate(ctx context.Context, req *connect.Request[mdk.RetryServerUpdateRequest]) (*connect.Response[mdk.RetryServerUpdateResponse], error) {
	return serveServerUpdate(c, ctx, mdkv2connect.ServerUpdateServiceRetryServerUpdateProcedure, req, c.api.RetryServerUpdate)
}

func (c *connectServerUpdate) CancelServerUpdate(ctx context.Context, req *connect.Request[mdk.CancelServerUpdateRequest]) (*connect.Response[mdk.CancelServerUpdateResponse], error) {
	return serveServerUpdate(c, ctx, mdkv2connect.ServerUpdateServiceCancelServerUpdateProcedure, req, c.api.CancelServerUpdate)
}
