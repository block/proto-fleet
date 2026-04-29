package sdk

import "fmt"

type ErrorCode string

const (
	// ErrCodeUnsupportedCapability represents an unsupported capability error
	ErrCodeUnsupportedCapability ErrorCode = "UNSUPPORTED_CAPABILITY"
	// ErrCodeDeviceNotFound represents a device not found error
	ErrCodeDeviceNotFound ErrorCode = "DEVICE_NOT_FOUND"
	// ErrCodeInvalidConfig represents an invalid configuration error
	ErrCodeInvalidConfig ErrorCode = "INVALID_CONFIG"
	// ErrCodeDeviceUnavailable represents a device unavailable error
	ErrCodeDeviceUnavailable ErrorCode = "DEVICE_UNAVAILABLE"
	// ErrCodeDriverShutdown represents a driver shutdown error
	ErrCodeDriverShutdown ErrorCode = "DRIVER_SHUTDOWN"
	// ErrCodeAuthenticationFailed represents an authentication failure error
	ErrCodeAuthenticationFailed ErrorCode = "AUTHENTICATION_FAILED"
	// ErrCodeCurtailCapabilityNotSupported is a permanent unsupported-level error.
	ErrCodeCurtailCapabilityNotSupported ErrorCode = "CURTAIL_CAPABILITY_NOT_SUPPORTED"
	// ErrCodeCurtailTransient is a retryable curtail/uncurtail dispatch failure.
	ErrCodeCurtailTransient ErrorCode = "CURTAIL_TRANSIENT"
)

type SDKError struct {
	Code    ErrorCode
	Message string
	Err     error
}

func (e SDKError) Error() string {
	return e.Message
}

func (e SDKError) Unwrap() error {
	return e.Err
}

// NewErrUnsupportedCapability returns a new unsupported capability error
func NewErrUnsupportedCapability(capability string, err ...error) SDKError {
	var underlying error
	if len(err) > 0 {
		underlying = err[0]
	}
	return SDKError{
		Code:    ErrCodeUnsupportedCapability,
		Message: "unsupported capability: " + capability,
		Err:     underlying,
	}
}

// NewErrorDeviceNotFound returns a new device not found error
func NewErrorDeviceNotFound(deviceID string, err ...error) SDKError {
	var underlying error
	if len(err) > 0 {
		underlying = err[0]
	}
	return SDKError{
		Code:    ErrCodeDeviceNotFound,
		Message: "device not found: " + deviceID,
		Err:     underlying,
	}
}

// NewErrorInvalidConfig returns a new invalid configuration error
func NewErrorInvalidConfig(deviceID string, err ...error) SDKError {
	var underlying error
	if len(err) > 0 {
		underlying = err[0]
	}
	return SDKError{
		Code:    ErrCodeInvalidConfig,
		Message: "invalid device configuration: " + deviceID,
		Err:     underlying,
	}
}

// NewErrorDeviceUnavailable returns a new device unavailable error
func NewErrorDeviceUnavailable(deviceID string, err ...error) SDKError {
	var underlying error
	if len(err) > 0 {
		underlying = err[0]
	}
	return SDKError{
		Code:    ErrCodeDeviceUnavailable,
		Message: "device unavailable: " + deviceID,
		Err:     underlying,
	}
}

// NewErrorDriverShutdown returns a new driver shutdown error
func NewErrorDriverShutdown(err ...error) SDKError {
	var underlying error
	if len(err) > 0 {
		underlying = err[0]
	}
	return SDKError{
		Code:    ErrCodeDriverShutdown,
		Message: "driver shutdown",
		Err:     underlying,
	}
}

// NewErrorAuthenticationFailed returns a new authentication failed error
func NewErrorAuthenticationFailed(deviceID string, err ...error) SDKError {
	var underlying error
	if len(err) > 0 {
		underlying = err[0]
	}
	return SDKError{
		Code:    ErrCodeAuthenticationFailed,
		Message: "authentication failed for device: " + deviceID,
		Err:     underlying,
	}
}

// NewErrCurtailCapabilityNotSupported reports an unsupported curtail level.
func NewErrCurtailCapabilityNotSupported(deviceID string, level int32, err ...error) SDKError {
	var underlying error
	if len(err) > 0 {
		underlying = err[0]
	}
	return SDKError{
		Code:    ErrCodeCurtailCapabilityNotSupported,
		Message: fmt.Sprintf("curtail level %d not supported by device: %s", level, deviceID),
		Err:     underlying,
	}
}

// NewErrCurtailTransient marks a curtail dispatch failure as retryable.
func NewErrCurtailTransient(deviceID string, err error) SDKError {
	return SDKError{
		Code:    ErrCodeCurtailTransient,
		Message: "transient curtail failure for device: " + deviceID,
		Err:     err,
	}
}
