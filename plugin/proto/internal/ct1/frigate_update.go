package ct1

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	mdk "github.com/block/proto-fleet/sdk/mdk/generated/mdk/v2"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	frigateActorHeader        = "x-frigate-actor"
	defaultFrigateActor       = "proto-fleet"
	frigateUpdateBulkTimeout  = 60 * time.Second
	frigateReasonPairMismatch = "FLEET_PAIR_MISMATCH"
)

// frigateUpdateCallTimeout bounds each control-plane call to one controller.
// It is a variable so tests can shorten it.
var frigateUpdateCallTimeout = 15 * time.Second

var _ sdk.DeviceFrigateUpdater = (*Device)(nil)

type actorContextKey struct{}

// withActor overrides the x-frigate-actor audit attribution for calls made
// with the returned context. The value is attribution only, not authentication.
func withActor(ctx context.Context, actor string) context.Context {
	return context.WithValue(ctx, actorContextKey{}, actor)
}

// outgoingActor returns ctx with exactly one x-frigate-actor value: the
// withActor override when present, otherwise the default attribution.
func outgoingActor(ctx context.Context) context.Context {
	actor := defaultFrigateActor
	if override, ok := ctx.Value(actorContextKey{}).(string); ok && override != "" {
		actor = override
	}
	md, _ := metadata.FromOutgoingContext(ctx)
	md = md.Copy()
	md.Set(frigateActorHeader, actor)
	return metadata.NewOutgoingContext(ctx, md)
}

// updateOrder remembers which controller last answered so status, retry and
// cancel try a reachable one first. It is a hint only; correctness never
// depends on it because every call falls through the remaining controllers.
type updateOrder struct {
	mu   sync.Mutex
	last int
}

func (o *updateOrder) order(n int) []int {
	o.mu.Lock()
	defer o.mu.Unlock()
	indexes := make([]int, 0, n)
	if o.last >= 0 && o.last < n {
		indexes = append(indexes, o.last)
	}
	for i := range n {
		if i != o.last {
			indexes = append(indexes, i)
		}
	}
	return indexes
}

func (o *updateOrder) remember(i int) {
	o.mu.Lock()
	o.last = i
	o.mu.Unlock()
}

// frigateClient returns the controller whose Frigate instance ID is id.
func (d *Device) frigateClient(id string) (*Client, error) {
	for _, c := range d.clients {
		if c.memberID != "" && c.memberID == id {
			return c, nil
		}
	}
	return nil, status.Errorf(codes.InvalidArgument, "CT1 device has no controller with instance ID %q", id)
}

// frigateError keeps a Frigate refusal reason when the controller sent one.
func frigateError(err error) error {
	if err == nil {
		return nil
	}
	if refusal := sdk.AsFrigateRefusal(err); refusal != nil {
		return refusal
	}
	return err
}

// supportResult is one controller's support answer. err keeps the original
// error so callers can tell a Frigate refusal from a transport failure.
type supportResult struct {
	support sdk.FrigateInstanceSupport
	err     error
}

func (d *Device) collectSupport(ctx context.Context) []supportResult {
	results := make([]supportResult, len(d.clients))
	var wg sync.WaitGroup
	for i, c := range d.clients {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i] = c.frigateSupport(ctx)
		}()
	}
	wg.Wait()
	return results
}

func (d *Device) FrigateUpdateSupport(ctx context.Context) ([]sdk.FrigateInstanceSupport, error) {
	results := d.collectSupport(ctx)
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("frigate update support: %w", err)
	}
	supports := make([]sdk.FrigateInstanceSupport, len(results))
	for i, r := range results {
		supports[i] = r.support
	}
	return supports, nil
}

