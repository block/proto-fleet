package sdk

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	pb "github.com/block/proto-fleet/server/sdk/v1/pb/generated"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// FrigateServerUpdate dispatches one update operation to a device that
// implements DeviceFrigateUpdater. Refusals keep their reason on the wire.
func (s *DriverGRPCServer) FrigateServerUpdate(ctx context.Context, req *pb.FrigateServerUpdateRequest) (*pb.FrigateServerUpdateResponse, error) {
	if req == nil || req.Ref == nil || req.Operation == nil {
		return nil, status.Error(codes.InvalidArgument, "frigate update request requires a device and an operation") //nolint:wrapcheck // The gRPC status is the plugin boundary error.
	}
	device, err := s.selectDevice(req.Ref)
	if err != nil {
		return nil, err
	}
	updater, ok := device.(DeviceFrigateUpdater)
	if !ok {
		return nil, sdkErrorToGRPCStatus(NewErrUnsupportedCapability(CapabilityFrigateServerUpdate))
	}
	resp, err := dispatchFrigateUpdate(ctx, updater, req)
	if err != nil {
		return nil, frigateServerError(err)
	}
	return resp, nil
}

// frigateServerError keeps refusals and gRPC statuses intact, and maps
// anything else through the SDK's normal error mapping.
func frigateServerError(err error) error {
	if refusal := AsFrigateRefusal(err); refusal != nil {
		return refusal
	}
	if _, ok := status.FromError(err); ok {
		return err
	}
	return sdkErrorToGRPCStatus(err)
}

func invalidFrigateRequest(err error) error {
	return status.Error(codes.InvalidArgument, err.Error()) //nolint:wrapcheck // The gRPC status is the plugin boundary error.
}

