package main

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	mdk "github.com/block/proto-fleet/sdk/mdk/generated/mdk/v2"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// This file simulates what a real CT1 HA pair's etcd store and root update
// helpers provide: a site-wide self-coordinated rollout that outlives either
// Frigate daemon. Both fake instances share one updateSim in process.

const (
	frigateRefusalDomain = "mdk.v2.frigate"
	// maxStageChunkBytes is Frigate's per-Append limit.
	maxStageChunkBytes = 1 << 20
	// maxFakePackageBytes bounds the in-memory packages the fake will stage.
	maxFakePackageBytes = 256 << 20
	// releaseStatementSchema is the only statement schema the fake accepts.
	releaseStatementSchema = "frigate-release-statement/v1"
	maxStatementBytes      = 4096
	maxSignatureBytes      = 128
	staleContactAge        = 45 * time.Second
	defaultUpdateStep      = 2 * time.Second
	defaultSpoolFreeBytes  = int64(8) << 30
	rolloutExpiry          = 10 * time.Minute
)

// updateFaults are the fault-injection knobs. Each maps to a documented refusal
// or runtime behavior of the real service.
type updateFaults struct {
	// RemoteTriggerDisabled models update.allow_remote_trigger being off.
	RemoteTriggerDisabled bool
	// SelfCoordinationUnsupported models a Frigate without the feature.
	SelfCoordinationUnsupported bool
	// EtcdUnhealthy models a cluster without etcd quorum.
	EtcdUnhealthy bool
	// HelperUnavailable models a missing root helper.
	HelperUnavailable bool
	// OutsideWindow models a closed maintenance window.
	OutsideWindow bool
	// Cooldown refuses a Start within this long after the last success.
	Cooldown time.Duration
	// MigrateFailures makes the next N migrate steps fail (retryable).
	MigrateFailures int
	// StaleContact makes the non-driver helper report a stale last_contact.
	StaleContact bool
	// DriverReboot moves the driver role to the peer once, mid-rollout.
	DriverReboot bool
	// FirmwareActive reports a hashboard or PSU flash running on a node even
	// when no simulated hashboard job is, so Start returns UPDATE_FIRMWARE_ACTIVE.
	FirmwareActive bool
	// ProtoOSSelfUnsupported models a Frigate that does not accept a
	// self-coordinated proto-os manifest.
	ProtoOSSelfUnsupported bool
	// ProtoOSInstallFails makes every proto-os install fail after verification.
	ProtoOSInstallFails bool
}

type updateSimConfig struct {
	// Primary is the initial HA primary's server ID.
	Primary        string
	EtcdClusterID  string // GetServerUpdateSupport.cluster_id: the etcd cluster, not the HA cluster UUID
	StepDelay      time.Duration
	SpoolFreeBytes int64
	TrustedKeys    []*ecdsa.PublicKey
	// ProtoOSTrustedKeys sign proto-os releases. They are a separate trust
	// domain: a Frigate release key never verifies a proto-os package, and the
	// reverse.
	ProtoOSTrustedKeys []*ecdsa.PublicKey
	Architecture       string
	Faults             updateFaults
	Now                func() time.Time
}

type stagedPackage struct {
	manifest    *mdk.ServerUpdateManifest
	fingerprint string
	data        []byte
	committed   bool
	sha256      string
}

type updateInstance struct {
	id             string
	state          *ContainerState
	runningVersion string
	stages         map[string]*stagedPackage
	protoOS        protoOSState
}

type helperState struct {
	phase             mdk.FrigateUpdateAgentPhase
	errorCode         string
	errorMessage      string
	migrationExecuted bool
}

type rollout struct {
	msg         *mdk.ServerUpdateRollout
	helpers     map[string]*helperState
	fingerprint map[string]string
	verified    bool
	rebooted    bool
	timer       *time.Timer
	// request is the Start request's identity, for idempotency checks.
	request string
}

type idempotentRecord struct {
	request string
	jobID   string
}

// updateSim is the shared rollout simulator for one HA pair.
type updateSim struct {
	// mu is never held while taking a ContainerState lock: request handlers
	// hold their state lock while reading the HA role through the sim.
	mu             sync.Mutex
	cfg            updateSimConfig
	instances      map[string]*updateInstance
	trusted        map[string]*ecdsa.PublicKey
	protoOSTrusted map[string]*ecdsa.PublicKey
	rollouts       map[string]*rollout
	order          []string // job IDs, oldest first
	activeID       string
	idempotency    map[string]idempotentRecord
	primary        string
	maintenance    bool
	lastSuccess    time.Time
	closed         bool
}

func newUpdateSim(cfg updateSimConfig) *updateSim {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.StepDelay <= 0 {
		cfg.StepDelay = defaultUpdateStep
	}
	if cfg.SpoolFreeBytes == 0 {
		cfg.SpoolFreeBytes = defaultSpoolFreeBytes
	}
	if cfg.Architecture == "" {
		cfg.Architecture = "arm64"
	}
	return &updateSim{
		cfg: cfg, instances: map[string]*updateInstance{}, trusted: keyring(cfg.TrustedKeys), protoOSTrusted: keyring(cfg.ProtoOSTrustedKeys),
		rollouts: map[string]*rollout{}, idempotency: map[string]idempotentRecord{}, primary: cfg.Primary,
	}
}

func keyring(keys []*ecdsa.PublicKey) map[string]*ecdsa.PublicKey {
	ring := map[string]*ecdsa.PublicKey{}
	for _, key := range keys {
		if id, err := releaseKeyID(key); err == nil {
			ring[id] = key
		}
	}
	return ring
}

// attach registers an instance; the first two attached form the pair.
func (s *updateSim) attach(state *ContainerState) *updateInstance {
	s.mu.Lock()
	defer s.mu.Unlock()
	inst := &updateInstance{id: state.ServerID, state: state, runningVersion: state.Version, stages: map[string]*stagedPackage{}, protoOS: newProtoOSState()}
	s.instances[inst.id] = inst
	if s.primary == "" {
		s.primary = inst.id
	}
	return inst
}

