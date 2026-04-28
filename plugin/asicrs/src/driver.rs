use std::collections::HashMap;
use std::net::IpAddr;
use std::sync::Arc;
use std::time::{Duration, Instant};

use asic_rs::MinerFactory;
use asic_rs_core::traits::miner::{Miner, MinerAuth};
use tokio::sync::RwLock;
use tonic::{Request, Response, Status};

use pb::driver_server::Driver;
use proto_fleet_plugin::pb;

use crate::capabilities::{
    default_credentials, detect_variant, driver_base_capabilities, firmware_manufacturer,
    make_to_family, static_base_capabilities, verify_identity, VARIANT_STOCK,
};
use crate::config::{MinerFamilyConfig, PluginConfig};
use crate::device::AsicRsDevice;

const DRIVER_NAME: &str = "asicrs";
const API_VERSION: &str = "v1";

#[allow(clippy::result_large_err)]
fn extract_auth(secret: Option<&pb::SecretBundle>) -> Result<Option<MinerAuth>, Status> {
    let Some(bundle) = secret else {
        return Ok(None);
    };
    let Some(kind) = bundle.kind.as_ref() else {
        return Ok(None);
    };
    match kind {
        pb::secret_bundle::Kind::UserPass(up) => {
            Ok(Some(MinerAuth::new(&up.username, &up.password)))
        }
        _ => Err(Status::invalid_argument(
            "unsupported SecretBundle kind; only UserPass is supported",
        )),
    }
}

/// Map device errors to appropriate gRPC status codes.
fn device_err_to_status(e: anyhow::Error) -> Status {
    let msg = e.to_string();
    let lower = msg.to_lowercase();
    if msg.starts_with("[unsupported]") {
        Status::unimplemented(msg)
    } else if lower.contains("auth")
        || lower.contains("password")
        || lower.contains("credential")
        || lower.contains("forbidden")
        || lower.contains("401")
        || lower.contains("403")
    {
        Status::unauthenticated(msg)
    } else {
        Status::unavailable(msg)
    }
}

/// Ports that asic-rs probes during detection.
/// - 80: Web-based miners (VNish, Braiins, Auradine, Goldshell, etc.)
/// - 4028: Socket-based miners (WhatsMiner CGMiner RPC)
const DISCOVERY_PORTS: &[u16] = &[80, 4028];

/// TTL for the discover→pair miner handle cache.
const MINER_CACHE_TTL: Duration = Duration::from_secs(60);

/// Canonical discovery port per miner family.
/// When a miner is reachable on multiple ports, we only claim it on its
/// canonical port to avoid duplicate discovery and connection exhaustion.
/// - 4028: WhatsMiner, AvalonMiner (CGMiner-style RPC over raw TCP socket)
/// - 80:   Everything else (HTTP web API)
fn canonical_port(family: &str) -> u16 {
    match family {
        crate::capabilities::FAMILY_WHATSMINER => 4028,
        crate::capabilities::FAMILY_AVALONMINER => 4028,
        // Web-based miners: Antminer, BitAxe, NerdAxe, ePIC, Auradine
        _ => 80,
    }
}

/// A cached miner handle with its insertion timestamp.
type MinerCacheEntry = (Instant, Box<dyn Miner>);

/// Thread-safe cache of IP → miner handle, populated by discover_device and
/// consumed by pair_device.  Encapsulates lock acquisition and poison recovery
/// so callers don't duplicate that logic.
struct MinerCache(std::sync::Mutex<HashMap<IpAddr, MinerCacheEntry>>);

impl MinerCache {
    fn new() -> Self {
        Self(std::sync::Mutex::new(HashMap::new()))
    }

    fn insert(&self, ip: IpAddr, miner: Box<dyn Miner>) {
        if let Ok(mut cache) = self.0.lock() {
            cache.insert(ip, (Instant::now(), miner));
        }
    }

    fn remove(&self, ip: &IpAddr) -> Option<Box<dyn Miner>> {
        match self.0.lock() {
            Ok(mut cache) => cache.remove(ip).map(|(_, m)| m),
            Err(e) => {
                tracing::warn!(ip = %ip, "miner cache poisoned; recovering cached miner");
                e.into_inner().remove(ip).map(|(_, m)| m)
            }
        }
    }

    fn evict_stale(&self) {
        if let Ok(mut cache) = self.0.lock() {
            cache.retain(|_, (ts, _)| ts.elapsed() < MINER_CACHE_TTL);
        }
    }
}

/// Wraps a `JoinHandle` and aborts the task when dropped, ensuring the background
/// cache-eviction task doesn't block Tokio's runtime shutdown.
struct AbortOnDrop(tokio::task::JoinHandle<()>);

impl Drop for AbortOnDrop {
    fn drop(&mut self) {
        self.0.abort();
    }
}

