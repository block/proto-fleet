# Proto Rig API Specifications

This directory contains vendored API specifications for Proto miner devices. The
v1 MDK spec is extracted from the private `miner-firmware` repository; the v2 MDK
spec is generated from Frigate's protobuf-first API and vendored here as the
OpenAPI contract consumed by ProtoOS.

## Directory Structure

```
proto-rig-api/
├── openapi/        # OpenAPI specifications for the REST APIs
│   ├── MDK-API.json
│   └── MDK-v2-API.json
├── VERSION.md      # Version tracking (single source of truth)
└── README.md       # This file
```

The OpenAPI specs are the consumed miner API contracts. They drive generated
ProtoOS adapter clients and serve as the reference for simulator and plugin
implementations.

## Usage

### OpenAPI Specifications

Used by:

1. **Client** - To generate private TypeScript clients for ProtoOS adapters
2. **Simulators** - As reference for fake miner REST API implementations
3. **Plugins** - As reference for plugin REST clients

Current specs:

| Spec                      | Source                                                     | Generated client                                                  |
| ------------------------- | ---------------------------------------------------------- | ----------------------------------------------------------------- |
| `openapi/MDK-API.json`    | MDK v1 OpenAPI from `miner-firmware`                       | `client/src/protoOS/api/adapters/mdkV1/generated/generatedApi.ts` |
| `openapi/MDK-v2-API.json` | MDK v2 OpenAPI generated from Frigate protobuf definitions | `client/src/protoOS/api/adapters/mdkV2/generated/generatedApi.ts` |

```bash
# Generate TypeScript client
cd client && npm run generate-api-types
```

The generated code is placed in `client/src/protoOS/api/adapters/<dialect>/generated/generatedApi.ts` (e.g. `adapters/mdkV1/generated/`). Each generated client is private to its adapter; consumers import through the adapter barrel (e.g. `@/protoOS/api/adapters/mdkV1`), a boundary enforced by `no-restricted-imports` in `client/eslint.config.js`.

MDK v2 has two vendored Frigate sources. The OpenAPI snapshot here generates
the adapter-local REST client and the request and response types the MDK v2
adapter is written against. The protos in
[`sdk/mdk/proto/mdk/v2`](../sdk/mdk/README.md) generate the protobuf-es
bindings that the ProtoOS Connect client uses on the device, plus the Go
contracts for the CT1 plugin and `server/fake-ct1`. The Connect client encodes
and decodes with the protos but returns data typed by this snapshot, so refresh
both from the same Frigate commit. `VERSION.md` records both pins and any skew
between them.

The simulators (`server/fake-proto-rig/`, `server/fake-ct1/`) manually implement
the consumed endpoint surfaces - see their READMEs for maintenance guidelines.

## Versioning

The `VERSION.md` file in this directory contains:

- Source repository and commit SHA
- Extraction date
- Update instructions

## Updating

When a miner API changes:

1. Re-vendor the affected OpenAPI specification (see `VERSION.md` for the
   exact source paths and steps). For MDK v2, also sync
   `sdk/mdk/proto/mdk/v2` from the same Frigate commit.
2. Update `VERSION.md` with the source commit SHA and dates (and the
   `sdk/mdk/README.md` pin for MDK v2 protos)
3. Regenerate dependent code:
   - `cd client && npm run generate-api-types` (TypeScript clients from the
     OpenAPI specs)
   - `just gen-mdk-protos` from the repository root for MDK v2 (Go,
     Connect-Go, and protobuf-es bindings)
4. Update simulator APIs if the contract changed:
   - See `server/fake-proto-rig/README.md` and `server/fake-ct1/README.md` for
     maintenance checklists
5. Run tests to verify compatibility
6. Commit all changes together
