# Fake CT1

A lightweight, dockerized simulator for a **CT1 container**, implementing the
MDK v2 REST API for ProtoOS and native gRPC for the Fleet CT1 plugin.

It is the CT1 counterpart to [`server/fake-proto-rig`](../fake-proto-rig)
(which fakes R2 rigs on the MDK v1 surface). It lets the fleet dev environment
develop and test the CT1 adapter, views, and device bundle **without real
hardware** and **without** Frigate's heavy `task dev:full` stack (TimescaleDB +
`hbsim` + `containersim` + `poolsim` + `maestrosim`).

## What it models

A full container is a **Tank → Module → Slot → Hashboard** tree:

- **96 CU1 modules × 3 hashboards = 288 boards** (6 tanks × 16 modules × 3 slots)
- Per-board hydro telemetry (hashrate, temperatures, voltage, power)
- Shared PSU identity, power, voltage, and thermal health per module, reported by
  each board. Tank 2 module 11 reports a PSU thermal warning while in its error state.
- **312 ASICs per board** with layout coordinates
  (four stacks of 78; the client wraps each stack into three rows of 26)
- Plant/PUE, fan, power-distribution, and power-feed telemetry
- Bucketed aggregate time-series telemetry
- Stateful mining writes (global/scoped power targets, start/stop, fixed voltage, module run-in)

State is held in memory and resets on restart. The default scenario has three
fully healthy tanks, one tank with mixed failures, one with a single board fault,
and one with sleeping/offline modules, leaving **272 mining boards**: 90 fully mining
CU1s plus one CU1 mining on two of its three boards. Set `CT1_DEMO_MODULE_STATES=false`
for the all-96-mining reference.

Telemetry uses a shared five-second simulation clock, slow
seeded variation, and stable device differences. Reads within a sample interval
reconcile across endpoints. `CT1_SEED` changes the pattern; identical seeds and
elapsed times reproduce it, independent of request order.

## Native gRPC

The listener defaults to `:9095` (`GRPC_PORT`; zero selects an ephemeral port).
SystemInfo, ContainerMap, MiningStatus, ContainerHealth and ContainerControls
reads share the REST state and clock. SetContainerControl writes update that same
state and return controller-register confirmation. The dedicated main breaker
remains unsupported, matching the REST fixture. Other generated services and
historical RPCs are not enabled. This development listener is plaintext without
authentication; see [CT1 transport configuration](../../docs/development/ct1-grpc.md)
for the production plugin's TLS/token settings.

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
| GET | `/api/v2/telemetry/shares/history` | deterministic demo history for pools 1–3; explicit pool filter + total grouping, aligned hour/day buckets, up to seven days |
| GET | `/api/v2/telemetry/container` | plant (PUE), phase, fan, distribution, and feed telemetry |
| GET | `/api/v2/power/container/controls` | six supported boolean controls and current known states, plus setpoints; dedicated main breaker unsupported |
| POST | `/api/v2/power/container/controls/{name}` | explicit desired state; subsequent equipment and telemetry reads return the same state |
| GET | `/api/v2/telemetry/timeseries` | recorded board and plant history, including fans, PDUs, phases and feed power/voltage |
| GET | `/api/v2/hashboards/{identifier}/asics` | 312 ASICs + layout (`identifier` = MAC or serial) |
| POST | `/api/v2/mining/scoped/power` · `/power/clear` | set/clear per-board power target |
| POST | `/api/v2/mining/scoped/start` · `/stop` | scoped board lifecycle |
| PUT/DELETE | `/api/v2/mining/modules/{moduleId}/voltage/fixed` | set/clear module fixed voltage |
| POST | `/api/v2/modules/{moduleId}/run-in` | accept a run-in request; returns queued, execution remains unimplemented |
| GET | `/health` | liveness |

**Scopes.** The scoped write endpoints accept a `scope` of `identifiers`
(MAC/serial) and/or `modules` (`{tank, module}` coordinates). An empty scope
matches every board.