pub struct DriverService {
    config: Arc<PluginConfig>,
    factory: Arc<MinerFactory>,
    devices: Arc<RwLock<HashMap<String, Arc<AsicRsDevice>>>>,
    miner_cache: Arc<MinerCache>,
    _cache_eviction_task: AbortOnDrop,
}

impl DriverService {
    pub fn new(config: PluginConfig) -> Self {
        let miner_cache = Arc::new(MinerCache::new());

        // Evict stale cache entries on a background timer so handles don't outlive the TTL
        // when no discover/pair calls occur after a large scan.
        // AbortOnDrop ensures the task is cancelled when DriverService is dropped, so it
        // doesn't block Tokio's runtime shutdown.
        let cache_clone = Arc::clone(&miner_cache);
        let eviction_handle = tokio::spawn(async move {
            let mut interval = tokio::time::interval(MINER_CACHE_TTL / 2);
            interval.tick().await; // skip the immediate first tick
            loop {
                interval.tick().await;
                cache_clone.evict_stale();
            }
        });

        Self {
            config: Arc::new(config),
            factory: Arc::new(MinerFactory::new()),
            devices: Arc::new(RwLock::new(HashMap::new())),
            miner_cache,
            _cache_eviction_task: AbortOnDrop(eviction_handle),
        }
    }

    /// Look up a device by ID. Clones the Arc and releases the read lock
    /// so the caller can await device methods without holding the global lock.
    #[allow(clippy::result_large_err)]
    async fn get_device(&self, device_id: &str) -> Result<Arc<AsicRsDevice>, Status> {
        self.devices
            .read()
            .await
            .get(device_id)
            .cloned()
            .ok_or_else(|| Status::not_found(format!("Device not found: {device_id}")))
    }
}

#[tonic::async_trait]
impl Driver for DriverService {
    // --- Driver Info ---

    async fn handshake(
        &self,
        _req: Request<()>,
    ) -> Result<Response<pb::HandshakeResponse>, Status> {
        Ok(Response::new(pb::HandshakeResponse {
            driver_name: DRIVER_NAME.into(),
            api_version: API_VERSION.into(),
        }))
    }

    async fn describe_driver(
        &self,
        _req: Request<()>,
    ) -> Result<Response<pb::DescribeDriverResponse>, Status> {
        Ok(Response::new(pb::DescribeDriverResponse {
            driver_name: DRIVER_NAME.into(),
            api_version: API_VERSION.into(),
            caps: Some(pb::Capabilities {
                flags: driver_base_capabilities(),
            }),
        }))
    }

    async fn get_discovery_ports(
        &self,
        _req: Request<()>,
    ) -> Result<Response<pb::GetDiscoveryPortsResponse>, Status> {
        Ok(Response::new(pb::GetDiscoveryPortsResponse {
            ports: DISCOVERY_PORTS.iter().map(|p| p.to_string()).collect(),
        }))
    }

    // --- Device Pairing ---

