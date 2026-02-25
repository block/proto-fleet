"""Pytest configuration and fixtures for SDK tests."""

from __future__ import annotations

from datetime import datetime, timezone
from typing import Any

import grpc
import pytest

from proto_fleet_sdk.auth import SecretBundle, UsernamePassword
from proto_fleet_sdk.enums import (
    ComponentStatus,
    CoolingMode,
    DeviceType,
    HealthStatus,
    MetricKind,
    PerformanceMode,
)
from proto_fleet_sdk.error_codes import DeviceErrors
from proto_fleet_sdk.telemetry import (
    ComponentInfo,
    DeviceMetrics,
    FanMetrics,
    HashBoardMetrics,
    MetricValue,
)
from proto_fleet_sdk.types import (
    Capabilities,
    ConfiguredPool,
    DeviceInfo,
    DriverIdentifier,
    MiningPoolConfig,
    NewDeviceResult,
)


# -- Test driver and device implementations ----------------------------------
# Inline fixtures for integration tests. Examples are out of scope for the SDK PRs.


class StubDevice:
    """Minimal device implementation for integration tests."""

    def __init__(self, device_id: str, device_info: DeviceInfo, secret: SecretBundle) -> None:
        self._id = device_id
        self._info = device_info
        self._mining = False

    def id(self) -> str:
        return self._id

    async def describe_device(self, ctx: grpc.ServicerContext) -> tuple[DeviceInfo, Capabilities]:
        return self._info, {}

    async def status(self, ctx: grpc.ServicerContext) -> DeviceMetrics:
        hashboard = HashBoardMetrics(
            component_info=ComponentInfo(
                index=0, name="HashBoard 0", status=ComponentStatus.COMPONENT_STATUS_HEALTHY,
            ),
            hash_rate_hs=MetricValue(value=36.67e12, kind=MetricKind.METRIC_KIND_RATE),
            temp_c=MetricValue(value=65.0, kind=MetricKind.METRIC_KIND_GAUGE),
        )
        fan = FanMetrics(
            component_info=ComponentInfo(
                index=0, name="Fan 0", status=ComponentStatus.COMPONENT_STATUS_HEALTHY,
            ),
            rpm=MetricValue(value=4500.0, kind=MetricKind.METRIC_KIND_GAUGE),
        )
        return DeviceMetrics(
            device_id=self._id,
            timestamp=datetime.now(timezone.utc),
            health=HealthStatus.HEALTH_HEALTHY_ACTIVE
            if self._mining
            else HealthStatus.HEALTH_HEALTHY_INACTIVE,
            hashrate_hs=MetricValue(value=110.0e12, kind=MetricKind.METRIC_KIND_RATE),
            temp_c=MetricValue(value=65.0, kind=MetricKind.METRIC_KIND_GAUGE),
            fan_rpm=MetricValue(value=4500.0, kind=MetricKind.METRIC_KIND_GAUGE),
            power_w=MetricValue(value=3250.0, kind=MetricKind.METRIC_KIND_GAUGE),
            hash_boards=[hashboard, hashboard, hashboard],
            fan_metrics=[fan, fan],
        )

    async def close(self, ctx: grpc.ServicerContext) -> None:
        pass

    async def start_mining(self, ctx: grpc.ServicerContext) -> None:
        self._mining = True

    async def stop_mining(self, ctx: grpc.ServicerContext) -> None:
        self._mining = False

    async def blink_led(self, ctx: grpc.ServicerContext) -> None:
        pass

    async def reboot(self, ctx: grpc.ServicerContext) -> None:
        pass

    async def set_cooling_mode(self, ctx: grpc.ServicerContext, mode: CoolingMode) -> None:
        pass

    async def get_cooling_mode(self, ctx: grpc.ServicerContext) -> CoolingMode:
        return CoolingMode.COOLING_MODE_AIR_COOLED

    async def set_power_target(self, ctx: grpc.ServicerContext, performance_mode: PerformanceMode) -> None:
        pass

    async def update_mining_pools(self, ctx: grpc.ServicerContext, pools: list[MiningPoolConfig]) -> None:
        pass

    async def get_mining_pools(self, ctx: grpc.ServicerContext) -> list[ConfiguredPool]:
        return [ConfiguredPool(priority=0, url="stratum+tcp://pool.example:3333", username="worker")]

    async def download_logs(
        self, ctx: grpc.ServicerContext, since: Any = None, batch_log_uuid: Any = None,
    ) -> tuple[str, bool]:
        return "Mock log data", False

    async def firmware_update(self, ctx: grpc.ServicerContext) -> None:
        pass

    async def unpair(self, ctx: grpc.ServicerContext) -> None:
        pass

    async def update_miner_password(
        self, ctx: grpc.ServicerContext, current_password: str, new_password: str,
    ) -> None:
        pass

    async def get_errors(self, ctx: grpc.ServicerContext) -> DeviceErrors:
        return DeviceErrors(device_id=self._id, errors=())


