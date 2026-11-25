package sdk

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// CapabilityFrigateServerUpdate denotes support for staging a signed Frigate
// package on each instance of an HA pair and driving one self-coordinated
// rollout across them. Each operation additionally depends on the live
// controller (see FrigateInstanceSupport).
const CapabilityFrigateServerUpdate = "frigate_server_update"

// FrigateRefusalDomain is the google.rpc.ErrorInfo domain Frigate uses for
// machine-readable refusal reasons.
const FrigateRefusalDomain = "mdk.v2.frigate"

// FrigateStageChunkBytes is the largest Append chunk Frigate accepts (1 MiB).
const FrigateStageChunkBytes = 1 << 20

// Bounds that keep plugin messages and gRPC metadata small.
const (
	maxFrigateIDLength   = 256
	maxFrigateActorBytes = 128

	maxFrigateInstances            = 8
	maxFrigatePackageBytes         = 1 << 30
	maxFrigateStatementBytes       = 4 << 10
	maxFrigateSignatureBytes       = 512
	maxFrigateIdempotencyKeyLength = 128
	maxFrigateVersionLength        = 128
)

// FrigateInstallationType mirrors mdk.v2.ServerInstallationType.
type FrigateInstallationType string

const (
	FrigateInstallationUnspecified   FrigateInstallationType = ""
	FrigateInstallationDebianPackage FrigateInstallationType = "debian_package"
	FrigateInstallationDirectBinary  FrigateInstallationType = "direct_binary"
	// FrigateInstallationUnknown is any installation type this SDK does not know.
	FrigateInstallationUnknown FrigateInstallationType = "unknown"
)

// FrigateCoordination mirrors mdk.v2.ServerUpdateCoordination. Fleet always
// uses FrigateCoordinationSelf.
type FrigateCoordination string

const (
	// FrigateCoordinationLegacy is the Flotilla-coordinated path.
	FrigateCoordinationLegacy FrigateCoordination = ""
	// FrigateCoordinationSelf has the root helpers coordinate through etcd.
	FrigateCoordinationSelf FrigateCoordination = "self"
)

// FrigatePackageKind selects the Debian package that a manifest installs.
type FrigatePackageKind string

const (
	// FrigatePackageFrigate is the controller software.
	FrigatePackageFrigate FrigatePackageKind = ""
	// FrigatePackageProtoOS is the ProtoOS browser UI. Each controller installs
	// it alone as soon as its copy is committed, so it needs no Start.
	FrigatePackageProtoOS FrigatePackageKind = "proto_os"
)

// FrigateProtoOSBundleSource mirrors mdk.v2.ProtoOSBundleSource.
type FrigateProtoOSBundleSource string

const (
	FrigateProtoOSBundleUnspecified FrigateProtoOSBundleSource = ""
	FrigateProtoOSBundleNone        FrigateProtoOSBundleSource = "none"
	FrigateProtoOSBundlePackage     FrigateProtoOSBundleSource = "package"
	FrigateProtoOSBundleBundled     FrigateProtoOSBundleSource = "bundled"
	// FrigateProtoOSBundleUnknown is any source this SDK does not know.
	FrigateProtoOSBundleUnknown FrigateProtoOSBundleSource = "unknown"
)

// FrigateProtoOSJobState mirrors mdk.v2.ProtoOSPackageJobState.
type FrigateProtoOSJobState string

const (
	FrigateProtoOSJobUnspecified FrigateProtoOSJobState = ""
	FrigateProtoOSJobQueued      FrigateProtoOSJobState = "queued"
	FrigateProtoOSJobInstalling  FrigateProtoOSJobState = "installing"
	FrigateProtoOSJobSucceeded   FrigateProtoOSJobState = "succeeded"
	FrigateProtoOSJobFailed      FrigateProtoOSJobState = "failed"
	// FrigateProtoOSJobUnknown is any state this SDK does not know. Treat it as
	// not final and show the job message.
	FrigateProtoOSJobUnknown FrigateProtoOSJobState = "unknown"
)

// FrigateProtoOSBundle mirrors mdk.v2.ProtoOSBundleStatus: the ProtoOS bundle
// that a controller serves now.
type FrigateProtoOSBundle struct {
	Source     FrigateProtoOSBundleSource
	ReleaseTag string
}

