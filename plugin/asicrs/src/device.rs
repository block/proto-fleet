use std::collections::HashMap;
use std::panic::AssertUnwindSafe;
use std::sync::Arc;
use std::time::Duration;

use asic_rs::MinerFactory;
use asic_rs_core::config::pools::{PoolConfig, PoolGroupConfig};
use asic_rs_core::config::tuning::TuningConfig;
use asic_rs_core::data::firmware::FirmwareImage;
use asic_rs_core::data::miner::{MinerData, MiningMode, TuningTarget};
use asic_rs_core::data::pool::PoolURL;
use asic_rs_core::traits::miner::{ExposeSecret, Miner, MinerAuth};
use chrono::{DateTime, Utc};
use digest_auth::{AuthContext, HttpMethod};
use futures::FutureExt;
use proto_fleet_plugin::capabilities::*;
use reqwest::header::AUTHORIZATION;
use tokio::sync::Mutex;
use tokio::time::Instant;

use proto_fleet_plugin::pb;

use crate::capabilities::{
    detect_variant, display_model, probe_capabilities, verify_identity, Capabilities,
};

/// Minimum interval between reconnection attempts to avoid hammering offline miners.
const RECONNECT_BACKOFF: Duration = Duration::from_secs(30);
/// Timeout for establishing a new miner connection.
const CONNECT_TIMEOUT: Duration = Duration::from_secs(10);
/// Timeout for identity verification get_data() during reconnect.
const IDENTITY_TIMEOUT: Duration = Duration::from_secs(10);
/// Timeout for telemetry get_data().
const TELEMETRY_TIMEOUT: Duration = Duration::from_secs(15);
/// Timeout for individual miner control/query operations.
const OP_TIMEOUT: Duration = Duration::from_secs(20);
/// Timeout for firmware upload/apply operations. Matches the legacy Go plugin.
const FIRMWARE_UPDATE_TIMEOUT: Duration = Duration::from_secs(30 * 60);
/// Match the server-side firmware upload limit.
const MAX_FIRMWARE_FILE_SIZE_BYTES: u64 = 500 * 1024 * 1024;
/// Keep plugin log responses below common gRPC message-size limits.
const MAX_LOG_RESPONSE_BYTES: usize = 512 * 1024;
/// Bound retained continuation batches so a sequence of log downloads cannot grow forever.
const MAX_LOG_BATCHES: usize = 16;
/// Timeout for write validation probe.
const WRITE_PROBE_TIMEOUT: Duration = Duration::from_secs(10);
/// Shorter timeout for the MiningMode attempt in set_power_target. asic-rs internally
/// uses ~5s per RPC, and the V3→V2 fallback chain makes two calls, so 12s is enough
/// for miners that support it while keeping the fallback path fast on those that don't.
const MODE_ATTEMPT_TIMEOUT: Duration = Duration::from_secs(12);
/// Fallback power floor (watts) — low enough that any miner clamps to its real minimum.
const POWER_FLOOR_WATTS: f64 = 100.0;
/// Fallback power ceiling (watts) — high enough that any miner clamps to its real maximum.
const POWER_CEILING_WATTS: f64 = 50_000.0;

/// Wraps an asic-rs miner instance and provides fleet SDK operations.
pub struct AsicRsDevice {
    pub id: String,
    pub info: pb::DeviceInfo,
    caps: Mutex<Capabilities>,
    /// True once probe_capabilities() has run for this device.
    /// Distinguishes "probed, all control caps false" from "never probed".
    probed: Mutex<bool>,
    /// Live model string, populated from miner data during ensure_connected().
    /// The server may not pass model via NewDevice, so we discover it from the miner.
    model: Mutex<String>,

    miner: Arc<Mutex<Option<Box<dyn Miner>>>>,
    cache_ttl: Duration,
    last_data: Mutex<Option<(Instant, MinerData)>>,
    pre_full_curtail_mining: Mutex<FullCurtailMiningState>,
    last_connect_attempt: Mutex<Option<Instant>>,
    factory: Arc<MinerFactory>,
    auth: Mutex<Option<MinerAuth>>,
    log_batches: Mutex<HashMap<String, String>>,
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
enum FullCurtailMiningState {
    Unknown,
    WasMining,
    WasNotMining,
}

impl FullCurtailMiningState {
    fn from_mining_status(was_mining: bool) -> Self {
        if was_mining {
            Self::WasMining
        } else {
            Self::WasNotMining
        }
    }

    fn restore_decision(self) -> Option<bool> {
        match self {
            Self::Unknown => None,
            Self::WasMining => Some(true),
            Self::WasNotMining => Some(false),
        }
    }
}

impl AsicRsDevice {
    pub fn new(
        id: String,
        info: pb::DeviceInfo,
        caps: Capabilities,
        miner: Option<Box<dyn Miner>>,
        cache_ttl: Duration,
        factory: Arc<MinerFactory>,
        auth: Option<MinerAuth>,
    ) -> Self {
        let model = info.model.clone();
        Self {
            id,
            info,
            caps: Mutex::new(caps),
            probed: Mutex::new(false),
            model: Mutex::new(model),
            miner: Arc::new(Mutex::new(miner)),
            cache_ttl,
            last_data: Mutex::new(None),
            pre_full_curtail_mining: Mutex::new(FullCurtailMiningState::Unknown),
            last_connect_attempt: Mutex::new(None),
            factory,
            auth: Mutex::new(auth),
            log_batches: Mutex::new(HashMap::new()),
        }
    }

    /// Check that a capability is enabled before executing an operation.
    /// Returns a specific error prefix so the driver can map it to UNIMPLEMENTED.
    async fn require_cap(&self, cap: &str) -> anyhow::Result<()> {
        match self.caps.lock().await.get(cap) {
            Some(true) => Ok(()),
            _ => Err(anyhow::anyhow!("[unsupported] {cap}")),
        }
    }

    /// Get a snapshot of current capabilities.
    pub async fn get_caps(&self) -> Capabilities {
        self.caps.lock().await.clone()
    }

    /// Whether capabilities have been probed from the live miner.
    pub async fn is_probed(&self) -> bool {
        *self.probed.lock().await
    }

    /// Live model string, populated from miner data during connection.
    pub async fn model(&self) -> String {
        self.model.lock().await.clone()
    }

    /// Retry capability/model probing on a connected but unprobed device.
    /// Best-effort: failures are logged but don't disconnect the device.
    async fn retry_probe(&self) {
        let guard = self.miner.lock().await;
        let Some(miner) = guard.as_ref() else {
            return;
        };
        if let Ok(Ok(data)) =
            catch_panic(tokio::time::timeout(IDENTITY_TIMEOUT, miner.get_data())).await
        {
            *self.caps.lock().await = probe_capabilities(miner.as_ref());
            *self.probed.lock().await = true;
            let variant = detect_variant(&data.device_info.make, &data.device_info.firmware);
            let model = display_model(&data.device_info.make, variant, &data.device_info.model);
            if !model.is_empty() {
                *self.model.lock().await = model;
            }
        }
    }