func (c *Client) frigateSupport(ctx context.Context) supportResult {
	ctx, cancel := context.WithTimeout(ctx, frigateUpdateCallTimeout)
	defer cancel()
	wire, err := c.serverUpdate.GetServerUpdateSupport(ctx, &mdk.GetServerUpdateSupportRequest{}, grpc.WaitForReady(false))
	if err == nil && c.memberID != "" && wire.GetInstanceId() != c.memberID {
		err = pairRefusal("controller reported instance %q, expected %q", wire.GetInstanceId(), c.memberID)
	}
	if err != nil {
		failed := sdk.FrigateInstanceSupport{InstanceID: c.memberID, Err: err.Error()}
		if refusal := sdk.AsFrigateRefusal(err); refusal != nil {
			failed.ErrReason = refusal.Reason
			failed.Err = refusal.Message
			if failed.Err == "" {
				failed.Err = refusal.Error()
			}
			err = refusal
		}
		return supportResult{support: failed, err: err}
	}
	return supportResult{support: sdk.FrigateInstanceSupport{
		InstanceID:                wire.GetInstanceId(),
		PeerInstanceID:            wire.GetPeerInstanceId(),
		RunningVersion:            wire.GetRunningVersion(),
		Architecture:              wire.GetArchitecture(),
		InstallationType:          installationTypeFromWire(wire.GetInstallationType()),
		HelperAvailable:           wire.GetHelperAvailable(),
		SupportMessage:            wire.GetSupportMessage(),
		ClusterID:                 wire.GetClusterId(),
		ActiveJobID:               wire.GetActiveJobId(),
		SpoolFreeBytes:            wire.GetSpoolFreeBytes(),
		EtcdMembersTotal:          wire.GetEtcdMembersTotal(),
		EtcdMembersHealthy:        wire.GetEtcdMembersHealthy(),
		TrustedReleaseKeyIDs:      slices.Clone(wire.GetTrustedReleaseKeyIds()),
		SelfCoordinationSupported: wire.GetSelfCoordinationSupported(),
		RemoteTriggerAllowed:      wire.GetRemoteTriggerAllowed(),
		ProtoOS:                   protoOSSupportFromWire(wire),
	}}
}

func protoOSSupportFromWire(wire *mdk.GetServerUpdateSupportResponse) sdk.FrigateProtoOSSupport {
	job := wire.GetProtoOsJob()
	return sdk.FrigateProtoOSSupport{
		SelfSupported:       wire.GetProtoOsSelfSupported(),
		SupportMessage:      wire.GetProtoOsSupportMessage(),
		InstalledVersion:    wire.GetProtoOsInstalledVersion(),
		ReleaseRepositories: slices.Clone(wire.GetProtoOsReleaseRepositories()),
		Served: sdk.FrigateProtoOSBundle{
			Source:     protoOSBundleSourceFromWire(wire.GetProtoOsServed().GetSource()),
			ReleaseTag: wire.GetProtoOsServed().GetReleaseTag(),
		},
		Job: sdk.FrigateProtoOSJob{
			JobID: job.GetJobId(), ReleaseTag: job.GetReleaseTag(), TargetVersion: job.GetTargetVersion(),
			State: protoOSJobStateFromWire(job.GetState()), Message: job.GetMessage(), UpdatedAt: timeFromWire(job.GetUpdatedAt()),
		},
	}
}

func (d *Device) FrigateStageBegin(ctx context.Context, instanceID string, m sdk.FrigateManifest) (sdk.FrigateStageProgress, error) {
	c, err := d.frigateClient(instanceID)
	if err != nil {
		return sdk.FrigateStageProgress{}, err
	}
	// Refuse before anything is sent: a manifest for another instance must
	// never be staged on this controller.
	if m.InstanceID != instanceID || (c.memberID != "" && m.InstanceID != c.memberID) {
		return sdk.FrigateStageProgress{}, status.Errorf(codes.InvalidArgument, "manifest instance %q does not match target controller %q", m.InstanceID, instanceID)
	}
	ctx, cancel := context.WithTimeout(withActor(ctx, sdk.FrigateAuditActor(m.Actor)), frigateUpdateCallTimeout)
	defer cancel()
	wire, err := c.serverUpdate.BeginServerUpdateStage(ctx, &mdk.BeginServerUpdateStageRequest{Manifest: manifestToWire(m)}, grpc.WaitForReady(false))
	if err != nil {
		return sdk.FrigateStageProgress{}, frigateError(err)
	}
	return sdk.FrigateStageProgress{JobID: wire.GetJobId(), ManifestSHA256: wire.GetManifestSha256(), NextOffset: wire.GetNextOffset(), SizeBytes: wire.GetSizeBytes()}, nil
}