// FrigateProtoOSJob mirrors mdk.v2.ProtoOSPackageJob: the most recent ProtoOS
// package job on one controller.
type FrigateProtoOSJob struct {
	JobID         string
	ReleaseTag    string
	TargetVersion string
	State         FrigateProtoOSJobState
	Message       string
	UpdatedAt     time.Time
}

// FrigateProtoOSSupport mirrors the proto_os_* fields of
// mdk.v2.GetServerUpdateSupportResponse for one instance. A caller may stage
// a ProtoOS manifest only when SelfSupported and the instance's
// RemoteTriggerAllowed are both true, and only for a release statement whose
// repository is in ReleaseRepositories.
type FrigateProtoOSSupport struct {
	SelfSupported       bool
	SupportMessage      string
	InstalledVersion    string
	ReleaseRepositories []string
	Served              FrigateProtoOSBundle
	// Job is the zero value when the controller reports no job.
	Job FrigateProtoOSJob
}

// FrigateRolloutPhase mirrors mdk.v2.ServerUpdateRolloutPhase.
type FrigateRolloutPhase string

const (
	FrigatePhaseUnspecified FrigateRolloutPhase = ""
	FrigatePhaseVerifying   FrigateRolloutPhase = "verifying"
	FrigatePhaseStopping    FrigateRolloutPhase = "stopping"
	FrigatePhaseInstalling  FrigateRolloutPhase = "installing"
	FrigatePhaseMigrating   FrigateRolloutPhase = "migrating"
	FrigatePhaseStarting    FrigateRolloutPhase = "starting"
	FrigatePhaseSucceeded   FrigateRolloutPhase = "succeeded"
	FrigatePhaseFailed      FrigateRolloutPhase = "failed"
	FrigatePhaseCancelled   FrigateRolloutPhase = "cancelled"
	FrigatePhaseVerified    FrigateRolloutPhase = "verified"
	FrigatePhaseAbandoned   FrigateRolloutPhase = "abandoned"
	// FrigatePhaseUnknown is any phase this SDK does not know. Treat it as
	// non-terminal and display the rollout error text.
	FrigatePhaseUnknown FrigateRolloutPhase = "unknown"
)

// Terminal reports whether the phase ends the rollout. FAILED is terminal for
// the driver but retryable; see FrigateRollout.Retryable.
func (p FrigateRolloutPhase) Terminal() bool {
	switch p {
	case FrigatePhaseSucceeded, FrigatePhaseFailed, FrigatePhaseCancelled, FrigatePhaseVerified, FrigatePhaseAbandoned:
		return true
	case FrigatePhaseUnspecified, FrigatePhaseVerifying, FrigatePhaseStopping, FrigatePhaseInstalling,
		FrigatePhaseMigrating, FrigatePhaseStarting, FrigatePhaseUnknown:
		return false
	default:
		return false
	}
}

// FrigateAgentPhase mirrors mdk.v2.FrigateUpdateAgentPhase for one instance.
type FrigateAgentPhase string

const (
	FrigateAgentUnspecified FrigateAgentPhase = ""
	FrigateAgentConnecting  FrigateAgentPhase = "connecting"
	FrigateAgentStaged      FrigateAgentPhase = "staged"
	FrigateAgentStopped     FrigateAgentPhase = "stopped"
	FrigateAgentInstalled   FrigateAgentPhase = "installed"
	FrigateAgentMigrated    FrigateAgentPhase = "migrated"
	FrigateAgentStarted     FrigateAgentPhase = "started"
	FrigateAgentFailed      FrigateAgentPhase = "failed"
	FrigateAgentUnknown     FrigateAgentPhase = "unknown"
)

// FrigateInstanceSupport mirrors mdk.v2.GetServerUpdateSupportResponse for one
// HA instance. When the instance could not be queried, Err is set, InstanceID
// still identifies it, and the remaining fields are empty. ErrReason is the
// UPDATE_* reason when the failure was a Frigate refusal rather than a
// transport failure.
type FrigateInstanceSupport struct {
	InstanceID                string
	PeerInstanceID            string
	RunningVersion            string
	Architecture              string
	InstallationType          FrigateInstallationType
	HelperAvailable           bool
	SupportMessage            string
	ClusterID                 string // etcd cluster holding rollout state; not GetHAStatus's HA cluster UUID
	ActiveJobID               string
	SpoolFreeBytes            int64
	EtcdMembersTotal          uint32
	EtcdMembersHealthy        uint32
	TrustedReleaseKeyIDs      []string
	SelfCoordinationSupported bool
	RemoteTriggerAllowed      bool
	ProtoOS                   FrigateProtoOSSupport
	Err                       string
	ErrReason                 string
}

