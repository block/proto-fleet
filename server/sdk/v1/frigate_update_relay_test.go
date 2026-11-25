package sdk

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	pb "github.com/block/proto-fleet/server/sdk/v1/pb/generated"
)

// relayTo returns a relay whose far side is device, as a Fleet Node serves it.
func relayTo(device DeviceFrigateUpdater) FrigateRelay {
	return NewFrigateRelay(func(ctx context.Context, request []byte) ([]byte, error) {
		return ServeFrigateRelay(ctx, device, request)
	})
}

func TestFrigateRelayCarriesEveryCallExceptAppend(t *testing.T) {
	device := &frigateUpdaterDevice{}
	relay := relayTo(device)
	ctx := t.Context()

	support, err := relay.FrigateUpdateSupport(ctx)
	require.NoError(t, err)
	require.Len(t, support, 2)
	assert.Equal(t, "controller unreachable", support[1].Err)

	progress, err := relay.FrigateStageBegin(ctx, "a", validManifest())
	require.NoError(t, err)
	assert.Equal(t, FrigateStageProgress{JobID: testFrigateJob, ManifestSHA256: testFrigateFP, NextOffset: 1024, SizeBytes: 1}, progress)
	assert.Equal(t, validManifest(), device.manifest)

	commit, err := relay.FrigateStageCommit(ctx, "a", validRef())
	require.NoError(t, err)
	assert.Equal(t, FrigateStageCommit{JobID: testFrigateJob, BytesStaged: 4096, SHA256: testFrigateDigest}, commit)

	require.NoError(t, relay.FrigateStageAbort(ctx, "b", validRef()))
	assert.Equal(t, "abort", device.method)
	assert.Equal(t, "b", device.instance)

	rollout, err := relay.FrigateUpdateStart(ctx, validStart())
	require.NoError(t, err)
	assert.Equal(t, frigateRollout, rollout)
	assert.Equal(t, validStart(), device.start)

	status, err := relay.FrigateUpdateStatus(ctx, testFrigateJob)
	require.NoError(t, err)
	assert.True(t, status.ServedByMaintenance)
	assert.Equal(t, testFrigateJob, device.jobID)

	_, err = relay.FrigateUpdateRetry(ctx, validAction())
	require.NoError(t, err)
	assert.Equal(t, "retry", device.method)
	_, err = relay.FrigateUpdateCancel(ctx, validAction())
	require.NoError(t, err)
	assert.Equal(t, "cancel", device.method)

	// Package bytes never cross as a call, on either side.
	_, err = relay.FrigateStageAppend(ctx, "a", validAppend())
	require.ErrorIs(t, err, ErrFrigateRelayAppend)
	assert.Equal(t, "cancel", device.method, "the append did not reach the controller")
	a := validAppend()
	request, err := proto.Marshal(&pb.FrigateServerUpdateRequest{Operation: &pb.FrigateServerUpdateRequest_StageAppend{
		StageAppend: &pb.FrigateStageAppendRequest{InstanceId: "a", JobId: a.JobID, ManifestSha256: a.ManifestSHA256, Chunk: a.Chunk},
	}})
	require.NoError(t, err)
	_, err = ServeFrigateRelay(ctx, device, request)
	require.ErrorIs(t, err, ErrFrigateRelayAppend)
	assert.Equal(t, "cancel", device.method)
}

func validProtoOSManifest() FrigateManifest {
	m := validManifest()
	m.Package, m.ReleaseTag, m.AssetName, m.TargetVersion = FrigatePackageProtoOS, "v0.6.2-internal.14", "proto-os_v0.6.2-internal.14_all.deb", "0.6.2~internal.14"
	return m
}

