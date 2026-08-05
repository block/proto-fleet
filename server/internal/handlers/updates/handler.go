// Package updates serves the instance.v1.InstanceUpdateService Connect RPCs:
// channel-gated update status and the per-org release channel.
package updates

import (
	"context"
	"errors"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	instancev1 "github.com/block/proto-fleet/server/generated/grpc/instance/v1"
	"github.com/block/proto-fleet/server/generated/grpc/instance/v1/instancev1connect"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	updates "github.com/block/proto-fleet/server/internal/domain/updates"
	"github.com/block/proto-fleet/server/internal/handlers/middleware"
	"github.com/block/proto-fleet/server/internal/updaterapi"
)

type Handler struct {
	svc *updates.Service
}

func NewHandler(svc *updates.Service) *Handler {
	return &Handler{svc: svc}
}

var _ instancev1connect.InstanceUpdateServiceHandler = (*Handler)(nil)

// authorize gates the update surface on org-wide instance:update and returns
// the caller's org ID. Org-wide rather than site-scoped: the release channel
// and the offered upgrade are instance-level state.
func (h *Handler) authorize(ctx context.Context) (int64, error) {
	info, err := middleware.RequireOrgWidePermission(ctx, authz.PermInstanceUpdate)
	if err != nil {
		return 0, err
	}
	if info.OrganizationID == 0 {
		return 0, fleeterror.NewUnauthenticatedError("organization id missing on session")
	}
	return info.OrganizationID, nil
}

// mapErr preserves caller cancellation at the transport boundary. The global
// error interceptor intentionally treats otherwise-unclassified errors as
// Internal, including wrapped context errors.
func mapErr(err error) error {
	switch {
	case errors.Is(err, context.Canceled):
		return connect.NewError(connect.CodeCanceled, err)
	case errors.Is(err, context.DeadlineExceeded):
		return connect.NewError(connect.CodeDeadlineExceeded, err)
	}
	return err
}

func (h *Handler) GetUpdateStatus(ctx context.Context, _ *connect.Request[instancev1.GetUpdateStatusRequest]) (*connect.Response[instancev1.GetUpdateStatusResponse], error) {
	orgID, err := h.authorize(ctx)
	if err != nil {
		return nil, err
	}
	status, err := h.svc.GetUpdateStatus(ctx, orgID)
	if err != nil {
		return nil, mapErr(err)
	}
	return connect.NewResponse(statusToProto(status)), nil
}

func (h *Handler) SetReleaseChannel(ctx context.Context, req *connect.Request[instancev1.SetReleaseChannelRequest]) (*connect.Response[instancev1.SetReleaseChannelResponse], error) {
	orgID, err := h.authorize(ctx)
	if err != nil {
		return nil, err
	}
	channel, err := protoToChannel(req.Msg.GetChannel())
	if err != nil {
		return nil, err
	}
	if err := h.svc.SetReleaseChannel(ctx, orgID, channel); err != nil {
		return nil, mapErr(err)
	}
	return connect.NewResponse(&instancev1.SetReleaseChannelResponse{}), nil
}

func (h *Handler) TriggerUpgrade(ctx context.Context, req *connect.Request[instancev1.TriggerUpgradeRequest]) (*connect.Response[instancev1.TriggerUpgradeResponse], error) {
	orgID, err := h.authorize(ctx)
	if err != nil {
		return nil, err
	}
	operation, err := h.svc.TriggerUpgrade(ctx, orgID, req.Msg.GetTargetVersion())
	if err != nil {
		return nil, mapErr(err)
	}
	return connect.NewResponse(&instancev1.TriggerUpgradeResponse{
		Operation: operationToProto(operation),
	}), nil
}

func (h *Handler) GetUpgradeStatus(ctx context.Context, _ *connect.Request[instancev1.GetUpgradeStatusRequest]) (*connect.Response[instancev1.GetUpgradeStatusResponse], error) {
	if _, err := h.authorize(ctx); err != nil {
		return nil, err
	}
	status := h.svc.GetUpgradeStatus(ctx)
	response := &instancev1.GetUpgradeStatusResponse{ExecutorAvailable: status.ExecutorAvailable}
	if status.Operation != nil {
		response.Operation = operationToProto(*status.Operation)
	}
	return connect.NewResponse(response), nil
}