func (d *Device) FrigateStageAppend(ctx context.Context, instanceID string, a sdk.FrigateStageAppend) (sdk.FrigateStageProgress, error) {
	c, err := d.frigateClient(instanceID)
	if err != nil {
		return sdk.FrigateStageProgress{}, err
	}
	ctx, cancel := context.WithTimeout(withActor(ctx, sdk.FrigateAuditActor(a.Actor)), frigateUpdateBulkTimeout)
	defer cancel()
	wire, err := c.serverUpdate.AppendServerUpdateStage(ctx, &mdk.AppendServerUpdateStageRequest{JobId: a.JobID, ManifestSha256: a.ManifestSHA256, Offset: a.Offset, Chunk: a.Chunk}, grpc.WaitForReady(false))
	if err != nil {
		return sdk.FrigateStageProgress{}, frigateError(err)
	}
	return sdk.FrigateStageProgress{JobID: wire.GetJobId(), ManifestSHA256: wire.GetManifestSha256(), NextOffset: wire.GetNextOffset(), SizeBytes: wire.GetSizeBytes()}, nil
}

func (d *Device) FrigateStageCommit(ctx context.Context, instanceID string, r sdk.FrigateStageRef) (sdk.FrigateStageCommit, error) {
	c, err := d.frigateClient(instanceID)
	if err != nil {
		return sdk.FrigateStageCommit{}, err
	}
	ctx, cancel := context.WithTimeout(withActor(ctx, sdk.FrigateAuditActor(r.Actor)), frigateUpdateBulkTimeout)
	defer cancel()
	wire, err := c.serverUpdate.CommitServerUpdateStage(ctx, &mdk.CommitServerUpdateStageRequest{JobId: r.JobID, ManifestSha256: r.ManifestSHA256}, grpc.WaitForReady(false))
	if err != nil {
		return sdk.FrigateStageCommit{}, frigateError(err)
	}
	return sdk.FrigateStageCommit{JobID: wire.GetJobId(), BytesStaged: wire.GetBytesStaged(), SHA256: wire.GetSha256()}, nil
}

func (d *Device) FrigateStageAbort(ctx context.Context, instanceID string, r sdk.FrigateStageRef) error {
	c, err := d.frigateClient(instanceID)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(withActor(ctx, sdk.FrigateAuditActor(r.Actor)), frigateUpdateCallTimeout)
	defer cancel()
	_, err = c.serverUpdate.AbortServerUpdateStage(ctx, &mdk.AbortServerUpdateStageRequest{JobId: r.JobID, ManifestSha256: r.ManifestSHA256}, grpc.WaitForReady(false))
	return frigateError(err)
}

// invalidFrigateRequest reports a request the device refuses to send.
func invalidFrigateRequest(err error) error {
	return status.Error(codes.InvalidArgument, err.Error()) //nolint:wrapcheck // The gRPC status is the plugin boundary error.
}

// FrigateUpdateStart validates the pair through update support, then asks a
// controller to start the rollout. Frigate routes the request to the control
// leader. A second controller is tried only after a failure that proves, or
// (with the mandatory idempotency key) tolerates, the first never applied the
// request. A deadline on a connected controller is ambiguous and returned as is:
// the caller reconciles through FrigateUpdateStatus.
func (d *Device) FrigateUpdateStart(ctx context.Context, r sdk.FrigateStartRequest) (sdk.FrigateRollout, error) {
	if err := r.Validate(); err != nil {
		return sdk.FrigateRollout{}, invalidFrigateRequest(err)
	}
	results := d.collectSupport(ctx)
	if err := ctx.Err(); err != nil {
		return sdk.FrigateRollout{}, fmt.Errorf("frigate update start: %w", err)
	}
	supports := make([]sdk.FrigateInstanceSupport, len(results))
	var failures []string
	var failed error
	for i, res := range results {
		supports[i] = res.support
		if res.err == nil {
			continue
		}
		// A Frigate refusal (even with code Unavailable) is final and intact.
		if refusal := sdk.AsFrigateRefusal(res.err); refusal != nil {
			return sdk.FrigateRollout{}, refusal
		}
		failed = res.err
		failures = append(failures, fmt.Sprintf("%s: %s", res.support.InstanceID, status.Convert(res.err).Message()))
	}
	if failed != nil {
		return sdk.FrigateRollout{}, status.Errorf(status.Code(failed), "cannot start a Frigate update while a controller cannot be queried (%s)", strings.Join(failures, "; "))
	}
	if err := d.validateFrigatePair(supports, r.ExpectedInstanceIDs); err != nil {
		return sdk.FrigateRollout{}, err
	}
	if err := d.verifyFrigateIdentity(ctx); err != nil {
		return sdk.FrigateRollout{}, err
	}
	wire := &mdk.StartServerUpdateRequest{JobId: r.JobID, TargetVersion: r.TargetVersion, ExpectedInstanceIds: slices.Clone(r.ExpectedInstanceIDs), StageOnly: r.StageOnly, IdempotencyKey: r.IdempotencyKey, ExpectedManifestSha256: cloneStringMap(r.ExpectedManifestSHA256)}
	rollout, _, err := d.rolloutCall(ctx, sdk.FrigateAuditActor(r.Actor), mutatingCall, func(ctx context.Context, c *Client) (*mdk.ServerUpdateRollout, error) {
		resp, err := c.serverUpdate.StartServerUpdate(ctx, wire, grpc.WaitForReady(false))
		return resp.GetRollout(), err
	})
	return rollout, err
}