    /// Ensure we have a live miner connection. Returns Ok if connected.
    /// Performs network I/O without holding the miner lock to avoid blocking
    /// concurrent operations for the duration of reconnection.
    pub async fn ensure_connected(&self) -> anyhow::Result<()> {
        // Quick check: already connected?
        if self.miner.lock().await.is_some() {
            // Connected but not probed (e.g. transient get_data failure on first connect).
            // Retry the probe so caps/model aren't stuck at defaults.
            if !*self.probed.lock().await {
                self.retry_probe().await;
            }
            return Ok(());
        }

        // Backoff: don't retry too frequently for offline miners
        {
            let mut last = self.last_connect_attempt.lock().await;
            if let Some(ts) = *last {
                if ts.elapsed() < RECONNECT_BACKOFF {
                    return Err(anyhow::anyhow!(
                        "Reconnect backoff: last attempt was {}s ago",
                        ts.elapsed().as_secs()
                    ));
                }
            }
            *last = Some(Instant::now());
        }

        let host = &self.info.host;
        let ip: std::net::IpAddr = host
            .parse()
            .map_err(|_| anyhow::anyhow!("Invalid IP: {}", host))?;

        // Network I/O happens without holding self.miner lock
        let factory = self.factory.clone();
        let miner = catch_panic(async move {
            tokio::time::timeout(CONNECT_TIMEOUT, factory.get_miner(ip)).await
        })
        .await?
        .map_err(|_| anyhow::anyhow!("Timeout connecting to {}", host))?
        .map_err(|e| anyhow::anyhow!("Failed to connect to {}: {}", host, e))?;

        let mut m = match miner {
            Some(m) => m,
            None => anyhow::bail!("No miner found at {}", host),
        };

        // Apply auth BEFORE identity check -- miners that require auth for read
        // operations (e.g. get_data) will fail the identity probe without credentials.
        let auth = self.auth.lock().await.clone();
        if let Some(ref auth) = auth {
            m.set_auth(auth.clone());
        }

        // Validate identity and refresh capabilities from live firmware data.
        // Fail closed: if identity cannot be verified, reject the connection.
        let has_identity = !self.info.model.is_empty()
            || !self.info.serial_number.is_empty()
            || !self.info.mac_address.is_empty();
        if has_identity {
            let data_result =
                catch_panic(tokio::time::timeout(IDENTITY_TIMEOUT, m.get_data())).await;
            match data_result {
                Ok(Ok(data)) => {
                    let actual_serial = data.serial_number.as_deref().unwrap_or("");
                    let actual_mac = data.mac.as_ref().map(|m| m.to_string()).unwrap_or_default();

                    let variant =
                        detect_variant(&data.device_info.make, &data.device_info.firmware);
                    let actual_model =
                        display_model(&data.device_info.make, variant, &data.device_info.model);

                    verify_identity(
                        &self.info.model,
                        &self.info.serial_number,
                        &self.info.mac_address,
                        &actual_model,
                        actual_serial,
                        &actual_mac,
                    )
                    .map_err(|reason| {
                        tracing::error!(device_id = %self.id, reason = %reason, "Identity mismatch on reconnect");
                        anyhow::anyhow!("IP reassigned at {}: {}", host, reason)
                    })?;

                    // Refresh capabilities and model from the live miner instance
                    *self.caps.lock().await = probe_capabilities(m.as_ref());
                    *self.probed.lock().await = true;
                    if !actual_model.is_empty() {
                        *self.model.lock().await = actual_model;
                    }
                }
                Ok(Err(_)) => {
                    return Err(anyhow::anyhow!(
                        "Identity check timed out for {}; rejecting reconnect",
                        host
                    ));
                }
                Err(e) => {
                    return Err(anyhow::anyhow!(
                        "Identity check failed for {}: {}; rejecting reconnect",
                        host,
                        e
                    ));
                }
            }
        } else {
            // No identity to verify — still probe for model and capabilities.
            // This handles the case where NewDevice didn't include model/serial/MAC.
            if let Ok(Ok(data)) =
                catch_panic(tokio::time::timeout(IDENTITY_TIMEOUT, m.get_data())).await
            {
                *self.caps.lock().await = probe_capabilities(m.as_ref());
                *self.probed.lock().await = true;
                let variant = detect_variant(&data.device_info.make, &data.device_info.firmware);
                let model = display_model(&data.device_info.make, variant, &data.device_info.model);
                if !model.is_empty() {
                    *self.model.lock().await = model;
                }
            }
        }

        tracing::debug!(device_id = %self.id, host = %host, "Reconnected to miner");
        *self.miner.lock().await = Some(m);

        // Reset backoff on successful connection
        *self.last_connect_attempt.lock().await = None;

        Ok(())
    }

    /// Get telemetry data, using cache if fresh enough.
    pub async fn get_data(&self) -> anyhow::Result<MinerData> {
        // Check cache
        {
            let cache = self.last_data.lock().await;
            if let Some((ts, ref data)) = *cache {
                if ts.elapsed() < self.cache_ttl {
                    return Ok(data.clone());
                }
            }
        }

        let guard = self.connected_miner().await?;
        let miner = guard.as_ref().unwrap();

        let result = catch_panic(tokio::time::timeout(TELEMETRY_TIMEOUT, miner.get_data())).await;

        // On failure, invalidate the connection so the next call forces a fresh probe
        match result {
            Ok(Ok(data)) => {
                let mut cache = self.last_data.lock().await;
                *cache = Some((Instant::now(), data.clone()));
                Ok(data)
            }
            Ok(Err(_)) => {
                drop(guard);
                self.invalidate_connection().await;
                Err(anyhow::anyhow!("get_data timed out"))
            }
            Err(e) => {
                drop(guard);
                self.invalidate_connection().await;
                Err(e)
            }
        }
    }

    /// Convert MinerData to proto DeviceMetrics.
    pub fn to_device_metrics(&self, data: &MinerData) -> pb::DeviceMetrics {
        let now = std::time::SystemTime::now();
        let timestamp = prost_types::Timestamp::from(now);

        tracing::debug!(
            device_id = %self.id,
            hashrate = ?data.hashrate.as_ref().map(|h| format!("{:?}", h)),
            wattage = ?data.wattage.as_ref().map(|w| w.as_watts()),
            efficiency = ?data.efficiency,
            avg_temp = ?data.average_temperature.as_ref().map(|t| t.as_celsius()),
            is_mining = data.is_mining,
            fans = data.fans.len(),
            boards = data.hashboards.len(),
            "telemetry data summary"
        );

        let (health, health_reason) = determine_health(data);

        // Device-level aggregated metrics
        let hashrate_hs = data
            .hashrate
            .as_ref()
            .map(|hr| metric_rate(hashrate_as_hs(hr)));

        let power_w = data.wattage.as_ref().map(|w| metric_gauge(w.as_watts()));

        let temp_c = data
            .average_temperature
            .as_ref()
            .map(|t| metric_gauge(t.as_celsius()));

        // Efficiency is meaningless when hashrate is zero (stopped/ramping up)
        let has_hashrate = data
            .hashrate
            .as_ref()
            .map(|hr| hashrate_as_hs(hr) > 0.0)
            .unwrap_or(false);
        let efficiency_jh = if has_hashrate {
            data.efficiency.map(|eff| {
                // asic-rs reports J/TH, fleet stores as J/H internally (multiplies by 1e12 for display)
                metric_gauge(eff / 1e12)
            })
        } else {
            None
        };

        // Hash boards
        let hash_boards: Vec<pb::HashBoardMetrics> = data
            .hashboards
            .iter()
            .enumerate()
            .map(|(i, board)| {
                let status = determine_board_status(board);
                let info = pb::ComponentInfo {
                    index: i as i32,
                    name: format!("hashboard_{i}"),
                    status: status.into(),
                    status_reason: None,
                    timestamp: None,
                };

                let hr = board
                    .hashrate
                    .as_ref()
                    .map(|h| metric_rate(hashrate_as_hs(h)));
                let temp = board
                    .board_temperature
                    .as_ref()
                    .map(|t| metric_gauge(t.as_celsius()));
                let voltage = board.voltage.as_ref().map(|v| metric_gauge(v.as_volts()));
                let freq = board
                    .frequency
                    .as_ref()
                    .map(|f| metric_gauge(f.as_megahertz()));

                pb::HashBoardMetrics {
                    component_info: Some(info),
                    serial_number: board.serial_number.clone(),
                    hash_rate_hs: hr,
                    temp_c: temp,
                    voltage_v: voltage,
                    current_a: None,
                    inlet_temp_c: board
                        .intake_temperature
                        .as_ref()
                        .map(|t| metric_gauge(t.as_celsius())),
                    outlet_temp_c: board
                        .outlet_temperature
                        .as_ref()
                        .map(|t| metric_gauge(t.as_celsius())),
                    ambient_temp_c: None,
                    chip_count: board.working_chips.map(|c| c as i32),
                    chip_frequency_mhz: freq,
                    asics: vec![],
                    fan_metrics: vec![],
                }
            })
            .collect();

        // Fans
        let fan_metrics: Vec<pb::FanMetrics> = data
            .fans
            .iter()
            .enumerate()
            .map(|(i, fan)| {
                let rpm_val = fan.rpm.as_ref().map(|r| r.as_rpm()).unwrap_or(0.0);
                let status = if rpm_val > 0.0 {
                    pb::ComponentStatus::Healthy
                } else {
                    pb::ComponentStatus::Offline
                };
                pb::FanMetrics {
                    component_info: Some(pb::ComponentInfo {
                        index: i as i32,
                        name: format!("fan_{i}"),
                        status: status.into(),
                        status_reason: None,
                        timestamp: None,
                    }),
                    rpm: fan.rpm.as_ref().map(|r| metric_gauge(r.as_rpm())),
                    temp_c: None,
                    percent: None,
                }
            })
            .collect();

        // PSU -- asic-rs gives us wattage at device level
        let psu_metrics: Vec<pb::PsuMetrics> = if data.wattage.is_some() {
            vec![pb::PsuMetrics {
                component_info: Some(pb::ComponentInfo {
                    index: 0,
                    name: "psu_0".into(),
                    status: pb::ComponentStatus::Healthy.into(),
                    status_reason: None,
                    timestamp: None,
                }),
                output_power_w: data.wattage.as_ref().map(|w| metric_gauge(w.as_watts())),
                output_voltage_v: None,
                output_current_a: None,
                input_power_w: None,
                input_voltage_v: None,
                input_current_a: None,
                hotspot_temp_c: None,
                efficiency_percent: None,
                fan_metrics: vec![],
            }]
        } else {
            vec![]
        };

        let firmware_version = data.firmware_version.clone().unwrap_or_default();

        pb::DeviceMetrics {
            device_id: self.id.clone(),
            timestamp: Some(timestamp),
            health: health.into(),
            health_reason,
            hashrate_hs,
            temp_c,
            fan_rpm: fan_metrics.first().and_then(|f| f.rpm),
            power_w,
            efficiency_jh,
            hash_boards,
            psu_metrics,
            control_board_metrics: vec![],
            fan_metrics,
            sensor_metrics: vec![],
            firmware_version,
        }
    }

