package sdk

import (
	"errors"

	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

const (
	WriteNotDispatched = "DEVICE_WRITE_NOT_DISPATCHED"
	WriteCompleted     = "DEVICE_WRITE_COMPLETED"
	WriteUnknown       = "DEVICE_WRITE_UNKNOWN"
	writeOutcomeDomain = "protofleet.sdk.write"
)

type WriteStage string

const (
	WriteStageValidation         WriteStage = "validation"
	WriteStageCapability         WriteStage = "capability"
	WriteStageBackendSelection   WriteStage = "backend_selection"
	WriteStageCredentials        WriteStage = "credentials"
	WriteStageConfigurationRead  WriteStage = "configuration_read"
	WriteStageConfigurationWrite WriteStage = "configuration_write"
	WriteStageAuthentication     WriteStage = "authentication"
	WriteStageDispatch           WriteStage = "dispatch"
)

type WriteReason string

const (
	WriteReasonUnsupported          WriteReason = "unsupported"
	WriteReasonInvalidRequest       WriteReason = "invalid_request"
	WriteReasonStaleBinding         WriteReason = "stale_binding"
	WriteReasonDeviceNotFound       WriteReason = "device_not_found"
	WriteReasonMissingCredentials   WriteReason = "missing_credentials"
	WriteReasonReadFailed           WriteReason = "read_failed"
	WriteReasonAuthenticationFailed WriteReason = "authentication_failed"
	WriteReasonTransportFailed      WriteReason = "transport_failed"
	WriteReasonRejected             WriteReason = "rejected"
	WriteReasonFinished             WriteReason = "finished"
	WriteReasonUnknown              WriteReason = "unknown"
)

// WithWriteOutcome carries authoritative execution evidence, independent of the
// error code. NOT_DISPATCHED means no mutation began and none can begin later.
// COMPLETED means all work ended, including asynchronous downstream work.
// UNKNOWN includes accepted background work and writes with lost responses.
// Never classify a timeout or an arbitrary firmware error code as completion.
// Stage and reason are fixed diagnostic identifiers, never device payloads.
func WithWriteOutcome(err error, outcome string, stage WriteStage, reason WriteReason) error {
	return WithWriteEvidence(err, NewWriteEvidence(outcome, stage, reason))
}

// WriteRefused marks a failure before any mutation can begin and retains its cause.
func WriteRefused(err error, stage WriteStage, reason WriteReason) error {
	return WithWriteOutcome(err, WriteNotDispatched, stage, reason)
}

// UnsupportedWrite rejects a capability before its device operation begins.
func UnsupportedWrite(capability string) error {
	return WriteRefused(NewErrUnsupportedCapability(capability), WriteStageCapability, WriteReasonUnsupported)
}

// InvalidWriteRequest retains a validation failure before device execution.
func InvalidWriteRequest(err error) error {
	return WriteRefused(err, WriteStageValidation, WriteReasonInvalidRequest)
}

// NewWriteEvidence builds the bounded wire and diagnostic representation.
func NewWriteEvidence(outcome string, stage WriteStage, reason WriteReason) *errdetails.ErrorInfo {
	info := &errdetails.ErrorInfo{Domain: writeOutcomeDomain, Reason: outcome, Metadata: map[string]string{"stage": string(stage), "reason": string(reason)}}
	if !validWriteEvidence(info) {
		return nil
	}
	return info
}

// WithWriteEvidence preserves validated evidence when an adapter maps an error.
func WithWriteEvidence(err error, evidence *errdetails.ErrorInfo) error {
	if err == nil || !validWriteEvidence(evidence) {
		return err
	}
	return &writeOutcomeError{err: err, evidence: proto.CloneOf(evidence)}
}

type writeOutcomeError struct {
	err      error
	evidence *errdetails.ErrorInfo
}

func (e *writeOutcomeError) Error() string { return e.err.Error() }
func (e *writeOutcomeError) Unwrap() error { return e.err }
func (e *writeOutcomeError) GRPCStatus() *status.Status {
	mapped := sdkErrorStatus(e.err)
	base, ok := status.FromError(mapped)
	if !ok {
		base = status.FromContextError(mapped)
	}
	wire := base.Proto()
	details := wire.Details[:0]
	for _, detail := range wire.Details {
		var info errdetails.ErrorInfo
		if detail.UnmarshalTo(&info) == nil && info.Domain == writeOutcomeDomain {
			continue
		}
		details = append(details, detail)
	}
	wire.Details = details
	withEvidence, err := status.FromProto(wire).WithDetails(e.evidence)
	if err != nil {
		return base
	}
	return withEvidence
}

func localWriteEvidence(err error) *errdetails.ErrorInfo {
	var local *writeOutcomeError
	if errors.As(err, &local) {
		return proto.CloneOf(local.evidence)
	}
	return nil
}

// SDK-owned receipts must originate in this process. A controller's gRPC
// status may use the same detail names without proving SDK execution state.
func sdkServerError(err error) error {
	if err == nil {
		return nil
	}
	var localOutcome *writeOutcomeError
	var localGeneration *backendGenerationError
	var localProbe *deviceProbeError
	errors.As(err, &localOutcome)
	errors.As(err, &localGeneration)
	errors.As(err, &localProbe)
	base, ok := status.FromError(err)
	if !ok {
		if localProbe == nil {
			return err
		}
		base = status.FromContextError(err)
	}
	wire := base.Proto()
	details := wire.Details[:0]
	filtered := false
	for _, detail := range wire.Details {
		var info errdetails.ErrorInfo
		if detail.UnmarshalTo(&info) == nil && (info.Domain == writeOutcomeDomain ||
			(info.Domain == "protofleet.sdk" && info.Reason == backendGenerationMismatch) ||
			(info.Domain == deviceProbeDomain && info.Reason == deviceProbeReason)) {
			filtered = true
			continue
		}
		details = append(details, detail)
	}
	if !filtered && localProbe == nil {
		return err
	}
	wire.Details = details
	filteredStatus := status.FromProto(wire)
	if localOutcome != nil {
		if withEvidence, addErr := filteredStatus.WithDetails(localOutcome.evidence); addErr == nil {
			filteredStatus = withEvidence
		}
	}
	if localGeneration != nil {
		if withGeneration, addErr := filteredStatus.WithDetails(&errdetails.ErrorInfo{Domain: "protofleet.sdk", Reason: backendGenerationMismatch}); addErr == nil {
			filteredStatus = withGeneration
		}
	}
	if localProbe != nil {
		if withProbe, addErr := filteredStatus.WithDetails(&errdetails.ErrorInfo{Domain: deviceProbeDomain, Reason: deviceProbeReason}); addErr == nil {
			filteredStatus = withProbe
		}
	}
	return &sdkFilteredError{err: err, status: filteredStatus}
}

type sdkFilteredError struct {
	err    error
	status *status.Status
}

func (e *sdkFilteredError) Error() string              { return e.err.Error() }
func (e *sdkFilteredError) Unwrap() error              { return e.err }
func (e *sdkFilteredError) GRPCStatus() *status.Status { return e.status }

// WriteEvidence accepts only the SDK's structured contract. Codes, message
// text, unknown versions, and extra metadata do not certify execution outcomes.
func WriteEvidence(err error) *errdetails.ErrorInfo {
	if err == nil {
		return nil
	}
	if local := localWriteEvidence(err); local != nil {
		return local
	}
	s, ok := status.FromError(err)
	if !ok {
		return nil
	}
	var found *errdetails.ErrorInfo
	for _, detail := range s.Details() {
		info, ok := detail.(*errdetails.ErrorInfo)
		if !ok || info.Domain != writeOutcomeDomain {
			continue
		}
		if found != nil || !validWriteEvidence(info) {
			return nil
		}
		found = info
	}
	return proto.CloneOf(found)
}

func validWriteEvidence(info *errdetails.ErrorInfo) bool {
	if info == nil || info.Domain != writeOutcomeDomain || len(info.Metadata) != 2 {
		return false
	}
	switch info.Reason {
	case WriteNotDispatched, WriteCompleted, WriteUnknown:
	default:
		return false
	}
	switch WriteStage(info.Metadata["stage"]) {
	case WriteStageValidation, WriteStageCapability, WriteStageBackendSelection, WriteStageCredentials, WriteStageConfigurationRead, WriteStageConfigurationWrite, WriteStageAuthentication, WriteStageDispatch:
	default:
		return false
	}
	switch WriteReason(info.Metadata["reason"]) {
	case WriteReasonUnsupported, WriteReasonInvalidRequest, WriteReasonStaleBinding, WriteReasonDeviceNotFound, WriteReasonMissingCredentials, WriteReasonReadFailed, WriteReasonAuthenticationFailed, WriteReasonTransportFailed, WriteReasonRejected, WriteReasonFinished, WriteReasonUnknown:
		return true
	default:
		return false
	}
}

func writeRefused(err error, stage WriteStage, reason WriteReason) error {
	return sdkErrorToGRPCStatus(WriteRefused(err, stage, reason))
}