func dispatchFrigateUpdate(ctx context.Context, u DeviceFrigateUpdater, req *pb.FrigateServerUpdateRequest) (*pb.FrigateServerUpdateResponse, error) {
	switch op := req.Operation.(type) {
	case *pb.FrigateServerUpdateRequest_Support:
		support, err := u.FrigateUpdateSupport(ctx)
		if err != nil {
			return nil, err
		}
		wire := make([]*pb.FrigateInstanceSupport, 0, len(support))
		for _, item := range support {
			wire = append(wire, frigateSupportToProto(item))
		}
		return &pb.FrigateServerUpdateResponse{Result: &pb.FrigateServerUpdateResponse_Support{Support: &pb.FrigateUpdateSupportResponse{Instances: wire}}}, nil
	case *pb.FrigateServerUpdateRequest_StageBegin:
		if err := validateFrigateInstanceID(op.StageBegin.GetInstanceId()); err != nil {
			return nil, invalidFrigateRequest(err)
		}
		manifest := frigateManifestFromProto(op.StageBegin.GetManifest())
		if err := manifest.ValidateForInstance(op.StageBegin.GetInstanceId()); err != nil {
			return nil, invalidFrigateRequest(err)
		}
		progress, err := u.FrigateStageBegin(ctx, op.StageBegin.GetInstanceId(), manifest)
		if err != nil {
			return nil, err
		}
		return frigateProgressResponse(progress), nil
	case *pb.FrigateServerUpdateRequest_StageAppend:
		if err := validateFrigateInstanceID(op.StageAppend.GetInstanceId()); err != nil {
			return nil, invalidFrigateRequest(err)
		}
		a := FrigateStageAppend{JobID: op.StageAppend.GetJobId(), ManifestSHA256: op.StageAppend.GetManifestSha256(), Offset: op.StageAppend.GetOffset(), Chunk: op.StageAppend.GetChunk(), Actor: op.StageAppend.GetActor()}
		if err := a.Validate(); err != nil {
			return nil, invalidFrigateRequest(err)
		}
		progress, err := u.FrigateStageAppend(ctx, op.StageAppend.GetInstanceId(), a)
		if err != nil {
			return nil, err
		}
		return frigateProgressResponse(progress), nil
	case *pb.FrigateServerUpdateRequest_StageCommit:
		if err := validateFrigateInstanceID(op.StageCommit.GetInstanceId()); err != nil {
			return nil, invalidFrigateRequest(err)
		}
		ref := FrigateStageRef{JobID: op.StageCommit.GetJobId(), ManifestSHA256: op.StageCommit.GetManifestSha256(), Actor: op.StageCommit.GetActor()}
		if err := ref.Validate(); err != nil {
			return nil, invalidFrigateRequest(err)
		}
		commit, err := u.FrigateStageCommit(ctx, op.StageCommit.GetInstanceId(), ref)
		if err != nil {
			return nil, err
		}
		return frigateCommitResponse(commit), nil
	case *pb.FrigateServerUpdateRequest_StageAbort:
		if err := validateFrigateInstanceID(op.StageAbort.GetInstanceId()); err != nil {
			return nil, invalidFrigateRequest(err)
		}
		ref := FrigateStageRef{JobID: op.StageAbort.GetJobId(), ManifestSHA256: op.StageAbort.GetManifestSha256(), Actor: op.StageAbort.GetActor()}
		if err := ref.Validate(); err != nil {
			return nil, invalidFrigateRequest(err)
		}
		if err := u.FrigateStageAbort(ctx, op.StageAbort.GetInstanceId(), ref); err != nil {
			return nil, err
		}
		return &pb.FrigateServerUpdateResponse{Result: &pb.FrigateServerUpdateResponse_StageAbort{StageAbort: &emptypb.Empty{}}}, nil
	case *pb.FrigateServerUpdateRequest_Start:
		start := FrigateStartRequest{
			JobID:                  op.Start.GetJobId(),
			TargetVersion:          op.Start.GetTargetVersion(),
			ExpectedInstanceIDs:    slices.Clone(op.Start.GetExpectedInstanceIds()),
			StageOnly:              op.Start.GetStageOnly(),
			IdempotencyKey:         op.Start.GetIdempotencyKey(),
			ExpectedManifestSHA256: cloneStringMap(op.Start.GetExpectedManifestSha256()),
			Actor:                  op.Start.GetActor(),
		}
		if err := start.Validate(); err != nil {
			return nil, invalidFrigateRequest(err)
		}
		rollout, err := u.FrigateUpdateStart(ctx, start)
		if err != nil {
			return nil, err
		}
		return frigateRolloutResponse(rollout), nil
	case *pb.FrigateServerUpdateRequest_Status:
		// An empty job ID selects the active (else latest) rollout.
		if err := validateFrigateStatusJobID(op.Status.GetJobId()); err != nil {
			return nil, invalidFrigateRequest(err)
		}
		result, err := u.FrigateUpdateStatus(ctx, op.Status.GetJobId())
		if err != nil {
			return nil, err
		}
		return &pb.FrigateServerUpdateResponse{Result: &pb.FrigateServerUpdateResponse_Status{Status: &pb.FrigateStatus{Rollout: frigateRolloutToProto(result.Rollout), ServedByMaintenance: result.ServedByMaintenance, AnsweredBy: result.AnsweredBy}}}, nil
	case *pb.FrigateServerUpdateRequest_Retry:
		action := FrigateAction{JobID: op.Retry.GetJobId(), IdempotencyKey: op.Retry.GetIdempotencyKey(), Actor: op.Retry.GetActor()}
		if err := action.Validate(); err != nil {
			return nil, invalidFrigateRequest(err)
		}
		rollout, err := u.FrigateUpdateRetry(ctx, action)
		if err != nil {
			return nil, err
		}
		return frigateRolloutResponse(rollout), nil
	case *pb.FrigateServerUpdateRequest_Cancel:
		action := FrigateAction{JobID: op.Cancel.GetJobId(), IdempotencyKey: op.Cancel.GetIdempotencyKey(), Actor: op.Cancel.GetActor()}
		if err := action.Validate(); err != nil {
			return nil, invalidFrigateRequest(err)
		}
		rollout, err := u.FrigateUpdateCancel(ctx, action)
		if err != nil {
			return nil, err
		}
		return frigateRolloutResponse(rollout), nil
	default:
		return nil, status.Error(codes.InvalidArgument, "unknown frigate update operation") //nolint:wrapcheck // The gRPC status is the plugin boundary error.
	}
}

func frigateProgressResponse(p FrigateStageProgress) *pb.FrigateServerUpdateResponse {
	return &pb.FrigateServerUpdateResponse{Result: &pb.FrigateServerUpdateResponse_StageProgress{StageProgress: &pb.FrigateStageProgress{JobId: p.JobID, ManifestSha256: p.ManifestSHA256, NextOffset: p.NextOffset, SizeBytes: p.SizeBytes}}}
}

