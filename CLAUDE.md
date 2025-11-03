# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Overview

Proto Fleet is a monorepo for a Bitcoin mining fleet management system with three main components:

- **Client**: TypeScript/React applications (ProtoOS dashboard and ProtoFleet management UI)
- **Server**: Go backend service (fleet API, telemetry, device management)
- **Miner Firmware**: Rust-based firmware for mining hardware (Git submodule)

The system allows management of both Proto (custom firmware) and Antminer devices through a unified interface.

**Note**: The `miner-firmware/` directory is a Git submodule that must be initialized before certain operations (e.g., generating ProtoOS API types from the miner API definitions).

## Quick Reference

For detailed development commands, testing instructions, and component-specific guidance:

- **Client development**: See `client/CLAUDE.md`
- **Server development**: See `server/CLAUDE.md`

## Tech Stack

- **Frontend**: React 19, TypeScript, Vite 7, Zustand, Tailwind CSS 4
- **Backend**: Go with Connect RPC (gRPC-compatible), MySQL, InfluxDB
- **API**: Protocol Buffers for type-safe cross-language communication
- **Build Tools**: Just (task runner), Buf (Protobuf), Docker Compose

## Initial Setup

```bash
# Activate Hermit environment (manages tool versions)
./bin/activate-hermit

# Install all dependencies
just init

# Initialize miner-firmware submodule
git submodule update --init --recursive
```

## Common Development Workflows

### Running the Full Stack

```bash
# Start both client and server (ProtoFleet)
just dev
```

This starts the Go backend with Docker Compose and the Vite dev server for ProtoFleet at http://localhost:5173.

For running individual components or other development commands, see:

- Client commands: `client/CLAUDE.md`
- Server commands: `server/CLAUDE.md`

### Working with Protocol Buffers

All APIs are defined in `.proto` files in the `proto/` directory. After modifying proto definitions:

```bash
# Generate TypeScript and Go code from protobuf definitions
just gen
```

**Important**: Always commit generated code changes alongside proto definition changes.

The code generation process:

1. Validates proto files with `buf lint`
2. Generates TypeScript clients in `client/src/protoFleet/api/generated/`
3. Generates Go server code in `server/generated/`
4. Regenerates sqlc database bindings (if migrations changed)

## Monorepo Structure

```
proto-fleet/
├── client/                    # TypeScript/React applications
│   ├── src/
│   │   ├── protoOS/          # Single miner dashboard (REST API)
│   │   ├── protoFleet/       # Fleet management UI (gRPC streaming)
│   │   └── shared/           # Shared components and utilities (50+ components)
│   └── CLAUDE.md             # Detailed client development guide
├── server/                    # Go backend service
│   ├── cmd/fleetd/           # Main entry point
│   ├── internal/domain/      # Business logic (pairing, telemetry, command, etc.)
│   ├── internal/handlers/    # gRPC request handlers
│   ├── internal/infrastructure/  # Database, queue, encryption, logging
│   ├── migrations/           # Database schema migrations (sequential)
│   ├── sqlc/queries/         # SQL query definitions for code generation
│   ├── generated/            # Generated code (protobuf, sqlc)
│   └── CLAUDE.md             # Detailed server development guide
├── proto/                     # Protocol Buffer API definitions (shared)
│   ├── auth/, pairing/, telemetry/, fleetmanagement/, etc.
├── miner-firmware/           # Rust firmware (Git submodule)
├── plugin/                   # External plugin support
├── deployment-files/         # Deployment configurations
└── bin/                      # Hermit-managed binaries
```

## Architecture Overview

### Protobuf-First API Design

All APIs are defined in Protocol Buffer format in the `proto/` directory. This ensures:

- Type safety across TypeScript and Go
- Automatic client/server code generation
- Clear API contracts and versioning
- Support for both gRPC and Connect protocols

### Client Architecture

Two separate React applications sharing common components:

**ProtoOS** (Single Miner Dashboard):

- REST API with polling for updates
- Generated types from OpenAPI/Swagger definitions in miner firmware
- Zustand state management with slice-based architecture
- Served by embedded API server on mining device

**ProtoFleet** (Fleet Management UI):

- gRPC-Web with Connect-RPC
- Server-to-client streaming for real-time telemetry
- Zustand state management with fleet-specific slices
- Connects to Go backend service

**Shared Component Library**:

- 50+ production-ready UI components in `src/shared/components/`
- 40+ Storybook stories for visual documentation
- Reusable across both applications

See `client/CLAUDE.md` for detailed state management, API integration patterns, and component organization.

### Server Architecture

Go service following Domain-Driven Design principles:

**Core Domains**:

- **Pairing**: Device discovery and registration (supports Proto and Antminer via plugins)
- **Telemetry**: Real-time and historical metrics collection (stored in InfluxDB)
- **Commands**: Asynchronous command execution with queue-based system
- **Fleet Management**: High-level operations for managing groups of devices
- **Authentication**: Token-based auth for clients (users) and miners (devices)

**Data Layer**:

- **MySQL**: Primary data store with golang-migrate for schema migrations
- **InfluxDB**: Time-series database for telemetry metrics
- **sqlc**: Type-safe Go code generation from SQL queries

**Plugin System**:

- External plugins provide custom discovery and pairing logic for new miner types
- Plugins are loaded at startup and take priority over internal implementations
- Supports extensibility without modifying core codebase

See `server/CLAUDE.md` for detailed domain architecture, handler patterns, and database workflows.

### Data Flow

1. **Device Discovery**: Nmap-based network scanning or plugin-based discovery identifies devices
2. **Pairing**: Device authentication and registration with fleet database
3. **Telemetry Collection**: Scheduled polling collects metrics and stores in InfluxDB
4. **Command Execution**: Queue-based system for asynchronous command dispatch
5. **Real-time Updates**: gRPC streaming pushes telemetry to connected ProtoFleet clients