func (d *Device) FrigateUpdateStatus(ctx context.Context, jobID string) (sdk.FrigateStatus, error) {
	var served bool
	rollout, answeredBy, err := d.rolloutCall(ctx, defaultFrigateActor, readOnlyCall, func(ctx context.Context, c *Client) (*mdk.ServerUpdateRollout, error) {
		resp, err := c.serverUpdate.GetServerUpdateStatus(ctx, &mdk.GetServerUpdateStatusRequest{JobId: jobID}, grpc.WaitForReady(false))
		served = resp.GetServedByMaintenance()
		return resp.GetRollout(), err
	})
	if err != nil {
		return sdk.FrigateStatus{}, err
	}
	return sdk.FrigateStatus{Rollout: rollout, ServedByMaintenance: served, AnsweredBy: answeredBy}, nil
}

func (d *Device) FrigateUpdateRetry(ctx context.Context, a sdk.FrigateAction) (sdk.FrigateRollout, error) {
	if err := a.Validate(); err != nil {
		return sdk.FrigateRollout{}, invalidFrigateRequest(err)
	}
	rollout, _, err := d.rolloutCall(ctx, sdk.FrigateAuditActor(a.Actor), mutatingCall, func(ctx context.Context, c *Client) (*mdk.ServerUpdateRollout, error) {
		resp, err := c.serverUpdate.RetryServerUpdate(ctx, &mdk.RetryServerUpdateRequest{JobId: a.JobID, IdempotencyKey: a.IdempotencyKey}, grpc.WaitForReady(false))
		return resp.GetRollout(), err
	})
	return rollout, err
}

func (d *Device) FrigateUpdateCancel(ctx context.Context, a sdk.FrigateAction) (sdk.FrigateRollout, error) {
	if err := a.Validate(); err != nil {
		return sdk.FrigateRollout{}, invalidFrigateRequest(err)
	}
	rollout, _, err := d.rolloutCall(ctx, sdk.FrigateAuditActor(a.Actor), mutatingCall, func(ctx context.Context, c *Client) (*mdk.ServerUpdateRollout, error) {
		resp, err := c.serverUpdate.CancelServerUpdate(ctx, &mdk.CancelServerUpdateRequest{JobId: a.JobID, IdempotencyKey: a.IdempotencyKey}, grpc.WaitForReady(false))
		return resp.GetRollout(), err
	})
	return rollout, err
}

type callKind int

const (
	// readOnlyCall may be repeated freely on the other controller.
	readOnlyCall callKind = iota
	// mutatingCall carries an idempotency key, so a repeat on the other
	// controller returns Frigate's stored result instead of applying twice.
	mutatingCall
)

