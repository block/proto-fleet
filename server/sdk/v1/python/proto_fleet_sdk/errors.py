"""SDK exception hierarchy.

This module defines all SDK exceptions and error handling utilities. All SDK exceptions
inherit from SDKError and include an error code, message, and optional cause.
"""

from __future__ import annotations

from enum import StrEnum

__all__ = [
    "ErrorCode",
    "SDKError",
    "UnsupportedCapabilityError",
    "DeviceNotFoundError",
    "InvalidConfigError",
    "DeviceUnavailableError",
    "AuthenticationFailedError",
    "DriverShutdownError",
    "NetworkError",
]


class ErrorCode(StrEnum):
    """SDK error codes for categorizing failures."""

    UNSUPPORTED_CAPABILITY = "UNSUPPORTED_CAPABILITY"
    DEVICE_NOT_FOUND = "DEVICE_NOT_FOUND"
    INVALID_CONFIG = "INVALID_CONFIG"
    DEVICE_UNAVAILABLE = "DEVICE_UNAVAILABLE"
    AUTHENTICATION_FAILED = "AUTHENTICATION_FAILED"
    DRIVER_SHUTDOWN = "DRIVER_SHUTDOWN"
    NETWORK_ERROR = "NETWORK_ERROR"


class SDKError(Exception):
    """Base exception for all SDK errors.

    All SDK exceptions include an error code for categorization, a human-readable
    message, and an optional cause exception.
    """

    def __init__(
        self,
        code: ErrorCode,
        message: str,
        cause: Exception | None = None,
        device_id: str | None = None,
    ) -> None:
        super().__init__(message)
        self.code = code
        self.message = message
        self.device_id = device_id
        if cause:
            self.__cause__ = cause

    def __str__(self) -> str:
        parts = [f"{self.code.value}: {self.message}"]
        if self.device_id:
            parts.append(f"(device_id={self.device_id})")
        if self.__cause__:
            parts.append(f"caused by: {self.__cause__}")
        return " ".join(parts)


class UnsupportedCapabilityError(SDKError):
    """Raised when a requested capability is not supported.

    This error indicates that a method was called but the device or driver does not
    support that functionality. Check capabilities before calling optional methods.
    """

    def __init__(
        self,
        capability: str,
        device_id: str | None = None,
        cause: Exception | None = None,
    ) -> None:
        message = f"Capability '{capability}' is not supported"
        if device_id:
            message += f" for device {device_id}"
        super().__init__(
            code=ErrorCode.UNSUPPORTED_CAPABILITY,
            message=message,
            cause=cause,
            device_id=device_id,
        )


class DeviceNotFoundError(SDKError):
    """Raised when a device cannot be found or identified.

    This error is typically raised during discovery when no compatible device is found
    at the specified address, or when attempting operations on a device that no longer
    exists in the driver's device map.
    """

    def __init__(self, device_id: str, cause: Exception | None = None) -> None:
        super().__init__(
            code=ErrorCode.DEVICE_NOT_FOUND,
            message=f"Device not found: {device_id}",
            cause=cause,
            device_id=device_id,
        )


class InvalidConfigError(SDKError):
    """Raised when configuration is invalid or incompatible.

    This error indicates that provided configuration (pools, passwords, settings, etc.)
    is malformed, incomplete, or incompatible with the device.
    """

    def __init__(self, message: str, cause: Exception | None = None) -> None:
        super().__init__(
            code=ErrorCode.INVALID_CONFIG,
            message=message,
            cause=cause,
        )


class DeviceUnavailableError(SDKError):
    """Raised when a device is unreachable or not responding.

    This error indicates network connectivity issues, device offline, or device not
    responding to requests. It's typically a transient error that may resolve.
    """

    def __init__(self, device_id: str, cause: Exception | None = None) -> None:
        super().__init__(
            code=ErrorCode.DEVICE_UNAVAILABLE,
            message=f"Device unavailable: {device_id}",
            cause=cause,
            device_id=device_id,
        )


class AuthenticationFailedError(SDKError):
    """Raised when authentication with a device fails.

    This error indicates that provided credentials are incorrect, expired, or the
    authentication mechanism failed for another reason.
    """

    def __init__(self, device_id: str, cause: Exception | None = None) -> None:
        super().__init__(
            code=ErrorCode.AUTHENTICATION_FAILED,
            message=f"Authentication failed for device: {device_id}",
            cause=cause,
            device_id=device_id,
        )


class DriverShutdownError(SDKError):
    """Raised when operations are attempted during or after driver shutdown.

    This error indicates that the driver is shutting down or has shut down, and
    no further operations can be performed.
    """

    def __init__(
        self,
        reason: str | None = None,
        cause: Exception | None = None,
    ) -> None:
        message = "Driver is shutting down"
        if reason:
            message += f": {reason}"
        super().__init__(
            code=ErrorCode.DRIVER_SHUTDOWN,
            message=message,
            cause=cause,
        )


class NetworkError(SDKError):
    """Raised when network-related errors occur.

    This error indicates network connectivity issues, timeouts, or other
    network-level failures that don't fit other error categories.
    """

    def __init__(self, message: str, cause: Exception | None = None) -> None:
        super().__init__(
            code=ErrorCode.NETWORK_ERROR,
            message=message,
            cause=cause,
        )
