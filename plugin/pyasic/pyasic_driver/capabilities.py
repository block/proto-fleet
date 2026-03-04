"""Dynamic capability builder from pyasic miner introspection.

Builds SDK capability maps by inspecting a pyasic miner instance at runtime.
Uses two detection mechanisms:
  1. Support flags (booleans on every miner): supports_shutdown, supports_power_modes
  2. Method introspection: checks if a method is overridden from the base no-op
"""

from __future__ import annotations

from typing import Any

from proto_fleet_sdk.auth import UsernamePassword
from proto_fleet_sdk.capabilities import (
    CAP_ASYMMETRIC_AUTH,
    CAP_BASIC_AUTH,
    CAP_BATCH_STATUS,
    CAP_DEVICE_STATUS,
    CAP_DISCOVERY,
    CAP_EFFICIENCY,
    CAP_ERROR_COUNT,
    CAP_FAN_SPEED,
    CAP_FIRMWARE,
    CAP_GET_COOLING_MODE,
    CAP_GET_ERRORS,
    CAP_GET_MINING_POOLS,
    CAP_HASHRATE_REPORTED,
    CAP_HISTORICAL_DATA,
    CAP_LED_BLINK,
    CAP_LOGS_DOWNLOAD,
    CAP_MINER_STATUS,
    CAP_MINING_START,
    CAP_MINING_STOP,
    CAP_OTA_UPDATE,
    CAP_PAIRING,
    CAP_PER_BOARD_STATS,
    CAP_PER_CHIP_STATS,
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
    CAP_STREAMING,
    CAP_TEMPERATURE,
    CAP_UPDATE_MINER_PASSWORD,
    CAP_UPDATE_MINING_POOLS,
    CAP_UPTIME,
)
from proto_fleet_sdk.types import Capabilities

# Maps config family name → pyasic miner.make string
FAMILY_TO_MAKE: dict[str, str] = {
    "whatsminer": "WhatsMiner",
    "antminer": "AntMiner",
    "avalonminer": "AvalonMiner",
    "goldshell": "Goldshell",
    "auradine": "Auradine",
    "bitaxe": "BitAxe",
    "iceriver": "IceRiver",
    "innosilicon": "Innosilicon",
    "braiins": "Braiins",
    "epic": "ePIC",
    "hammer": "Hammer",
    "volcminer": "VolcMiner",
    "elphapex": "Elphapex",
    "luckyminer": "LuckyMiner",
}

# Reverse lookup: pyasic make string → config family name
MAKE_TO_FAMILY: dict[str, str] = {v: k for k, v in FAMILY_TO_MAKE.items()}

# Default credentials per manufacturer make string
DEFAULT_CREDENTIALS: dict[str, list[UsernamePassword]] = {
    "WhatsMiner": [UsernamePassword(username="admin", password="admin")],
    "AntMiner": [UsernamePassword(username="root", password="root")],
    "AvalonMiner": [UsernamePassword(username="admin", password="admin")],
    "Goldshell": [UsernamePassword(username="admin", password="123456789")],
    "Auradine": [UsernamePassword(username="admin", password="admin")],
    "Innosilicon": [UsernamePassword(username="admin", password="admin")],
    "ePIC": [UsernamePassword(username="admin", password="letmein")],
    "IceRiver": [UsernamePassword(username="admin", password="12345678")],
    "Hammer": [UsernamePassword(username="root", password="root")],
    "VolcMiner": [UsernamePassword(username="admin", password="ltc@dog")],
    "Braiins": [UsernamePassword(username="root", password="root")],
}

# Reference to pyasic's base class for method introspection.
# Lazy-loaded to avoid import-time dependency on pyasic internals.
_base_miner_class: type[Any] | None = None


def _get_base_class() -> type[Any]:
    global _base_miner_class
    if _base_miner_class is None:
        from pyasic.miners.base import BaseMiner

        _base_miner_class = BaseMiner
    return _base_miner_class


def _is_implemented(miner: Any, method_name: str) -> bool:
    """Check if a pyasic method is actually implemented (overridden from base no-op)."""
    base_cls = _get_base_class()
    miner_method = getattr(type(miner), method_name, None)
    base_method = getattr(base_cls, method_name, None)
    if miner_method is None or base_method is None:
        return False
    return miner_method is not base_method


