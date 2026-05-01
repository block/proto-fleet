package fleeterror

import (
	"context"
	"errors"
	"fmt"

	"connectrpc.com/connect"
	commonv1 "github.com/block/proto-fleet/server/generated/grpc/common/v1"
)

// FleetError represents a custom error type that can be converted to a gRPC ConnectError
type FleetError struct {
	DebugMessage       string
	GRPCCode           connect.Code
	FleetErrorCode     int32
	FleetErrorCodeType FleetErrorCodeType
	StackTrace         StackTrace
}

// FleetErrorCodeType represents the type of error code being used
//
//goland:noinspection GoNameStartsWithPackageName
type FleetErrorCodeType int

// Error code type constants
const (
	// ErrorCodeTypeCommon represents common error codes shared across services
	ErrorCodeTypeCommon FleetErrorCodeType = 0
	// ErrorCodeTypeService represents service-specific error codes
	ErrorCodeTypeService FleetErrorCodeType = 1
	// ErrorCodeTypeEndpoint represents endpoint-specific error codes
	ErrorCodeTypeEndpoint FleetErrorCodeType = 2
)

func (t FleetErrorCodeType) String() string {
	switch t {
	case ErrorCodeTypeCommon:
		return "Common"
	case ErrorCodeTypeService:
		return "Service"
	case ErrorCodeTypeEndpoint:
		return "Endpoint"
	default:
		return "Unknown"
	}
}

// Error implements the error interface
func (e FleetError) Error() string {
	return fmt.Sprintf("FleetError: %s (%s: %d) %s", e.GRPCCode.String(), e.FleetErrorCodeType.String(), e.FleetErrorCode, e.DebugMessage)
}

func (e FleetError) IsExpected() bool {
	return isExpectedCode(e.GRPCCode)
}

// IsExpectedCode is the exported form of isExpectedCode for callers
// outside this package (e.g. interceptors that need to classify a
// connect.Error from a third-party source like protovalidate).
func IsExpectedCode(code connect.Code) bool {
	return isExpectedCode(code)
}

// isExpectedCode reports whether a gRPC code represents an expected (client-side) error.
// Stack traces are not captured for expected errors since they fire on hot paths
// (e.g. every failed plugin probe during discovery) and the traces are never useful.
func isExpectedCode(code connect.Code) bool {
	switch code {
	case connect.CodeCanceled,
		connect.CodeInvalidArgument,
		connect.CodeNotFound,
		connect.CodeAlreadyExists,
		connect.CodePermissionDenied,
		connect.CodeResourceExhausted,
		connect.CodeFailedPrecondition,
		connect.CodeAborted,
		connect.CodeOutOfRange,
		connect.CodeUnauthenticated,
		connect.CodeUnimplemented:
		return true
	case connect.CodeUnknown,
		connect.CodeDeadlineExceeded,
		connect.CodeInternal,
		connect.CodeUnavailable,
		connect.CodeDataLoss:
		return false
	}
	return false
}

func (e FleetError) ErrorWithStackTrace() string {
	return e.Error() + "\n" + e.StackTrace.String()
}

func New(
	debugMessage string,
	grpcCode connect.Code,
	fleetErrorCode int32,
	fleetErrorCodeType FleetErrorCodeType,
) FleetError {
	e := FleetError{
		DebugMessage:       debugMessage,
		GRPCCode:           grpcCode,
		FleetErrorCode:     fleetErrorCode,
		FleetErrorCodeType: fleetErrorCodeType,
	}
	if !isExpectedCode(grpcCode) {
		e.StackTrace = NewStackTrace(1)
	}
	return e
}

func (e FleetError) WithCallerStackTrace() FleetError {
	if !isExpectedCode(e.GRPCCode) {
		e.StackTrace = NewStackTrace(2)
	}
	return e
}

func (e FleetError) ConnectError() *connect.Error {
	connectError := connect.NewError(e.GRPCCode, errors.New(e.DebugMessage))

	fleetErrorDetails, err := e.fleetErrorDetails()
	if err != nil {
		return connect.NewError(connect.CodeInternal, fmt.Errorf("cannot create fleet error details: %w", err))
	}

	errorDetail, err := connect.NewErrorDetail(fleetErrorDetails)
	if err != nil {
		return connect.NewError(connect.CodeInternal, fmt.Errorf("cannot create fleet error details: %w", err))
	}

	connectError.AddDetail(errorDetail)

	return connectError
}

