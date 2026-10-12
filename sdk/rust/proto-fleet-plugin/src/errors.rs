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

// Wire-compatible google.rpc status details. Keeping the SDK-owned rejection
// structured lets Fleet distinguish a pre-I/O gate from device-reported errors.
#[derive(Clone, PartialEq, prost::Message)]
struct RpcStatus {
    #[prost(int32, tag = "1")]
    code: i32,
    #[prost(string, tag = "2")]
    message: String,
    #[prost(message, repeated, tag = "3")]
    details: Vec<prost_types::Any>,
}

#[derive(Clone, PartialEq, prost::Message)]
struct ErrorInfo {
    #[prost(string, tag = "1")]
    reason: String,
    #[prost(string, tag = "2")]
    domain: String,
}

/// Reject a stale registry reference before selecting a backend for I/O.
pub fn backend_generation_mismatch() -> Status {
    use prost::Message;
    let message = "Device backend generation changed";
    let error = ErrorInfo {
        reason: "SDK_BACKEND_GENERATION_MISMATCH".into(),
        domain: "protofleet.sdk".into(),
    };
    let details = RpcStatus {
        code: Code::FailedPrecondition as i32,
        message: message.into(),
        details: vec![prost_types::Any {
            type_url: "type.googleapis.com/google.rpc.ErrorInfo".into(),
            value: error.encode_to_vec(),
        }],
    };
    Status::with_details(
        Code::FailedPrecondition,
        message,
        details.encode_to_vec().into(),
    )
}

#[cfg(test)]
mod generation_tests {
    use super::*;
    use prost::Message;

    #[test]
    fn backend_generation_rejection_has_canonical_pre_io_details() {
        let error = backend_generation_mismatch();
        assert_eq!(error.code(), Code::FailedPrecondition);
        let status = RpcStatus::decode(error.details()).unwrap();
        assert_eq!(status.code, Code::FailedPrecondition as i32);
        assert_eq!(status.details.len(), 1);
        assert_eq!(
            status.details[0].type_url,
            "type.googleapis.com/google.rpc.ErrorInfo"
        );
        let info = ErrorInfo::decode(status.details[0].value.as_slice()).unwrap();
        assert_eq!(info.reason, "SDK_BACKEND_GENERATION_MISMATCH");
        assert_eq!(info.domain, "protofleet.sdk");
    }
}
