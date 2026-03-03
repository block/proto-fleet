"""Proto Fleet Python SDK.

This SDK enables developers to create mining device plugins in Python that integrate
seamlessly with the Proto Fleet management system.

Quick Start:
    >>> from proto_fleet_sdk import Driver, Device, DeviceMetrics
    >>> from proto_fleet_sdk.server import PluginServer
    >>>
    >>> class MyDriver:
    >>>     async def handshake(self, ctx):
    >>>         return DriverIdentifier(driver_name="my-plugin", api_version="v1")
    >>>
    >>> if __name__ == "__main__":
    >>>     driver = MyDriver()
    >>>     server = PluginServer(driver, port=50051)
    >>>     server.run()

For more information, see the README.md.
"""

__version__ = "1.0.0"
__author__ = "Proto Fleet Team"

# Re-export commonly used types and functions for convenience
from proto_fleet_sdk.auth import APIKey, BearerToken, SecretBundle, TLSClientCert, UsernamePassword
from proto_fleet_sdk.capabilities import (
    CAP_ASYMMETRIC_AUTH,
    CAP_BASIC_AUTH,
    CAP_BATCH_STATUS,
    CAP_COOLING_MODE_AIR,
    CAP_COOLING_MODE_IMMERSE,
    CAP_CUSTOM_FAN_CURVES,
    CAP_DEVICE_STATUS,
    CAP_DISCOVER_DEVICE,
    CAP_DISCOVERY,
    CAP_EFFICIENCY,
    CAP_ERROR_COUNT,
    CAP_FACTORY_RESET,
    CAP_FAN_SPEED,
    CAP_FIRMWARE,
    CAP_FREQUENCY_CONTROL,
    CAP_GET_CAPABILITIES_FOR_MODEL,
    CAP_GET_COOLING_MODE,
    CAP_GET_DEFAULT_CREDENTIALS,
    CAP_GET_ERRORS,
    CAP_GET_MINING_POOLS,
    CAP_GET_TIME_SERIES_DATA,
    CAP_GET_WEB_VIEW_URL,
    CAP_HASHRATE_REPORTED,
    CAP_HISTORICAL_DATA,
    CAP_IMMERSION_COOLING,
    CAP_LED_BLINK,
    CAP_LOGS_DOWNLOAD,
    CAP_MANUAL_UPLOAD,
    CAP_MINER_STATUS,
    CAP_MINING_START,
    CAP_MINING_STOP,
    CAP_OTA_UPDATE,
    CAP_OVERCLOCKING,
    CAP_PAIR_DEVICE,
    CAP_PAIRING,
    CAP_PER_BOARD_STATS,
    CAP_PER_CHIP_STATS,
    CAP_PERFORMANCE_PROFILES,
    CAP_POLLING_HOST,
    CAP_POLLING_PLUGIN,
    CAP_POOL_CONFIG,
    CAP_POOL_PRIORITY,
    CAP_POOL_STATS,
    CAP_POWER_MODE_EFFICIENCY,
    CAP_POWER_USAGE,
    CAP_PSU_STATS,
    CAP_REALTIME_TELEMETRY,
    CAP_REBOOT,
    CAP_SET_COOLING_MODE,
    CAP_SET_POWER_TARGET,
    CAP_STREAMING,
    CAP_SUBSCRIBE_STATUS,
    CAP_TEMPERATURE,
    CAP_UNPAIR,
    CAP_UPDATE_FIRMWARE,
    CAP_UPDATE_MINER_PASSWORD,
    CAP_UPDATE_MINING_POOLS,
    CAP_UPTIME,
    CAP_VOLTAGE_CONTROL,
)
from proto_fleet_sdk.enums import (
    ComponentStatus,
    CoolingMode,
    HealthStatus,
    MetricKind,
    PerformanceMode,
)
from proto_fleet_sdk.error_codes import ComponentType, DeviceError, DeviceErrors, MinerError, Severity
from proto_fleet_sdk.errors import (
    AuthenticationFailedError,
    DeviceNotFoundError,
    DeviceUnavailableError,
    DriverShutdownError,
    InvalidConfigError,
    NetworkError,
    SDKError,
    UnsupportedCapabilityError,
)
from proto_fleet_sdk.protocols.device import Device
from proto_fleet_sdk.protocols.driver import Driver
from proto_fleet_sdk.telemetry.components import (
    ASICMetrics,
    ComponentInfo,
    ControlBoardMetrics,
    FanMetrics,
    HashBoardMetrics,
    PSUMetrics,
    SensorMetrics,
)
from proto_fleet_sdk.telemetry.metrics import DeviceMetrics, MetricValue, MetricValueMetaData
from proto_fleet_sdk.types import (
    Capabilities,
    ConfiguredPool,
    DeviceInfo,
    DriverIdentifier,
    MiningPoolConfig,
    NewDeviceResult,
)

