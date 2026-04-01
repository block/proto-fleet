use std::collections::HashMap;
use std::sync::LazyLock;

use asic_rs_core::traits::miner::Miner;
use proto_fleet_plugin::capabilities::*;

pub type Capabilities = HashMap<String, bool>;

/// Static base capabilities built once and cloned on use.
static BASE_CAPABILITIES: LazyLock<Capabilities> = LazyLock::new(|| {
    let mut caps = Capabilities::new();

    // Core
    caps.insert(CAP_POLLING_HOST.into(), true);
    caps.insert(CAP_DISCOVERY.into(), true);
    caps.insert(CAP_PAIRING.into(), true);

    // Telemetry
    caps.insert(CAP_DEVICE_STATUS.into(), true);
    caps.insert(CAP_REALTIME_TELEMETRY.into(), true);
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

    // Control -- default false
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

/// Optimistic driver-level capabilities returned by DescribeDriver.
/// Control/config caps are set to true because the plugin supports these
/// operations in general — per-model overrides from GetCapabilitiesForModel
/// and per-device require_cap() checks provide the accurate gatekeeping.
pub fn driver_base_capabilities() -> Capabilities {
    let mut caps = static_base_capabilities();
    for &key in PROBED_CAPS {
        caps.insert(key.into(), true);
    }
    caps
}

/// Control and configuration caps that vary per miner model.
/// Probed from the live miner by probe_capabilities(); set optimistically
/// by driver_base_capabilities() for the driver-level default.
const PROBED_CAPS: &[&str] = &[
    CAP_REBOOT,
    CAP_LED_BLINK,
    CAP_MINING_START,
    CAP_MINING_STOP,
    CAP_POOL_CONFIG,
    CAP_POOL_PRIORITY,
    CAP_GET_MINING_POOLS,
    CAP_UPDATE_MINING_POOLS,
    CAP_POWER_MODE_EFFICIENCY,
];

/// Probe capabilities from a live miner instance using asic-rs supports_*() methods.
pub fn probe_capabilities(miner: &dyn Miner) -> Capabilities {
    let mut caps = static_base_capabilities();

    // Control -- from live miner introspection
    caps.insert(CAP_REBOOT.into(), miner.supports_restart());
    caps.insert(CAP_LED_BLINK.into(), miner.supports_set_fault_light());
    caps.insert(CAP_MINING_START.into(), miner.supports_resume());
    caps.insert(CAP_MINING_STOP.into(), miner.supports_pause());

    // Configuration -- from live miner introspection
    let pools = miner.supports_pools_config();
    caps.insert(CAP_POOL_CONFIG.into(), pools);
    caps.insert(CAP_POOL_PRIORITY.into(), pools);
    caps.insert(CAP_GET_MINING_POOLS.into(), pools);
    caps.insert(CAP_UPDATE_MINING_POOLS.into(), pools);
    caps.insert(
        CAP_POWER_MODE_EFFICIENCY.into(),
        miner.supports_tuning_config(),
    );
    // Do not advertise firmware updates until the update_firmware RPC is implemented.
    caps.insert(CAP_FIRMWARE.into(), false);

    caps
}

/// Map manufacturer/make string to config family name (no allocation).
pub fn make_to_family(make: &str) -> Option<&'static str> {
    // Aftermarket firmware display names map back to their hardware family
    let families: &[(&[&str], &str)] = &[
        (&["whatsminer"], "whatsminer"),
        (&["antminer", "vnish", "braiins", "luxos"], "antminer"),
        (&["avalonminer"], "avalonminer"),
        (&["goldshell"], "goldshell"),
        (&["auradine"], "auradine"),
        (&["bitaxe"], "bitaxe"),
        (&["iceriver"], "iceriver"),
        (&["innosilicon"], "innosilicon"),
        (&["epic"], "epic"),
        (&["hammer"], "hammer"),
        (&["volcminer"], "volcminer"),
        (&["elphapex"], "elphapex"),
        (&["luckyminer"], "luckyminer"),
    ];
    for (names, family) in families {
        for name in *names {
            if make.eq_ignore_ascii_case(name) {
                return Some(family);
            }
        }
    }
    None
}

/// Map firmware string to config variant key (no allocation).
pub fn firmware_to_variant(firmware: &str) -> &'static str {
    let lower = firmware.as_bytes();
    if contains_ascii_ci(lower, b"vnish") {
        "vnish"
    } else if contains_ascii_ci(lower, b"braiins") {
        "braiins"
    } else if contains_ascii_ci(lower, b"luxos") {
        "luxos"
    } else {
        "stock"
    }
}