// FrigateManifest mirrors the mdk.v2.ServerUpdateManifest fields used by a
// self-coordinated update. Frigate's Flotilla connection fields and the
// maintenance token are intentionally absent: Fleet never sends them.
type FrigateManifest struct {
	JobID               string
	SiteID              string
	ContainerID         string
	InstanceID          string
	ExpectedInstanceIDs []string
	ReleaseTag          string
	TargetVersion       string
	AssetID             int64
	AssetName           string
	SHA256              string
	SizeBytes           int64
	// Actor is the Fleet user. The plugin writes it to the manifest in the same
	// sanitized, bounded "proto-fleet:<user>" form it sends as the
	// x-frigate-actor header, so the manifest and the header agree.
	Actor            string
	Coordination     FrigateCoordination
	Package          FrigatePackageKind
	ReleaseStatement []byte
	ReleaseSignature []byte
}

// FrigateStageProgress is the acknowledged staging position on one instance.
type FrigateStageProgress struct {
	JobID          string
	ManifestSHA256 string
	NextOffset     int64
	SizeBytes      int64
}

// FrigateStageAppend writes Chunk at exactly Offset, which must equal the
// instance's acknowledged NextOffset.
type FrigateStageAppend struct {
	JobID          string
	ManifestSHA256 string
	Offset         int64
	Chunk          []byte
	// Actor is the Fleet user for audit attribution.
	Actor string
}

// FrigateStageRef identifies one staged package on an instance.
type FrigateStageRef struct {
	JobID          string
	ManifestSHA256 string
	// Actor is the Fleet user for audit attribution.
	Actor string
}

// FrigateStageCommit is the result of finishing a staged package.
type FrigateStageCommit struct {
	JobID       string
	BytesStaged int64
	SHA256      string
}

// FrigateStageCopy is one copy that the controller began, for CopyFrigateStage.
type FrigateStageCopy struct {
	InstanceID string
	Ref        FrigateStageRef
	Package    io.ReaderAt
	SizeBytes  int64
	// Offset is the offset that the controller acknowledged.
	Offset       int64
	ChunkTimeout time.Duration
	CallTimeout  time.Duration
	// OnChunk, if set, gets the acknowledged offset after each chunk. An error
	// from it stops the copy and CopyFrigateStage returns it unchanged.
	OnChunk func(next int64) error
	// BeforeCommit, if set, runs once every byte is appended, before the
	// commit. An error from it stops the copy and CopyFrigateStage returns it
	// unchanged.
	BeforeCommit func() error
}

// CopyFrigateStage appends the package from c.Offset to its end, one chunk at a
// time, and commits it. With an error, it also returns the last offset that
// the controller acknowledged. Errors from u come back unchanged, so a refusal
// stays a *FrigateRefusal.
func CopyFrigateStage(ctx context.Context, u DeviceFrigateUpdater, c FrigateStageCopy) (FrigateStageCommit, int64, error) {
	next := c.Offset
	buf := make([]byte, 0, FrigateStageChunkBytes)
	for next < c.SizeBytes {
		n := min(int64(FrigateStageChunkBytes), c.SizeBytes-next)
		buf = buf[:n]
		// ReadAt may report io.EOF with a full read at the end of the package.
		if read, err := c.Package.ReadAt(buf, next); int64(read) != n {
			return FrigateStageCommit{}, next, fmt.Errorf("read package: %w", err)
		}
		chunkCtx, cancel := context.WithTimeout(ctx, c.ChunkTimeout)
		ack, err := u.FrigateStageAppend(chunkCtx, c.InstanceID, FrigateStageAppend{
			JobID: c.Ref.JobID, ManifestSHA256: c.Ref.ManifestSHA256, Offset: next, Chunk: buf, Actor: c.Ref.Actor,
		})
		cancel()
		if err != nil {
			return FrigateStageCommit{}, next, err
		}
		if ack.NextOffset != next+n {
			return FrigateStageCommit{}, next, fmt.Errorf("controller acknowledged offset %d, expected %d", ack.NextOffset, next+n)
		}
		next = ack.NextOffset
		if c.OnChunk != nil {
			if err := c.OnChunk(next); err != nil {
				return FrigateStageCommit{}, next, err
			}
		}
	}
	if c.BeforeCommit != nil {
		if err := c.BeforeCommit(); err != nil {
			return FrigateStageCommit{}, next, err
		}
	}
	callCtx, cancel := context.WithTimeout(ctx, c.CallTimeout)
	defer cancel()
	commit, err := u.FrigateStageCommit(callCtx, c.InstanceID, c.Ref)
	return commit, next, err
}

