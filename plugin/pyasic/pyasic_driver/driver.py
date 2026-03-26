"""PyASIC driver implementation.

Implements the gRPC DriverServicer for multi-manufacturer miner support via pyasic.
Uses pyasic's auto-detection for manufacturer/model identification and dynamic
capability introspection per miner instance.
"""

from __future__ import annotations

import asyncio
import logging
from typing import Any, Callable, Coroutine

import grpc
from google.protobuf.empty_pb2 import Empty
from proto_fleet_sdk.errors import (
    AuthenticationFailedError,
    DeviceNotFoundError,
    DeviceUnavailableError,
    InvalidConfigError,
    UnsupportedCapabilityError,
    grpc_error_handler,
)
from proto_fleet_sdk.generated.pb import driver_pb2, driver_pb2_grpc

from pyasic_driver.capabilities import (
    DEFAULT_CREDENTIALS,
    FIRMWARE_MANUFACTURER,
    MAKE_TO_FAMILY,
    STATIC_BASE_CAPABILITIES,
    Capabilities,
    build_capabilities,
    detect_firmware_variant,
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

_write_validators: dict[type, Callable[[Any], Coroutine[Any, Any, None]]] = {}


def register_write_validator(cls: type, fn: Callable[[Any], Coroutine[Any, Any, None]]) -> None:
    _write_validators[cls] = fn


async def validate_write_access(miner: Any) -> None:
    for cls, fn in _write_validators.items():
        if isinstance(miner, cls):
            await fn(miner)
            return


def _default_get_miner() -> GetMinerFunc:
    """Return the default pyasic.get_miner function."""
    import pyasic

    return pyasic.get_miner


def _apply_credentials(miner: Any, secret: driver_pb2.SecretBundle) -> None:
    """Apply SDK credentials to a pyasic miner instance.

    Sets the RPC password (used for privileged commands like set.system.led)
    and web credentials (used by web-based miners like Antminer/BOS).
    The fleet server stores the validated RPC password from pairing, so we
    always apply it here to ensure commands authenticate correctly.
    """
    kind_field = secret.WhichOneof("kind")
    if kind_field is None:
        logger.warning("No credentials provided for %s, skipping", getattr(miner, "ip", "?"))
        return
    if kind_field != "user_pass":
        raise InvalidConfigError(
            f"Unsupported credential type '{kind_field}' for {getattr(miner, 'ip', '?')}"
        )

    up = secret.user_pass
    rpc_applied = False
    if hasattr(miner, "rpc") and miner.rpc is not None and hasattr(miner.rpc, "pwd"):
        miner.rpc.pwd = up.password
        rpc_applied = True

    web_applied = False
    if hasattr(miner, "web") and miner.web is not None:
        if hasattr(miner.web, "pwd"):
            miner.web.pwd = up.password
            web_applied = True
        if hasattr(miner.web, "username"):
            miner.web.username = up.username

    logger.info(
        "Applied credentials to %s (user=%s, rpc=%s, web=%s)",
        getattr(miner, "ip", "?"),
        up.username,
        rpc_applied,
        web_applied,
    )


class PyAsicDriver(driver_pb2_grpc.DriverServicer):
    """PyASIC-based multi-manufacturer miner driver.

    Discovers, pairs, and manages miners from any manufacturer that pyasic
    supports. Capabilities are detected dynamically per miner instance.

    Implements the gRPC DriverServicer interface directly using proto types.
    """

    def __init__(
        self,
        config: PluginConfig,
        *,
        get_miner: GetMinerFunc | None = None,
    ) -> None:
        self._config = config
        self._get_miner_fn = get_miner or _default_get_miner()
        self._devices: dict[str, PyAsicDevice] = {}
        self._lock = asyncio.Lock()
        self._model_capabilities: dict[str, Capabilities] = {}

    # ========== Driver Info ==========

    @grpc_error_handler
    async def Handshake(
        self, request: Empty, context: grpc.ServicerContext
    ) -> driver_pb2.HandshakeResponse:
        return driver_pb2.HandshakeResponse(
            driver_name=_DRIVER_NAME, api_version=_API_VERSION
        )

    @grpc_error_handler
    async def DescribeDriver(
        self, request: Empty, context: grpc.ServicerContext
    ) -> driver_pb2.DescribeDriverResponse:
        return driver_pb2.DescribeDriverResponse(
            driver_name=_DRIVER_NAME,
            api_version=_API_VERSION,
            caps=driver_pb2.Capabilities(flags=dict(STATIC_BASE_CAPABILITIES)),
        )

    @grpc_error_handler
    async def GetDiscoveryPorts(
        self, request: Empty, context: grpc.ServicerContext
    ) -> driver_pb2.GetDiscoveryPortsResponse:
        return driver_pb2.GetDiscoveryPortsResponse(
            ports=[str(port) for port in sorted(_DISCOVERY_PORTS)]
        )

    # ========== Discovery & Pairing ==========

    @grpc_error_handler
    async def DiscoverDevice(
        self, request: driver_pb2.DiscoverDeviceRequest, context: grpc.ServicerContext
    ) -> driver_pb2.DiscoverDeviceResponse:
        try:
            port = int(request.port)
        except ValueError:
            raise InvalidConfigError(f"port must be a number, got '{request.port}'")
        ip_address = request.ip_address

        if port not in _DISCOVERY_PORTS:
            raise DeviceNotFoundError(ip_address)

        miner = await self._probe_miner(ip_address)

        make = getattr(miner, "make", None)
        if not make:
            raise DeviceNotFoundError(ip_address)

        make_str = str(make)
        family = MAKE_TO_FAMILY.get(make_str)
        if not family or family not in self._config.miners:
            raise DeviceNotFoundError(ip_address)

        enabled_fw = self._config.enabled_firmware(family)
        variant = detect_firmware_variant(miner, family)
        if variant not in enabled_fw:
            raise DeviceNotFoundError(ip_address)

        manufacturer = FIRMWARE_MANUFACTURER.get(variant, make_str)
        model = getattr(miner, "model", "") or ""
        firmware_version = getattr(miner, "fw_ver", "") or ""

        effective_port = port or _DEFAULT_PORT
        url_scheme = "https" if effective_port == _HTTPS_PORT else "http"

        logger.info("Discovered %s %s at %s", manufacturer, model, ip_address)
        return driver_pb2.DiscoverDeviceResponse(
            device=driver_pb2.DeviceInfo(
                host=ip_address,
                port=effective_port,
                url_scheme=url_scheme,
                serial_number="",
                model=model,
                manufacturer=manufacturer,
                mac_address="",
                firmware_version=firmware_version,
            )
        )

    @grpc_error_handler
    async def PairDevice(
        self, request: driver_pb2.PairDeviceRequest, context: grpc.ServicerContext
    ) -> driver_pb2.PairDeviceResponse:
        device_info = request.device
        secret = request.access

        miner = await self._probe_miner(device_info.host)
        _apply_credentials(miner, secret)

        try:
            data = await miner.get_data(exclude=["config"])
        except (OSError, asyncio.TimeoutError) as exc:
            raise DeviceUnavailableError(device_info.host, cause=exc) from exc
        except Exception as exc:
            raise AuthenticationFailedError(device_info.host, cause=exc) from exc

        if data is None:
            raise AuthenticationFailedError(device_info.host)

        try:
            await validate_write_access(miner)
        except (OSError, asyncio.TimeoutError) as exc:
            raise DeviceUnavailableError(device_info.host, cause=exc) from exc
        except Exception as exc:
            logger.warning("Write credential validation failed for %s: %s", device_info.host, exc)
            raise AuthenticationFailedError(device_info.host, cause=exc) from exc

        mac = getattr(data, "mac", "") or ""
        firmware = getattr(data, "fw_ver", "") or device_info.firmware_version

        logger.info("Paired %s at %s (mac=%s)", device_info.model, device_info.host, mac)
        return driver_pb2.PairDeviceResponse(
            device=driver_pb2.DeviceInfo(
                host=device_info.host,
                port=device_info.port,
                url_scheme=device_info.url_scheme,
                serial_number=device_info.serial_number,
                model=device_info.model,
                manufacturer=device_info.manufacturer,
                mac_address=mac,
                firmware_version=firmware,
            )
        )

    @grpc_error_handler
    async def GetDefaultCredentials(
        self, request: Empty, context: grpc.ServicerContext
    ) -> driver_pb2.GetDefaultCredentialsResponse:
        creds: list[driver_pb2.UsernamePassword] = []
        seen: set[tuple[str, str]] = set()
        for family_name, family_config in self._config.miners.items():
            family_creds = DEFAULT_CREDENTIALS.get(family_name, {})
            for variant_name, fw_config in family_config.firmware.items():
                if not fw_config.enabled:
                    continue
                for cred in family_creds.get(variant_name, []):
                    key = (cred.username, cred.password)
                    if key not in seen:
                        creds.append(cred)
                        seen.add(key)
        return driver_pb2.GetDefaultCredentialsResponse(credentials=creds)

    @grpc_error_handler
    async def GetCapabilitiesForModel(
        self, request: driver_pb2.GetCapabilitiesForModelRequest, context: grpc.ServicerContext
    ) -> driver_pb2.GetCapabilitiesForModelResponse:
        caps = dict(self._model_capabilities.get(request.model, {}))
        return driver_pb2.GetCapabilitiesForModelResponse(
            caps=driver_pb2.Capabilities(flags=caps)
        )

    # ========== Device Management ==========

    @grpc_error_handler
    async def NewDevice(
        self, request: driver_pb2.NewDeviceRequest, context: grpc.ServicerContext
    ) -> driver_pb2.NewDeviceResponse:
        device_info = request.info
        secret = request.secret
        device_id = request.device_id

        # Reject unsupported credential types, but allow unset (None) for
        # legacy/no-auth devices that the server sends with an empty SecretBundle.
        kind = secret.WhichOneof("kind")
        if kind is not None and kind != "user_pass":
            raise InvalidConfigError(
                f"Unsupported credential type '{kind}' — "
                "pyasic driver only supports username/password authentication"
            )

        miner = await self._try_probe_miner(device_info.host)
        if miner is not None:
            if kind == "user_pass":
                _apply_credentials(miner, secret)
            caps = build_capabilities(miner)
            if device_info.model:
                self._model_capabilities[device_info.model] = caps
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
            on_caps_update=self._update_model_capabilities,
        )

        async with self._lock:
            self._devices[device_id] = device

        logger.info(
            "Created device %s for %s at %s (connected=%s)",
            device_id, device_info.model, device_info.host, miner is not None,
        )
        return driver_pb2.NewDeviceResponse(device_id=device_id)

    @grpc_error_handler
    async def DescribeDevice(
        self, request: driver_pb2.DescribeDeviceRequest, context: grpc.ServicerContext
    ) -> driver_pb2.DescribeDeviceResponse:
        device = self._get_device(request.device_id)
        device_info, caps = device.describe_device()
        return driver_pb2.DescribeDeviceResponse(
            device=device_info,
            caps=driver_pb2.Capabilities(flags=caps),
        )

    @grpc_error_handler
    async def CloseDevice(
        self, request: driver_pb2.DeviceRef, context: grpc.ServicerContext
    ) -> Empty:
        async with self._lock:
            device = self._devices.pop(request.device_id, None)
        if device is None:
            raise DeviceNotFoundError(request.device_id)
        device.close()
        return Empty()

    # ========== Telemetry ==========

    @grpc_error_handler
    async def DeviceStatus(
        self, request: driver_pb2.DeviceRef, context: grpc.ServicerContext
    ) -> driver_pb2.DeviceMetrics:
        device = self._get_device(request.device_id)
        return await device.status()

    @grpc_error_handler
    async def GetErrors(
        self, request: driver_pb2.DeviceRef, context: grpc.ServicerContext
    ) -> driver_pb2.DeviceErrors:
        device = self._get_device(request.device_id)
        return await device.get_errors()

    # ========== Control ==========

    @grpc_error_handler
    async def StartMining(
        self, request: driver_pb2.DeviceRef, context: grpc.ServicerContext
    ) -> Empty:
        device = self._get_device(request.device_id)
        await device.start_mining()
        return Empty()

    @grpc_error_handler
    async def StopMining(
        self, request: driver_pb2.DeviceRef, context: grpc.ServicerContext
    ) -> Empty:
        device = self._get_device(request.device_id)
        await device.stop_mining()
        return Empty()

    @grpc_error_handler
    async def BlinkLED(
        self, request: driver_pb2.DeviceRef, context: grpc.ServicerContext
    ) -> Empty:
        device = self._get_device(request.device_id)
        await device.blink_led()
        return Empty()

    @grpc_error_handler
    async def Reboot(
        self, request: driver_pb2.DeviceRef, context: grpc.ServicerContext
    ) -> Empty:
        device = self._get_device(request.device_id)
        await device.reboot()
        return Empty()

    # ========== Configuration ==========

    @grpc_error_handler
    async def SetCoolingMode(
        self, request: driver_pb2.SetCoolingModeRequest, context: grpc.ServicerContext
    ) -> Empty:
        device = self._get_device(request.ref.device_id)
        raise UnsupportedCapabilityError("set_cooling_mode", device_id=device.id())

    @grpc_error_handler
    async def GetCoolingMode(
        self, request: driver_pb2.DeviceRef, context: grpc.ServicerContext
    ) -> driver_pb2.GetCoolingModeResponse:
        device = self._get_device(request.device_id)
        raise UnsupportedCapabilityError("get_cooling_mode", device_id=device.id())

    @grpc_error_handler
    async def SetPowerTarget(
        self, request: driver_pb2.SetPowerTargetRequest, context: grpc.ServicerContext
    ) -> Empty:
        device = self._get_device(request.ref.device_id)
        await device.set_power_target(request.performance_mode)
        return Empty()

    @grpc_error_handler
    async def UpdateMiningPools(
        self, request: driver_pb2.UpdateMiningPoolsRequest, context: grpc.ServicerContext
    ) -> Empty:
        device = self._get_device(request.ref.device_id)
        await device.update_mining_pools(list(request.pools))
        return Empty()

    @grpc_error_handler
    async def GetMiningPools(
        self, request: driver_pb2.GetMiningPoolsRequest, context: grpc.ServicerContext
    ) -> driver_pb2.GetMiningPoolsResponse:
        device = self._get_device(request.ref.device_id)
        pools = await device.get_mining_pools()
        return driver_pb2.GetMiningPoolsResponse(pools=pools)

    @grpc_error_handler
    async def UpdateMinerPassword(
        self, request: driver_pb2.UpdateMinerPasswordRequest, context: grpc.ServicerContext
    ) -> Empty:
        device = self._get_device(request.ref.device_id)
        raise UnsupportedCapabilityError("update_miner_password", device_id=device.id())

    # ========== Maintenance ==========

    @grpc_error_handler
    async def DownloadLogs(
        self, request: driver_pb2.DownloadLogsRequest, context: grpc.ServicerContext
    ) -> driver_pb2.DownloadLogsResponse:
        device = self._get_device(request.ref.device_id)
        raise UnsupportedCapabilityError("download_logs", device_id=device.id())

    @grpc_error_handler
    async def UpdateFirmware(
        self, request: driver_pb2.UpdateFirmwareRequest, context: grpc.ServicerContext
    ) -> Empty:
        device = self._get_device(request.ref.device_id)
        await device.firmware_update()
        return Empty()

    @grpc_error_handler
    async def Unpair(
        self, request: driver_pb2.DeviceRef, context: grpc.ServicerContext
    ) -> Empty:
        # No-op for pyasic devices
        self._get_device(request.device_id)
        return Empty()

    # ========== Not Implemented ==========

    @grpc_error_handler
    async def GetTimeSeriesData(
        self, request: driver_pb2.GetTimeSeriesDataRequest, context: grpc.ServicerContext
    ) -> driver_pb2.GetTimeSeriesDataResponse:
        device = self._get_device(request.ref.device_id)
        raise UnsupportedCapabilityError("get_time_series_data", device_id=device.id())

    @grpc_error_handler
    async def GetDeviceWebViewURL(
        self, request: driver_pb2.GetDeviceWebViewURLRequest, context: grpc.ServicerContext
    ) -> driver_pb2.GetDeviceWebViewURLResponse:
        device = self._get_device(request.ref.device_id)
        raise UnsupportedCapabilityError("get_device_web_view_url", device_id=device.id())

    @grpc_error_handler
    async def BatchStatus(
        self, request: driver_pb2.BatchStatusRequest, context: grpc.ServicerContext
    ) -> driver_pb2.StatusBatchResponse:
        raise UnsupportedCapabilityError("batch_status")

    # No @grpc_error_handler — streaming RPCs handle errors inline.
    async def Subscribe(
        self, request: driver_pb2.SubscribeRequest, context: grpc.ServicerContext
    ) -> None:
        await context.abort(grpc.StatusCode.UNIMPLEMENTED, "subscribe is not supported")

    # ========== Helpers ==========

    def _get_device(self, device_id: str) -> PyAsicDevice:
        """Look up a device by ID, raising DeviceNotFoundError if missing."""
        device = self._devices.get(device_id)
        if device is None:
            raise DeviceNotFoundError(device_id)
        return device

    def _update_model_capabilities(self, model: str, caps: Capabilities) -> None:
        """Callback for devices to update model capabilities on reconnect."""
        self._model_capabilities[model] = caps

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
