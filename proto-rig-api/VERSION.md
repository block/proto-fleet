# Proto Rig API Version Information

## Source
- Repository: miner-firmware (private)
  - Commit SHA: 89ef557cbec85ebe7aced9fc47d0bdfde744e913
  - Commit Date: 2026-08-27
  - Extraction Date: 2026-08-31

This snapshot tracks the `proto-apps-1.8.5` source revision. The
`POST /api/v1/system/update/check` contract drops its unreachable `400`,
`422`, and `500` responses (the endpoint only returns `200`, `409`, or
`401`), and JWT examples are replaced with a neutral placeholder string.

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
