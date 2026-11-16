# CT1 native gRPC transport

The Proto plugin uses Frigate MDK v2 gRPC for CT1 discovery, pairing,
current telemetry, error polling, equipment controls, and container-menu sleep, wake, and power actions. ProtoOS and
Fleet's web proxy continue to use HTTP(S). Poll frequency is unchanged. Concurrent polling, physical-equipment rollups,
and topology persistence are described in [polling measurements](ct1-polling-metrics.md).
Retained historical ingestion remains #179.

## Firmware contract

The shared [MDK module](../../sdk/mdk/README.md) pins Frigate commit
`fc06e9c3f71c8b23d03262387c68a790e4b7fcbd`. This is a source compatibility
target, not a qualified hardware deployment. Both the plugin and fake-ct1 use
the same generated types.

At that revision, `internal/config/config.go` defaults `server.mdk_listen` to
`:9095` and `server.api_listen` to `:8080`. `pkg/api/server.go` registers the
public MDK v2 services, including System, Hashboard, Mining, MiningGroup,
Telemetry, Power, Pool, and RunIn, on a plaintext gRPC server. There is no
registered AuthService/login RPC. The optional
`server.mdk_auth.enabled` / `server.mdk_auth.token_file` configuration gates
native RPCs with a shared bearer credential (`pkg/api/mdkauth/auth.go`).
It is disabled by default. Audit actor metadata does not authenticate callers.

## Endpoints and credentials

SDK `DeviceInfo` and FleetNode descriptors carry `RPCPort`/`RPCTLS` independently
of the existing web `Port`/`URLScheme`, on the same host. These values persist
in `discovered_device.rpc_port` / `rpc_tls`. CT1 requires an explicit RPC port;
discovery saves the scanned port, normally 9095. False selects plaintext.
Verified TLS uses the host as server name, system trust roots, and TLS 1.2 or
newer, with no insecure verification bypass
or plaintext downgrade. It requires a deployment that exposes a TLS endpoint;
the pinned Frigate server itself is plaintext.

Discovery adds 9095 to the existing R2 ports. An explicitly requested scan port
can discover a custom gRPC listener. New CT1s get a web endpoint of
`http://<host>:8080`, with these plugin-process overrides:

| Variable | Meaning |
| --- | --- |
| `CT1_WEB_PORT` | Web port for newly discovered CT1s |
| `CT1_WEB_SCHEME` | `http` or `https` for newly discovered CT1s |
| `CT1_RPC_TLS` | Explicit boolean for gRPC discovery; stored with the device |

Set these on the process that runs the Proto plugin: the server for direct
controllers, or the managing FleetNode.

Authenticated native MDK connections are not supported. The plugin never sends
bearer tokens or saved REST username/password credentials as RPC metadata,
including when probing unknown discovery candidates. A nonempty
`CT1_RPC_TOKEN_FILE` is rejected before creating a connection; the file is never
read. This prevents a shared credential from being disclosed to scanned hosts.

Discovery verifies `SystemService.GetSystemInfo`. Pairing a saved group verifies
its cluster ID and controller IDs through `GetHAStatus` and `GetSystemStatus`.
Pairing does not perform REST login or token refresh. Controllers requiring authentication
return `Unauthenticated`, with no REST fallback. Supporting authenticated
controllers requires a separate design with verified TLS and credentials bound
to explicitly trusted endpoints. The `x-frigate-actor` audit metadata remains
present and is not an authentication credential.

Saved HA groups verify their cluster and controller IDs at pairing and before
each operation. Custom RPC endpoints are persisted by identity on rediscovery;
rediscovery retains an existing CT1's web port and scheme, so a discovery default
cannot replace a custom browser address. The probed host remains authoritative.
Direct rediscovery matches the organization's server-managed container by shared
IP, class, driver, model and manufacturer, independently of HTTP/RPC ports. It
rejects ambiguous matches and excludes FleetNode-owned inventory. FleetNode
discovery reports are admitted against the scanned RPC port; their separate HTTP
port is retained for the ProtoOS proxy. For new CT1s, that HTTP address is a
configured default, not a listener verified by the gRPC discovery call.
FleetNode carries the independent endpoint through pairing, telemetry, commands,
and IP recovery. Recovery scans the configured RPC port and preserves the web
port; it does not infer a changed RPC listener port. Endpoint changes invalidate
the FleetNode telemetry handle fingerprint.

