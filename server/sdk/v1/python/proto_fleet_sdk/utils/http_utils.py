"""HTTP error mapping utilities."""

from __future__ import annotations

try:
    import httpx  # type: ignore[import-not-found]  # httpx is an optional runtime dependency
    HAS_HTTPX = True
except ImportError:
    HAS_HTTPX = False

from proto_fleet_sdk.errors import (
    AuthenticationFailedError,
    DeviceNotFoundError,
    DeviceUnavailableError,
    InvalidConfigError,
    NetworkError,
    SDKError,
)

__all__ = ["map_http_error"]


def _map_status_code(
    status: int,
    error: Exception,
    device_id: str | None,
    context: str,
    op_context: str,
) -> SDKError:
    """Map HTTP status code to SDK error."""
    if status == 404:
        return DeviceNotFoundError(device_id or "unknown", cause=error)
    elif status in (401, 403):
        return AuthenticationFailedError(device_id=device_id or "unknown", cause=error)
    elif status == 400:
        return InvalidConfigError(f"Bad request{context}{op_context}: {error}", cause=error)
    elif status >= 500:
        return DeviceUnavailableError(device_id or "unknown", cause=error)
    else:
        return NetworkError(f"HTTP {status}{context}{op_context}", cause=error)


def map_http_error(
    error: Exception,
    device_id: str | None = None,
    operation: str | None = None
) -> SDKError:
    """Map HTTP errors to appropriate SDK error types.

    Provides standard mapping from httpx exceptions to SDK errors,
    ensuring consistent error handling across all plugin implementations.

    Example:
        >>> async def status(self, ctx: Context) -> DeviceMetrics:
        ...     try:
        ...         response = await self.client.get(f"{self.base_url}/status")
        ...         response.raise_for_status()
        ...         return self._parse_status(response.json())
        ...     except httpx.HTTPError as e:
        ...         raise map_http_error(e, device_id=self.id(), operation="status")
    """
    context = f" for device {device_id}" if device_id else ""
    op_context = f" during {operation}" if operation else ""

    if HAS_HTTPX and isinstance(error, httpx.HTTPStatusError):
        return _map_status_code(error.response.status_code, error, device_id, context, op_context)

    if HAS_HTTPX and isinstance(error, httpx.ConnectError):
        return DeviceUnavailableError(device_id or "unknown", cause=error)

    if HAS_HTTPX and isinstance(error, httpx.TimeoutException):
        return DeviceUnavailableError(device_id or "unknown", cause=error)

    if hasattr(error, "response") and hasattr(error.response, "status_code"):
        return _map_status_code(error.response.status_code, error, device_id, context, op_context)

    error_str = str(error).lower()
    if "connection" in error_str or "timeout" in error_str:
        return DeviceUnavailableError(device_id or "unknown", cause=error)

    return NetworkError(f"Network error{context}{op_context}: {error}", cause=error)