func (s *updateSim) close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	for _, r := range s.rollouts {
		if r.timer != nil {
			r.timer.Stop()
		}
	}
}

func (s *updateSim) setFaults(mutate func(*updateFaults)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	mutate(&s.cfg.Faults)
}

func (s *updateSim) currentPrimary() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.primary
}

func (s *updateSim) inMaintenance() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.maintenance
}

func (s *updateSim) sortedIDsLocked() []string {
	ids := make([]string, 0, len(s.instances))
	for id := range s.instances {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	return ids
}

func (s *updateSim) peerOfLocked(id string) string {
	for other := range s.instances {
		if other != id {
			return other
		}
	}
	return ""
}

// refusal builds a gRPC status carrying a Frigate ErrorInfo reason.
func refusal(code codes.Code, reason, format string, args ...any) error {
	st := status.Newf(code, format, args...)
	if withInfo, err := st.WithDetails(&errdetails.ErrorInfo{Domain: frigateRefusalDomain, Reason: reason}); err == nil {
		st = withInfo
	}
	return st.Err()
}

func releaseKeyID(key *ecdsa.PublicKey) (string, error) {
	der, err := x509.MarshalPKIXPublicKey(key)
	if err != nil {
		return "", fmt.Errorf("marshal release key: %w", err)
	}
	sum := sha256.Sum256(der)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

// loadTrustedKeys reads PEM-encoded PKIX ECDSA P-256 public keys.
func loadTrustedKeys(paths []string) ([]*ecdsa.PublicKey, error) {
	var keys []*ecdsa.PublicKey
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read trusted release key: %w", err)
		}
		block, _ := pem.Decode(data)
		if block == nil {
			return nil, fmt.Errorf("trusted release key %s is not PEM", path)
		}
		parsed, err := x509.ParsePKIXPublicKey(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("parse trusted release key %s: %w", path, err)
		}
		key, ok := parsed.(*ecdsa.PublicKey)
		if !ok || key.Curve.Params().Name != "P-256" {
			return nil, fmt.Errorf("trusted release key %s is not ECDSA P-256", path)
		}
		keys = append(keys, key)
	}
	return keys, nil
}

type releaseStatement struct {
	Schema              string `json:"schema"`
	Package             string `json:"package"`
	Version             string `json:"version"`
	Architecture        string `json:"architecture"`
	Repository          string `json:"repository"`
	SHA256              string `json:"sha256"`
	Size                int64  `json:"size"`
	MinInstalledVersion string `json:"min_installed_version"`
	IssuedAt            string `json:"issued_at"`
}

// verifyRelease checks the manifest's signed statement against the trusted
// keys with real ECDSA verification, then binds it to the manifest. It returns
// the verifying key's ID.
func (s *updateSim) verifyReleaseLocked(m *mdk.ServerUpdateManifest) (string, error) {
	if len(s.trusted) == 0 {
		return "", refusal(codes.FailedPrecondition, "UPDATE_NO_TRUSTED_KEYS", "this instance trusts no release signing keys")
	}
	statement, signature := m.GetReleaseStatement(), m.GetReleaseSignature()
	if len(statement) == 0 || len(statement) > maxStatementBytes || len(signature) == 0 || len(signature) > maxSignatureBytes {
		return "", refusal(codes.FailedPrecondition, "UPDATE_SIGNATURE_INVALID", "release statement or signature is missing or oversized")
	}
	keyID := verifySignature(s.trusted, statement, signature)
	if keyID == "" {
		return "", refusal(codes.FailedPrecondition, "UPDATE_SIGNATURE_INVALID", "no trusted release key validates the signature")
	}
	decoded, ok := parseStatement(statement)
	if !ok {
		return "", refusal(codes.FailedPrecondition, "UPDATE_SIGNATURE_INVALID", "release statement is malformed or has an unsupported schema")
	}
	if decoded.Package != "frigate" || decoded.Architecture != s.cfg.Architecture || decoded.Version != m.GetTargetVersion() ||
		decoded.SHA256 != m.GetSha256() || decoded.Size != m.GetSizeBytes() {
		return "", refusal(codes.FailedPrecondition, "UPDATE_STAGE_MISMATCH", "release statement does not match the staged manifest")
	}
	return keyID, nil
}

// verifySignature returns the ID of the key in trusted that verifies the
// signature over statement, or "" when none does.
func verifySignature(trusted map[string]*ecdsa.PublicKey, statement, signature []byte) string {
	digest := sha256.Sum256(statement)
	for id, key := range trusted {
		if ecdsa.VerifyASN1(key, digest[:], signature) {
			return id
		}
	}
	return ""
}

func parseStatement(statement []byte) (releaseStatement, bool) {
	var decoded releaseStatement
	decoder := json.NewDecoder(bytes.NewReader(statement))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&decoded); err != nil || decoded.Schema != releaseStatementSchema {
		return releaseStatement{}, false
	}
	return decoded, true
}