// FrigateStartRequest mirrors mdk.v2.StartServerUpdateRequest.
type FrigateStartRequest struct {
	JobID               string
	TargetVersion       string
	ExpectedInstanceIDs []string
	StageOnly           bool
	IdempotencyKey      string
	// ExpectedManifestSHA256 maps an instance ID to the manifest fingerprint
	// returned when that instance was staged.
	ExpectedManifestSHA256 map[string]string
	// Actor is the Fleet user for audit attribution.
	Actor string
}

// FrigateAction addresses an existing rollout for retry or cancel.
type FrigateAction struct {
	JobID          string
	IdempotencyKey string
	// Actor is the Fleet user for audit attribution.
	Actor string
}

// FrigateInstanceProgress mirrors mdk.v2.ServerUpdateInstanceProgress.
type FrigateInstanceProgress struct {
	InstanceID        string
	Phase             FrigateAgentPhase
	LastContact       time.Time
	RunningVersion    string
	MigrationExecuted bool
	ErrorCode         string
	ErrorMessage      string
	ManifestSHA256    string
	ContactStale      bool
}

// FrigateRollout mirrors mdk.v2.ServerUpdateRollout. ErrorCode is an open set;
// treat any unrecognized value as a generic failure and show ErrorMessage.
type FrigateRollout struct {
	JobID                       string
	TargetVersion               string
	Phase                       FrigateRolloutPhase
	Instances                   []FrigateInstanceProgress
	DriverInstanceID            string
	MigrationExecutorInstanceID string
	ErrorCode                   string
	ErrorMessage                string
	NextStep                    string
	StageOnly                   bool
	DisruptionStarted           bool
	Retryable                   bool
	Cancellable                 bool
	AutomaticRetries            uint32
	Actor                       string
	CreatedAt                   time.Time
	UpdatedAt                   time.Time
	StoppedAt                   time.Time
	ExpiresAt                   time.Time
	FromVersion                 string
	PackageSHA256               string
	ReleaseKeyID                string
}

// FrigateStatus is a rollout read. ServedByMaintenance is true when Frigate's
// maintenance service answered because the regular daemon is stopped.
// AnsweredBy is the Frigate instance ID of the endpoint that answered.
type FrigateStatus struct {
	Rollout             FrigateRollout
	ServedByMaintenance bool
	AnsweredBy          string
}

// DeviceFrigateUpdater is optional. Staging and support are instance-scoped;
// instanceID is the Frigate instance ID of one of the device's endpoints (a
// single-node CT1 uses its server ID). Start, status, retry, and cancel address
// the HA pair and work while Frigate is stopped for an update. Refusals are
// returned as *FrigateRefusal, including refusals that carry gRPC code
// Unavailable; they are final and never retried on another controller.
//
// Start, retry, and cancel may be sent to the second controller after the first
// fails. That is safe only because Frigate replays the stored result for a
// repeated idempotency key, so every such request must carry one (see
// Validate). A Start that returns a deadline or transport error on a connected
// controller is ambiguous: Frigate may have accepted it. The caller must
// reconcile through FrigateUpdateStatus (and the same idempotency key) rather
// than assume it failed.
//
// A single-node CT1 device is built by verifying the controller's identity, so
// it cannot be created while Frigate is stopped. Operations on an
// already-built device keep working; a device created during the outage fails
// to build instead.
type DeviceFrigateUpdater interface {
	FrigateUpdateSupport(ctx context.Context) ([]FrigateInstanceSupport, error)
	FrigateStageBegin(ctx context.Context, instanceID string, m FrigateManifest) (FrigateStageProgress, error)
	FrigateStageAppend(ctx context.Context, instanceID string, a FrigateStageAppend) (FrigateStageProgress, error)
	FrigateStageCommit(ctx context.Context, instanceID string, r FrigateStageRef) (FrigateStageCommit, error)
	FrigateStageAbort(ctx context.Context, instanceID string, r FrigateStageRef) error
	FrigateUpdateStart(ctx context.Context, r FrigateStartRequest) (FrigateRollout, error)
	FrigateUpdateStatus(ctx context.Context, jobID string) (FrigateStatus, error)
	FrigateUpdateRetry(ctx context.Context, a FrigateAction) (FrigateRollout, error)
	FrigateUpdateCancel(ctx context.Context, a FrigateAction) (FrigateRollout, error)
}