    async fn discover_device(
        &self,
        req: Request<pb::DiscoverDeviceRequest>,
    ) -> Result<Response<pb::DiscoverDeviceResponse>, Status> {
        let req = req.into_inner();
        let port: u16 = req
            .port
            .parse()
            .map_err(|_| Status::invalid_argument(format!("Invalid port: {}", req.port)))?;

        tracing::debug!(ip = %req.ip_address, port = port, "discover_device called");

        if !DISCOVERY_PORTS.contains(&port) {
            tracing::debug!(port = port, "Port not in discovery set, skipping");
            return Err(Status::not_found(format!(
                "Port {port} not in discovery set"
            )));
        }

        let ip: IpAddr = req
            .ip_address
            .parse()
            .map_err(|_| Status::invalid_argument(format!("Invalid IP: {}", req.ip_address)))?;

        let timeout_secs = self.config.plugin.discovery_timeout_seconds;
        let timeout_dur = Duration::from_secs(timeout_secs);
        let factory = self.factory.clone();
        let result = crate::device::catch_panic(async move {
            tokio::time::timeout(timeout_dur, factory.get_miner(ip)).await
        })
        .await;

        let miner = match result {
            Err(e) => {
                return Err(Status::unavailable(format!(
                    "Discovery panicked for {}: {e}",
                    req.ip_address
                )));
            }
            Ok(Err(_)) => {
                tracing::warn!(ip = %req.ip_address, timeout_secs, "get_miner timed out");
                return Err(Status::unavailable(format!(
                    "Timeout discovering {}",
                    req.ip_address
                )));
            }
            Ok(Ok(Err(e))) => {
                tracing::warn!(ip = %req.ip_address, error = %e, "get_miner returned error");
                return Err(Status::unavailable(format!(
                    "Discovery error for {}: {e}",
                    req.ip_address
                )));
            }
            Ok(Ok(Ok(None))) => {
                tracing::debug!(
                    ip = %req.ip_address,
                    "get_miner returned None - no miner identified"
                );
                return Err(Status::not_found(format!(
                    "No miner found at {}",
                    req.ip_address
                )));
            }
            Ok(Ok(Ok(Some(m)))) => {
                tracing::debug!(ip = %req.ip_address, "get_miner succeeded - miner identified");
                m
            }
        };

        // Early reject: use trait-level device info (no network call) to check
        // family, canonical port, and config before doing the expensive get_data().
        let trait_info = miner.get_device_info();

        let family = make_to_family(&trait_info.make).ok_or_else(|| {
            tracing::warn!(ip = %req.ip_address, make = %trait_info.make, "Unsupported manufacturer");
            Status::not_found(format!("Unsupported manufacturer: {}", trait_info.make))
        })?;

        if !self.config.miners.contains_key(family) {
            tracing::warn!(ip = %req.ip_address, family, "Family not configured");
            return Err(Status::not_found(format!("Family {family} not configured")));
        }

        // Only claim device on its canonical port to prevent duplicate discovery.
        let expected_port = canonical_port(family);
        if port != expected_port {
            tracing::debug!(
                ip = %req.ip_address, port, expected_port, family,
                "Skipping non-canonical port for family"
            );
            return Err(Status::not_found(format!(
                "Port {port} is not canonical for {family} (expected {expected_port})"
            )));
        }

        // Full device info: try get_data() for serial, MAC, firmware version.
        // Falls back to trait-level info if the miner requires auth for reads.
        let data = crate::device::catch_panic(tokio::time::timeout(
            Duration::from_secs(timeout_secs),
            miner.get_data(),
        ))
        .await;

        let (make, model, firmware_str, serial_number, mac_address, firmware_version) = match data {
            Ok(Ok(data)) => {
                let make = data.device_info.make.clone();
                let model = data.device_info.model.clone();
                let firmware = data.device_info.firmware.clone();
                let serial = data.serial_number.clone().unwrap_or_default();
                let mac = data.mac.map(|m| m.to_string()).unwrap_or_default();
                let fw_ver = data.firmware_version.clone().unwrap_or_default();
                (make, model, firmware, serial, mac, fw_ver)
            }
            _ => {
                tracing::info!(
                    ip = %req.ip_address,
                    make = %trait_info.make,
                    model = %trait_info.model,
                    "get_data failed, falling back to trait device info"
                );
                (
                    trait_info.make.clone(),
                    trait_info.model.to_string(),
                    trait_info.firmware.clone(),
                    String::new(),
                    String::new(),
                    String::new(),
                )
            }
        };

        let variant = detect_variant(&make, &firmware_str);
        if !self.config.is_firmware_enabled(family, variant) {
            tracing::warn!(ip = %req.ip_address, family, variant, "Firmware variant not enabled");
            return Err(Status::not_found(format!(
                "Firmware variant {variant} not enabled for {family}"
            )));
        }

        let manufacturer = firmware_manufacturer(variant)
            .unwrap_or(make.as_str())
            .to_string();

        let url_scheme = "http";

        tracing::info!(
            manufacturer = %manufacturer,
            model = %model,
            ip = %req.ip_address,
            "Discovered device"
        );

        // Cache the miner handle for reuse by pair_device.
        self.miner_cache.insert(ip, miner);

        Ok(Response::new(pb::DiscoverDeviceResponse {
            device: Some(pb::DeviceInfo {
                host: req.ip_address,
                port: port as i32,
                url_scheme: url_scheme.into(),
                serial_number,
                model,
                manufacturer,
                mac_address,
                firmware_version,
            }),
        }))
    }

