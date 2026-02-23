"""Protocol interfaces for Proto Fleet SDK.

This module contains all Protocol definitions for structural typing. Protocols define
the interface contracts that plugin implementations must satisfy.
"""

from proto_fleet_sdk.protocols.device import (
    Device,
    DeviceConfiguration,
    DeviceControl,
    DeviceCore,
    DeviceErrorReporting,
    DeviceMaintenance,
)
from proto_fleet_sdk.protocols.driver import (
    DefaultCredentialsProvider,
    Driver,
    ModelCapabilitiesProvider,
)

__all__ = [
    # Driver protocols
    "Driver",
    "DefaultCredentialsProvider",
    "ModelCapabilitiesProvider",
    # Device protocols
    "Device",
    "DeviceCore",
    "DeviceControl",
    "DeviceConfiguration",
    "DeviceMaintenance",
    "DeviceErrorReporting",
]