func frigateCommitResponse(c FrigateStageCommit) *pb.FrigateServerUpdateResponse {
	return &pb.FrigateServerUpdateResponse{Result: &pb.FrigateServerUpdateResponse_StageCommit{StageCommit: &pb.FrigateStageCommitResult{JobId: c.JobID, BytesStaged: c.BytesStaged, Sha256: c.SHA256}}}
}

func frigateRolloutResponse(r FrigateRollout) *pb.FrigateServerUpdateResponse {
	return &pb.FrigateServerUpdateResponse{Result: &pb.FrigateServerUpdateResponse_Rollout{Rollout: frigateRolloutToProto(r)}}
}

// cloneStringMap copies a request map so a message handed across a boundary
// (or retained by a test double) never aliases the caller's map. An empty map
// becomes nil, which the wire treats the same.
func cloneStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

// The wire message has one error string. A refusal reason travels in it as a
// leading "[UPDATE_REASON] " token, which decodeFrigateSupportError strips back
// into FrigateInstanceSupport.ErrReason. A dedicated field can replace this
// without changing the Go types.
func encodeFrigateSupportError(reason, message string) string {
	if reason == "" {
		return message
	}
	return "[" + reason + "] " + message
}

func decodeFrigateSupportError(wire string) (reason, message string) {
	if !strings.HasPrefix(wire, "[") {
		return "", wire
	}
	end := strings.Index(wire, "] ")
	if end < 2 {
		return "", wire
	}
	token := wire[1:end]
	for _, r := range token {
		if (r < 'A' || r > 'Z') && (r < '0' || r > '9') && r != '_' {
			return "", wire
		}
	}
	return token, wire[end+2:]
}

func frigateSupportToProto(s FrigateInstanceSupport) *pb.FrigateInstanceSupport {
	return &pb.FrigateInstanceSupport{
		InstanceId: s.InstanceID, PeerInstanceId: s.PeerInstanceID, RunningVersion: s.RunningVersion, Architecture: s.Architecture,
		InstallationType: string(s.InstallationType), HelperAvailable: s.HelperAvailable, SupportMessage: s.SupportMessage,
		ClusterId: s.ClusterID, ActiveJobId: s.ActiveJobID, SpoolFreeBytes: s.SpoolFreeBytes,
		EtcdMembersTotal: s.EtcdMembersTotal, EtcdMembersHealthy: s.EtcdMembersHealthy,
		TrustedReleaseKeyIds: slices.Clone(s.TrustedReleaseKeyIDs), SelfCoordinationSupported: s.SelfCoordinationSupported,
		RemoteTriggerAllowed: s.RemoteTriggerAllowed, Error: encodeFrigateSupportError(s.ErrReason, s.Err),
	}
}

func frigateSupportFromProto(s *pb.FrigateInstanceSupport) FrigateInstanceSupport {
	reason, err := decodeFrigateSupportError(s.GetError())
	return FrigateInstanceSupport{
		InstanceID: s.GetInstanceId(), PeerInstanceID: s.GetPeerInstanceId(), RunningVersion: s.GetRunningVersion(), Architecture: s.GetArchitecture(),
		InstallationType: FrigateInstallationType(s.GetInstallationType()), HelperAvailable: s.GetHelperAvailable(), SupportMessage: s.GetSupportMessage(),
		ClusterID: s.GetClusterId(), ActiveJobID: s.GetActiveJobId(), SpoolFreeBytes: s.GetSpoolFreeBytes(),
		EtcdMembersTotal: s.GetEtcdMembersTotal(), EtcdMembersHealthy: s.GetEtcdMembersHealthy(),
		TrustedReleaseKeyIDs: slices.Clone(s.GetTrustedReleaseKeyIds()), SelfCoordinationSupported: s.GetSelfCoordinationSupported(),
		RemoteTriggerAllowed: s.GetRemoteTriggerAllowed(),
		Err:                  err, ErrReason: reason,
	}
}