## Endpoint groups and HA failover

The Proto plugin reads `GetSystemInfo` and `GetHAStatus`. A confirmed, durable
`clusterId` becomes the opaque `ct1:<UUID>` device key. The cluster ID identifies
the container; a controller ID identifies the server at each saved address.
MAC addresses, advertised IPs, and hashboard inventory do not define identity.
A new group needs loaded inventory and a confirmed cluster ID. A saved group
can use a retained matching ID during a shared-store outage, with degraded HA.
Conflicting identity, a different cluster, or the wrong controller rejects the
endpoint. Without HA identity, the existing single-address path remains usable.

### Discovery and pairing

Scan both controller addresses before pairing. Each report adds only the address
that Fleet scanned. Fleet does not dial advertised peer addresses. It stores
scanned endpoints under the plugin's key, scoped by organization, driver, and
network owner. The plugin also supplies current member IDs so unpaired discovery
can remove a replaced controller's route without interpreting CT1 state.
A rescan updates an unpaired controller's address while retaining the other
scanned route and custom web settings. Different cluster IDs are different
inventory identities; discovery does not migrate one cluster into another.

CT1 discovery uses an organization lock before device and inventory locks.
Rig-only batches do not take this lock.
Network probes remain concurrent. Pairing checks the saved owner, key, and
routes against the discovery snapshot before writing identity or pairing state.
Fleet Node retries retain the original dispatched snapshot. A changed snapshot
requires a new pairing attempt. An invalid endpoint report does not discard other
valid reports; a database failure rolls back the batch.

Once paired, routes are pinned. Scans cannot add or move a route. Unpair the last
owner, scan both addresses, and pair again to change routes or replace a
controller. Removing the last pairing clears the saved endpoint set and hardware
identity. Removing only a Fleet Node binding retains routes while cloud pairing
still exists. If an unpaired controller first reports no HA identity, a later
scan adds its confirmed group to the same record at that socket.
A report without a group cannot erase an existing group.

A replacement Fleet Node can reclaim one matching group from a revoked node at
its saved addresses. Live network owners remain separate. Public IDs do not
authenticate a route change; initial discovery and pairing need a trusted network.

### Controller selection

The plugin retains one connection per saved controller. Before an operation, it
calls `GetHAStatus` and `GetSystemStatus` on both controllers in parallel.
The shared probe budget is at most five seconds and half the caller's remaining
deadline. The first call checks durable identity; the second supplies the live
role, write authority, and firmware version. It does not repeat `GetSystemInfo`.

Reads prefer a settled primary, then a settled standby, then the unique live
write primary. If primary telemetry collection fails, the same poll can use its
verified settled standby once, within the caller's deadline, with degraded HA.
Writes require one
verified controller with primary role and explicit write authority. Recovery
health does not override that authority. Conflicting write claims reject the
command. Frigate owns promotion. A command stays on the selected connection,
and an uncertain write is never replayed on the peer. All container mutations,
including mining-group commands, use the same device-level lock and selection.

### HA health

Both saved controllers must respond with this live state:

| Field | Primary | Standby |
| --- | --- | --- |
| `haRole` | `HA_ROLE_PRIMARY` | `HA_ROLE_STANDBY` |
| `containerWritePrimary` | `true` | `false` |
| `databaseConnected` | `true` | `true` |
| `haRecovery.state` | `HA_RECOVERY_STATE_STEADY` | `HA_RECOVERY_STATE_STEADY` |

Cached HA reports must agree with this pair. Role and Frigate reachability
observations need the expected source and must be fresh within 15 seconds.
Reported etcd observations, including witness observations, must be reachable
and fresh within 90 seconds. Witnesses need no Frigate role. Response time and
interface inventory age do not refresh role or service observations. Missing,
stale, conflicting, or unknown evidence degrades HA. An explicitly false
optional `dcsHealthy` also degrades HA. Cached diagnostics never authorize writes.