def build_capabilities(miner: Any) -> Capabilities:
    """Build SDK capabilities dynamically from a pyasic miner instance."""
    return {
        # Core — always available
        CAP_POLLING_HOST: True,
        CAP_DISCOVERY: True,
        CAP_PAIRING: True,
        # Telemetry — get_data() works on all pyasic miners
        CAP_DEVICE_STATUS: True,
        CAP_REALTIME_TELEMETRY: True,
        CAP_HASHRATE_REPORTED: True,
        CAP_POWER_USAGE: True,
        CAP_TEMPERATURE: True,
        CAP_FAN_SPEED: True,
        CAP_EFFICIENCY: True,
        CAP_UPTIME: True,
        CAP_ERROR_COUNT: True,
        CAP_MINER_STATUS: True,
        CAP_POOL_STATS: True,
        CAP_PER_BOARD_STATS: True,
        CAP_PSU_STATS: True,
        CAP_BASIC_AUTH: True,
        CAP_GET_ERRORS: True,
        # Control — detected from method introspection
        CAP_REBOOT: _is_implemented(miner, "reboot"),
        CAP_LED_BLINK: _is_implemented(miner, "fault_light_on"),
        CAP_MINING_START: _is_implemented(miner, "resume_mining"),
        CAP_MINING_STOP: _is_implemented(miner, "stop_mining"),
        # Configuration — detected from method introspection
        CAP_GET_MINING_POOLS: _is_implemented(miner, "get_config"),
        CAP_UPDATE_MINING_POOLS: _is_implemented(miner, "send_config"),
        CAP_POOL_CONFIG: _is_implemented(miner, "get_config"),
        CAP_POOL_PRIORITY: _is_implemented(miner, "send_config"),
        CAP_POWER_MODE_EFFICIENCY: getattr(miner, "supports_power_modes", False),
        CAP_FIRMWARE: _is_implemented(miner, "upgrade_firmware"),
        # Not available through pyasic's API
        CAP_SET_COOLING_MODE: False,
        CAP_GET_COOLING_MODE: False,
        CAP_UPDATE_MINER_PASSWORD: False,
        CAP_STREAMING: False,
        CAP_BATCH_STATUS: False,
        CAP_POLLING_PLUGIN: False,
        CAP_HISTORICAL_DATA: False,
        CAP_PER_CHIP_STATS: False,
        CAP_LOGS_DOWNLOAD: False,
        CAP_OTA_UPDATE: False,
        CAP_ASYMMETRIC_AUTH: False,
    }


# Static base capabilities used for describe_driver() and capability checks.
# This is the superset of what pyasic-supported miners can do. The server uses
# these to determine which UI actions are available for a device. Individual
# per-device capabilities (from build_capabilities) gate actual operations.
STATIC_BASE_CAPABILITIES: Capabilities = {
    # Core
    CAP_POLLING_HOST: True,
    CAP_DISCOVERY: True,
    CAP_PAIRING: True,
    # Telemetry — get_data() works on all pyasic miners
    CAP_DEVICE_STATUS: True,
    CAP_REALTIME_TELEMETRY: True,
    CAP_HASHRATE_REPORTED: True,
    CAP_POWER_USAGE: True,
    CAP_TEMPERATURE: True,
    CAP_FAN_SPEED: True,
    CAP_EFFICIENCY: True,
    CAP_UPTIME: True,
    CAP_ERROR_COUNT: True,
    CAP_MINER_STATUS: True,
    CAP_POOL_STATS: True,
    CAP_PER_BOARD_STATS: True,
    CAP_PSU_STATS: True,
    CAP_BASIC_AUTH: True,
    CAP_GET_ERRORS: True,
    # Control — most pyasic backends implement these
    CAP_REBOOT: True,
    CAP_LED_BLINK: True,
    CAP_MINING_START: True,
    CAP_MINING_STOP: True,
    # Configuration
    CAP_POOL_CONFIG: True,
    CAP_POOL_PRIORITY: True,
    CAP_POWER_MODE_EFFICIENCY: True,
}