func (e FleetError) fleetErrorDetails() (*commonv1.FleetErrorDetails, error) {
	switch e.FleetErrorCodeType {
	case ErrorCodeTypeCommon:
		return &commonv1.FleetErrorDetails{
			Code: &commonv1.FleetErrorDetails_Common{
				Common: commonv1.FleetErrorCode(e.FleetErrorCode),
			},
		}, nil
	case ErrorCodeTypeService:
		return &commonv1.FleetErrorDetails{
			Code: &commonv1.FleetErrorDetails_Service{
				Service: e.FleetErrorCode,
			},
		}, nil
	case ErrorCodeTypeEndpoint:
		return &commonv1.FleetErrorDetails{
			Code: &commonv1.FleetErrorDetails_Endpoint{
				Endpoint: e.FleetErrorCode,
			},
		}, nil
	default:
		return nil, fmt.Errorf("unknown fleet error code type: %d", e.FleetErrorCodeType)
	}
}

func NewErrorWithCommonCode(
	debugMessage string,
	grpcCode connect.Code,
	fleetErrorCode commonv1.FleetErrorCode,
) FleetError {
	return New(debugMessage, grpcCode, int32(fleetErrorCode), ErrorCodeTypeCommon).WithCallerStackTrace()
}

func NewErrorWithServiceCode(
	debugMessage string,
	grpcCode connect.Code,
	fleetErrorCode int32,
) FleetError {
	return New(debugMessage, grpcCode, fleetErrorCode, ErrorCodeTypeService).WithCallerStackTrace()
}

func NewErrorWithEndpointCode(
	debugMessage string,
	grpcCode connect.Code,
	fleetErrorCode int32,
) FleetError {
	return New(debugMessage, grpcCode, fleetErrorCode, ErrorCodeTypeEndpoint).WithCallerStackTrace()
}

func NewPlainError(debugMessage string, grpcCode connect.Code) FleetError {
	return NewErrorWithCommonCode(debugMessage, grpcCode, commonv1.FleetErrorCode_FLEET_ERROR_CODE_UNSPECIFIED).WithCallerStackTrace()
}

func NewInternalError(debugMessage string) FleetError {
	return NewPlainError(debugMessage, connect.CodeInternal).WithCallerStackTrace()
}

func NewInternalErrorf(format string, a ...any) FleetError {
	return NewInternalError(
		fmt.Sprintf(format, a...),
	).WithCallerStackTrace()
}

func NewUnauthenticatedError(debugMessage string) FleetError {
	return NewPlainError(debugMessage, connect.CodeUnauthenticated).WithCallerStackTrace()
}

func NewUnauthenticatedErrorf(format string, a ...any) FleetError {
	return NewUnauthenticatedError(
		fmt.Sprintf(format, a...),
	).WithCallerStackTrace()
}

func NewForbiddenError(debugMessage string) FleetError {
	return NewPlainError(debugMessage, connect.CodePermissionDenied).WithCallerStackTrace()
}

func NewForbiddenErrorf(format string, a ...any) FleetError {
	return NewForbiddenError(
		fmt.Sprintf(format, a...),
	).WithCallerStackTrace()
}

func NewInvalidArgumentError(debugMessage string) FleetError {
	return NewPlainError(debugMessage, connect.CodeInvalidArgument).WithCallerStackTrace()
}

func NewInvalidArgumentErrorf(format string, a ...any) FleetError {
	return NewInvalidArgumentError(
		fmt.Sprintf(format, a...),
	).WithCallerStackTrace()
}

func NewNotFoundError(debugMessage string) FleetError {
	return NewPlainError(debugMessage, connect.CodeNotFound).WithCallerStackTrace()
}

func NewNotFoundErrorf(format string, a ...any) FleetError {
	return NewNotFoundError(
		fmt.Sprintf(format, a...),
	).WithCallerStackTrace()
}

func NewFailedPreconditionError(debugMessage string) FleetError {
	return NewPlainError(debugMessage, connect.CodeFailedPrecondition).WithCallerStackTrace()
}

func NewFailedPreconditionErrorf(format string, a ...any) FleetError {
	return NewFailedPreconditionError(
		fmt.Sprintf(format, a...),
	).WithCallerStackTrace()
}

func NewUnimplementedError(debugMessage string) FleetError {
	return NewPlainError(debugMessage, connect.CodeUnimplemented).WithCallerStackTrace()
}

func NewUnimplementedErrorf(format string, a ...any) FleetError {
	return NewUnimplementedError(
		fmt.Sprintf(format, a...),
	).WithCallerStackTrace()
}

func NewUnavailableErrorf(format string, a ...any) FleetError {
	return NewPlainError(
		fmt.Sprintf(format, a...),
		connect.CodeUnavailable,
	).WithCallerStackTrace()
}

func NewCanceledError() FleetError {
	return NewPlainError("operation was canceled", connect.CodeCanceled).WithCallerStackTrace()
}