// fallsThrough reports whether a failed attempt may be repeated on the next
// controller. A Frigate refusal never does, whatever its gRPC code: it is the
// pair's answer. Unavailable is a transport failure. A deadline is final for a
// mutating call on a connected channel, where the request may have been
// applied, but proves nothing was sent when the channel was not READY.
func fallsThrough(kind callKind, err error, wasReady bool) bool {
	if sdk.AsFrigateRefusal(err) != nil {
		return false
	}
	code := status.Code(err)
	if code == codes.Unavailable {
		return true
	}
	return code == codes.DeadlineExceeded && (kind == readOnlyCall || !wasReady)
}

// controllerOrder lists controller indexes with connected channels first, then
// idle ones, then the rest, preserving the sticky last-good preference within a
// class. It also returns whether each channel was READY now, which decides
// whether a later deadline may be treated as never sent.
func (d *Device) controllerOrder() ([]int, []bool) {
	states := make([]connectivity.State, len(d.clients))
	ready := make([]bool, len(d.clients))
	for i, c := range d.clients {
		states[i] = c.conn.GetState()
		ready[i] = states[i] == connectivity.Ready
	}
	rank := func(i int) int {
		if states[i] == connectivity.Ready {
			return 0
		}
		if states[i] == connectivity.Idle {
			return 1
		}
		return 2
	}
	order := d.updateOrder.order(len(d.clients))
	slices.SortStableFunc(order, func(a, b int) int { return rank(a) - rank(b) })
	return order, ready
}

// rolloutCall runs call against the controllers in preference order. It is
// deliberately not gated on HA probing: a stopped Frigate answers rollout
// status through its maintenance service while health probes fail. When every
// controller fails without a Frigate answer, the last failure's gRPC code is
// returned with every controller's detail in the message.
func (d *Device) rolloutCall(ctx context.Context, actor string, kind callKind, call func(context.Context, *Client) (*mdk.ServerUpdateRollout, error)) (sdk.FrigateRollout, string, error) {
	order, ready := d.controllerOrder()
	var failures []string
	var last error
	for _, i := range order {
		c := d.clients[i]
		attempt, cancel := context.WithTimeout(withActor(ctx, actor), frigateUpdateCallTimeout)
		rollout, err := call(attempt, c)
		cancel()
		if err == nil {
			if rollout == nil {
				return sdk.FrigateRollout{}, "", status.Errorf(codes.Internal, "controller %q returned no rollout", c.memberID)
			}
			d.updateOrder.remember(i)
			return rolloutFromWire(rollout), c.memberID, nil
		}
		if ctx.Err() != nil || !fallsThrough(kind, err, ready[i]) {
			return sdk.FrigateRollout{}, "", frigateError(err)
		}
		last = err
		failures = append(failures, fmt.Sprintf("%s: %s", c.memberID, status.Convert(err).Message()))
	}
	return sdk.FrigateRollout{}, "", status.Errorf(status.Code(last), "no CT1 controller answered the update request (%s)", strings.Join(failures, "; "))
}

// frigateHAKey returns the device's ct1:<uuid> HA identity key, or "" when the
// device has none.
func (d *Device) frigateHAKey() string {
	if len(d.info.Endpoints) == 0 {
		return ""
	}
	if key := d.info.Endpoints[0].DeviceKey; strings.HasPrefix(key, "ct1:") {
		return key
	}
	return ""
}

// validateFrigatePair checks that every expected controller answered update
// support, that a pair shares one etcd cluster, and that each one allows a
// Fleet-triggered, self-coordinated rollout. Update support reports the etcd
// cluster that holds the rollout state, not the HA cluster UUID in the device
// key, so only the controllers' reports are compared here;
// verifyFrigateIdentity binds them to this device. It never fails open: a pair
// must report one shared non-empty etcd cluster ID.
func (d *Device) validateFrigatePair(supports []sdk.FrigateInstanceSupport, expected []string) error {
	ids := make([]string, 0, len(supports))
	for _, s := range supports {
		ids = append(ids, s.InstanceID)
	}
	want := slices.Clone(expected)
	slices.Sort(want)
	slices.Sort(ids)
	if !slices.Equal(want, ids) {
		return pairRefusal("expected controllers %v do not match this device's controllers %v", want, ids)
	}
	if len(supports) >= 2 {
		cluster := supports[0].ClusterID
		if cluster == "" {
			return pairRefusal("controller %s reports no etcd cluster", supports[0].InstanceID)
		}
		for _, s := range supports[1:] {
			if !strings.EqualFold(s.ClusterID, cluster) {
				return pairRefusal("controllers %s and %s report different etcd clusters (%q and %q)", supports[0].InstanceID, s.InstanceID, cluster, s.ClusterID)
			}
		}
	}
	for _, s := range supports {
		if !s.RemoteTriggerAllowed {
			return &sdk.FrigateRefusal{Reason: "UPDATE_REMOTE_TRIGGER_DISABLED", Code: codes.FailedPrecondition, Message: fmt.Sprintf("controller %s does not allow remotely triggered updates", s.InstanceID)}
		}
		if !s.SelfCoordinationSupported {
			return &sdk.FrigateRefusal{Reason: "UPDATE_SELF_COORDINATION_UNSUPPORTED", Code: codes.FailedPrecondition, Message: fmt.Sprintf("controller %s does not support self-coordinated updates", s.InstanceID)}
		}
	}
	return nil
}

