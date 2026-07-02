# Proto Rig API Version Information

## Source
- Repository: miner-firmware (private)
- Commit SHA: 23df12824401403ec909dd9053e8d002f4c3d1e3
- Commit Date: 2026-06-12
- Extraction Date: 2026-06-14

The hashboard proto files live in the `external/hashboard` submodule, pinned by
the superproject commit above to:
- Submodule: external/hashboard (github.com/btc-mining/hashboard)
- Commit SHA: 54d78b5f7791ad8e235b82d49bcb17b4dee0c141

## Files Extracted

### gRPC Proto Files (from `crates/rpc/protos/`)
- mfgtool_api.proto
- mfgtool_test_commands.proto
- miner_command_api.proto
- miner_common_api.proto
- miner_data_api.proto
- miner_debug_api.proto
- miner_error_code.proto
- miner_fan_api.proto
- miner_hb_api.proto
- miner_hb_test_api.proto
- miner_psu_api.proto
- miner_psu_test_api.proto
- miner_system_api.proto
- miner_telemetry_api.proto
- miner_ui_api.proto

### Hashboard Proto Files (from `external/hashboard/lib/protobuf/protos/`)
- hashboard.proto
- hashboard_async.proto
- hashboard_cmd.proto
- hashboard_cmd_debug.proto
- hashboard_cmd_evb.proto
- hashboard_cmd_mfgtest.proto
- hashboard_log.proto

### OpenAPI Spec (from `crates/miner-api-server/docs/`)
- MDK-API.json

## Update Instructions

To update these API specifications:

1. Clone or access the miner-firmware repository
2. Checkout the desired commit/tag and sync submodules:
   `git submodule update --init external/hashboard`
3. Copy gRPC proto files from `crates/rpc/protos/` to `grpc/`
4. Copy hashboard proto files from `external/hashboard/lib/protobuf/protos/` to `grpc/`
5. Copy MDK-API.json from `crates/miner-api-server/docs/` to `openapi/`
6. Update this VERSION.md with the new commit SHA(s) and dates
7. Regenerate the dependent generated code:
   - Client: `cd client && npm run generate-api-types` (TypeScript types from the OpenAPI spec)
   - Rig telemetry stubs: in miner-firmware, run
     `tools/telemetry/otlp-bridge/generate-fleet-stubs.sh` twice —
     once with `<proto-fleet>/server/rig-otlp-bridge/internal/rigapi
     github.com/block/proto-fleet/server/rig-otlp-bridge/internal/rigapi`
     (the bridge sidecar's stubs) and once with
     `<proto-fleet>/server/fake-proto-rig/internal/rigapi
     github.com/block/proto-fleet/server/fake-proto-rig/internal/rigapi`
     (the simulator's copy); verify each module with `GOWORK=off go build ./...`.
     Also re-vendor the bridge's Go source into `server/rig-otlp-bridge/`
     if the upstream otlp-bridge changed (fleet-local modifications are
     documented in that directory's README).
8. Update the simulator REST API if the OpenAPI spec changed
   (see `server/fake-proto-rig/README.md`)
9. Run tests to verify compatibility
10. Commit all changes together

**Note**: The gRPC proto files are vendored as reference documentation of
the on-rig gRPC surface and are not compiled by Proto Fleet's own codegen —
with one exception: `miner_telemetry_api.proto` is compiled **in
miner-firmware** by `generate-fleet-stubs.sh` and the resulting Go stubs
are vendored at `server/rig-otlp-bridge/internal/rigapi/` (the rig
telemetry bridge sidecar) and `server/fake-proto-rig/internal/rigapi/`
(the simulator's copy); see each directory's README for provenance. The
OpenAPI spec (`MDK-API.json`) is the source that drives generated code (the
ProtoOS TypeScript client) and the hand-maintained fake-proto-rig simulator.

**Important**: Always update the gRPC and OpenAPI specs together to maintain
version consistency.