func (e FleetError) Is(target error) bool {
	t, ok := target.(FleetError)
	if !ok {
		return false
	}

	return e.GRPCCode == t.GRPCCode &&
		e.FleetErrorCode == t.FleetErrorCode &&
		e.FleetErrorCodeType == t.FleetErrorCodeType &&
		e.DebugMessage == t.DebugMessage
}

// IsAuthenticationError checks if an error is an authentication error
func IsAuthenticationError(err error) bool {
	if err == nil {
		return false
	}

	var fleetErr FleetError
	if errors.As(err, &fleetErr) {
		return fleetErr.GRPCCode == connect.CodeUnauthenticated
	}

	// Also check for connect.Error directly
	var connectErr *connect.Error
	if errors.As(err, &connectErr) {
		return connectErr.Code() == connect.CodeUnauthenticated
	}

	return false
}

// IsForbiddenError checks if an error is a permission denied error.
func IsForbiddenError(err error) bool {
	if err == nil {
		return false
	}

	var fleetErr FleetError
	if errors.As(err, &fleetErr) {
		return fleetErr.GRPCCode == connect.CodePermissionDenied
	}

	var connectErr *connect.Error
	if errors.As(err, &connectErr) {
		return connectErr.Code() == connect.CodePermissionDenied
	}

	return false
}

// IsNotFoundError checks if an error is a not found error
func IsNotFoundError(err error) bool {
	if err == nil {
		return false
	}

	var fleetErr FleetError
	if errors.As(err, &fleetErr) {
		return fleetErr.GRPCCode == connect.CodeNotFound
	}

	// Also check for connect.Error directly
	var connectErr *connect.Error
	if errors.As(err, &connectErr) {
		return connectErr.Code() == connect.CodeNotFound
	}

	return false
}

// IsInvalidArgumentError checks if an error is an invalid argument error
func IsInvalidArgumentError(err error) bool {
	if err == nil {
		return false
	}

	var fleetErr FleetError
	if errors.As(err, &fleetErr) {
		return fleetErr.GRPCCode == connect.CodeInvalidArgument
	}

	var connectErr *connect.Error
	if errors.As(err, &connectErr) {
		return connectErr.Code() == connect.CodeInvalidArgument
	}

	return false
}

// IsCanceledError checks if an error is a cancellation error (e.g., client disconnect)
func IsCanceledError(err error) bool {
	if err == nil {
		return false
	}

	// Check for context.Canceled
	if errors.Is(err, context.Canceled) {
		return true
	}

	var fleetErr FleetError
	if errors.As(err, &fleetErr) {
		return fleetErr.GRPCCode == connect.CodeCanceled
	}

	var connectErr *connect.Error
	if errors.As(err, &connectErr) {
		return connectErr.Code() == connect.CodeCanceled
	}

	return false
}

// IsUnimplementedError checks if an error represents an unimplemented/unsupported capability
func IsUnimplementedError(err error) bool {
	if err == nil {
		return false
	}

	var fleetErr FleetError
	if errors.As(err, &fleetErr) {
		return fleetErr.GRPCCode == connect.CodeUnimplemented
	}

	var connectErr *connect.Error
	if errors.As(err, &connectErr) {
		return connectErr.Code() == connect.CodeUnimplemented
	}

	return false
}

func IsUnavailableError(err error) bool {
	if err == nil {
		return false
	}

	var fleetErr FleetError
	if errors.As(err, &fleetErr) {
		return fleetErr.GRPCCode == connect.CodeUnavailable
	}

	var connectErr *connect.Error
	if errors.As(err, &connectErr) {
		return connectErr.Code() == connect.CodeUnavailable
	}

	return false
}

func IsFailedPreconditionError(err error) bool {
	if err == nil {
		return false
	}

	var fleetErr FleetError
	if errors.As(err, &fleetErr) {
		return fleetErr.GRPCCode == connect.CodeFailedPrecondition
	}

	var connectErr *connect.Error
	if errors.As(err, &connectErr) {
		return connectErr.Code() == connect.CodeFailedPrecondition
	}

	return false
}

// ConnectionError represents a network connectivity error when attempting to reach a device
type ConnectionError struct {
	DeviceIdentifier string
	Err              error
}

func (e ConnectionError) Error() string {
	return fmt.Sprintf("failed to connect to device %s: %v", e.DeviceIdentifier, e.Err)
}

func (e ConnectionError) Unwrap() error {
	return e.Err
}

// NewConnectionError creates a new ConnectionError
func NewConnectionError(deviceIdentifier string, err error) ConnectionError {
	return ConnectionError{
		DeviceIdentifier: deviceIdentifier,
		Err:              err,
	}
}

// IsConnectionError checks if an error is a connection error
func IsConnectionError(err error) bool {
	if err == nil {
		return false
	}

	var connErr ConnectionError
	return errors.As(err, &connErr)
}