Fleet stores the plugin's HA observation without interpreting CT1 APIs. The
container's Issues column shows **HA degraded** when the observation is absent,
degraded, or older than two minutes. A current healthy observation clears it.
Older samples cannot replace newer samples. If both controllers are unreachable,
ordinary polling reports the outage and the last HA observation becomes stale.

### Rollout and boundaries

- Upgrade both Frigate peers and confirm that they report the same cluster UUID.
  Initial identity can remain unavailable until both peers are ready.
- Upgrade Fleet Server, Fleet Nodes, and the Proto plugin together. Scan both
  addresses before pairing. This unmerged feature has no stored-data migration.
- The HTTP ProtoOS proxy and plugin web URL use the saved browser address.
  HTTP proxy failover is separate. Reading the web URL does not probe controllers.
- The single-address recovery job does not rewrite endpoint groups.
- Authenticated MDK discovery remains unsupported. The optional `TestFrigateHALab`
  sends its lab credential only to loopback test addresses.

## Hashboard firmware updates

CT1 advertises the firmware capability. Fleet sends one native
`FirmwareService` job for the explicit set of occupied board MACs, including
unlocated boards, using the selected prod, dev, or mfgtest build. When some
located tanks are reported, every tank and module position must be reported;
a partial container map refuses the update, and the command retries. Fleet owns
release-channel enforcement; it does not install a Frigate firmware policy.
Normal channel enforcement can enqueue another update when newly adopted boards
report a different version.

The plugin extracts Fleet's selected ZIP, streams image metadata and 1 MiB
chunks to `UploadFirmwareArtifact` within a five-minute deadline, validates the
returned image identities, and calls `StartFirmwareUpdate` once with a unique
task ID. Only the start serializes with other container writes, so breaker,
pool, and mining commands proceed during an upload. Frigate validates
signatures and hardware compatibility, stops mining, flashes and verifies the
boards, and restores mining intent. Force is enabled to support Fleet rollback.

An uncertain start is not replayed: while its task is unfinished or its owners
are unreachable, a new update is refused. An automatic retry of the same Fleet
command, such as after status polling timed out, resumes that command's task
instead of starting another; a new command updates again. A refused start can
be retried and reuses the same task ID, because no job was created. A start that Frigate definitively
rejects (for example, invalid scope or a board already updating) creates no job
and releases the handle immediately. When no owning controller knows a recorded
task, the next command releases it, and status polling reports `failed` rather
than an error, so a command is never retried against a task that is gone.

Completion (`completed`) requires every target on every owning controller to
report success or already-current, with all owner jobs terminal. Missing owners
or targets cannot prove completion; an owner that no longer knows the task has
no running job and counts as a failure for its boards, and a board missing from
its owner's job counts as a failure. Once every existing
owner job is terminal with any failure, the plugin reports `failed` with a failure summary bounded to the
Fleet Node status limit. Frigate has already handled board reboot, so neither
state triggers a CT1 controller reboot, and Fleet does not retry a `failed`
update immediately; rollout re-send pacing or the operator decides when to
retry. Every update targets every occupied board: boards report a version but
not their installed build, so an earlier success cannot prove a board still
runs the artifact. Repeated work stays bounded because a `failed` update is not
retried immediately and rollouts re-send at most three times. The plugin
process retains the task and its owners per container, shared by every handle
for it, including Fleet Node's per-command handles. The task ID is derived
from the command's request ID, which includes the batch UUID, so after a
process restart a retried command finds its task and resumes it instead of
flashing again. The lookup asks the controllers that own boards now, which must
answer, and every other paired controller on a best-effort basis, so a job is
found even if board ownership moved during the restart, while an unreachable
peer never blocks an update. The resumed task is tracked by the targets its
jobs report, not by inventory that changed while the plugin was down. Occupied
boards those jobs never targeted, such as a replaced board or one whose
previous owner could not be reached, count as not updated, so the command
reports `failed` rather than success. Commands without a request ID, such
as those relayed by an older Fleet Node, use a random task ID and are not
recoverable after a restart.