/// Detect firmware variant using both firmware string and make.
/// Falls back to make when the firmware string lacks recognizable tokens
/// (e.g. VNish reporting make="VNish" with a version-only firmware string).
pub fn detect_variant(make: &str, firmware: &str) -> &'static str {
    let variant = firmware_to_variant(firmware);
    if variant != "stock" {
        return variant;
    }
    // Firmware string didn't match — check make as fallback
    let make_lower = make.as_bytes();
    if contains_ascii_ci(make_lower, b"vnish") {
        "vnish"
    } else if contains_ascii_ci(make_lower, b"braiins") {
        "braiins"
    } else if contains_ascii_ci(make_lower, b"luxos") {
        "luxos"
    } else {
        "stock"
    }
}

/// Normalize a MAC address to lowercase hex digits only (no separators).
/// e.g. "AA:BB:CC:DD:EE:FF", "aa-bb-cc-dd-ee-ff" → "aabbccddeeff"
fn normalize_mac(mac: &str) -> String {
    mac.chars()
        .filter(|c| c.is_ascii_hexdigit())
        .map(|c| c.to_ascii_lowercase())
        .collect()
}

/// Case-insensitive ASCII substring search (no allocation).
fn contains_ascii_ci(haystack: &[u8], needle: &[u8]) -> bool {
    haystack
        .windows(needle.len())
        .any(|w| w.eq_ignore_ascii_case(needle))
}

/// Map firmware variant to manufacturer display name.
pub fn firmware_manufacturer(variant: &str) -> Option<&'static str> {
    match variant {
        "braiins" => Some("Braiins"),
        "vnish" => Some("VNish"),
        "luxos" => Some("LuxOS"),
        _ => None,
    }
}

/// Verify that a miner's identity matches expected values.
/// Requires at least one strong identifier (serial or MAC) to match when available.
/// Exception: if the discovery record never captured serial/MAC (both empty),
/// the check allows through with a warning (initial pairing case).
/// Once serial/MAC are stored after pairing, subsequent reconnects enforce them.
pub fn verify_identity(
    expected_model: &str,
    expected_serial: &str,
    expected_mac: &str,
    actual_model: &str,
    actual_serial: &str,
    actual_mac: &str,
) -> Result<(), String> {
    // Check model (if available on both sides)
    if !expected_model.is_empty() && !actual_model.is_empty() && actual_model != expected_model {
        return Err(format!(
            "model mismatch: expected '{}', got '{}'",
            expected_model, actual_model
        ));
    }

    // Check serial (if available on both sides)
    if !expected_serial.is_empty() && !actual_serial.is_empty() && actual_serial != expected_serial
    {
        return Err(format!(
            "serial mismatch: expected '{}', got '{}'",
            expected_serial, actual_serial
        ));
    }

    // Check MAC (if available on both sides), normalizing to lowercase hex
    // to avoid false mismatches from case/separator differences.
    let norm_expected_mac = normalize_mac(expected_mac);
    let norm_actual_mac = normalize_mac(actual_mac);
    if !norm_expected_mac.is_empty()
        && !norm_actual_mac.is_empty()
        && norm_actual_mac != norm_expected_mac
    {
        return Err(format!(
            "MAC mismatch: expected '{}', got '{}'",
            expected_mac, actual_mac
        ));
    }

    // Require at least one strong identifier to have matched (not just model).
    // If the miner doesn't report serial or MAC, we can't verify identity reliably.
    let serial_verified = !expected_serial.is_empty()
        && !actual_serial.is_empty()
        && actual_serial == expected_serial;
    let mac_verified = !norm_expected_mac.is_empty()
        && !norm_actual_mac.is_empty()
        && norm_actual_mac == norm_expected_mac;

    if !serial_verified && !mac_verified {
        // Neither serial nor MAC could be verified -- only model matched (or nothing)
        if expected_serial.is_empty() && expected_mac.is_empty() {
            // Discovery never captured serial/MAC -- allow through for initial pairing.
            // Once paired, serial/MAC will be stored and enforced on reconnect.
            tracing::warn!(
                expected_model,
                "Identity verification degraded: no serial/MAC available, model-only match"
            );
            return Ok(());
        }
        return Err(
            "no strong identifier (serial/MAC) could be verified; miner may have changed".into(),
        );
    }

    Ok(())
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