func TestFrigateProtoOSFieldsCrossThePluginAndTheRelay(t *testing.T) {
	supports := []FrigateInstanceSupport{
		{
			InstanceID: "a", PeerInstanceID: "b", RemoteTriggerAllowed: true,
			ProtoOS: FrigateProtoOSSupport{
				SelfSupported: true, InstalledVersion: "0.6.1", ReleaseRepositories: []string{"btc-mining/proto-fleet-internal", "block/proto-fleet"},
				Served: FrigateProtoOSBundle{Source: FrigateProtoOSBundlePackage, ReleaseTag: "v0.6.1"},
				Job: FrigateProtoOSJob{
					JobID: testFrigateJob, ReleaseTag: "v0.6.2-internal.14", TargetVersion: "0.6.2~internal.14",
					State: FrigateProtoOSJobFailed, Message: "downgrade refused", UpdatedAt: frigateRoundTripTime,
				},
			},
		},
		{
			InstanceID: "b", PeerInstanceID: "a",
			ProtoOS: FrigateProtoOSSupport{SupportMessage: "the ProtoOS web UI is disabled", Served: FrigateProtoOSBundle{Source: FrigateProtoOSBundleNone}},
		},
	}
	for name, connect := range map[string]func(*frigateUpdaterDevice) DeviceFrigateUpdater{
		"plugin": func(d *frigateUpdaterDevice) DeviceFrigateUpdater { return outcomeRPCDevice(t, d) },
		"relay":  func(d *frigateUpdaterDevice) DeviceFrigateUpdater { return relayTo(d) },
	} {
		t.Run(name, func(t *testing.T) {
			device := &frigateUpdaterDevice{supports: supports}
			updater := connect(device)

			got, err := updater.FrigateUpdateSupport(t.Context())
			require.NoError(t, err)
			assert.Equal(t, supports, got)

			_, err = updater.FrigateStageBegin(t.Context(), "a", validProtoOSManifest())
			require.NoError(t, err)
			assert.Equal(t, validProtoOSManifest(), device.manifest)
		})
	}

	t.Run("relayed copy", func(t *testing.T) {
		blob := []byte("proto-os package")
		sum := sha256.Sum256(blob)
		manifest := validProtoOSManifest()
		manifest.SHA256, manifest.SizeBytes = hex.EncodeToString(sum[:]), int64(len(blob))
		begin, err := EncodeFrigateRelayStageBegin("a", manifest)
		require.NoError(t, err)
		c := &stagingController{fingerprint: testFrigateFP}
		_, err = ServeFrigateRelayStage(t.Context(), c, begin, testFrigateFP, FrigateRelayPackage{Data: bytes.NewReader(blob), SizeBytes: int64(len(blob))})
		require.NoError(t, err)
		assert.Equal(t, manifest, c.manifest)
	})
}

func TestServeFrigateRelayKeepsARefusalAndRejectsBadRequests(t *testing.T) {
	refusal := &FrigateRefusal{Reason: "UPDATE_NOT_FOUND", Message: "unknown rollout", Code: codes.NotFound}
	_, err := relayTo(&frigateUpdaterDevice{err: refusal}).FrigateUpdateStatus(t.Context(), testFrigateJob)
	got := AsFrigateRefusal(err)
	require.NotNil(t, got)
	assert.Equal(t, *refusal, *got)

	device := &frigateUpdaterDevice{}
	for name, request := range map[string][]byte{
		"empty":        nil,
		"too large":    make([]byte, FrigateRelayRequestMaxBytes+1),
		"not protobuf": []byte("\xff\xff\xff"),
		"no operation": {},
	} {
		_, err := ServeFrigateRelay(t.Context(), device, request)
		require.Error(t, err, name)
		assert.Empty(t, device.method, name)
	}
}

func TestFrigateRelayReadOnly(t *testing.T) {
	encode := func(req *pb.FrigateServerUpdateRequest) []byte {
		data, err := proto.Marshal(req)
		require.NoError(t, err)
		return data
	}
	begin, err := EncodeFrigateRelayStageBegin("a", validManifest())
	require.NoError(t, err)

	assert.True(t, FrigateRelayReadOnly(encode(&pb.FrigateServerUpdateRequest{Operation: &pb.FrigateServerUpdateRequest_Support{Support: &pb.FrigateUpdateSupportRequest{}}})))
	assert.True(t, FrigateRelayReadOnly(encode(&pb.FrigateServerUpdateRequest{Operation: &pb.FrigateServerUpdateRequest_Status{Status: &pb.FrigateUpdateStatusRequest{JobId: testFrigateJob}}})))
	assert.False(t, FrigateRelayReadOnly(begin))
	assert.False(t, FrigateRelayReadOnly(encode(&pb.FrigateServerUpdateRequest{Operation: &pb.FrigateServerUpdateRequest_Cancel{Cancel: &pb.FrigateUpdateCancelRequest{JobId: testFrigateJob}}})))
	assert.False(t, FrigateRelayReadOnly([]byte("\xff\xff\xff")), "a request that does not decode counts as a write")
	assert.False(t, FrigateRelayReadOnly(nil))
}

// stagingController is a controller that keeps the bytes it was sent.
type stagingController struct {
	frigateUpdaterDevice
	fingerprint string
	data        []byte
	appends     []int64
	appendErr   error
	// ackShort makes the controller acknowledge one byte less than it took.
	ackShort bool
}

