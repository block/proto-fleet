# Proto Rig API Version Information

## Source
- Repository: miner-firmware (private)
  - Commit SHA: 43528f31b52379fc12356e88e9497a53a1e97ab6
  - Commit Date: 2026-09-15
  - Extraction Date: 2026-09-22

This snapshot tracks the `proto-apps-1.9.0` source revision. The
`UnlockResponse.lock-status` field uses a `LockStatus` enum with `OPEN`,
`CLOSED`, `UNLOCKED`, `UNINITIALIZED`, and `UNKNOWN` states. Secure-boot
documentation clarifies that the hardware lock state is independent of
the effective secure override.

## Files Extracted

### OpenAPI Spec (from `crates/miner-api-server/docs/`)
- MDK-API.json

## Update Instructions

To update the API specification:

1. Clone or access the miner-firmware repository
2. Checkout the desired commit/tag
3. Copy MDK-API.json from `crates/miner-api-server/docs/` to `openapi/`
4. Update this VERSION.md with the new commit SHA and dates
5. Regenerate the dependent generated code:
   - Client: `cd client && npm run generate-api-types` (TypeScript types from the OpenAPI spec)
6. Update the simulator REST API if the OpenAPI spec changed
   (see `server/fake-proto-rig/README.md`)
7. Run tests to verify compatibility
8. Commit all changes together

**Note**: The OpenAPI spec (`MDK-API.json`) is the consumed miner API contract.
It drives the generated ProtoOS TypeScript client and serves as the reference
for the hand-maintained fake-proto-rig simulator and Proto plugin REST client.