    /// Convert MinerData messages to proto DeviceErrors.
    pub fn to_device_errors(&self, data: &MinerData) -> pb::DeviceErrors {
        let now = prost_types::Timestamp::from(std::time::SystemTime::now());

        let errors: Vec<pb::DeviceError> = data
            .messages
            .iter()
            .map(|msg| {
                let (miner_error, severity, component_type) = classify_error(&msg.message);

                let mut vendor_attributes = std::collections::HashMap::new();
                if msg.code != 0 {
                    vendor_attributes.insert("vendor_error_code".into(), msg.code.to_string());
                }

                pb::DeviceError {
                    miner_error: miner_error.into(),
                    cause_summary: msg.message.clone(),
                    recommended_action: "Check device status".into(),
                    severity: severity.into(),
                    first_seen_at: Some(now),
                    last_seen_at: Some(now),
                    closed_at: None,
                    vendor_attributes,
                    device_id: self.id.clone(),
                    component_id: None,
                    impact: String::new(),
                    summary: msg.message.clone(),
                    component_type: component_type.into(),
                }
            })
            .collect();

        pb::DeviceErrors {
            device_id: self.id.clone(),
            errors,
        }
    }

    /// Invalidate the telemetry cache so the next poll fetches fresh data.
    async fn invalidate_cache(&self) {
        let mut cache = self.last_data.lock().await;
        *cache = None;
    }

    /// Invalidate the miner connection and cached telemetry so the next operation reconnects.
    async fn invalidate_connection(&self) {
        let mut guard = self.miner.lock().await;
        *guard = None;
        drop(guard);
        self.invalidate_cache().await;
    }

    /// Ensure connected and acquire the miner lock.
    /// Returns the locked guard; caller can borrow the inner `dyn Miner` via `.as_ref()`.
    async fn connected_miner(
        &self,
    ) -> anyhow::Result<tokio::sync::MutexGuard<'_, Option<Box<dyn Miner>>>> {
        self.ensure_connected().await?;
        let guard = self.miner.lock().await;
        if guard.is_none() {
            return Err(anyhow::anyhow!("Not connected"));
        }
        Ok(guard)
    }

    // --- Control operations ---

    pub async fn start_mining(&self) -> anyhow::Result<()> {
        // Connect first to probe live caps, then check capability
        let guard = self.connected_miner().await?;
        self.require_cap(CAP_MINING_START).await?;
        let miner = guard.as_ref().unwrap();
        let result = catch_panic(tokio::time::timeout(OP_TIMEOUT, miner.resume(None))).await?;
        let ok = result
            .map_err(|_| anyhow::anyhow!("resume_mining timed out"))?
            .map_err(|e| anyhow::anyhow!("resume_mining failed: {e}"))?;
        if !ok {
            return Err(anyhow::anyhow!("resume_mining command returned false"));
        }
        drop(guard);
        self.clear_full_curtailment_state().await;
        self.invalidate_cache().await;
        Ok(())
    }

    pub async fn stop_mining(&self) -> anyhow::Result<()> {
        let guard = self.connected_miner().await?;
        self.require_cap(CAP_MINING_STOP).await?;
        let miner = guard.as_ref().unwrap();
        let result = catch_panic(tokio::time::timeout(OP_TIMEOUT, miner.pause(None))).await?;
        let ok = result
            .map_err(|_| anyhow::anyhow!("stop_mining timed out"))?
            .map_err(|e| anyhow::anyhow!("stop_mining failed: {e}"))?;
        if !ok {
            return Err(anyhow::anyhow!("stop_mining command returned false"));
        }
        drop(guard);
        self.clear_full_curtailment_state().await;
        self.invalidate_cache().await;
        Ok(())
    }

    pub async fn curtail_full(&self) -> anyhow::Result<()> {
        if *self.probed.lock().await {
            self.require_cap(CAP_CURTAIL_FULL).await?;
        }
        self.invalidate_cache().await;
        let was_mining = self.get_data().await?.is_mining;
        let guard = self.connected_miner().await?;
        self.require_cap(CAP_CURTAIL_FULL).await?;
        let miner = guard.as_ref().unwrap();
        let result = catch_panic(tokio::time::timeout(OP_TIMEOUT, miner.pause(None))).await?;
        let ok = result
            .map_err(|_| anyhow::anyhow!("curtail_full timed out"))?
            .map_err(|e| anyhow::anyhow!("curtail_full failed: {e}"))?;
        if !ok {
            return Err(anyhow::anyhow!("curtail_full command returned false"));
        }
        drop(guard);
        self.record_full_curtailment_state(was_mining).await;
        self.invalidate_cache().await;
        Ok(())
    }

    pub async fn uncurtail_full(&self) -> anyhow::Result<()> {
        if *self.probed.lock().await {
            self.require_cap(CAP_CURTAIL_FULL).await?;
        }
        let should_resume = self.full_curtailment_should_resume().await.unwrap_or(true);
        if !should_resume {
            self.clear_full_curtailment_state().await;
            self.invalidate_cache().await;
            return Ok(());
        }
        let guard = self.connected_miner().await?;
        self.require_cap(CAP_CURTAIL_FULL).await?;
        let miner = guard.as_ref().unwrap();
        let result = catch_panic(tokio::time::timeout(OP_TIMEOUT, miner.resume(None))).await?;
        let ok = result
            .map_err(|_| anyhow::anyhow!("uncurtail_full timed out"))?
            .map_err(|e| anyhow::anyhow!("uncurtail_full failed: {e}"))?;
        if !ok {
            return Err(anyhow::anyhow!("uncurtail_full command returned false"));
        }
        drop(guard);
        self.clear_full_curtailment_state().await;
        self.invalidate_cache().await;
        Ok(())
    }

