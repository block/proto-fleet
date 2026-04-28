"""Capability constants for Proto Fleet SDK.

This module defines all capability string constants that can be used in the Capabilities
flags dictionary to indicate what features a driver or device supports.
"""

from __future__ import annotations

__all__ = [
    # Core Capabilities
    "CAP_POLLING_HOST",
    "CAP_POLLING_PLUGIN",
    "CAP_DISCOVERY",
    "CAP_PAIRING",
    # Device Control
    "CAP_MINING_START",
    "CAP_MINING_STOP",
    "CAP_LED_BLINK",
    "CAP_REBOOT",
    "CAP_FACTORY_RESET",
    # Configuration
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
    "CAP_NATIVE_STRATUM_V2",
    "CAP_UPDATE_MINER_PASSWORD",
    # Maintenance
    "CAP_LOGS_DOWNLOAD",
    "CAP_UPDATE_FIRMWARE",
    "CAP_FIRMWARE",
    "CAP_OTA_UPDATE",
    "CAP_MANUAL_UPLOAD",
    "CAP_UNPAIR",
    # Telemetry
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
    # Device Info
    "CAP_GET_WEB_VIEW_URL",
    # Error Reporting
    "CAP_GET_ERRORS",
    # Discovery & Pairing
    "CAP_DISCOVER_DEVICE",
    "CAP_PAIR_DEVICE",
    "CAP_GET_DEFAULT_CREDENTIALS",
    "CAP_GET_CAPABILITIES_FOR_MODEL",
    # Authentication
    "CAP_BASIC_AUTH",
    "CAP_ASYMMETRIC_AUTH",
    # Advanced Features
    "CAP_IMMERSION_COOLING",
    "CAP_PERFORMANCE_PROFILES",
    "CAP_CUSTOM_FAN_CURVES",
    "CAP_OVERCLOCKING",
    "CAP_VOLTAGE_CONTROL",
    "CAP_FREQUENCY_CONTROL",
]

# Core Capabilities
CAP_POLLING_HOST = "polling_host"
CAP_POLLING_PLUGIN = "polling_plugin"
CAP_DISCOVERY = "discovery"
CAP_PAIRING = "pairing"

# Device Control Capabilities (canonical names match Go SDK)
CAP_MINING_START = "mining_start"
CAP_MINING_STOP = "mining_stop"
CAP_LED_BLINK = "led_blink"
CAP_REBOOT = "reboot"
CAP_FACTORY_RESET = "factory_reset"

# Configuration Capabilities
CAP_SET_COOLING_MODE = "set_cooling_mode"
CAP_GET_COOLING_MODE = "get_cooling_mode"
CAP_COOLING_MODE_AIR = "cooling_mode_air"
CAP_COOLING_MODE_IMMERSE = "cooling_mode_immerse"
CAP_SET_POWER_TARGET = "set_power_target"
CAP_POWER_MODE_EFFICIENCY = "power_mode_efficiency"
CAP_UPDATE_MINING_POOLS = "update_mining_pools"
CAP_GET_MINING_POOLS = "get_mining_pools"
CAP_POOL_CONFIG = "pool_config"
CAP_POOL_PRIORITY = "pool_priority"
CAP_NATIVE_STRATUM_V2 = "native_stratum_v2"
CAP_UPDATE_MINER_PASSWORD = "update_miner_password"

# Maintenance Capabilities
CAP_LOGS_DOWNLOAD = "logs_download"
CAP_UPDATE_FIRMWARE = "update_firmware"
CAP_FIRMWARE = "firmware"  # Generic firmware capability
CAP_OTA_UPDATE = "ota_update"
CAP_MANUAL_UPLOAD = "manual_upload"
CAP_UNPAIR = "unpair"

# Telemetry Capabilities
CAP_DEVICE_STATUS = "device_status"
CAP_BATCH_STATUS = "batch_status"
CAP_SUBSCRIBE_STATUS = "subscribe_status"
CAP_STREAMING = "streaming"
CAP_GET_TIME_SERIES_DATA = "get_time_series_data"
CAP_REALTIME_TELEMETRY = "realtime_telemetry"
CAP_HISTORICAL_DATA = "historical_data"

# Telemetry Metric Capabilities
CAP_HASHRATE_REPORTED = "hashrate_reported"
CAP_POWER_USAGE = "power_usage_reported"
CAP_TEMPERATURE = "temperature_reported"
CAP_FAN_SPEED = "fan_speed_reported"
CAP_EFFICIENCY = "efficiency_reported"
CAP_UPTIME = "uptime_reported"
CAP_ERROR_COUNT = "error_count_reported"
CAP_MINER_STATUS = "miner_status_reported"
CAP_POOL_STATS = "pool_stats_reported"
CAP_PER_CHIP_STATS = "per_chip_stats"
CAP_PER_BOARD_STATS = "per_board_stats"
CAP_PSU_STATS = "psu_stats_reported"

# Device Info Capabilities
CAP_GET_WEB_VIEW_URL = "get_web_view_url"

# Error Reporting Capabilities
CAP_GET_ERRORS = "get_errors"

# Discovery & Pairing Capabilities
CAP_DISCOVER_DEVICE = "discover_device"
CAP_PAIR_DEVICE = "pair_device"
CAP_GET_DEFAULT_CREDENTIALS = "get_default_credentials"
CAP_GET_CAPABILITIES_FOR_MODEL = "get_capabilities_for_model"

# Authentication Capabilities
CAP_BASIC_AUTH = "basic_auth"
CAP_ASYMMETRIC_AUTH = "asymmetric_auth"

# Advanced Feature Capabilities
CAP_IMMERSION_COOLING = "immersion_cooling"
CAP_PERFORMANCE_PROFILES = "performance_profiles"
CAP_CUSTOM_FAN_CURVES = "custom_fan_curves"
CAP_OVERCLOCKING = "overclocking"
CAP_VOLTAGE_CONTROL = "voltage_control"
CAP_FREQUENCY_CONTROL = "frequency_control"
