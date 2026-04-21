use std::path::PathBuf;

// This build script references the canonical driver.proto from the monorepo
// (server/sdk/v1/pb/) rather than vendoring a copy. This avoids proto drift
// between the Go, Python, and Rust SDKs. The trade-off is that the crate can
// only be built from within this repository checkout.
fn main() -> Result<(), Box<dyn std::error::Error>> {
    let manifest_dir = PathBuf::from(std::env::var("CARGO_MANIFEST_DIR")?);
    let proto_dir = manifest_dir
        .join("../../../server/sdk/v1/pb")
        .canonicalize()?;

    let proto_file = proto_dir.join("driver.proto");
    println!("cargo:rerun-if-changed={}", proto_file.display());

    tonic_build::configure()
        .build_server(true)
        .build_client(false)
        .compile_protos(&[&proto_file], &[&proto_dir])?;

    Ok(())
}
