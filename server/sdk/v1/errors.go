package sdk

import "strings"

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
	// ErrCodeDefaultPasswordActive indicates the device requires a password change before normal operations
	ErrCodeDefaultPasswordActive ErrorCode = "DEFAULT_PASSWORD_ACTIVE"
)

// DefaultPasswordMessageMarker is the firmware #3269 free-text 403 substring.
// Match via IsDefaultPasswordMessage only — TestDefaultPasswordContract pins it.
const DefaultPasswordMessageMarker = "default password must be changed"

// DefaultPasswordCodeMarker is the lowercased form of the error code; firmware
// sometimes surfaces the code as the message body rather than a structured field.
var DefaultPasswordCodeMarker = strings.ToLower(string(ErrCodeDefaultPasswordActive))

// IsDefaultPasswordMessage reports whether msg contains a default-password
// marker from the firmware contract. Use this helper everywhere that inspects
// an error message for default-password intent.
func IsDefaultPasswordMessage(msg string) bool {
	lower := strings.ToLower(msg)
	return strings.Contains(lower, DefaultPasswordMessageMarker) ||
		strings.Contains(lower, DefaultPasswordCodeMarker)
}

// IsDefaultPasswordCode reports whether code matches ErrCodeDefaultPasswordActive
// (case-insensitive), regardless of how firmware formats the constant.
func IsDefaultPasswordCode(code string) bool {
	return strings.EqualFold(code, string(ErrCodeDefaultPasswordActive))
}

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

// NewErrorDefaultPasswordActive returns an error indicating the device still has
// its factory default password. Most API operations are blocked (HTTP 403) until
// the password is changed.
func NewErrorDefaultPasswordActive(deviceID string, err ...error) SDKError {
	var underlying error
	if len(err) > 0 {
		underlying = err[0]
	}
	return SDKError{
		Code:    ErrCodeDefaultPasswordActive,
		Message: DefaultPasswordMessageMarker + " for device: " + deviceID,
		Err:     underlying,
	}
}