    async fn pair_device(
        &self,
        req: Request<pb::PairDeviceRequest>,
    ) -> Result<Response<pb::PairDeviceResponse>, Status> {
        let req = req.into_inner();
        let device_info = req
            .device
            .ok_or_else(|| Status::invalid_argument("Missing device info"))?;
        let auth = extract_auth(req.access.as_ref())?;

        let ip: IpAddr = device_info
            .host
            .parse()
            .map_err(|_| Status::invalid_argument(format!("Invalid IP: {}", device_info.host)))?;

        let timeout_dur = Duration::from_secs(self.config.plugin.discovery_timeout_seconds);
        let cached = self.miner_cache.remove(&ip);
        let was_cached = cached.is_some();
        let mut miner: Box<dyn Miner> = if let Some(m) = cached {
            tracing::debug!(ip = %device_info.host, "reusing cached miner from discovery");
            m
        } else {
            let factory = self.factory.clone();
            crate::device::catch_panic(async move {
                tokio::time::timeout(timeout_dur, factory.get_miner(ip)).await
            })
            .await
            .map_err(|e| {
                Status::unavailable(format!("Pairing panicked for {}: {e}", device_info.host))
            })?
            .map_err(|_| Status::unavailable(format!("Timeout pairing {}", device_info.host)))?
            .map_err(|e| {
                Status::unavailable(format!("Pairing error for {}: {e}", device_info.host))
            })?
            .ok_or_else(|| Status::not_found(format!("No miner found at {}", device_info.host)))?
        };

        // Apply custom auth before validating read access
        if let Some(ref a) = auth {
            miner.set_auth(a.clone());
        }

        // Validate read access. If get_data fails on a cached handle, fall back to a fresh
        // connection — the cached miner may be stale after DHCP churn or a dropped session.
        let data = {
            let first =
                crate::device::catch_panic(tokio::time::timeout(timeout_dur, miner.get_data()))
                    .await;
            match first {
                Ok(Ok(d)) => d,
                _ if was_cached => {
                    tracing::debug!(
                        ip = %device_info.host,
                        "cached miner stale, recreating for pairing"
                    );
                    let factory = self.factory.clone();
                    let mut fresh = crate::device::catch_panic(async move {
                        tokio::time::timeout(timeout_dur, factory.get_miner(ip)).await
                    })
                    .await
                    .map_err(|e| {
                        Status::unavailable(format!(
                            "Pairing panicked for {}: {e}",
                            device_info.host
                        ))
                    })?
                    .map_err(|_| {
                        Status::unavailable(format!("Timeout pairing {}", device_info.host))
                    })?
                    .map_err(|e| {
                        Status::unavailable(format!("Pairing error for {}: {e}", device_info.host))
                    })?
                    .ok_or_else(|| {
                        Status::not_found(format!("No miner found at {}", device_info.host))
                    })?;
                    if let Some(ref a) = auth {
                        fresh.set_auth(a.clone());
                    }
                    miner = fresh;
                    crate::device::catch_panic(tokio::time::timeout(timeout_dur, miner.get_data()))
                        .await
                        .map_err(device_err_to_status)?
                        .map_err(|_| Status::unavailable("get_data timed out during pairing"))?
                }
                Err(e) => return Err(device_err_to_status(e)),
                Ok(Err(_)) => return Err(Status::unavailable("get_data timed out during pairing")),
            }
        };

        // Verify identity: compare fresh device data against the discovery record.
        // If the IP was reassigned between discovery and pairing, this catches it
        // before we persist the wrong identity into the fleet record.
        let fresh_model = &data.device_info.model;
        let fresh_serial = data.serial_number.clone().unwrap_or_default();
        let fresh_mac = data.mac.as_ref().map(|m| m.to_string()).unwrap_or_default();

        verify_identity(
            &device_info.model,
            &device_info.serial_number,
            &device_info.mac_address,
            fresh_model,
            &fresh_serial,
            &fresh_mac,
        )
        .map_err(|reason| {
            Status::failed_precondition(format!("Identity mismatch during pairing: {reason}"))
        })?;

        // Validate credentials: LED probe (if supported) + firmware-specific check.
        // Pass the already-fetched data so the Hostname strategy (VNish) can reuse
        // it instead of making a redundant get_data() network call.
        crate::device::validate_write_access(
            miner.as_ref(),
            miner.supports_set_fault_light(),
            &data.device_info.make,
            &data.device_info.firmware,
            Some(&data),
        )
        .await
        .map_err(device_err_to_status)?;

        let firmware_version = data
            .firmware_version
            .clone()
            .unwrap_or(device_info.firmware_version.clone());

        // Derive canonical manufacturer from fresh firmware data, not stale discovery.
        // Aftermarket firmware (VNish, Braiins, LuxOS) gets reported as the firmware vendor.
        let fresh_variant = detect_variant(&data.device_info.make, &data.device_info.firmware);
        let fresh_manufacturer = firmware_manufacturer(fresh_variant)
            .unwrap_or(data.device_info.make.as_str())
            .to_string();

        tracing::info!(
            model = %fresh_model,
            manufacturer = %fresh_manufacturer,
            ip = %device_info.host,
            mac = %fresh_mac,
            "Paired device"
        );

        // Populate the response entirely from fresh device data
        Ok(Response::new(pb::PairDeviceResponse {
            device: Some(pb::DeviceInfo {
                host: device_info.host,
                port: device_info.port,
                url_scheme: device_info.url_scheme,
                serial_number: fresh_serial,
                model: fresh_model.clone(),
                manufacturer: fresh_manufacturer,
                mac_address: fresh_mac,
                firmware_version,
            }),
        }))
    }

