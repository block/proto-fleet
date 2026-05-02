# Proto Rig API Version Information

## Source
- Repository: miner-firmware (private)
- Commit SHA: c2b42e5173a90234090e405fedead62b52673478
- Commit Date: 2026-05-01
- Extraction Date: 2026-05-01

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
- miner_psu_api.proto
- miner_psu_test_api.proto
- miner_system_api.proto
- miner_ui_api.proto

### Hashboard Proto Files (from `crates/mcdd/hashboard/lib/protobuf/protos/`)
- hashboard.proto
- hashboard_async.proto
- hashboard_cmd.proto
- hashboard_cmd_debug.proto
- hashboard_cmd_mfgtest.proto
- hashboard_log.proto

### OpenAPI Spec (from `crates/miner-api-server/docs/`)
- MDK-API.json

## Update Instructions

To update these API specifications:

1. Clone or access the miner-firmware repository
2. Checkout the desired commit/tag
3. Copy proto files from `crates/rpc/protos/` to `grpc/`
4. Copy MDK-API.json from `crates/miner-api-server/docs/` to `openapi/`
5. Update this VERSION.md with the new commit SHA and date
6. Regenerate code:
   - Server: `cd server && just gen`
   - Client: `cd client && npm run generate-api-types`
7. Run tests to verify compatibility
8. Commit all changes together

**Important**: Always update both gRPC and OpenAPI specs together to maintain version consistency.
