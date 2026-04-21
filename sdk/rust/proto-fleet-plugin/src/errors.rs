use tonic::{Code, Status};

/// SDK error types for Fleet miner driver plugins.
///
/// Each variant maps to a specific gRPC status code via the `From<PluginError> for Status`
/// implementation, allowing the `?` operator to automatically convert plugin errors into
/// appropriate gRPC responses.
#[derive(Debug, thiserror::Error)]
pub enum PluginError {
    #[error("device not found: {device_id}")]
    DeviceNotFound { device_id: String },

    #[error("device unavailable: {device_id}")]
    DeviceUnavailable {
        device_id: String,
        #[source]
        source: Option<Box<dyn std::error::Error + Send + Sync>>,
    },

    #[error("authentication failed for device: {device_id}")]
    AuthenticationFailed { device_id: String },

    #[error("invalid config: {message}")]
    InvalidConfig { message: String },

    #[error("unsupported capability: {capability}")]
    UnsupportedCapability { capability: String },

    #[error("driver shutting down: {reason}")]
    DriverShutdown { reason: String },

    #[error("network error: {message}")]
    Network {
        message: String,
        #[source]
        source: Option<Box<dyn std::error::Error + Send + Sync>>,
    },
}

impl From<PluginError> for Status {
    fn from(err: PluginError) -> Self {
        let code = match &err {
            PluginError::DeviceNotFound { .. } => Code::NotFound,
            PluginError::DeviceUnavailable { .. } => Code::Unavailable,
            PluginError::AuthenticationFailed { .. } => Code::Unauthenticated,
            PluginError::InvalidConfig { .. } => Code::InvalidArgument,
            PluginError::UnsupportedCapability { .. } => Code::Unimplemented,
            PluginError::DriverShutdown { .. } => Code::Aborted,
            PluginError::Network { .. } => Code::Unavailable,
        };
        Status::new(code, err.to_string())
    }
}