    async fn record_full_curtailment_state(&self, was_mining: bool) {
        let mut state = self.pre_full_curtail_mining.lock().await;
        if *state == FullCurtailMiningState::Unknown {
            *state = FullCurtailMiningState::from_mining_status(was_mining);
        }
    }

    async fn full_curtailment_should_resume(&self) -> Option<bool> {
        self.pre_full_curtail_mining.lock().await.restore_decision()
    }

    async fn clear_full_curtailment_state(&self) {
        *self.pre_full_curtail_mining.lock().await = FullCurtailMiningState::Unknown;
    }

    pub async fn reboot(&self) -> anyhow::Result<()> {
        let guard = self.connected_miner().await?;
        self.require_cap(CAP_REBOOT).await?;
        let miner = guard.as_ref().unwrap();
        let result = catch_panic(tokio::time::timeout(OP_TIMEOUT, miner.restart())).await?;
        // Reboot commands commonly fail with connection drops -- treat as benign
        match result {
            Ok(Ok(_)) => {}
            Ok(Err(e)) if is_benign_command_error(&e.to_string()) => {
                tracing::info!(device_id = %self.id, error = %e, "Benign error during reboot (expected)");
            }
            Ok(Err(e)) => return Err(anyhow::anyhow!("reboot failed: {e}")),
            Err(_) => {
                tracing::info!(device_id = %self.id, "Reboot timed out (expected for disruptive commands)");
            }
        }
        // Connection will be stale after reboot
        drop(guard);
        self.invalidate_connection().await;
        Ok(())
    }

    pub async fn blink_led(&self) -> anyhow::Result<()> {
        let guard = self.connected_miner().await?;
        self.require_cap(CAP_LED_BLINK).await?;
        let miner = guard.as_ref().unwrap();
        let result = catch_panic(tokio::time::timeout(
            OP_TIMEOUT,
            miner.set_fault_light(true),
        ))
        .await?;
        let ok = result
            .map_err(|_| anyhow::anyhow!("blink_led timed out"))?
            .map_err(|e| anyhow::anyhow!("blink_led failed: {e}"))?;
        if !ok {
            return Err(anyhow::anyhow!("blink_led command returned false"));
        }

        // Auto-off after 30 seconds with timeout to avoid indefinite mutex hold
        let miner_ref = self.miner.clone();
        tokio::spawn(async move {
            tokio::time::sleep(Duration::from_secs(30)).await;
            let guard = miner_ref.lock().await;
            if let Some(miner) = guard.as_ref() {
                let result = catch_panic(tokio::time::timeout(
                    WRITE_PROBE_TIMEOUT,
                    miner.set_fault_light(false),
                ))
                .await;
                if result.is_err() {
                    tracing::warn!("LED auto-off failed or timed out");
                    // Don't invalidate connection here -- let the next real operation handle it
                }
            }
        });

        Ok(())
    }

    pub async fn get_mining_pools(&self) -> anyhow::Result<Vec<pb::ConfiguredPool>> {
        let guard = self.connected_miner().await?;
        self.require_cap(CAP_GET_MINING_POOLS).await?;
        let miner = guard.as_ref().unwrap();

        let result = catch_panic(tokio::time::timeout(OP_TIMEOUT, miner.get_pools())).await?;
        let pool_groups = result.map_err(|_| anyhow::anyhow!("get_pools timed out"))?;

        let mut pools = Vec::new();
        for (i, group) in pool_groups.iter().enumerate() {
            for pool in &group.pools {
                let url = pool.url.as_ref().map(|u| u.to_string()).unwrap_or_default();
                if url.is_empty() {
                    continue; // skip unconfigured pool slots
                }
                pools.push(pb::ConfiguredPool {
                    priority: i as i32,
                    url,
                    username: pool.user.clone().unwrap_or_default(),
                });
            }
        }
        Ok(pools)
    }

    pub async fn update_mining_pools(&self, pools: Vec<pb::MiningPool>) -> anyhow::Result<()> {
        let guard = self.connected_miner().await?;
        self.require_cap(CAP_UPDATE_MINING_POOLS).await?;
        let miner = guard.as_ref().unwrap();

        let mut sorted_pools = pools;
        sorted_pools.sort_by_key(|p| p.priority);

        let pool_configs: Vec<PoolConfig> = sorted_pools
            .iter()
            .map(|p| PoolConfig {
                url: PoolURL::from(p.url.clone()),
                username: p.worker_name.clone(),
                password: "x".into(),
            })
            .collect();

        let group = PoolGroupConfig {
            name: "default".into(),
            quota: 1,
            pools: pool_configs,
        };

        let result = catch_panic(tokio::time::timeout(
            OP_TIMEOUT,
            miner.set_pools_config(vec![group]),
        ))
        .await?;
        let ok = result
            .map_err(|_| anyhow::anyhow!("set_pools timed out"))?
            .map_err(|e| anyhow::anyhow!("set_pools failed: {e}"))?;
        if !ok {
            return Err(anyhow::anyhow!("set_pools command returned false"));
        }

        Ok(())
    }

    pub async fn download_logs(
        &self,
        since: Option<prost_types::Timestamp>,
        batch_log_uuid: &str,
    ) -> anyhow::Result<(String, bool)> {
        if !batch_log_uuid.is_empty() {
            let mut batches = self.log_batches.lock().await;
            if let Some(remaining) = batches.remove(batch_log_uuid) {
                let (chunk, next) = take_log_chunk(remaining);
                if let Some(next) = next {
                    batches.insert(batch_log_uuid.to_string(), next);
                    return Ok((chunk, true));
                }
                return Ok((chunk, false));
            }
        }

        let guard = self.connected_miner().await?;
        self.require_cap(CAP_LOGS_DOWNLOAD).await?;
        let miner = guard.as_ref().unwrap();

        let result = catch_panic(tokio::time::timeout(OP_TIMEOUT, miner.read_logs())).await?;
        let logs = result
            .map_err(|_| anyhow::anyhow!("download_logs timed out"))?
            .map_err(|e| anyhow::anyhow!("download_logs failed: {e}"))?;
        let logs = filter_logs_since(logs, since)?;
        let (chunk, next) = take_log_chunk(logs);

        if let Some(next) = next {
            if batch_log_uuid.is_empty() {
                return Err(anyhow::anyhow!(
                    "batch_log_uuid is required when logs exceed {MAX_LOG_RESPONSE_BYTES} bytes"
                ));
            }
            let mut batches = self.log_batches.lock().await;
            if batches.len() >= MAX_LOG_BATCHES {
                batches.clear();
            }
            batches.insert(batch_log_uuid.to_string(), next);
            Ok((chunk, true))
        } else {
            Ok((chunk, false))
        }
    }

    pub async fn update_miner_password(
        &self,
        current_password: &str,
        new_password: &str,
    ) -> anyhow::Result<()> {
        let mut guard = self.connected_miner().await?;
        self.require_cap(CAP_UPDATE_MINER_PASSWORD).await?;
        let miner = guard.as_mut().unwrap();

        let original_auth =
            self.auth.lock().await.clone().ok_or_else(|| {
                anyhow::anyhow!("stored credentials are required to update password")
            })?;
        let current_auth = MinerAuth::new(original_auth.username.clone(), current_password);
        miner.set_auth(current_auth);

        let result = catch_panic(tokio::time::timeout(
            OP_TIMEOUT,
            miner.change_password(new_password),
        ))
        .await?;
        let ok = match result
            .map_err(|_| anyhow::anyhow!("update_miner_password timed out"))?
            .map_err(|e| anyhow::anyhow!("update_miner_password failed: {e}"))
        {
            Ok(ok) => ok,
            Err(err) => {
                miner.set_auth(original_auth);
                return Err(err);
            }
        };
        if !ok {
            miner.set_auth(original_auth);
            return Err(anyhow::anyhow!(
                "update_miner_password command returned false"
            ));
        }

        let new_auth = MinerAuth::new(original_auth.username, new_password);
        miner.set_auth(new_auth.clone());
        *self.auth.lock().await = Some(new_auth);

        drop(guard);
        self.invalidate_cache().await;
        Ok(())
    }

