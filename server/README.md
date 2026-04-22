# Fleet Service

Fleet is a Go-based service for managing a fleet of Bitcoin mining devices. It provides gRPC/HTTP API endpoints for device discovery, pairing, telemetry, command execution, and fleet management. It uses PostgreSQL/TimescaleDB for persistence and supports multiple miner types (Proto, Antminer, etc.) through a plugin-based architecture.

## Development Commands

### Build and Run

```bash
just dev              # Run local server with Docker Compose (watch mode)
just start            # Start all services (without watch)
just stop             # Stop services
just build            # Build all Go packages
just install          # Install fleetd binary
just rebuild-all      # Clean rebuild (wipes all data, rebuilds plugins)
just rebuild-services # Clean rebuild of docker services only (reuses existing plugin binaries)
just rebuild-fleet-api  # Rebuild just fleet-api
```

From the repo root, `just rebuild-plugin <name>` rebuilds a single plugin (`proto`, `antminer`, `virtual`, or `asicrs`) without touching the others.

### Delve debugging

`fleet-api` runs under `dlv` and exposes it on `:40000` inside the container. The dev image builds without debug symbols by default for faster rebuilds. To enable breakpoints:

```bash
GO_GCFLAGS="all=-N -l" just dev
```

Attach from the host with `dlv connect` or your IDE. Requires mapping port 40000 in `docker-compose.yaml` (not mapped by default).

### Profiling

In `docker-compose.base.yaml`, uncomment `HTTP_PPROF_ADDR` and the matching `ports` entry under `fleet-api` (the port mapping binds to host loopback only), then restart:

```bash
just rebuild-fleet-api
```

Capture profiles while the server is running:

```bash
# CPU (30s sample) — open interactive flame graph in browser
curl "http://localhost:6060/debug/pprof/profile?seconds=30" > cpu.pprof
go tool pprof -http=:8000 cpu.pprof

# Heap allocations
curl http://localhost:6060/debug/pprof/heap > heap.pprof
go tool pprof -http=:8000 heap.pprof

# Goroutine trace — shows GC pauses, scheduler latency, I/O stalls
curl "http://localhost:6060/debug/pprof/trace?seconds=5" > trace.out
go tool trace trace.out

# Live goroutine dump (useful for detecting leaks or deadlocks)
curl "http://localhost:6060/debug/pprof/goroutine?debug=2"
```

> **Note:** pprof exposes sensitive runtime data. Only bind to loopback (`127.0.0.1`) unless you intentionally want remote access. Never enable in production.

### Testing and Quality

```bash
just test             # Run all tests
just lint             # Lint code
just format           # Format code
```

To run tests for a specific package:

```bash
go test ./internal/domain/pairing -v
go test ./internal/domain/pairing -v -run TestFunctionName
```

### Code Generation

```bash
just gen              # Run all code generation
just gen-db-queries   # Generate database query bindings (sqlc)
just gen-sdk-protos   # Generate SDK protobuf code
just gen-go           # Run go:generate directives
```

All generated code must be checked into Git. Run `just gen` after modifying protobuf definitions, database migrations, or sqlc queries.

### Database Operations

```bash
just db-up            # Start PostgreSQL/TimescaleDB
just db-migrate       # Run migrations
just db-migration-new <name>  # Create new migration
just db-shell         # Interactive PostgreSQL shell
just db-down          # Stop database
just db-reset         # Run down migrations
```

Migrations are sequential and run automatically on startup. **Never modify existing migrations after they have been deployed.**

## Architecture

### Project Structure

The codebase follows a domain-driven design with clear separation of concerns:

- **`cmd/fleetd/`** — Main application entry point and configuration
- **`internal/domain/`** — Core business logic organized by domain (auth, pairing, telemetry, command, etc.)
- **`internal/handlers/`** — gRPC/HTTP request handlers that delegate to domain services
- **`internal/infrastructure/`** — Infrastructure concerns (database, queue, encryption, logging, networking)
- **`generated/`** — All generated code (protobuf, sqlc bindings)
- **`migrations/`** — Database schema migrations (sequential numbered files)
- **`sqlc/queries/`** — SQL query definitions for sqlc code generation
- **`sdk/v1/`** — SDK for external plugin integrations
- **`fake-antminer/`** — Development simulator for testing Antminer plugin integration

### Core Domains

- **Pairing**: Device discovery and registration. Supports multiple miner types through a pluggable pairer interface.
- **Telemetry**: Real-time and historical metrics collection. Uses TimescaleDB and a scheduler for periodic data collection.
- **Commands**: Asynchronous command execution with a database-backed message queue.
- **Fleet Management**: High-level operations for managing groups of devices (listing, filtering, monitoring status).
- **Authentication**: Token-based auth with separate token types for clients (users) and miners (devices).

