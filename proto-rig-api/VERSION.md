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
protobuf-first `/api/v2/...` REST/JSON gateway. ProtoOS generates a private
TypeScript client from it under the MDK v2 adapter, and its request and
response types are the shapes the adapter works with on both MDK v2
transports.

The MDK v2 protos are vendored separately in `sdk/mdk/proto/mdk/v2` and pinned
in `sdk/mdk/README.md`. They generate the protobuf-es bindings that the ProtoOS
Connect client encodes and decodes with, so this snapshot and those protos must
describe compatible contracts. Refresh both from the same Frigate commit.

The two pins currently differ: this snapshot is at `5225b5fa`, the protos at
`4f3fe6ee8a99`. A comparison on 2026-10-25 found every property and enum value
of this snapshot's 363 `mdk.v2` definitions in the `4f3fe6ee8a99` protos. The
protos only add fields and enum values, such as
`HASHBOARD_ERROR_CAUSE_STACK_DROPOUT`, so Connect responses still carry every
field these types declare. Re-vendor this snapshot at the proto pin the next
time Frigate's OpenAPI document is generated.

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
5. Copy the same commit's `proto/mdk/v2` sources to `sdk/mdk/proto/mdk/v2` and
   update the pin in `sdk/mdk/README.md`
6. Update this VERSION.md with the new commit SHA and dates
7. Regenerate the dependent generated code:
   - Client: `cd client && npm run generate-api-types`
   - MDK v2 bindings: `just gen-mdk-protos` from the repository root, after
     installing client dependencies (it runs the client's `protoc-gen-es`)
8. Update the fake CT1 simulator if the consumed MDK v2 surface changed
   (see `server/fake-ct1/README.md`)
9. Run tests to verify compatibility
10. Commit all changes together

**Note**: The OpenAPI specs are the consumed miner API contracts. They drive the
generated ProtoOS TypeScript clients and serve as references for the
hand-maintained simulators and plugin REST clients.