    pub async fn update_firmware(&self, firmware: pb::FirmwareFileInfo) -> anyhow::Result<()> {
        if firmware.file_path.is_empty() {
            return Err(anyhow::anyhow!("firmware file path is required"));
        }
        if firmware.file_size < 0 {
            return Err(anyhow::anyhow!("firmware file size must not be negative"));
        }
        if firmware.file_size as u64 > MAX_FIRMWARE_FILE_SIZE_BYTES {
            return Err(anyhow::anyhow!(
                "firmware file size {} exceeds maximum {}",
                firmware.file_size,
                MAX_FIRMWARE_FILE_SIZE_BYTES
            ));
        }
        let metadata = tokio::fs::metadata(&firmware.file_path)
            .await
            .map_err(|e| anyhow::anyhow!("failed to stat firmware file: {e}"))?;
        if metadata.len() > MAX_FIRMWARE_FILE_SIZE_BYTES {
            return Err(anyhow::anyhow!(
                "firmware file size {} exceeds maximum {}",
                metadata.len(),
                MAX_FIRMWARE_FILE_SIZE_BYTES
            ));
        }

        self.ensure_connected().await?;
        self.require_cap(CAP_MANUAL_UPLOAD).await?;

        let mut image = FirmwareImage::from_file_async(&firmware.file_path)
            .await
            .map_err(|e| anyhow::anyhow!("failed to read firmware file: {e}"))?;
        if !firmware.original_filename.is_empty() {
            image.filename = firmware.original_filename;
        }

        if self.is_stock_antminer_device() {
            let ok = self.update_stock_antminer_firmware(image).await?;
            if !ok {
                return Err(anyhow::anyhow!("update_firmware command returned false"));
            }
            self.invalidate_connection().await;
            return Ok(());
        }

        let guard = self.connected_miner().await?;
        self.require_cap(CAP_MANUAL_UPLOAD).await?;
        let miner = guard.as_ref().unwrap();
        let result = catch_panic(tokio::time::timeout(
            FIRMWARE_UPDATE_TIMEOUT,
            miner.upgrade_firmware(image),
        ))
        .await?;
        let ok = match result {
            Err(_) => return Err(anyhow::anyhow!("update_firmware timed out")),
            Ok(Ok(ok)) => ok,
            Ok(Err(e)) => return Err(anyhow::anyhow!("update_firmware failed: {e}")),
        };
        if !ok {
            return Err(anyhow::anyhow!("update_firmware command returned false"));
        }

        drop(guard);
        self.invalidate_connection().await;
        Ok(())
    }

    fn is_stock_antminer_device(&self) -> bool {
        self.info.manufacturer.eq_ignore_ascii_case("bitmain")
            || self.info.manufacturer.eq_ignore_ascii_case("antminer")
    }

    async fn update_stock_antminer_firmware(&self, image: FirmwareImage) -> anyhow::Result<bool> {
        let auth = self
            .auth
            .lock()
            .await
            .clone()
            .ok_or_else(|| anyhow::anyhow!("stock Antminer firmware fallback requires auth"))?;

        let host = &self.info.host;
        let client = reqwest::Client::builder()
            .timeout(FIRMWARE_UPDATE_TIMEOUT)
            .build()
            .map_err(|e| anyhow::anyhow!("failed to create firmware upload client: {e}"))?;

        let challenge_url = format!("http://{host}:80/cgi-bin/get_system_info.cgi");
        let challenge = client
            .get(&challenge_url)
            .timeout(OP_TIMEOUT)
            .send()
            .await
            .map_err(|e| anyhow::anyhow!("firmware auth challenge failed: {e}"))?;
        let www_authenticate = challenge
            .headers()
            .get("www-authenticate")
            .ok_or_else(|| anyhow::anyhow!("firmware auth challenge missing WWW-Authenticate"))?
            .to_str()
            .map_err(|e| anyhow::anyhow!("invalid firmware auth challenge header: {e}"))?;

        let upload_path = "/cgi-bin/upgrade.cgi";
        let mut prompt = digest_auth::parse(www_authenticate)
            .map_err(|e| anyhow::anyhow!("invalid firmware auth challenge: {e}"))?;
        let context = AuthContext::new_with_method(
            &auth.username,
            auth.password.expose_secret(),
            upload_path,
            None::<&[u8]>,
            HttpMethod::POST,
        );
        let authorization = prompt
            .respond(&context)
            .map_err(|e| anyhow::anyhow!("failed to build firmware auth header: {e}"))?;

        let FirmwareImage { filename, bytes } = image;
        let part = reqwest::multipart::Part::bytes(bytes)
            .file_name(filename)
            .mime_str("application/octet-stream")
            .map_err(|e| anyhow::anyhow!("failed to build firmware upload part: {e}"))?;
        let form = reqwest::multipart::Form::new().part("firmware", part);

        let upload_url = format!("http://{host}:80{upload_path}");
        let response = client
            .post(upload_url)
            .multipart(form)
            .header(AUTHORIZATION, authorization.to_header_string())
            .timeout(FIRMWARE_UPDATE_TIMEOUT)
            .send()
            .await
            .map_err(|e| anyhow::anyhow!("firmware upload HTTP request failed: {e}"))?;

        let status = response.status();
        let body = response
            .text()
            .await
            .map_err(|e| anyhow::anyhow!("failed to read firmware upload response body: {e}"))?;
        if !status.is_success() {
            return Err(anyhow::anyhow!(
                "firmware upload failed with status code {status}: {body}"
            ));
        }

        let parsed: serde_json::Value = serde_json::from_str(&body)
            .map_err(|e| anyhow::anyhow!("invalid firmware upload response: {e}"))?;
        let ok = parsed.get("code").and_then(|v| v.as_str()) == Some("U000")
            && parsed.get("stats").and_then(|v| v.as_str()) == Some("success");
        if !ok {
            let message = parsed
                .get("msg")
                .and_then(|v| v.as_str())
                .unwrap_or("unknown error");
            return Err(anyhow::anyhow!("firmware upload rejected: {message}"));
        }

        Ok(true)
    }

    pub async fn set_power_target(&self, mode: pb::PerformanceMode) -> anyhow::Result<()> {
        let guard = self.connected_miner().await?;
        self.require_cap(CAP_POWER_MODE_EFFICIENCY).await?;
        let miner = guard.as_ref().unwrap();

        let mining_mode = match mode {
            pb::PerformanceMode::MaximumHashrate => MiningMode::High,
            pb::PerformanceMode::Efficiency => MiningMode::Low,
            other => return Err(anyhow::anyhow!("Unsupported performance mode: {:?}", other)),
        };

        tracing::debug!(
            device_id = %self.id,
            mining_mode = %mining_mode,
            performance_mode = ?mode,
            "set_power_target"
        );

        catch_panic(async {
            // Try MiningMode first (works on V2 and some V3 firmware).
            let config = TuningConfig::new(TuningTarget::MiningMode(mining_mode));
            let mode_result =
                tokio::time::timeout(MODE_ATTEMPT_TIMEOUT, miner.set_tuning_config(config, None))
                    .await;

            match mode_result {
                Ok(Ok(true)) => Ok(()),
                Ok(Ok(false)) => Err(anyhow::anyhow!("set_tuning_config returned false")),
                // Timeout from fire-and-forget mining mode commands is expected
                Err(_) => Ok(()),
                Ok(Err(mode_err)) => {
                    // MiningMode failed — some V3 firmware doesn't support set.miner.mode
                    // and the V2 fallback may fail due to credential mismatch. Fall back to
                    // an explicit power limit via set.miner.power_limit (V3 auth).
                    // Use extreme values so the miner clamps to its actual min/max.
                    let target_watts = match mining_mode {
                        MiningMode::Low => POWER_FLOOR_WATTS,
                        MiningMode::High => POWER_CEILING_WATTS,
                        _ => return Err(mode_err),
                    };

                    tracing::warn!(
                        device_id = %self.id,
                        error = %mode_err,
                        target_watts,
                        "MiningMode failed, falling back to power limit"
                    );

                    let power_config = TuningConfig::new(TuningTarget::Power(
                        measurements::Power::from_watts(target_watts),
                    ));
                    let ok = tokio::time::timeout(
                        OP_TIMEOUT,
                        miner.set_tuning_config(power_config, None),
                    )
                    .await
                    .map_err(|_| anyhow::anyhow!("power limit fallback timed out"))?
                    .map_err(|e| anyhow::anyhow!("power limit fallback failed: {e}"))?;
                    if !ok {
                        return Err(anyhow::anyhow!("power limit fallback returned false"));
                    }
                    Ok(())
                }
            }
        })
        .await?
    }

