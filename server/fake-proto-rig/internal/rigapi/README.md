# Vendored rig telemetry API stubs

Go + gRPC client stubs for the on-rig `telemetry-service` gRPC API
(`MinerTelemetryApi`), generated in the miner-firmware repository by
`tools/telemetry/otlp-bridge/generate-fleet-stubs.sh` and vendored here.

**Do not edit or regenerate these files in proto-fleet.** To update, re-run
the generator in miner-firmware against the desired commit and copy the
output, then update `proto-rig-api/VERSION.md`.

- Source proto: `crates/rpc/protos/miner_telemetry_api.proto`
- miner-firmware commit: `1faabece53e734a6f2eb5269cb768f9f4fb6168b`
- protoc: `22.5`, protoc-gen-go: `v1.36.5`, protoc-gen-go-grpc: `1.3.0`
