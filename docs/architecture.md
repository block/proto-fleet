# Architecture Overview

Proto Fleet has two client applications and a Go backend for managing device pairing, telemetry, and fleet operations.

## Client Applications

### ProtoOS

ProtoOS is a single-miner dashboard that talks to a miner-hosted HTTP API. It uses REST endpoints generated from OpenAPI and Swagger definitions and relies on polling for most live state.

### ProtoFleet

ProtoFleet is the fleet management UI for operating multiple miners. It uses Connect RPC from the web client, combining unary queries for snapshots and historical data with server-streaming for live telemetry and other long-running operations.

Both applications share code under `client/src/shared/`, including components, hooks, assets, and styles.

## Server

The backend is a Go service that handles device pairing, telemetry collection, command execution, fleet management, and authentication. It uses a plugin system for different miner types, stores time-series metrics in TimescaleDB, and executes commands through an asynchronous database-backed queue.

## Plugins and API Definitions

Proto Fleet supports multiple miner integrations through plugins:

- `plugin/proto/` for Proto miners
- `plugin/antminer/` for Antminer devices
- `plugin/virtual/` for the virtual miner simulator used in development and testing
- `plugin/asicrs/` for Rust-based multi-manufacturer ASIC miner support
- `plugin/example-python/` for the example Python plugin (template for plugin authors)

Shared RPC and message contracts live in `proto/`. Miner-hosted ProtoOS API definitions live in `proto-rig-api/`.

## Project Layout

- `client/` React and TypeScript applications for ProtoOS and ProtoFleet
- `server/` Go backend service
- `proto/` shared Protocol Buffer API definitions
- `plugin/` miner integrations and simulators
- `packages/` shared tooling and generators
- `sdk/` generated or published client SDKs
- `deployment-files/` production deployment assets

## Data Flow

1. Device discovery uses plugin-based discovery and IP scanning to find or re-identify miners.
2. Pairing authenticates devices and registers them with the fleet database.
3. Telemetry collection polls devices on a schedule and stores metrics in TimescaleDB.
4. Command execution uses a database-backed queue for asynchronous dispatch and status tracking.
5. Live updates are streamed from the server to ProtoFleet clients for telemetry and related status updates.

## See Also

- [README.md](../README.md)
- [CONTRIBUTING.md](../CONTRIBUTING.md)
