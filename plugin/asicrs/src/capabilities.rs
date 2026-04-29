use std::collections::HashMap;
use std::sync::LazyLock;

use asic_rs_core::traits::miner::Miner;
use proto_fleet_plugin::capabilities::*;

pub type Capabilities = HashMap<String, bool>;

// Hardware families
pub const FAMILY_WHATSMINER: &str = "whatsminer";
pub const FAMILY_ANTMINER: &str = "antminer";
pub const FAMILY_AVALONMINER: &str = "avalonminer";
pub const FAMILY_BITAXE: &str = "bitaxe";
pub const FAMILY_NERDAXE: &str = "nerdaxe";
pub const FAMILY_EPIC: &str = "epic";
pub const FAMILY_AURADINE: &str = "auradine";

// Firmware variants
pub const VARIANT_STOCK: &str = "stock";
pub const VARIANT_VNISH: &str = "vnish";
pub const VARIANT_BRAIINS: &str = "braiins";
pub const VARIANT_EPIC: &str = "epic";
pub const VARIANT_LUXOS: &str = "luxos";
pub const VARIANT_MARATHON: &str = "marathon";

// Manufacturer display names for aftermarket firmware
const DISPLAY_VNISH: &str = "VNish";
const DISPLAY_EPIC: &str = "ePIC";
const DISPLAY_BRAIINS: &str = "Braiins";
const DISPLAY_LUXOS: &str = "LuxOS";
const DISPLAY_MARATHON: &str = "Marathon";

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
    caps.insert(CAP_CURTAIL_FULL.into(), false);
    caps.insert(CAP_CURTAIL_EFFICIENCY.into(), false);
    caps.insert(CAP_CURTAIL_PARTIAL.into(), false);

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
/// operations in general. Curtailment stays model-probed because it requires
/// both pause and resume support.
pub fn driver_base_capabilities() -> Capabilities {
    let mut caps = static_base_capabilities();
    for &key in PROBED_CAPS {
        caps.insert(key.into(), true);
    }
    caps
}

/// Control and configuration caps that vary per miner model.
/// Probed from the live miner by probe_capabilities(); set optimistically
/// by driver_base_capabilities() for the driver-level default when safe.
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
    let supports_resume = miner.supports_resume();
    let supports_pause = miner.supports_pause();
    caps.insert(CAP_REBOOT.into(), miner.supports_restart());
    caps.insert(CAP_LED_BLINK.into(), miner.supports_set_fault_light());
    caps.insert(CAP_MINING_START.into(), supports_resume);
    caps.insert(CAP_MINING_STOP.into(), supports_pause);
    caps.insert(
        CAP_CURTAIL_FULL.into(),
        curtail_capability(supports_pause, supports_resume),
    );

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

fn curtail_capability(supports_pause: bool, supports_resume: bool) -> bool {
    supports_pause && supports_resume
}

/// Map manufacturer/make string to config family name (no allocation).
pub fn make_to_family(make: &str) -> Option<&'static str> {
    // Aftermarket firmware display names map back to their hardware family
    let families: &[(&[&str], &str)] = &[
        (&[FAMILY_WHATSMINER], FAMILY_WHATSMINER),
        (
            &[
                FAMILY_ANTMINER,
                VARIANT_VNISH,
                VARIANT_BRAIINS,
                VARIANT_LUXOS,
                VARIANT_MARATHON,
            ],
            FAMILY_ANTMINER,
        ),
        (&[FAMILY_AVALONMINER], FAMILY_AVALONMINER),
        (&[FAMILY_BITAXE], FAMILY_BITAXE),
        (&[FAMILY_NERDAXE], FAMILY_NERDAXE),
        (&[FAMILY_EPIC], FAMILY_EPIC),
        (&[FAMILY_AURADINE], FAMILY_AURADINE),
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

// "marafw" appears in live Marathon device version strings (e.g. "MARAFW_1.0.0"),
// while VARIANT_MARATHON ("marathon") matches the display name returned by asic-rs.
const FIRMWARE_STR_MARAFW: &str = "marafw";

/// Map firmware string to config variant key (no allocation).
pub fn firmware_to_variant(firmware: &str) -> &'static str {
    let lower = firmware.as_bytes();
    if contains_ascii_ci(lower, VARIANT_VNISH.as_bytes()) {
        VARIANT_VNISH
    } else if contains_ascii_ci(lower, VARIANT_EPIC.as_bytes()) {
        VARIANT_EPIC
    } else if contains_ascii_ci(lower, VARIANT_BRAIINS.as_bytes()) {
        VARIANT_BRAIINS
    } else if contains_ascii_ci(lower, VARIANT_LUXOS.as_bytes()) {
        VARIANT_LUXOS
    } else if contains_ascii_ci(lower, FIRMWARE_STR_MARAFW.as_bytes())
        || contains_ascii_ci(lower, VARIANT_MARATHON.as_bytes())
    {
        VARIANT_MARATHON
    } else {
        VARIANT_STOCK
    }
}

