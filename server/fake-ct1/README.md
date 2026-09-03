# Fake CT1

A lightweight, dockerized simulator for a **CT1 container**, implementing the
MDK v2 REST API surface a fleet client hits against real hardware.

It is the CT1 counterpart to [`server/fake-proto-rig`](../fake-proto-rig)
(which fakes R2 rigs on the MDK v1 surface). It lets the fleet dev environment
develop and test the CT1 adapter, views, and device bundle **without real
hardware** and **without** Frigate's heavy `task dev:full` stack (TimescaleDB +
`hbsim` + `containersim` + `poolsim` + `maestrosim`).

## What it models

A full container is a **Tank → Module → Slot → Hashboard** tree:

- **96 modules × 3 hashboards = 288 boards** (default: 2 tanks × 48 modules × 3 slots)
- Per-board hydro telemetry (hashrate, temperatures, voltage, power)
- **312 ASICs per board** with layout coordinates
- Plant/PUE, fan, power-distribution, and power-feed telemetry
- Bucketed aggregate time-series telemetry
- Stateful mining writes (global/scoped power targets, start/stop, fixed voltage, module run-in)

State is held in memory and mutated by the write endpoints; telemetry reads
apply a small random variation so values look realistic.

## Implemented endpoints

| Method | Path | Notes |
|--------|------|-------|
| GET | `/api/v2/system/info` | `product: "CT1"`, version, control board |
| GET | `/api/v2/system/status` | health, active/total boards, HA role |
| GET | `/api/v2/container/map` | full Tank→Module→Slot→Hashboard tree |
| GET | `/api/v2/mining/status` | aggregate hashrate/power/efficiency + per-state counts |
| POST | `/api/v2/mining/start` · `/stop` | container-wide mining intent |
| GET/PUT | `/api/v2/mining/target-power` | read/set miner-wide per-board power target |
| GET | `/api/v2/pools` · `/pools/diagnostics` | configured pool and live connection state |
| GET | `/api/v2/telemetry/container` | plant (PUE), phase, fan, distribution, and feed telemetry |
| GET | `/api/v2/telemetry/timeseries` | bucketed aggregate or identifier-scoped hashrate, power, or temperature |
| GET | `/api/v2/hashboards/{identifier}/asics` | 312 ASICs + layout (`identifier` = MAC or serial) |
| POST | `/api/v2/mining/scoped/power` · `/power/clear` | set/clear per-board power target |
| POST | `/api/v2/mining/scoped/start` · `/stop` | scoped board lifecycle |
| PUT/DELETE | `/api/v2/mining/modules/{moduleId}/voltage/fixed` | set/clear module fixed voltage |
| POST | `/api/v2/modules/{moduleId}/run-in` | start a simulated synchronized module run-in |
| GET | `/health` | liveness |

**Scopes.** The scoped write endpoints accept a `scope` of `identifiers`
(MAC/serial) and/or `modules` (`{tank, module}` coordinates). An empty scope
matches every board.

## Usage

### Run directly

```bash
GOWORK=off go run .
curl -s localhost:8080/api/v2/system/status | jq
```

### Run with Docker

```bash
docker build -t fake-ct1 -f server/fake-ct1/Dockerfile .
docker run -p 8080:8080 fake-ct1
```

## Environment variables

| Variable | Description | Default |
|----------|-------------|---------|
| `HTTP_PORT` | Port to listen on | `8080` |
| `SERVER_ID` | Container server id | `ct1-sim-<uuid>` |
| `HOSTNAME` | Reported hostname | `fake-ct1-<uuid>` |
| `CT1_VERSION` | Reported firmware/OS version | `2.0.0` |
| `CT1_TANKS` | Number of tanks | `2` |
| `CT1_MODULES_PER_TANK` | Modules per tank | `48` |
| `CT1_SLOTS_PER_MODULE` | Hashboard slots per module | `3` |
| `CT1_ASICS_PER_BOARD` | ASICs reported per board | `312` |

Shrink the tree for fast tests, e.g. `CT1_TANKS=1 CT1_MODULES_PER_TANK=2`.

## Contract fidelity

Response shapes mirror the `mdk.v2` grpc-gateway JSON (camelCase field names,
`*_STATE_*` string enums), captured from
`proto-rig-api/openapi/MDK-v2-API.json`. Once `gen/mdk/v2` Go stubs are
vendored into the buf workspace (see issue #32 open decisions), the simulator
should be contract-locked to those generated types so it can't silently drift.

Use the real `task dev:full` stack as a reference oracle to refine shapes/values:

```bash
curl -s localhost:9091/api/v2/system/status | jq
```

## Tests

```bash
GOWORK=off go test ./...
```

> **Note:** This fixture is consumed by the CT1 adapter (#34), device bundle
> (#36), and local fleet testing. See the `fake-rig-fixtures` skill — a change
> here can ripple into contract and E2E suites.
