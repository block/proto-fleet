// Package frigateupdate exposes Fleet-triggered Frigate software updates over
// Connect and the chunked package upload endpoints over REST.
package frigateupdate

import (
	"context"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/timestamppb"

	pb "github.com/block/proto-fleet/server/generated/grpc/frigateupdate/v1"
	"github.com/block/proto-fleet/server/generated/grpc/frigateupdate/v1/frigateupdatev1connect"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	domain "github.com/block/proto-fleet/server/internal/domain/frigateupdate"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

// Handler implements FrigateUpdateService.
type Handler struct {
	service *domain.Service
}

var _ frigateupdatev1connect.FrigateUpdateServiceHandler = (*Handler)(nil)

// NewHandler creates the Connect handler.
func NewHandler(s *domain.Service) *Handler { return &Handler{service: s} }

func parseID(name, value string) (uuid.UUID, error) {
	id, err := uuid.Parse(value)
	if err != nil {
		return uuid.Nil, fleeterror.NewInvalidArgumentErrorf("%s must be a UUID", name)
	}
	return id, nil
}

func (h *Handler) GetSupport(ctx context.Context, r *connect.Request[pb.GetSupportRequest]) (*connect.Response[pb.GetSupportResponse], error) {
	packageID := uuid.Nil
	if r.Msg.PackageId != "" {
		id, err := parseID("package_id", r.Msg.PackageId)
		if err != nil {
			return nil, err
		}
		packageID = id
	}
	support, err := h.service.GetSupport(ctx, r.Msg.DeviceIdentifier, packageID)
	if err != nil {
		return nil, err
	}
	out := &pb.GetSupportResponse{
		Eligible: support.Eligible, IneligibleCode: support.IneligibleCode, IneligibleReason: support.IneligibleReason,
	}
	for _, in := range support.Instances {
		out.Instances = append(out.Instances, instanceSupportToProto(in))
	}
	return connect.NewResponse(out), nil
}

func (h *Handler) ListPackages(ctx context.Context, _ *connect.Request[pb.ListPackagesRequest]) (*connect.Response[pb.ListPackagesResponse], error) {
	packages, err := h.service.ListPackages(ctx)
	if err != nil {
		return nil, err
	}
	updatable, err := h.service.HasUpdatableContainer(ctx)
	if err != nil {
		return nil, err
	}
	out := &pb.ListPackagesResponse{HasUpdatableContainer: updatable}
	for _, p := range packages {
		out.Packages = append(out.Packages, PackageToProto(p))
	}
	return connect.NewResponse(out), nil
}

func (h *Handler) DeletePackage(ctx context.Context, r *connect.Request[pb.DeletePackageRequest]) (*connect.Response[pb.DeletePackageResponse], error) {
	id, err := parseID("package_id", r.Msg.PackageId)
	if err != nil {
		return nil, err
	}
	if err := h.service.DeletePackage(ctx, id); err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.DeletePackageResponse{}), nil
}

func (h *Handler) StartUpdate(ctx context.Context, r *connect.Request[pb.StartUpdateRequest]) (*connect.Response[pb.StartUpdateResponse], error) {
	packageID, err := parseID("package_id", r.Msg.PackageId)
	if err != nil {
		return nil, err
	}
	job, err := h.service.StartUpdate(ctx, domain.StartInput{
		DeviceIdentifier: r.Msg.DeviceIdentifier, PackageID: packageID, StageOnly: r.Msg.StageOnly, IdempotencyKey: r.Msg.IdempotencyKey,
	})
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.StartUpdateResponse{Job: jobToProto(job)}), nil
}

func (h *Handler) GetUpdate(ctx context.Context, r *connect.Request[pb.GetUpdateRequest]) (*connect.Response[pb.GetUpdateResponse], error) {
	id, err := parseID("job_id", r.Msg.JobId)
	if err != nil {
		return nil, err
	}
	job, err := h.service.GetUpdate(ctx, id)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.GetUpdateResponse{Job: jobToProto(job)}), nil
}

