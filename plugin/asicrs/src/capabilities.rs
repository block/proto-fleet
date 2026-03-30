use std::collections::HashMap;
use std::sync::LazyLock;

use proto_fleet_plugin::capabilities::*;

pub type Capabilities = HashMap<String, bool>;

/// Static base capabilities built once and cloned on use.
static BASE_CAPABILITIES: LazyLock<Capabilities> = LazyLock::new(|| {
    let mut caps = Capabilities::new();

    // Core
    caps.insert(CAP_POLLING_HOST.into(), true);
    // Discovery and pairing disabled until RPCs are implemented (see business logic PR)
    caps.insert(CAP_DISCOVERY.into(), false);
    caps.insert(CAP_PAIRING.into(), false);

    // Telemetry -- advertised but RPCs are stubbed until business logic PR
    caps.insert(CAP_DEVICE_STATUS.into(), false);
    caps.insert(CAP_REALTIME_TELEMETRY.into(), false);
    caps.insert(CAP_HASHRATE_REPORTED.into(), true);
    caps.insert(CAP_POWER_USAGE.into(), true);
    caps.insert(CAP_TEMPERATURE.into(), true);
    caps.insert(CAP_FAN_SPEED.into(), true);
    caps.insert(CAP_EFFICIENCY.into(), true);
    caps.insert(CAP_UPTIME.into(), true);
    caps.insert(CAP_ERROR_COUNT.into(), true);
    caps.insert(CAP_MINER_STATUS.into(), true);
    caps.insert(CAP_POOL_STATS.into(), true);
    caps.insert(CAP_PER_BOARD_STATS.into(), true);
    caps.insert(CAP_PSU_STATS.into(), true);
    caps.insert(CAP_BASIC_AUTH.into(), true);
    caps.insert(CAP_GET_ERRORS.into(), true);

    // Control -- default false (enabled per-device in the business logic layer)
    caps.insert(CAP_REBOOT.into(), false);
    caps.insert(CAP_LED_BLINK.into(), false);
    caps.insert(CAP_MINING_START.into(), false);
    caps.insert(CAP_MINING_STOP.into(), false);

    // Configuration -- default false
    caps.insert(CAP_POOL_CONFIG.into(), false);
    caps.insert(CAP_POOL_PRIORITY.into(), false);
    caps.insert(CAP_GET_MINING_POOLS.into(), false);
    caps.insert(CAP_UPDATE_MINING_POOLS.into(), false);
    caps.insert(CAP_POWER_MODE_EFFICIENCY.into(), false);

    // Not supported
    caps.insert(CAP_SET_COOLING_MODE.into(), false);
    caps.insert(CAP_GET_COOLING_MODE.into(), false);
    caps.insert(CAP_UPDATE_MINER_PASSWORD.into(), false);
    caps.insert(CAP_STREAMING.into(), false);
    caps.insert(CAP_BATCH_STATUS.into(), false);
    caps.insert(CAP_POLLING_PLUGIN.into(), false);
    caps.insert(CAP_HISTORICAL_DATA.into(), false);
    caps.insert(CAP_PER_CHIP_STATS.into(), false);
    caps.insert(CAP_LOGS_DOWNLOAD.into(), false);
    caps.insert(CAP_OTA_UPDATE.into(), false);
    caps.insert(CAP_ASYMMETRIC_AUTH.into(), false);
    caps.insert(CAP_FIRMWARE.into(), false);

    caps
});

/// Clone the static base capabilities (built once via LazyLock).
pub fn static_base_capabilities() -> Capabilities {
    BASE_CAPABILITIES.clone()
}

/// Default credentials by family and firmware variant.
pub struct DefaultCredential {
    pub username: &'static str,
    pub password: &'static str,
}

pub fn default_credentials(family: &str, variant: &str) -> Vec<DefaultCredential> {
    match (family, variant) {
        ("whatsminer", "stock") => vec![
            DefaultCredential {
                username: "admin",
                password: "admin",
            },
            DefaultCredential {
                username: "admin",
                password: "super",
            },
        ],
        ("antminer", _) => vec![DefaultCredential {
            username: "root",
            password: "root",
        }],
        ("avalonminer", _) => vec![DefaultCredential {
            username: "admin",
            password: "admin",
        }],
        ("goldshell", _) => vec![DefaultCredential {
            username: "admin",
            password: "123456789",
        }],
        ("auradine", _) => vec![DefaultCredential {
            username: "admin",
            password: "admin",
        }],
        ("iceriver", _) => vec![DefaultCredential {
            username: "admin",
            password: "12345678",
        }],
        ("innosilicon", _) => vec![DefaultCredential {
            username: "admin",
            password: "admin",
        }],
        ("epic", _) => vec![DefaultCredential {
            username: "admin",
            password: "letmein",
        }],
        ("hammer", _) => vec![DefaultCredential {
            username: "root",
            password: "root",
        }],
        ("volcminer", _) => vec![DefaultCredential {
            username: "admin",
            password: "ltc@dog",
        }],
        _ => vec![],
    }
}