func (c *stagingController) FrigateStageBegin(_ context.Context, _ string, m FrigateManifest) (FrigateStageProgress, error) {
	c.manifest = m
	return FrigateStageProgress{JobID: m.JobID, ManifestSHA256: c.fingerprint, NextOffset: int64(len(c.data)), SizeBytes: m.SizeBytes}, nil
}

func (c *stagingController) FrigateStageAppend(_ context.Context, _ string, a FrigateStageAppend) (FrigateStageProgress, error) {
	if c.appendErr != nil {
		return FrigateStageProgress{}, c.appendErr
	}
	c.appends = append(c.appends, a.Offset)
	c.data = append(c.data, a.Chunk...)
	next := int64(len(c.data))
	if c.ackShort {
		next--
	}
	return FrigateStageProgress{JobID: a.JobID, ManifestSHA256: a.ManifestSHA256, NextOffset: next}, nil
}

func (c *stagingController) FrigateStageCommit(_ context.Context, _ string, r FrigateStageRef) (FrigateStageCommit, error) {
	sum := sha256.Sum256(c.data)
	return FrigateStageCommit{JobID: r.JobID, BytesStaged: int64(len(c.data)), SHA256: hex.EncodeToString(sum[:])}, nil
}

func TestServeFrigateRelayStage(t *testing.T) {
	blob := make([]byte, 5<<19) // 2.5 MiB: three chunks.
	_, err := rand.Read(blob)
	require.NoError(t, err)
	sum := sha256.Sum256(blob)
	digest := hex.EncodeToString(sum[:])
	manifest := validManifest()
	manifest.SHA256, manifest.SizeBytes, manifest.Actor = digest, int64(len(blob)), "alice"
	begin, err := EncodeFrigateRelayStageBegin("a", manifest)
	require.NoError(t, err)
	pkg := FrigateRelayPackage{Data: bytes.NewReader(blob), SizeBytes: int64(len(blob))}
	stage := func(c *stagingController, request []byte, fingerprint string, pkg FrigateRelayPackage) (FrigateStageCommit, error) {
		response, err := ServeFrigateRelayStage(t.Context(), c, request, fingerprint, pkg)
		if err != nil {
			return FrigateStageCommit{}, err
		}
		return DecodeFrigateRelayStageCommit(response)
	}

	t.Run("copies the whole package in chunks and commits", func(t *testing.T) {
		c := &stagingController{fingerprint: testFrigateFP}
		commit, err := stage(c, begin, testFrigateFP, pkg)
		require.NoError(t, err)
		assert.Equal(t, FrigateStageCommit{JobID: testFrigateJob, BytesStaged: int64(len(blob)), SHA256: digest}, commit)
		assert.Equal(t, []int64{0, 1 << 20, 2 << 20}, c.appends)
		assert.Equal(t, blob, c.data)
	})

	t.Run("sends only the bytes the controller does not have", func(t *testing.T) {
		c := &stagingController{fingerprint: testFrigateFP, data: bytes.Clone(blob[:1<<20+7])}
		commit, err := stage(c, begin, testFrigateFP, pkg)
		require.NoError(t, err)
		assert.Equal(t, digest, commit.SHA256)
		assert.Equal(t, []int64{1<<20 + 7, 2<<20 + 7}, c.appends)

		// A copy that is already complete is only committed.
		c.appends = nil
		_, err = stage(c, begin, testFrigateFP, pkg)
		require.NoError(t, err)
		assert.Empty(t, c.appends)
	})

	t.Run("sends nothing to a controller that holds another copy", func(t *testing.T) {
		c := &stagingController{fingerprint: testFrigateFPB}
		_, err := stage(c, begin, testFrigateFP, pkg)
		require.ErrorContains(t, err, "different manifest fingerprint")
		assert.Nil(t, AsFrigateRefusal(err), "the caller can begin again and retry")
		assert.Empty(t, c.appends)
	})

	t.Run("keeps a refusal from the controller", func(t *testing.T) {
		c := &stagingController{fingerprint: testFrigateFP, appendErr: &FrigateRefusal{Reason: "UPDATE_INSUFFICIENT_SPACE", Code: codes.ResourceExhausted}}
		_, err := stage(c, begin, testFrigateFP, pkg)
		refusal := AsFrigateRefusal(err)
		require.NotNil(t, refusal)
		assert.Equal(t, "UPDATE_INSUFFICIENT_SPACE", refusal.Reason)
	})

	t.Run("stops when the controller acknowledges an unexpected offset", func(t *testing.T) {
		c := &stagingController{fingerprint: testFrigateFP, ackShort: true}
		_, err := stage(c, begin, testFrigateFP, pkg)
		require.ErrorContains(t, err, "acknowledged offset")
		assert.Len(t, c.appends, 1)
	})

	t.Run("the plugin client rejects a begin for another instance before any call", func(t *testing.T) {
		wrong, err := proto.Marshal(&pb.FrigateServerUpdateRequest{Operation: &pb.FrigateServerUpdateRequest_StageBegin{
			StageBegin: &pb.FrigateStageBeginRequest{InstanceId: "b", Manifest: frigateManifestToProto(manifest)},
		}})
		require.NoError(t, err)
		calls := 0
		client := frigateCallFunc(func(context.Context, *pb.FrigateServerUpdateRequest) (*pb.FrigateServerUpdateResponse, error) {
			calls++
			return nil, errors.New("unexpected call")
		})
		_, err = ServeFrigateRelayStage(t.Context(), client, wrong, testFrigateFP, pkg)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
		assert.Zero(t, calls)
	})

	t.Run("rejects a request it cannot copy from", func(t *testing.T) {
		status, err := proto.Marshal(&pb.FrigateServerUpdateRequest{Operation: &pb.FrigateServerUpdateRequest_Status{Status: &pb.FrigateUpdateStatusRequest{}}})
		require.NoError(t, err)
		for name, tc := range map[string]struct {
			request     []byte
			fingerprint string
			pkg         FrigateRelayPackage
		}{
			"not a stage begin":      {status, testFrigateFP, pkg},
			"bad fingerprint":        {begin, "nope", pkg},
			"package size differs":   {begin, testFrigateFP, FrigateRelayPackage{Data: bytes.NewReader(blob[:10]), SizeBytes: 10}},
			"no package":             {begin, testFrigateFP, FrigateRelayPackage{SizeBytes: int64(len(blob))}},
			"request does not parse": {[]byte("\xff\xff\xff"), testFrigateFP, pkg},
		} {
			c := &stagingController{fingerprint: testFrigateFP}
			_, err := stage(c, tc.request, tc.fingerprint, tc.pkg)
			require.Error(t, err, name)
			assert.Empty(t, c.appends, name)
		}
	})
}