func manifestFingerprint(m *mdk.ServerUpdateManifest) string {
	data, err := proto.MarshalOptions{Deterministic: true}.Marshal(m)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func isLowerHex64(v string) bool {
	if len(v) != 64 {
		return false
	}
	for _, c := range v {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

func invalidManifest(format string, args ...any) error {
	return refusal(codes.InvalidArgument, "UPDATE_MANIFEST_INVALID", format, args...)
}

func (s *updateSim) validateManifestLocked(m *mdk.ServerUpdateManifest) error {
	switch {
	case m == nil:
		return invalidManifest("manifest is required")
	case strings.TrimSpace(m.GetJobId()) == "" || strings.TrimSpace(m.GetInstanceId()) == "":
		return invalidManifest("manifest job_id and instance_id are required")
	case m.GetSizeBytes() <= 0 || m.GetSizeBytes() > maxFakePackageBytes:
		return invalidManifest("manifest size_bytes is out of range")
	case !isLowerHex64(m.GetSha256()):
		return invalidManifest("manifest sha256 must be 64 lowercase hex characters")
	case m.GetTargetVersion() == "":
		return invalidManifest("manifest target_version is required")
	case !slices.Contains(m.GetExpectedInstanceIds(), m.GetInstanceId()):
		return invalidManifest("manifest expected_instance_ids must include instance_id")
	}
	hasRelease := len(m.GetReleaseStatement()) > 0 || len(m.GetReleaseSignature()) > 0
	switch m.GetPackage() {
	case mdk.ServerUpdatePackage_SERVER_UPDATE_PACKAGE_UNSPECIFIED:
	case mdk.ServerUpdatePackage_SERVER_UPDATE_PACKAGE_PROTO_OS:
		return s.validateProtoOSManifestLocked(m)
	default:
		return invalidManifest("unknown manifest package")
	}
	switch m.GetCoordination() {
	case mdk.ServerUpdateCoordination_SERVER_UPDATE_COORDINATION_SELF:
		if s.cfg.Faults.SelfCoordinationUnsupported {
			return refusal(codes.FailedPrecondition, "UPDATE_SELF_COORDINATION_UNSUPPORTED", "this server does not support self-coordinated updates")
		}
		if len(m.GetReleaseStatement()) == 0 || len(m.GetReleaseSignature()) == 0 {
			return invalidManifest("a self-coordinated manifest requires a release statement and signature")
		}
		if hasFlotillaFields(m) {
			return invalidManifest("a self-coordinated manifest must not carry Flotilla connection fields")
		}
	case mdk.ServerUpdateCoordination_SERVER_UPDATE_COORDINATION_UNSPECIFIED:
		if hasRelease {
			return invalidManifest("a legacy manifest must not carry a release statement or signature")
		}
	default:
		return invalidManifest("unknown manifest coordination")
	}
	return nil
}

func hasFlotillaFields(m *mdk.ServerUpdateManifest) bool {
	return m.GetMaintenanceToken() != "" || m.GetFlotillaAddress() != "" || m.GetTlsServerName() != "" || m.GetRootCaFile() != "" ||
		m.GetClientCertificateFile() != "" || m.GetClientKeyFile() != "" || m.GetSiteToken() != ""
}

// ---------------------------------------------------------------------------
// Staging (instance-scoped).

func (s *updateSim) instanceLocked(id string) (*updateInstance, error) {
	inst, ok := s.instances[id]
	if !ok {
		return nil, invalidManifest("unknown instance %q", id)
	}
	return inst, nil
}

func stageProgress(p *stagedPackage) *mdk.BeginServerUpdateStageResponse {
	next := int64(len(p.data))
	if p.committed {
		// A committed proto-os copy drops its bytes once the job ends.
		next = p.manifest.GetSizeBytes()
	}
	return &mdk.BeginServerUpdateStageResponse{JobId: p.manifest.GetJobId(), ManifestSha256: p.fingerprint, NextOffset: next, SizeBytes: p.manifest.GetSizeBytes()}
}

// beginStage opens or resumes one exact manifest on the addressed instance.
// Like Frigate, it refuses a manifest whose instance_id is not the instance the
// call reached (the connected controller or the x-frigate-server-id header), so
// no byte is staged for the wrong host.
func (s *updateSim) beginStage(m *mdk.ServerUpdateManifest, addressed string) (*mdk.BeginServerUpdateStageResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.validateManifestLocked(m); err != nil {
		return nil, err
	}
	if addressed != m.GetInstanceId() {
		return nil, invalidManifest("manifest instance_id %q does not match the addressed instance %q", m.GetInstanceId(), addressed)
	}
	inst, err := s.instanceLocked(m.GetInstanceId())
	if err != nil {
		return nil, err
	}
	fingerprint := manifestFingerprint(m)
	if existing, ok := inst.stages[m.GetJobId()]; ok {
		if existing.fingerprint != fingerprint {
			return nil, refusal(codes.FailedPrecondition, "UPDATE_STAGE_MISMATCH", "job %s is already staged with a different manifest", m.GetJobId())
		}
		return stageProgress(existing), nil
	}
	if err := s.checkNoActiveJobLocked(inst); err != nil {
		return nil, err
	}
	staged := &stagedPackage{manifest: proto.Clone(m).(*mdk.ServerUpdateManifest), fingerprint: fingerprint}
	inst.stages[m.GetJobId()] = staged
	return stageProgress(staged), nil
}

func (s *updateSim) stageLocked(inst *updateInstance, jobID, fingerprint string) (*stagedPackage, error) {
	staged, ok := inst.stages[jobID]
	if !ok {
		return nil, status.Errorf(codes.InvalidArgument, "server update job %q is not staged", jobID)
	}
	if staged.fingerprint != fingerprint {
		return nil, status.Errorf(codes.InvalidArgument, "server update manifest fingerprint does not match job %q", jobID)
	}
	return staged, nil
}

// appendStage writes one exact-offset chunk. A chunk wholly inside the bytes
// already staged and identical to them is acknowledged again, so a caller can
// repeat an Append whose reply was lost.
func (s *updateSim) appendStage(instanceID, jobID, fingerprint string, offset int64, chunk []byte) (*mdk.AppendServerUpdateStageResponse, error) {
	if len(chunk) == 0 || len(chunk) > maxStageChunkBytes {
		return nil, status.Error(codes.InvalidArgument, "server update chunks must contain between 1 byte and 1 MiB")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	inst, err := s.instanceLocked(instanceID)
	if err != nil {
		return nil, err
	}
	staged, err := s.stageLocked(inst, jobID, fingerprint)
	if err != nil {
		return nil, err
	}
	if staged.committed {
		return nil, status.Error(codes.InvalidArgument, "server update stage is already committed")
	}
	have := int64(len(staged.data))
	end := offset + int64(len(chunk))
	switch {
	case offset < 0:
		return nil, status.Errorf(codes.InvalidArgument, "server update chunk offset %d does not match next offset %d", offset, have)
	case offset < have:
		if end > have || !bytes.Equal(staged.data[offset:end], chunk) {
			return nil, status.Errorf(codes.InvalidArgument, "server update chunk offset %d does not match next offset %d", offset, have)
		}
	case offset > have:
		return nil, status.Errorf(codes.InvalidArgument, "server update chunk offset %d does not match next offset %d", offset, have)
	default:
		if end > staged.manifest.GetSizeBytes() {
			return nil, status.Error(codes.InvalidArgument, "server update chunk exceeds the declared package size")
		}
		staged.data = append(staged.data, chunk...)
	}
	p := stageProgress(staged)
	return &mdk.AppendServerUpdateStageResponse{JobId: p.JobId, ManifestSha256: p.ManifestSha256, NextOffset: p.NextOffset, SizeBytes: p.SizeBytes}, nil
}

func (s *updateSim) commitStage(instanceID, jobID, fingerprint string) (*mdk.CommitServerUpdateStageResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	inst, err := s.instanceLocked(instanceID)
	if err != nil {
		return nil, err
	}
	staged, err := s.stageLocked(inst, jobID, fingerprint)
	if err != nil {
		return nil, err
	}
	if staged.committed {
		return &mdk.CommitServerUpdateStageResponse{JobId: jobID, BytesStaged: staged.manifest.GetSizeBytes(), Sha256: staged.sha256}, nil
	}
	if int64(len(staged.data)) != staged.manifest.GetSizeBytes() {
		return nil, refusal(codes.InvalidArgument, "UPDATE_PACKAGE_REJECTED", "staged %d of %d bytes", len(staged.data), staged.manifest.GetSizeBytes())
	}
	sum := sha256.Sum256(staged.data)
	digest := hex.EncodeToString(sum[:])
	if digest != staged.manifest.GetSha256() {
		return nil, refusal(codes.InvalidArgument, "UPDATE_PACKAGE_REJECTED", "staged package digest does not match the manifest")
	}
	protoOS := staged.manifest.GetPackage() == mdk.ServerUpdatePackage_SERVER_UPDATE_PACKAGE_PROTO_OS
	if protoOS {
		if err := s.checkNoActiveJobLocked(inst); err != nil {
			return nil, err
		}
	}
	staged.committed, staged.sha256 = true, digest
	if protoOS {
		s.queueProtoOSLocked(inst, staged)
	}
	return &mdk.CommitServerUpdateStageResponse{JobId: jobID, BytesStaged: int64(len(staged.data)), Sha256: digest}, nil
}

// abortStage mirrors Frigate: it discards a matching partial stage, and
// withdraws a committed one whose rollout never started or has finished. A
// committed stage under a running rollout is refused, and aborting a job that
// is not staged is a no-op so cleanup can be repeated.
func (s *updateSim) abortStage(instanceID, jobID, fingerprint string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	inst, err := s.instanceLocked(instanceID)
	if err != nil {
		return err
	}
	if _, ok := inst.stages[jobID]; !ok {
		return nil
	}
	staged, err := s.stageLocked(inst, jobID, fingerprint)
	if err != nil {
		return err
	}
	if r, ok := s.rollouts[jobID]; staged.committed && ok && !isTerminalPhase(r.msg.Phase) {
		return refusal(codes.FailedPrecondition, "UPDATE_ACTIVE_JOB", "server update %s has started; cancel it instead", jobID)
	}
	if job := inst.protoOS.activeJob(); job != nil && job.GetJobId() == jobID {
		return refusal(codes.FailedPrecondition, "UPDATE_ACTIVE_JOB", "proto-os package job %s is installing", jobID)
	}
	delete(inst.stages, jobID)
	return nil
}

// supportFor describes one instance.
func (s *updateSim) supportFor(instanceID string) (*mdk.GetServerUpdateSupportResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	inst, err := s.instanceLocked(instanceID)
	if err != nil {
		return nil, err
	}
	healthy := uint32(3)
	if s.cfg.Faults.EtcdUnhealthy {
		healthy = 1
	}
	keys := make([]string, 0, len(s.trusted))
	for id := range s.trusted {
		keys = append(keys, id)
	}
	slices.Sort(keys)
	return &mdk.GetServerUpdateSupportResponse{
		InstanceId:                 inst.id,
		PeerInstanceId:             s.peerOfLocked(inst.id),
		RunningVersion:             inst.runningVersion,
		Architecture:               s.cfg.Architecture,
		InstallationType:           mdk.ServerInstallationType_SERVER_INSTALLATION_TYPE_DEBIAN_PACKAGE,
		HelperAvailable:            !s.cfg.Faults.HelperUnavailable,
		ClusterId:                  s.cfg.EtcdClusterID,
		ActiveJobId:                s.activeID,
		SpoolFreeBytes:             s.cfg.SpoolFreeBytes,
		EtcdMembersTotal:           3,
		EtcdMembersHealthy:         healthy,
		TrustedReleaseKeyIds:       keys,
		SelfCoordinationSupported:  !s.cfg.Faults.SelfCoordinationUnsupported,
		RemoteTriggerAllowed:       !s.cfg.Faults.RemoteTriggerDisabled,
		ProtoOsSelfSupported:       !s.cfg.Faults.ProtoOSSelfUnsupported,
		ProtoOsSupportMessage:      s.protoOSSupportMessageLocked(),
		ProtoOsInstalledVersion:    inst.protoOS.installed,
		ProtoOsServed:              proto.Clone(inst.protoOS.served).(*mdk.ProtoOSBundleStatus),
		ProtoOsJob:                 cloneProtoOSJob(inst.protoOS.job),
		ProtoOsReleaseRepositories: slices.Clone(protoOSReleaseRepositories),
	}, nil
}

// checkNoActiveJobLocked refuses a new job on inst while a Frigate rollout runs
// on the pair or a proto-os job runs on inst: a controller holds one update
// job at a time.
func (s *updateSim) checkNoActiveJobLocked(inst *updateInstance) error {
	if s.activeID != "" {
		return refusal(codes.FailedPrecondition, "UPDATE_ACTIVE_JOB", "update %s is active", s.activeID)
	}
	if job := inst.protoOS.activeJob(); job != nil {
		return refusal(codes.FailedPrecondition, "UPDATE_ACTIVE_JOB", "proto-os package job %s is active on %s", job.GetJobId(), inst.id)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Rollout.

func isTerminalPhase(p mdk.ServerUpdateRolloutPhase) bool {
	switch p {
	case mdk.ServerUpdateRolloutPhase_SERVER_UPDATE_ROLLOUT_PHASE_SUCCEEDED,
		mdk.ServerUpdateRolloutPhase_SERVER_UPDATE_ROLLOUT_PHASE_FAILED,
		mdk.ServerUpdateRolloutPhase_SERVER_UPDATE_ROLLOUT_PHASE_CANCELLED,
		mdk.ServerUpdateRolloutPhase_SERVER_UPDATE_ROLLOUT_PHASE_VERIFIED,
		mdk.ServerUpdateRolloutPhase_SERVER_UPDATE_ROLLOUT_PHASE_ABANDONED:
		return true
	case mdk.ServerUpdateRolloutPhase_SERVER_UPDATE_ROLLOUT_PHASE_UNSPECIFIED,
		mdk.ServerUpdateRolloutPhase_SERVER_UPDATE_ROLLOUT_PHASE_VERIFYING,
		mdk.ServerUpdateRolloutPhase_SERVER_UPDATE_ROLLOUT_PHASE_STOPPING,
		mdk.ServerUpdateRolloutPhase_SERVER_UPDATE_ROLLOUT_PHASE_INSTALLING,
		mdk.ServerUpdateRolloutPhase_SERVER_UPDATE_ROLLOUT_PHASE_MIGRATING,
		mdk.ServerUpdateRolloutPhase_SERVER_UPDATE_ROLLOUT_PHASE_STARTING:
		return false
	}
	return false
}

func startRequestIdentity(in *mdk.StartServerUpdateRequest) string {
	ids := slices.Clone(in.GetExpectedInstanceIds())
	slices.Sort(ids)
	keys := make([]string, 0, len(in.GetExpectedManifestSha256()))
	for k := range in.GetExpectedManifestSha256() {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	var b strings.Builder
	fmt.Fprintf(&b, "%s|%s|%v|%s", in.GetJobId(), in.GetTargetVersion(), in.GetStageOnly(), strings.Join(ids, ","))
	for _, k := range keys {
		fmt.Fprintf(&b, "|%s=%s", k, in.GetExpectedManifestSha256()[k])
	}
	return b.String()
}

// activeFlashes lists the firmware jobs still running on either instance, which
// real Frigate tracks as leased etcd markers. State locks are taken only after
// the simulator lock is released (see updateSim.mu).
func (s *updateSim) activeFlashes() []string {
	s.mu.Lock()
	states := make([]*ContainerState, 0, len(s.instances))
	for _, id := range s.sortedIDsLocked() {
		states = append(states, s.instances[id].state)
	}
	var jobs []string
	if s.cfg.Faults.FirmwareActive {
		jobs = append(jobs, "simulated PSU firmware")
	}
	s.mu.Unlock()
	for _, state := range states {
		state.mu.Lock()
		for id, job := range state.firmware.jobs {
			state.advanceFirmware(job)
			if job.CompletedAt == nil {
				jobs = append(jobs, state.ServerID+"/"+id)
			}
		}
		state.mu.Unlock()
	}
	slices.Sort(jobs)
	return jobs
}

// startRollout validates a Start request against the staged state of every
// instance and begins the scripted rollout. flashing lists the firmware jobs
// running when the request arrived.
func (s *updateSim) startRollout(in *mdk.StartServerUpdateRequest, actor string, flashing []string) (*mdk.ServerUpdateRollout, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if strings.TrimSpace(in.GetJobId()) == "" || in.GetTargetVersion() == "" || len(in.GetExpectedInstanceIds()) == 0 {
		return nil, invalidManifest("job_id, target_version and expected_instance_ids are required")
	}
	if s.cfg.Faults.SelfCoordinationUnsupported {
		return nil, refusal(codes.FailedPrecondition, "UPDATE_SELF_COORDINATION_UNSUPPORTED", "this server does not support self-coordinated updates")
	}
	if s.cfg.Faults.RemoteTriggerDisabled {
		return nil, refusal(codes.FailedPrecondition, "UPDATE_REMOTE_TRIGGER_DISABLED", "update.allow_remote_trigger is off")
	}
	identity := startRequestIdentity(in)
	if key := in.GetIdempotencyKey(); key != "" {
		if prior, ok := s.idempotency["start|"+key]; ok {
			if prior.request != identity {
				return nil, refusal(codes.FailedPrecondition, "UPDATE_IDEMPOTENCY_CONFLICT", "idempotency key was used for a different request")
			}
			return s.renderLocked(s.rollouts[prior.jobID]), nil
		}
	}
	if s.activeID != "" {
		return nil, refusal(codes.FailedPrecondition, "UPDATE_ACTIVE_JOB", "update %s is already active", s.activeID)
	}
	for _, id := range s.sortedIDsLocked() {
		if job := s.instances[id].protoOS.activeJob(); job != nil {
			return nil, refusal(codes.FailedPrecondition, "UPDATE_ACTIVE_JOB", "proto-os package job %s is active on %s", job.GetJobId(), id)
		}
	}
	if len(flashing) > 0 {
		return nil, refusal(codes.FailedPrecondition, "UPDATE_FIRMWARE_ACTIVE", "a firmware update is running: %s", strings.Join(flashing, ", "))
	}
	if _, exists := s.rollouts[in.GetJobId()]; exists {
		return nil, refusal(codes.FailedPrecondition, "UPDATE_ACTIVE_JOB", "job %s has already run", in.GetJobId())
	}
	if s.cfg.Faults.HelperUnavailable {
		return nil, refusal(codes.Unavailable, "UPDATE_HELPER_UNAVAILABLE", "the root update helper is unavailable")
	}
	if s.cfg.Faults.EtcdUnhealthy {
		return nil, refusal(codes.Unavailable, "UPDATE_ETCD_UNHEALTHY", "etcd has no quorum")
	}
	expected := slices.Clone(in.GetExpectedInstanceIds())
	slices.Sort(expected)
	if !slices.Equal(expected, s.sortedIDsLocked()) {
		return nil, refusal(codes.FailedPrecondition, "UPDATE_STAGE_MISMATCH", "expected_instance_ids %v do not match the cluster %v", expected, s.sortedIDsLocked())
	}
	var keyID string
	var size int64
	fingerprints := map[string]string{}
	for _, id := range expected {
		staged, ok := s.instances[id].stages[in.GetJobId()]
		if !ok || !staged.committed {
			return nil, refusal(codes.FailedPrecondition, "UPDATE_NOT_STAGED", "job %s is not staged on instance %s", in.GetJobId(), id)
		}
		m := staged.manifest
		if m.GetTargetVersion() != in.GetTargetVersion() || m.GetCoordination() != mdk.ServerUpdateCoordination_SERVER_UPDATE_COORDINATION_SELF ||
			m.GetPackage() != mdk.ServerUpdatePackage_SERVER_UPDATE_PACKAGE_UNSPECIFIED || !slices.Equal(sortedCopy(m.GetExpectedInstanceIds()), expected) {
			return nil, refusal(codes.FailedPrecondition, "UPDATE_STAGE_MISMATCH", "the staged manifest on %s does not match the request", id)
		}
		if want, ok := in.GetExpectedManifestSha256()[id]; ok && want != staged.fingerprint {
			return nil, refusal(codes.FailedPrecondition, "UPDATE_STAGE_MISMATCH", "the staged manifest fingerprint on %s does not match", id)
		}
		verifiedKey, err := s.verifyReleaseLocked(m)
		if err != nil {
			return nil, err
		}
		keyID, size = verifiedKey, m.GetSizeBytes()
		fingerprints[id] = staged.fingerprint
	}
	if s.cfg.SpoolFreeBytes < size {
		return nil, refusal(codes.FailedPrecondition, "UPDATE_INSUFFICIENT_SPACE", "spool has %d free bytes, package needs %d", s.cfg.SpoolFreeBytes, size)
	}
	if s.cfg.Faults.Cooldown > 0 && !s.lastSuccess.IsZero() && s.cfg.Now().Sub(s.lastSuccess) < s.cfg.Faults.Cooldown {
		return nil, refusal(codes.FailedPrecondition, "UPDATE_COOLDOWN", "an update finished less than %s ago", s.cfg.Faults.Cooldown)
	}
	if s.cfg.Faults.OutsideWindow {
		return nil, refusal(codes.FailedPrecondition, "UPDATE_OUTSIDE_WINDOW", "the maintenance window is closed")
	}
	now := s.cfg.Now()
	first := s.instances[expected[0]]
	msg := &mdk.ServerUpdateRollout{
		JobId:                       in.GetJobId(),
		TargetVersion:               in.GetTargetVersion(),
		Phase:                       mdk.ServerUpdateRolloutPhase_SERVER_UPDATE_ROLLOUT_PHASE_VERIFYING,
		DriverInstanceId:            s.primary,
		MigrationExecutorInstanceId: s.primary,
		NextStep:                    "Waiting for both helpers to verify the release statement and package.",
		StageOnly:                   in.GetStageOnly(),
		Cancellable:                 true,
		Actor:                       actor,
		CreatedAt:                   timestamppb.New(now),
		UpdatedAt:                   timestamppb.New(now),
		ExpiresAt:                   timestamppb.New(now.Add(rolloutExpiry)),
		FromVersion:                 first.runningVersion,
		PackageSha256:               first.stages[in.GetJobId()].manifest.GetSha256(),
		ReleaseKeyId:                keyID,
	}
	r := &rollout{msg: msg, helpers: map[string]*helperState{}, fingerprint: fingerprints, request: identity}
	for _, id := range expected {
		r.helpers[id] = &helperState{phase: mdk.FrigateUpdateAgentPhase_FRIGATE_UPDATE_AGENT_PHASE_CONNECTING}
	}
	s.rollouts[msg.JobId] = r
	s.order = append(s.order, msg.JobId)
	s.activeID = msg.JobId
	if key := in.GetIdempotencyKey(); key != "" {
		s.idempotency["start|"+key] = idempotentRecord{request: identity, jobID: msg.JobId}
	}
	s.scheduleLocked(r)
	return s.renderLocked(r), nil
}

func sortedCopy(in []string) []string {
	out := slices.Clone(in)
	slices.Sort(out)
	return out
}

func (s *updateSim) scheduleLocked(r *rollout) {
	r.timer = time.AfterFunc(s.cfg.StepDelay, func() {
		s.mu.Lock()
		if s.closed || isTerminalPhase(r.msg.Phase) {
			s.mu.Unlock()
			return
		}
		if r.msg.Phase == mdk.ServerUpdateRolloutPhase_SERVER_UPDATE_ROLLOUT_PHASE_STARTING {
			// Record the installed version on every controller before success
			// is visible, without holding mu (see updateSim.mu).
			target := r.msg.TargetVersion
			states := make([]*ContainerState, 0, len(s.instances))
			for _, inst := range s.instances {
				states = append(states, inst.state)
			}
			s.mu.Unlock()
			for _, state := range states {
				state.setVersion(target)
			}
			s.mu.Lock()
			if s.closed || r.msg.Phase != mdk.ServerUpdateRolloutPhase_SERVER_UPDATE_ROLLOUT_PHASE_STARTING {
				s.mu.Unlock()
				return
			}
		}
		s.advanceLocked(r)
		s.mu.Unlock()
	})
}

func (s *updateSim) setHelpersLocked(r *rollout, phase mdk.FrigateUpdateAgentPhase) {
	for _, h := range r.helpers {
		h.phase = phase
	}
}

func (s *updateSim) finishLocked(r *rollout) {
	if s.activeID == r.msg.JobId {
		s.activeID = ""
	}
}

// advanceLocked performs the next scripted step.
func (s *updateSim) advanceLocked(r *rollout) {
	m := r.msg
	m.UpdatedAt = timestamppb.New(s.cfg.Now())
	switch m.Phase {
	case mdk.ServerUpdateRolloutPhase_SERVER_UPDATE_ROLLOUT_PHASE_VERIFYING:
		if !r.verified {
			r.verified = true
			s.setHelpersLocked(r, mdk.FrigateUpdateAgentPhase_FRIGATE_UPDATE_AGENT_PHASE_STAGED)
			s.scheduleLocked(r)
			return
		}
		if m.StageOnly {
			m.Phase = mdk.ServerUpdateRolloutPhase_SERVER_UPDATE_ROLLOUT_PHASE_VERIFIED
			m.Cancellable = false
			m.NextStep = "Verification finished on every instance. Nothing was stopped."
			s.finishLocked(r)
			return
		}
		m.Phase = mdk.ServerUpdateRolloutPhase_SERVER_UPDATE_ROLLOUT_PHASE_STOPPING
		m.DisruptionStarted, m.Cancellable = true, false
		m.StoppedAt = timestamppb.New(s.cfg.Now())
		m.NextStep = "Frigate is stopping on both controllers."
		s.maintenance = true
		s.setHelpersLocked(r, mdk.FrigateUpdateAgentPhase_FRIGATE_UPDATE_AGENT_PHASE_STOPPED)
	case mdk.ServerUpdateRolloutPhase_SERVER_UPDATE_ROLLOUT_PHASE_STOPPING:
		s.enterInstallingLocked(r)
	case mdk.ServerUpdateRolloutPhase_SERVER_UPDATE_ROLLOUT_PHASE_INSTALLING:
		if s.enterMigratingLocked(r) {
			return
		}
	case mdk.ServerUpdateRolloutPhase_SERVER_UPDATE_ROLLOUT_PHASE_MIGRATING:
		m.Phase = mdk.ServerUpdateRolloutPhase_SERVER_UPDATE_ROLLOUT_PHASE_STARTING
		m.NextStep = "Starting Frigate on both controllers."
		s.setHelpersLocked(r, mdk.FrigateUpdateAgentPhase_FRIGATE_UPDATE_AGENT_PHASE_STARTED)
	case mdk.ServerUpdateRolloutPhase_SERVER_UPDATE_ROLLOUT_PHASE_STARTING:
		s.succeedLocked(r)
		return
	case mdk.ServerUpdateRolloutPhase_SERVER_UPDATE_ROLLOUT_PHASE_UNSPECIFIED,
		mdk.ServerUpdateRolloutPhase_SERVER_UPDATE_ROLLOUT_PHASE_SUCCEEDED,
		mdk.ServerUpdateRolloutPhase_SERVER_UPDATE_ROLLOUT_PHASE_FAILED,
		mdk.ServerUpdateRolloutPhase_SERVER_UPDATE_ROLLOUT_PHASE_CANCELLED,
		mdk.ServerUpdateRolloutPhase_SERVER_UPDATE_ROLLOUT_PHASE_VERIFIED,
		mdk.ServerUpdateRolloutPhase_SERVER_UPDATE_ROLLOUT_PHASE_ABANDONED:
		return
	}
	s.scheduleLocked(r)
}

func (s *updateSim) enterInstallingLocked(r *rollout) {
	m := r.msg
	m.Phase = mdk.ServerUpdateRolloutPhase_SERVER_UPDATE_ROLLOUT_PHASE_INSTALLING
	m.NextStep = "Installing the package on both controllers."
	s.setHelpersLocked(r, mdk.FrigateUpdateAgentPhase_FRIGATE_UPDATE_AGENT_PHASE_INSTALLED)
	if s.cfg.Faults.DriverReboot && !r.rebooted {
		// The driving node reboots; the peer's helper takes over from shared state.
		r.rebooted = true
		m.DriverInstanceId = s.peerOfLocked(m.DriverInstanceId)
	}
}

// enterMigratingLocked reports whether the rollout stopped (failed).
func (s *updateSim) enterMigratingLocked(r *rollout) bool {
	m := r.msg
	m.Phase = mdk.ServerUpdateRolloutPhase_SERVER_UPDATE_ROLLOUT_PHASE_MIGRATING
	m.NextStep = "Migrating the database on the Patroni leader."
	if s.cfg.Faults.MigrateFailures > 0 {
		s.cfg.Faults.MigrateFailures--
		m.Phase = mdk.ServerUpdateRolloutPhase_SERVER_UPDATE_ROLLOUT_PHASE_FAILED
		m.ErrorCode = "UPDATE_MIGRATE_FAILED"
		m.ErrorMessage = "database migration failed on " + m.MigrationExecutorInstanceId
		m.NextStep = "Both controllers remain stopped. Retry the update once the database is healthy."
		m.Retryable = true
		h := r.helpers[m.MigrationExecutorInstanceId]
		h.phase, h.errorCode, h.errorMessage = mdk.FrigateUpdateAgentPhase_FRIGATE_UPDATE_AGENT_PHASE_FAILED, m.ErrorCode, m.ErrorMessage
		// Frigate stays stopped; the maintenance service keeps answering.
		return true
	}
	s.setHelpersLocked(r, mdk.FrigateUpdateAgentPhase_FRIGATE_UPDATE_AGENT_PHASE_MIGRATED)
	r.helpers[m.MigrationExecutorInstanceId].migrationExecuted = true
	return false
}

// succeedLocked finishes the rollout. The scheduler has already recorded the
// new version on each ContainerState.
func (s *updateSim) succeedLocked(r *rollout) {
	m := r.msg
	m.Phase = mdk.ServerUpdateRolloutPhase_SERVER_UPDATE_ROLLOUT_PHASE_SUCCEEDED
	m.NextStep = "The update finished."
	s.maintenance = false
	s.lastSuccess = s.cfg.Now()
	for _, inst := range s.instances {
		inst.runningVersion = m.TargetVersion
		delete(inst.stages, m.JobId)
	}
	s.finishLocked(r)
}

// renderLocked builds the wire rollout for the current helper reports.
func (s *updateSim) renderLocked(r *rollout) *mdk.ServerUpdateRollout {
	out := proto.Clone(r.msg).(*mdk.ServerUpdateRollout)
	now := s.cfg.Now()
	ids := make([]string, 0, len(r.helpers))
	for id := range r.helpers {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	for _, id := range ids {
		h := r.helpers[id]
		progress := &mdk.ServerUpdateInstanceProgress{
			InstanceId: id, Phase: h.phase, LastContact: timestamppb.New(now), RunningVersion: s.instances[id].runningVersion,
			MigrationExecuted: h.migrationExecuted, ErrorCode: h.errorCode, ErrorMessage: h.errorMessage, ManifestSha256: r.fingerprint[id],
		}
		if s.cfg.Faults.StaleContact && id != r.msg.DriverInstanceId && !isTerminalPhase(r.msg.Phase) {
			progress.LastContact = timestamppb.New(now.Add(-staleContactAge))
			progress.ContactStale = true
		}
		out.Instances = append(out.Instances, progress)
	}
	return out
}

func (s *updateSim) lookupLocked(jobID string) (*rollout, error) {
	if jobID == "" {
		if s.activeID != "" {
			return s.rollouts[s.activeID], nil
		}
		if n := len(s.order); n > 0 {
			return s.rollouts[s.order[n-1]], nil
		}
		return nil, refusal(codes.NotFound, "UPDATE_NOT_FOUND", "no server update has run")
	}
	r, ok := s.rollouts[jobID]
	if !ok {
		return nil, refusal(codes.NotFound, "UPDATE_NOT_FOUND", "server update %s was not found", jobID)
	}
	return r, nil
}

// statusOf returns a rollout and whether the maintenance service answers.
func (s *updateSim) statusOf(jobID string) (*mdk.ServerUpdateRollout, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, err := s.lookupLocked(jobID)
	if err != nil {
		return nil, false, err
	}
	return s.renderLocked(r), s.maintenance, nil
}

func (s *updateSim) retryRollout(jobID, key string) (*mdk.ServerUpdateRollout, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, err := s.lookupLocked(jobID)
	if err != nil {
		return nil, err
	}
	if key != "" {
		if prior, ok := s.idempotency["retry|"+key]; ok {
			if prior.jobID != jobID {
				return nil, refusal(codes.FailedPrecondition, "UPDATE_IDEMPOTENCY_CONFLICT", "idempotency key was used for a different request")
			}
			return s.renderLocked(r), nil
		}
	}
	m := r.msg
	if m.Phase != mdk.ServerUpdateRolloutPhase_SERVER_UPDATE_ROLLOUT_PHASE_FAILED || !m.Retryable {
		return nil, refusal(codes.FailedPrecondition, "UPDATE_NOT_RETRYABLE", "server update %s cannot be retried", m.JobId)
	}
	if key != "" {
		s.idempotency["retry|"+key] = idempotentRecord{jobID: jobID}
	}
	// Resume at the step that failed.
	m.Phase = mdk.ServerUpdateRolloutPhase_SERVER_UPDATE_ROLLOUT_PHASE_INSTALLING
	m.ErrorCode, m.ErrorMessage, m.Retryable = "", "", false
	m.AutomaticRetries++
	m.UpdatedAt = timestamppb.New(s.cfg.Now())
	for _, h := range r.helpers {
		h.errorCode, h.errorMessage = "", ""
		h.phase = mdk.FrigateUpdateAgentPhase_FRIGATE_UPDATE_AGENT_PHASE_INSTALLED
	}
	s.activeID = m.JobId
	s.scheduleLocked(r)
	return s.renderLocked(r), nil
}

func (s *updateSim) cancelRollout(jobID, key string) (*mdk.ServerUpdateRollout, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, err := s.lookupLocked(jobID)
	if err != nil {
		return nil, err
	}
	if key != "" {
		if prior, ok := s.idempotency["cancel|"+key]; ok {
			if prior.jobID != jobID {
				return nil, refusal(codes.FailedPrecondition, "UPDATE_IDEMPOTENCY_CONFLICT", "idempotency key was used for a different request")
			}
			return s.renderLocked(r), nil
		}
	}
	m := r.msg
	if !m.Cancellable {
		return nil, refusal(codes.FailedPrecondition, "UPDATE_NOT_CANCELLABLE", "server update %s can no longer be cancelled", m.JobId)
	}
	if key != "" {
		s.idempotency["cancel|"+key] = idempotentRecord{jobID: jobID}
	}
	if r.timer != nil {
		r.timer.Stop()
	}
	m.Phase = mdk.ServerUpdateRolloutPhase_SERVER_UPDATE_ROLLOUT_PHASE_CANCELLED
	m.Cancellable = false
	m.NextStep = "The update was cancelled before anything was stopped."
	m.UpdatedAt = timestamppb.New(s.cfg.Now())
	for _, inst := range s.instances {
		delete(inst.stages, m.JobId)
	}
	s.finishLocked(r)
	return s.renderLocked(r), nil
}