func (h *Handler) ListUpdates(ctx context.Context, r *connect.Request[pb.ListUpdatesRequest]) (*connect.Response[pb.ListUpdatesResponse], error) {
	jobs, err := h.service.ListUpdates(ctx, r.Msg.DeviceIdentifier, r.Msg.PageSize)
	if err != nil {
		return nil, err
	}
	out := &pb.ListUpdatesResponse{}
	for _, j := range jobs {
		out.Jobs = append(out.Jobs, jobToProto(j))
	}
	return connect.NewResponse(out), nil
}

func (h *Handler) RetryUpdate(ctx context.Context, r *connect.Request[pb.RetryUpdateRequest]) (*connect.Response[pb.RetryUpdateResponse], error) {
	id, err := parseID("job_id", r.Msg.JobId)
	if err != nil {
		return nil, err
	}
	job, err := h.service.RetryUpdate(ctx, id, r.Msg.IdempotencyKey)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.RetryUpdateResponse{Job: jobToProto(job)}), nil
}

func (h *Handler) CancelUpdate(ctx context.Context, r *connect.Request[pb.CancelUpdateRequest]) (*connect.Response[pb.CancelUpdateResponse], error) {
	id, err := parseID("job_id", r.Msg.JobId)
	if err != nil {
		return nil, err
	}
	job, err := h.service.CancelUpdate(ctx, id, r.Msg.IdempotencyKey)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.CancelUpdateResponse{Job: jobToProto(job)}), nil
}

// PackageToProto converts a stored package.
func PackageToProto(p domain.Package) *pb.FrigatePackage {
	return &pb.FrigatePackage{
		Id: p.ID.String(), Filename: p.Filename, Version: p.Version, Architecture: p.Architecture, Sha256: p.SHA256,
		SizeBytes: p.SizeBytes, Repository: p.Repository, ReleaseKeyId: p.ReleaseKeyID,
		MinInstalledVersion: p.MinInstalledVersion, IssuedAt: timestamppb.New(p.IssuedAt),
		UploadedAt: timestamppb.New(p.UploadedAt), UploadedBy: p.UploadedBy, StoredOnThisHost: p.StoredOnThisHost,
	}
}

func instanceSupportToProto(in sdk.FrigateInstanceSupport) *pb.InstanceSupport {
	return &pb.InstanceSupport{
		InstanceId: in.InstanceID, PeerInstanceId: in.PeerInstanceID, RunningVersion: in.RunningVersion,
		Architecture: in.Architecture, InstallationType: installationTypeToProto(in.InstallationType),
		HelperAvailable: in.HelperAvailable, SupportMessage: in.SupportMessage, ClusterId: in.ClusterID,
		ActiveJobId: in.ActiveJobID, SpoolFreeBytes: in.SpoolFreeBytes, EtcdMembersTotal: in.EtcdMembersTotal,
		EtcdMembersHealthy: in.EtcdMembersHealthy, TrustedReleaseKeyIds: in.TrustedReleaseKeyIDs,
		SelfCoordinationSupported: in.SelfCoordinationSupported, RemoteTriggerAllowed: in.RemoteTriggerAllowed,
		Error: in.Err,
	}
}

func installationTypeToProto(t sdk.FrigateInstallationType) pb.FrigateInstallationType {
	switch t {
	case sdk.FrigateInstallationDebianPackage:
		return pb.FrigateInstallationType_FRIGATE_INSTALLATION_TYPE_DEBIAN_PACKAGE
	case sdk.FrigateInstallationDirectBinary:
		return pb.FrigateInstallationType_FRIGATE_INSTALLATION_TYPE_DIRECT_BINARY
	case sdk.FrigateInstallationUnknown:
		return pb.FrigateInstallationType_FRIGATE_INSTALLATION_TYPE_UNKNOWN
	case sdk.FrigateInstallationUnspecified:
		return pb.FrigateInstallationType_FRIGATE_INSTALLATION_TYPE_UNSPECIFIED
	default:
		return pb.FrigateInstallationType_FRIGATE_INSTALLATION_TYPE_UNKNOWN
	}
}