func frigateManifestToProto(m FrigateManifest) *pb.FrigateManifest {
	return &pb.FrigateManifest{
		JobId: m.JobID, SiteId: m.SiteID, ContainerId: m.ContainerID, InstanceId: m.InstanceID,
		ExpectedInstanceIds: slices.Clone(m.ExpectedInstanceIDs), ReleaseTag: m.ReleaseTag, TargetVersion: m.TargetVersion,
		AssetId: m.AssetID, AssetName: m.AssetName, Sha256: m.SHA256, SizeBytes: m.SizeBytes, Actor: m.Actor,
		Coordination: string(m.Coordination), ReleaseStatement: slices.Clone(m.ReleaseStatement), ReleaseSignature: slices.Clone(m.ReleaseSignature),
	}
}

func frigateManifestFromProto(m *pb.FrigateManifest) FrigateManifest {
	return FrigateManifest{
		JobID: m.GetJobId(), SiteID: m.GetSiteId(), ContainerID: m.GetContainerId(), InstanceID: m.GetInstanceId(),
		ExpectedInstanceIDs: slices.Clone(m.GetExpectedInstanceIds()), ReleaseTag: m.GetReleaseTag(), TargetVersion: m.GetTargetVersion(),
		AssetID: m.GetAssetId(), AssetName: m.GetAssetName(), SHA256: m.GetSha256(), SizeBytes: m.GetSizeBytes(), Actor: m.GetActor(),
		Coordination: FrigateCoordination(m.GetCoordination()), ReleaseStatement: slices.Clone(m.GetReleaseStatement()), ReleaseSignature: slices.Clone(m.GetReleaseSignature()),
	}
}

func frigateTimeToProto(t time.Time) *timestamppb.Timestamp {
	if t.IsZero() {
		return nil
	}
	return timestamppb.New(t)
}

func frigateTimeFromProto(t *timestamppb.Timestamp) time.Time {
	if t == nil {
		return time.Time{}
	}
	return t.AsTime()
}

func frigateRolloutToProto(r FrigateRollout) *pb.FrigateRollout {
	instances := make([]*pb.FrigateInstanceProgress, 0, len(r.Instances))
	for _, i := range r.Instances {
		instances = append(instances, &pb.FrigateInstanceProgress{
			InstanceId: i.InstanceID, Phase: string(i.Phase), LastContact: frigateTimeToProto(i.LastContact), RunningVersion: i.RunningVersion,
			MigrationExecuted: i.MigrationExecuted, ErrorCode: i.ErrorCode, ErrorMessage: i.ErrorMessage, ManifestSha256: i.ManifestSHA256, ContactStale: i.ContactStale,
		})
	}
	return &pb.FrigateRollout{
		JobId: r.JobID, TargetVersion: r.TargetVersion, Phase: string(r.Phase), Instances: instances,
		DriverInstanceId: r.DriverInstanceID, MigrationExecutorInstanceId: r.MigrationExecutorInstanceID,
		ErrorCode: r.ErrorCode, ErrorMessage: r.ErrorMessage, NextStep: r.NextStep, StageOnly: r.StageOnly,
		DisruptionStarted: r.DisruptionStarted, Retryable: r.Retryable, Cancellable: r.Cancellable, AutomaticRetries: r.AutomaticRetries,
		Actor: r.Actor, CreatedAt: frigateTimeToProto(r.CreatedAt), UpdatedAt: frigateTimeToProto(r.UpdatedAt),
		StoppedAt: frigateTimeToProto(r.StoppedAt), ExpiresAt: frigateTimeToProto(r.ExpiresAt),
		FromVersion: r.FromVersion, PackageSha256: r.PackageSHA256, ReleaseKeyId: r.ReleaseKeyID,
	}
}

