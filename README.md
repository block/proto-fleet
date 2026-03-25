<p align="center">
  <a href="https://github.com/proto-at-block/proto-fleet" target="_blank" rel="noopener noreferrer">
    <img width="64" src="https://raw.githubusercontent.com/btc-mining/proto-fleet/main/docs/logo.svg" alt="Proto logo">
  </a>
</p>
<h1 align="center">
  Proto Fleet
</h1>
<h3 align="center">
  Mining management software. Evolved.
</h3>
<p align="center">
  No fees. No training. Full control.<br/>
  Open source fleet management for bitcoin miners.
</p>
<p align="center">
  <a href="https://github.com/proto-at-block/proto-fleet/blob/main/LICENSE">
    <img src="https://img.shields.io/badge/license-Apache%202.0-blue.svg" alt="Proto Fleet is released under the Apache 2.0 license." />
  </a>
  <a href="https://github.com/proto-at-block/proto-fleet/actions/workflows/protofleet-client-checks.yml">
    <img src="https://github.com/proto-at-block/proto-fleet/actions/workflows/protofleet-client-checks.yml/badge.svg" alt="Client checks status." />
  </a>
  <a href="https://github.com/proto-at-block/proto-fleet/actions/workflows/protofleet-backend-tests.yml">
    <img src="https://github.com/proto-at-block/proto-fleet/actions/workflows/protofleet-backend-tests.yml/badge.svg" alt="Backend tests status." />
  </a>
  <a href="https://github.com/proto-at-block/proto-fleet/actions/workflows/protofleet-server-checks.yml">
    <img src="https://github.com/proto-at-block/proto-fleet/actions/workflows/protofleet-server-checks.yml/badge.svg" alt="Server checks status." />
  </a>
  <a href="https://github.com/proto-at-block/proto-fleet/actions/workflows/protofleet-e2e-tests.yml">
    <img src="https://github.com/proto-at-block/proto-fleet/actions/workflows/protofleet-e2e-tests.yml/badge.svg" alt="E2E tests status." />
  </a>
</p>

<br/>

## Tech Stack

- **Frontend**: React 19, TypeScript, Vite 7, Zustand, Tailwind CSS 4
- **Backend**: Go with Connect RPC (gRPC-compatible), PostgreSQL/TimescaleDB
- **API**: Protocol Buffers for type-safe cross-language communication
- **Build Tools**: Just (task runner), Buf (Protobuf), Hermit (tool management), Docker Compose

## Repository Structure

```
proto-fleet/
├── client/                    # TypeScript/React applications
│   ├── src/
│   │   ├── protoOS/          # Single miner dashboard (REST API)
│   │   ├── protoFleet/       # Fleet management UI (gRPC streaming)
│   │   └── shared/           # Shared components and utilities (50+ components)
├── server/                    # Go backend service
│   ├── cmd/fleetd/           # Main entry point
│   ├── internal/domain/      # Business logic (pairing, telemetry, command, etc.)
│   ├── internal/handlers/    # gRPC request handlers
│   ├── internal/infrastructure/  # Database, queue, encryption, logging
│   ├── migrations/           # Database schema migrations (sequential)
│   ├── sqlc/queries/         # SQL query definitions for code generation
│   └── generated/            # Generated code (protobuf, sqlc)
├── proto/                     # Protocol Buffer API definitions (shared)
├── proto-rig-api/            # Vendored API specs for Proto miner communication
│   ├── grpc/                 # Protocol Buffer definitions
│   └── openapi/              # OpenAPI/Swagger specification
├── plugin/                   # Miner plugins (proto, antminer, virtual)
├── deployment-files/         # Production deployment configurations
└── bin/                      # Hermit-managed binaries
```

## Architecture Overview

### Two Client Applications

**ProtoOS** — Single miner dashboard served by the miner's embedded API server. Uses REST API with polling for updates, with types generated from OpenAPI/Swagger definitions.

**ProtoFleet** — Fleet management UI for managing multiple miners. Uses gRPC-Web with Connect-RPC and server-to-client streaming for real-time telemetry.

Both apps share a common component library in `src/shared/components/`.

### Server

Go service handling device pairing, telemetry collection, command execution, fleet management, and authentication. Uses TimescaleDB for time-series metrics and a plugin system for supporting different miner types.

### Data Flow

1. **Device Discovery**: Network scanning or plugin-based discovery identifies devices
2. **Pairing**: Device authentication and registration with fleet database
3. **Telemetry Collection**: Scheduled polling collects metrics and stores in TimescaleDB
4. **Command Execution**: Queue-based system for asynchronous command dispatch
5. **Real-time Updates**: gRPC streaming pushes telemetry to connected ProtoFleet clients

## Getting Started

### Prerequisites

- Docker and Docker Compose
- [Hermit](https://cashapp.github.io/hermit/) (or manually install Go, Node.js, Just, Buf)

### Initial Setup

```bash
# Activate Hermit environment (manages tool versions)
./bin/activate-hermit

# Install all dependencies
just setup
```

### Start Development

```bash
# Start both client and server
just dev
```

This starts the Go backend with Docker Compose and the Vite dev server for ProtoFleet at http://localhost:5173.

### Protocol Buffer Code Generation

After modifying proto definitions in `proto/`:

```bash
just gen
```

This validates proto files, generates TypeScript clients and Go server code, and regenerates sqlc database bindings. Always commit generated code changes alongside proto definition changes.

## Go Workspace

This repository uses a Go workspace (`go.work`) for integrated development across modules:

- `server/` — Main fleet backend service
- `plugin/proto/` — Proto miner plugin
- `plugin/antminer/` — Antminer plugin
- `plugin/virtual/` — Virtual miner simulator

Changes across modules are immediately available without publishing versions. Run `go work sync` after updating dependencies.

## Production Install

### Latest version

```shell
bash <(curl -fsSL https://proto-fleet.s3.us-east-1.amazonaws.com/releases/fleet/latest/install.sh)
```

### Specific version

```shell
bash <(curl -fsSL https://proto-fleet.s3.us-east-1.amazonaws.com/releases/fleet/latest/install.sh) v0.1.0
```

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for development workflows, coding conventions, and how to submit changes.
