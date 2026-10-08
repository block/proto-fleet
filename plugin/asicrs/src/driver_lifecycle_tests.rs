use super::*;

fn service() -> Arc<DriverService> {
    Arc::new(DriverService::new(
        serde_yaml_ng::from_str("plugin: {}\nminers: {}\n").unwrap(),
    ))
}

fn scoped_request(id: &str) -> pb::NewDeviceRequest {
    pb::NewDeviceRequest {
        device_id: id.into(),
        info: Some(pb::DeviceInfo {
            host: "127.0.0.1".into(),
            port: 4028,
            ..Default::default()
        }),
        secret: Some(pb::SecretBundle {
            version: "fixture-version".into(),
            kind: Some(pb::secret_bundle::Kind::UserPass(pb::UsernamePassword {
                username: "fixture-user".into(),
                password: "fixture-password".into(),
            })),
            ttl: Some(prost_types::Duration {
                seconds: 30,
                nanos: 0,
            }),
        }),
        reuse_scope_key: "trusted-fixture-binding".into(),
    }
}

async fn resolve(service: &DriverService, request: pb::NewDeviceRequest) -> Arc<AsicRsDevice> {
    let id = request.device_id.clone();
    let scope = request.reuse_scope_key.clone();
    let response = service
        .new_device(Request::new(request))
        .await
        .unwrap()
        .into_inner();
    assert_eq!(response.device_id, id);
    assert_eq!(response.reuse_scope_key, scope);
    service.get_device(&id).await.unwrap()
}

#[tokio::test]
async fn scoped_lifecycle_advertises_and_reuses_exact_backend() {
    let service = service();
    let caps = service
        .describe_driver(Request::new(()))
        .await
        .unwrap()
        .into_inner()
        .caps
        .unwrap();
    assert_eq!(caps.flags.get(CAP_SCOPED_DEVICE_REUSE), Some(&true));
    let request = scoped_request("device");
    let first = resolve(&service, request.clone()).await;
    let second = resolve(&service, request).await;
    assert!(Arc::ptr_eq(&first, &second));
    assert!(service.devices.read().await["device"]
        .request_digest
        .is_some());
}

#[tokio::test]
async fn scoped_lifecycle_complete_configuration_changes_replace() {
    let service = service();
    let request = scoped_request("device");
    let original = resolve(&service, request.clone()).await;
    let mut variants = Vec::new();
    let mut changed = request.clone();
    changed.reuse_scope_key.push_str("-changed");
    variants.push(changed);
    for field in 0..10 {
        let mut changed = request.clone();
        let info = changed.info.as_mut().unwrap();
        match field {
            0 => info.host = "127.0.0.2".into(),
            1 => info.port += 1,
            2 => info.url_scheme = "https".into(),
            3 => info.serial_number = "different-serial".into(),
            4 => info.model = "different-model".into(),
            5 => info.manufacturer = "different-manufacturer".into(),
            6 => info.mac_address = "01:02:03:04:05:06".into(),
            7 => info.firmware_version = "different-firmware".into(),
            8 => info.default_password_active = Some(true),
            _ => info.device_class += 1,
        }
        variants.push(changed);
    }
    let mut changed = request.clone();
    changed
        .secret
        .as_mut()
        .unwrap()
        .version
        .push_str("-changed");
    variants.push(changed);
    let mut changed = request.clone();
    changed
        .secret
        .as_mut()
        .unwrap()
        .ttl
        .as_mut()
        .unwrap()
        .seconds += 1;
    variants.push(changed);
    let mut changed = request.clone();
    changed.secret.as_mut().unwrap().ttl.as_mut().unwrap().nanos += 1;
    variants.push(changed);
    let mut changed = request.clone();
    if let Some(pb::secret_bundle::Kind::UserPass(user_pass)) =
        changed.secret.as_mut().unwrap().kind.as_mut()
    {
        user_pass.password.push_str("-changed");
    }
    variants.push(changed);
    let mut changed = request.clone();
    if let Some(pb::secret_bundle::Kind::UserPass(user_pass)) =
        changed.secret.as_mut().unwrap().kind.as_mut()
    {
        user_pass.username.push_str("-changed");
    }
    variants.push(changed);
    let mut changed = request.clone();
    changed.secret.as_mut().unwrap().kind = None;
    variants.push(changed);
    let mut changed = request.clone();
    changed.secret.as_mut().unwrap().ttl = None;
    variants.push(changed);
    let mut changed = request.clone();
    changed.secret = None;
    variants.push(changed);
    for changed in variants {
        let baseline = resolve(&service, request.clone()).await;
        let replacement = resolve(&service, changed).await;
        assert!(
            !Arc::ptr_eq(&baseline, &replacement),
            "changed request must replace without exposing credential inputs"
        );
    }
    assert!(!Arc::ptr_eq(&original, &resolve(&service, request).await));
}

