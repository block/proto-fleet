# Proto Rig API Specifications

This directory contains vendored API specifications for the Proto miner devices. These files are extracted from the private `miner-firmware` repository to enable open-source development of the fleet management system.

## Directory Structure

```
proto-rig-api/
├── openapi/        # OpenAPI specification for REST API
│   └── MDK-API.json
├── VERSION.md      # Version tracking (single source of truth)
└── README.md       # This file
```

## Usage

### OpenAPI Specification

Used by:
1. **Client** - To generate TypeScript types for the ProtoOS dashboard
2. **Simulator** - As reference for the fake-proto-rig REST API implementation
3. **Plugin** - As reference for the proto plugin REST client

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

## Updating

When the miner API changes:

1. Update the OpenAPI specification file
2. Update the VERSION.md with new commit information
3. Regenerate dependent code:
   - `cd client && npm run generate-api-types` (TypeScript types)
4. Update the simulator REST API if OpenAPI spec changed:
   - See `server/fake-proto-rig/README.md` for maintenance checklist
5. Run tests to verify compatibility
6. Commit all changes together
