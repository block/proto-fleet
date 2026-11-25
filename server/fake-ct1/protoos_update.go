package main

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"time"

	mdk "github.com/block/proto-fleet/sdk/mdk/generated/mdk/v2"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// This file simulates what a CT1 controller does with a self-coordinated
// proto-os manifest: as soon as the copy is committed, the root update helper
// verifies the signed release statement against the ProtoOS release keys and
// installs the package on that controller alone. Frigate keeps running, and
// the peer is not involved.

const (
	protoOSPackageName      = "proto-os"
	protoOSArchitecture     = "all"
	maxProtoOSTagBytes      = 128
	protoOSUnsupportedText  = "this Frigate does not accept self-coordinated proto-os packages"
	protoOSQueuedMessage    = "Waiting for the update helper to verify the package."
	protoOSInstallingFormat = "Installing proto-os %s."
)

// protoOSReleaseRepositories are the repositories whose proto-os release
// statements Frigate accepts.
var protoOSReleaseRepositories = []string{"btc-mining/proto-fleet-internal", "block/proto-fleet"}

// protoOSState is one controller's proto-os package and its latest job.
type protoOSState struct {
	// installed is the Debian version of the installed package, or "".
	installed string
	served    *mdk.ProtoOSBundleStatus
	job       *mdk.ProtoOSPackageJob
}

// newProtoOSState is a controller that serves the copy of ProtoOS that the
// frigate package embeds, with no proto-os package installed.
func newProtoOSState() protoOSState {
	return protoOSState{served: &mdk.ProtoOSBundleStatus{Source: mdk.ProtoOSBundleSource_PROTO_OS_BUNDLE_SOURCE_BUNDLED}}
}

// activeJob returns the job while it is queued or installing, else nil.
func (p *protoOSState) activeJob() *mdk.ProtoOSPackageJob {
	switch p.job.GetState() {
	case mdk.ProtoOSPackageJobState_PROTO_OS_PACKAGE_JOB_STATE_QUEUED, mdk.ProtoOSPackageJobState_PROTO_OS_PACKAGE_JOB_STATE_INSTALLING:
		return p.job
	case mdk.ProtoOSPackageJobState_PROTO_OS_PACKAGE_JOB_STATE_UNSPECIFIED,
		mdk.ProtoOSPackageJobState_PROTO_OS_PACKAGE_JOB_STATE_SUCCEEDED,
		mdk.ProtoOSPackageJobState_PROTO_OS_PACKAGE_JOB_STATE_FAILED:
		return nil
	}
	return nil
}

func cloneProtoOSJob(job *mdk.ProtoOSPackageJob) *mdk.ProtoOSPackageJob {
	if job == nil {
		return nil
	}
	return proto.Clone(job).(*mdk.ProtoOSPackageJob)
}

func (s *updateSim) protoOSSupportMessageLocked() string {
	if s.cfg.Faults.ProtoOSSelfUnsupported {
		return protoOSUnsupportedText
	}
	return ""
}

// protoOSDebianVersion is Frigate's mapping from a ProtoOS release tag to the
// Debian version of its package.
func protoOSDebianVersion(tag string) string {
	return strings.Replace(strings.TrimPrefix(tag, "v"), "-", "~", 1)
}

