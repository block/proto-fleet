"""gRPC servicer adapter for Proto Fleet plugins.

This module implements the gRPC Driver servicer that bridges between the gRPC protocol
and Python SDK types. It handles type conversions and error mapping.
"""

from __future__ import annotations

import asyncio
import functools
import logging
from collections.abc import Callable
from datetime import timedelta
from typing import Any, TypeVar

import grpc
from google.protobuf.empty_pb2 import Empty

from proto_fleet_sdk.auth import APIKey, BearerToken, SecretBundle, TLSClientCert, UsernamePassword
from proto_fleet_sdk.enums import (
    CoolingMode,
    PerformanceMode,
)
from proto_fleet_sdk.error_codes import DeviceError, DeviceErrors
from proto_fleet_sdk.errors import (
    AuthenticationFailedError,
    DeviceNotFoundError,
    DeviceUnavailableError,
    DriverShutdownError,
    InvalidConfigError,
    SDKError,
    UnsupportedCapabilityError,
)
from proto_fleet_sdk.generated.pb import driver_pb2, driver_pb2_grpc
from proto_fleet_sdk.protocols.driver import Driver
from proto_fleet_sdk.telemetry import (
    ASICMetrics,
    ComponentInfo,
    ControlBoardMetrics,
    DeviceMetrics,
    FanMetrics,
    HashBoardMetrics,
    MetricValue,
    PSUMetrics,
    SensorMetrics,
)
from proto_fleet_sdk.types import (
    Capabilities,
    ConfiguredPool,
    DeviceInfo,
    FirmwareFile,
    MiningPoolConfig,
)

__all__ = ["DriverServicer"]

F = TypeVar("F", bound=Callable[..., Any])

logger = logging.getLogger(__name__)

DEFAULT_MAX_TIME_SERIES_POINTS = 1000


def _rpc_handler(method: F) -> F:
    """Decorator that wraps RPC methods with SDK error handling."""
    @functools.wraps(method)
    async def wrapper(self: Any, request: Any, context: grpc.ServicerContext) -> Any:
        try:
            return await method(self, request, context)
        except asyncio.CancelledError:
            raise
        except Exception as e:
            await self._handle_sdk_error(e, context)
            raise
    return wrapper  # type: ignore[return-value]  # wrapper preserves F's signature via @functools.wraps