class StubDriver:
    """Minimal driver implementation for integration tests."""

    async def handshake(self, ctx: grpc.ServicerContext) -> DriverIdentifier:
        return DriverIdentifier(driver_name="stub-plugin", api_version="v1")

    async def describe_driver(self, ctx: grpc.ServicerContext) -> tuple[DriverIdentifier, Capabilities]:
        return DriverIdentifier(driver_name="stub-plugin", api_version="v1"), {}

    async def discover_device(self, ctx: grpc.ServicerContext, ip_address: str, port: int) -> DeviceInfo:
        return DeviceInfo(
            host=ip_address, port=port, url_scheme="http", serial_number="",
            model="Stub Miner", manufacturer="Stub Corp",
            device_type=DeviceType.DEVICE_TYPE_ASIC, mac_address="", firmware_version="1.0.0",
        )

    async def pair_device(
        self, ctx: grpc.ServicerContext, device_info: DeviceInfo, secret: SecretBundle,
    ) -> DeviceInfo:
        return DeviceInfo(
            host=device_info.host, port=device_info.port, url_scheme=device_info.url_scheme,
            serial_number="SN123456789", model=device_info.model,
            manufacturer=device_info.manufacturer, device_type=device_info.device_type,
            mac_address="00:1A:2B:3C:4D:5E", firmware_version=device_info.firmware_version,
        )

    async def new_device(
        self, ctx: grpc.ServicerContext, device_id: str, device_info: DeviceInfo, secret: SecretBundle,
    ) -> NewDeviceResult:
        return NewDeviceResult(device=StubDevice(device_id, device_info, secret))


# -- Pytest fixtures ----------------------------------------------------------


@pytest.fixture
def mock_device_info() -> DeviceInfo:
    """Create mock DeviceInfo for testing."""
    return DeviceInfo(
        host="192.168.1.100",
        port=80,
        url_scheme="http",
        serial_number="SN123456789",
        model="Test Miner",
        manufacturer="Test Corp",
        device_type=DeviceType.DEVICE_TYPE_ASIC,
        mac_address="00:1A:2B:3C:4D:5E",
        firmware_version="1.0.0",
    )


@pytest.fixture
def mock_secret() -> SecretBundle:
    """Create mock SecretBundle for testing."""
    return SecretBundle(
        version="v1", kind=UsernamePassword(username="admin", password="admin123")
    )


@pytest.fixture
def mock_device_metrics() -> DeviceMetrics:
    """Create mock DeviceMetrics for testing."""
    hashboard = HashBoardMetrics(
        component_info=ComponentInfo(
            index=0, name="HashBoard 0", status=ComponentStatus.COMPONENT_STATUS_HEALTHY
        ),
        hash_rate_hs=MetricValue(value=36.67e12, kind=MetricKind.METRIC_KIND_RATE),
        temp_c=MetricValue(value=65.0, kind=MetricKind.METRIC_KIND_GAUGE),
    )

    return DeviceMetrics(
        device_id="test-device-1",
        timestamp=datetime.now(timezone.utc),
        health=HealthStatus.HEALTH_HEALTHY_ACTIVE,
        hashrate_hs=MetricValue(value=110.0e12, kind=MetricKind.METRIC_KIND_RATE),
        temp_c=MetricValue(value=65.0, kind=MetricKind.METRIC_KIND_GAUGE),
        power_w=MetricValue(value=3250.0, kind=MetricKind.METRIC_KIND_GAUGE),
        hash_boards=[hashboard, hashboard, hashboard],
    )
