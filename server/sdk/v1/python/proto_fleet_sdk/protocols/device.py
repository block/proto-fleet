"""Device protocol interfaces.

This module defines all device-level protocols that device implementations should satisfy.
The Device protocol is a composite of multiple sub-protocols for different functional areas.
"""

from __future__ import annotations

from datetime import datetime
from typing import TYPE_CHECKING, Protocol

import grpc

from proto_fleet_sdk.enums import CoolingMode, PerformanceMode
from proto_fleet_sdk.types import (
    Capabilities,
    ConfiguredPool,
    DeviceInfo,
    FirmwareFile,
    MiningPoolConfig,
)

if TYPE_CHECKING:
    from proto_fleet_sdk.error_codes import DeviceErrors
    from proto_fleet_sdk.telemetry.metrics import DeviceMetrics

__all__ = [
    "DeviceCore",
    "DeviceControl",
    "DeviceConfiguration",
    "DeviceMaintenance",
    "DeviceErrorReporting",
    "Device",
]


class DeviceCore(Protocol):
    """Core device interface methods required for all devices."""

    def id(self) -> str:
        ...

    async def describe_device(self, ctx: grpc.ServicerContext) -> tuple[DeviceInfo, Capabilities]:
        ...

    async def status(self, ctx: grpc.ServicerContext) -> DeviceMetrics:
        """Get current device telemetry.

        Returns a complete DeviceMetrics snapshot with health status, aggregated
        metrics, and component-level telemetry.
        """
        ...

    async def close(self, ctx: grpc.ServicerContext) -> None:
        """Close device and release resources.

        Called when the device is being removed from the fleet or the plugin is
        shutting down. Should close connections, cancel background tasks, and
        clean up any other resources.
        """
        ...


class DeviceControl(Protocol):
    """Device control operations for mining and diagnostics."""

    async def start_mining(self, ctx: grpc.ServicerContext) -> None:
        ...

    async def stop_mining(self, ctx: grpc.ServicerContext) -> None:
        ...

    async def blink_led(self, ctx: grpc.ServicerContext) -> None:
        ...

    async def reboot(self, ctx: grpc.ServicerContext) -> None:
        ...


class DeviceConfiguration(Protocol):
    """Device configuration operations."""

    async def set_cooling_mode(self, ctx: grpc.ServicerContext, mode: CoolingMode) -> None:
        ...

    async def get_cooling_mode(self, ctx: grpc.ServicerContext) -> CoolingMode:
        ...

    async def set_power_target(self, ctx: grpc.ServicerContext, performance_mode: PerformanceMode) -> None:
        ...

    async def update_mining_pools(self, ctx: grpc.ServicerContext, pools: list[MiningPoolConfig]) -> None:
        ...

    async def get_mining_pools(self, ctx: grpc.ServicerContext) -> list[ConfiguredPool]:
        ...

    async def update_miner_password(
        self, ctx: grpc.ServicerContext, current_password: str, new_password: str
    ) -> None:
        ...


class DeviceMaintenance(Protocol):
    """Device maintenance operations."""

    async def download_logs(
        self, ctx: grpc.ServicerContext, since: datetime | None = None, batch_log_uuid: str | None = None
    ) -> tuple[str, bool]:
        """Download device logs.

        Returns (log_data, more_data) where more_data indicates if additional
        logs are available with the same batch_log_uuid.
        """
        ...

    async def firmware_update(self, ctx: grpc.ServicerContext, firmware: FirmwareFile) -> None:
        ...

    async def unpair(self, ctx: grpc.ServicerContext) -> None:
        """Unpair device and clear credentials.

        Clears any stored credentials or configuration related to the
        Fleet management system from the device.
        """
        ...


class DeviceErrorReporting(Protocol):
    """Device error reporting interface."""

    async def get_errors(self, ctx: grpc.ServicerContext) -> DeviceErrors:
        ...


class Device(
    DeviceCore,
    DeviceControl,
    DeviceConfiguration,
    DeviceMaintenance,
    DeviceErrorReporting,
    Protocol,
):
    """Complete device interface composing all sub-protocols.

    A device implementation should satisfy all these protocols. Methods that are not
    supported by the hardware should raise UnsupportedCapabilityError.
    """

    pass