func validProtoOSTag(tag string) bool {
	if len(tag) < 2 || len(tag) > maxProtoOSTagBytes || tag[0] != 'v' || tag[1] < '0' || tag[1] > '9' {
		return false
	}
	for _, c := range tag[1:] {
		if (c < '0' || c > '9') && (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && c != '.' && c != '-' {
			return false
		}
	}
	return true
}

// validateProtoOSManifestLocked checks a proto-os manifest as Frigate does
// before it accepts any byte: the identity fields, the statement syntax and
// its binding to the manifest, then support. The signature is checked only by
// the helper, after the copy is committed.
func (s *updateSim) validateProtoOSManifestLocked(m *mdk.ServerUpdateManifest) error {
	if m.GetCoordination() != mdk.ServerUpdateCoordination_SERVER_UPDATE_COORDINATION_SELF {
		return invalidManifest("the simulator accepts only self-coordinated proto-os manifests")
	}
	if hasFlotillaFields(m) {
		return invalidManifest("a self-coordinated manifest must not carry Flotilla connection fields")
	}
	tag := m.GetReleaseTag()
	if !validProtoOSTag(tag) {
		return invalidManifest("proto-os release tag must be a v-prefixed version")
	}
	if m.GetTargetVersion() != protoOSDebianVersion(tag) {
		return invalidManifest("proto-os target version must be the Debian version of the release tag")
	}
	if m.GetAssetName() != "proto-os_"+tag+"_all.deb" {
		return invalidManifest("proto-os asset name is invalid")
	}
	statement, signature := m.GetReleaseStatement(), m.GetReleaseSignature()
	if len(statement) == 0 || len(statement) > maxStatementBytes || len(signature) == 0 || len(signature) > maxSignatureBytes {
		return invalidManifest("the proto-os package requires a bounded release statement and signature")
	}
	decoded, ok := parseStatement(statement)
	switch {
	case !ok:
		return invalidManifest("release statement is malformed or has an unsupported schema")
	case decoded.Package != protoOSPackageName || decoded.Architecture != protoOSArchitecture:
		return invalidManifest("release statement names package %q for %q, not %s for %s", decoded.Package, decoded.Architecture, protoOSPackageName, protoOSArchitecture)
	case !slices.Contains(protoOSReleaseRepositories, decoded.Repository):
		return invalidManifest("release statement names repository %q, which this instance does not accept", decoded.Repository)
	case decoded.Version != m.GetTargetVersion() || decoded.SHA256 != m.GetSha256() || decoded.Size != m.GetSizeBytes():
		return invalidManifest("release statement does not match the manifest")
	case decoded.MinInstalledVersion != "":
		return invalidManifest("release statement for proto-os must not set min_installed_version")
	}
	if s.cfg.Faults.ProtoOSSelfUnsupported {
		return status.Error(codes.FailedPrecondition, protoOSUnsupportedText)
	}
	if s.cfg.Faults.RemoteTriggerDisabled {
		return refusal(codes.FailedPrecondition, "UPDATE_REMOTE_TRIGGER_DISABLED", "update.allow_remote_trigger is off")
	}
	return nil
}

// queueProtoOSLocked starts the helper's job for a committed proto-os copy.
func (s *updateSim) queueProtoOSLocked(inst *updateInstance, staged *stagedPackage) {
	m := staged.manifest
	inst.protoOS.job = &mdk.ProtoOSPackageJob{
		JobId: m.GetJobId(), ReleaseTag: m.GetReleaseTag(), TargetVersion: m.GetTargetVersion(),
		State: mdk.ProtoOSPackageJobState_PROTO_OS_PACKAGE_JOB_STATE_QUEUED, Message: protoOSQueuedMessage, UpdatedAt: timestamppb.New(s.cfg.Now()),
	}
	s.scheduleProtoOSLocked(inst, staged)
}

func (s *updateSim) scheduleProtoOSLocked(inst *updateInstance, staged *stagedPackage) {
	job := inst.protoOS.job
	time.AfterFunc(s.cfg.StepDelay, func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		if !s.closed && inst.protoOS.job == job {
			s.advanceProtoOSLocked(inst, staged)
		}
	})
}

