package main

import (
	"context"
	"errors"
	"io"
	"slices"

	mdk "github.com/block/proto-fleet/sdk/mdk/generated/mdk/v2"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

const (
	serverIDHeader     = "x-frigate-server-id"
	actorHeader        = "x-frigate-actor"
	serverUpdatePrefix = "/mdk.v2.ServerUpdateService/"
	maintenanceMessage = "frigate is stopped for a software update"
)

// serverUpdateAPI is one fake instance's ServerUpdateService. It delegates all
// state to the pair's shared simulator, as the real service delegates to etcd
// and the HA peer router.
type serverUpdateAPI struct {
	mdk.UnimplementedServerUpdateServiceServer
	sim  *updateSim
	self string
}

func headerValue(ctx context.Context, key string) string {
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		if values := md.Get(key); len(values) > 0 {
			return values[0]
		}
	}
	return ""
}

// scoped returns the instance an instance-scoped call addresses: the
// x-frigate-server-id header if present, otherwise this instance.
func (a *serverUpdateAPI) scoped(ctx context.Context) string {
	if id := headerValue(ctx, serverIDHeader); id != "" {
		return id
	}
	return a.self
}

func (a *serverUpdateAPI) actor(ctx context.Context) string {
	if actor := headerValue(ctx, actorHeader); actor != "" {
		return actor
	}
	return "unknown"
}

func (a *serverUpdateAPI) GetServerUpdateSupport(ctx context.Context, _ *mdk.GetServerUpdateSupportRequest) (*mdk.GetServerUpdateSupportResponse, error) {
	return a.sim.supportFor(a.scoped(ctx))
}

func (a *serverUpdateAPI) BeginServerUpdateStage(ctx context.Context, in *mdk.BeginServerUpdateStageRequest) (*mdk.BeginServerUpdateStageResponse, error) {
	return a.sim.beginStage(in.GetManifest(), a.scoped(ctx))
}

func (a *serverUpdateAPI) AppendServerUpdateStage(ctx context.Context, in *mdk.AppendServerUpdateStageRequest) (*mdk.AppendServerUpdateStageResponse, error) {
	return a.sim.appendStage(a.scoped(ctx), in.GetJobId(), in.GetManifestSha256(), in.GetOffset(), in.GetChunk())
}

func (a *serverUpdateAPI) CommitServerUpdateStage(ctx context.Context, in *mdk.CommitServerUpdateStageRequest) (*mdk.CommitServerUpdateStageResponse, error) {
	return a.sim.commitStage(a.scoped(ctx), in.GetJobId(), in.GetManifestSha256())
}

func (a *serverUpdateAPI) AbortServerUpdateStage(ctx context.Context, in *mdk.AbortServerUpdateStageRequest) (*mdk.AbortServerUpdateStageResponse, error) {
	if err := a.sim.abortStage(a.scoped(ctx), in.GetJobId(), in.GetManifestSha256()); err != nil {
		return nil, err
	}
	return &mdk.AbortServerUpdateStageResponse{}, nil
}

// StageServerUpdate is the legacy streaming path: one manifest, then chunks.
func (a *serverUpdateAPI) StageServerUpdate(stream mdk.ServerUpdateService_StageServerUpdateServer) error {
	first, err := stream.Recv()
	if err != nil || first.GetManifest() == nil {
		return status.Error(codes.InvalidArgument, "first server update message must contain the manifest")
	}
	manifest := first.GetManifest()
	progress, err := a.sim.beginStage(manifest, a.self)
	if err != nil {
		return err
	}
	fingerprint, instance := progress.GetManifestSha256(), manifest.GetInstanceId()
	for {
		message, recvErr := stream.Recv()
		if errors.Is(recvErr, io.EOF) {
			break
		}
		if recvErr != nil {
			return recvErr
		}
		offset := progress.GetNextOffset()
		appended, appendErr := a.sim.appendStage(instance, manifest.GetJobId(), fingerprint, offset, message.GetChunk())
		if appendErr != nil {
			_ = a.sim.abortStage(instance, manifest.GetJobId(), fingerprint)
			return appendErr
		}
		progress = &mdk.BeginServerUpdateStageResponse{NextOffset: appended.GetNextOffset(), ManifestSha256: fingerprint}
	}
	committed, err := a.sim.commitStage(instance, manifest.GetJobId(), fingerprint)
	if err != nil {
		return err
	}
	return stream.SendAndClose(&mdk.StageServerUpdateResponse{JobId: committed.GetJobId(), BytesStaged: committed.GetBytesStaged(), Sha256: committed.GetSha256()})
}

