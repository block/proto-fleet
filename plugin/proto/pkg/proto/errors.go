package proto

import (
	"fmt"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// ErrCodeDefaultPasswordActive is the Proto firmware's default-password
// error-code value. It's Proto-firmware-specific — the shared plugin SDK does
// not define it because no other driver has an equivalent gate. Fleet server
// code detects the resulting failure via the gRPC status (PermissionDenied +
// marker text).
const ErrCodeDefaultPasswordActive = "DEFAULT_PASSWORD_ACTIVE"

// NewErrorDefaultPasswordActive builds a gRPC PermissionDenied status
// indicating the device still has its factory default password. Constructing
// the gRPC status directly (rather than going through an SDK error type)
// keeps the shared SDK driver-neutral.
func NewErrorDefaultPasswordActive(deviceID string, cause error) error {
	msg := fmt.Sprintf("%s for device: %s", defaultPasswordMessageMarker, deviceID)
	if cause != nil {
		msg = fmt.Sprintf("%s: %v", msg, cause)
	}
	return fmt.Errorf("default password active: %w", status.Error(codes.PermissionDenied, msg))
}
