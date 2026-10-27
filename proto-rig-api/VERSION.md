# Proto Rig API Version Information

## Sources

### MDK v1

- Repository: miner-firmware (private)
  - Commit SHA: 10bc4cdf7e3ab3cc8bc9a3558baf10f66d12cbb8
  - Commit Date: 2026-10-19
  - Extraction Date: 2026-10-26
- Published mirror: btc-mining/proto-api `mdk/MDK-API.json` at
  c6fdc5a0fae634ea0853b885f65ae32e3e2f0592, identical to the source file

This snapshot is 12 commits after the `proto-apps-1.9.4` source revision
(`def3bae4`). The `GET /api/v1/system/logs` contract counts `lines` in journal
entries and returns an empty result for `lines=0`. The default `miner_sw`
source now scans until the requested matching tail is complete or the journal
ends, within a 20-second read deadline, and fails rather than truncating when
the deadline expires. Fleet's miner proxy allows 30 seconds per request.
`UpdateStatus.error` can now be present in `available` or `current` to report
the newest failed catalog check, update request, or update attempt, and
`message` carries a one-line summary of `error` (at most 512 bytes). The
fake-proto-rig simulator is updated for the `lines=0` and `message` changes.

### MDK v2

- Published mirror: btc-mining/proto-api
  - Source path: `mdk/v2/spec-all.json`
  - Specification version: `2-af84dedafde56a43`
  - Mirror commit SHA: 38d9beae4296b7747ae8bb828f69099148667f68
  - Extraction Date: 2026-10-26
- Contract source: Frigate (private)
  - Source path: `proto/mdk/v2/`
  - Commit SHA: 6acbeba0dcde568d33ca7121aee15a3b567abb4f
  - Commit Date: 2026-10-26

`openapi/MDK-v2-API.json` is the mirror's `spec-all.json`, byte for byte; its
SHA-256 matches the mirror's `source.json`. Frigate publishes that mirror from
`bin/task api:export`. Running the export at the Frigate commit above
reproduces all four mirror files and the specification version exactly, so
this snapshot and the protos in `sdk/mdk/proto/mdk/v2` come from the same
Frigate commit. `spec-all.json` covers every product; `spec-ct1.json` is a
strict subset of it with identical shared paths and definitions.

The snapshot documents the protobuf-first `/api/v2/...` REST/JSON gateway.
ProtoOS generates a private TypeScript client from it under the MDK v2 adapter,
and its request and response types are the shapes the adapter works with on
both MDK v2 transports.

The MDK v2 protos are vendored separately in `sdk/mdk/proto/mdk/v2` and pinned
in `sdk/mdk/README.md`. They generate the protobuf-es bindings that the ProtoOS
Connect client encodes and decodes with, so this snapshot and those protos must
describe compatible contracts. Refresh both from the same Frigate commit.

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

1. Copy `mdk/v2/spec-all.json` from the chosen `btc-mining/proto-api`
   publication to `openapi/MDK-v2-API.json`, and check its SHA-256 against that
   publication's `mdk/v2/source.json`
2. Identify the Frigate commit it was exported from: run `bin/task api:export`
   at a candidate Frigate `main` commit and confirm that
   `gen/mdkv2-public/source.json` matches the mirror's `source.json`
3. Copy that commit's `proto/mdk/v2` sources to `sdk/mdk/proto/mdk/v2` and
   update the pin in `sdk/mdk/README.md`
4. Update this VERSION.md with the mirror version, both commit SHAs, and dates
5. Regenerate the dependent generated code:
   - Client: `cd client && npm run generate-api-types`
   - MDK v2 bindings: `just gen-mdk-protos` from the repository root
6. Update the fake CT1 simulator if the consumed MDK v2 surface changed
   (see `server/fake-ct1/README.md`)
7. Run tests to verify compatibility
8. Commit all changes together

**Note**: The OpenAPI specs are the consumed miner API contracts. They drive the
generated ProtoOS TypeScript clients and serve as references for the
hand-maintained simulators and plugin REST clients.
