# Proto Rig API Version Information

## Source
- Repository: miner-firmware (private)
- Commit SHA: b093c1e0652164373c963046b152ad3ee29c6be6
- Commit Date: 2026-07-17
- Extraction Date: 2026-07-20

The hashboard proto files live in the `external/hashboard` submodule, pinned by
the superproject commit above to:
- Submodule: external/hashboard (github.com/btc-mining/hashboard)
- Commit SHA: 068732a3eac94ce3cf98ea65bcf2d2dc50075837

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
8. Update the simulator REST API if the OpenAPI spec changed
   (see `server/fake-proto-rig/README.md`)
9. Run tests to verify compatibility
10. Commit all changes together

**Note**: The gRPC proto files are vendored as reference only — they document
the on-rig gRPC surface and are not inputs to Proto Fleet code generation. The
OpenAPI spec (`MDK-API.json`) is the source that drives generated code (the
ProtoOS TypeScript client) and the hand-maintained fake-proto-rig simulator.

**Important**: Always update the gRPC and OpenAPI specs together to maintain
version consistency.