#[tokio::test]
async fn scoped_lifecycle_default_replaces_and_close_removes_evidence() {
    let service = service();
    let request = scoped_request("device");
    let first = resolve(&service, request.clone()).await;
    let mut default_request = request.clone();
    default_request.reuse_scope_key.clear();
    let default_first = resolve(&service, default_request.clone()).await;
    let default_second = resolve(&service, default_request).await;
    assert!(!Arc::ptr_eq(&first, &default_first));
    assert!(!Arc::ptr_eq(&default_first, &default_second));
    assert!(service.devices.read().await["device"]
        .request_digest
        .is_none());
    let scoped = resolve(&service, request.clone()).await;
    assert!(!Arc::ptr_eq(&scoped, &default_second));
    service
        .close_device(Request::new(pb::DeviceRef {
            device_id: "device".into(),
        }))
        .await
        .unwrap();
    assert!(!service.devices.read().await.contains_key("device"));
    let after_close = resolve(&service, request).await;
    assert!(!Arc::ptr_eq(&scoped, &after_close));
}

#[tokio::test]
async fn scoped_lifecycle_concurrent_exact_calls_keep_one_arc() {
    let service = service();
    let request = scoped_request("device");
    let mut tasks = Vec::new();
    for _ in 0..32 {
        let service = service.clone();
        let request = request.clone();
        tasks.push(tokio::spawn(
            async move { resolve(&service, request).await },
        ));
    }
    let first = tasks.remove(0).await.unwrap();
    for task in tasks {
        assert!(Arc::ptr_eq(&first, &task.await.unwrap()));
    }
    assert_eq!(service.devices.read().await.len(), 1);
}

#[tokio::test]
async fn scoped_lifecycle_cancelled_waiter_cannot_replace() {
    let service = service();
    let request = scoped_request("device");
    let original = resolve(&service, request.clone()).await;
    let guard = service.device_stripes[device_stripe("device")].lock().await;
    let worker = service.clone();
    let mut changed = request.clone();
    changed.reuse_scope_key.push_str("-changed");
    let waiter = tokio::spawn(async move { resolve(&worker, changed).await });
    tokio::task::yield_now().await;
    waiter.abort();
    assert!(waiter.await.err().unwrap().is_cancelled());
    drop(guard);
    assert!(Arc::ptr_eq(&original, &resolve(&service, request).await));
}

#[tokio::test]
async fn scoped_lifecycle_close_and_default_wait_on_same_id_only() {
    let service = service();
    let request = scoped_request("device");
    let original = resolve(&service, request.clone()).await;
    let guard = service.device_stripes[device_stripe("device")].lock().await;
    let worker = service.clone();
    let close = tokio::spawn(async move {
        worker
            .close_device(Request::new(pb::DeviceRef {
                device_id: "device".into(),
            }))
            .await
    });
    tokio::task::yield_now().await;
    assert!(!close.is_finished());
    let other_id = (0..128)
        .map(|i| format!("other-{i}"))
        .find(|id| device_stripe(id) != device_stripe("device"))
        .unwrap();
    tokio::time::timeout(
        Duration::from_secs(2),
        resolve(&service, scoped_request(&other_id)),
    )
    .await
    .unwrap();
    assert!(Arc::ptr_eq(
        &original,
        &service.get_device("device").await.unwrap()
    ));
    drop(guard);
    close.await.unwrap().unwrap();
    let after_close = resolve(&service, request.clone()).await;
    assert!(!Arc::ptr_eq(&original, &after_close));
    let guard = service.device_stripes[device_stripe("device")].lock().await;
    let worker = service.clone();
    let mut default_request = request.clone();
    default_request.reuse_scope_key.clear();
    let default = tokio::spawn(async move { resolve(&worker, default_request).await });
    tokio::task::yield_now().await;
    assert!(!default.is_finished());
    drop(guard);
    let default = default.await.unwrap();
    let after_default = resolve(&service, request).await;
    assert!(!Arc::ptr_eq(&after_close, &default));
    assert!(!Arc::ptr_eq(&default, &after_default));
}

