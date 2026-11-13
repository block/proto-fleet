# Fake CT1

A lightweight, dockerized simulator for a **CT1 container**, implementing the
MDK v2 Connect routes that ProtoOS calls on the device, the REST API behind
Fleet-hosted CT1 views, and native gRPC for the Fleet CT1 plugin.

It is the CT1 counterpart to [`server/fake-proto-rig`](../fake-proto-rig)
(which fakes R2 rigs on the MDK v1 surface). It lets the fleet dev environment
develop and test the CT1 adapter, views, and device bundle **without real
hardware** and **without** Frigate's heavy `task dev:full` stack (TimescaleDB +
`hbsim` + `containersim` + `poolsim` + `maestrosim`).

## What it models

A full container is a **Tank → Module → Slot → Hashboard** tree:

- **96 CU1 modules × 3 hashboards = 288 boards** (6 tanks × 16 modules × 3 slots)
- Per-board hydro telemetry (hashrate, temperatures, voltage, power). Container-map
  health includes `maximumAsicTemperatureC` and its sample timestamp, derived from
  the same simulated ASIC readings as the detail endpoint for Fleet’s ASIC high metric.
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

## Native gRPC and Connect

The native gRPC listener defaults to `:9095` (`GRPC_PORT`; zero selects an
ephemeral port). It implements the System, Hashboard, Mining, Telemetry, Power,
Pool, and RunIn reads and controls that ProtoOS and the CT1 plugin use,
plus the MiningGroup service described in
[Mining groups in Fleet](#mining-groups-in-fleet). They share the REST state and clock.
Whole-container priority pool writes are held in memory and visible through REST,
Connect, and gRPC reads. Enabling mining groups copies the configured pools and
credentials into the Default group and assigns all boards to it. Whole-container
pool writes are refused after activation. Restarting the simulator resets its pools.
Weighted pool allocation and real pool connections are not simulated.

SetContainerControl writes return controller-register confirmation. The dedicated main breaker remains unsupported, matching the REST
fixture. Other generated services and historical RPCs are not enabled. This
development listener is plaintext without authentication; see
[CT1 transport configuration](../../docs/development/ct1-grpc.md) for the
production plugin's TLS/token settings.

The HTTP origin serves the same methods, except the MiningGroup service, as Connect routes
(`/mdk.v2.<Service>/<Method>`), ahead of the `--web-root` static fallback. Native
status codes are kept as Connect codes.

REST, Connect, and native gRPC calls share one request lifecycle: calls from
every transport are serialized, the clock advances first, and writes are
recorded into history afterwards. A REST write is any non-GET request; an RPC
is a write unless its `google.api.http` binding is a GET.

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
| POST | `/api/v2/power/container/setpoints/{name}` | write one of five analog settings; controls and telemetry reads share the same values and units |
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

**Plant settings.** REST, Connect, and native gRPC expose the same five settings:

| Name | Unit | Demo default |
| --- | --- | --- |
| `dry_cooler_auto_temp_c` | °C | 32 |
| `coolant_pump_speed_hz` | Hz | 50 |
| `dry_cooler_manual_speed_percent` | % | 80 |
| `air_conditioner_1_temp_c` | °C | 24 |
| `air_conditioner_2_temp_c` | °C | 24 |

Writes accept finite values within the float32 wire range, matching MDKv2.
The fixture does not enforce or qualify PLC operational limits. AC temperature
settings are stored and read back; room temperature and AC power do not model
thermostat behavior. All settings reset on restart.

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

**Logs and audit trail.** `SystemService/GetLogs` and
`SystemService/ListAuditEvents` are available over Connect and native gRPC
only, with no REST route. They follow Frigate's bounds:

- `GetLogs` sends the newest matching entries from a 1000-entry in-memory ring,
  then follows new entries every two seconds until the client disconnects.
  `tail` defaults to 200, with a maximum of 1000. An unspecified level means
  info. The ring starts full with 45 minutes of deterministic entries, so the
  oldest retained entry moves forward as new entries arrive.
- `ListAuditEvents` returns deterministic events every 47 minutes over the last
  eight days, newest first. Filters are exact match. `range` defaults to the
  last 24 hours and is rejected above seven days. `limit` defaults to 200, with
  a maximum of 1000.

Set `CT1_LOG_SCENARIO` to exercise failure handling:

| Value | Behavior |
| --- | --- |
| `logs-unavailable` | `GetLogs` fails with Unavailable "process log stream is not configured" |
| `logs-interrupted` | `GetLogs` fails with Unavailable halfway through the retained tail |
| `audit-unavailable` | `ListAuditEvents` fails with Unavailable "audit log requires a configured database" |

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

## Mining groups in Fleet

The native gRPC listener supports Fleet's mining-group workflow. New simulator
processes advertise `mining-groups-v1` with groups **disabled**. Use **Enable
mining groups** in Fleet: it automatically stops mining, enables groups, and
resumes with existing pools if the container was running. Activation adopts the
existing hashboards into the stopped `default` group before Fleet resumes it.
Native gRPC and REST global start/stop share the same lifecycle behavior. Module and hashboard **Edit pool**
actions become available after Fleet observes completed activation.

The fixture supports capability and membership reads, group creation, ordered
pool replacement (including explicit empty replacements), exact-board transfers, mining intent, operation receipts, and
empty-group deletion. New groups start stopped. Groups without enabled pools do
not mine or fall back to default pools. Once activated, container start/stop
changes only the default group. Revisions and successful idempotency receipts
are checked; pool credentials never appear in readback. Group pool diagnostics
report configured simulated pools and the connected member MACs, allowing Fleet
to verify selected-board clearing without treating missing boards as cleared.

All group state resets when the simulator restarts. Connected-board transfers
complete synchronously; transfers involving isolated or disconnected boards are
rejected. This fixture does not model HA ownership, physical shutdown proofs,
real Stratum sessions, or per-group connection history. Existing pool
diagnostics remain demonstration data. REST retains its existing API; Fleet uses
the native gRPC group service.

## Hashboard firmware in Fleet and ProtoOS

The native gRPC listener implements `FirmwareService` upload, start, get, list,
and cancel. The Connect handler exposes the chunked begin/write/commit/abort
upload calls and the same update jobs for standalone ProtoOS. Chunked uploads
accept at most 1 MiB per write, skip received bytes on retry, refuse gaps, reset
an image on a digest mismatch, expire after ten idle minutes, and allow two
concurrent sessions. Both upload transports share metadata decoding and caching. Fleet can upload a selected prod/dev/mfgtest archive and target the
container through its existing firmware action or a release channel. Stream
framing, size limits, SHA-256 digests, and embedded image metadata are checked.
The fixture does not verify real signing certificates or hardware compatibility.

Jobs show simulated progress over four seconds. Connected, non-isolated targets
then report the new board version; disconnected or isolated targets report an
individual failure, producing a partially failed job. Controller software stays
unchanged. Reads advance jobs, and all artifacts/jobs reset on restart. This
models one controller; it does not emulate cross-controller HA job routing.

The default demo module states leave two modules disconnected. Their boards
keep stale health, so Fleet reports no container hashboard version, and an
update fails those six boards while flashing the rest. For a clean local run,
recreate the Compose service with demo states off, keeping its address so the
pairing remains valid:

```yaml
# /tmp/ct1-firmware.yaml
services:
  fake-ct1:
    environment:
      CT1_DEMO_MODULE_STATES: "false"
    networks:
      fleet-network:
        ipv4_address: 192.168.2.4 # the paired simulator's current address
```

```bash
docker compose -f server/docker-compose.yaml -f /tmp/ct1-firmware.yaml up -d --no-deps fake-ct1
```

Release archives are the `hashboard-firmware-<version>.zip` assets of the
`btc-mining/hashboard` GitHub releases, for example
`gh release download v1.37.0 -R btc-mining/hashboard -p 'hashboard-firmware-1.37.0.zip'`.
Upload one for Proto CT1 with a hashboard build, then use the container's
**Update firmware** action, or a release channel that covers the container's
site, building, rack, or group.

The plugin's `TestModuleMiningSimulatorLeavesNeighborsUnaffected/firmware`
exercises the native upload-to-inventory round trip with `CT1_SIMULATOR_BIN`
pointing to a built fake-ct1 binary; the Proto Plugin CI job builds one and
sets the variable. Simulator unit tests also cover
partial failures. Synthetic test images are not signed production firmware.

## Environment variables

| Variable                 | Description                                                                                                                                    | Default           |
| ------------------------ | ---------------------------------------------------------------------------------------------------------------------------------------------- | ----------------- |
| `GRPC_PORT`              | Native gRPC listener port (zero selects an ephemeral port) | `9095` |
| `LISTEN_ADDRESS` | Specific local IP shared by the native gRPC and HTTP listeners; omit to bind all interfaces. Invalid or wildcard addresses fail before listening. | unset |
| `HTTP_PORT`              | HTTP listener port (zero selects an ephemeral port)                                                                                                                              | `8080`            |
| `CT1_INSTANCE_ID` | Board serial prefix and default identity namespace; defaults to `SERVER_ID` when configured. Set to an existing fixture’s old prefix to retain board serials. | `SERVER_ID` or random |
| `CT1_IDENTITY_NAMESPACE` | Optional explicit namespace for board MACs, module IDs and PSU serials. Configured `CT1_INSTANCE_ID` or `SERVER_ID` supplies the default; give every controller a unique stable value. | configured controller ID |
| `SERVER_ID`              | Container server id                                                                                                                            | `ct1-sim-<uuid>`  |
| `HOSTNAME`               | Reported hostname                                                                                                                              | `fake-ct1-<uuid>` |
| `CT1_VERSION`            | Reported firmware/OS version                                                                                                                   | `2.0.0`           |
| `CT1_TANKS`              | Number of tanks                                                                                                                                | `6`               |
| `CT1_MODULES_PER_TANK`   | Modules per tank                                                                                                                               | `16`              |
| `CT1_SLOTS_PER_MODULE`   | Hashboard slots per module                                                                                                                     | `3`               |
| `CT1_ASICS_PER_BOARD`    | ASICs reported per board                                                                                                                       | `312`             |
| `CT1_DEMO_MODULE_STATES` | Set to `true` for mixed failures in tank 2, a single-board fault in tank 3, and sleeping/offline modules in tank 4 (tanks 1, 5, and 6 healthy) | `true`            |
| `CT1_DEMO_ALL_BOARD_FAULTS` | Report every current MDK fault cause on every board to exercise telemetry payload limits; use with `CT1_DEMO_MODULE_STATES=false` | `false` |
| `CT1_OCCUPIED_BOARDS` | Populate the first N slots in tank/module/slot order, in `1..capacity`. Remaining physical slots are empty, with configured locations and no hashboard identity or telemetry. Invalid values fail before listening. | all slots |
| `FAKE_MINER_RESPONSE_DELAY` | Nonnegative Go duration before each REST, Connect or native gRPC operation, outside simulation locks and canceled by request deadlines. Set `5ms` for the load campaign. | `0` |
| `FAKE_MINER_METRICS_FILE` | Append cumulative operation counts, errors and latency sums as JSONL every ten seconds and on graceful shutdown; omit to disable file output. | unset |
| `CT1_UNLOCATED_BOARDS` | Number of existing boards to report under tank/module zero, or `all` for entirely unlocated inventory | `0` |
| `CT1_LOG_SCENARIO` | Log and audit failure scenario: `logs-unavailable`, `logs-interrupted` or `audit-unavailable` (see [Logs and audit trail](#implemented-endpoints)) | unset |
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
  mode follows `dry_cooler_manual_speed_percent` (80% by default); automatic mode
  follows mining load. Seeded speed variation is at most 1.25% of the command.
  Demo fan readings stay within 0–100%, independently of the stored setpoint.
- The 32°C inlet and 7–8°C rise are imposed test conditions. Flow is the required
  thermal flow using 0.8 kg/L density, 2 kJ/kg·K heat capacity, all PSU input heat,
  and a provisional 16 kW pump heat allowance. No hydraulic calibration is claimed.
  The normal rise/flow model is disabled when mining, pump, or cooler is off;
  it does not model transient overheating or circulation in those conditions.
- Live pump speed follows `coolant_pump_speed_hz` with seeded variation of at most
  1% (**50 ± 0.5 Hz** by default). Demo readings cannot be negative; a zero command
  produces zero speed. Changing this setting does not change the imposed thermal
  flow, temperature rise, or 16 kW pump allowance.
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
- Power targets clamp to **2,500–3,000 W** and fixed module voltage accepts
  **26–30 V**. These are simulator
  bounds, not qualified hardware limits. Below 3 kW, constant nominal efficiency
  is an uncalibrated extrapolation.
- Sleeping/error temperatures decay toward fluid temperature. Disconnected
  board samples freeze, PSU ages increase, and live-status flags become invalid.
  The demo PSU warning remains 80/88/85°C. Run-in remains queued-only.

## Contract fidelity

Native service types come from the pinned [shared MDK module](../../sdk/mdk/README.md).
REST retains its existing DTOs for Fleet-hosted views. Native gRPC and Connect
project the same DTO builders into generated types, dropping only older REST
diagnostic fields that are absent from the pinned protobuf contract. The integration test exercises the
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

### Multiple controllers and partial inventory

Run one process per CT1 IP, with a distinct stable `SERVER_ID` (or
`CT1_INSTANCE_ID`). Both listeners bind `LISTEN_ADDRESS`, keeping native gRPC
9095 and HTTP 8080 available independently on each controller IP. This models
independent controllers; it does not emulate controller HA.

For the 5,000-board campaign, use seventeen normal 288-board controllers and
one with `CT1_OCCUPIED_BOARDS=104`. Keep the normal six tanks, sixteen modules
per tank and three slots per module on all eighteen processes. The last
controller has 104 accepted boards and 184 confirmed empty slots. Use
`CT1_DEMO_MODULE_STATES=false` for healthy boards and
`FAKE_MINER_RESPONSE_DELAY=5ms` on every campaign process. Existing single
process defaults, including demo failures, remain available.

Board serials retain their existing controller prefix. Board MACs now derive
from the controller namespace and position, fixing collisions between
controllers. A configured controller also namespaces module IDs and PSU
serials; numeric tank/module/slot coordinates remain local physical positions.
Previously paired fixtures may need fresh discovery after their MACs change.
Give duplicate-controller negative controls the same namespace to deliberately
reproduce duplicate identities.

Metrics contain UTC `timestamp`, `requests_total`, `errors_total` and an
`operations` map. Each operation records `requests_total`, `errors_total` and
`latency_sum_ms`, including any configured delay. Keys are fixed registered
REST patterns or RPC methods, without per-board, per-IP or credential labels.
Files grow by one aggregate snapshot every ten seconds; rotate or retain them
through the campaign supervisor. Deadlines that expire during the delay never
dispatch their write and are counted as errors. No automatic retries or write
replay are added.

### HA pair and Frigate server updates

`CT1_HA_PAIR=1` runs two Frigate controllers of one cluster in a single
process, which is how a real CT1 appears to Fleet (one device, two
endpoints). The primary listens on `GRPC_PORT`/`HTTP_PORT` and the peer on the
next ports (`CT1_HA_PEER_GRPC_PORT`/`CT1_HA_PEER_HTTP_PORT`, 0 stays
ephemeral). Both controllers share the physical inventory and report the same
`CT1_HA_CLUSTER_ID` through `SystemService.GetHAStatus`; the standby reports
`HA_ROLE_STANDBY`. Without the variable, `GetHAStatus` stays Unimplemented.

The pair serves the full MDK `ServerUpdateService` over native gRPC only (no
REST or Connect routes): instance-scoped support and Begin/Append/Commit/Abort
staging with exact-offset resume, plus Start/Status/Retry/Cancel that either
controller answers. `x-frigate-server-id` selects the instance when one
connection is used for both. Release statements are verified for real with
ECDSA P-256 against `CT1_UPDATE_TRUSTED_KEY_FILES` and bound to the manifest.
Once a rollout stops Frigate, every service except update Status/Retry/Cancel
answers Unavailable and Status reports `served_by_maintenance=true`, as with
the real maintenance service. Unlike real Frigate, the fake allows remote
triggering by default.

Generate a development signing key:

```bash
openssl ecparam -genkey -name prime256v1 -noout -out release.key
openssl ec -in release.key -pubout -out release.pub
CT1_HA_PAIR=1 CT1_UPDATE_TRUSTED_KEY_FILES=$PWD/release.pub go run .
```

| Variable | Effect |
| --- | --- |
| `CT1_HA_PEER_SERVER_ID` | Second controller ID (default `<SERVER_ID>-b`). |
| `CT1_HA_CLUSTER_ID` | Cluster UUID (default derived from both IDs). |
| `CT1_HA_PRIMARY` | `a` (default) or `b`: which controller is HA primary. |
| `CT1_UPDATE_TRUSTED_KEY_FILES` | Comma-separated PEM public keys; none means `UPDATE_NO_TRUSTED_KEYS`. |
| `CT1_UPDATE_STEP` | Duration of each scripted rollout phase (default `2s`). |
| `CT1_UPDATE_SPOOL_FREE_BYTES` | Advertised free spool space (`UPDATE_INSUFFICIENT_SPACE` when too small). |
| `CT1_UPDATE_FAULT_REMOTE_TRIGGER_DISABLED` | Start refused with `UPDATE_REMOTE_TRIGGER_DISABLED`. |
| `CT1_UPDATE_FAULT_SELF_COORDINATION_UNSUPPORTED` | Support reports no self coordination; staging and Start refuse. |
| `CT1_UPDATE_FAULT_ETCD_UNHEALTHY` | Start refused with Unavailable `UPDATE_ETCD_UNHEALTHY`. |
| `CT1_UPDATE_FAULT_HELPER_UNAVAILABLE` | Start refused with Unavailable `UPDATE_HELPER_UNAVAILABLE`. |
| `CT1_UPDATE_FAULT_OUTSIDE_WINDOW` | Start refused with `UPDATE_OUTSIDE_WINDOW`. |
| `CT1_UPDATE_FAULT_COOLDOWN` | Duration after a success during which Start returns `UPDATE_COOLDOWN`. |
| `CT1_UPDATE_FAULT_MIGRATE_FAILURES` | Number of rollouts that fail at MIGRATING (retryable, stays stopped). |
| `CT1_UPDATE_FAULT_STALE_CONTACT` | Non-driver instance reports `contact_stale` with an old `last_contact`. |
| `CT1_UPDATE_FAULT_DRIVER_REBOOT` | Driver role moves to the peer during INSTALLING. |
