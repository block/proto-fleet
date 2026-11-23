package sdk

import (
	"context"
	"errors"
	"net"
	"syscall"

	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/status"
)

const (
	deviceProbeDomain = "protofleet.sdk"
	deviceProbeReason = "SDK_DEVICE_PROBE_FAILED"
)

type deviceProbeError struct{ error }

func (e *deviceProbeError) Unwrap() error { return e.error }

// WithDeviceProbeFailure marks a failed connection to the device from inside the plugin.
func WithDeviceProbeFailure(err error) error {
	if err == nil {
		return nil
	}
	return &deviceProbeError{err}
}

// WithDeviceDialFailure marks a socket connection failure from a device probe.
// Read/write errors, DNS or configuration failures, and canceled work do not
// establish a failed connection to the miner.
func WithDeviceDialFailure(ctx context.Context, err error) error {
	if DeviceDialFailed(ctx, err) {
		return WithDeviceProbeFailure(err)
	}
	return err
}

// DeviceDialFailed checks the socket error before a plugin replaces it with a
// sanitized error for callers.
func DeviceDialFailed(ctx context.Context, err error) bool {
	var opErr *net.OpError
	if ctx.Err() != nil || errors.Is(err, context.Canceled) || !errors.As(err, &opErr) || opErr == nil || opErr.Op != "dial" {
		return false
	}
	var dnsErr *net.DNSError
	if errors.As(opErr, &dnsErr) {
		return false
	}
	// The dialer can time out before the caller's context expires.
	return opErr.Timeout() || errors.Is(opErr, syscall.ECONNREFUSED) || errors.Is(opErr, syscall.EHOSTUNREACH) ||
		errors.Is(opErr, syscall.ENETUNREACH) || errors.Is(opErr, syscall.ETIMEDOUT)
}

// DeviceProbeFailed reports evidence from a plugin's device status probe. A
// plugin transport failure has no SDK evidence.
func DeviceProbeFailed(err error) bool {
	var local *deviceProbeError
	if errors.As(err, &local) {
		return true
	}
	wire, ok := status.FromError(err)
	if !ok || wire == nil {
		return false
	}
	for _, detail := range wire.Details() {
		if info, ok := detail.(*errdetails.ErrorInfo); ok && info.Domain == deviceProbeDomain && info.Reason == deviceProbeReason {
			return true
		}
	}
	return false
}
