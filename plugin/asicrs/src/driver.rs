use std::sync::Arc;

use tonic::{Request, Response, Status};

use pb::driver_server::Driver;
use proto_fleet_plugin::pb;

use crate::capabilities::{default_credentials, static_base_capabilities};
use crate::config::PluginConfig;

const DRIVER_NAME: &str = "asicrs";
const API_VERSION: &str = "v1";

/// Ports that asic-rs probes during detection.
const DISCOVERY_PORTS: &[u16] = &[80, 443, 4028];

pub struct DriverService {
    config: Arc<PluginConfig>,
}

impl DriverService {
    pub fn new(config: PluginConfig) -> Self {
        Self {
            config: Arc::new(config),
        }
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
                flags: static_base_capabilities(),
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

    async fn get_default_credentials(
        &self,
        _req: Request<()>,
    ) -> Result<Response<pb::GetDefaultCredentialsResponse>, Status> {
        let mut creds = Vec::new();
        let mut seen = std::collections::HashSet::new();

        for (family_name, family_config) in &self.config.miners {
            for (variant_name, fw_config) in &family_config.firmware {
                if !fw_config.enabled {
                    continue;
                }
                for cred in default_credentials(family_name, variant_name) {
                    let key = (cred.username.to_string(), cred.password.to_string());
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

    // --- Stubs (implemented in the business logic PR) ---

    async fn discover_device(
        &self,
        _req: Request<pb::DiscoverDeviceRequest>,
    ) -> Result<Response<pb::DiscoverDeviceResponse>, Status> {
        Err(Status::unimplemented("discover_device not yet implemented"))
    }

    async fn pair_device(
        &self,
        _req: Request<pb::PairDeviceRequest>,
    ) -> Result<Response<pb::PairDeviceResponse>, Status> {
        Err(Status::unimplemented("pair_device not yet implemented"))
    }

    async fn get_capabilities_for_model(
        &self,
        _req: Request<pb::GetCapabilitiesForModelRequest>,
    ) -> Result<Response<pb::GetCapabilitiesForModelResponse>, Status> {
        Ok(Response::new(pb::GetCapabilitiesForModelResponse {
            caps: Some(pb::Capabilities {
                flags: static_base_capabilities(),
            }),
        }))
    }

    async fn new_device(
        &self,
        _req: Request<pb::NewDeviceRequest>,
    ) -> Result<Response<pb::NewDeviceResponse>, Status> {
        Err(Status::unimplemented("new_device not yet implemented"))
    }

    async fn describe_device(
        &self,
        _req: Request<pb::DescribeDeviceRequest>,
    ) -> Result<Response<pb::DescribeDeviceResponse>, Status> {
        Err(Status::unimplemented("describe_device not yet implemented"))
    }

    async fn close_device(&self, _req: Request<pb::DeviceRef>) -> Result<Response<()>, Status> {
        Ok(Response::new(()))
    }

    async fn start_mining(&self, _req: Request<pb::DeviceRef>) -> Result<Response<()>, Status> {
        Err(Status::unimplemented("start_mining not yet implemented"))
    }

    async fn stop_mining(&self, _req: Request<pb::DeviceRef>) -> Result<Response<()>, Status> {
        Err(Status::unimplemented("stop_mining not yet implemented"))
    }

    async fn blink_led(&self, _req: Request<pb::DeviceRef>) -> Result<Response<()>, Status> {
        Err(Status::unimplemented("blink_led not yet implemented"))
    }

    async fn reboot(&self, _req: Request<pb::DeviceRef>) -> Result<Response<()>, Status> {
        Err(Status::unimplemented("reboot not yet implemented"))
    }

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
        _req: Request<pb::SetPowerTargetRequest>,
    ) -> Result<Response<()>, Status> {
        Err(Status::unimplemented(
            "set_power_target not yet implemented",
        ))
    }

    async fn update_mining_pools(
        &self,
        _req: Request<pb::UpdateMiningPoolsRequest>,
    ) -> Result<Response<()>, Status> {
        Err(Status::unimplemented(
            "update_mining_pools not yet implemented",
        ))
    }

    async fn get_mining_pools(
        &self,
        _req: Request<pb::GetMiningPoolsRequest>,
    ) -> Result<Response<pb::GetMiningPoolsResponse>, Status> {
        Err(Status::unimplemented(
            "get_mining_pools not yet implemented",
        ))
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
        Err(Status::unimplemented("update_firmware not supported"))
    }

    async fn unpair(&self, _req: Request<pb::DeviceRef>) -> Result<Response<()>, Status> {
        Ok(Response::new(()))
    }

    async fn update_miner_password(
        &self,
        _req: Request<pb::UpdateMinerPasswordRequest>,
    ) -> Result<Response<()>, Status> {
        Err(Status::unimplemented("update_miner_password not supported"))
    }

    async fn device_status(
        &self,
        _req: Request<pb::DeviceRef>,
    ) -> Result<Response<pb::DeviceMetrics>, Status> {
        Err(Status::unimplemented("device_status not yet implemented"))
    }

    async fn get_time_series_data(
        &self,
        _req: Request<pb::GetTimeSeriesDataRequest>,
    ) -> Result<Response<pb::GetTimeSeriesDataResponse>, Status> {
        Err(Status::unimplemented("get_time_series_data not supported"))
    }

    async fn get_device_web_view_url(
        &self,
        _req: Request<pb::GetDeviceWebViewUrlRequest>,
    ) -> Result<Response<pb::GetDeviceWebViewUrlResponse>, Status> {
        Err(Status::unimplemented(
            "get_device_web_view_url not yet implemented",
        ))
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
        _req: Request<pb::DeviceRef>,
    ) -> Result<Response<pb::DeviceErrors>, Status> {
        Err(Status::unimplemented("get_errors not yet implemented"))
    }
}
