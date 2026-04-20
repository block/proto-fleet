use std::collections::HashMap;
use std::path::PathBuf;

use anyhow::{Context, Result};
use serde::Deserialize;

#[derive(Debug, Deserialize)]
pub struct PluginConfig {
    pub plugin: PluginSettings,
    pub miners: HashMap<String, MinerFamilyConfig>,
}

#[derive(Debug, Deserialize)]
pub struct PluginSettings {
    #[serde(default = "default_log_level")]
    pub log_level: String,
    #[serde(default = "default_discovery_timeout")]
    pub discovery_timeout_seconds: u64,
    #[serde(default = "default_cache_ttl")]
    pub telemetry_cache_ttl_seconds: u64,
}

fn default_log_level() -> String {
    "info".into()
}
fn default_discovery_timeout() -> u64 {
    10
}
fn default_cache_ttl() -> u64 {
    5
}

#[derive(Debug, Deserialize)]
pub struct MinerFamilyConfig {
    #[serde(flatten)]
    pub firmware: HashMap<String, FirmwareConfig>,
}

#[derive(Debug, Deserialize)]
pub struct FirmwareConfig {
    #[serde(default)]
    pub enabled: bool,
}

impl PluginConfig {
    /// Check if a specific firmware variant is enabled for a family.
    pub fn is_firmware_enabled(&self, family: &str, variant: &str) -> bool {
        self.miners
            .get(family)
            .and_then(|f| f.firmware.get(variant))
            .is_some_and(|fw| fw.enabled)
    }
}

/// Find the config file bundled next to the binary, or in the current directory.
fn find_config() -> Result<PathBuf> {
    let candidates = ["asicrs-config.yaml", "config.yaml"];

    // Check next to the binary first (deployment layout)
    if let Ok(exe) = std::env::current_exe() {
        let dir = exe.parent().unwrap_or(exe.as_ref());
        for name in &candidates {
            let candidate = dir.join(name);
            if candidate.exists() {
                return Ok(candidate);
            }
        }
    }

    // Check current directory
    for name in &candidates {
        let cwd = PathBuf::from(name);
        if cwd.exists() {
            return Ok(cwd);
        }
    }

    anyhow::bail!(
        "asicrs-config.yaml or config.yaml not found next to binary or in current directory"
    )
}

pub fn load_config() -> Result<PluginConfig> {
    let path = find_config()?;
    tracing::info!(path = %path.display(), "Loading config");
    let contents = std::fs::read_to_string(&path)
        .with_context(|| format!("Failed to read {}", path.display()))?;
    let config: PluginConfig =
        serde_yml::from_str(&contents).with_context(|| "Failed to parse config.yaml")?;
    Ok(config)
}