**Pools demo.** Pool 1 is active while any board is mining; pools 2 and 3
remain ready on standby. The explicit demo cadence is 199 accepted / 1 rejected
share per mining minute (99.5%), shared by live acceptance and history. Difficulty
2048 is a demo value, not a physical share-rate model. Pre-start backfill includes
a daily 02:00–03:00 UTC failover to pool 2. Shares occur only on the active pool;
pool 3 has observed zeros. A daily 00:17–00:22 UTC collection gap reduces coverage.
After startup, recorded start/stop transitions determine shares. Future and
expired minutes are unobserved, not zero-share samples.

**History.** Seven days of deterministic backfill use the initial scenario.
After startup the simulator records minute samples and control transitions in
memory. Samples are advanced lazily before each request, so idle polling periods
are filled using the settings that actually applied. Changes never rewrite older
samples. Buckets report measured min/max/average and sample counts, with at most
300 buckets per general-history query. Choose 1h buckets for a five-day view.
Board identifiers, `pdu-1` through `pdu-6`, `dry-cooler-fan-1` through
`dry-cooler-fan-10`, `PHASE_A/B/C`, and `pdu-1-outlet-01` style feed IDs scope the
corresponding metrics. Unknown components are rejected. Startup backfill is
synthetic; runtime history is not persisted across restarts.