__all__ = [
    "__version__",
    "__author__",
    # Core protocols
    "Driver",
    "Device",
    # Types
    "DriverIdentifier",
    "Capabilities",
    "DeviceInfo",
    "DeviceMetrics",
    "NewDeviceResult",
    "MiningPoolConfig",
    "ConfiguredPool",
    # Authentication
    "SecretBundle",
    "UsernamePassword",
    "APIKey",
    "BearerToken",
    "TLSClientCert",
    # Enums
    "HealthStatus",
    "ComponentStatus",
    "MetricKind",
    "CoolingMode",
    "PerformanceMode",
    # Errors
    "SDKError",
    "UnsupportedCapabilityError",
    "DeviceNotFoundError",
    "InvalidConfigError",
    "DeviceUnavailableError",
    "AuthenticationFailedError",
    "DriverShutdownError",
    "NetworkError",
    # Error codes
    "MinerError",
    "Severity",
    "ComponentType",
    "DeviceError",
    "DeviceErrors",
    # Telemetry
    "MetricValue",
    "MetricValueMetaData",
    "ComponentInfo",
    "HashBoardMetrics",
    "ASICMetrics",
    "PSUMetrics",
    "FanMetrics",
    "ControlBoardMetrics",
    "SensorMetrics",
    # Capability Constants
    "CAP_POLLING_HOST",
    "CAP_POLLING_PLUGIN",
    "CAP_DISCOVERY",
    "CAP_PAIRING",
    "CAP_MINING_START",
    "CAP_MINING_STOP",
    "CAP_LED_BLINK",
    "CAP_REBOOT",
    "CAP_FACTORY_RESET",
    "CAP_SET_COOLING_MODE",
    "CAP_GET_COOLING_MODE",
    "CAP_COOLING_MODE_AIR",
    "CAP_COOLING_MODE_IMMERSE",
    "CAP_SET_POWER_TARGET",
    "CAP_POWER_MODE_EFFICIENCY",
    "CAP_UPDATE_MINING_POOLS",
    "CAP_GET_MINING_POOLS",
    "CAP_POOL_CONFIG",
    "CAP_POOL_PRIORITY",
    "CAP_UPDATE_MINER_PASSWORD",
    "CAP_LOGS_DOWNLOAD",
    "CAP_UPDATE_FIRMWARE",
    "CAP_FIRMWARE",
    "CAP_OTA_UPDATE",
    "CAP_MANUAL_UPLOAD",
    "CAP_UNPAIR",
    "CAP_DEVICE_STATUS",
    "CAP_BATCH_STATUS",
    "CAP_SUBSCRIBE_STATUS",
    "CAP_STREAMING",
    "CAP_GET_TIME_SERIES_DATA",
    "CAP_REALTIME_TELEMETRY",
    "CAP_HISTORICAL_DATA",
    "CAP_HASHRATE_REPORTED",
    "CAP_POWER_USAGE",
    "CAP_TEMPERATURE",
    "CAP_FAN_SPEED",
    "CAP_EFFICIENCY",
    "CAP_UPTIME",
    "CAP_ERROR_COUNT",
    "CAP_MINER_STATUS",
    "CAP_POOL_STATS",
    "CAP_PER_CHIP_STATS",
    "CAP_PER_BOARD_STATS",
    "CAP_PSU_STATS",
    "CAP_GET_WEB_VIEW_URL",
    "CAP_GET_ERRORS",
    "CAP_DISCOVER_DEVICE",
    "CAP_PAIR_DEVICE",
    "CAP_GET_DEFAULT_CREDENTIALS",
    "CAP_GET_CAPABILITIES_FOR_MODEL",
    "CAP_BASIC_AUTH",
    "CAP_ASYMMETRIC_AUTH",
    "CAP_IMMERSION_COOLING",
    "CAP_PERFORMANCE_PROFILES",
    "CAP_CUSTOM_FAN_CURVES",
    "CAP_OVERCLOCKING",
    "CAP_VOLTAGE_CONTROL",
    "CAP_FREQUENCY_CONTROL",
]
