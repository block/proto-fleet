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

Used by the client to generate TypeScript types for the ProtoOS dashboard:

```bash
# Generate TypeScript client
cd client && npm run gen-api
```

The generated code is placed in `client/src/protoOS/api/generatedApi.ts`.

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
3. Regenerate all dependent code
4. Run tests to verify compatibility
5. Commit all changes together