Share-history range validation follows Frigate's
[`validateIndefiniteHistoryRange`](https://github.com/btc-mining/frigate/blob/5225b5fa64f8d81d95329dcd5bbd6b9c70005198/pkg/api/telemetry_service.go):
only one-hour or one-day buckets with aligned start/end timestamps are accepted.
The response reports `HISTORY_SOURCE_HOURLY`; the retained hourly data is
aggregated onto the requested grid, not returned at a mixed resolution.

## Usage

### Run directly

```bash
GOWORK=off go run .
curl -s localhost:8080/api/v2/system/status | jq
```

### Serve the production ProtoOS build on the API origin

Static hosting is opt-in; without `--web-root`, fake-ct1 remains API-only.
From the repository root, build ProtoOS and launch the simulator with that output:

```bash
npm --prefix client run build:protoOS
(cd server/fake-ct1 && HTTP_PORT=18080 GOWORK=off go run . --web-root ../../client/dist/protoOS)
```

Open `http://localhost:18080/` or a deep link such as `/tanks/1/modules/2`.
The production assets and `/api/v2/*` now share one origin; no Vite dev server,
proxy, or CORS setup is needed. Startup fails if the selected root does not have
a readable, non-empty regular `index.html`. This is a local build-preview fixture,
not the MDK v2 host's artifact verification or packaging/installation flow.

GET/HEAD serve `index.html`, `config.js`, `version.txt`, `favicon.png`, and files
under `/assets/` or `/fonts/`. Extensionless browser routes use the MDK v2
host-compatible route prefixes:
`hashrate`, `efficiency`, `power-usage`, `temperature`, `logs`, `pools`, `systems`,
`tanks`, `modules`, `onboarding`, and `settings`. Unknown routes, missing assets,
directories, and unsafe paths return 404; static writes return 405. API and health
routes keep their existing behavior, and fixture static responses use `no-store`.

For automated desktop/mobile smoke coverage, `cd client && npm run test:e2e:ct1`
builds the production bundle and launches its own simulator on port 18081.

### Run with Docker

```bash
docker build -t fake-ct1 -f server/fake-ct1/Dockerfile .
docker run -p 8080:8080 -p 9095:9095 fake-ct1
```

## Environment variables

| Variable                 | Description                                                                                                                                    | Default           |
| ------------------------ | ---------------------------------------------------------------------------------------------------------------------------------------------- | ----------------- |
| `GRPC_PORT`              | Native gRPC listener port (zero selects an ephemeral port) | `9095` |
| `HTTP_PORT`              | HTTP listener port (zero selects an ephemeral port)                                                                                                                              | `8080`            |
| `CT1_INSTANCE_ID`        | Board serial prefix; defaults to `SERVER_ID` when configured. Set to an existing fixture’s old prefix to retain previously paired inventory. | `SERVER_ID` or random |
| `SERVER_ID`              | Container server id                                                                                                                            | `ct1-sim-<uuid>`  |
| `HOSTNAME`               | Reported hostname                                                                                                                              | `fake-ct1-<uuid>` |
| `CT1_VERSION`            | Reported firmware/OS version                                                                                                                   | `2.0.0`           |
| `CT1_TANKS`              | Number of tanks                                                                                                                                | `6`               |
| `CT1_MODULES_PER_TANK`   | Modules per tank                                                                                                                               | `16`              |
| `CT1_SLOTS_PER_MODULE`   | Hashboard slots per module                                                                                                                     | `3`               |
| `CT1_ASICS_PER_BOARD`    | ASICs reported per board                                                                                                                       | `312`             |
| `CT1_DEMO_MODULE_STATES` | Set to `true` for mixed failures in tank 2, a single-board fault in tank 3, and sleeping/offline modules in tank 4 (tanks 1, 5, and 6 healthy) | `true`            |
| `CT1_UNLOCATED_BOARDS` | Number of existing boards to report under tank/module zero, or `all` for entirely unlocated inventory | `0` |
| `CT1_SEED`               | Positive integer controlling deterministic variation                                                                                           | `1`               |

Shrink the tree for fast tests, e.g. `CT1_TANKS=1 CT1_MODULES_PER_TANK=2`.

### Unlocated inventory

Set `CT1_UNLOCATED_BOARDS=6` to reproduce Frigate's `switch_ports` inventory
group at `tank=0/module=0`, alongside the remaining mapped tanks. Set it to
`all` for entirely unlocated inventory, or `0` (the default) for normal mapping:

```bash
# From the repository root, run the simulator with the ProtoOS dev client:
CT1_UNLOCATED_BOARDS=6 just dev-ct1
CT1_UNLOCATED_BOARDS=all just dev-ct1

# Or run only the simulator:
(cd server/fake-ct1 && CT1_UNLOCATED_BOARDS=6 GOWORK=off go run .)

# Docker: read CT1_UNLOCATED_BOARDS from the repository-root .env and rebuild:
docker compose --env-file .env -f server/docker-compose.yaml up -d --build fake-ct1
```

Compose must recreate the simulator to apply a changed environment setting;
restarting Fleet API does not change the simulator's map. Fleet API and the
Proto plugin must also be rebuilt after changing their telemetry contracts.

The first N existing boards in tank/module/slot order move into the unlocated
group in both REST and gRPC container maps. They retain MACs, serials, health,
and mining state, report `HASHBOARD_SOURCE_INVENTORY`, and omit their physical
board position and module/feed assignments. Their former mapped entries are
omitted; no duplicate boards or replacement empty-slot claims are emitted.
Modules and tanks with no remaining mapped slots are omitted too. The default
six-board scenario leaves all six tanks and 282 mapped boards. Counts larger
than the inventory select every board; negative or malformed values fail startup.

This changes the reported map only. The underlying physical simulation, mining
totals, plant telemetry, controls, and controller identity stay the same. The
Fleet collector should retain mapped readings, report incomplete location
coverage, and log the unlocated board count. Restart without the setting to
restore normal mapping.

The standalone defaults and `just dev-ct1` use the same topology and demo states:

| Tank    | Initial module states                                                                                      |
| ------- | ---------------------------------------------------------------------------------------------------------- |
| 1, 5, 6 | All 16 modules healthy and mining                                                                          |
| 2       | Module 03 sleeping, 06 offline, and 11 faulted; the remaining 13 healthy                                   |
| 3       | Module 08's first board has one over-current error; its other two boards and all other modules keep mining |
| 4       | Module 04 sleeping and module 09 offline; the remaining 14 healthy                                         |

Tank 2 module 11's boards report one, two, and three errors, plus a shared PSU
thermal warning. Tank 3's lighter fault has no PSU thermal warning. Scenarios
only apply when their tank/module/slot coordinates exist in the configured
topology. Global start clears these initial mining states; restarting restores
them.

## Measurement boundaries and provisional assumptions

- Mining status and board power are **DC hashboard power**. Hashrate is measured
  board power divided by roughly 14.9 J/TH; per-ASIC integer hashrates reconcile
  with board hashrate to 0.1 GH/s. ASIC pass rate is separate from ideal hashrate.
- PSU efficiency is `14.9 / 15.5`. Plant IT power, six PDUs, and 96 feeds count
  each CU1's **AC input once**, even though three boards expose the same PSU.
- Facility power is IT AC + pump allowance + actual fan power + controls/AC.
  PUE is facility / IT AC; zero IT load uses a zero sentinel and unavailable PUE
  history. Auxiliary consumption can remain nonzero with mining stopped.
- Nominal demo/all-mining totals are about **54.77 / 57.99 PH/s**, **816 / 864 kW
  board DC**, **848.86 / 898.79 kW IT AC**, and **901.86 / 954.06 kW facility**.
  Measured totals vary slowly around these reference values.
- Fans use `3 kW × (speed / 80%)³`, around 84.2% with the current demo faults and 86%
  with all boards mining. The original 80% reference corresponds to 234 active
  boards; the same load-based curve now reflects the healthier default scenario.
  Disabling the cooler zeros fan speed and power. Manual cooler
  mode uses an explicit 80% demo command; automatic mode follows mining load.
- The 32°C inlet and 7–8°C rise are imposed test conditions. Flow is the required
  thermal flow using 0.8 kg/L density, 2 kJ/kg·K heat capacity, all PSU input heat,
  and a provisional 16 kW pump heat allowance. No hydraulic calibration is claimed.
  The normal rise/flow model is disabled when mining, pump, or cooler is off;
  it does not model transient overheating or circulation in those conditions.
- Live pump speed is a seeded, slowly varying **50 ± 0.5 Hz** demo reading.
  Pump inlet pressure is **190 kPa**; the nominal **100 kPa** pump pressure rise
  scales with squared speed. The cooler accounts for 80% of that pressure drop.
  These are illustrative readings in the API's Hz/Pa fields, not calibrated
  hardware predictions. Disabling the pump reports explicit zero speed and
  differentials while all pressure readings settle to 190 kPa. Cooler fan
  controls do not change pump readings. Pump/pressure history remains unsupported.
- CU1 feed current is omitted pending wiring confirmation. The unresolved ASIC voltage channel stays
  at 385 mV. No new contract fields or hardware identifiers are invented.
- The other-auxiliary allowance is provisionally split into 1 kW controls and
  0.5 kW per enabled AC unit, so those controls affect facility power.
- Power targets clamp to **2,500–3,000 W**, fixed module voltage accepts
  **26–30 V**, and the cooling setpoint accepts **20–55°C**. These are simulator
  bounds, not qualified hardware limits. Below 3 kW, constant nominal efficiency
  is an uncalibrated extrapolation.
- Sleeping/error temperatures decay toward fluid temperature. Disconnected
  board samples freeze, PSU ages increase, and live-status flags become invalid.
  The demo PSU warning remains 80/88/85°C. Run-in remains queued-only.

## Contract fidelity

Native service types come from the pinned [shared MDK module](../../sdk/mdk/README.md).
REST retains its existing DTOs and browser surface. gRPC projects the same DTO
builders into generated types, dropping only older REST diagnostic fields that
are absent from the pinned protobuf contract. The integration test exercises the
real plugin against a full 288-board process and checks HTTP coexistence.

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

The dry-cooler inlet temperature models the mixed return from the two tank outlets. Fleet can display this dedicated plant reading alongside the average of current individual fan speeds; the automatic temperature setpoint is a target, not a measured temperature.