    async fn get_default_credentials(
        &self,
        req: Request<pb::GetDefaultCredentialsRequest>,
    ) -> Result<Response<pb::GetDefaultCredentialsResponse>, Status> {
        let req = req.into_inner();
        let target_family = if req.manufacturer.is_empty() {
            None
        } else {
            make_to_family(&req.manufacturer)
        };
        let target_variant =
            target_family.map(|_| detect_variant(&req.manufacturer, &req.firmware_version));

        let mut creds = Vec::new();
        let mut seen = std::collections::HashSet::new();

        let families: Vec<(&str, &MinerFamilyConfig)> =
            match target_family.and_then(|tf| self.config.miners.get(tf).map(|c| (tf, c))) {
                Some((tf, c)) => vec![(tf, c)],
                None => self
                    .config
                    .miners
                    .iter()
                    .map(|(k, v)| (k.as_str(), v))
                    .collect(),
            };

        for (family_name, family_config) in families {
            for (variant_name, fw_config) in &family_config.firmware {
                if !fw_config.enabled {
                    continue;
                }
                if let Some(tv) = target_variant {
                    if tv != VARIANT_STOCK && variant_name.as_str() != tv {
                        continue;
                    }
                }
                for cred in default_credentials(family_name, variant_name) {
                    let key = (cred.username, cred.password);
                    if seen.insert(key) {
                        creds.push(pb::UsernamePassword {
                            username: cred.username.into(),
                            password: cred.password.into(),
                        });
                    }
                }
            }
        }

        Ok(Response::new(pb::GetDefaultCredentialsResponse {
            credentials: creds,
        }))
    }

    async fn get_capabilities_for_model(
        &self,
        req: Request<pb::GetCapabilitiesForModelRequest>,
    ) -> Result<Response<pb::GetCapabilitiesForModelResponse>, Status> {
        let model = req.into_inner().model;
        // Query live device caps. Find a probed device for this model,
        // or collect unprobed candidates to try connecting.
        let devices = self.devices.read().await;
        let mut candidates: Vec<Arc<AsicRsDevice>> = Vec::new();
        for device in devices.values() {
            let dev_model = device.model().await;
            if dev_model == model {
                if device.is_probed().await {
                    return Ok(Response::new(pb::GetCapabilitiesForModelResponse {
                        caps: Some(pb::Capabilities {
                            flags: device.get_caps().await,
                        }),
                    }));
                }
                candidates.push(device.clone());
            } else if dev_model.is_empty() {
                candidates.push(device.clone());
            }
        }
        // Release the read lock before doing I/O
        drop(devices);

        // No probed device found — try connecting candidates until one succeeds.
        for device in candidates {
            if device.ensure_connected().await.is_ok() && device.model().await == model {
                return Ok(Response::new(pb::GetCapabilitiesForModelResponse {
                    caps: Some(pb::Capabilities {
                        flags: device.get_caps().await,
                    }),
                }));
            }
        }
        // No device could be probed for this model
        Ok(Response::new(pb::GetCapabilitiesForModelResponse {
            caps: None,
        }))
    }

    // --- Device Management ---

    async fn new_device(
        &self,
        req: Request<pb::NewDeviceRequest>,
    ) -> Result<Response<pb::NewDeviceResponse>, Status> {
        let req = req.into_inner();
        let device_id = req.device_id.clone();
        let device_info = req
            .info
            .ok_or_else(|| Status::invalid_argument("Missing device info"))?;
        let auth = extract_auth(req.secret.as_ref())?;

        // Validate IP is parseable but don't connect yet
        let _: IpAddr = device_info
            .host
            .parse()
            .map_err(|_| Status::invalid_argument(format!("Invalid IP: {}", device_info.host)))?;

        // Create device disconnected. The first telemetry/control call will trigger
        // ensure_connected() which runs the full identity verification (model/serial/MAC)
        // before accepting the connection. This avoids storing an unverified miner handle.

        // Start with conservative base capabilities. Live capabilities are probed
        // on first connect via ensure_connected() -> probe_capabilities().
        let caps = static_base_capabilities();

        let cache_ttl = Duration::from_secs(self.config.plugin.telemetry_cache_ttl_seconds);
        let device = Arc::new(AsicRsDevice::new(
            device_id.clone(),
            device_info,
            caps,
            None, // created disconnected; ensure_connected() verifies identity on first use
            cache_ttl,
            self.factory.clone(),
            auth,
        ));

        self.devices.write().await.insert(device_id.clone(), device);

        tracing::debug!(device_id = %device_id, "Created device");

        Ok(Response::new(pb::NewDeviceResponse { device_id }))
    }

    async fn describe_device(
        &self,
        req: Request<pb::DescribeDeviceRequest>,
    ) -> Result<Response<pb::DescribeDeviceResponse>, Status> {
        let device_id = req.into_inner().device_id;
        let device = self.get_device(&device_id).await?;
        // Connect to probe live capabilities before returning them.
        // Errors are non-fatal: return base caps if the device is unreachable.
        if let Err(e) = device.ensure_connected().await {
            tracing::warn!(device_id = %device_id, error = %e, "describe_device: could not connect");
        }
        let caps = device.get_caps().await;
        Ok(Response::new(pb::DescribeDeviceResponse {
            device: Some(device.info.clone()),
            caps: Some(pb::Capabilities { flags: caps }),
        }))
    }