func stateToProto(s string) pb.FrigateUpdateState {
	switch s {
	case domain.StateStaging:
		return pb.FrigateUpdateState_FRIGATE_UPDATE_STATE_STAGING
	case domain.StateStaged:
		return pb.FrigateUpdateState_FRIGATE_UPDATE_STATE_STAGED
	case domain.StateStarting:
		return pb.FrigateUpdateState_FRIGATE_UPDATE_STATE_STARTING
	case domain.StateRunning:
		return pb.FrigateUpdateState_FRIGATE_UPDATE_STATE_RUNNING
	case domain.StateSucceeded:
		return pb.FrigateUpdateState_FRIGATE_UPDATE_STATE_SUCCEEDED
	case domain.StateFailed:
		return pb.FrigateUpdateState_FRIGATE_UPDATE_STATE_FAILED
	case domain.StateCancelled:
		return pb.FrigateUpdateState_FRIGATE_UPDATE_STATE_CANCELLED
	case domain.StateVerified:
		return pb.FrigateUpdateState_FRIGATE_UPDATE_STATE_VERIFIED
	case domain.StateAbandoned:
		return pb.FrigateUpdateState_FRIGATE_UPDATE_STATE_ABANDONED
	default:
		return pb.FrigateUpdateState_FRIGATE_UPDATE_STATE_UNSPECIFIED
	}
}

func phaseToProto(p string) pb.FrigateRolloutPhase {
	switch sdk.FrigateRolloutPhase(p) {
	case sdk.FrigatePhaseVerifying:
		return pb.FrigateRolloutPhase_FRIGATE_ROLLOUT_PHASE_VERIFYING
	case sdk.FrigatePhaseStopping:
		return pb.FrigateRolloutPhase_FRIGATE_ROLLOUT_PHASE_STOPPING
	case sdk.FrigatePhaseInstalling:
		return pb.FrigateRolloutPhase_FRIGATE_ROLLOUT_PHASE_INSTALLING
	case sdk.FrigatePhaseMigrating:
		return pb.FrigateRolloutPhase_FRIGATE_ROLLOUT_PHASE_MIGRATING
	case sdk.FrigatePhaseStarting:
		return pb.FrigateRolloutPhase_FRIGATE_ROLLOUT_PHASE_STARTING
	case sdk.FrigatePhaseSucceeded:
		return pb.FrigateRolloutPhase_FRIGATE_ROLLOUT_PHASE_SUCCEEDED
	case sdk.FrigatePhaseFailed:
		return pb.FrigateRolloutPhase_FRIGATE_ROLLOUT_PHASE_FAILED
	case sdk.FrigatePhaseCancelled:
		return pb.FrigateRolloutPhase_FRIGATE_ROLLOUT_PHASE_CANCELLED
	case sdk.FrigatePhaseVerified:
		return pb.FrigateRolloutPhase_FRIGATE_ROLLOUT_PHASE_VERIFIED
	case sdk.FrigatePhaseAbandoned:
		return pb.FrigateRolloutPhase_FRIGATE_ROLLOUT_PHASE_ABANDONED
	case sdk.FrigatePhaseUnspecified:
		return pb.FrigateRolloutPhase_FRIGATE_ROLLOUT_PHASE_UNSPECIFIED
	case sdk.FrigatePhaseUnknown:
		return pb.FrigateRolloutPhase_FRIGATE_ROLLOUT_PHASE_UNKNOWN
	default:
		return pb.FrigateRolloutPhase_FRIGATE_ROLLOUT_PHASE_UNKNOWN
	}
}

