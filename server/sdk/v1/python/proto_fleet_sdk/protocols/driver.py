"""Driver protocol interface.

This module defines the Driver protocol and optional driver-level interfaces.
"""

from __future__ import annotations

from typing import Protocol

import grpc

from proto_fleet_sdk.auth import SecretBundle, UsernamePassword
from proto_fleet_sdk.types import Capabilities, DeviceInfo, DriverIdentifier, NewDeviceResult

__all__ = ["Driver", "DefaultCredentialsProvider", "ModelCapabilitiesProvider"]


class Driver(Protocol):
    """Main driver interface that all plugins must implement.

    The Driver protocol defines the entry point for plugin functionality, including
    driver identification, device discovery, pairing, and device instance management.
    """

    async def handshake(self, ctx: grpc.ServicerContext) -> DriverIdentifier:
        ...

    async def describe_driver(self, ctx: grpc.ServicerContext) -> tuple[DriverIdentifier, Capabilities]:
        ...

    async def discover_device(self, ctx: grpc.ServicerContext, ip_address: str, port: int) -> DeviceInfo:
        """Discover a device at the given IP address and port.

        Probes the device to gather identification information without authentication.

        Raises:
            DeviceNotFoundError: If no compatible device found at this address
            DeviceUnavailableError: If device is unreachable or not responding
        """
        ...

    async def pair_device(
        self, ctx: grpc.ServicerContext, device_info: DeviceInfo, secret: SecretBundle
    ) -> DeviceInfo:
        """Pair with a discovered device using provided credentials.

        Authenticates with the device and retrieves full device information including
        serial number, MAC address, and firmware version.

        Raises:
            AuthenticationFailedError: If credentials are invalid
            DeviceUnavailableError: If device is unreachable
            InvalidConfigError: If device configuration is incompatible
        """
        ...

    async def new_device(
        self, ctx: grpc.ServicerContext, device_id: str, device_info: DeviceInfo, secret: SecretBundle
    ) -> NewDeviceResult:
        """Create a new device instance for ongoing telemetry collection.

        Instantiates a Device object that maintains a persistent connection (or connection
        pool) to the mining hardware for all subsequent telemetry, control, and
        configuration operations.
        """
        ...


class DefaultCredentialsProvider(Protocol):
    """Optional interface for providing default credentials during pairing.

    Drivers can implement this to enable auto-authentication with common default
    credentials when user doesn't provide explicit credentials.
    """

    async def get_default_credentials(self, ctx: grpc.ServicerContext) -> list[UsernamePassword]:
        ...


class ModelCapabilitiesProvider(Protocol):
    """Optional interface for providing model-specific capability overrides.

    Drivers can implement this to return different capabilities based on device model,
    allowing for fine-grained feature support across product lines.
    """

    async def get_capabilities_for_model(self, ctx: grpc.ServicerContext, model: str) -> Capabilities:
        ...