// advanceProtoOSLocked performs the helper's next step: verify, then install.
func (s *updateSim) advanceProtoOSLocked(inst *updateInstance, staged *stagedPackage) {
	job, m := inst.protoOS.job, staged.manifest
	job.UpdatedAt = timestamppb.New(s.cfg.Now())
	switch job.GetState() {
	case mdk.ProtoOSPackageJobState_PROTO_OS_PACKAGE_JOB_STATE_QUEUED:
		if len(s.protoOSTrusted) == 0 {
			s.finishProtoOSLocked(inst, staged, false, "UPDATE_NO_TRUSTED_KEYS: no trusted ProtoOS release keys are installed")
			return
		}
		if verifySignature(s.protoOSTrusted, m.GetReleaseStatement(), m.GetReleaseSignature()) == "" {
			s.finishProtoOSLocked(inst, staged, false, "UPDATE_SIGNATURE_INVALID: no trusted ProtoOS release key validates the signature")
			return
		}
		if installed := inst.protoOS.installed; installed != "" {
			switch order := compareDebianVersions(m.GetTargetVersion(), installed); {
			case order < 0:
				s.finishProtoOSLocked(inst, staged, false, fmt.Sprintf("refusing to downgrade proto-os from %s to %s", installed, m.GetTargetVersion()))
				return
			case order == 0:
				s.finishProtoOSLocked(inst, staged, true, "proto-os "+installed+" is already installed")
				return
			}
		}
		job.State = mdk.ProtoOSPackageJobState_PROTO_OS_PACKAGE_JOB_STATE_INSTALLING
		job.Message = fmt.Sprintf(protoOSInstallingFormat, m.GetTargetVersion())
		s.scheduleProtoOSLocked(inst, staged)
	case mdk.ProtoOSPackageJobState_PROTO_OS_PACKAGE_JOB_STATE_INSTALLING:
		if s.cfg.Faults.ProtoOSInstallFails {
			s.finishProtoOSLocked(inst, staged, false, "UPDATE_INSTALL_FAILED: dpkg could not install proto-os (simulated fault)")
			return
		}
		inst.protoOS.installed = m.GetTargetVersion()
		inst.protoOS.served = &mdk.ProtoOSBundleStatus{
			Source: mdk.ProtoOSBundleSource_PROTO_OS_BUNDLE_SOURCE_PACKAGE, ReleaseTag: m.GetReleaseTag(), LoadedAt: job.GetUpdatedAt(),
		}
		s.finishProtoOSLocked(inst, staged, true, "proto-os "+m.GetTargetVersion()+" is installed")
	case mdk.ProtoOSPackageJobState_PROTO_OS_PACKAGE_JOB_STATE_UNSPECIFIED,
		mdk.ProtoOSPackageJobState_PROTO_OS_PACKAGE_JOB_STATE_SUCCEEDED,
		mdk.ProtoOSPackageJobState_PROTO_OS_PACKAGE_JOB_STATE_FAILED:
	}
}

// finishProtoOSLocked ends the job. The committed copy stays as a record so a
// repeated Begin or Commit for the job answers as complete, but its bytes go.
func (s *updateSim) finishProtoOSLocked(inst *updateInstance, staged *stagedPackage, succeeded bool, message string) {
	job := inst.protoOS.job
	job.State = mdk.ProtoOSPackageJobState_PROTO_OS_PACKAGE_JOB_STATE_FAILED
	if succeeded {
		job.State = mdk.ProtoOSPackageJobState_PROTO_OS_PACKAGE_JOB_STATE_SUCCEEDED
	}
	job.Message = message
	staged.data = nil
}

// compareDebianVersions orders two Debian versions with dpkg's algorithm. The
// simulator only sees versions that protoOSDebianVersion produced, so it skips
// epochs.
func compareDebianVersions(a, b string) int {
	split := func(v string) (string, string) {
		for i := len(v) - 1; i >= 0; i-- {
			if v[i] == '-' {
				return v[:i], v[i+1:]
			}
		}
		return v, ""
	}
	aUpstream, aRevision := split(a)
	bUpstream, bRevision := split(b)
	if order := compareDebianFragment(aUpstream, bUpstream); order != 0 {
		return order
	}
	return compareDebianFragment(aRevision, bRevision)
}

// compareDebianFragment is dpkg's verrevcmp.
func compareDebianFragment(a, b string) int {
	isDigit := func(c byte) bool { return c >= '0' && c <= '9' }
	weight := func(v string, i int) int {
		switch {
		case i >= len(v) || isDigit(v[i]):
			return 0
		case (v[i] >= 'a' && v[i] <= 'z') || (v[i] >= 'A' && v[i] <= 'Z'):
			return int(v[i])
		case v[i] == '~':
			return -1
		default:
			return int(v[i]) + 256
		}
	}
	i, j := 0, 0
	for i < len(a) || j < len(b) {
		for (i < len(a) && !isDigit(a[i])) || (j < len(b) && !isDigit(b[j])) {
			if wa, wb := weight(a, i), weight(b, j); wa != wb {
				return cmp.Compare(wa, wb)
			}
			i++
			j++
		}
		for i < len(a) && a[i] == '0' {
			i++
		}
		for j < len(b) && b[j] == '0' {
			j++
		}
		first := 0
		for i < len(a) && j < len(b) && isDigit(a[i]) && isDigit(b[j]) {
			if first == 0 {
				first = int(a[i]) - int(b[j])
			}
			i++
			j++
		}
		if i < len(a) && isDigit(a[i]) {
			return 1
		}
		if j < len(b) && isDigit(b[j]) {
			return -1
		}
		if first != 0 {
			return cmp.Compare(first, 0)
		}
	}
	return 0
}