    async fn close_device(&self, req: Request<pb::DeviceRef>) -> Result<Response<()>, Status> {
        let device_id = req.into_inner().device_id;
        if let Some(device) = self.devices.write().await.remove(&device_id) {
            device.close().await;
            tracing::debug!(device_id = %device_id, "Closed device");
            Ok(Response::new(()))
        } else {
            Err(Status::not_found(format!("Device not found: {device_id}")))
        }
    }

    // --- Control ---

    async fn start_mining(&self, req: Request<pb::DeviceRef>) -> Result<Response<()>, Status> {
        let device_id = req.into_inner().device_id;
        let device = self.get_device(&device_id).await?;
        device.start_mining().await.map_err(device_err_to_status)?;
        Ok(Response::new(()))
    }

    async fn stop_mining(&self, req: Request<pb::DeviceRef>) -> Result<Response<()>, Status> {
        let device_id = req.into_inner().device_id;
        let device = self.get_device(&device_id).await?;
        device.stop_mining().await.map_err(device_err_to_status)?;
        Ok(Response::new(()))
    }

    async fn blink_led(&self, req: Request<pb::DeviceRef>) -> Result<Response<()>, Status> {
        let device_id = req.into_inner().device_id;
        let device = self.get_device(&device_id).await?;
        device.blink_led().await.map_err(device_err_to_status)?;
        Ok(Response::new(()))
    }

    async fn reboot(&self, req: Request<pb::DeviceRef>) -> Result<Response<()>, Status> {
        let device_id = req.into_inner().device_id;
        let device = self.get_device(&device_id).await?;
        device.reboot().await.map_err(device_err_to_status)?;
        Ok(Response::new(()))
    }

    // --- Configuration ---

    async fn set_cooling_mode(
        &self,
        _req: Request<pb::SetCoolingModeRequest>,
    ) -> Result<Response<()>, Status> {
        Err(Status::unimplemented("set_cooling_mode not supported"))
    }

    async fn get_cooling_mode(
        &self,
        _req: Request<pb::DeviceRef>,
    ) -> Result<Response<pb::GetCoolingModeResponse>, Status> {
        Err(Status::unimplemented("get_cooling_mode not supported"))
    }

    async fn set_power_target(
        &self,
        req: Request<pb::SetPowerTargetRequest>,
    ) -> Result<Response<()>, Status> {
        let req = req.into_inner();
        let device_id = req
            .r#ref
            .as_ref()
            .map(|r| &r.device_id)
            .ok_or_else(|| Status::invalid_argument("Missing device ref"))?;
        let device = self.get_device(device_id).await?;
        let mode = pb::PerformanceMode::try_from(req.performance_mode).map_err(|_| {
            Status::invalid_argument(format!(
                "Unknown performance_mode value: {}",
                req.performance_mode
            ))
        })?;
        if mode == pb::PerformanceMode::Unspecified {
            return Err(Status::invalid_argument(
                "performance_mode must be specified (not UNSPECIFIED)".to_string(),
            ));
        }
        device
            .set_power_target(mode)
            .await
            .map_err(device_err_to_status)?;
        Ok(Response::new(()))
    }

    async fn update_mining_pools(
        &self,
        req: Request<pb::UpdateMiningPoolsRequest>,
    ) -> Result<Response<()>, Status> {
        let req = req.into_inner();
        let device_id = req
            .r#ref
            .as_ref()
            .map(|r| &r.device_id)
            .ok_or_else(|| Status::invalid_argument("Missing device ref"))?;
        let device = self.get_device(device_id).await?;
        device
            .update_mining_pools(req.pools)
            .await
            .map_err(device_err_to_status)?;
        Ok(Response::new(()))
    }

    async fn get_mining_pools(
        &self,
        req: Request<pb::GetMiningPoolsRequest>,
    ) -> Result<Response<pb::GetMiningPoolsResponse>, Status> {
        let device_id = req
            .into_inner()
            .r#ref
            .as_ref()
            .map(|r| r.device_id.clone())
            .ok_or_else(|| Status::invalid_argument("Missing device ref"))?;
        let device = self.get_device(&device_id).await?;
        let pools = device
            .get_mining_pools()
            .await
            .map_err(device_err_to_status)?;
        Ok(Response::new(pb::GetMiningPoolsResponse { pools }))
    }

    async fn download_logs(
        &self,
        _req: Request<pb::DownloadLogsRequest>,
    ) -> Result<Response<pb::DownloadLogsResponse>, Status> {
        Err(Status::unimplemented("download_logs not supported"))
    }

    async fn update_firmware(
        &self,
        _req: Request<pb::UpdateFirmwareRequest>,
    ) -> Result<Response<()>, Status> {
        Err(Status::unimplemented("update_firmware not yet supported"))
    }

    async fn get_firmware_update_status(
        &self,
        _req: Request<pb::DeviceRef>,
    ) -> Result<Response<pb::GetFirmwareUpdateStatusResponse>, Status> {
        Err(Status::unimplemented(
            "get_firmware_update_status not supported",
        ))
    }