func statusToProto(status updates.UpdateStatus) *instancev1.GetUpdateStatusResponse {
	out := &instancev1.GetUpdateStatusResponse{
		CurrentVersion:    status.CurrentVersion,
		Channel:           channelToProto(status.Channel),
		StatusAvailable:   status.StatusAvailable,
		UpdateAvailable:   status.UpdateAvailable,
		InstallCommand:    status.InstallCommand,
		OneClickAvailable: status.OneClickAvailable,
	}
	if status.LatestEligible != nil {
		out.LatestEligible = releaseToProto(*status.LatestEligible)
	}
	return out
}

func operationToProto(operation updaterapi.Operation) *instancev1.UpgradeOperation {
	out := &instancev1.UpgradeOperation{
		Id:              operation.ID,
		TargetVersion:   operation.TargetVersion,
		Phase:           phaseToProto(operation.Phase),
		Message:         operation.Message,
		StartedAt:       timestamppb.New(operation.StartedAt),
		UpdatedAt:       timestamppb.New(operation.UpdatedAt),
		Error:           operation.Error,
		RecoveryCommand: operation.RecoveryCommand,
		HostLogPath:     operation.LogPath,
	}
	if operation.CompletedAt != nil {
		out.CompletedAt = timestamppb.New(*operation.CompletedAt)
	}
	return out
}

func phaseToProto(phase updaterapi.Phase) instancev1.UpgradePhase {
	switch phase {
	case updaterapi.PhaseQueued:
		return instancev1.UpgradePhase_UPGRADE_PHASE_QUEUED
	case updaterapi.PhaseDownloading:
		return instancev1.UpgradePhase_UPGRADE_PHASE_DOWNLOADING
	case updaterapi.PhaseVerifying:
		return instancev1.UpgradePhase_UPGRADE_PHASE_VERIFYING
	case updaterapi.PhaseStaging:
		return instancev1.UpgradePhase_UPGRADE_PHASE_STAGING
	case updaterapi.PhasePreflight:
		return instancev1.UpgradePhase_UPGRADE_PHASE_PREFLIGHT
	case updaterapi.PhaseActivating:
		return instancev1.UpgradePhase_UPGRADE_PHASE_ACTIVATING
	case updaterapi.PhaseSucceeded:
		return instancev1.UpgradePhase_UPGRADE_PHASE_SUCCEEDED
	case updaterapi.PhaseFailed:
		return instancev1.UpgradePhase_UPGRADE_PHASE_FAILED
	default:
		return instancev1.UpgradePhase_UPGRADE_PHASE_UNSPECIFIED
	}
}

func releaseToProto(release updates.Release) *instancev1.ReleaseInfo {
	return &instancev1.ReleaseInfo{
		Version:         release.Version,
		ReleaseNotesUrl: release.NotesURL,
		PublishedAt:     timestamppb.New(release.PublishedAt),
		Prerelease:      release.Prerelease,
	}
}

func channelToProto(channel updates.Channel) instancev1.ReleaseChannel {
	switch channel {
	case updates.ChannelStable:
		return instancev1.ReleaseChannel_RELEASE_CHANNEL_STABLE
	case updates.ChannelStableAndRC:
		return instancev1.ReleaseChannel_RELEASE_CHANNEL_STABLE_AND_RC
	}
	return instancev1.ReleaseChannel_RELEASE_CHANNEL_UNSPECIFIED
}

func protoToChannel(channel instancev1.ReleaseChannel) (updates.Channel, error) {
	switch channel {
	case instancev1.ReleaseChannel_RELEASE_CHANNEL_STABLE:
		return updates.ChannelStable, nil
	case instancev1.ReleaseChannel_RELEASE_CHANNEL_STABLE_AND_RC:
		return updates.ChannelStableAndRC, nil
	case instancev1.ReleaseChannel_RELEASE_CHANNEL_UNSPECIFIED:
	}
	return "", fleeterror.NewInvalidArgumentError("release channel is required")
}
