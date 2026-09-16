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

- **288 modules × 3 hashboards = 864 boards** (default: 6 tanks × 48 modules × 3 slots, matching a real CT1)
- Per-board hydro telemetry (hashrate, temperatures, voltage, power)
- Shared PSU identity, power, voltage, and thermal health per module, reported by
  each board. Demo module 11 reports a PSU thermal warning while in its error state.
- **312 ASICs per board** with layout coordinates
  (four stacks of 78; the client wraps each stack into three rows of 26)
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
| GET | `/api/v2/pools` · `/pools/diagnostics` | primary pool plus two standby backups and live connection state |
| GET | `/api/v2/telemetry/shares/history` | deterministic demo history for pools 1–3; explicit pool filter + total grouping, up to seven days |
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

**Pools demo.** Pool 1 (`Primary`) is active while any board is mining. Pools 2
and 3 (`Backup 1` and `Backup 2`) remain ready on standby. Historical share
fixtures provide 98 accepted / 2 rejected per minute for Primary, 9 accepted /
1 rejected for Backup 1 (illustrating earlier failover activity), and observed
zero shares for Backup 2. These deterministic samples do not reconstruct live
mining or failover events; stopping mining changes diagnostics, not history.

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
| `CT1_TANKS` | Number of tanks | `6` |
| `CT1_MODULES_PER_TANK` | Modules per tank | `48` |
| `CT1_SLOTS_PER_MODULE` | Hashboard slots per module | `3` |
| `CT1_ASICS_PER_BOARD` | ASICs reported per board | `312` |
| `CT1_DEMO_MODULE_STATES` | Set to `true` to start each tank with module 03 sleeping, 06 offline, and module 11's hashboards reporting 1, 2, and 3 errors | `false` |

Shrink the tree for fast tests, e.g. `CT1_TANKS=1 CT1_MODULES_PER_TANK=2`.

For tank-page UI checks, use `CT1_TANKS=6 CT1_MODULES_PER_TANK=16
CT1_DEMO_MODULE_STATES=true`. Each tank starts with one sleeping module, one
offline module, one needing attention, and 13 healthy modules. Module 11's
left, middle, and right hashboards report one, two, and three errors respectively,
showing light pink, medium pink, and red in the tank page. The examples
only apply to module numbers present in the configured topology. These are
initial states: start/stop commands can change them, and restarting with the
flag enabled restores the examples.

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