func agentPhaseToProto(p string) pb.FrigateAgentPhase {
	switch sdk.FrigateAgentPhase(p) {
	case sdk.FrigateAgentConnecting:
		return pb.FrigateAgentPhase_FRIGATE_AGENT_PHASE_CONNECTING
	case sdk.FrigateAgentStaged:
		return pb.FrigateAgentPhase_FRIGATE_AGENT_PHASE_STAGED
	case sdk.FrigateAgentStopped:
		return pb.FrigateAgentPhase_FRIGATE_AGENT_PHASE_STOPPED
	case sdk.FrigateAgentInstalled:
		return pb.FrigateAgentPhase_FRIGATE_AGENT_PHASE_INSTALLED
	case sdk.FrigateAgentMigrated:
		return pb.FrigateAgentPhase_FRIGATE_AGENT_PHASE_MIGRATED
	case sdk.FrigateAgentStarted:
		return pb.FrigateAgentPhase_FRIGATE_AGENT_PHASE_STARTED
	case sdk.FrigateAgentFailed:
		return pb.FrigateAgentPhase_FRIGATE_AGENT_PHASE_FAILED
	case sdk.FrigateAgentUnspecified:
		return pb.FrigateAgentPhase_FRIGATE_AGENT_PHASE_UNSPECIFIED
	case sdk.FrigateAgentUnknown:
		return pb.FrigateAgentPhase_FRIGATE_AGENT_PHASE_UNKNOWN
	default:
		return pb.FrigateAgentPhase_FRIGATE_AGENT_PHASE_UNKNOWN
	}
}

func stageStateToProto(s string) pb.InstanceStageState {
	switch s {
	case domain.StagePending:
		return pb.InstanceStageState_INSTANCE_STAGE_STATE_PENDING
	case domain.StageStaging:
		return pb.InstanceStageState_INSTANCE_STAGE_STATE_STAGING
	case domain.StageStaged:
		return pb.InstanceStageState_INSTANCE_STAGE_STATE_STAGED
	case domain.StageFailed:
		return pb.InstanceStageState_INSTANCE_STAGE_STATE_FAILED
	default:
		return pb.InstanceStageState_INSTANCE_STAGE_STATE_UNSPECIFIED
	}
}

func jobToProto(j domain.Job) *pb.UpdateJob {
	r := j.Row
	pkg := PackageToProto(j.Package)
	out := &pb.UpdateJob{
		JobId: r.ID.String(), DeviceIdentifier: j.DeviceIdentifier, Package: pkg,
		State: stateToProto(r.State), RolloutPhase: phaseToProto(r.RolloutPhase),
		FromVersion: r.FromVersion, TargetVersion: r.TargetVersion, StageOnly: r.StageOnly,
		DriverInstanceId: r.DriverInstanceID, ErrorCode: r.ErrorCode, ErrorMessage: r.ErrorMessage, NextStep: r.NextStep,
		Retryable: r.Retryable, Cancellable: r.Cancellable, DisruptionStarted: r.DisruptionStarted,
		ServedByMaintenance: r.ServedByMaintenance, Actor: r.ActorUsername,
		CreatedAt: timestamppb.New(r.CreatedAt), UpdatedAt: timestamppb.New(r.UpdatedAt),
	}
	if r.LastContactAt.Valid {
		out.LastContact = timestamppb.New(r.LastContactAt.Time)
	}
	if r.UnreachableSince.Valid {
		out.UnreachableSince = timestamppb.New(r.UnreachableSince.Time)
	}
	if r.CompletedAt.Valid {
		out.CompletedAt = timestamppb.New(r.CompletedAt.Time)
	}
	if domain.CancelPending(r) {
		out.CancelRequestedAt = timestamppb.New(r.CancelRequestedAt.Time)
	}
	for _, in := range j.Instances {
		p := &pb.InstanceProgress{
			InstanceId: in.InstanceID, StageState: stageStateToProto(in.StageState), StagedBytes: in.StagedBytes,
			SizeBytes: in.SizeBytes, AgentPhase: agentPhaseToProto(in.AgentPhase), ContactStale: in.ContactStale,
			RunningVersion: in.RunningVersion, MigrationExecuted: in.MigrationExecuted,
			ErrorCode: in.ErrorCode, ErrorMessage: in.ErrorMessage,
		}
		if in.LastContact.Valid {
			p.LastContact = timestamppb.New(in.LastContact.Time)
		}
		out.Instances = append(out.Instances, p)
	}
	return out
}