// FrigateRefusal is a Frigate-side refusal. Reason is the UPDATE_* code from
// the google.rpc.ErrorInfo in domain FrigateRefusalDomain, Message is the
// human-readable text, and Code is the gRPC code Frigate used. It round-trips
// through the plugin boundary as a gRPC status carrying that ErrorInfo.
type FrigateRefusal struct {
	Reason  string
	Message string
	Code    codes.Code
}

func (e *FrigateRefusal) Error() string {
	if e.Message == "" {
		return "frigate refused the request: " + e.Reason
	}
	return fmt.Sprintf("frigate refused the request (%s): %s", e.Reason, e.Message)
}

// GRPCStatus lets a refusal cross a gRPC boundary without losing its reason.
func (e *FrigateRefusal) GRPCStatus() *status.Status {
	code := e.Code
	if code == codes.OK {
		code = codes.FailedPrecondition
	}
	base := status.New(code, e.Message)
	withInfo, err := base.WithDetails(&errdetails.ErrorInfo{Domain: FrigateRefusalDomain, Reason: e.Reason})
	if err != nil {
		return base
	}
	return withInfo
}

// AsFrigateRefusal extracts a refusal from err: either a *FrigateRefusal
// anywhere in the chain or a gRPC status carrying an ErrorInfo in domain
// FrigateRefusalDomain. It returns nil otherwise.
func AsFrigateRefusal(err error) *FrigateRefusal {
	if err == nil {
		return nil
	}
	var refusal *FrigateRefusal
	if errors.As(err, &refusal) {
		return refusal
	}
	st, ok := status.FromError(err)
	if !ok {
		return nil
	}
	for _, detail := range st.Details() {
		info, ok := detail.(*errdetails.ErrorInfo)
		if ok && info.Domain == FrigateRefusalDomain && info.Reason != "" {
			return &FrigateRefusal{Reason: info.Reason, Message: st.Message(), Code: st.Code()}
		}
	}
	return nil
}

// frigateRefusalFromWire converts a status error received over the plugin
// boundary, preserving any refusal reason.
func frigateRefusalFromWire(err error) error {
	if err == nil {
		return nil
	}
	if refusal := AsFrigateRefusal(err); refusal != nil {
		return refusal
	}
	return err
}

func validateFrigateInstanceID(id string) error {
	if strings.TrimSpace(id) == "" {
		return errors.New("frigate instance ID is required")
	}
	if len(id) > maxFrigateIDLength {
		return errors.New("frigate instance ID is too long")
	}
	return nil
}

func isLowerHex(s string, length int) bool {
	if len(s) != length {
		return false
	}
	for _, r := range s {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}

// validateFrigateJobID requires the canonical lowercase UUID form Fleet uses
// for every job.
func validateFrigateJobID(id string) error {
	if id == "" {
		return errors.New("frigate job ID is required")
	}
	if len(id) != 36 || id[8] != '-' || id[13] != '-' || id[18] != '-' || id[23] != '-' ||
		!isLowerHex(id[:8]+id[9:13]+id[14:18]+id[19:23]+id[24:], 32) {
		return errors.New("frigate job ID must be a canonical lowercase UUID")
	}
	return nil
}

func validateFrigateSHA256(name, value string) error {
	if !isLowerHex(value, 64) {
		return fmt.Errorf("frigate %s must be 64 lowercase hex characters", name)
	}
	return nil
}

func validateFrigateInstanceIDs(ids []string) error {
	if len(ids) == 0 || len(ids) > maxFrigateInstances {
		return fmt.Errorf("frigate expected instance IDs must number between 1 and %d", maxFrigateInstances)
	}
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		if err := validateFrigateInstanceID(id); err != nil {
			return err
		}
		if _, dup := seen[id]; dup {
			return fmt.Errorf("frigate expected instance ID %q is repeated", id)
		}
		seen[id] = struct{}{}
	}
	return nil
}

