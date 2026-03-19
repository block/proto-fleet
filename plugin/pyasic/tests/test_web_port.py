"""Tests for pyasic web port override transport."""

from __future__ import annotations

from ssl import SSLContext

import httpx
import pyasic.settings as pyasic_settings
import pytest

from pyasic_driver import web_port


class RecordingTransport(httpx.AsyncBaseTransport):
    """Capture requests passed through the override transport."""

    def __init__(self) -> None:
        self.requests: list[httpx.Request] = []
        self.closed = False

    async def handle_async_request(self, request: httpx.Request) -> httpx.Response:
        self.requests.append(request)
        return httpx.Response(200, request=request)

    async def aclose(self) -> None:
        self.closed = True


@pytest.fixture(autouse=True)
def reset_web_port_state(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setattr(web_port, "_configured", False)
    monkeypatch.setattr(pyasic_settings, "transport", pyasic_settings.transport)
    monkeypatch.delenv("PYASIC_WEB_PORT", raising=False)


def test_configure_is_noop_when_env_missing_or_invalid(
    monkeypatch: pytest.MonkeyPatch,
    caplog: pytest.LogCaptureFixture,
) -> None:
    original_transport = pyasic_settings.transport

    for raw in (None, "", "abc", "0", "65536"):
        monkeypatch.setattr(web_port, "_configured", False)
        monkeypatch.setattr(pyasic_settings, "transport", original_transport)
        if raw is None:
            monkeypatch.delenv("PYASIC_WEB_PORT", raising=False)
        else:
            monkeypatch.setenv("PYASIC_WEB_PORT", raw)

        with caplog.at_level("WARNING"):
            web_port.configure()

        assert pyasic_settings.transport is original_transport
        assert web_port._configured is False


def test_configure_wraps_transport_when_env_is_valid(monkeypatch: pytest.MonkeyPatch) -> None:
    inner = RecordingTransport()

    def original_transport(
        verify: str | bool | SSLContext = pyasic_settings.ssl_cxt,
    ) -> httpx.AsyncBaseTransport:
        assert verify is pyasic_settings.ssl_cxt
        return inner

    monkeypatch.setenv("PYASIC_WEB_PORT", "18080")
    monkeypatch.setattr(pyasic_settings, "transport", original_transport)

    web_port.configure()

    assert web_port._configured is True
    assert pyasic_settings.transport is not original_transport
    assert isinstance(pyasic_settings.transport(), web_port.PortOverrideTransport)


@pytest.mark.asyncio
@pytest.mark.parametrize(
    ("url", "expected"),
    [
        ("http://miner.local/api/v1/info", "http://miner.local:18080/api/v1/info"),
        ("http://miner.local:80/api/v1/info", "http://miner.local:18080/api/v1/info"),
        ("https://miner.local/api/v1/info", "https://miner.local:18080/api/v1/info"),
        ("https://miner.local:443/api/v1/info", "https://miner.local:18080/api/v1/info"),
    ],
)
async def test_default_ports_are_rewritten(url: str, expected: str) -> None:
    inner = RecordingTransport()
    transport = web_port.PortOverrideTransport(inner, 18080)

    response = await transport.handle_async_request(httpx.Request("GET", url))

    assert str(inner.requests[0].url) == expected
    assert str(response.request.url) == expected


@pytest.mark.asyncio
@pytest.mark.parametrize(
    "url",
    [
        "http://miner.local:8080/api/v1/info",
        "https://miner.local:8443/api/v1/info",
    ],
)
async def test_non_default_ports_are_left_untouched(url: str) -> None:
    inner = RecordingTransport()
    transport = web_port.PortOverrideTransport(inner, 18080)

    response = await transport.handle_async_request(httpx.Request("GET", url))

    assert str(inner.requests[0].url) == url
    assert str(response.request.url) == url
