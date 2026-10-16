# Proto Rig API Version Information

## Sources

### MDK v1

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

### MDK v2

- Repository: Frigate (private)
  - Source path: `proto/mdk/v2/`
  - OpenAPI generation config: `proto/buf.gen.openapi.yaml`
  - Commit SHA: 5225b5fa64f8d81d95329dcd5bbd6b9c70005198
  - Extraction Date: 2026-09-04

This snapshot contains Frigate's generated MDK v2 OpenAPI document for the
protobuf-first `/api/v2/...` REST/JSON gateway. ProtoOS consumes the vendored
OpenAPI snapshot to generate a private TypeScript client under the MDK v2
adapter. The current sourcing decision is to commit the generated OpenAPI
document here, rather than vendoring Frigate protos into this repository or
publishing a separate Frigate TypeScript package.

## Files Extracted

### MDK v1 OpenAPI Spec (from `crates/miner-api-server/docs/`)

- MDK-API.json

### MDK v2 OpenAPI Spec (generated from Frigate protobuf definitions)

- MDK-v2-API.json

## Update Instructions

To update the MDK v1 API specification:

1. Clone or access the miner-firmware repository
2. Checkout the desired commit/tag
3. Copy MDK-API.json from `crates/miner-api-server/docs/` to `openapi/`
4. Update this VERSION.md with the new commit SHA and dates
5. Regenerate the dependent generated code:
   - Client: `cd client && npm run generate-api-types` (adapter-local TypeScript client from the OpenAPI spec)
6. Update the simulator REST API if the OpenAPI spec changed
   (see `server/fake-proto-rig/README.md`)
7. Run tests to verify compatibility
8. Commit all changes together

To update the MDK v2 API specification:

1. Clone or access the Frigate repository
2. Checkout the desired commit/tag
3. Generate the OpenAPI document from Frigate's protobuf definitions using its
   `proto/buf.gen.openapi.yaml` configuration
4. Copy the generated MDK v2 OpenAPI document to `openapi/MDK-v2-API.json`
5. Update this VERSION.md with the new commit SHA and dates
6. Regenerate the dependent generated code:
   - Client: `cd client && npm run generate-api-types`
7. Update the fake CT1 simulator if the consumed MDK v2 endpoint surface changed
   (see `server/fake-ct1/README.md`)
8. Run tests to verify compatibility
9. Commit all changes together

**Note**: The OpenAPI specs are the consumed miner API contracts. They drive the
generated ProtoOS TypeScript clients and serve as references for the
hand-maintained simulators and plugin REST clients.