// validateFrigateIdempotencyKey requires a non-empty key of printable ASCII.
// Safe fall-through to the second controller depends on it.
func validateFrigateIdempotencyKey(key string) error {
	if key == "" {
		return errors.New("frigate idempotency key is required")
	}
	if len(key) > maxFrigateIdempotencyKeyLength {
		return fmt.Errorf("frigate idempotency key must be at most %d characters", maxFrigateIdempotencyKeyLength)
	}
	for _, r := range key {
		if r < 0x20 || r > 0x7e {
			return errors.New("frigate idempotency key must be printable ASCII")
		}
	}
	return nil
}

// Validate checks the manifest fields every self-coordinated update needs.
// Frigate performs the authoritative validation.
func (m FrigateManifest) Validate() error {
	if err := validateFrigateJobID(m.JobID); err != nil {
		return err
	}
	if err := validateFrigateInstanceID(m.InstanceID); err != nil {
		return err
	}
	if m.Coordination != FrigateCoordinationSelf {
		return errors.New("frigate manifest must use self coordination")
	}
	if err := validateFrigateInstanceIDs(m.ExpectedInstanceIDs); err != nil {
		return err
	}
	if !slices.Contains(m.ExpectedInstanceIDs, m.InstanceID) {
		return errors.New("frigate manifest expected instance IDs must include the target instance")
	}
	if err := validateFrigateSHA256("manifest sha256", m.SHA256); err != nil {
		return err
	}
	if m.SizeBytes <= 0 || m.SizeBytes > maxFrigatePackageBytes {
		return fmt.Errorf("frigate manifest size must be between 1 and %d bytes", maxFrigatePackageBytes)
	}
	if m.TargetVersion == "" || len(m.TargetVersion) > maxFrigateVersionLength {
		return errors.New("frigate manifest target version is required and bounded")
	}
	if len(m.ReleaseStatement) == 0 || len(m.ReleaseStatement) > maxFrigateStatementBytes {
		return fmt.Errorf("frigate release statement must be between 1 and %d bytes", maxFrigateStatementBytes)
	}
	if len(m.ReleaseSignature) == 0 || len(m.ReleaseSignature) > maxFrigateSignatureBytes {
		return fmt.Errorf("frigate release signature must be between 1 and %d bytes", maxFrigateSignatureBytes)
	}
	switch m.Package {
	case FrigatePackageFrigate:
		return nil
	case FrigatePackageProtoOS:
		return validateProtoOSRelease(m)
	default:
		return fmt.Errorf("frigate manifest package %q is unknown", m.Package)
	}
}

// ProtoOSDebianVersion returns the Debian version of the proto-os package of a
// ProtoOS release tag: the tag without its "v" and with the first "-" replaced
// by "~", so that a prerelease sorts before its final release. Frigate refuses
// a ProtoOS manifest whose target version is not this value.
func ProtoOSDebianVersion(releaseTag string) string {
	return strings.Replace(strings.TrimPrefix(releaseTag, "v"), "-", "~", 1)
}

// ProtoOSAssetName returns the only asset name that Frigate accepts for the
// proto-os package of a ProtoOS release tag.
func ProtoOSAssetName(releaseTag string) string {
	return "proto-os_" + releaseTag + "_all.deb"
}

// ProtoOSReleaseTag is the inverse of ProtoOSDebianVersion. It returns false
// for a version that no v-prefixed release tag maps to, for example "1.0.0-1",
// whose tag would map back to "1.0.0~1".
func ProtoOSReleaseTag(debianVersion string) (string, bool) {
	tag := "v" + strings.Replace(debianVersion, "~", "-", 1)
	if !protoOSReleaseTagShape(tag) || ProtoOSDebianVersion(tag) != debianVersion {
		return "", false
	}
	return tag, true
}