func frigateRolloutFromProto(r *pb.FrigateRollout) FrigateRollout {
	instances := make([]FrigateInstanceProgress, 0, len(r.GetInstances()))
	for _, i := range r.GetInstances() {
		instances = append(instances, FrigateInstanceProgress{
			InstanceID: i.GetInstanceId(), Phase: FrigateAgentPhase(i.GetPhase()), LastContact: frigateTimeFromProto(i.GetLastContact()), RunningVersion: i.GetRunningVersion(),
			MigrationExecuted: i.GetMigrationExecuted(), ErrorCode: i.GetErrorCode(), ErrorMessage: i.GetErrorMessage(), ManifestSHA256: i.GetManifestSha256(), ContactStale: i.GetContactStale(),
		})
	}
	return FrigateRollout{
		JobID: r.GetJobId(), TargetVersion: r.GetTargetVersion(), Phase: FrigateRolloutPhase(r.GetPhase()), Instances: instances,
		DriverInstanceID: r.GetDriverInstanceId(), MigrationExecutorInstanceID: r.GetMigrationExecutorInstanceId(),
		ErrorCode: r.GetErrorCode(), ErrorMessage: r.GetErrorMessage(), NextStep: r.GetNextStep(), StageOnly: r.GetStageOnly(),
		DisruptionStarted: r.GetDisruptionStarted(), Retryable: r.GetRetryable(), Cancellable: r.GetCancellable(), AutomaticRetries: r.GetAutomaticRetries(),
		Actor: r.GetActor(), CreatedAt: frigateTimeFromProto(r.GetCreatedAt()), UpdatedAt: frigateTimeFromProto(r.GetUpdatedAt()),
		StoppedAt: frigateTimeFromProto(r.GetStoppedAt()), ExpiresAt: frigateTimeFromProto(r.GetExpiresAt()),
		FromVersion: r.GetFromVersion(), PackageSHA256: r.GetPackageSha256(), ReleaseKeyID: r.GetReleaseKeyId(),
	}
}

var errFrigateUnexpectedResponse = errors.New("frigate update: plugin returned an unexpected response")

func (d *DeviceGRPCClient) frigateCall(ctx context.Context, req *pb.FrigateServerUpdateRequest) (*pb.FrigateServerUpdateResponse, error) {
	req.Ref = d.deviceRef()
	resp, err := d.client.FrigateServerUpdate(ctx, req)
	if err != nil {
		return nil, frigateRefusalFromWire(err)
	}
	return resp, nil
}

// frigateCallFunc sends one update envelope and returns its response. The
// update calls are written once against it. They serve the plugin's gRPC
// client and a relay that carries the envelope as bytes (see FrigateRelay).
type frigateCallFunc func(ctx context.Context, req *pb.FrigateServerUpdateRequest) (*pb.FrigateServerUpdateResponse, error)

var _ DeviceFrigateUpdater = frigateCallFunc(nil)

func (d *DeviceGRPCClient) FrigateUpdateSupport(ctx context.Context) ([]FrigateInstanceSupport, error) {
	return frigateCallFunc(d.frigateCall).FrigateUpdateSupport(ctx)
}

func (d *DeviceGRPCClient) FrigateStageBegin(ctx context.Context, instanceID string, m FrigateManifest) (FrigateStageProgress, error) {
	return frigateCallFunc(d.frigateCall).FrigateStageBegin(ctx, instanceID, m)
}

func (d *DeviceGRPCClient) FrigateStageAppend(ctx context.Context, instanceID string, a FrigateStageAppend) (FrigateStageProgress, error) {
	return frigateCallFunc(d.frigateCall).FrigateStageAppend(ctx, instanceID, a)
}

func (d *DeviceGRPCClient) FrigateStageCommit(ctx context.Context, instanceID string, r FrigateStageRef) (FrigateStageCommit, error) {
	return frigateCallFunc(d.frigateCall).FrigateStageCommit(ctx, instanceID, r)
}

func (d *DeviceGRPCClient) FrigateStageAbort(ctx context.Context, instanceID string, r FrigateStageRef) error {
	return frigateCallFunc(d.frigateCall).FrigateStageAbort(ctx, instanceID, r)
}

func (d *DeviceGRPCClient) FrigateUpdateStart(ctx context.Context, r FrigateStartRequest) (FrigateRollout, error) {
	return frigateCallFunc(d.frigateCall).FrigateUpdateStart(ctx, r)
}

func (d *DeviceGRPCClient) FrigateUpdateStatus(ctx context.Context, jobID string) (FrigateStatus, error) {
	return frigateCallFunc(d.frigateCall).FrigateUpdateStatus(ctx, jobID)
}

func (d *DeviceGRPCClient) FrigateUpdateRetry(ctx context.Context, a FrigateAction) (FrigateRollout, error) {
	return frigateCallFunc(d.frigateCall).FrigateUpdateRetry(ctx, a)
}