// verifyFrigateIdentity re-reads every controller's HA identity and requires it
// to still be this device's paired cluster, so an update never starts on
// controllers that were re-addressed into another cluster. A device without a
// ct1:<uuid> key has no HA identity to check.
func (d *Device) verifyFrigateIdentity(ctx context.Context) error {
	key := d.frigateHAKey()
	if key == "" {
		return nil
	}
	errs := make([]error, len(d.clients))
	var wg sync.WaitGroup
	for i, c := range d.clients {
		wg.Add(1)
		go func() {
			defer wg.Done()
			callCtx, cancel := context.WithTimeout(ctx, frigateUpdateCallTimeout)
			defer cancel()
			_, _, errs[i] = c.readHA(callCtx, c.memberID, key)
		}()
	}
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("frigate update start: %w", err)
	}
	for i, err := range errs {
		if err == nil {
			continue
		}
		id := d.clients[i].memberID
		// A transport failure is not proof of a different cluster; keep its code.
		if s, ok := status.FromError(err); ok && s.Code() != codes.Unknown {
			return status.Errorf(s.Code(), "cannot verify the HA identity of controller %s: %s", id, s.Message())
		}
		return pairRefusal("controller %s is not this device's CT1 cluster: %v", id, err)
	}
	return nil
}

// pairRefusal is a Fleet-synthesized refusal: Frigate never sent it.
func pairRefusal(format string, args ...any) error {
	return &sdk.FrigateRefusal{Reason: frigateReasonPairMismatch, Code: codes.FailedPrecondition, Message: fmt.Sprintf(format, args...)}
}

