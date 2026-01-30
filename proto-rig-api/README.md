# Proto Rig API Specifications

This directory contains vendored API specifications for the Proto miner devices. These files are extracted from the private `miner-firmware` repository to enable open-source development of the fleet management system.

## Directory Structure

```
proto-rig-api/
├── grpc/           # Protocol Buffer definitions for gRPC API
│   └── *.proto     # 13 proto files defining miner services
├── openapi/        # OpenAPI specification for REST API
│   └── MDK-API.json
├── VERSION.md      # Version tracking (single source of truth)
└── README.md       # This file
```

## Usage

### gRPC Proto Files

Used by the server to generate Go code for communicating with Proto miners:

```bash
# Generate Go code from proto files
cd server && just gen
```

The generated code is placed in `server/generated/miner-api/`.

### OpenAPI Specification

Used by:
1. **Client** - To generate TypeScript types for the ProtoOS dashboard
2. **Simulator** - As reference for the fake-proto-rig REST API implementation

```bash
# Generate TypeScript client
cd client && npm run generate-api-types
```

The generated code is placed in `client/src/protoOS/api/generatedApi.ts`.

The simulator (`server/fake-proto-rig/`) manually implements these endpoints - see its README for maintenance guidelines.

## Versioning

The `VERSION.md` file in this directory contains:
- Source repository and commit SHA
- Extraction date
- Update instructions

Both gRPC and OpenAPI specs are always updated together from the same commit to maintain consistency.

## Updating

When the miner API changes:

1. Update the appropriate specification files
2. Update the VERSION.md with new commit information
3. Regenerate all dependent code:
   - `cd client && npm run generate-api-types` (TypeScript types)
   - `cd server && just gen` (Go gRPC code)
4. Update the simulator REST API if OpenAPI spec changed:
   - See `server/fake-proto-rig/README.md` for maintenance checklist
5. Run tests to verify compatibility
6. Commit all changes together