func (d *DeviceGRPCClient) FrigateUpdateCancel(ctx context.Context, a FrigateAction) (FrigateRollout, error) {
	return frigateCallFunc(d.frigateCall).FrigateUpdateCancel(ctx, a)
}

func (call frigateCallFunc) FrigateUpdateSupport(ctx context.Context) ([]FrigateInstanceSupport, error) {
	resp, err := call(ctx, &pb.FrigateServerUpdateRequest{Operation: &pb.FrigateServerUpdateRequest_Support{Support: &pb.FrigateUpdateSupportRequest{}}})
	if err != nil {
		return nil, err
	}
	support := resp.GetSupport()
	if support == nil {
		return nil, errFrigateUnexpectedResponse
	}
	out := make([]FrigateInstanceSupport, 0, len(support.GetInstances()))
	for _, item := range support.GetInstances() {
		out = append(out, frigateSupportFromProto(item))
	}
	return out, nil
}

func (call frigateCallFunc) FrigateStageBegin(ctx context.Context, instanceID string, m FrigateManifest) (FrigateStageProgress, error) {
	if err := validateFrigateInstanceID(instanceID); err != nil {
		return FrigateStageProgress{}, invalidFrigateRequest(err)
	}
	if err := m.ValidateForInstance(instanceID); err != nil {
		return FrigateStageProgress{}, invalidFrigateRequest(err)
	}
	resp, err := call(ctx, &pb.FrigateServerUpdateRequest{Operation: &pb.FrigateServerUpdateRequest_StageBegin{StageBegin: &pb.FrigateStageBeginRequest{InstanceId: instanceID, Manifest: frigateManifestToProto(m)}}})
	if err != nil {
		return FrigateStageProgress{}, err
	}
	return frigateProgressFromResponse(resp)
}

func (call frigateCallFunc) FrigateStageAppend(ctx context.Context, instanceID string, a FrigateStageAppend) (FrigateStageProgress, error) {
	if err := validateFrigateInstanceID(instanceID); err != nil {
		return FrigateStageProgress{}, invalidFrigateRequest(err)
	}
	if err := a.Validate(); err != nil {
		return FrigateStageProgress{}, invalidFrigateRequest(err)
	}
	resp, err := call(ctx, &pb.FrigateServerUpdateRequest{Operation: &pb.FrigateServerUpdateRequest_StageAppend{StageAppend: &pb.FrigateStageAppendRequest{InstanceId: instanceID, JobId: a.JobID, ManifestSha256: a.ManifestSHA256, Offset: a.Offset, Chunk: a.Chunk, Actor: a.Actor}}})
	if err != nil {
		return FrigateStageProgress{}, err
	}
	return frigateProgressFromResponse(resp)
}

func frigateProgressFromResponse(resp *pb.FrigateServerUpdateResponse) (FrigateStageProgress, error) {
	p := resp.GetStageProgress()
	if p == nil {
		return FrigateStageProgress{}, errFrigateUnexpectedResponse
	}
	return FrigateStageProgress{JobID: p.GetJobId(), ManifestSHA256: p.GetManifestSha256(), NextOffset: p.GetNextOffset(), SizeBytes: p.GetSizeBytes()}, nil
}

func frigateCommitFromResponse(resp *pb.FrigateServerUpdateResponse) (FrigateStageCommit, error) {
	c := resp.GetStageCommit()
	if c == nil {
		return FrigateStageCommit{}, errFrigateUnexpectedResponse
	}
	return FrigateStageCommit{JobID: c.GetJobId(), BytesStaged: c.GetBytesStaged(), SHA256: c.GetSha256()}, nil
}

func (call frigateCallFunc) FrigateStageCommit(ctx context.Context, instanceID string, r FrigateStageRef) (FrigateStageCommit, error) {
	if err := validateFrigateInstanceID(instanceID); err != nil {
		return FrigateStageCommit{}, invalidFrigateRequest(err)
	}
	if err := r.Validate(); err != nil {
		return FrigateStageCommit{}, invalidFrigateRequest(err)
	}
	resp, err := call(ctx, &pb.FrigateServerUpdateRequest{Operation: &pb.FrigateServerUpdateRequest_StageCommit{StageCommit: &pb.FrigateStageCommitRequest{InstanceId: instanceID, JobId: r.JobID, ManifestSha256: r.ManifestSHA256, Actor: r.Actor}}})
	if err != nil {
		return FrigateStageCommit{}, err
	}
	return frigateCommitFromResponse(resp)
}