    pub async fn close(&self) {
        let mut guard = self.miner.lock().await;
        *guard = None;
    }
}

/// Strategy for validating write access, chosen based on firmware variant.
#[derive(Debug)]
pub(crate) enum WriteAccessProbeStrategy {
    /// LED toggle — LED is auth-gated on this firmware.
    /// Used for: stock, Braiins, LuxOS and anything else with `supports_led=true`.
    Led,
    /// Hostname check — VNish allows LED without auth but returns empty hostname
    /// when unauthenticated. Skip the LED probe entirely for VNish.
    Hostname,
    /// No probe possible.
    None,
}

impl WriteAccessProbeStrategy {
    pub(crate) fn for_miner(make: &str, firmware: &str, supports_led: bool) -> Self {
        match crate::capabilities::detect_variant(make, firmware) {
            crate::capabilities::VARIANT_VNISH => Self::Hostname,
            _ if supports_led => Self::Led,
            _ => Self::None,
        }
    }
}

/// Validate that the miner accepts authenticated operations.
/// `cached_data` is reused by the Hostname strategy to skip a redundant `get_data()` call.
pub async fn validate_write_access(
    miner: &dyn Miner,
    supports_led: bool,
    make: &str,
    firmware: &str,
    cached_data: Option<&MinerData>,
) -> anyhow::Result<()> {
    let strategy = WriteAccessProbeStrategy::for_miner(make, firmware, supports_led);
    tracing::debug!(
        make,
        firmware,
        supports_led,
        ?strategy,
        "write-access probe: starting"
    );
    match strategy {
        WriteAccessProbeStrategy::Led => probe_led(miner).await,
        WriteAccessProbeStrategy::Hostname => probe_hostname(miner, cached_data).await,
        WriteAccessProbeStrategy::None => Ok(()),
    }
}

/// Probe write access via fault light toggle.
/// LED is auth-gated on stock, Braiins, and LuxOS firmware.
async fn probe_led(miner: &dyn Miner) -> anyhow::Result<()> {
    let result = catch_panic(tokio::time::timeout(
        WRITE_PROBE_TIMEOUT,
        miner.set_fault_light(true),
    ))
    .await;
    match result {
        Ok(Ok(Ok(true))) => {
            let _ = catch_panic(tokio::time::timeout(
                WRITE_PROBE_TIMEOUT,
                miner.set_fault_light(false),
            ))
            .await;
        }
        Ok(Ok(Ok(false))) => {
            return Err(anyhow::anyhow!(
                "[unauthenticated] LED command returned false, credentials may lack write permission"
            ));
        }
        Ok(Ok(Err(e))) => {
            return Err(anyhow::anyhow!(
                "[unauthenticated] LED probe failed: {e}, credentials may lack write permission"
            ));
        }
        Ok(Err(_)) => {
            return Err(anyhow::anyhow!(
                "[unavailable] LED probe timed out, cannot confirm write permission"
            ));
        }
        Err(e) => {
            return Err(anyhow::anyhow!(
                "[unavailable] LED probe panicked: {e}, cannot confirm write permission"
            ));
        }
    }
    Ok(())
}

/// Probe write access via hostname check.
/// VNish returns an empty hostname when unauthenticated. Uses `cached_data` when
/// available to skip the redundant `get_data()` call.
async fn probe_hostname(miner: &dyn Miner, cached_data: Option<&MinerData>) -> anyhow::Result<()> {
    let hostname = if let Some(data) = cached_data {
        data.hostname.clone()
    } else {
        let data = catch_panic(tokio::time::timeout(WRITE_PROBE_TIMEOUT, miner.get_data())).await;
        match data {
            Ok(Ok(data)) => data.hostname,
            Ok(Err(_)) => {
                return Err(anyhow::anyhow!(
                    "[unavailable] hostname check timed out, cannot confirm write access"
                ));
            }
            Err(e) => {
                return Err(anyhow::anyhow!(
                    "[unavailable] hostname check failed: {e}, cannot confirm write access"
                ));
            }
        }
    };

    if hostname.as_deref().unwrap_or("").is_empty() {
        return Err(anyhow::anyhow!(
            "[unauthenticated] hostname is empty, credentials may be invalid"
        ));
    }
    Ok(())
}

/// Check if an error from a disruptive command (reboot, stop) is expected and benign.
/// Miners commonly drop TCP connections or return errors during reboot/shutdown.
fn is_benign_command_error(err: &str) -> bool {
    let lower = err.to_lowercase();
    lower.contains("connection reset")
        || lower.contains("broken pipe")
        || lower.contains("eof")
        || lower.contains("connection refused")
        || lower.contains("timed out")
        || lower.contains("deadline exceeded")
}

/// Wrap an async call with catch_unwind to convert panics into errors.
pub async fn catch_panic<F, T>(fut: F) -> anyhow::Result<T>
where
    F: std::future::Future<Output = T>,
{
    AssertUnwindSafe(fut).catch_unwind().await.map_err(|panic| {
        let msg = if let Some(s) = panic.downcast_ref::<&str>() {
            s.to_string()
        } else if let Some(s) = panic.downcast_ref::<String>() {
            s.clone()
        } else {
            "unknown panic".to_string()
        };
        tracing::error!(panic = %msg, "asic-rs panicked");
        anyhow::anyhow!("asic-rs panicked: {msg}")
    })
}

// --- Helper functions ---

/// Convert a hashrate to H/s (base unit).
fn hashrate_as_hs(hr: &asic_rs_core::data::hashrate::HashRate) -> f64 {
    hr.clone()
        .as_unit(asic_rs_core::data::hashrate::HashRateUnit::Hash)
        .value
}

fn metric_gauge(value: f64) -> pb::MetricValue {
    pb::MetricValue {
        value,
        kind: pb::MetricKind::Gauge.into(),
        metadata: None,
    }
}

fn metric_rate(value: f64) -> pb::MetricValue {
    pb::MetricValue {
        value,
        kind: pb::MetricKind::Rate.into(),
        metadata: None,
    }
}

fn determine_health(data: &MinerData) -> (pb::HealthStatus, Option<String>) {
    // Explicitly stopped miners are inactive regardless of stale error codes.
    if !data.is_mining {
        return (pb::HealthStatus::HealthHealthyInactive, None);
    }
    if !data.messages.is_empty() {
        return (
            pb::HealthStatus::HealthWarning,
            Some(format!("{} error(s) reported", data.messages.len())),
        );
    }
    if let Some(ref hr) = data.hashrate {
        if hashrate_as_hs(hr) > 0.0 {
            return (pb::HealthStatus::HealthHealthyActive, None);
        }
        return (
            pb::HealthStatus::HealthWarning,
            Some("Mining but no hashrate detected".into()),
        );
    }
    (pb::HealthStatus::HealthUnknown, None)
}

