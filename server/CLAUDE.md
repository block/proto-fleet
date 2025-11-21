# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Fleet is a Go-based service for managing a fleet of Bitcoin mining devices. The service provides gRPC/HTTP API endpoints for device discovery, pairing, telemetry, command execution, and fleet management. It uses MySQL for persistence, InfluxDB for telemetry data, and supports multiple miner types (Proto, Antminer, etc.) through a plugin-based architecture.

## Development Commands

### Build and Run

```bash
# Run local server with Docker Compose (watch mode enabled)
just dev

# Start all services (without watch)
just start

# Stop services
just stop

# Build all Go packages
just build-all

# Install fleetd binary
just install

# Clean rebuild (wipes all data)
just clean-build

# Rebuild just fleet-api
just rebuild-fleet-api
```

### Testing and Quality

```bash
# Run all tests
just test

# Lint code
just lint

# Format code
just format
```

### Code Generation

```bash
# Run all code generation
just gen

# Generate database query bindings (sqlc)
just gen-db-queries

# Generate protobuf/gRPC code
just gen-miner-protos

# Run go:generate directives
just gen-go
```

### Database Operations

```bash
# Start MySQL database
just db-up

# Run migrations
just db-migrate

# Create new migration
just new-migration <migration_name>

# Interactive MySQL shell
just db-shell

# Stop database
just db-down

# Run down migrations
just db-clean
```

### Testing Single Files

To run tests for a specific package:

```bash
go test ./internal/domain/pairing -v
```

To run a specific test function:

```bash
go test ./internal/domain/pairing -v -run TestFunctionName
```

## Architecture Overview

### Project Structure

The codebase follows a domain-driven design with clear separation of concerns:

- **`cmd/fleetd/`** - Main application entry point and configuration
- **`internal/domain/`** - Core business logic organized by domain concepts (auth, pairing, telemetry, command, etc.)
- **`internal/handlers/`** - gRPC/HTTP request handlers that delegate to domain services
- **`internal/infrastructure/`** - Infrastructure concerns (database, queue, encryption, logging, networking)
- **`generated/`** - All generated code (protobuf, sqlc bindings)
- **`migrations/`** - Database schema migrations (sequential numbered files)
- **`sqlc/queries/`** - SQL query definitions for sqlc code generation
- **`sdk/v1/`** - SDK for external integrations
- **`fake-antminer/`** - Development simulator for testing Antminer plugin integration

### Core Domain Concepts

**Pairing**: The process of discovering and registering mining devices with the fleet. Supports multiple miner types through a pluggable pairer interface.

**Telemetry**: Real-time and historical metrics collection from mining devices. Uses InfluxDB for storage and a scheduler for periodic data collection.

**Commands**: Asynchronous command execution system for mining devices. Commands are queued, executed, and their status can be monitored.

**Fleet Management**: High-level operations for managing groups of devices (listing, filtering, monitoring status).

**Plugins**: Extensible plugin system that allows external plugins to provide custom discoverers and pairers for new miner types. Plugins are loaded at startup and take priority over internal implementations.

**Authentication**: Token-based authentication with separate token types for clients (users) and miners (devices).

### Data Layer

**Database**: MySQL with schema defined in `migrations/` directory. Migrations run automatically on startup.

**Query Generation**: Uses [sqlc](https://sqlc.dev/) to generate type-safe Go code from SQL queries. Queries are defined in `sqlc/queries/` and the schema is derived from migrations.

**Stores**: Repository pattern implementations in `internal/domain/stores/sqlstores/` provide database access.

**Transactions**: Transactor pattern (`sqlstores.NewSQLTransactor`) for managing transactions across multiple operations.

### API Layer

**Protocol**: Uses [Connect RPC](https://connectrpc.com/) which supports both gRPC and Connect protocols over HTTP/1.1 and HTTP/2.

**Code Generation**: API definitions are in protobuf format. The `miner-protos` directory is a symlink to miner firmware protos. Generated code goes to `generated/miner-api/` and `generated/grpc/`.

**Interceptors**: Authentication, error mapping, logging, and validation are implemented as Connect interceptors in `internal/handlers/interceptors/`.

**Endpoints**: Each domain has a corresponding handler package that implements the gRPC service interface.

### Plugin System

Plugins are external processes that communicate with the fleet service to provide custom functionality:

- **Discovery**: Plugins can implement custom device discovery logic for new miner types
- **Pairing**: Plugins can implement custom pairing logic for new miner types
- **Priority**: Plugin-based discoverers and pairers take priority over internal implementations

Configuration is in `cmd/fleetd/config.go` with options for plugin directories, startup timeouts, and health check behavior.

### Key Services

**MinerService** (`internal/domain/miner/`): Core service for interacting with mining devices, managing credentials, and device lifecycle.

**TelemetryService** (`internal/domain/telemetry/`): Manages telemetry collection, storage in InfluxDB, and scheduled polling of devices.

**ExecutionService** (`internal/domain/command/`): Manages asynchronous command execution using a database-backed message queue.

**PairingService** (`internal/domain/pairing/`): Orchestrates device discovery and pairing with support for multiple miner types.

### Testing

The service includes Docker Compose configurations for development with simulated miners:

- **mms**: Proto firmware simulator (5 replicas by default)
- **fake-antminer**: Antminer simulator for testing the Antminer plugin (5 replicas by default)
- **proto-sim**: Single Proto simulator on fixed ports
- **antminer-sim**: Single Antminer simulator on fixed ports for plugin testing

Replicas can be scaled with: `docker compose up --scale mms=10`

## Important Development Notes

### Generated Code

All generated code must be checked into Git. Run `just gen` after:

- Modifying protobuf definitions
- Changing database migrations
- Adding/modifying sqlc queries
- Updating dependencies

### Environment Variables

Configuration uses environment variables or command-line flags (see `internal/domain/config.go`). For local development, create a `.env` file in the server directory.

### Docker Networking

The service runs in Docker with host networking mode. On non-Linux systems, enable host networking in Docker Desktop: Settings → Resources → Network → Enable host networking.

### Database Migrations

Migrations are sequential and run automatically on startup. Always create migrations using `just new-migration <name>`. Never modify existing migrations after they've been deployed.

### API Testing

Use the `testing.http` file with GoLand's HTTP Client or the VS Code Rest Client extension to make API requests during development.