use asic_rs_core::config::collector::{ConfigCollector, ConfigField, ConfigLocation};
use asic_rs_core::data::collector::{DataCollector, DataField, DataLocation};
use asic_rs_core::data::command::MinerCommand;
use asic_rs_core::data::device::{DeviceInfo as MinerDeviceInfo, HashAlgorithm, MinerHardware};
use asic_rs_core::traits::miner::*;
use std::sync::atomic::{AtomicBool, AtomicUsize, Ordering};

#[derive(Debug, Default)]
struct MiningControl {
    mining: AtomicBool,
    pauses: AtomicUsize,
    resumes: AtomicUsize,
    block_pause: AtomicBool,
    pause_started: tokio::sync::Notify,
    release_pause: tokio::sync::Notify,
}

#[derive(Debug)]
struct LifecycleMiner(Arc<MiningControl>);

impl GetIP for LifecycleMiner {
    fn get_ip(&self) -> IpAddr {
        "127.0.0.1".parse().unwrap()
    }
}
impl GetDeviceInfo for LifecycleMiner {
    fn get_device_info(&self) -> MinerDeviceInfo {
        MinerDeviceInfo {
            make: "Bitmain".into(),
            model: "S21".into(),
            hardware: MinerHardware::default(),
            firmware: "stock".into(),
            algo: HashAlgorithm::SHA256,
        }
    }
}
impl GetDataLocations for LifecycleMiner {
    fn get_locations(&self, _field: DataField) -> Vec<DataLocation> {
        Vec::new()
    }
}
impl CollectData for LifecycleMiner {
    fn get_collector(&self) -> DataCollector<'_> {
        DataCollector::new(self)
    }
}
impl GetConfigsLocations for LifecycleMiner {
    fn get_configs_locations(&self, _field: ConfigField) -> Vec<ConfigLocation> {
        Vec::new()
    }
}
impl CollectConfigs for LifecycleMiner {
    fn get_config_collector(&self) -> ConfigCollector<'_> {
        ConfigCollector::new(self)
    }
}
#[tonic::async_trait]
impl APIClient for LifecycleMiner {
    async fn get_api_result(&self, _command: &MinerCommand) -> anyhow::Result<serde_json::Value> {
        anyhow::bail!("fixture has no network commands")
    }
}
#[tonic::async_trait]
impl GetIsMining for LifecycleMiner {
    fn parse_is_mining(&self, _data: &HashMap<DataField, serde_json::Value>) -> bool {
        self.0.mining.load(Ordering::SeqCst)
    }
}
macro_rules! default_data_traits {
    ($($trait:ident),*) => { $(impl $trait for LifecycleMiner {})* };
}
default_data_traits!(
    GetMAC,
    GetSerialNumber,
    GetHostname,
    GetApiVersion,
    GetFirmwareVersion,
    GetControlBoardVersion,
    GetHashboards,
    GetHashrate,
    GetExpectedHashrate,
    GetFans,
    GetPsuFans,
    GetFluidTemperature,
    GetWattage,
    GetTuningTarget,
    GetLightFlashing,
    GetMessages,
    GetUptime,
    GetPools,
    HasAuth,
    HasDefaultAuth,
    UpgradeFirmware,
    SupportsTuningConfig,
    SupportsFanConfig
);
macro_rules! unsupported_control_traits {
    ($($trait:ident => $method:ident),*) => { $(impl $trait for LifecycleMiner { fn $method(&self) -> bool { false } })* };
}
unsupported_control_traits!(SetFaultLight => supports_set_fault_light, SetPowerLimit => supports_set_power_limit, Restart => supports_restart, ChangePassword => supports_change_password, FactoryReset => supports_factory_reset, ReadLogs => supports_read_logs, SupportsPoolsConfig => supports_pools_config, SupportsScalingConfig => supports_scaling_config);
#[tonic::async_trait]
impl Pause for LifecycleMiner {
    async fn pause(&self, _at_time: Option<Duration>) -> anyhow::Result<bool> {
        self.0.pauses.fetch_add(1, Ordering::SeqCst);
        if self.0.block_pause.load(Ordering::SeqCst) {
            self.0.pause_started.notify_one();
            self.0.release_pause.notified().await;
        }
        self.0.mining.store(false, Ordering::SeqCst);
        Ok(true)
    }
    fn supports_pause(&self) -> bool {
        true
    }
}
#[tonic::async_trait]
impl Resume for LifecycleMiner {
    async fn resume(&self, _at_time: Option<Duration>) -> anyhow::Result<bool> {
        self.0.resumes.fetch_add(1, Ordering::SeqCst);
        self.0.mining.store(true, Ordering::SeqCst);
        Ok(true)
    }
    fn supports_resume(&self) -> bool {
        true
    }
}

