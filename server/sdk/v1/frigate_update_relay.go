package sdk

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"google.golang.org/protobuf/proto"

	pb "github.com/block/proto-fleet/server/sdk/v1/pb/generated"
)

// Controller software updates through a relay.
//
// Fleet reaches some controllers through a Fleet Node. Each update call then
// crosses the node as the bytes of the plugin's update envelope
// (FrigateServerUpdateRequest and FrigateServerUpdateResponse), and the node
// runs it on the plugin next to the controller. The relay therefore needs no
// update schema of its own.
//
// The envelope is thus also part of the Fleet Node gateway contract. Before
// Fleet sends a new operation, or a new request field that a node must obey,
// the gateway needs a new command protocol version (see FrigateUpdateAction).
//
// Package bytes do not cross as calls. The far side gets the package by its own
// means and copies it to each controller with ServeFrigateRelayStage.

const (
	// FrigateRelayRequestMaxBytes bounds one encoded request. The largest is a
	// stage begin, which carries the release statement and its signature.
	FrigateRelayRequestMaxBytes = 64 << 10
	// FrigateRelayResponseMaxBytes bounds one encoded response.
	FrigateRelayResponseMaxBytes = 1 << 20

	// The fixed limits for one controller call during a relayed copy. Fleet's
	// call and chunk timeout settings do not apply on the far side.
	frigateRelayCallTimeout  = 30 * time.Second
	frigateRelayChunkTimeout = 60 * time.Second
)

var (
	// ErrFrigateRelayAppend is returned for a stage append through a relay.
	ErrFrigateRelayAppend = errors.New("frigate update: package bytes do not cross a relay; copy the package with a relayed stage")
	// ErrFrigateRelayRequest is returned for a request that the far side cannot
	// read. No controller call was made.
	ErrFrigateRelayRequest = errors.New("frigate update: invalid relayed request")
)

// FrigateRelaySend carries one encoded request to the far side and returns the
// encoded response. A refusal from the controller comes back as *FrigateRefusal.
type FrigateRelaySend func(ctx context.Context, request []byte) ([]byte, error)

// FrigateRelay is the update surface of a controller that is reached through a
// relay. It makes every DeviceFrigateUpdater call except FrigateStageAppend.
type FrigateRelay struct {
	frigateCallFunc
}

var _ DeviceFrigateUpdater = FrigateRelay{}

// NewFrigateRelay returns an update surface that sends each call with send.
func NewFrigateRelay(send FrigateRelaySend) FrigateRelay {
	return FrigateRelay{frigateCallFunc: func(ctx context.Context, req *pb.FrigateServerUpdateRequest) (*pb.FrigateServerUpdateResponse, error) {
		request, err := encodeFrigateRelayRequest(req)
		if err != nil {
			return nil, err
		}
		response, err := send(ctx, request)
		if err != nil {
			return nil, err
		}
		return decodeFrigateRelayResponse(response)
	}}
}

// FrigateStageAppend is refused: see ErrFrigateRelayAppend.
func (FrigateRelay) FrigateStageAppend(context.Context, string, FrigateStageAppend) (FrigateStageProgress, error) {
	return FrigateStageProgress{}, ErrFrigateRelayAppend
}

func encodeFrigateRelayRequest(req *pb.FrigateServerUpdateRequest) ([]byte, error) {
	request, err := proto.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("encode frigate update request: %w", err)
	}
	if len(request) > FrigateRelayRequestMaxBytes {
		return nil, fmt.Errorf("frigate update request is %d bytes; a relay carries at most %d", len(request), FrigateRelayRequestMaxBytes)
	}
	return request, nil
}

func decodeFrigateRelayRequest(request []byte) (*pb.FrigateServerUpdateRequest, error) {
	if len(request) == 0 || len(request) > FrigateRelayRequestMaxBytes {
		return nil, InvalidWriteRequest(fmt.Errorf("%w: it must be between 1 and %d bytes", ErrFrigateRelayRequest, FrigateRelayRequestMaxBytes))
	}
	var req pb.FrigateServerUpdateRequest
	if err := proto.Unmarshal(request, &req); err != nil {
		return nil, InvalidWriteRequest(fmt.Errorf("%w: %w", ErrFrigateRelayRequest, err))
	}
	if req.GetOperation() == nil {
		return nil, InvalidWriteRequest(fmt.Errorf("%w: it has no operation", ErrFrigateRelayRequest))
	}
	return &req, nil
}

func checkFrigateRelayResponseSize(response []byte) error {
	if len(response) > FrigateRelayResponseMaxBytes {
		return fmt.Errorf("frigate update response is %d bytes; a relay carries at most %d", len(response), FrigateRelayResponseMaxBytes)
	}
	return nil
}

func decodeFrigateRelayResponse(response []byte) (*pb.FrigateServerUpdateResponse, error) {
	if err := checkFrigateRelayResponseSize(response); err != nil {
		return nil, err
	}
	var resp pb.FrigateServerUpdateResponse
	if err := proto.Unmarshal(response, &resp); err != nil {
		return nil, fmt.Errorf("decode frigate update response: %w", err)
	}
	return &resp, nil
}