/// Detect firmware variant using both firmware string and make.
/// Falls back to make when the firmware string lacks recognizable tokens
/// (e.g. VNish reporting make="VNish" with a version-only firmware string).
pub fn detect_variant(make: &str, firmware: &str) -> &'static str {
    let variant = firmware_to_variant(firmware);
    if variant != VARIANT_STOCK {
        return variant;
    }
    // Firmware string didn't match — check make as fallback
    let make_lower = make.as_bytes();
    if contains_ascii_ci(make_lower, VARIANT_VNISH.as_bytes()) {
        VARIANT_VNISH
    } else if contains_ascii_ci(make_lower, VARIANT_BRAIINS.as_bytes()) {
        VARIANT_BRAIINS
    } else if contains_ascii_ci(make_lower, VARIANT_LUXOS.as_bytes()) {
        VARIANT_LUXOS
    } else if contains_ascii_ci(make_lower, VARIANT_MARATHON.as_bytes()) {
        VARIANT_MARATHON
    } else {
        VARIANT_STOCK
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
        VARIANT_BRAIINS => Some(DISPLAY_BRAIINS),
        VARIANT_VNISH => Some(DISPLAY_VNISH),
        VARIANT_EPIC => Some(DISPLAY_EPIC),
        VARIANT_LUXOS => Some(DISPLAY_LUXOS),
        VARIANT_MARATHON => Some(DISPLAY_MARATHON),
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
        (FAMILY_WHATSMINER, VARIANT_STOCK) => vec![
            DefaultCredential {
                username: "admin",
                password: "admin",
            },
            DefaultCredential {
                username: "super",
                password: "super",
            },
        ],
        (FAMILY_ANTMINER, VARIANT_VNISH) => vec![DefaultCredential {
            username: "admin",
            password: "admin",
        }],
        (FAMILY_ANTMINER, VARIANT_EPIC) => vec![DefaultCredential {
            username: "root",
            password: "letmein",
        }],
        (FAMILY_ANTMINER, _) => vec![DefaultCredential {
            username: "root",
            password: "root",
        }],
        (FAMILY_AVALONMINER, _) => vec![DefaultCredential {
            username: "admin",
            password: "admin",
        }],
        (FAMILY_EPIC, _) => vec![DefaultCredential {
            username: "root",
            password: "letmein",
        }],
        _ => vec![],
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_driver_base_capabilities_do_not_advertise_model_dependent_curtail() {
        let caps = driver_base_capabilities();

        assert_eq!(caps.get(CAP_CURTAIL_FULL), Some(&false));
        assert_eq!(caps.get(CAP_CURTAIL_EFFICIENCY), Some(&false));
        assert_eq!(caps.get(CAP_CURTAIL_PARTIAL), Some(&false));
    }

    #[test]
    fn test_curtail_capability_requires_pause_and_resume() {
        assert!(curtail_capability(true, true));
        assert!(!curtail_capability(true, false));
        assert!(!curtail_capability(false, true));
        assert!(!curtail_capability(false, false));
    }

    #[test]
    fn test_make_to_family_marathon_maps_to_antminer() {
        // Arrange
        let make = "Marathon";

        // Act
        let family = make_to_family(make);

        // Assert
        assert_eq!(family, Some(FAMILY_ANTMINER));
    }

    #[test]
    fn test_firmware_to_variant_marafw_prefix() {
        // Arrange
        let firmware = "MARAFW_1.0.0";

        // Act
        let variant = firmware_to_variant(firmware);

        // Assert
        assert_eq!(variant, VARIANT_MARATHON);
    }

    #[test]
    fn test_firmware_to_variant_marathon_display_name() {
        // Arrange
        let firmware = "Marathon";

        // Act
        let variant = firmware_to_variant(firmware);

        // Assert
        assert_eq!(variant, VARIANT_MARATHON);
    }

    #[test]
    fn test_detect_variant_marathon_make_fallback() {
        // Arrange — bare version string with no recognizable firmware token; make is "Marathon"
        let make = "Marathon";
        let firmware = "1.0.0";

        // Act
        let variant = detect_variant(make, firmware);

        // Assert
        assert_eq!(variant, VARIANT_MARATHON);
    }

    #[test]
    fn test_make_to_family_auradine() {
        assert_eq!(make_to_family("Auradine"), Some(FAMILY_AURADINE));
    }

    #[test]
    fn test_make_to_family_auradine_case_insensitive() {
        assert_eq!(make_to_family("auradine"), Some(FAMILY_AURADINE));
        assert_eq!(make_to_family("AURADINE"), Some(FAMILY_AURADINE));
    }

    #[test]
    fn test_make_to_family_all_known_families() {
        // Verify every supported family resolves correctly
        assert_eq!(make_to_family("WhatsMiner"), Some(FAMILY_WHATSMINER));
        assert_eq!(make_to_family("Antminer"), Some(FAMILY_ANTMINER));
        assert_eq!(make_to_family("AvalonMiner"), Some(FAMILY_AVALONMINER));
        assert_eq!(make_to_family("BitAxe"), Some(FAMILY_BITAXE));
        assert_eq!(make_to_family("NerdAxe"), Some(FAMILY_NERDAXE));
        assert_eq!(make_to_family("ePIC"), Some(FAMILY_EPIC));
        assert_eq!(make_to_family("Auradine"), Some(FAMILY_AURADINE));
    }

    #[test]
    fn test_make_to_family_unknown_returns_none() {
        assert_eq!(make_to_family("UnknownMiner"), None);
        assert_eq!(make_to_family(""), None);
    }

    #[test]
    fn test_make_to_family_aftermarket_maps_to_antminer() {
        // VNish, Braiins, LuxOS, Marathon all map to antminer family
        assert_eq!(make_to_family("VNish"), Some(FAMILY_ANTMINER));
        assert_eq!(make_to_family("Braiins"), Some(FAMILY_ANTMINER));
        assert_eq!(make_to_family("LuxOS"), Some(FAMILY_ANTMINER));
        assert_eq!(make_to_family("Marathon"), Some(FAMILY_ANTMINER));
    }
}
