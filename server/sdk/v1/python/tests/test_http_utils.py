"""Tests for HTTP error mapping utilities."""

from __future__ import annotations

from unittest.mock import MagicMock

import pytest

from proto_fleet_sdk.errors import (
    AuthenticationFailedError,
    DeviceNotFoundError,
    DeviceUnavailableError,
    InvalidConfigError,
    NetworkError,
)
from proto_fleet_sdk.utils.http_utils import map_http_error


def _make_http_error(status_code: int) -> Exception:
    """Create an exception with a duck-typed .response.status_code attribute."""
    err = Exception(f"HTTP {status_code}")
    err.response = MagicMock()
    err.response.status_code = status_code
    return err


try:
    import httpx
    HAS_HTTPX = True
except ImportError:
    HAS_HTTPX = False


class TestMapHttpError:
    """Tests for map_http_error function."""

    def test_generic_exception_returns_network_error(self) -> None:
        err = Exception("something broke")
        result = map_http_error(err, device_id="miner-1")
        assert isinstance(result, NetworkError)

    def test_connection_string_returns_device_unavailable(self) -> None:
        err = Exception("connection refused")
        result = map_http_error(err, device_id="miner-1")
        assert isinstance(result, DeviceUnavailableError)

    def test_timeout_string_returns_device_unavailable(self) -> None:
        err = Exception("request timeout")
        result = map_http_error(err, device_id="miner-1")
        assert isinstance(result, DeviceUnavailableError)

    def test_duck_type_404_returns_not_found(self) -> None:
        err = _make_http_error(404)
        result = map_http_error(err, device_id="miner-1")
        assert isinstance(result, DeviceNotFoundError)

    def test_duck_type_401_returns_auth_failed(self) -> None:
        err = _make_http_error(401)
        result = map_http_error(err, device_id="miner-1")
        assert isinstance(result, AuthenticationFailedError)

    def test_duck_type_400_returns_invalid_config(self) -> None:
        err = _make_http_error(400)
        result = map_http_error(err, device_id="miner-1")
        assert isinstance(result, InvalidConfigError)

    def test_duck_type_500_returns_device_unavailable(self) -> None:
        err = _make_http_error(500)
        result = map_http_error(err, device_id="miner-1")
        assert isinstance(result, DeviceUnavailableError)

    def test_operation_context_in_error(self) -> None:
        err = Exception("something broke")
        result = map_http_error(err, device_id="miner-1", operation="status")
        assert "status" in str(result)
        assert "miner-1" in str(result)

    @pytest.mark.skipif(not HAS_HTTPX, reason="httpx not installed")
    def test_httpx_connect_error(self) -> None:
        err = httpx.ConnectError("connection refused")
        result = map_http_error(err, device_id="miner-1")
        assert isinstance(result, DeviceUnavailableError)

    @pytest.mark.skipif(not HAS_HTTPX, reason="httpx not installed")
    def test_httpx_timeout_error(self) -> None:
        err = httpx.ReadTimeout("timed out")
        result = map_http_error(err, device_id="miner-1")
        assert isinstance(result, DeviceUnavailableError)