class DriverServicer(driver_pb2_grpc.DriverServicer):
    """gRPC Driver servicer implementation.

    This class implements all RPC methods defined in the Driver service, converting between
    protobuf messages and SDK types, and delegating to the plugin's Driver implementation.
    """

    def __init__(self, driver: Driver) -> None:
        """Initialize servicer with driver implementation.

        Args:
            driver: Driver implementation satisfying the Driver protocol
        """
        self.driver = driver
        self._devices: dict[str, Any] = {}
        self._lock = asyncio.Lock()

    async def _handle_sdk_error(
        self, e: Exception, grpc_context: grpc.ServicerContext
    ) -> None:
        """Map SDK errors to gRPC status codes.

        Note: This method raises an exception via grpc_context.abort()
        and never returns normally. No code after calling this method
        will execute.

        Args:
            e: Exception to map
            grpc_context: gRPC context to set error status on
        """
        if isinstance(e, UnsupportedCapabilityError):
            await grpc_context.abort(grpc.StatusCode.UNIMPLEMENTED, str(e))
        elif isinstance(e, DeviceNotFoundError):
            await grpc_context.abort(grpc.StatusCode.NOT_FOUND, str(e))
        elif isinstance(e, InvalidConfigError):
            await grpc_context.abort(grpc.StatusCode.INVALID_ARGUMENT, str(e))
        elif isinstance(e, DeviceUnavailableError):
            await grpc_context.abort(grpc.StatusCode.UNAVAILABLE, str(e))
        elif isinstance(e, AuthenticationFailedError):
            await grpc_context.abort(grpc.StatusCode.UNAUTHENTICATED, str(e))
        elif isinstance(e, DriverShutdownError):
            await grpc_context.abort(grpc.StatusCode.ABORTED, str(e))
        elif isinstance(e, SDKError):
            await grpc_context.abort(grpc.StatusCode.INTERNAL, str(e))
        else:
            logger.error("Unexpected error: %s", e, exc_info=True)
            await grpc_context.abort(grpc.StatusCode.INTERNAL, "internal error")

    def _get_device(self, device_id: str) -> Any:
        """Look up a device by ID, raising DeviceNotFoundError if missing."""
        device = self._devices.get(device_id)
        if device is None:
            raise DeviceNotFoundError(device_id)
        return device

    # ========== Type Conversion Helpers ==========

    @staticmethod
    def _proto_to_secret_bundle(secret: driver_pb2.SecretBundle) -> SecretBundle:
        """Convert protobuf SecretBundle to SDK SecretBundle."""
        # Determine which kind is set
        kind_field = secret.WhichOneof("kind")

        kind: APIKey | UsernamePassword | BearerToken | TLSClientCert
        if kind_field == "api_key":
            kind = APIKey(key=secret.api_key.key)
        elif kind_field == "user_pass":
            kind = UsernamePassword(
                username=secret.user_pass.username, password=secret.user_pass.password
            )
        elif kind_field == "bearer_token":
            kind = BearerToken(token=secret.bearer_token.token)
        elif kind_field == "tls_client_cert":
            kind = TLSClientCert(
                client_cert_pem=secret.tls_client_cert.client_cert_pem,
                key_pem=secret.tls_client_cert.key_pem,
                ca_cert_pem=secret.tls_client_cert.ca_cert_pem,
            )
        else:
            raise InvalidConfigError("secret: no valid authentication kind provided")

        # Convert TTL if present
        ttl = None
        if secret.HasField("ttl"):
            ttl = timedelta(seconds=secret.ttl.seconds, microseconds=secret.ttl.nanos // 1000)

        return SecretBundle(version=secret.version, kind=kind, ttl=ttl)

    @staticmethod
    def _proto_to_device_info(info: driver_pb2.DeviceInfo) -> DeviceInfo:
        """Convert protobuf DeviceInfo to SDK DeviceInfo."""
        return DeviceInfo(
            host=info.host,
            port=info.port,
            url_scheme=info.url_scheme,
            serial_number=info.serial_number,
            model=info.model,
            manufacturer=info.manufacturer,
            mac_address=info.mac_address,
            firmware_version=info.firmware_version,
        )

    @staticmethod
    def _device_info_to_proto(info: DeviceInfo) -> driver_pb2.DeviceInfo:
        """Convert SDK DeviceInfo to protobuf DeviceInfo."""
        return driver_pb2.DeviceInfo(
            host=info.host,
            port=info.port,
            url_scheme=info.url_scheme,
            serial_number=info.serial_number,
            model=info.model,
            manufacturer=info.manufacturer,
            mac_address=info.mac_address,
            firmware_version=info.firmware_version,
        )

    @staticmethod
    def _capabilities_to_proto(caps: Capabilities) -> driver_pb2.Capabilities:
        """Convert SDK Capabilities to protobuf Capabilities."""
        return driver_pb2.Capabilities(flags=caps)

    @staticmethod
    def _proto_to_capabilities(caps: driver_pb2.Capabilities) -> Capabilities:
        """Convert protobuf Capabilities to SDK Capabilities."""
        return dict(caps.flags)

    @staticmethod
    def _metric_value_to_proto(
        metric: MetricValue,
    ) -> driver_pb2.MetricValue:
        """Convert SDK MetricValue to protobuf MetricValue."""
        pb_metric = driver_pb2.MetricValue(value=metric.value, kind=metric.kind)  # type: ignore[arg-type]  # SDK IntEnum values match protobuf enum values by design

        if metric.metadata:
            metadata = driver_pb2.MetricValueMetaData()
            if metric.metadata.window is not None:
                metadata.window.seconds = int(metric.metadata.window.total_seconds())
                metadata.window.nanos = (
                    metric.metadata.window.microseconds * 1000
                ) % 1_000_000_000
            if metric.metadata.min is not None:
                metadata.min = metric.metadata.min
            if metric.metadata.max is not None:
                metadata.max = metric.metadata.max
            if metric.metadata.avg is not None:
                metadata.avg = metric.metadata.avg
            if metric.metadata.std_dev is not None:
                metadata.std_dev = metric.metadata.std_dev
            if metric.metadata.timestamp:
                metadata.timestamp.FromDatetime(metric.metadata.timestamp)
            pb_metric.metadata.CopyFrom(metadata)

        return pb_metric

    @staticmethod
    def _component_info_to_proto(info: ComponentInfo) -> driver_pb2.ComponentInfo:
        """Convert SDK ComponentInfo to protobuf ComponentInfo."""
        pb_info = driver_pb2.ComponentInfo(
            index=info.index, name=info.name, status=info.status  # type: ignore[arg-type]  # SDK IntEnum → protobuf enum
        )
        if info.status_reason:
            pb_info.status_reason = info.status_reason
        if info.timestamp:
            pb_info.timestamp.FromDatetime(info.timestamp)
        return pb_info

    @staticmethod
    def _device_metrics_to_proto(metrics: DeviceMetrics) -> driver_pb2.DeviceMetrics:
        """Convert SDK DeviceMetrics to protobuf DeviceMetrics."""
        pb_metrics = driver_pb2.DeviceMetrics(
            device_id=metrics.device_id, health=metrics.health  # type: ignore[arg-type]  # SDK IntEnum → protobuf enum
        )
        pb_metrics.timestamp.FromDatetime(metrics.timestamp)

        if metrics.health_reason:
            pb_metrics.health_reason = metrics.health_reason

        # Device-level metrics
        if metrics.hashrate_hs:
            pb_metrics.hashrate_hs.CopyFrom(DriverServicer._metric_value_to_proto(metrics.hashrate_hs))
        if metrics.temp_c:
            pb_metrics.temp_c.CopyFrom(DriverServicer._metric_value_to_proto(metrics.temp_c))
        if metrics.fan_rpm:
            pb_metrics.fan_rpm.CopyFrom(DriverServicer._metric_value_to_proto(metrics.fan_rpm))
        if metrics.power_w:
            pb_metrics.power_w.CopyFrom(DriverServicer._metric_value_to_proto(metrics.power_w))
        if metrics.efficiency_jh:
            pb_metrics.efficiency_jh.CopyFrom(DriverServicer._metric_value_to_proto(metrics.efficiency_jh))

        # Component metrics
        for hashboard in metrics.hash_boards:
            pb_metrics.hash_boards.append(DriverServicer._hashboard_to_proto(hashboard))
        for psu in metrics.psu_metrics:
            pb_metrics.psu_metrics.append(DriverServicer._psu_to_proto(psu))
        for cb in metrics.control_board_metrics:
            pb_metrics.control_board_metrics.append(DriverServicer._control_board_to_proto(cb))
        for fan in metrics.fan_metrics:
            pb_metrics.fan_metrics.append(DriverServicer._fan_to_proto(fan))
        for sensor in metrics.sensor_metrics:
            pb_metrics.sensor_metrics.append(DriverServicer._sensor_to_proto(sensor))

        return pb_metrics

    @staticmethod
    def _hashboard_to_proto(hb: HashBoardMetrics) -> driver_pb2.HashBoardMetrics:
        """Convert SDK HashBoardMetrics to protobuf."""
        pb_hb = driver_pb2.HashBoardMetrics(
            component_info=DriverServicer._component_info_to_proto(hb.component_info)
        )
        if hb.serial_number:
            pb_hb.serial_number = hb.serial_number
        if hb.hash_rate_hs:
            pb_hb.hash_rate_hs.CopyFrom(DriverServicer._metric_value_to_proto(hb.hash_rate_hs))
        if hb.temp_c:
            pb_hb.temp_c.CopyFrom(DriverServicer._metric_value_to_proto(hb.temp_c))
        if hb.voltage_v:
            pb_hb.voltage_v.CopyFrom(DriverServicer._metric_value_to_proto(hb.voltage_v))
        if hb.current_a:
            pb_hb.current_a.CopyFrom(DriverServicer._metric_value_to_proto(hb.current_a))
        if hb.inlet_temp_c:
            pb_hb.inlet_temp_c.CopyFrom(DriverServicer._metric_value_to_proto(hb.inlet_temp_c))
        if hb.outlet_temp_c:
            pb_hb.outlet_temp_c.CopyFrom(DriverServicer._metric_value_to_proto(hb.outlet_temp_c))
        if hb.ambient_temp_c:
            pb_hb.ambient_temp_c.CopyFrom(DriverServicer._metric_value_to_proto(hb.ambient_temp_c))
        if hb.chip_count is not None:
            pb_hb.chip_count = hb.chip_count
        if hb.chip_frequency_mhz:
            pb_hb.chip_frequency_mhz.CopyFrom(DriverServicer._metric_value_to_proto(hb.chip_frequency_mhz))

        for asic in hb.asics:
            pb_hb.asics.append(DriverServicer._asic_to_proto(asic))
        for fan in hb.fan_metrics:
            pb_hb.fan_metrics.append(DriverServicer._fan_to_proto(fan))

        return pb_hb

    @staticmethod
    def _asic_to_proto(asic: ASICMetrics) -> driver_pb2.ASICMetrics:
        """Convert SDK ASICMetrics to protobuf."""
        pb_asic = driver_pb2.ASICMetrics(
            component_info=DriverServicer._component_info_to_proto(asic.component_info)
        )
        if asic.temp_c:
            pb_asic.temp_c.CopyFrom(DriverServicer._metric_value_to_proto(asic.temp_c))
        if asic.frequency_mhz:
            pb_asic.frequency_mhz.CopyFrom(DriverServicer._metric_value_to_proto(asic.frequency_mhz))
        if asic.voltage_v:
            pb_asic.voltage_v.CopyFrom(DriverServicer._metric_value_to_proto(asic.voltage_v))
        if asic.hashrate_hs:
            pb_asic.hashrate_hs.CopyFrom(DriverServicer._metric_value_to_proto(asic.hashrate_hs))
        return pb_asic

    @staticmethod
    def _psu_to_proto(psu: PSUMetrics) -> driver_pb2.PSUMetrics:
        """Convert SDK PSUMetrics to protobuf."""
        pb_psu = driver_pb2.PSUMetrics(
            component_info=DriverServicer._component_info_to_proto(psu.component_info)
        )
        if psu.output_power_w:
            pb_psu.output_power_w.CopyFrom(DriverServicer._metric_value_to_proto(psu.output_power_w))
        if psu.output_voltage_v:
            pb_psu.output_voltage_v.CopyFrom(DriverServicer._metric_value_to_proto(psu.output_voltage_v))
        if psu.output_current_a:
            pb_psu.output_current_a.CopyFrom(DriverServicer._metric_value_to_proto(psu.output_current_a))
        if psu.input_power_w:
            pb_psu.input_power_w.CopyFrom(DriverServicer._metric_value_to_proto(psu.input_power_w))
        if psu.input_voltage_v:
            pb_psu.input_voltage_v.CopyFrom(DriverServicer._metric_value_to_proto(psu.input_voltage_v))
        if psu.input_current_a:
            pb_psu.input_current_a.CopyFrom(DriverServicer._metric_value_to_proto(psu.input_current_a))
        if psu.hotspot_temp_c:
            pb_psu.hotspot_temp_c.CopyFrom(DriverServicer._metric_value_to_proto(psu.hotspot_temp_c))
        if psu.efficiency_percent:
            pb_psu.efficiency_percent.CopyFrom(
                DriverServicer._metric_value_to_proto(psu.efficiency_percent)
            )

        for fan in psu.fan_metrics:
            pb_psu.fan_metrics.append(DriverServicer._fan_to_proto(fan))

        return pb_psu

    @staticmethod
    def _fan_to_proto(fan: FanMetrics) -> driver_pb2.FanMetrics:
        """Convert SDK FanMetrics to protobuf."""
        pb_fan = driver_pb2.FanMetrics(
            component_info=DriverServicer._component_info_to_proto(fan.component_info)
        )
        if fan.rpm:
            pb_fan.rpm.CopyFrom(DriverServicer._metric_value_to_proto(fan.rpm))
        if fan.temp_c:
            pb_fan.temp_c.CopyFrom(DriverServicer._metric_value_to_proto(fan.temp_c))
        if fan.percent:
            pb_fan.percent.CopyFrom(DriverServicer._metric_value_to_proto(fan.percent))
        return pb_fan

    @staticmethod
    def _control_board_to_proto(
        cb: ControlBoardMetrics,
    ) -> driver_pb2.ControlBoardMetrics:
        """Convert SDK ControlBoardMetrics to protobuf."""
        return driver_pb2.ControlBoardMetrics(
            component_info=DriverServicer._component_info_to_proto(cb.component_info)
        )

    @staticmethod
    def _sensor_to_proto(sensor: SensorMetrics) -> driver_pb2.SensorMetrics:
        """Convert SDK SensorMetrics to protobuf."""
        pb_sensor = driver_pb2.SensorMetrics(
            component_info=DriverServicer._component_info_to_proto(sensor.component_info)
        )
        if sensor.type:
            pb_sensor.type = sensor.type
        if sensor.unit:
            pb_sensor.unit = sensor.unit
        if sensor.value:
            pb_sensor.value.CopyFrom(DriverServicer._metric_value_to_proto(sensor.value))
        return pb_sensor

    @staticmethod
    def _proto_to_mining_pool(pool: driver_pb2.MiningPool) -> MiningPoolConfig:
        """Convert protobuf MiningPool to SDK MiningPoolConfig."""
        return MiningPoolConfig(
            priority=pool.priority,
            url=pool.url,
            worker_name=pool.worker_name,
        )

    @staticmethod
    def _configured_pool_to_proto(pool: ConfiguredPool) -> driver_pb2.ConfiguredPool:
        """Convert SDK ConfiguredPool to protobuf ConfiguredPool."""
        return driver_pb2.ConfiguredPool(
            priority=pool.priority,
            url=pool.url,
            username=pool.username,
        )

    @staticmethod
    def _device_error_to_proto(error: DeviceError) -> driver_pb2.DeviceError:
        """Convert SDK DeviceError to protobuf DeviceError."""
        pb_error = driver_pb2.DeviceError(
            miner_error=error.miner_error,  # type: ignore[arg-type]  # SDK IntEnum → protobuf enum
            cause_summary=error.cause_summary,
            recommended_action=error.recommended_action,
            severity=error.severity,  # type: ignore[arg-type]  # SDK IntEnum → protobuf enum
            device_id=error.device_id,
            impact=error.impact,
            summary=error.summary,
            component_type=error.component_type,  # type: ignore[arg-type]  # SDK IntEnum → protobuf enum
        )

        if error.first_seen_at:
            pb_error.first_seen_at.FromDatetime(error.first_seen_at)
        if error.last_seen_at:
            pb_error.last_seen_at.FromDatetime(error.last_seen_at)
        if error.closed_at:
            pb_error.closed_at.FromDatetime(error.closed_at)
        if error.component_id:
            pb_error.component_id = error.component_id

        for key, value in error.vendor_attributes.items():
            pb_error.vendor_attributes[key] = value

        return pb_error

    @staticmethod
    def _device_errors_to_proto(errors: DeviceErrors) -> driver_pb2.DeviceErrors:
        """Convert SDK DeviceErrors to protobuf DeviceErrors."""
        pb_errors = driver_pb2.DeviceErrors(device_id=errors.device_id)
        for error in errors.errors:
            pb_errors.errors.append(DriverServicer._device_error_to_proto(error))
        return pb_errors

    # ========== RPC Method Implementations ==========

    @_rpc_handler
    async def Handshake(
        self, request: Empty, context: grpc.ServicerContext
    ) -> driver_pb2.HandshakeResponse:
        ident = await self.driver.handshake(context)
        return driver_pb2.HandshakeResponse(
            driver_name=ident.driver_name, api_version=ident.api_version
        )

    @_rpc_handler
    async def DescribeDriver(
        self, request: Empty, context: grpc.ServicerContext
    ) -> driver_pb2.DescribeDriverResponse:
        ident, caps = await self.driver.describe_driver(context)
        return driver_pb2.DescribeDriverResponse(
            driver_name=ident.driver_name,
            api_version=ident.api_version,
            caps=self._capabilities_to_proto(caps),
        )

    @_rpc_handler
    async def GetDiscoveryPorts(
        self, request: Empty, context: grpc.ServicerContext
    ) -> driver_pb2.GetDiscoveryPortsResponse:
        del request

        if not hasattr(self.driver, "get_discovery_ports"):
            raise UnsupportedCapabilityError("get_discovery_ports")

        ports = await self.driver.get_discovery_ports(context)
        return driver_pb2.GetDiscoveryPortsResponse(ports=ports)

    @_rpc_handler
    async def DiscoverDevice(
        self, request: driver_pb2.DiscoverDeviceRequest, context: grpc.ServicerContext
    ) -> driver_pb2.DiscoverDeviceResponse:
        device_info = await self.driver.discover_device(
            context, request.ip_address, int(request.port)
        )
        return driver_pb2.DiscoverDeviceResponse(
            device=self._device_info_to_proto(device_info)
        )

    @_rpc_handler
    async def PairDevice(
        self, request: driver_pb2.PairDeviceRequest, context: grpc.ServicerContext
    ) -> driver_pb2.PairDeviceResponse:
        device_info = self._proto_to_device_info(request.device)
        secret = self._proto_to_secret_bundle(request.access)
        updated_info = await self.driver.pair_device(context, device_info, secret)
        return driver_pb2.PairDeviceResponse(device=self._device_info_to_proto(updated_info))

    @_rpc_handler
    async def NewDevice(
        self, request: driver_pb2.NewDeviceRequest, context: grpc.ServicerContext
    ) -> driver_pb2.NewDeviceResponse:
        device_info = self._proto_to_device_info(request.info)
        secret = self._proto_to_secret_bundle(request.secret)
        result = await self.driver.new_device(context, request.device_id, device_info, secret)

        device = result.device
        device_id = device.id()
        if device_id != request.device_id:
            raise InvalidConfigError(
                f"device ID mismatch: expected {request.device_id}, got {device_id}"
            )

        async with self._lock:
            self._devices[request.device_id] = device

        return driver_pb2.NewDeviceResponse(device_id=request.device_id)

    @_rpc_handler
    async def DeviceStatus(
        self, request: driver_pb2.DeviceRef, context: grpc.ServicerContext
    ) -> driver_pb2.DeviceMetrics:
        device = self._get_device(request.device_id)
        metrics = await device.status(context)
        return self._device_metrics_to_proto(metrics)

    @_rpc_handler
    async def StartMining(
        self, request: driver_pb2.DeviceRef, context: grpc.ServicerContext
    ) -> Empty:
        device = self._get_device(request.device_id)
        await device.start_mining(context)
        return Empty()

    @_rpc_handler
    async def StopMining(
        self, request: driver_pb2.DeviceRef, context: grpc.ServicerContext
    ) -> Empty:
        device = self._get_device(request.device_id)
        await device.stop_mining(context)
        return Empty()

    # Additional RPC methods follow similar patterns...
    # (Continuing with remaining methods for brevity - they follow the same structure)

    @_rpc_handler
    async def CloseDevice(
        self, request: driver_pb2.DeviceRef, context: grpc.ServicerContext
    ) -> Empty:
        async with self._lock:
            device = self._devices.pop(request.device_id, None)
        if device is None:
            raise DeviceNotFoundError(request.device_id)
        await device.close(context)
        return Empty()

    @_rpc_handler
    async def DescribeDevice(
        self, request: driver_pb2.DescribeDeviceRequest, context: grpc.ServicerContext
    ) -> driver_pb2.DescribeDeviceResponse:
        device = self._get_device(request.device_id)
        device_info, caps = await device.describe_device(context)
        return driver_pb2.DescribeDeviceResponse(
            device=self._device_info_to_proto(device_info),
            caps=self._capabilities_to_proto(caps),
        )

    @_rpc_handler
    async def BlinkLED(
        self, request: driver_pb2.DeviceRef, context: grpc.ServicerContext
    ) -> Empty:
        device = self._get_device(request.device_id)
        await device.blink_led(context)
        return Empty()

    @_rpc_handler
    async def Reboot(
        self, request: driver_pb2.DeviceRef, context: grpc.ServicerContext
    ) -> Empty:
        device = self._get_device(request.device_id)
        await device.reboot(context)
        return Empty()

    @_rpc_handler
    async def GetErrors(
        self, request: driver_pb2.DeviceRef, context: grpc.ServicerContext
    ) -> driver_pb2.DeviceErrors:
        device = self._get_device(request.device_id)
        errors = await device.get_errors(context)
        return self._device_errors_to_proto(errors)

    @_rpc_handler
    async def SetCoolingMode(
        self, request: driver_pb2.SetCoolingModeRequest, context: grpc.ServicerContext
    ) -> Empty:
        device = self._get_device(request.ref.device_id)
        cooling_mode = CoolingMode(request.mode)
        await device.set_cooling_mode(context, cooling_mode)
        return Empty()

    @_rpc_handler
    async def GetCoolingMode(
        self, request: driver_pb2.DeviceRef, context: grpc.ServicerContext
    ) -> driver_pb2.GetCoolingModeResponse:
        device = self._get_device(request.device_id)
        mode = await device.get_cooling_mode(context)
        return driver_pb2.GetCoolingModeResponse(mode=mode)

    @_rpc_handler
    async def SetPowerTarget(
        self, request: driver_pb2.SetPowerTargetRequest, context: grpc.ServicerContext
    ) -> Empty:
        device = self._get_device(request.ref.device_id)
        performance_mode = PerformanceMode(request.performance_mode)
        await device.set_power_target(context, performance_mode)
        return Empty()

    @_rpc_handler
    async def UpdateMiningPools(
        self, request: driver_pb2.UpdateMiningPoolsRequest, context: grpc.ServicerContext
    ) -> Empty:
        device = self._get_device(request.ref.device_id)
        pools = [self._proto_to_mining_pool(p) for p in request.pools]
        await device.update_mining_pools(context, pools)
        return Empty()

    @_rpc_handler
    async def GetMiningPools(
        self, request: driver_pb2.GetMiningPoolsRequest, context: grpc.ServicerContext
    ) -> driver_pb2.GetMiningPoolsResponse:
        device = self._get_device(request.ref.device_id)
        pools = await device.get_mining_pools(context)
        pb_pools = [self._configured_pool_to_proto(p) for p in pools]
        return driver_pb2.GetMiningPoolsResponse(pools=pb_pools)

    @_rpc_handler
    async def DownloadLogs(
        self, request: driver_pb2.DownloadLogsRequest, context: grpc.ServicerContext
    ) -> driver_pb2.DownloadLogsResponse:
        device = self._get_device(request.ref.device_id)

        since = None
        if request.HasField("since"):
            since = request.since.ToDatetime()

        batch_log_uuid = request.batch_log_uuid if request.batch_log_uuid else None

        log_data, more_data = await device.download_logs(context, since, batch_log_uuid)
        return driver_pb2.DownloadLogsResponse(log_data=log_data, more_data=more_data)

    @_rpc_handler
    async def UpdateFirmware(
        self, request: driver_pb2.UpdateFirmwareRequest, context: grpc.ServicerContext
    ) -> Empty:
        device = self._get_device(request.ref.device_id)
        fw = request.firmware
        firmware = FirmwareFile(
            file_path=fw.file_path,
            filename=fw.original_filename,
            size=fw.file_size,
        )
        await device.firmware_update(context, firmware)
        return Empty()

    @_rpc_handler
    async def Unpair(
        self, request: driver_pb2.DeviceRef, context: grpc.ServicerContext
    ) -> Empty:
        device = self._get_device(request.device_id)
        await device.unpair(context)
        return Empty()

    @_rpc_handler
    async def UpdateMinerPassword(
        self, request: driver_pb2.UpdateMinerPasswordRequest, context: grpc.ServicerContext
    ) -> Empty:
        device = self._get_device(request.ref.device_id)
        await device.update_miner_password(context, request.current_password, request.new_password)
        return Empty()

    @_rpc_handler
    async def GetDeviceWebViewURL(
        self, request: driver_pb2.GetDeviceWebViewURLRequest, context: grpc.ServicerContext
    ) -> driver_pb2.GetDeviceWebViewURLResponse:
        device = self._get_device(request.ref.device_id)

        if not hasattr(device, "get_device_web_view_url"):
            raise UnsupportedCapabilityError("get_device_web_view_url")

        url = await device.get_device_web_view_url(context)
        return driver_pb2.GetDeviceWebViewURLResponse(url=url)

    @_rpc_handler
    async def GetTimeSeriesData(
        self, request: driver_pb2.GetTimeSeriesDataRequest, context: grpc.ServicerContext
    ) -> driver_pb2.GetTimeSeriesDataResponse:
        device = self._get_device(request.ref.device_id)

        if not hasattr(device, "get_time_series_data"):
            raise UnsupportedCapabilityError("get_time_series_data")

        start_time = request.start_time.ToDatetime() if request.HasField("start_time") else None
        end_time = request.end_time.ToDatetime() if request.HasField("end_time") else None
        granularity = None
        if request.HasField("granularity"):
            granularity = timedelta(
                seconds=request.granularity.seconds,
                microseconds=request.granularity.nanos // 1000
            )

        series, next_token = await device.get_time_series_data(
            context,
            list(request.metric_names),
            start_time,
            end_time,
            granularity,
            request.max_points if request.max_points else DEFAULT_MAX_TIME_SERIES_POINTS,
            request.page_token if request.page_token else None,
        )

        pb_series = [self._device_metrics_to_proto(m) for m in series]
        return driver_pb2.GetTimeSeriesDataResponse(
            series=pb_series,
            next_page_token=next_token if next_token else "",
        )

    @_rpc_handler
    async def GetDefaultCredentials(
        self, request: Empty, context: grpc.ServicerContext
    ) -> driver_pb2.GetDefaultCredentialsResponse:

        if not hasattr(self.driver, "get_default_credentials"):
            raise UnsupportedCapabilityError("get_default_credentials")

        credentials = await self.driver.get_default_credentials(context)

        pb_creds = []
        for cred in credentials:
            pb_creds.append(
                driver_pb2.UsernamePassword(username=cred.username, password=cred.password)
            )

        return driver_pb2.GetDefaultCredentialsResponse(credentials=pb_creds)

    @_rpc_handler
    async def GetCapabilitiesForModel(
        self, request: driver_pb2.GetCapabilitiesForModelRequest, context: grpc.ServicerContext
    ) -> driver_pb2.GetCapabilitiesForModelResponse:

        if not hasattr(self.driver, "get_capabilities_for_model"):
            raise UnsupportedCapabilityError("get_capabilities_for_model")

        caps = await self.driver.get_capabilities_for_model(context, request.model)
        return driver_pb2.GetCapabilitiesForModelResponse(
            caps=self._capabilities_to_proto(caps)
        )