#[tokio::test]
async fn scoped_lifecycle_actual_full_restoration_preserves_stopped_and_running_state() {
    for initially_mining in [false, true] {
        let service = service();
        let request = scoped_request("device");
        let created = resolve(&service, request.clone()).await;
        let control = Arc::new(MiningControl::default());
        control.mining.store(initially_mining, Ordering::SeqCst);
        // Supply an already connected miner in the actual AsicRsDevice fixture.
        // No snapshot is injected: actual FULL curtailment must observe/record it.
        let connected = Arc::new(AsicRsDevice::new(
            created.id.clone(),
            created.info.clone(),
            static_base_capabilities(),
            Some(Box::new(LifecycleMiner(control.clone()))),
            Duration::from_secs(5),
            service.factory.clone(),
            None,
        ));
        service
            .devices
            .write()
            .await
            .get_mut("device")
            .unwrap()
            .device = connected.clone();
        service
            .curtail(Request::new(pb::CurtailRequest {
                r#ref: Some(pb::DeviceRef {
                    device_id: "device".into(),
                }),
                level: pb::CurtailLevel::Full as i32,
            }))
            .await
            .unwrap();
        assert_eq!(control.pauses.load(Ordering::SeqCst), 1);
        assert!(!control.mining.load(Ordering::SeqCst));
        let retained = resolve(&service, request).await;
        assert!(Arc::ptr_eq(&connected, &retained));
        service
            .uncurtail(Request::new(pb::UncurtailRequest {
                r#ref: Some(pb::DeviceRef {
                    device_id: "device".into(),
                }),
            }))
            .await
            .unwrap();
        assert_eq!(
            control.resumes.load(Ordering::SeqCst),
            usize::from(initially_mining)
        );
        assert_eq!(control.mining.load(Ordering::SeqCst), initially_mining);
    }
}

#[tokio::test]
async fn scoped_lifecycle_cancelled_close_keeps_id_serialized_through_cleanup() {
    let service = service();
    let request = scoped_request("device");
    let created = resolve(&service, request.clone()).await;
    let control = Arc::new(MiningControl::default());
    control.block_pause.store(true, Ordering::SeqCst);
    let connected = Arc::new(AsicRsDevice::new(
        created.id.clone(),
        created.info.clone(),
        static_base_capabilities(),
        Some(Box::new(LifecycleMiner(control.clone()))),
        Duration::from_secs(5),
        service.factory.clone(),
        None,
    ));
    service
        .devices
        .write()
        .await
        .get_mut("device")
        .unwrap()
        .device = connected.clone();
    let worker = service.clone();
    let curtail = tokio::spawn(async move {
        worker
            .curtail(Request::new(pb::CurtailRequest {
                r#ref: Some(pb::DeviceRef {
                    device_id: "device".into(),
                }),
                level: pb::CurtailLevel::Full as i32,
            }))
            .await
    });
    tokio::time::timeout(Duration::from_secs(2), control.pause_started.notified())
        .await
        .unwrap();
    let worker = service.clone();
    let close = tokio::spawn(async move {
        worker
            .close_device(Request::new(pb::DeviceRef {
                device_id: "device".into(),
            }))
            .await
    });
    tokio::time::timeout(Duration::from_secs(2), async {
        while service.devices.read().await.contains_key("device") {
            tokio::task::yield_now().await;
        }
    })
    .await
    .unwrap();
    close.abort();
    assert!(close.await.err().unwrap().is_cancelled());
    let worker = service.clone();
    let next = tokio::spawn(async move { resolve(&worker, request).await });
    tokio::task::yield_now().await;
    assert!(
        !next.is_finished(),
        "canceled Close waiter must not release an unfinished backend cleanup"
    );
    let other_id = (0..128)
        .map(|i| format!("other-{i}"))
        .find(|id| device_stripe(id) != device_stripe("device"))
        .unwrap();
    tokio::time::timeout(
        Duration::from_secs(2),
        resolve(&service, scoped_request(&other_id)),
    )
    .await
    .unwrap();
    control.release_pause.notify_one();
    curtail.await.unwrap().unwrap();
    let replacement = tokio::time::timeout(Duration::from_secs(2), next)
        .await
        .unwrap()
        .unwrap();
    assert!(!Arc::ptr_eq(&connected, &replacement));
}