func TestCopyFrigateStageReportsEachChunkAndStopsWhenAsked(t *testing.T) {
	blob := make([]byte, 5<<19) // 2.5 MiB: three chunks.
	copyTo := func(c *stagingController, onChunk func(int64) error) (FrigateStageCommit, int64, error) {
		return CopyFrigateStage(t.Context(), c, FrigateStageCopy{
			InstanceID: "a", Ref: validRef(), Package: bytes.NewReader(blob), SizeBytes: int64(len(blob)), Offset: int64(len(c.data)),
			ChunkTimeout: time.Minute, CallTimeout: time.Minute, OnChunk: onChunk,
		})
	}

	var seen []int64
	commit, _, err := copyTo(&stagingController{}, func(next int64) error { seen = append(seen, next); return nil })
	require.NoError(t, err)
	assert.Equal(t, int64(len(blob)), commit.BytesStaged)
	assert.Equal(t, []int64{1 << 20, 2 << 20, int64(len(blob))}, seen)

	stop := errors.New("the job left staging")
	c := &stagingController{}
	_, staged, err := copyTo(c, func(next int64) error {
		if next == 2<<20 {
			return stop
		}
		return nil
	})
	assert.Same(t, stop, err, "the stop comes back unchanged")
	assert.Equal(t, int64(2<<20), staged)
	assert.Len(t, c.appends, 2, "no chunk follows the stop")

	c = &stagingController{data: bytes.Clone(blob[:7]), ackShort: true}
	_, staged, err = copyTo(c, nil)
	require.ErrorContains(t, err, "acknowledged offset")
	assert.Equal(t, int64(7), staged, "a failed chunk reports the last acknowledged offset")
}

func TestEncodeFrigateRelayStageBeginBoundsTheRequest(t *testing.T) {
	m := validManifest()
	m.ReleaseStatement = make([]byte, FrigateRelayRequestMaxBytes)
	_, err := EncodeFrigateRelayStageBegin("a", m)
	require.ErrorContains(t, err, "a relay carries at most")

	_, err = DecodeFrigateRelayStageCommit([]byte("\xff\xff\xff"))
	require.Error(t, err)
	progress, err := encodeFrigateRelayResponse(frigateProgressResponse(FrigateStageProgress{JobID: testFrigateJob}))
	require.NoError(t, err)
	_, err = DecodeFrigateRelayStageCommit(progress)
	require.ErrorIs(t, err, errFrigateUnexpectedResponse)
}
