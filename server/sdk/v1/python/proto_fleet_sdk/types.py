"""Core data types for Proto Fleet SDK.

This module defines immutable dataclasses for device information, driver identification,
pool configuration, and other core types used throughout the SDK.
"""

from __future__ import annotations

import re
from dataclasses import dataclass
from typing import TYPE_CHECKING, TypeAlias

from proto_fleet_sdk.errors import InvalidConfigError

if TYPE_CHECKING:
    from proto_fleet_sdk.protocols.device import Device

__all__ = [
    "DriverIdentifier",
    "Capabilities",
    "DeviceInfo",
    "FirmwareFile",
    "MiningPoolConfig",
    "ConfiguredPool",
    "NewDeviceResult",
]


@dataclass(frozen=True)
class DriverIdentifier:
    """Identifies a plugin driver with name and API version."""

    driver_name: str
    api_version: str

    def __post_init__(self) -> None:
        if not self.driver_name:
            raise InvalidConfigError("driver_name cannot be empty")
        if not self.api_version:
            raise InvalidConfigError("api_version cannot be empty")


# Type alias for capability flags
Capabilities: TypeAlias = dict[str, bool]

MAX_PORT = 65535
# RFC 3986: scheme = ALPHA *( ALPHA / DIGIT / "+" / "-" / "." )
_URL_SCHEME_RE = re.compile(r"^[a-zA-Z][a-zA-Z0-9+\-.]*$")


@dataclass(frozen=True)
class DeviceInfo:
    """Connection and identification information for a mining device."""

    host: str
    port: int
    url_scheme: str
    serial_number: str
    model: str
    manufacturer: str
    mac_address: str
    firmware_version: str

    def __post_init__(self) -> None:
        if not self.host:
            raise InvalidConfigError("host cannot be empty")
        if self.port <= 0 or self.port > MAX_PORT:
            raise InvalidConfigError(f"port must be between 1 and {MAX_PORT}, got {self.port}")

        if not self.url_scheme:
            raise InvalidConfigError("url_scheme cannot be empty")
        if not _URL_SCHEME_RE.match(self.url_scheme):
            raise InvalidConfigError(
                f"url_scheme must be a valid URI scheme (RFC 3986), got '{self.url_scheme}'"
            )


@dataclass(frozen=True)
class MiningPoolConfig:
    """Mining pool configuration for device pairing or updates."""

    priority: int
    url: str
    worker_name: str

    def __post_init__(self) -> None:
        if self.priority < 0:
            raise InvalidConfigError(f"priority must be non-negative, got {self.priority}")
        if not self.url:
            raise InvalidConfigError("url cannot be empty")
        if not self.worker_name:
            raise InvalidConfigError("worker_name cannot be empty")


@dataclass(frozen=True)
class ConfiguredPool:
    """Represents a mining pool currently configured on a device."""

    priority: int
    url: str
    username: str


@dataclass(frozen=True)
class FirmwareFile:
    """Firmware file reference for file-based firmware updates.

    The file_path points to a firmware file on the shared filesystem
    between the server and plugin processes.
    """

    file_path: str
    filename: str
    size: int


@dataclass(frozen=True)
class NewDeviceResult:
    """Result of creating a new device instance."""

    device: Device
