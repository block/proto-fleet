"""PyASIC driver implementation.

Implements the Driver protocol for multi-manufacturer miner support via pyasic.
Uses pyasic's auto-detection for manufacturer/model identification and dynamic
capability introspection per miner instance.
"""

from __future__ import annotations

import asyncio
import logging
from typing import Any, Callable, Coroutine

import grpc
from proto_fleet_sdk.auth import SecretBundle, UsernamePassword
from proto_fleet_sdk.errors import (
    AuthenticationFailedError,
    DeviceNotFoundError,
    DeviceUnavailableError,
)
from proto_fleet_sdk.types import Capabilities, DeviceInfo, DriverIdentifier, NewDeviceResult

from pyasic_driver.capabilities import (
    DEFAULT_CREDENTIALS,
    STATIC_BASE_CAPABILITIES,
    build_capabilities,
)
from pyasic_driver.config import PluginConfig
from pyasic_driver.device import PyAsicDevice

logger = logging.getLogger(__name__)

_DRIVER_NAME = "pyasic"
_API_VERSION = "v1"
_DEFAULT_PORT = 80

# Ports that pyasic actually probes during detection.
# Socket-based miners (WhatsMiner, Antminer CGMiner API) use 4028;
# web-based miners (Auradine, Goldshell, etc.) use 80 or 443.
# If nmap reports an open port outside this set, we must skip it to
# avoid claiming the same device twice on different ports.
_DISCOVERY_PORTS = {80, 443, 4028}
_HTTPS_PORT = 443

GetMinerFunc = Callable[[str], Coroutine[Any, Any, Any]]


def _default_get_miner() -> GetMinerFunc:
    """Return the default pyasic.get_miner function."""
    import pyasic

    return pyasic.get_miner


def _apply_credentials(miner: Any, secret: SecretBundle) -> None:
    """Apply SDK credentials to a pyasic miner instance.

    Sets the RPC password (used for privileged commands like set.system.led)
    and web credentials (used by web-based miners like Antminer/BOS).
    The fleet server stores the validated RPC password from pairing, so we
    always apply it here to ensure commands authenticate correctly.
    """
    if isinstance(secret.kind, UsernamePassword):
        rpc_applied = False
        if hasattr(miner, "rpc") and miner.rpc is not None and hasattr(miner.rpc, "pwd"):
            miner.rpc.pwd = secret.kind.password
            rpc_applied = True

        web_applied = False
        if hasattr(miner, "web") and miner.web is not None:
            if hasattr(miner.web, "pwd"):
                miner.web.pwd = secret.kind.password
                web_applied = True
            if hasattr(miner.web, "username"):
                miner.web.username = secret.kind.username

        logger.info(
            "Applied credentials to %s (user=%s, rpc=%s, web=%s)",
            getattr(miner, "ip", "?"),
            secret.kind.username,
            rpc_applied,
            web_applied,
        )
    else:
        logger.warning(
            "Unsupported credential type %s for %s, skipping",
            type(secret.kind).__name__,
            getattr(miner, "ip", "?"),
        )