// FrigateRelayReadOnly reports whether an encoded request only reads: a support
// or a status call. A request that does not decode counts as a write.
func FrigateRelayReadOnly(request []byte) bool {
	req, err := decodeFrigateRelayRequest(request)
	if err != nil {
		return false
	}
	switch req.GetOperation().(type) {
	case *pb.FrigateServerUpdateRequest_Support, *pb.FrigateServerUpdateRequest_Status:
		return true
	default:
		return false
	}
}

// ServeFrigateRelay runs one relayed call on u and returns the encoded
// response. A stage append is refused with ErrFrigateRelayAppend. Other errors
// come from u unchanged, so a refusal stays a *FrigateRefusal.
func ServeFrigateRelay(ctx context.Context, u DeviceFrigateUpdater, request []byte) ([]byte, error) {
	req, err := decodeFrigateRelayRequest(request)
	if err != nil {
		return nil, err
	}
	if req.GetStageAppend() != nil {
		return nil, InvalidWriteRequest(fmt.Errorf("%w: %w", ErrFrigateRelayRequest, ErrFrigateRelayAppend))
	}
	resp, err := dispatchFrigateUpdate(ctx, u, req)
	if err != nil {
		return nil, err
	}
	return encodeFrigateRelayResponse(resp)
}

func encodeFrigateRelayResponse(resp *pb.FrigateServerUpdateResponse) ([]byte, error) {
	response, err := proto.Marshal(resp)
	if err != nil {
		return nil, fmt.Errorf("encode frigate update response: %w", err)
	}
	if err := checkFrigateRelayResponseSize(response); err != nil {
		return nil, err
	}
	return response, nil
}

// EncodeFrigateRelayStageBegin encodes the stage-begin call for one controller
// of a relayed copy. It does not validate the call: the caller begins the copy
// through the relay first, and that Begin validates the same instance ID and
// manifest.
func EncodeFrigateRelayStageBegin(instanceID string, m FrigateManifest) ([]byte, error) {
	return encodeFrigateRelayRequest(&pb.FrigateServerUpdateRequest{Operation: &pb.FrigateServerUpdateRequest_StageBegin{
		StageBegin: &pb.FrigateStageBeginRequest{InstanceId: instanceID, Manifest: frigateManifestToProto(m)},
	}})
}

// DecodeFrigateRelayStageCommit decodes the response of a relayed copy to one
// controller.
func DecodeFrigateRelayStageCommit(response []byte) (FrigateStageCommit, error) {
	resp, err := decodeFrigateRelayResponse(response)
	if err != nil {
		return FrigateStageCommit{}, err
	}
	return frigateCommitFromResponse(resp)
}

// FrigateRelayPackage is the package that the far side of a relay copies.
type FrigateRelayPackage struct {
	Data      io.ReaderAt
	SizeBytes int64
}

// ServeFrigateRelayStage copies pkg to the controller that beginRequest names,
// and returns the encoded commit response. It begins the copy, appends the
// bytes that the controller does not have yet, and commits. u validates the
// instance ID and the manifest, as the plugin client does.
//
// manifestSHA256 is the fingerprint that the caller recorded when the copy
// began. If the controller reports a different one, it holds another copy for
// this job, and no bytes are sent. Errors come from u unchanged, so a refusal
// stays a *FrigateRefusal.
func ServeFrigateRelayStage(ctx context.Context, u DeviceFrigateUpdater, beginRequest []byte, manifestSHA256 string, pkg FrigateRelayPackage) ([]byte, error) {
	req, err := decodeFrigateRelayRequest(beginRequest)
	if err != nil {
		return nil, err
	}
	begin := req.GetStageBegin()
	if begin == nil {
		return nil, InvalidWriteRequest(errors.New("a relayed copy needs a stage-begin request"))
	}
	instanceID := begin.GetInstanceId()
	manifest := frigateManifestFromProto(begin.GetManifest())
	if pkg.Data == nil || pkg.SizeBytes != manifest.SizeBytes {
		return nil, InvalidWriteRequest(fmt.Errorf("the package is %d bytes but the manifest says %d", pkg.SizeBytes, manifest.SizeBytes))
	}

	callCtx, cancel := context.WithTimeout(ctx, frigateRelayCallTimeout)
	progress, err := u.FrigateStageBegin(callCtx, instanceID, manifest)
	cancel()
	if err != nil {
		return nil, err
	}
	if progress.ManifestSHA256 != manifestSHA256 {
		return nil, fmt.Errorf("controller %s holds a copy with a different manifest fingerprint", instanceID)
	}
	if progress.NextOffset < 0 || progress.NextOffset > pkg.SizeBytes {
		return nil, fmt.Errorf("controller %s reported staged offset %d outside the package", instanceID, progress.NextOffset)
	}

	commit, _, err := CopyFrigateStage(ctx, u, FrigateStageCopy{
		InstanceID: instanceID,
		Ref:        FrigateStageRef{JobID: manifest.JobID, ManifestSHA256: manifestSHA256, Actor: manifest.Actor},
		Package:    pkg.Data, SizeBytes: pkg.SizeBytes, Offset: progress.NextOffset,
		ChunkTimeout: frigateRelayChunkTimeout, CallTimeout: frigateRelayCallTimeout,
	})
	if err != nil {
		return nil, err
	}
	return encodeFrigateRelayResponse(frigateCommitResponse(commit))
}