func protoOSReleaseTagShape(tag string) bool {
	if len(tag) < 2 || len(tag) > maxFrigateVersionLength || tag[0] != 'v' || tag[1] < '0' || tag[1] > '9' {
		return false
	}
	for _, c := range tag[1:] {
		if (c < '0' || c > '9') && (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && c != '.' && c != '-' {
			return false
		}
	}
	return true
}

// validateProtoOSRelease checks that the release tag, asset name and target
// version agree. Frigate also checks that the tag is a semantic version.
func validateProtoOSRelease(m FrigateManifest) error {
	tag := m.ReleaseTag
	if !protoOSReleaseTagShape(tag) {
		return errors.New("frigate proto-os manifest needs a v-prefixed release tag")
	}
	if m.AssetName != ProtoOSAssetName(tag) {
		return fmt.Errorf("frigate proto-os manifest asset name must be %q", ProtoOSAssetName(tag))
	}
	if m.TargetVersion != ProtoOSDebianVersion(tag) {
		return fmt.Errorf("frigate proto-os manifest target version must be %q", ProtoOSDebianVersion(tag))
	}
	return nil
}

// ValidateForInstance additionally requires the manifest to address the
// instance the call is sent to.
func (m FrigateManifest) ValidateForInstance(instanceID string) error {
	if err := m.Validate(); err != nil {
		return err
	}
	if m.InstanceID != instanceID {
		return fmt.Errorf("frigate manifest instance %q does not match target instance %q", m.InstanceID, instanceID)
	}
	return nil
}

// Validate checks an append request's shape and chunk bound.
func (a FrigateStageAppend) Validate() error {
	if err := validateFrigateJobID(a.JobID); err != nil {
		return err
	}
	if err := validateFrigateSHA256("manifest fingerprint", a.ManifestSHA256); err != nil {
		return err
	}
	if a.Offset < 0 {
		return errors.New("frigate append offset must not be negative")
	}
	if len(a.Chunk) == 0 || len(a.Chunk) > FrigateStageChunkBytes {
		return fmt.Errorf("frigate append chunk must be between 1 and %d bytes", FrigateStageChunkBytes)
	}
	return nil
}

// Validate checks the reference.
func (r FrigateStageRef) Validate() error {
	if err := validateFrigateJobID(r.JobID); err != nil {
		return err
	}
	return validateFrigateSHA256("manifest fingerprint", r.ManifestSHA256)
}

// Validate checks the start request. The idempotency key is mandatory: a start
// may be sent to the second controller after the first fails, and Frigate
// replays the stored result only for a repeated key.
func (r FrigateStartRequest) Validate() error {
	if err := validateFrigateJobID(r.JobID); err != nil {
		return err
	}
	if r.TargetVersion == "" || len(r.TargetVersion) > maxFrigateVersionLength {
		return errors.New("frigate start requires a bounded target version")
	}
	if err := validateFrigateInstanceIDs(r.ExpectedInstanceIDs); err != nil {
		return err
	}
	if err := validateFrigateIdempotencyKey(r.IdempotencyKey); err != nil {
		return err
	}
	for id, fingerprint := range r.ExpectedManifestSHA256 {
		if !slices.Contains(r.ExpectedInstanceIDs, id) {
			return fmt.Errorf("frigate manifest fingerprint is given for unexpected instance %q", id)
		}
		if err := validateFrigateSHA256("manifest fingerprint", fingerprint); err != nil {
			return err
		}
	}
	return nil
}

// Validate checks the action. The idempotency key is mandatory for the same
// reason as for a start.
func (a FrigateAction) Validate() error {
	if err := validateFrigateJobID(a.JobID); err != nil {
		return err
	}
	return validateFrigateIdempotencyKey(a.IdempotencyKey)
}

// validateFrigateStatusJobID allows the empty ID, which selects the active
// (else latest) rollout.
func validateFrigateStatusJobID(id string) error {
	if id == "" {
		return nil
	}
	return validateFrigateJobID(id)
}

// FrigateAuditActor formats the x-frigate-actor audit value as
// "proto-fleet:<user>". It strips a leading "fleet:" so a manifest actor and a
// bare user produce the same value, and reduces the value to bounded printable
// ASCII because it travels as gRPC metadata. An empty user yields "proto-fleet".
func FrigateAuditActor(user string) string {
	user = strings.TrimPrefix(strings.TrimSpace(user), "fleet:")
	var b strings.Builder
	for _, r := range user {
		if b.Len() >= maxFrigateActorBytes {
			break
		}
		if r < 0x20 || r > 0x7e {
			r = '?'
		}
		b.WriteRune(r)
	}
	if b.Len() == 0 {
		return "proto-fleet"
	}
	return "proto-fleet:" + b.String()
}