### Plugin System

Plugins are external processes that communicate with the fleet service:

- **Discovery**: Plugins implement custom device discovery logic for new miner types
- **Pairing**: Plugins implement custom pairing logic for new miner types
- **Priority**: Plugin-based discoverers and pairers take priority over internal implementations

Configuration is in `cmd/fleetd/config.go` with options for plugin directories, startup timeouts, and health check behavior.

### Data Layer

- **PostgreSQL/TimescaleDB**: Primary data store with schema defined in `migrations/`. Migrations run automatically on startup.
- **sqlc**: Type-safe Go code generated from SQL queries in `sqlc/queries/`. Regenerate with `just gen-db-queries` from the server directory.
- **Stores**: Repository pattern implementations in `internal/domain/stores/sqlstores/`.
- **Transactions**: Transactor pattern (`sqlstores.NewSQLTransactor`) for managing transactions across multiple operations.

### API Layer

- **Protocol**: [Connect RPC](https://connectrpc.com/) supporting both gRPC and Connect protocols over HTTP/1.1 and HTTP/2.
- **Interceptors**: Authentication, error mapping, logging, and validation in `internal/handlers/interceptors/`.
- **Proto definitions**: API definitions in `../proto/`. Miner API definitions vendored in `../proto-rig-api/grpc/`.

## Running via Docker

The service runs in Docker with host networking mode. On non-Linux systems, enable host networking in Docker Desktop: **Settings → Resources → Network → Enable host networking**.

```bash
just dev
```

This will:

1. Connect to the database
2. Run any pending migrations
3. Start the server in Docker Compose with watch enabled
4. Serve the API on http://localhost:4000

### Test Simulators

Docker Compose includes simulated miners for development:

- **fake-proto-rig**: Proto firmware simulator (5 replicas by default)
- **fake-antminer**: Antminer simulator (2 replicas by default)
- **proto-sim**: Single Proto simulator on fixed ports
- **antminer-sim**: Single Antminer simulator on fixed ports

Scale replicas with: `docker compose up --scale fake-proto-rig=10`

## Configuration

The service is configured using environment variables or command-line flags. See `internal/domain/config.go`. For local development, create a `.env` file in this directory.

## API Testing

## Error Query Service (Testing/Development Only)

The Error Query Service provides a mock error management system for testing. It can return controlled seed data or randomly generated errors for devices.

> **Note:** This is temporary scaffolding to enable client development while server-side error query logic is being implemented.

### Seed Data Configuration

```bash
# Via environment variable
ERROR_QUERY_TEST_ERROR_SEED_FILE=./seed_errors.yaml fleetd
```

**Docker Compose:** In `server/docker-compose.yaml`, uncomment the seed file lines:
```yaml
environment:
  ERROR_QUERY_TEST_ERROR_SEED_FILE: "/app/testdata/seed_errors.yaml"
volumes:
  - ./internal/domain/errorquery/testdata:/app/testdata:ro
```

**Behavior:**
- Devices listed in the seed file use **only** the errors defined in the file
- Devices **not** in the seed file get randomly generated errors (30% probability per device)
- To force a device to have no errors, include it with an empty errors list: `errors: []`

### Seed File Format

```yaml
devices:
  - device_id: 1
    device_type: proto
    errors:
      - canonical_error: HASHBOARD_OVER_TEMPERATURE
        severity: major
        first_seen_ago: 2h
        last_seen_ago: 5m

  - device_id: 2
    device_type: proto
    errors: []  # Healthy device with no errors
```

### Available Options

| Field | Description | Example |
|-------|-------------|---------|
| `canonical_error` | Error code (with or without `MINER_ERROR_` prefix) | `HASHBOARD_OVER_TEMPERATURE` |
| `severity` | `critical`, `major`, `minor`, or `info` | `major` |
| `cause_summary` | Human-readable cause description | `"PSU temperature exceeded threshold"` |
| `recommended_action` | Suggested remediation | `"Check cooling system"` |
| `impact` | Effect on operations | `"Reduced hashrate"` |
| `component_id` | Format: `{device_id}_{type}_{index}` | `"1_psu_0"` |
| `first_seen_ago` | Duration since first occurrence | `2h`, `7d`, `30m` |
| `last_seen_ago` | Duration since last occurrence | `5m`, `1h` |
| `closed` | Mark error as resolved | `true` |
| `vendor_attributes` | Custom key-value metadata | `psu_temp_c: "87"` |