// cloneStringMap copies the caller's fingerprint map so the request sent to
// Frigate never aliases it.
func cloneStringMap(in map[string]string) map[string]string {
	if in == nil {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func manifestToWire(m sdk.FrigateManifest) *mdk.ServerUpdateManifest {
	wire := &mdk.ServerUpdateManifest{
		JobId: m.JobID, SiteId: m.SiteID, ContainerId: m.ContainerID, InstanceId: m.InstanceID,
		ExpectedInstanceIds: slices.Clone(m.ExpectedInstanceIDs), ReleaseTag: m.ReleaseTag, TargetVersion: m.TargetVersion,
		AssetId: m.AssetID, AssetName: m.AssetName, Sha256: m.SHA256, SizeBytes: m.SizeBytes, Actor: sdk.FrigateAuditActor(m.Actor),
		ReleaseStatement: slices.Clone(m.ReleaseStatement), ReleaseSignature: slices.Clone(m.ReleaseSignature),
	}
	if m.Coordination == sdk.FrigateCoordinationSelf {
		wire.Coordination = mdk.ServerUpdateCoordination_SERVER_UPDATE_COORDINATION_SELF
	}
	if m.Package == sdk.FrigatePackageProtoOS {
		wire.Package = mdk.ServerUpdatePackage_SERVER_UPDATE_PACKAGE_PROTO_OS
	}
	return wire
}

func protoOSBundleSourceFromWire(s mdk.ProtoOSBundleSource) sdk.FrigateProtoOSBundleSource {
	switch s {
	case mdk.ProtoOSBundleSource_PROTO_OS_BUNDLE_SOURCE_UNSPECIFIED:
		return sdk.FrigateProtoOSBundleUnspecified
	case mdk.ProtoOSBundleSource_PROTO_OS_BUNDLE_SOURCE_NONE:
		return sdk.FrigateProtoOSBundleNone
	case mdk.ProtoOSBundleSource_PROTO_OS_BUNDLE_SOURCE_PACKAGE:
		return sdk.FrigateProtoOSBundlePackage
	case mdk.ProtoOSBundleSource_PROTO_OS_BUNDLE_SOURCE_BUNDLED:
		return sdk.FrigateProtoOSBundleBundled
	default:
		return sdk.FrigateProtoOSBundleUnknown
	}
}

func protoOSJobStateFromWire(s mdk.ProtoOSPackageJobState) sdk.FrigateProtoOSJobState {
	switch s {
	case mdk.ProtoOSPackageJobState_PROTO_OS_PACKAGE_JOB_STATE_UNSPECIFIED:
		return sdk.FrigateProtoOSJobUnspecified
	case mdk.ProtoOSPackageJobState_PROTO_OS_PACKAGE_JOB_STATE_QUEUED:
		return sdk.FrigateProtoOSJobQueued
	case mdk.ProtoOSPackageJobState_PROTO_OS_PACKAGE_JOB_STATE_INSTALLING:
		return sdk.FrigateProtoOSJobInstalling
	case mdk.ProtoOSPackageJobState_PROTO_OS_PACKAGE_JOB_STATE_SUCCEEDED:
		return sdk.FrigateProtoOSJobSucceeded
	case mdk.ProtoOSPackageJobState_PROTO_OS_PACKAGE_JOB_STATE_FAILED:
		return sdk.FrigateProtoOSJobFailed
	default:
		return sdk.FrigateProtoOSJobUnknown
	}
}

func installationTypeFromWire(t mdk.ServerInstallationType) sdk.FrigateInstallationType {
	switch t {
	case mdk.ServerInstallationType_SERVER_INSTALLATION_TYPE_UNSPECIFIED:
		return sdk.FrigateInstallationUnspecified
	case mdk.ServerInstallationType_SERVER_INSTALLATION_TYPE_DEBIAN_PACKAGE:
		return sdk.FrigateInstallationDebianPackage
	case mdk.ServerInstallationType_SERVER_INSTALLATION_TYPE_DIRECT_BINARY:
		return sdk.FrigateInstallationDirectBinary
	default:
		return sdk.FrigateInstallationUnknown
	}
}

func rolloutPhaseFromWire(p mdk.ServerUpdateRolloutPhase) sdk.FrigateRolloutPhase {
	switch p {
	case mdk.ServerUpdateRolloutPhase_SERVER_UPDATE_ROLLOUT_PHASE_UNSPECIFIED:
		return sdk.FrigatePhaseUnspecified
	case mdk.ServerUpdateRolloutPhase_SERVER_UPDATE_ROLLOUT_PHASE_VERIFYING:
		return sdk.FrigatePhaseVerifying
	case mdk.ServerUpdateRolloutPhase_SERVER_UPDATE_ROLLOUT_PHASE_STOPPING:
		return sdk.FrigatePhaseStopping
	case mdk.ServerUpdateRolloutPhase_SERVER_UPDATE_ROLLOUT_PHASE_INSTALLING:
		return sdk.FrigatePhaseInstalling
	case mdk.ServerUpdateRolloutPhase_SERVER_UPDATE_ROLLOUT_PHASE_MIGRATING:
		return sdk.FrigatePhaseMigrating
	case mdk.ServerUpdateRolloutPhase_SERVER_UPDATE_ROLLOUT_PHASE_STARTING:
		return sdk.FrigatePhaseStarting
	case mdk.ServerUpdateRolloutPhase_SERVER_UPDATE_ROLLOUT_PHASE_SUCCEEDED:
		return sdk.FrigatePhaseSucceeded
	case mdk.ServerUpdateRolloutPhase_SERVER_UPDATE_ROLLOUT_PHASE_FAILED:
		return sdk.FrigatePhaseFailed
	case mdk.ServerUpdateRolloutPhase_SERVER_UPDATE_ROLLOUT_PHASE_CANCELLED:
		return sdk.FrigatePhaseCancelled
	case mdk.ServerUpdateRolloutPhase_SERVER_UPDATE_ROLLOUT_PHASE_VERIFIED:
		return sdk.FrigatePhaseVerified
	case mdk.ServerUpdateRolloutPhase_SERVER_UPDATE_ROLLOUT_PHASE_ABANDONED:
		return sdk.FrigatePhaseAbandoned
	default:
		return sdk.FrigatePhaseUnknown
	}
}

func agentPhaseFromWire(p mdk.FrigateUpdateAgentPhase) sdk.FrigateAgentPhase {
	switch p {
	case mdk.FrigateUpdateAgentPhase_FRIGATE_UPDATE_AGENT_PHASE_UNSPECIFIED:
		return sdk.FrigateAgentUnspecified
	case mdk.FrigateUpdateAgentPhase_FRIGATE_UPDATE_AGENT_PHASE_CONNECTING:
		return sdk.FrigateAgentConnecting
	case mdk.FrigateUpdateAgentPhase_FRIGATE_UPDATE_AGENT_PHASE_STAGED:
		return sdk.FrigateAgentStaged
	case mdk.FrigateUpdateAgentPhase_FRIGATE_UPDATE_AGENT_PHASE_STOPPED:
		return sdk.FrigateAgentStopped
	case mdk.FrigateUpdateAgentPhase_FRIGATE_UPDATE_AGENT_PHASE_INSTALLED:
		return sdk.FrigateAgentInstalled
	case mdk.FrigateUpdateAgentPhase_FRIGATE_UPDATE_AGENT_PHASE_MIGRATED:
		return sdk.FrigateAgentMigrated
	case mdk.FrigateUpdateAgentPhase_FRIGATE_UPDATE_AGENT_PHASE_STARTED:
		return sdk.FrigateAgentStarted
	case mdk.FrigateUpdateAgentPhase_FRIGATE_UPDATE_AGENT_PHASE_FAILED:
		return sdk.FrigateAgentFailed
	default:
		return sdk.FrigateAgentUnknown
	}
}

func timeFromWire(t *timestamppb.Timestamp) time.Time {
	if t == nil {
		return time.Time{}
	}
	return t.AsTime()
}

func rolloutFromWire(r *mdk.ServerUpdateRollout) sdk.FrigateRollout {
	out := sdk.FrigateRollout{
		JobID: r.GetJobId(), TargetVersion: r.GetTargetVersion(), Phase: rolloutPhaseFromWire(r.GetPhase()),
		DriverInstanceID: r.GetDriverInstanceId(), MigrationExecutorInstanceID: r.GetMigrationExecutorInstanceId(),
		ErrorCode: r.GetErrorCode(), ErrorMessage: r.GetErrorMessage(), NextStep: r.GetNextStep(),
		StageOnly: r.GetStageOnly(), DisruptionStarted: r.GetDisruptionStarted(), Retryable: r.GetRetryable(), Cancellable: r.GetCancellable(),
		AutomaticRetries: r.GetAutomaticRetries(), Actor: r.GetActor(),
		CreatedAt: timeFromWire(r.GetCreatedAt()), UpdatedAt: timeFromWire(r.GetUpdatedAt()), StoppedAt: timeFromWire(r.GetStoppedAt()), ExpiresAt: timeFromWire(r.GetExpiresAt()),
		FromVersion: r.GetFromVersion(), PackageSHA256: r.GetPackageSha256(), ReleaseKeyID: r.GetReleaseKeyId(),
	}
	for _, inst := range r.GetInstances() {
		out.Instances = append(out.Instances, sdk.FrigateInstanceProgress{
			InstanceID: inst.GetInstanceId(), Phase: agentPhaseFromWire(inst.GetPhase()), LastContact: timeFromWire(inst.GetLastContact()),
			RunningVersion: inst.GetRunningVersion(), MigrationExecuted: inst.GetMigrationExecuted(),
			ErrorCode: inst.GetErrorCode(), ErrorMessage: inst.GetErrorMessage(), ManifestSHA256: inst.GetManifestSha256(), ContactStale: inst.GetContactStale(),
		})
	}
	return out
}
