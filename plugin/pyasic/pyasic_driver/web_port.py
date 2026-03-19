"""Optional web port override for pyasic HTTP traffic.

Contract tests run mock web APIs on high localhost ports. PyASIC hardcodes port
80 for most web-based miners, so the plugin can optionally rewrite outbound
HTTP requests via `PYASIC_WEB_PORT`.
"""

from __future__ import annotations

import logging
import os
from collections.abc import Callable
from ssl import SSLContext

import httpx
import pyasic.settings as pyasic_settings

logger = logging.getLogger(__name__)

_configured = False
_ENV_VAR = "PYASIC_WEB_PORT"
_DEFAULT_WEB_PORTS = {None, 80, 443}


def configure() -> None:
    """Install an HTTP transport wrapper when PYASIC_WEB_PORT is set."""
    global _configured  # noqa: PLW0603
    if _configured:
        return

    port = _get_override_port()
    if port is None:
        return

    original_transport: Callable[[str | bool | SSLContext], httpx.AsyncBaseTransport] = (
        pyasic_settings.transport
    )

    def transport_with_port_override(
        verify: str | bool | SSLContext = pyasic_settings.ssl_cxt,
    ) -> httpx.AsyncBaseTransport:
        return PortOverrideTransport(original_transport(verify), port)

    pyasic_settings.transport = transport_with_port_override
    _configured = True
    logger.info("Configured pyasic web port override: %s", port)


def _get_override_port() -> int | None:
    raw = os.getenv(_ENV_VAR, "").strip()
    if not raw:
        return None

    try:
        port = int(raw)
    except ValueError:
        logger.warning("Ignoring invalid %s=%r", _ENV_VAR, raw)
        return None

    if not (1 <= port <= 65535):
        logger.warning("Ignoring out-of-range %s=%r", _ENV_VAR, raw)
        return None

    return port


class PortOverrideTransport(httpx.AsyncBaseTransport):
    """Wrap an HTTP transport and rewrite default web ports to the override."""

    def __init__(self, inner: httpx.AsyncBaseTransport, port: int) -> None:
        self._inner = inner
        self._port = port

    async def handle_async_request(self, request: httpx.Request) -> httpx.Response:
        if request.url.scheme in {"http", "https"} and request.url.port in _DEFAULT_WEB_PORTS:
            request.url = request.url.copy_with(port=self._port)
        return await self._inner.handle_async_request(request)

    async def aclose(self) -> None:
        await self._inner.aclose()