    async fn unpair(&self, _req: Request<pb::DeviceRef>) -> Result<Response<()>, Status> {
        // No-op
        Ok(Response::new(()))
    }

    async fn update_miner_password(
        &self,
        _req: Request<pb::UpdateMinerPasswordRequest>,
    ) -> Result<Response<()>, Status> {
        Err(Status::unimplemented("update_miner_password not supported"))
    }

    // --- Curtailment ---

    // For v1, FULL curtailment is a stop_mining/start_mining wrapper.
    // Higher levels (efficiency, partial-percent) are reserved for v4 and
    // will require vendor-specific underclock logic.
    async fn curtail(&self, req: Request<pb::CurtailRequest>) -> Result<Response<()>, Status> {
        let req = req.into_inner();
        let device_id = req
            .r#ref
            .as_ref()
            .map(|r| &r.device_id)
            .ok_or_else(|| Status::invalid_argument("Missing device ref"))?;
        let level = pb::CurtailLevel::try_from(req.level).map_err(|_| {
            Status::invalid_argument(format!("Unknown curtail level value: {}", req.level))
        })?;
        match level {
            pb::CurtailLevel::Full => {
                let device = self.get_device(device_id).await?;
                device.stop_mining().await.map_err(device_err_to_status)?;
                Ok(Response::new(()))
            }
            pb::CurtailLevel::Unspecified => Err(Status::invalid_argument(
                "curtail level must be specified",
            )),
            _ => Err(Status::unimplemented(format!(
                "curtail level {level:?} not supported by asicrs"
            ))),
        }
    }

    async fn uncurtail(&self, req: Request<pb::DeviceRef>) -> Result<Response<()>, Status> {
        let device_id = req.into_inner().device_id;
        let device = self.get_device(&device_id).await?;
        device.start_mining().await.map_err(device_err_to_status)?;
        Ok(Response::new(()))
    }

    // --- Telemetry ---

    async fn device_status(
        &self,
        req: Request<pb::DeviceRef>,
    ) -> Result<Response<pb::DeviceMetrics>, Status> {
        let device_id = req.into_inner().device_id;
        let device = self.get_device(&device_id).await?;

        let data = device.get_data().await.map_err(device_err_to_status)?;

        let metrics = device.to_device_metrics(&data);
        Ok(Response::new(metrics))
    }

    async fn get_time_series_data(
        &self,
        _req: Request<pb::GetTimeSeriesDataRequest>,
    ) -> Result<Response<pb::GetTimeSeriesDataResponse>, Status> {
        Err(Status::unimplemented("get_time_series_data not supported"))
    }

    async fn get_device_web_view_url(
        &self,
        req: Request<pb::GetDeviceWebViewUrlRequest>,
    ) -> Result<Response<pb::GetDeviceWebViewUrlResponse>, Status> {
        let device_id = req
            .into_inner()
            .r#ref
            .as_ref()
            .map(|r| r.device_id.clone())
            .ok_or_else(|| Status::invalid_argument("Missing device ref"))?;
        let device = self.get_device(&device_id).await?;
        let url = format!("{}://{}", device.info.url_scheme, device.info.host);
        Ok(Response::new(pb::GetDeviceWebViewUrlResponse { url }))
    }

    async fn batch_status(
        &self,
        _req: Request<pb::BatchStatusRequest>,
    ) -> Result<Response<pb::StatusBatchResponse>, Status> {
        Err(Status::unimplemented("batch_status not supported"))
    }

    type SubscribeStream =
        tokio_stream::wrappers::ReceiverStream<Result<pb::DeviceMetrics, Status>>;

    async fn subscribe(
        &self,
        _req: Request<pb::SubscribeRequest>,
    ) -> Result<Response<Self::SubscribeStream>, Status> {
        Err(Status::unimplemented("subscribe not supported"))
    }