At most two decoded firmware archives are held in memory per process. The
plugin holds its slot through the controller upload; a command that cannot get
a slot before its deadline is refused before any upload and can be retried.

Each poll reads local job lists from the verified controller endpoints and adds
the latest task, owner, progress, and failure evidence to board diagnostics.
Fleet shows this in the existing hashboard details. Peer job visibility requires
both controller addresses to have been discovered and paired. Job reads have a
one-second budget and do not replace the required inventory read.

The controller's software version remains independent. Inventory reports a
hashboard firmware version only when every CT1 tank and module position is
reported and every occupied board, located or not, has the same version and
fresh health. The same complete, fresh evidence with differing versions is
reported as mixed. Anything less (missing, duplicate, or stale board evidence,
a partial map, or only unlocated switch_ports inventory) leaves the version
unknown. Release-channel convergence reads this observation within a
two-minute freshness limit together with artifact provenance: a uniform version
can verify a container, a mixed one is drift that enforcement corrects, and an
unknown one neither verifies nor reopens a converged container, so it never
triggers a reflash on its own.

## Collection and controls

Each active CT1 owns a reusable gRPC connection per saved endpoint, closed by
`Device.Close`.
Temporary discovery/pairing connections are closed on success and failure.
Client construction is lazy; bounded discovery or HA/status calls prove reachability.
The transport reconnects after interruptions. No CT1 REST fallback is enabled.

Collection requires `HashboardService.GetContainerMap`. Mining status, plant/PDU/fan
health and controls run concurrently as supplemental calls with individual budgets.
The outer collection deadline is ten seconds; supplements reserve 500 ms for
returning the core result. Cancellation and gRPC status errors are retained.
Structured warnings identify unavailable supplements and rejected slot data.

The maximum received RPC message is 8 MiB. No gRPC message compression is enabled.
Controls retain the existing mutex/sequence: validate desired state, read live
support and known state, write once, then require authoritative readback. General
controls require a confirmed controller-register observation with the requested
value. The pinned breaker producer returns success only after authoritative PLC
status readback, so its false scalar is a valid confirmed-open state. Transport
failures report unconfirmed state; configured retry policies are disabled and no
application retry/hedging replays writes. gRPC may transparently retry only calls
known not to have reached application processing.

Board identification calls `HashboardService.SetIdentifyLED` (blink or off) with
one board MAC per request. MDK returns only an aggregate `boards_updated` count
and has no LED readback, so the plugin first checks the slot's current MAC and
module identity in `GetContainerMap` and succeeds only when exactly one board was
updated. Fleet queues one command per confirmed board installation so each board
reports its own outcome. Fleet Node routing requires command protocol V10.
Frigate owns the blink window (15 minutes). Fleet command results do not carry
MDK's `expires_at`, so the client shows an approximate end time counted from
when each board's result was recorded.

## Protobuf presence and diagnostic fidelity

Normalization reads typed protobuf values. Diagnostic JSON is generated separately
with the pinned schema's field names and units. Known diagnostic fields, nested
issues and shutdown evidence are retained; future unknown protobuf fields cannot
be represented by name until the pin is updated.

- Slot arrays are not physical order. Explicit hashboard/configured locations
  determine positions; absent, conflicting, duplicate or out-of-range placement
  remains unavailable. An explicit configured empty slot remains empty.
- Optional measurements preserve absent versus zero. Board and legacy container hashrate have no
  presence/validity field in this contract, so ambiguous scalar zero remains
  unavailable. A present PSU electrical message is defined upstream as a complete
  snapshot, including zero power/sample age. Legacy mining power is used only
  with complete shared-assignment coverage from `current_measurements`.
- Unknown enums do not become a known healthy state. Source timestamp freshness,
  invalid diagnostic rejection, identity conflicts, shared-PSU deduplication,
  ASIC temperature, and module state rules remain in the existing normalizer.
- Container hashrate still comes from mining status; facility/plant power still
  takes precedence over the mining fallback; container efficiency still uses
  that final selection. These definitions are deliberately not replaced by the
  planned polling/storage corrections.