fn determine_board_status(board: &asic_rs_core::data::board::BoardData) -> pb::ComponentStatus {
    if let Some(ref hr) = board.hashrate {
        if hashrate_as_hs(hr) > 0.0 {
            if let (Some(expected), Some(working)) = (board.expected_chips, board.working_chips) {
                if working < expected {
                    return pb::ComponentStatus::Warning;
                }
            }
            return pb::ComponentStatus::Healthy;
        }
    }
    if board.board_temperature.is_some() {
        return pb::ComponentStatus::Warning;
    }
    pb::ComponentStatus::Offline
}

/// Classify an error message into (MinerError, Severity, ComponentType).
fn classify_error(msg: &str) -> (pb::MinerError, pb::Severity, pb::ComponentType) {
    let lower = msg.to_lowercase();

    let miner_error = if lower.contains("fan") {
        pb::MinerError::FanFailed
    } else if lower.contains("psu") || lower.contains("power supply") {
        pb::MinerError::PsuFaultGeneric
    } else if lower.contains("over temperature") || lower.contains("overheat") {
        pb::MinerError::DeviceOverTemperature
    } else if lower.contains("hashboard") || lower.contains("hash board") {
        pb::MinerError::HashboardNotPresent
    } else if lower.contains("eeprom") {
        pb::MinerError::EepromReadFailure
    } else if lower.contains("control board") {
        pb::MinerError::ControlBoardFailure
    } else if lower.contains("firmware") {
        pb::MinerError::FirmwareImageInvalid
    } else {
        pb::MinerError::VendorErrorUnmapped
    };

    let severity = if [
        "over temperature",
        "short",
        "protection",
        "fault",
        "failed",
        "overcurrent",
    ]
    .iter()
    .any(|kw| lower.contains(kw))
    {
        pb::Severity::Critical
    } else if ["deviation", "warning", "ambient", "low"]
        .iter()
        .any(|kw| lower.contains(kw))
    {
        pb::Severity::Minor
    } else {
        pb::Severity::Major
    };

    let component_type = if lower.contains("fan") {
        pb::ComponentType::Fan
    } else if ["hashboard", "hash board", "chip", "asic", "chain"]
        .iter()
        .any(|kw| lower.contains(kw))
    {
        pb::ComponentType::HashBoard
    } else if ["psu", "power supply", "power", "voltage", "current"]
        .iter()
        .any(|kw| lower.contains(kw))
    {
        pb::ComponentType::Psu
    } else if ["eeprom", "firmware", "checksum"]
        .iter()
        .any(|kw| lower.contains(kw))
    {
        pb::ComponentType::Eeprom
    } else if ["control board", "mac", "network"]
        .iter()
        .any(|kw| lower.contains(kw))
    {
        pb::ComponentType::ControlBoard
    } else {
        pb::ComponentType::Unspecified
    };

    (miner_error, severity, component_type)
}

fn take_log_chunk(mut logs: String) -> (String, Option<String>) {
    if logs.len() <= MAX_LOG_RESPONSE_BYTES {
        return (logs, None);
    }

    let mut split_at = MAX_LOG_RESPONSE_BYTES;
    while split_at > 0 && !logs.is_char_boundary(split_at) {
        split_at -= 1;
    }
    if split_at == 0 {
        split_at = logs
            .char_indices()
            .nth(1)
            .map(|(idx, _)| idx)
            .unwrap_or(logs.len());
    }

    let remaining = logs.split_off(split_at);
    (logs, Some(remaining))
}

fn filter_logs_since(
    logs: String,
    since: Option<prost_types::Timestamp>,
) -> anyhow::Result<String> {
    let Some(since) = since else {
        return Ok(logs);
    };
    if since.nanos < 0 || since.nanos >= 1_000_000_000 {
        return Err(anyhow::anyhow!("invalid since timestamp nanos"));
    }
    let Some(since) = DateTime::<Utc>::from_timestamp(since.seconds, since.nanos as u32) else {
        return Err(anyhow::anyhow!("invalid since timestamp"));
    };
    let since_prefix = since.format("%Y-%m-%d %H:%M:%S").to_string();

    Ok(logs
        .lines()
        .filter(|line| {
            let Some(prefix) = line.get(..19) else {
                return true;
            };
            !is_log_timestamp_prefix(prefix) || prefix >= since_prefix.as_str()
        })
        .collect::<Vec<_>>()
        .join("\n"))
}