### API Integration Patterns

**ProtoOS → Miner API** (REST):

- Polling-based updates
- Generated client from Swagger definitions
- Custom hooks abstract API calls and update Zustand store

**ProtoFleet → Fleet Service** (gRPC):

- Server-to-client streaming for live telemetry
- Generated client from Protocol Buffers
- Custom hooks handle streaming connections and store updates

Key difference: ProtoOS uses REST polling while ProtoFleet uses gRPC streaming for live data.

## Cross-Component Development Workflows

### Adding a New API Endpoint

1. Define the API in appropriate `.proto` file in `proto/` directory
2. Run `just gen` to regenerate TypeScript and Go code
3. Implement server handler in `server/internal/handlers/`
4. Register handler in `server/cmd/fleetd/main.go`
5. Create client hook in `client/src/{app}/api/`
6. Update Zustand store slice to consume the data
7. Commit proto definitions and all generated code together

### Making Database Schema Changes

1. Create migration: `cd server && just new-migration <name>`
2. Write both up and down migrations in `server/migrations/`
3. Run `just gen` to regenerate sqlc bindings
4. Update queries in `server/sqlc/queries/` if needed
5. Never modify existing migrations after deployment

### Adding Features to Client

1. Determine target app: ProtoOS, ProtoFleet, or shared
2. Check `src/shared/components/` for existing components (50+ available)
3. Place feature in appropriate `src/{app}/features/` directory
4. Create Storybook stories for new components
5. Write tests with Vitest and Testing Library

See `client/CLAUDE.md` for detailed component organization, import rules, and development patterns.

### Adding Business Logic to Server

1. Add domain logic to appropriate package in `internal/domain/`
2. Create gRPC handler in `internal/handlers/`
3. Add tests for domain logic and handlers
4. Update stores in `internal/domain/stores/sqlstores/` if database access needed

See `server/CLAUDE.md` for detailed domain patterns, testing infrastructure, and database workflows.

## Important Development Notes

### Code Generation

All generated code must be committed to Git. Run `just gen` after:

- Modifying protobuf definitions in `proto/`
- Changing database migrations in `server/migrations/`
- Adding/modifying sqlc queries in `server/sqlc/queries/`

Never manually edit generated files in:

- `client/src/protoOS/api/generatedApi.ts`
- `client/src/protoFleet/api/generated/`
- `server/generated/`

### Git Submodules

The miner-firmware directory must be initialized for certain operations:

```bash
# Initialize/update submodule
git submodule update --init --recursive

# Clean submodules
just clean-submodules
```

### Multi-App Build System

The client uses Vite with mode-based builds to support two separate applications:

- ProtoOS: `vite build --mode protoOS` → `dist/protoOS/`
- ProtoFleet: `vite build --mode protoFleet` → `dist/protoFleet/`

Each app has its own `index.html` entry point in `src/{app}/`.

### Component Boundaries

Maintain strict separation between applications:

- Code in `client/src/shared/` cannot import from ProtoOS or ProtoFleet
- ProtoOS and ProtoFleet cannot import from each other
- Server code is completely independent of client code

This ensures applications remain decoupled and shared code stays truly reusable.

## Git Workflow

### Creating Branches

Create feature branches from Linear issue numbers:

```bash
# From Linear issue DASH-123
git checkout -b <username>/dash-123-short-description
```

### Committing Changes

Write clear, logical commit messages:

```bash
git add .
git commit -m "feat: add telemetry streaming to fleet UI

- Implement server-to-client streaming connection
- Add telemetry slice to fleet store
- Update MinerList to display live metrics"
```

Follow conventional commit format:

- `feat:` - New feature
- `fix:` - Bug fix
- `refactor:` - Code refactoring
- `docs:` - Documentation changes
- `test:` - Test additions or updates
- `chore:` - Build/tooling changes

### Creating Pull Requests

**Important**: Always use the Linear MCP to retrieve the correct issue URL before creating a PR.

When creating a PR, follow these steps:

1. Use the Linear MCP `get_issue` tool to retrieve the issue details and URL
2. Use the returned URL in the PR body
3. Create the PR with `gh` CLI using the consistent format below

Example workflow:

```bash
# First, use Linear MCP to get issue details (this returns the correct URL)
# mcp__linear-server__get_issue with id: "DASH-123"

# Then create PR with the URL from Linear
gh pr create --title "[DASH-123] Brief description" --body "$(cat <<'EOF'
## [[DASH-123] Brief description](https://linear.app/squareup/issue/DASH-123/brief-description)

## Summary
- Bullet point summary of changes

## Test Plan
- How to verify the changes work

## Related
- Linear issue: DASH-123
EOF
)"
```

**Note**: Never manually construct Linear URLs. Always use the Linear MCP `get_issue` tool to get the correct URL format.

## Testing

The client and server each have their own testing approach:

**Client**: Vitest + Testing Library for unit/integration tests, Storybook for visual component testing
**Server**: Go test framework with Docker Compose providing test environment (MySQL, InfluxDB, simulated miners)

For detailed testing commands and patterns, see `client/CLAUDE.md` and `server/CLAUDE.md`.

## Additional Resources

- **Root README**: `README.md` - High-level project introduction
- **Client README**: `client/README.md` - Client build and directory structure
- **Server README**: `server/README.md` - Server features and API overview
- **Copilot Instructions**: `.github/copilot-instructions.md` - Code review guidelines
- **ProtoOS Store Documentation**: `client/src/protoOS/store/README.md` - Comprehensive state management guide

Each subdirectory contains additional component-specific documentation and README files.
