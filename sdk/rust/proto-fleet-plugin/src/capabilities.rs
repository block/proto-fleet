// Capability constants for Proto Fleet Plugin SDK.
//
// These match the canonical values defined in the Python SDK's `capabilities.py`
// and Go SDK's `interface.go`. Use these with the `Capabilities` proto message
// flags map.

// Core Capabilities
pub const CAP_POLLING_HOST: &str = "polling_host";
pub const CAP_POLLING_PLUGIN: &str = "polling_plugin";
pub const CAP_DISCOVERY: &str = "discovery";
pub const CAP_PAIRING: &str = "pairing";

// Device Control
pub const CAP_MINING_START: &str = "mining_start";
pub const CAP_MINING_STOP: &str = "mining_stop";
pub const CAP_LED_BLINK: &str = "led_blink";
pub const CAP_REBOOT: &str = "reboot";
pub const CAP_FACTORY_RESET: &str = "factory_reset";
pub const CAP_CURTAIL_FULL: &str = "curtail_full";
pub const CAP_CURTAIL_EFFICIENCY: &str = "curtail_efficiency";

// Configuration
pub const CAP_SET_COOLING_MODE: &str = "set_cooling_mode";
pub const CAP_GET_COOLING_MODE: &str = "get_cooling_mode";
pub const CAP_COOLING_MODE_AIR: &str = "cooling_mode_air";
pub const CAP_COOLING_MODE_IMMERSE: &str = "cooling_mode_immerse";
pub const CAP_SET_POWER_TARGET: &str = "set_power_target";
pub const CAP_POWER_MODE_EFFICIENCY: &str = "power_mode_efficiency";
pub const CAP_UPDATE_MINING_POOLS: &str = "update_mining_pools";
pub const CAP_GET_MINING_POOLS: &str = "get_mining_pools";
pub const CAP_POOL_CONFIG: &str = "pool_config";
pub const CAP_POOL_PRIORITY: &str = "pool_priority";
pub const CAP_UPDATE_MINER_PASSWORD: &str = "update_miner_password";

// Maintenance
pub const CAP_LOGS_DOWNLOAD: &str = "logs_download";
pub const CAP_UPDATE_FIRMWARE: &str = "update_firmware";
pub const CAP_FIRMWARE: &str = "firmware";
pub const CAP_OTA_UPDATE: &str = "ota_update";
pub const CAP_MANUAL_UPLOAD: &str = "manual_upload";
pub const CAP_UNPAIR: &str = "unpair";

// Telemetry
pub const CAP_DEVICE_STATUS: &str = "device_status";
pub const CAP_BATCH_STATUS: &str = "batch_status";
pub const CAP_SUBSCRIBE_STATUS: &str = "subscribe_status";
pub const CAP_STREAMING: &str = "streaming";
pub const CAP_GET_TIME_SERIES_DATA: &str = "get_time_series_data";
pub const CAP_REALTIME_TELEMETRY: &str = "realtime_telemetry";
pub const CAP_HISTORICAL_DATA: &str = "historical_data";

// Telemetry Metrics
pub const CAP_HASHRATE_REPORTED: &str = "hashrate_reported";
pub const CAP_POWER_USAGE: &str = "power_usage_reported";
pub const CAP_TEMPERATURE: &str = "temperature_reported";
pub const CAP_FAN_SPEED: &str = "fan_speed_reported";
pub const CAP_EFFICIENCY: &str = "efficiency_reported";
pub const CAP_UPTIME: &str = "uptime_reported";
pub const CAP_ERROR_COUNT: &str = "error_count_reported";
pub const CAP_MINER_STATUS: &str = "miner_status_reported";
pub const CAP_POOL_STATS: &str = "pool_stats_reported";
pub const CAP_PER_CHIP_STATS: &str = "per_chip_stats";
pub const CAP_PER_BOARD_STATS: &str = "per_board_stats";
pub const CAP_PSU_STATS: &str = "psu_stats_reported";

// Device Info
pub const CAP_GET_WEB_VIEW_URL: &str = "get_web_view_url";

// Error Reporting
pub const CAP_GET_ERRORS: &str = "get_errors";

// Discovery & Pairing
pub const CAP_DISCOVER_DEVICE: &str = "discover_device";
pub const CAP_PAIR_DEVICE: &str = "pair_device";
pub const CAP_GET_DEFAULT_CREDENTIALS: &str = "get_default_credentials";
pub const CAP_GET_CAPABILITIES_FOR_MODEL: &str = "get_capabilities_for_model";

// Authentication
pub const CAP_BASIC_AUTH: &str = "basic_auth";
pub const CAP_ASYMMETRIC_AUTH: &str = "asymmetric_auth";

// Advanced Features
pub const CAP_IMMERSION_COOLING: &str = "immersion_cooling";
pub const CAP_PERFORMANCE_PROFILES: &str = "performance_profiles";
pub const CAP_CUSTOM_FAN_CURVES: &str = "custom_fan_curves";
pub const CAP_OVERCLOCKING: &str = "overclocking";
pub const CAP_VOLTAGE_CONTROL: &str = "voltage_control";
pub const CAP_FREQUENCY_CONTROL: &str = "frequency_control";