func (a *serverUpdateAPI) StartServerUpdate(ctx context.Context, in *mdk.StartServerUpdateRequest) (*mdk.StartServerUpdateResponse, error) {
	rollout, err := a.sim.startRollout(in, a.actor(ctx), a.sim.activeFlashes())
	if err != nil {
		return nil, err
	}
	return &mdk.StartServerUpdateResponse{Rollout: rollout}, nil
}

func (a *serverUpdateAPI) GetServerUpdateStatus(_ context.Context, in *mdk.GetServerUpdateStatusRequest) (*mdk.GetServerUpdateStatusResponse, error) {
	rollout, maintenance, err := a.sim.statusOf(in.GetJobId())
	if err != nil {
		return nil, err
	}
	return &mdk.GetServerUpdateStatusResponse{Rollout: rollout, ServedByMaintenance: maintenance}, nil
}

func (a *serverUpdateAPI) RetryServerUpdate(_ context.Context, in *mdk.RetryServerUpdateRequest) (*mdk.RetryServerUpdateResponse, error) {
	rollout, err := a.sim.retryRollout(in.GetJobId(), in.GetIdempotencyKey())
	if err != nil {
		return nil, err
	}
	return &mdk.RetryServerUpdateResponse{Rollout: rollout}, nil
}

func (a *serverUpdateAPI) CancelServerUpdate(_ context.Context, in *mdk.CancelServerUpdateRequest) (*mdk.CancelServerUpdateResponse, error) {
	rollout, err := a.sim.cancelRollout(in.GetJobId(), in.GetIdempotencyKey())
	if err != nil {
		return nil, err
	}
	return &mdk.CancelServerUpdateResponse{Rollout: rollout}, nil
}

// maintenanceServed lists the methods the maintenance service answers while
// the regular daemon is stopped. Everything else is Unavailable.
var maintenanceServed = []string{
	serverUpdatePrefix + "GetServerUpdateStatus",
	serverUpdatePrefix + "RetryServerUpdate",
	serverUpdatePrefix + "CancelServerUpdate",
}

// pairUnary routes a pair member's unary calls. ServerUpdateService calls reach
// the shared simulator directly (they do not advance the container clock);
// during maintenance only the maintenance-served methods answer and every other
// service is Unavailable. All other calls take the normal simulation path.
func pairUnary(state *ContainerState, sim *updateSim) grpc.UnaryServerInterceptor {
	normal := serveUnary(state)
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		maintenance := sim.inMaintenance()
		if maintenance && !slices.Contains(maintenanceServed, info.FullMethod) {
			return nil, status.Error(codes.Unavailable, maintenanceMessage)
		}
		if len(info.FullMethod) > len(serverUpdatePrefix) && info.FullMethod[:len(serverUpdatePrefix)] == serverUpdatePrefix {
			return handler(ctx, req)
		}
		return normal(ctx, req, info, handler)
	}
}

// pairStream makes the streaming staging call Unavailable during maintenance.
func pairStream(sim *updateSim) grpc.StreamServerInterceptor {
	return func(srv any, stream grpc.ServerStream, _ *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		if sim.inMaintenance() {
			return status.Error(codes.Unavailable, maintenanceMessage)
		}
		return handler(srv, stream)
	}
}