fn is_log_timestamp_prefix(prefix: &str) -> bool {
    let bytes = prefix.as_bytes();
    bytes.len() == 19
        && bytes[0..4].iter().all(u8::is_ascii_digit)
        && bytes[4] == b'-'
        && bytes[5..7].iter().all(u8::is_ascii_digit)
        && bytes[7] == b'-'
        && bytes[8..10].iter().all(u8::is_ascii_digit)
        && bytes[10] == b' '
        && bytes[11..13].iter().all(u8::is_ascii_digit)
        && bytes[13] == b':'
        && bytes[14..16].iter().all(u8::is_ascii_digit)
        && bytes[16] == b':'
        && bytes[17..19].iter().all(u8::is_ascii_digit)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_classify_error_fan_failure() {
        let (error, severity, component) = classify_error("Fan 1 speed is too low");
        assert!(matches!(error, pb::MinerError::FanFailed));
        assert!(matches!(component, pb::ComponentType::Fan));
        assert!(matches!(severity, pb::Severity::Minor));
    }

    #[test]
    fn test_classify_error_psu_fault() {
        let (error, severity, component) = classify_error("PSU output voltage fault detected");
        assert!(matches!(error, pb::MinerError::PsuFaultGeneric));
        assert!(matches!(severity, pb::Severity::Critical));
        assert!(matches!(component, pb::ComponentType::Psu));
    }

    #[test]
    fn test_classify_error_over_temperature() {
        let (error, severity, _) = classify_error("Over temperature protection triggered");
        assert!(matches!(error, pb::MinerError::DeviceOverTemperature));
        assert!(matches!(severity, pb::Severity::Critical));
    }

    #[test]
    fn test_classify_error_hashboard() {
        let (error, _, component) = classify_error("Hashboard 2 not responding");
        assert!(matches!(error, pb::MinerError::HashboardNotPresent));
        assert!(matches!(component, pb::ComponentType::HashBoard));
    }

    #[test]
    fn test_classify_error_unknown() {
        let (error, severity, component) = classify_error("Something unexpected happened");
        assert!(matches!(error, pb::MinerError::VendorErrorUnmapped));
        assert!(matches!(severity, pb::Severity::Major));
        assert!(matches!(component, pb::ComponentType::Unspecified));
    }

    #[test]
    fn test_classify_error_empty_string() {
        let (error, _, _) = classify_error("");
        assert!(matches!(error, pb::MinerError::VendorErrorUnmapped));
    }

    fn make_miner_data(is_mining: bool) -> MinerData {
        use std::net::IpAddr;
        MinerData {
            schema_version: String::new(),
            timestamp: 0,
            ip: IpAddr::from([0, 0, 0, 0]),
            mac: None,
            device_info: asic_rs_core::data::device::DeviceInfo {
                make: String::new(),
                model: String::new(),
                firmware: String::new(),
                algo: asic_rs_core::data::device::HashAlgorithm::SHA256,
                hardware: asic_rs_core::data::device::MinerHardware {
                    fans: None,
                    boards: None,
                },
            },
            serial_number: None,
            hostname: None,
            api_version: None,
            firmware_version: None,
            control_board_version: None,
            expected_hashboards: None,
            expected_chips: None,
            expected_fans: None,
            expected_hashrate: None,
            hashboards: vec![],
            hashrate: None,
            fans: vec![],
            psu_fans: vec![],
            wattage: None,
            average_temperature: None,
            efficiency: None,
            is_mining,
            uptime: None,
            pools: vec![],
            messages: vec![],
            tuning_target: None,
            scaled_tuning_target: None,
            fluid_temperature: None,
            light_flashing: None,
            total_chips: None,
        }
    }

    #[test]
    fn test_determine_health_not_mining() {
        let data = make_miner_data(false);
        let (health, reason) = determine_health(&data);
        assert!(matches!(health, pb::HealthStatus::HealthHealthyInactive));
        assert!(reason.is_none());
    }

    #[test]
    fn test_determine_health_unknown_no_hashrate() {
        let data = make_miner_data(true);
        let (health, _) = determine_health(&data);
        assert!(matches!(health, pb::HealthStatus::HealthUnknown));
    }

    #[test]
    fn test_is_benign_command_error() {
        assert!(is_benign_command_error("connection reset by peer"));
        assert!(is_benign_command_error("Broken pipe"));
        assert!(is_benign_command_error("request timed out"));
        assert!(is_benign_command_error("context deadline exceeded"));
        assert!(!is_benign_command_error("invalid credentials"));
        assert!(!is_benign_command_error("command not supported"));
    }

    #[tokio::test]
    async fn test_require_cap_enabled() {
        let mut caps = Capabilities::new();
        caps.insert(CAP_REBOOT.into(), true);
        let device = AsicRsDevice::new(
            "test".into(),
            pb::DeviceInfo::default(),
            caps,
            None,
            Duration::from_secs(5),
            Arc::new(MinerFactory::new()),
            None,
        );
        assert!(device.require_cap(CAP_REBOOT).await.is_ok());
        assert!(device.require_cap(CAP_MINING_START).await.is_err());
    }

    #[tokio::test]
    async fn test_curtail_full_requires_curtail_capability() {
        let mut caps = Capabilities::new();
        caps.insert(CAP_MINING_STOP.into(), true);
        caps.insert(CAP_CURTAIL_FULL.into(), false);
        let device = AsicRsDevice::new(
            "test".into(),
            pb::DeviceInfo::default(),
            caps,
            None,
            Duration::from_secs(5),
            Arc::new(MinerFactory::new()),
            None,
        );
        *device.probed.lock().await = true;

        let err = device
            .curtail_full()
            .await
            .expect_err("expected curtail capability error");

        assert!(err.to_string().contains("[unsupported] curtail_full"));
    }

    #[tokio::test]
    async fn test_uncurtail_full_requires_curtail_capability() {
        let mut caps = Capabilities::new();
        caps.insert(CAP_MINING_START.into(), true);
        caps.insert(CAP_CURTAIL_FULL.into(), false);
        let device = AsicRsDevice::new(
            "test".into(),
            pb::DeviceInfo::default(),
            caps,
            None,
            Duration::from_secs(5),
            Arc::new(MinerFactory::new()),
            None,
        );
        *device.probed.lock().await = true;

        let err = device
            .uncurtail_full()
            .await
            .expect_err("expected curtail capability error");

        assert!(err.to_string().contains("[unsupported] curtail_full"));
    }

    #[tokio::test]
    async fn test_full_curtailment_state_preserves_first_snapshot() {
        let device = AsicRsDevice::new(
            "test".into(),
            pb::DeviceInfo::default(),
            Capabilities::new(),
            None,
            Duration::from_secs(5),
            Arc::new(MinerFactory::new()),
            None,
        );

        device.record_full_curtailment_state(true).await;
        device.record_full_curtailment_state(false).await;

        assert_eq!(device.full_curtailment_should_resume().await, Some(true));
    }

    #[tokio::test]
    async fn test_uncurtail_full_skips_resume_when_snapshot_was_not_mining() {
        let mut caps = Capabilities::new();
        caps.insert(CAP_CURTAIL_FULL.into(), true);
        let device = AsicRsDevice::new(
            "test".into(),
            pb::DeviceInfo::default(),
            caps,
            None,
            Duration::from_secs(5),
            Arc::new(MinerFactory::new()),
            None,
        );
        *device.probed.lock().await = true;
        device.record_full_curtailment_state(false).await;

        device
            .uncurtail_full()
            .await
            .expect("uncurtail should skip resume when FULL did not stop mining");

        assert_eq!(device.full_curtailment_should_resume().await, None);
    }

    // --- WriteAccessProbeStrategy::for_miner ---

    #[test]
    fn test_strategy_vnish_firmware_uses_hostname_probe() {
        // Arrange
        let make = "Antminer";
        let firmware = "VNish 1.2.3";

        // Act
        let strategy = WriteAccessProbeStrategy::for_miner(make, firmware, true);

        // Assert — VNish allows LED without auth; hostname check is the real discriminator
        assert!(matches!(strategy, WriteAccessProbeStrategy::Hostname));
    }

    #[test]
    fn test_strategy_vnish_firmware_uses_hostname_even_without_led_support() {
        // Arrange
        let make = "Antminer";
        let firmware = "VNish 1.2.3";

        // Act — supports_led=false shouldn't matter; VNish always uses hostname
        let strategy = WriteAccessProbeStrategy::for_miner(make, firmware, false);

        // Assert
        assert!(matches!(strategy, WriteAccessProbeStrategy::Hostname));
    }

    #[test]
    fn test_strategy_vnish_make_fallback_uses_hostname_probe() {
        // Arrange — VNish sometimes reports make="VNish" with a version-only firmware string
        let make = "VNish";
        let firmware = "2.4.1";

        // Act
        let strategy = WriteAccessProbeStrategy::for_miner(make, firmware, true);

        // Assert — detect_variant falls back to make when firmware string has no keyword
        assert!(matches!(strategy, WriteAccessProbeStrategy::Hostname));
    }

    #[test]
    fn test_strategy_braiins_with_led_uses_led_probe() {
        // Arrange
        let make = "Antminer";
        let firmware = "Braiins OS+ 22.08";

        // Act
        let strategy = WriteAccessProbeStrategy::for_miner(make, firmware, true);

        // Assert
        assert!(matches!(strategy, WriteAccessProbeStrategy::Led));
    }

    #[test]
    fn test_strategy_luxos_with_led_uses_led_probe() {
        // Arrange
        let make = "Antminer";
        let firmware = "LuxOS 2.1.0";

        // Act
        let strategy = WriteAccessProbeStrategy::for_miner(make, firmware, true);

        // Assert
        assert!(matches!(strategy, WriteAccessProbeStrategy::Led));
    }

    #[test]
    fn test_strategy_whatsminer_stock_with_led_uses_led_probe() {
        // Arrange
        let make = "Whatsminer";
        let firmware = "WhatsMiner Stock";

        // Act
        let strategy = WriteAccessProbeStrategy::for_miner(make, firmware, true);

        // Assert
        assert!(matches!(strategy, WriteAccessProbeStrategy::Led));
    }

    #[test]
    fn test_strategy_stock_firmware_without_led_uses_none() {
        // Arrange — device with no LED support and no firmware-specific probe
        let make = "Goldshell";
        let firmware = "GoldshellFirmware";

        // Act
        let strategy = WriteAccessProbeStrategy::for_miner(make, firmware, false);

        // Assert
        assert!(matches!(strategy, WriteAccessProbeStrategy::None));
    }

    #[test]
    fn test_filter_logs_since_keeps_lines_at_or_after_timestamp() {
        let logs = [
            "2026-03-12 09:59:59 old",
            "line without timestamp",
            "2026-03-12 10:00:00 keep",
            "2026-03-12 10:00:01 newer",
        ]
        .join("\n");

        let filtered = filter_logs_since(
            logs,
            Some(prost_types::Timestamp {
                seconds: 1_773_309_600,
                nanos: 0,
            }),
        )
        .unwrap();

        assert!(!filtered.contains("old"));
        assert!(filtered.contains("line without timestamp"));
        assert!(filtered.contains("keep"));
        assert!(filtered.contains("newer"));
    }

    #[test]
    fn test_take_log_chunk_splits_on_utf8_boundary() {
        let logs = format!("{}é{}", "a".repeat(MAX_LOG_RESPONSE_BYTES - 1), "b");

        let (chunk, remaining) = take_log_chunk(logs);

        assert!(chunk.len() <= MAX_LOG_RESPONSE_BYTES);
        assert_eq!(remaining.unwrap(), "éb");
    }
}