## Error polling

The CT1 plugin advertises `get_errors` and implements `GetErrors` from the two
reads telemetry already makes: `HashboardService.GetContainerMap` (required) and
`TelemetryService.GetContainerHealth` (a bounded supplement). Errors reach
Fleet's diagnostics store the same way on direct and Fleet Node paths. The
[module issue evidence](../architecture/container-inventory.md#current-module-issue-evidence)
shown in container inventory uses the same classification.

| Source | Fleet component | Component ID | Code and severity |
| --- | --- | --- | --- |
| Board `errors` and `warnings`; rail and clock flags while mining | Hashboard | `mac:<board MAC>` | Same canonical codes as module issues |
| PSU thermal state; board-reported power-supply causes | PSU | `tank:<n>/module:<n>` | Same canonical codes as module issues |
| The same, for a board without a location | Hashboard or PSU | `mac:<board MAC>` | Same canonical codes as module issues |
| Active plant alarm | Unspecified | `plant-alarm:<alarm ID>` | `VENDOR_ERROR_UNMAPPED`; critical, major (error), minor (warning or unrated), or info |

Component IDs are identities, not slot numbers. The summary and vendor attributes
carry the current tank, module, and board position. The firmware cause or alarm ID
is the vendor code. Fleet stores one error per code and component, so causes that
share both are reported as one error listing each cause, at the most severe
level. `lastShutdownError` is history and is never reported. Plant fault codes
and per-fan alarm flags are not reported.

### Evidence and retention

Fleet closes an error once polls stop reporting it. A report therefore
distinguishes a source that cleared an error from a source that said nothing.
A source is a board's own health sample, a module's PSU thermal state, or the
plant alarm list. Each source that asserts an error holds its own claim: a
severity and the sample that made it. The error is reported until every source
that asserted it supplies a complete reading without it.

- Frigate attaches board health to the map only while it is under two minutes
  old. A board without current health is silent: errors it asserted stay
  reported, and it adds none. The plugin applies the same two-minute rule to the
  timestamps it receives.
- A source's readings are ordered by their own sample time, not by when they
  were collected. A later read can return an earlier sample, for example from a
  standby that lags the primary. A sample older than one already applied
  neither raises nor clears an error. The order is remembered while such a
  sample could still be fresh, whether or not the source holds an error.
- PSU thermal state is shared by a module's boards, so any reporting board can
  clear it. A power-supply cause stays with the board that reported it.
- The reported severity is the most severe claim not yet withdrawn. A PSU
  thermal warning does not downgrade an over-temperature fault that a silent
  board still holds.
- The plant alarm list is authoritative only in a fresh snapshot that carries
  plant telemetry. A failed, stale, or unsupported `GetContainerHealth` read
  leaves plant alarms as they were; board and PSU errors are still reported.
  Before its first plant poll, Frigate serves saved alarm flags without the
  list. The plugin then reports one aggregate plant alarm and keeps the alarms
  it already knew.
- A report that had to omit causes (more than 64 per module, or the container
  issue budget) is not a complete reading. It raises the causes it kept and
  clears nothing.
- An error that no source asserted in the latest evidence is carried. It has
  the vendor attributes `ct1_evidence=retained` and `ct1_last_confirmed_at`,
  a note with the same time in its summary, and no last-seen time of its own.
  These values do not change while it is carried.
- A board that the map no longer lists has been removed, and its errors are
  dropped, only when the map came from the container's only saved controller or
  from an HA pair's one settled write primary. A map read from any other
  controller keeps the errors of boards it does not list: a standby, a primary
  that is not settled, or either controller while both claim write authority.
  During an HA transition such a controller's board catalog can be behind.
  Errors are also kept when the map lists no boards at all, or lists a board the
  plugin rejected without a usable identity.
- Removal does not wait for a complete topology, unlike inventory. At the pinned
  Frigate commit a silent board stays in the map: Frigate lists every board that
  has a location, and deletes a location only when it retires the board
  (`location_retention_days`, seven by default, with no owner and no discovery)
  or when another board supersedes it at its switch port. Tanks and modules are
  derived from the listed boards, an occupied slot always carries its board, and
  every board carries a MAC. A board missing from the authoritative map is
  therefore one Frigate has retired. Requiring a complete topology would instead
  keep its errors open wherever any slot is unknown, which includes every empty
  slot outside `switch_ports` mode.
- A failed map read fails the poll. It never produces an empty report.

Claims and sample order live in the plugin process, shared by every handle for
a container, including Fleet Node's per-command handles. They do not survive a
plugin restart: after one, carried errors are no longer reported, and Fleet
closes them after its staleness threshold.

Fleet polls errors right after telemetry. Error polling reuses the outcome of a
telemetry poll that ended in the last ten seconds, so the poll cycle reads the
map once. If that poll read the container, its observation is the report. If
it could not, the error poll fails with the same error and does not probe or
read the controllers again. A poll the caller abandoned is not reused.
Otherwise error polling selects a controller like telemetry does, including one
retry on the verified standby, and reads only the map and plant health.

These times come from the host's wall clock. If it steps backwards, the plugin
does not wait for it to catch up: an outcome dated after now is not reused, and
evidence older than the last by more than ten seconds is applied. Controller
samples are then dated ahead of the host and cannot be judged fresh, so errors
are carried until the clocks agree.

Fleet Node results carry at most 512 reports. Reports are ordered most severe
first; when a result is truncated, Fleet keeps the device's open errors.

## Validation and performance boundary

`server/fake-ct1` exposes both transports over the same state/clock. The plugin
integration test builds and launches it with independent ephemeral ports, then
checks all 288 occupied boards, collection, pairing and the HTTP system endpoint.
Real gRPC tests cover restart/reconnect, TLS certificate rejection, credential-free
discovery, rejected token configuration, status/cancellation/deadlines, receive
limits and ambiguous writes.
Real Postgres tests cover endpoint persistence and identity-based rediscovery.

On an Apple M4 Max, a 96-module/288-board simulator map measured 110,248 protobuf
bytes versus 382,898 equivalent protojson bytes, uncompressed. The reproducible
`BenchmarkContainerMapCodec` measured the following single local run; timings
vary and exclude network, normalization, diagnostic JSON generation and hardware:

| Codec operation | Time/op | Bytes allocated/op | Allocations/op |
| --- | ---: | ---: | ---: |
| Protobuf encode | 197 µs | 114,692 | 1 |
| Protobuf decode | 310 µs | 455,321 | 10,506 |
| Protojson encode | 1,798 µs | 2,554,702 | 15,552 |
| Protojson decode | 3,334 µs | 964,559 | 51,223 |

Run from `server/fake-ct1` with `GOWORK=off go test -run
TestGRPCSharesContainerState -bench BenchmarkContainerMapCodec -benchmem -v`.
This measures equivalent wire codecs, not the old plugin's JSON decoder or an
end-to-end speedup. Hardware latency, timeout rates and controller CPU/load at
the existing cadence remain rollout measurements.

## Local development upgrades

Rebuild/recreate the running `fake-ct1` service when adopting this transport:
`docker compose -f server/docker-compose.yaml up -d --build --no-deps fake-ct1`.
An older simulator may still pass its HTTP health check while having no listener
on 9095. The collector logs `Unavailable` / `connection refused`, and stale
module observations then appear unknown. If polling has already stopped after
repeated failures, restart the local Fleet API after the simulator is ready.

The development configuration uses `PROTO_MINER_PORT=0` so discovery includes
both R2 HTTP(S) and CT1 gRPC. A fixed `8080` override excludes native CT1 discovery.
For shared or custom Compose projects, use the same project/override files that
started the simulator, and rebuild only that service; retain its controller ID
and network address. REST/ProtoOS continues to use its existing HTTP listener.

Board serials now use a stable prefix derived from `SERVER_ID`. When upgrading
an already-paired legacy simulator, set `CT1_INSTANCE_ID` to its original random
prefix (for example, `eb22b1c8` for `CT1-eb22b1c8-0001`) to preserve existing board
identities. Keep this override across subsequent recreates.

## Rollout and rollback

Apply the additive endpoint migration and deploy compatible server, FleetNode and
Proto plugin builds together. Before rollout, validate one direct and one
FleetNode-managed controller running the selected firmware contract: SystemInfo
identity, the full map/diagnostics, supplemental fields, both web links/proxying,
reconnect, configured TLS, and approved control readback. Authenticated controllers
remain outside the supported rollout configuration. Compare bytes, latency,
timeouts and controller load against the previous build at the same cadence. No hardware validation is implied by simulator success.

Monitor failed required reads, supplemental gRPC status warnings, unknown slots,
and unconfirmed writes during the canary. Roll back application/plugin builds
before removing the additive schema. The endpoint-group down migration removes
saved endpoint lists and HA observations; it does not remove hashboard telemetry.
Both peers must support the new HA API before a new group can be established.

## Frigate software updates

The CT1 plugin advertises the `frigate_server_update` capability and implements
`sdk.DeviceFrigateUpdater` over the controller's MDK `ServerUpdateService`
(`sdk/mdk/proto/mdk/v2/server_update.proto`, pinned with the other MDK v2
sources; see `sdk/mdk/README.md`). The plugin boundary carries it as one
`FrigateServerUpdate` RPC with a oneof over nine operations: support, stage
begin/append/commit/abort, and rollout start/status/retry/cancel. Fleet
coordinates nothing itself: it stages the same signed package on both
controllers and starts a self-coordinated rollout that the Frigate root helpers
drive through etcd.

- Transport and auth: native gRPC to the controller RPC port, reusing the
  per-endpoint clients. There is no login or bearer token, so controllers that
  require `mdk_auth` are not supported. FleetNode-owned controllers are not
  supported either: `RemoteFleetNodeMiner` answers every update call with
  FailedPrecondition.
- Instance routing: one HA pair is one Fleet device. `DeviceEndpoint.ID` is the
  Frigate instance ID, and the plugin sends staging calls to the connection of
  the instance named in the call. A single-node controller uses its instance
  ID from discovery, so its device can only be built while Frigate is running
  (`NewDevice` without endpoints verifies identity); a device that was already
  built keeps working, including status through the maintenance service, while
  Frigate is stopped. This limitation is deliberate and applies only to
  single-node CT1; HA pairs are built from stored endpoints. Support returns
  one entry per controller; an unreachable controller or one answering as the
  wrong instance has `Err` set instead of failing the whole call, and
  `ErrReason` carries the Frigate refusal reason when the failure was a refusal.
  `ErrReason` crosses the plugin boundary in-band as an `[UPDATE_REASON] `
  prefix on the wire `error` string (the proto has no dedicated field yet) and
  is decoded back by the SDK.
- Staging: `Begin` returns the manifest fingerprint and the next offset, so an
  interrupted upload resumes from that offset. `Append` sends at most 1 MiB
  (`FrigateStageChunkBytes`) at the exact next offset; repeating an
  acknowledged chunk is accepted without advancing. `Commit` verifies the
  digest and size. The manifest carries `coordination = SELF`, the release
  statement and signature, and the Fleet user as `actor`; no maintenance token
  or Flotilla connection fields are ever set.
- Start: before sending, the plugin reads support from both controllers and
  fails clearly unless the instance IDs equal `expected_instance_ids`, each
  `cluster_id` matches the device key (`ct1:<cluster uuid>`), and each
  controller reports `remote_trigger_allowed` and
  `self_coordination_supported`. Those two cases surface as the Frigate
  refusal reasons `UPDATE_REMOTE_TRIGGER_DISABLED` and
  `UPDATE_SELF_COORDINATION_UNSUPPORTED`; identity and cluster mismatches use
  the Fleet-synthesized reason `FLEET_PAIR_MISMATCH`. The cluster check never
  fails open: when the device key carries no cluster, both support responses
  must report the same non-empty `cluster_id`. A controller that cannot be
  queried fails the pre-check with that failure's own gRPC code. Start goes to
  one controller (Frigate routes it to the control leader).
- Idempotency: Start, Retry and Cancel require a non-empty idempotency key (at
  most 128 printable ASCII characters) and a canonical lowercase UUID job ID,
  checked by SDK validation and again by the device. Fall-through to the other
  controller is only safe because Frigate replays the stored result for a
  repeated key.
- Controller choice and fall-through: attempts go to controllers whose gRPC
  channel is READY first, then IDLE, then the rest, keeping the last controller
  that answered first within each class. Calls use `WaitForReady(false)`. A
  Frigate refusal is final and returned intact on every call, whatever its gRPC
  code (an `UPDATE_ETCD_UNHEALTHY` refusal with code Unavailable is not a
  transport failure). Unavailable falls through to the other controller. A
  deadline falls through for Status; for Start, Retry and Cancel it falls
  through only when the channel was not READY when the call began (nothing was
  sent), and is otherwise returned as is, because the request may have been
  applied. The caller reconciles an ambiguous mutation by reading
  `FrigateUpdateStatus` and, if still needed, repeating it with the same key.
  When every controller fails with a transport error the last error's gRPC code
  is returned with each controller's detail in the message.
- Status, retry and cancel try controllers in that order. They are never gated
  on HA probing: while
  Frigate is stopped for the install, the maintenance service on the same RPC
  port answers them with `served_by_maintenance=true`, and the result's
  `AnsweredBy` names the controller. Other update calls are Unavailable until
  Frigate is back.
- Refusals: Frigate sends `google.rpc.ErrorInfo` in the `mdk.v2.frigate` domain
  with an `UPDATE_*` reason (for example `UPDATE_NOT_STAGED`,
  `UPDATE_SIGNATURE_INVALID`, `UPDATE_ACTIVE_JOB`, `UPDATE_NOT_RETRYABLE`).
  The plugin converts them to `*sdk.FrigateRefusal{Reason, Message, Code}`,
  which keeps the reason and gRPC code across the plugin boundary; use
  `sdk.AsFrigateRefusal(err)`. Retry and cancel carry idempotency keys; Frigate
  replays the original result for a repeated key.
- Audit attribution: update calls send `x-frigate-actor: proto-fleet:<user>`
  (`sdk.FrigateAuditActor`) instead of the fixed `proto-fleet` value, and the
  manifest `actor` field carries the same sanitized, bounded value so body and
  header agree. This is attribution only, not authentication.
- Validation: the SDK rejects malformed requests before they reach the plugin
  (self coordination only, 1 to 8 unique instance IDs including the target,
  64-character lowercase hex digests and fingerprints, package size 1 byte to
  1 GiB, statement at most 4 KiB, signature at most 512 bytes). The device also
  refuses a manifest whose instance differs from the controller it is sent to,
  before any byte is sent; the simulator rejects the same case with
  `UPDATE_MANIFEST_INVALID`, like Frigate.
- Update calls bypass the Fleet write gate and command queue. They are
  rollout orchestration, not device writes, and the caller owns idempotency.

`server/fake-ct1` serves this API for an HA pair with `CT1_HA_PAIR=1`,
including signature verification, refusals, a scripted rollout with maintenance
mode, and fault knobs; see its README. The plugin tests in
`plugin/proto/internal/ct1/frigate_update_simulator_test.go` build and run it
(or use `CT1_SIMULATOR_BIN`).

## Container menu commands

Fleet's container menu uses typed Fleet RPCs for current mining status and power
settings, and queued commands for sleep, wake, and power changes. Direct controllers
execute through the Proto plugin; node-owned controllers execute the same native
MDK operations on their assigned Fleet Node. These commands require node protocol
V8. The embedded HTTP relay remains limited to Proto rigs; existing direct CT1
HTTP views remain supported.

The plugin verifies the saved controller identity and uses the existing HA
controller-selection policy before dispatch. It does not replay a mutation after
an uncertain result or fail over after sending it. Fleet waits for the queued
command result before reporting acknowledgement, then refreshes applied power
without resending the write if that read fails. Power mode and optional balance
and disconnect settings retain their presence and values.

Global start/stop retains the firmware semantics: with mining groups enabled it
changes only the default group; isolated boards retain their individual state.