class PyAsicDriver:
    """PyASIC-based multi-manufacturer miner driver.

    Discovers, pairs, and manages miners from any manufacturer that pyasic
    supports. Capabilities are detected dynamically per miner instance.
    """

    def __init__(
        self,
        config: PluginConfig,
        *,
        get_miner: GetMinerFunc | None = None,
    ) -> None:
        self._config = config
        self._enabled_makes = config.enabled_makes()
        self._get_miner_fn = get_miner or _default_get_miner()
        self._devices: dict[str, PyAsicDevice] = {}
        self._lock = asyncio.Lock()

    async def handshake(self, ctx: grpc.ServicerContext) -> DriverIdentifier:
        return DriverIdentifier(driver_name=_DRIVER_NAME, api_version=_API_VERSION)

    async def describe_driver(self, ctx: grpc.ServicerContext) -> tuple[DriverIdentifier, Capabilities]:
        identifier = DriverIdentifier(driver_name=_DRIVER_NAME, api_version=_API_VERSION)
        return identifier, dict(STATIC_BASE_CAPABILITIES)

    async def discover_device(self, ctx: grpc.ServicerContext, ip_address: str, port: int) -> DeviceInfo:
        if port not in _DISCOVERY_PORTS:
            raise DeviceNotFoundError(ip_address)

        miner = await self._probe_miner(ip_address)

        make = getattr(miner, "make", None)
        if not make or str(make) not in self._enabled_makes:
            raise DeviceNotFoundError(ip_address)

        model = getattr(miner, "model", "") or ""
        manufacturer = str(make)
        firmware_version = getattr(miner, "fw_ver", "") or ""

        effective_port = port or _DEFAULT_PORT
        url_scheme = "https" if effective_port == _HTTPS_PORT else "http"

        logger.info("Discovered %s %s at %s", manufacturer, model, ip_address)
        return DeviceInfo(
            host=ip_address,
            port=effective_port,
            url_scheme=url_scheme,
            serial_number="",
            model=model,
            manufacturer=manufacturer,
            mac_address="",
            firmware_version=firmware_version,
        )

    async def pair_device(
        self, ctx: grpc.ServicerContext, device_info: DeviceInfo, secret: SecretBundle
    ) -> DeviceInfo:
        miner = await self._probe_miner(device_info.host)
        _apply_credentials(miner, secret)

        try:
            data = await miner.get_data()
        except (OSError, asyncio.TimeoutError) as exc:
            raise DeviceUnavailableError(device_info.host, cause=exc) from exc
        except Exception as exc:
            raise AuthenticationFailedError(device_info.host, cause=exc) from exc

        if data is None:
            raise AuthenticationFailedError(device_info.host)

        await self._validate_privileged_access(miner, device_info.host)

        mac = getattr(data, "mac", "") or ""
        firmware = getattr(data, "fw_ver", "") or device_info.firmware_version

        logger.info("Paired %s at %s (mac=%s)", device_info.model, device_info.host, mac)
        return DeviceInfo(
            host=device_info.host,
            port=device_info.port,
            url_scheme=device_info.url_scheme,
            serial_number=device_info.serial_number,
            model=device_info.model,
            manufacturer=device_info.manufacturer,
            mac_address=mac,
            firmware_version=firmware,
        )

    async def _validate_privileged_access(self, miner: Any, host: str) -> None:
        """Validate write credentials by sending back the current config unchanged.

        Some miners (e.g. WhatsMiner V3) use separate auth for reads vs writes.
        get_data() succeeds with any password, so we must verify a write operation
        actually works before storing the credential. Reading the current config
        and writing it back is a no-op that validates auth across all manufacturers.
        """
        try:
            config = await miner.get_config()
        except (OSError, asyncio.TimeoutError) as exc:
            raise DeviceUnavailableError(host, cause=exc) from exc
        except Exception as exc:
            logger.warning("Failed to read config for credential validation on %s: %s", host, exc)
            raise AuthenticationFailedError(host, cause=exc) from exc

        if config is None:
            logger.debug("No config available for %s, skipping write validation", host)
            return

        try:
            await miner.send_config(config)
        except (OSError, asyncio.TimeoutError) as exc:
            raise DeviceUnavailableError(host, cause=exc) from exc
        except Exception as exc:
            logger.warning("Credential validation (send_config) failed for %s: %s", host, exc)
            raise AuthenticationFailedError(host, cause=exc) from exc

    async def new_device(
        self, ctx: grpc.ServicerContext, device_id: str, device_info: DeviceInfo, secret: SecretBundle
    ) -> NewDeviceResult:
        miner = await self._try_probe_miner(device_info.host)
        if miner is not None:
            _apply_credentials(miner, secret)
            caps = build_capabilities(miner)
        else:
            caps = dict(STATIC_BASE_CAPABILITIES)

        device = PyAsicDevice(
            device_id=device_id,
            miner=miner,
            device_info=device_info,
            caps=caps,
            cache_ttl_seconds=self._config.plugin.telemetry_cache_ttl_seconds,
            probe_fn=self._get_miner_fn,
            secret=secret,
        )

        async with self._lock:
            self._devices[device_id] = device

        logger.info(
            "Created device %s for %s at %s (connected=%s)",
            device_id, device_info.model, device_info.host, miner is not None,
        )
        return NewDeviceResult(device=device)

    async def get_default_credentials(self, ctx: grpc.ServicerContext) -> list[UsernamePassword]:
        creds: list[UsernamePassword] = []
        seen: set[tuple[str, str]] = set()
        for make in self._enabled_makes:
            for cred in DEFAULT_CREDENTIALS.get(make, []):
                key = (cred.username, cred.password)
                if key not in seen:
                    creds.append(cred)
                    seen.add(key)
        return creds

    async def get_capabilities_for_model(self, ctx: grpc.ServicerContext, model: str) -> Capabilities:
        return dict(STATIC_BASE_CAPABILITIES)

    async def _probe_miner(self, ip_address: str) -> Any:
        """Probe an IP with pyasic and return the identified miner object.

        Raises DeviceUnavailableError on timeout/connection failure and
        DeviceNotFoundError if pyasic cannot identify the device.
        """
        timeout = self._config.plugin.discovery_timeout_seconds
        try:
            miner = await asyncio.wait_for(self._get_miner_fn(ip_address), timeout=timeout)
        except asyncio.TimeoutError:
            raise DeviceUnavailableError(ip_address) from None
        except Exception as exc:
            raise DeviceUnavailableError(ip_address, cause=exc) from exc

        if miner is None or not getattr(miner, "make", None):
            raise DeviceNotFoundError(ip_address)

        return miner

    async def _try_probe_miner(self, ip_address: str) -> Any | None:
        """Probe an IP, returning None if the miner is temporarily unreachable.

        Used by new_device to allow device creation even when the miner is
        offline. The device will attempt reconnection on first use.
        """
        try:
            return await self._probe_miner(ip_address)
        except DeviceUnavailableError:
            logger.warning("Miner at %s is unreachable, creating device in disconnected state", ip_address)
            return None
