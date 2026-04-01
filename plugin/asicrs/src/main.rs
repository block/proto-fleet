mod capabilities;
mod config;
mod device;
mod driver;

use tracing_subscriber::EnvFilter;

use crate::config::load_config;
use crate::driver::DriverService;

#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error>> {
    // Load configuration first, then initialize logging using the configured log level.
    let config = load_config()?;

    let env_filter = EnvFilter::try_from_default_env()
        .ok()
        .or_else(|| EnvFilter::try_new(&config.plugin.log_level).ok())
        .unwrap_or_else(|| EnvFilter::new("info"));

    tracing_subscriber::fmt()
        .with_env_filter(env_filter)
        .with_writer(std::io::stderr)
        .init();

    tracing::info!(
        log_level = %config.plugin.log_level,
        "asicrs-plugin starting"
    );

    let service = DriverService::new(config);
    proto_fleet_plugin::serve(service).await
}