    async fn get_errors(
        &self,
        req: Request<pb::DeviceRef>,
    ) -> Result<Response<pb::DeviceErrors>, Status> {
        let device_id = req.into_inner().device_id;
        let device = self.get_device(&device_id).await?;

        let data = device.get_data().await.map_err(device_err_to_status)?;

        let errors = device.to_device_errors(&data);
        Ok(Response::new(errors))
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    /// Build a minimal PluginConfig from a YAML snippet for use in tests.
    fn config_from_yaml(yaml: &str) -> PluginConfig {
        serde_yml::from_str(yaml).expect("invalid test config YAML")
    }

    fn make_request(
        manufacturer: &str,
        firmware_version: &str,
    ) -> Request<pb::GetDefaultCredentialsRequest> {
        Request::new(pb::GetDefaultCredentialsRequest {
            manufacturer: manufacturer.to_string(),
            firmware_version: firmware_version.to_string(),
        })
    }

    const TWO_FAMILY_CONFIG: &str = r#"
plugin:
  log_level: info
  discovery_timeout_seconds: 10
  telemetry_cache_ttl_seconds: 5
miners:
  whatsminer:
    stock:
      enabled: true
  antminer:
    stock:
      enabled: true
    vnish:
      enabled: true
"#;

    #[tokio::test]
    async fn test_empty_manufacturer_returns_all_families() {
        // Arrange
        let service = DriverService::new(config_from_yaml(TWO_FAMILY_CONFIG));

        // Act
        let resp = service
            .get_default_credentials(make_request("", ""))
            .await
            .unwrap();
        let creds = resp.into_inner().credentials;

        // Assert: deduplicated union of whatsminer (admin/admin, super/super) +
        // antminer stock (root/root) + antminer vnish (admin/admin deduped)
        assert_eq!(
            creds.len(),
            3,
            "should return one entry per unique credential across all families"
        );
        let usernames: Vec<&str> = creds.iter().map(|c| c.username.as_str()).collect();
        assert!(usernames.contains(&"admin"));
        assert!(usernames.contains(&"super"));
        assert!(usernames.contains(&"root"));
    }

    #[tokio::test]
    async fn test_known_family_returns_only_that_familys_credentials() {
        // Arrange
        let service = DriverService::new(config_from_yaml(TWO_FAMILY_CONFIG));

        // Act
        let resp = service
            .get_default_credentials(make_request("whatsminer", ""))
            .await
            .unwrap();
        let creds = resp.into_inner().credentials;

        // Assert: only whatsminer stock credentials
        assert_eq!(creds.len(), 2);
        assert_eq!(creds[0].username, "admin");
        assert_eq!(creds[0].password, "admin");
        assert_eq!(creds[1].username, "super");
        assert_eq!(creds[1].password, "super");
    }

    #[tokio::test]
    async fn test_vnish_firmware_filters_to_vnish_variant() {
        // Arrange
        let service = DriverService::new(config_from_yaml(TWO_FAMILY_CONFIG));

        // Act
        let resp = service
            .get_default_credentials(make_request("antminer", "VNish_1.2.0"))
            .await
            .unwrap();
        let creds = resp.into_inner().credentials;

        // Assert: only VNish variant credentials (admin/admin), not stock (root/root)
        assert_eq!(creds.len(), 1);
        assert_eq!(creds[0].username, "admin");
        assert_eq!(creds[0].password, "admin");
    }

    #[tokio::test]
    async fn test_unknown_manufacturer_falls_back_to_all_families() {
        // Arrange
        let service = DriverService::new(config_from_yaml(TWO_FAMILY_CONFIG));

        // Act — "acmeminer" is not a recognized family
        let resp = service
            .get_default_credentials(make_request("acmeminer", ""))
            .await
            .unwrap();
        let creds = resp.into_inner().credentials;

        // Assert: same as empty manufacturer — all configured families
        assert_eq!(creds.len(), 3);
    }

    #[tokio::test]
    async fn test_recognized_family_absent_from_config_falls_back_to_all() {
        // Arrange: config has only antminer, no whatsminer entry
        let config_yaml = r#"
plugin:
  log_level: info
  discovery_timeout_seconds: 10
  telemetry_cache_ttl_seconds: 5
miners:
  antminer:
    stock:
      enabled: true
"#;
        let service = DriverService::new(config_from_yaml(config_yaml));

        // Act — whatsminer is a recognized family but absent from this config
        let resp = service
            .get_default_credentials(make_request("whatsminer", ""))
            .await
            .unwrap();
        let creds = resp.into_inner().credentials;

        // Assert: falls back to all configured families (antminer stock only)
        assert_eq!(creds.len(), 1);
        assert_eq!(creds[0].username, "root");
        assert_eq!(creds[0].password, "root");
    }

    #[test]
    fn test_canonical_port_whatsminer() {
        assert_eq!(canonical_port(crate::capabilities::FAMILY_WHATSMINER), 4028);
    }

    #[test]
    fn test_canonical_port_avalonminer() {
        assert_eq!(
            canonical_port(crate::capabilities::FAMILY_AVALONMINER),
            4028
        );
    }

    #[test]
    fn test_canonical_port_auradine_is_80() {
        // Auradine is web-based (discovered on port 80), not CGMiner RPC
        assert_eq!(canonical_port(crate::capabilities::FAMILY_AURADINE), 80);
    }

    #[test]
    fn test_canonical_port_web_families_default_to_80() {
        assert_eq!(canonical_port(crate::capabilities::FAMILY_ANTMINER), 80);
        assert_eq!(canonical_port(crate::capabilities::FAMILY_BITAXE), 80);
        assert_eq!(canonical_port(crate::capabilities::FAMILY_NERDAXE), 80);
        assert_eq!(canonical_port(crate::capabilities::FAMILY_EPIC), 80);
    }
}