func (call frigateCallFunc) FrigateStageAbort(ctx context.Context, instanceID string, r FrigateStageRef) error {
	if err := validateFrigateInstanceID(instanceID); err != nil {
		return invalidFrigateRequest(err)
	}
	if err := r.Validate(); err != nil {
		return invalidFrigateRequest(err)
	}
	_, err := call(ctx, &pb.FrigateServerUpdateRequest{Operation: &pb.FrigateServerUpdateRequest_StageAbort{StageAbort: &pb.FrigateStageAbortRequest{InstanceId: instanceID, JobId: r.JobID, ManifestSha256: r.ManifestSHA256, Actor: r.Actor}}})
	return err
}

func (call frigateCallFunc) FrigateUpdateStart(ctx context.Context, r FrigateStartRequest) (FrigateRollout, error) {
	if err := r.Validate(); err != nil {
		return FrigateRollout{}, invalidFrigateRequest(err)
	}
	resp, err := call(ctx, &pb.FrigateServerUpdateRequest{Operation: &pb.FrigateServerUpdateRequest_Start{Start: &pb.FrigateUpdateStartRequest{
		JobId: r.JobID, TargetVersion: r.TargetVersion, ExpectedInstanceIds: slices.Clone(r.ExpectedInstanceIDs), StageOnly: r.StageOnly,
		IdempotencyKey: r.IdempotencyKey, ExpectedManifestSha256: cloneStringMap(r.ExpectedManifestSHA256), Actor: r.Actor,
	}}})
	if err != nil {
		return FrigateRollout{}, err
	}
	return frigateRolloutFromResponse(resp)
}

func (call frigateCallFunc) FrigateUpdateStatus(ctx context.Context, jobID string) (FrigateStatus, error) {
	if err := validateFrigateStatusJobID(jobID); err != nil {
		return FrigateStatus{}, invalidFrigateRequest(err)
	}
	resp, err := call(ctx, &pb.FrigateServerUpdateRequest{Operation: &pb.FrigateServerUpdateRequest_Status{Status: &pb.FrigateUpdateStatusRequest{JobId: jobID}}})
	if err != nil {
		return FrigateStatus{}, err
	}
	st := resp.GetStatus()
	if st == nil {
		return FrigateStatus{}, errFrigateUnexpectedResponse
	}
	return FrigateStatus{Rollout: frigateRolloutFromProto(st.GetRollout()), ServedByMaintenance: st.GetServedByMaintenance(), AnsweredBy: st.GetAnsweredBy()}, nil
}

func (call frigateCallFunc) FrigateUpdateRetry(ctx context.Context, a FrigateAction) (FrigateRollout, error) {
	if err := a.Validate(); err != nil {
		return FrigateRollout{}, invalidFrigateRequest(err)
	}
	resp, err := call(ctx, &pb.FrigateServerUpdateRequest{Operation: &pb.FrigateServerUpdateRequest_Retry{Retry: &pb.FrigateUpdateRetryRequest{JobId: a.JobID, IdempotencyKey: a.IdempotencyKey, Actor: a.Actor}}})
	if err != nil {
		return FrigateRollout{}, err
	}
	return frigateRolloutFromResponse(resp)
}

func (call frigateCallFunc) FrigateUpdateCancel(ctx context.Context, a FrigateAction) (FrigateRollout, error) {
	if err := a.Validate(); err != nil {
		return FrigateRollout{}, invalidFrigateRequest(err)
	}
	resp, err := call(ctx, &pb.FrigateServerUpdateRequest{Operation: &pb.FrigateServerUpdateRequest_Cancel{Cancel: &pb.FrigateUpdateCancelRequest{JobId: a.JobID, IdempotencyKey: a.IdempotencyKey, Actor: a.Actor}}})
	if err != nil {
		return FrigateRollout{}, err
	}
	return frigateRolloutFromResponse(resp)
}

func frigateRolloutFromResponse(resp *pb.FrigateServerUpdateResponse) (FrigateRollout, error) {
	r := resp.GetRollout()
	if r == nil {
		return FrigateRollout{}, errFrigateUnexpectedResponse
	}
	return frigateRolloutFromProto(r), nil
}
