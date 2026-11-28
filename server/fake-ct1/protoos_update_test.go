package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	mdk "github.com/block/proto-fleet/sdk/mdk/generated/mdk/v2"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	protoOSJobA = "a1b2c3d4-0000-4000-8000-000000000001"
	protoOSJobB = "a1b2c3d4-0000-4000-8000-000000000002"
	protoOSJobC = "a1b2c3d4-0000-4000-8000-000000000003"
	protoOSJobD = "a1b2c3d4-0000-4000-8000-000000000004"
)

// newProtoOSHarness is a pair that trusts its own ProtoOS release key beside
// the harness's Frigate release key.
func newProtoOSHarness(t *testing.T, mutate func(*updateSimConfig)) (*updateHarness, *ecdsa.PrivateKey) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	h := newUpdateHarness(t, func(cfg *updateSimConfig) {
		cfg.ProtoOSTrustedKeys = []*ecdsa.PublicKey{&key.PublicKey}
		if mutate != nil {
			mutate(cfg)
		}
	})
	return h, key
}

// protoOSManifest is a self-coordinated proto-os manifest for tag, signed with
// key. edit may change the statement before it is signed.
func protoOSManifest(t *testing.T, key *ecdsa.PrivateKey, job, instance, tag string, pkg []byte, edit func(*releaseStatement)) *mdk.ServerUpdateManifest {
	t.Helper()
	sum := sha256.Sum256(pkg)
	version := protoOSDebianVersion(tag)
	s := releaseStatement{
		Schema: releaseStatementSchema, Package: "proto-os", Version: version, Architecture: "all", Repository: "block/proto-fleet",
		SHA256: hex.EncodeToString(sum[:]), Size: int64(len(pkg)), IssuedAt: "2026-01-01T00:00:00Z",
	}
	if edit != nil {
		edit(&s)
	}
	statement, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(statement)
	signature, err := ecdsa.SignASN1(rand.Reader, key, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	return &mdk.ServerUpdateManifest{
		JobId: job, InstanceId: instance, ExpectedInstanceIds: []string{"ct1-a", "ct1-b"}, ReleaseTag: tag, TargetVersion: version,
		AssetName: "proto-os_" + tag + "_all.deb", Sha256: hex.EncodeToString(sum[:]), SizeBytes: int64(len(pkg)), Actor: "fleet:tester",
		Coordination: mdk.ServerUpdateCoordination_SERVER_UPDATE_COORDINATION_SELF, Package: mdk.ServerUpdatePackage_SERVER_UPDATE_PACKAGE_PROTO_OS,
		ReleaseStatement: statement, ReleaseSignature: signature,
	}
}

func (h *updateHarness) support(i int) *mdk.GetServerUpdateSupportResponse {
	h.t.Helper()
	support, err := h.update(i).GetServerUpdateSupport(context.Background(), &mdk.GetServerUpdateSupportRequest{})
	if err != nil {
		h.t.Fatal(err)
	}
	return support
}

// waitProtoOSJob polls instance i until job reaches a final state and returns
// that support answer and every state it saw.
func (h *updateHarness) waitProtoOSJob(i int, job string) (*mdk.GetServerUpdateSupportResponse, []mdk.ProtoOSPackageJobState) {
	h.t.Helper()
	var seen []mdk.ProtoOSPackageJobState
	deadline := time.Now().Add(10 * time.Second)
	for {
		support := h.support(i)
		got := support.GetProtoOsJob()
		if got.GetJobId() == job && (len(seen) == 0 || seen[len(seen)-1] != got.GetState()) {
			seen = append(seen, got.GetState())
		}
		if got.GetJobId() == job && (got.GetState() == mdk.ProtoOSPackageJobState_PROTO_OS_PACKAGE_JOB_STATE_SUCCEEDED ||
			got.GetState() == mdk.ProtoOSPackageJobState_PROTO_OS_PACKAGE_JOB_STATE_FAILED) {
			return support, seen
		}
		if time.Now().After(deadline) {
			h.t.Fatalf("proto-os job %s never finished on instance %d: %v", job, i, got)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func requireProtoOSJob(t *testing.T, support *mdk.GetServerUpdateSupportResponse, state mdk.ProtoOSPackageJobState, messagePart string) {
	t.Helper()
	job := support.GetProtoOsJob()
	if job.GetState() != state || !strings.Contains(job.GetMessage(), messagePart) {
		t.Fatalf("proto-os job = %v, want %s with a message that contains %q", job, state, messagePart)
	}
}

func TestProtoOSPackageInstallsOnTheCommittedController(t *testing.T) {
	h, key := newProtoOSHarness(t, func(cfg *updateSimConfig) { cfg.StepDelay = 100 * time.Millisecond })
	before := h.support(0)
	if !before.GetProtoOsSelfSupported() || before.GetProtoOsSupportMessage() != "" || before.GetProtoOsInstalledVersion() != "" ||
		before.GetProtoOsServed().GetSource() != mdk.ProtoOSBundleSource_PROTO_OS_BUNDLE_SOURCE_BUNDLED || before.GetProtoOsJob() != nil {
		t.Fatalf("unexpected initial proto-os support: %v", before)
	}
	if got := before.GetProtoOsReleaseRepositories(); !slices.Contains(got, "block/proto-fleet") {
		t.Fatalf("release repositories = %v", got)
	}

	pkg := testPackage(2500)
	m := protoOSManifest(t, key, protoOSJobA, "ct1-a", "v0.6.2-internal.14", pkg, nil)
	fingerprint := h.stageManifest(0, m, pkg, 700)
	support, seen := h.waitProtoOSJob(0, protoOSJobA)
	requireProtoOSJob(t, support, mdk.ProtoOSPackageJobState_PROTO_OS_PACKAGE_JOB_STATE_SUCCEEDED, "installed")
	want := []mdk.ProtoOSPackageJobState{
		mdk.ProtoOSPackageJobState_PROTO_OS_PACKAGE_JOB_STATE_QUEUED,
		mdk.ProtoOSPackageJobState_PROTO_OS_PACKAGE_JOB_STATE_INSTALLING,
		mdk.ProtoOSPackageJobState_PROTO_OS_PACKAGE_JOB_STATE_SUCCEEDED,
	}
	if len(seen) != len(want) || seen[0] != want[0] || seen[1] != want[1] || seen[2] != want[2] {
		t.Fatalf("job states = %v, want %v", seen, want)
	}
	job := support.GetProtoOsJob()
	if job.GetReleaseTag() != "v0.6.2-internal.14" || job.GetTargetVersion() != "0.6.2~internal.14" || job.GetUpdatedAt() == nil {
		t.Fatalf("job = %v", job)
	}
	if support.GetProtoOsInstalledVersion() != "0.6.2~internal.14" ||
		support.GetProtoOsServed().GetSource() != mdk.ProtoOSBundleSource_PROTO_OS_BUNDLE_SOURCE_PACKAGE ||
		support.GetProtoOsServed().GetReleaseTag() != "v0.6.2-internal.14" {
		t.Fatalf("support after install = %v", support)
	}
	if peer := h.support(1); peer.GetProtoOsJob() != nil || peer.GetProtoOsInstalledVersion() != "" {
		t.Fatalf("the peer changed: %v", peer)
	}

	// The copy stays complete: a repeated Begin or Commit queues no new job.
	ctx := context.Background()
	begin, err := h.update(0).BeginServerUpdateStage(ctx, &mdk.BeginServerUpdateStageRequest{Manifest: m})
	if err != nil || begin.GetNextOffset() != int64(len(pkg)) {
		t.Fatalf("repeated begin = %v, %v", begin, err)
	}
	commit, err := h.update(0).CommitServerUpdateStage(ctx, &mdk.CommitServerUpdateStageRequest{JobId: protoOSJobA, ManifestSha256: fingerprint})
	if err != nil || commit.GetBytesStaged() != int64(len(pkg)) || commit.GetSha256() != m.GetSha256() {
		t.Fatalf("repeated commit = %v, %v", commit, err)
	}
	requireProtoOSJob(t, h.support(0), mdk.ProtoOSPackageJobState_PROTO_OS_PACKAGE_JOB_STATE_SUCCEEDED, "installed")
}

func TestProtoOSPackageOrdersVersions(t *testing.T) {
	h, key := newProtoOSHarness(t, nil)
	pkg := testPackage(900)
	install := func(job, tag string) *mdk.GetServerUpdateSupportResponse {
		t.Helper()
		h.stageManifest(0, protoOSManifest(t, key, job, "ct1-a", tag, pkg, nil), pkg, 700)
		support, _ := h.waitProtoOSJob(0, job)
		return support
	}

	requireProtoOSJob(t, install(protoOSJobA, "v0.6.2"), mdk.ProtoOSPackageJobState_PROTO_OS_PACKAGE_JOB_STATE_SUCCEEDED, "installed")
	// A prerelease of the installed version is older.
	downgrade := install(protoOSJobB, "v0.6.2-internal.14")
	requireProtoOSJob(t, downgrade, mdk.ProtoOSPackageJobState_PROTO_OS_PACKAGE_JOB_STATE_FAILED, "refusing to downgrade proto-os from 0.6.2 to 0.6.2~internal.14")
	if downgrade.GetProtoOsInstalledVersion() != "0.6.2" || downgrade.GetProtoOsServed().GetReleaseTag() != "v0.6.2" {
		t.Fatalf("a refused downgrade changed the controller: %v", downgrade)
	}
	requireProtoOSJob(t, install(protoOSJobC, "v0.6.2"), mdk.ProtoOSPackageJobState_PROTO_OS_PACKAGE_JOB_STATE_SUCCEEDED, "is already installed")
	// Versions compare by number, not as text.
	upgrade := install(protoOSJobD, "v0.6.10")
	requireProtoOSJob(t, upgrade, mdk.ProtoOSPackageJobState_PROTO_OS_PACKAGE_JOB_STATE_SUCCEEDED, "installed")
	if upgrade.GetProtoOsInstalledVersion() != "0.6.10" {
		t.Fatalf("installed = %q", upgrade.GetProtoOsInstalledVersion())
	}
}

func TestProtoOSAndFrigateReleaseKeysAreSeparate(t *testing.T) {
	h, protoOSKey := newProtoOSHarness(t, nil)
	pkg := testPackage(900)

	// A proto-os statement signed with the Frigate release key is refused.
	h.stageManifest(0, protoOSManifest(t, h.key, protoOSJobA, "ct1-a", "v0.6.2", pkg, nil), pkg, 700)
	support, _ := h.waitProtoOSJob(0, protoOSJobA)
	requireProtoOSJob(t, support, mdk.ProtoOSPackageJobState_PROTO_OS_PACKAGE_JOB_STATE_FAILED, "UPDATE_SIGNATURE_INVALID")
	if support.GetProtoOsInstalledVersion() != "" {
		t.Fatalf("an untrusted package was installed: %v", support)
	}

	// A Frigate statement signed with the ProtoOS release key is refused.
	h.key = protoOSKey
	fingerprints := h.stageBoth("d9428888-122b-11e1-b85c-61cd3cbb3210", pkg)
	_, err := h.update(0).StartServerUpdate(context.Background(), startRequest("d9428888-122b-11e1-b85c-61cd3cbb3210", fingerprints))
	requireRefusal(t, err, codes.FailedPrecondition, "UPDATE_SIGNATURE_INVALID")

	t.Run("no ProtoOS keys", func(t *testing.T) {
		h, _ := newProtoOSHarness(t, func(cfg *updateSimConfig) { cfg.ProtoOSTrustedKeys = nil })
		h.stageManifest(1, protoOSManifest(t, h.key, protoOSJobA, "ct1-b", "v0.6.2", pkg, nil), pkg, 700)
		support, _ := h.waitProtoOSJob(1, protoOSJobA)
		requireProtoOSJob(t, support, mdk.ProtoOSPackageJobState_PROTO_OS_PACKAGE_JOB_STATE_FAILED, "UPDATE_NO_TRUSTED_KEYS")
	})
}

func TestProtoOSPackageInstallFault(t *testing.T) {
	h, key := newProtoOSHarness(t, func(cfg *updateSimConfig) { cfg.Faults.ProtoOSInstallFails = true })
	pkg := testPackage(900)
	h.stageManifest(1, protoOSManifest(t, key, protoOSJobA, "ct1-b", "v0.6.2", pkg, nil), pkg, 700)
	support, _ := h.waitProtoOSJob(1, protoOSJobA)
	requireProtoOSJob(t, support, mdk.ProtoOSPackageJobState_PROTO_OS_PACKAGE_JOB_STATE_FAILED, "UPDATE_INSTALL_FAILED")
	if support.GetProtoOsInstalledVersion() != "" || support.GetProtoOsServed().GetSource() != mdk.ProtoOSBundleSource_PROTO_OS_BUNDLE_SOURCE_BUNDLED {
		t.Fatalf("a failed install changed the controller: %v", support)
	}
}

func TestProtoOSPackageStagingRefusals(t *testing.T) {
	pkg := testPackage(900)
	begin := func(h *updateHarness, m *mdk.ServerUpdateManifest) error {
		_, err := h.update(0).BeginServerUpdateStage(context.Background(), &mdk.BeginServerUpdateStageRequest{Manifest: m})
		return err
	}

	t.Run("remote trigger disabled", func(t *testing.T) {
		h, key := newProtoOSHarness(t, func(cfg *updateSimConfig) { cfg.Faults.RemoteTriggerDisabled = true })
		requireRefusal(t, begin(h, protoOSManifest(t, key, protoOSJobA, "ct1-a", "v0.6.2", pkg, nil)), codes.FailedPrecondition, "UPDATE_REMOTE_TRIGGER_DISABLED")
	})
	t.Run("self coordination for proto-os unsupported", func(t *testing.T) {
		h, key := newProtoOSHarness(t, func(cfg *updateSimConfig) { cfg.Faults.ProtoOSSelfUnsupported = true })
		support := h.support(0)
		if support.GetProtoOsSelfSupported() || support.GetProtoOsSupportMessage() == "" {
			t.Fatalf("support = %v", support)
		}
		err := begin(h, protoOSManifest(t, key, protoOSJobA, "ct1-a", "v0.6.2", pkg, nil))
		if status.Code(err) != codes.FailedPrecondition {
			t.Fatalf("begin = %v", err)
		}
	})

	h, key := newProtoOSHarness(t, nil)
	for name, m := range map[string]*mdk.ServerUpdateManifest{
		"other asset":    withManifest(protoOSManifest(t, key, protoOSJobA, "ct1-a", "v0.6.2", pkg, nil), func(m *mdk.ServerUpdateManifest) { m.AssetName = "proto-os_v0.6.2_arm64.deb" }),
		"tag as version": withManifest(protoOSManifest(t, key, protoOSJobA, "ct1-a", "v0.6.2-rc.1", pkg, nil), func(m *mdk.ServerUpdateManifest) { m.TargetVersion = "0.6.2-rc.1" }),
		"tag without v":  withManifest(protoOSManifest(t, key, protoOSJobA, "ct1-a", "v0.6.2", pkg, nil), func(m *mdk.ServerUpdateManifest) { m.ReleaseTag, m.AssetName = "0.6.2", "proto-os_0.6.2_all.deb" }),
		"legacy": withManifest(protoOSManifest(t, key, protoOSJobA, "ct1-a", "v0.6.2", pkg, nil), func(m *mdk.ServerUpdateManifest) {
			m.Coordination = mdk.ServerUpdateCoordination_SERVER_UPDATE_COORDINATION_UNSPECIFIED
		}),
		"flotilla fields": withManifest(protoOSManifest(t, key, protoOSJobA, "ct1-a", "v0.6.2", pkg, nil), func(m *mdk.ServerUpdateManifest) { m.MaintenanceToken = "token" }),
		"frigate statement": protoOSManifest(t, key, protoOSJobA, "ct1-a", "v0.6.2", pkg, func(s *releaseStatement) {
			s.Package, s.Architecture = "frigate", "arm64"
		}),
		"other repository": protoOSManifest(t, key, protoOSJobA, "ct1-a", "v0.6.2", pkg, func(s *releaseStatement) { s.Repository = "example/proto-os" }),
		"floor set":        protoOSManifest(t, key, protoOSJobA, "ct1-a", "v0.6.2", pkg, func(s *releaseStatement) { s.MinInstalledVersion = "0.5.0" }),
		"other version":    protoOSManifest(t, key, protoOSJobA, "ct1-a", "v0.6.2", pkg, func(s *releaseStatement) { s.Version = "0.6.3" }),
	} {
		t.Run(name, func(t *testing.T) {
			requireRefusal(t, begin(h, m), codes.InvalidArgument, "UPDATE_MANIFEST_INVALID")
		})
	}
}

func withManifest(m *mdk.ServerUpdateManifest, edit func(*mdk.ServerUpdateManifest)) *mdk.ServerUpdateManifest {
	edit(m)
	return m
}

func TestProtoOSAndFrigateJobsExcludeEachOther(t *testing.T) {
	ctx := context.Background()
	pkg := testPackage(900)

	t.Run("a proto-os job blocks Frigate on its controller", func(t *testing.T) {
		// The job stays queued for the whole test.
		h, key := newProtoOSHarness(t, func(cfg *updateSimConfig) { cfg.StepDelay = time.Hour })
		const frigateJob = "b1b2c3d4-0000-4000-8000-000000000001"
		fingerprints := h.stageBoth(frigateJob, pkg)
		protoOSFingerprint := h.stageManifest(0, protoOSManifest(t, key, protoOSJobA, "ct1-a", "v0.6.2", pkg, nil), pkg, 700)
		if state := h.support(0).GetProtoOsJob().GetState(); state != mdk.ProtoOSPackageJobState_PROTO_OS_PACKAGE_JOB_STATE_QUEUED {
			t.Fatalf("proto-os job state = %s", state)
		}

		_, err := h.update(0).StartServerUpdate(ctx, startRequest(frigateJob, fingerprints))
		requireRefusal(t, err, codes.FailedPrecondition, "UPDATE_ACTIVE_JOB")
		_, err = h.update(0).BeginServerUpdateStage(ctx, &mdk.BeginServerUpdateStageRequest{Manifest: h.manifest("b1b2c3d4-0000-4000-8000-000000000002", "ct1-a", pkg)})
		requireRefusal(t, err, codes.FailedPrecondition, "UPDATE_ACTIVE_JOB")
		_, err = h.update(0).BeginServerUpdateStage(ctx, &mdk.BeginServerUpdateStageRequest{Manifest: protoOSManifest(t, key, protoOSJobB, "ct1-a", "v0.6.3", pkg, nil)})
		requireRefusal(t, err, codes.FailedPrecondition, "UPDATE_ACTIVE_JOB")
		_, err = h.update(0).AbortServerUpdateStage(ctx, &mdk.AbortServerUpdateStageRequest{JobId: protoOSJobA, ManifestSha256: protoOSFingerprint})
		requireRefusal(t, err, codes.FailedPrecondition, "UPDATE_ACTIVE_JOB")

		// The other controller holds no job.
		if _, err := h.update(1).BeginServerUpdateStage(ctx, &mdk.BeginServerUpdateStageRequest{Manifest: protoOSManifest(t, key, protoOSJobB, "ct1-b", "v0.6.2", pkg, nil)}); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("a Frigate rollout blocks proto-os", func(t *testing.T) {
		h, key := newProtoOSHarness(t, func(cfg *updateSimConfig) { cfg.StepDelay = time.Hour })
		const frigateJob = "b1b2c3d4-0000-4000-8000-000000000003"
		if _, err := h.update(0).StartServerUpdate(ctx, startRequest(frigateJob, h.stageBoth(frigateJob, pkg))); err != nil {
			t.Fatal(err)
		}
		_, err := h.update(1).BeginServerUpdateStage(ctx, &mdk.BeginServerUpdateStageRequest{Manifest: protoOSManifest(t, key, protoOSJobA, "ct1-b", "v0.6.2", pkg, nil)})
		requireRefusal(t, err, codes.FailedPrecondition, "UPDATE_ACTIVE_JOB")
	})
}

func TestCompareDebianVersions(t *testing.T) {
	for _, tc := range []struct {
		a, b string
		want int
	}{
		{"0.6.2", "0.6.2", 0},
		{"0.6.2~internal.14", "0.6.2", -1},
		{"0.6.10", "0.6.9", 1},
		{"0.6.2~internal.14", "0.6.2~internal.9", 1},
		{"1.0.0~rc-1", "1.0.0~rc-2", -1},
		{"1.0.0", "1.0.0~rc-1", 1},
	} {
		if got := compareDebianVersions(tc.a, tc.b); got != tc.want {
			t.Errorf("compareDebianVersions(%q, %q) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
}
