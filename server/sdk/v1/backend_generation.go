package sdk

import (
	"bytes"
	"context"
	"fmt"
	"sync"

	pb "github.com/block/proto-fleet/server/sdk/v1/pb/generated"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const backendGenerationMismatch = "SDK_BACKEND_GENERATION_MISMATCH"

func backendGenerationRejected() error {
	rejection, err := status.New(codes.FailedPrecondition, "SDK backend registration changed").WithDetails(&errdetails.ErrorInfo{Reason: backendGenerationMismatch, Domain: "protofleet.sdk"})
	if err != nil {
		panic(err)
	}
	return fmt.Errorf("select SDK backend: %w", &backendGenerationError{err: rejection.Err()})
}

type backendGenerationError struct{ err error }

func (e *backendGenerationError) Error() string { return e.err.Error() }
func (e *backendGenerationError) Unwrap() error { return e.err }

// IsBackendGenerationRejected identifies the SDK's pre-backend admission failure.
// Arbitrary driver FailedPrecondition errors do not establish a known outcome.
func IsBackendGenerationRejected(err error) bool {
	s, ok := status.FromError(err)
	if !ok || s.Code() != codes.FailedPrecondition {
		return false
	}
	for _, detail := range s.Details() {
		if info, ok := detail.(*errdetails.ErrorInfo); ok && info.Domain == "protofleet.sdk" && info.Reason == backendGenerationMismatch {
			return true
		}
	}
	return false
}

// selectDevice validates and captures the concrete backend under the same lock.
// Later registry replacements cannot redirect an admitted call to another object.
func (s *DriverGRPCServer) selectDevice(ref *pb.DeviceRef) (Device, error) {
	if ref == nil || ref.DeviceId == "" {
		return nil, sdkErrorToGRPCStatus(InvalidWriteRequest(grpcStatusError("select SDK backend", codes.InvalidArgument, "device reference is required")))
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	device, exists := s.devices[ref.DeviceId]
	if !exists {
		if len(ref.BackendGeneration) != 0 {
			return nil, WriteRefused(backendGenerationRejected(), WriteStageBackendSelection, WriteReasonStaleBinding)
		}
		return nil, writeRefused(NewErrorDeviceNotFound(ref.DeviceId), WriteStageBackendSelection, WriteReasonDeviceNotFound)
	}
	if !bytes.Equal(s.generations[ref.DeviceId], ref.BackendGeneration) {
		return nil, WriteRefused(backendGenerationRejected(), WriteStageBackendSelection, WriteReasonStaleBinding)
	}
	return device, nil
}

func (d *DeviceGRPCClient) deviceRef() *pb.DeviceRef {
	return &pb.DeviceRef{DeviceId: d.deviceID, BackendGeneration: d.backendGeneration}
}

// References include waiters, so cancellation never removes a lock still in use.
// The entry is removed when its last holder/waiter leaves, bounding idle memory.
type deviceIDLocks struct {
	mu      sync.Mutex
	entries map[string]*deviceIDLock
}
type deviceIDLock struct {
	token chan struct{}
	refs  int
}

func (locks *deviceIDLocks) lock(ctx context.Context, id string) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("acquire device lifecycle lock: %w", err)
	}
	locks.mu.Lock()
	if locks.entries == nil {
		locks.entries = make(map[string]*deviceIDLock)
	}
	entry := locks.entries[id]
	if entry == nil {
		entry = &deviceIDLock{token: make(chan struct{}, 1)}
		entry.token <- struct{}{}
		locks.entries[id] = entry
	}
	entry.refs++
	locks.mu.Unlock()
	releaseRef := func() {
		locks.mu.Lock()
		entry.refs--
		if entry.refs == 0 {
			delete(locks.entries, id)
		}
		locks.mu.Unlock()
	}
	select {
	case <-ctx.Done():
		releaseRef()
		return nil, fmt.Errorf("wait for device lifecycle lock: %w", ctx.Err())
	case <-entry.token:
		if err := ctx.Err(); err != nil {
			entry.token <- struct{}{}
			releaseRef()
			return nil, fmt.Errorf("acquire device lifecycle lock: %w", err)
		}
		return func() { entry.token <- struct{}{}; releaseRef() }, nil
	}
}
