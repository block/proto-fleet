pub mod capabilities;
pub mod errors;
pub mod pb;

#[cfg(feature = "http-client")]
pub mod http_client;

const MAGIC_COOKIE_KEY: &str = "MINER_DRIVER_PLUGIN";
const MAGIC_COOKIE_VALUE: &str = "fleet-miner-driver";
const CORE_PROTOCOL_VERSION: u32 = 1;
const APP_PROTOCOL_VERSION: u32 = 1;

/// Validates the go-plugin magic cookie environment variable.
///
/// Exits with code 1 if the binary is not being run as a managed plugin.
pub fn check_magic_cookie() {
    match std::env::var(MAGIC_COOKIE_KEY) {
        Ok(v) if v == MAGIC_COOKIE_VALUE => {}
        _ => {
            eprintln!("This binary is a plugin. Do not run it directly.");
            std::process::exit(1);
        }
    }
}

/// Binds to a free port on localhost, emits the go-plugin handshake line to stdout,
/// and serves the given `Driver` implementation as a gRPC service.
///
/// # Example
///
/// ```ignore
/// use proto_fleet_plugin::serve;
///
/// #[tokio::main]
/// async fn main() -> Result<(), Box<dyn std::error::Error>> {
///     let driver = MyDriver::new();
///     serve(driver).await
/// }
/// ```
pub async fn serve<S>(svc: S) -> Result<(), Box<dyn std::error::Error>>
where
    S: pb::Driver,
{
    check_magic_cookie();

    let listener = tokio::net::TcpListener::bind("127.0.0.1:0").await?;
    let addr = listener.local_addr()?;

    use std::io::Write;
    println!("{CORE_PROTOCOL_VERSION}|{APP_PROTOCOL_VERSION}|tcp|{addr}|grpc");
    std::io::stdout().flush()?;

    tonic::transport::Server::builder()
        .add_service(pb::DriverServer::new(svc))
        .serve_with_incoming(tokio_stream::wrappers::TcpListenerStream::new(listener))
        .await?;

    Ok(())
}
