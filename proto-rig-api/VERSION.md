# Proto Rig API Version Information

## Source
- Repository: miner-firmware (private)
  - Commit SHA: def3bae416409d693aa9536d8bbdf2a0b902421a
  - Commit Date: 2026-10-13
  - Extraction Date: 2026-10-15

This snapshot tracks the `proto-apps-1.9.4` source revision. The
`GET /api/v1/system/logs` contract counts `lines` in journal entries, returns
an empty result for `lines=0`, and fails rather than truncating when the
default `miner_sw` source needs more than 10000 entries of history.
`UpdateStatus.error` can now be present in `available` or `current` to report
the newest failed catalog check, update request, or update attempt, and
`message` carries a one-line summary of `error` (at most 512 bytes). The
fake-proto-rig simulator is updated for the `lines=0` and `message` changes.

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
