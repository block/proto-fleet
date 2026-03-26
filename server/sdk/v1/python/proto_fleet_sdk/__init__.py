"""Proto Fleet Python SDK.

This SDK enables developers to create mining device plugins in Python that integrate
seamlessly with the Proto Fleet management system.

Plugins implement driver_pb2_grpc.DriverServicer directly using protoc-generated types,
and use the @grpc_error_handler decorator for automatic SDK error → gRPC status mapping.

Quick Start:
    >>> from proto_fleet_sdk.generated.pb import driver_pb2, driver_pb2_grpc
    >>> from proto_fleet_sdk.errors import grpc_error_handler, DeviceNotFoundError
    >>> from proto_fleet_sdk.server import PluginServer
    >>>
    >>> class MyServicer(driver_pb2_grpc.DriverServicer):
    >>>     @grpc_error_handler
    >>>     async def Handshake(self, request, context):
    >>>         return driver_pb2.HandshakeResponse(
    >>>             driver_name="my-plugin", api_version="v1",
    >>>         )
    >>>
    >>> if __name__ == "__main__":
    >>>     server = PluginServer(MyServicer())
    >>>     server.run()

For more information, see the README.md.
"""

__version__ = "2.0.0"
__author__ = "Proto Fleet Team"

# Re-export commonly used types and functions for convenience
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
from proto_fleet_sdk.error_codes import (
    ComponentType,
    DeviceError,
    DeviceErrors,
    MinerError,
    Severity,
)
from proto_fleet_sdk.errors import (
    AuthenticationFailedError,
    DeviceNotFoundError,
    DeviceUnavailableError,
    DriverShutdownError,
    InvalidConfigError,
    NetworkError,
    SDKError,
    UnsupportedCapabilityError,
    grpc_error_handler,
)

__all__ = [
    "__version__",
    "__author__",
    # Errors
    "SDKError",
    "UnsupportedCapabilityError",
    "DeviceNotFoundError",
    "InvalidConfigError",
    "DeviceUnavailableError",
    "AuthenticationFailedError",
    "DriverShutdownError",
    "NetworkError",
    "grpc_error_handler",
    # Error codes
    "MinerError",
    "Severity",
    "ComponentType",
    "DeviceError",
    "DeviceErrors",
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
