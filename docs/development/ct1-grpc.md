# CT1 native gRPC transport

The Proto plugin uses Frigate MDK v2 gRPC for CT1 discovery, pairing,
current telemetry, error polling, equipment controls, and container-menu sleep, wake, and power actions. ProtoOS and
Fleet's web proxy continue to use HTTP(S). Poll frequency is unchanged. Concurrent polling, physical-equipment rollups,
and topology persistence are described in [polling measurements](ct1-polling-metrics.md).
Retained historical ingestion remains #179.

## Firmware contract

The shared [MDK module](../../sdk/mdk/README.md) pins Frigate commit
`41756dc6e26ce0dccc594d7794f1ca3a107d4192`. This is a source compatibility
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

Controllers with `server.mdk_auth` enabled need their bearer token in Fleet.
Fleet stores it per controller in the existing encrypted device credentials, as
a credential with an empty username and the token as its password. Both
controllers in an HA pair use the same token. The plugin sends
`authorization: Bearer <token>` on every unary and streaming RPC to the paired
endpoints only. It is never sent while probing discovery candidates. The plugin
rejects a token that is not RFC 6750 `b64token` syntax, or that is longer than
4096 characters, without echoing it. A credential with a username is a saved
REST login and is never sent as RPC metadata. A nonempty `CT1_RPC_TOKEN_FILE`
is still rejected before a connection is created and the file is never read: a
process-wide token would reach every scanned host.

A controller's `Unauthenticated` response becomes the SDK authentication-failed
error during pairing, handle creation, `DescribeDevice`, and status polling.
Fleet then shows the container as needing authentication rather than offline.
Pairing a protected controller without a token puts it in the same state. To
rotate, replace the token on both controllers, then re-authenticate the device
in Fleet with the new token. That re-runs pairing with the new credential and
keeps the device's identity and history. The `x-frigate-actor` audit metadata
remains present and is not an authentication credential.

When `CT1_RPC_TLS` is false, the token travels in plaintext, as with Frigate's
own clients. Put protected controllers behind TLS termination or on an
encrypted private network.

`mdk_auth` also gates `GetSystemInfo`, and discovery sends no token. At an
explicit CT1 RPC port (9095, or the plugin's configured port), a controller that
answers `Unauthenticated` is listed as a CT1 container at the scanned address
with no controller ID, firmware version, or HA group. Opportunistic probes of
web ports never list one, because any authenticated gRPC service would match.
Pairing without a token then needs authentication. Pairing with the token reads
and checks the controller ID. Rescanning a paired protected controller keeps the
controller ID that pairing verified. An unpaired listing keeps no controller ID,
so a replacement controller at the same address pairs under its own ID. If a
different controller answers at a paired device's address, pairing fails with
"CT1 controller identity mismatch" and asks the operator to remove the existing
device, then discover and pair the new controller.

Protected HA pairs cannot be paired yet. Pairing with the token returns an HA
group that discovery did not record, and only discovery saves endpoint groups.
Fleet reports that it cannot pair token-protected HA pairs yet, and saves
nothing. A rescan of an HA pair that was paired before `mdk_auth` was enabled
reports that it could not save the discovered device; the paired group is
unchanged.

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
- Discovery never sends a controller token; protected controllers are listed
  without an identity, as described above. The optional `TestFrigateHALab`
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

## PSU firmware updates

The boards of a CT1 module share one PSU. Frigate's `PSUFirmwareService`
updates it, one module per job, from a signed PSUCAP v1 capsule. Fleet stores a
capsule as CT1 firmware with the `psu` component (see the
[rollout API](firmware-rollout-api.md#ct1-psu-capsules)) and sends it with the
same firmware command as a hashboard archive. The plugin tells the two apart by
the file's first bytes. A release channel assigns it as the model's `psu`
component.

Frigate keeps capsules, jobs, and job history on the controller that owns a
module's boards. It picks each job's task ID, keeps jobs in memory only, and
runs one module job at a time per controller. The plugin therefore addresses
each owning controller directly instead of the write primary. It reads the
container map, refuses a partial map as it does for hashboards, and groups
occupied, located boards by module ID. Frigate addresses a module by that ID,
so a position whose occupied boards report none fails as a module of its own
instead of being left out, and the command is refused when no board reports
one. Before it changes anything, every owning controller must be paired with
Fleet and answer, and its job list must be readable. A controller gets the
capsule only when it has a module to start.

Each module is planned from its newest job on its controller:

| Newest job on the owning controller | What the plugin does |
| --- | --- |
| None, cancelled, or succeeded with another capsule | Updates the module |
| Succeeded with this capsule | Leaves the module alone if its PSU still reports what that job installed (below); otherwise updates the module |
| Running with this capsule | Tracks that job instead of starting another |
| Running with another capsule | Refuses the whole command |
| Failed | Reads PSU firmware status from the module's boards, then decides (below) |

Frigate keeps a successful job, and the boards keep their record of the
capsule, after a PSU is replaced, so neither shows which PSU is installed now.
The plugin therefore reads PSU firmware status from the board that led the
job. The module counts as updated only when that board still leads the PSU,
reports the capsule as applied, and reports the same PSU application and
bootloader revisions the job recorded when it finished. Otherwise the module
gets a new job, and Frigate ends that job without applying if the PSU already
runs the capsule.

After a failed job the board that leads the PSU still journals the update, and
that record outlives Frigate's job history. If it reports that the PSU needs
recovery with this capsule, the plugin starts the job with `recover`. If
recovery needs another capsule, or an earlier apply is still running, the
module fails without a job being started. Otherwise the module is updated
again. When no board answers, the failed job's last leader status decides.

The command is therefore safe to send again: it finishes what is left, recovers
what the capsule left half programmed, and leaves updated modules alone. `force`
is never set, so Frigate ends a job without applying when the PSU already runs
the capsule.

The plugin starts one module per controller and starts the next only after that
job ended. It reads the jobs of every controller before it starts a module on
any of them, so a failure on one controller stops the others in the same pass.
No further module is started once:

- a module is left out of mining, because its PSU needs recovery or the outcome
  of its apply is unresolved;
- a job failed after its apply may have reached the PSU, the leader's last
  status is not final, and Frigate restarted no board of the module. Frigate
  holds such a module out of mining but records that only on boards that were
  mining before the job, so on a module that was not mining this is the only
  sign that the PSU may still be programming. A board that Frigate did restart
  shows that it resolved the apply as never started, and the update goes on;
- board firmware rejects the capsule itself (format, digest, signature, or
  trust), which every other module would do too;
- a controller no longer has a running job, which happens when Frigate restarts
  and leaves the PSU's state unknown;
- a controller answers as a different controller.

Other failures do not stop the update. A capsule for another PSU revision is
refused by that module's board before anything changes, and a start that
Frigate refuses creates no job; in both cases the next module is started. If
Frigate refuses a plain job because the PSU needs recovery, which it does when
a restart erased the failed job, the plugin reads the boards' status and
retries once as a recovery of this capsule. A start whose answer was lost is
looked up in the controller's job list before it is sent again.

Only the firmware command starts a module. Fleet admits that command as a
device write and checks the admission again each time it sends it. Fleet does
not admit a status poll, so a poll only reads the controllers' jobs and never
starts, recovers, or repeats one. When a controller is ready for its next
module the poll reports `continue`, and Fleet sends the same command again,
with the same capsule and request ID, which starts that module. At most one
send follows each poll. A command that lost write admission, because its queue
message was reaped or the container's write fence was reserved, is refused at
that send and starts nothing further.

An update whose command stopped therefore stops after its running jobs end. An
automatic retry of the same command resumes it. Any other firmware command is
refused while the update was polled within the last minute; after that a new
PSU command replaces the stopped update and takes over its running jobs. PSU
and hashboard updates use the same boards: a PSU update is refused while a
hashboard job is unfinished or its controllers are unreachable, and a hashboard
update is refused while a PSU update is being polled.

The command reports the percentage of modules that finished, updated or
failed, with `installing` while a job runs and with `continue` when a module is
ready to start. It reports `completed` when every module runs the capsule, and
`failed` when the update ended with failed or unattempted modules. The failure summary names each module with Frigate's
reason, bounded to the Fleet Node status limit. Neither result reboots the
controller.

Each poll also reads the PSU job lists of the verified controllers, within one
second, and attaches a module's newest job to the metadata of each of its
boards as `psuFirmwareUpdate`: task, controller, capsule, state, the leader's
phase, progress, and whether the PSU needs recovery or the module was left out
of mining. Fleet shows it in the module's PSU details and flags a PSU that
needs recovery on its card. The PSU firmware version in those details is the
`psuIdentity.firmwareVersion` Frigate reports; BOCO PSUs report
`app=<revision>;bl=<revision>`.

Limits:

- **Duration.** One module updates at a time per controller, so a full
  container takes hours. A firmware command's timeout (15 minutes by default)
  restarts whenever the device reports a higher install percentage, and each
  module that finishes, updated or failed, raises it, so the command stays
  open while modules keep finishing. It times out when no module finishes
  within that time. The queue
  then retries the command, which resumes the update, up to five attempts. A
  Fleet restart fails the command and the update stops. In both cases sending
  the capsule again continues it.
- **Version.** A capsule does not name the firmware version it installs. The
  version stored with the file is the uploader's label, and the plugin judges
  an update by Frigate's job results. It compares the revisions a PSU reports
  only with the revisions that PSU's own successful job recorded, to tell
  whether the PSU that job updated is still installed.
- **Split modules.** Frigate does not update a module whose boards two
  controllers own. The plugin fails such a module without starting a job.
- **Capsule cache.** Frigate caches at most 32 capsules per controller and has
  no call to delete one.
- **Release channels.** The rollout API assigns a capsule to a channel as the
  `psu` component of Proto CT1. It verifies a container by the PSU firmware
  version the container reports. The capsule's version must therefore be the
  version its PSUs report after the update; see the
  [rollout API](firmware-rollout-api.md#ct1-psu-capsules).
- **Validation.** This has run against `server/fake-ct1` only, not against
  Frigate or CT1 hardware.

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

## Plant alarm mute and reset

`PowerService.MuteContainerAlarm` and `ResetContainerAlarm` take an empty request
and return an empty response. Each pulses a PLC command bit once, so success means
the PLC accepted the command and carries no readback. The plugin advertises
`CapabilityContainerAlarm` and implements `RunContainerAlarmAction` with action
`mute` or `reset`. Fleet exposes it as `ContainerService.RunPlantAlarmAction`; the
outcome is read through `GetEquipmentCommandResult`.

Capability comes from `GetContainerControls.alarm_mute_supported` and
`alarm_reset_supported`. Mute also requires that
`GetContainerHealth.alarm_mute_supported` is not false; an absent value (older
releases) keeps mute available. Reset depends only on the controls flag. Production
CT1 supports mute and not reset, because its PLC clears alarm bits only when their
conditions clear, so Fleet rejects reset admission when the flag is false or absent.
Admission also needs a controls observation that is fresh (two minutes) and not
stale, and takes the same per-container pending-command lock as
`SetContainerControl`, so an alarm action and an equipment control cannot be queued
together in either order.

The plugin sends at most one controller write and never retries it. Controller
status codes InvalidArgument, FailedPrecondition, Unimplemented and the HA
not-primary refusal mean nothing was sent: they are reported as a refused write and
stored as "was not sent". Any other failure (DeadlineExceeded, Internal,
Unavailable, Unknown, transport loss) may have reached the PLC. It carries unknown
write evidence, quarantines the device-write fence, and is stored as "outcome
unconfirmed". Both outcomes are terminal in the command queue. Fleet Node routing
requires command protocol V14; older nodes are refused before enqueue.

Plant readback is a later observation, never the write response. The container
projection reports `plant.alarm_active`, `critical_alarm_active` and `buzzer_active`
as optional values: an absent field means the controller did not report it, which
is not the same as clear. Per-alarm detail comes from the existing errors path.

Module power-feed inspection is read-only. The plugin reads
`PowerService.ListPowerFeeds`, `PowerService.GetActivePowerRecoveryCycle` and
`HashboardService.GetContainerMap` from a verified controller and derives the
module's feed from board slot evidence (slot, observed and expected feed,
switch-port wiring status and configured location). The mapping is
`wiring_verified` or `configured` only when the module position is unique, its
identity matches the one Fleet confirmed, every occupied slot names the same
single feed, wiring is not contradicted and no other module claims that feed;
anything else is `ambiguous` or `unavailable` with machine-readable reasons. An
occupied slot that names no feed while others do is `feed_evidence_incomplete`.
Feed records name no modules, so sharing is known only from the map. A map
that omits a tank or module position, leaves a slot unreported, lists an
unlocated board, or has an occupied slot elsewhere that names no feed is
`topology_incomplete`: the mapping is `ambiguous` and controls stay off,
because hardware the map does not show could share the feed.
Feed readings are shown only from a ready snapshot with exactly one record for
the feed; an unreadable recovery cycle stays unknown rather than "none". Fleet
reads through the `container_power_feed` capability and Fleet Node routing
requires command protocol V13. Nodes and plugins reject unknown actions and
request fields, so a new action or request field needs a new protocol version;
servers ignore unknown result fields. This path is never polled: each read
holds the container's binding lock until the controller answers, so Fleet reads
once when the module page opens and again only when the operator refreshes.
Inspection uses the Fleet Node's deferrable-read capacity, so it never takes
the command slots or device handles reserved for operator commands.
fake-ct1 reports every simulated outlet as enabled with no active recovery
cycle.

A board's live ASIC readings reach Fleet over the same stream whether the
container is direct or behind a Fleet Node. Through a Fleet Node, the stream is
one `stream_board_asic_readings` command. The node sends each controller frame
as a `CommandFrame`: the command id and an opaque payload of at most 512 KiB,
here a `BoardASICStreamFrame`. One ack follows, whose payload names how the
stream ended (controller ended, cancelled, board changed, unsupported,
unavailable or at capacity). Controller error text stays in the node log.
fleetd relays payloads without reading them. The caller decodes and validates
each one, and an invalid payload ends the stream as unavailable. fleetd relays
only the newest frame to a slow viewer, carrying the layout of a skipped frame
onto it.

When the viewer leaves, fleetd sends `ControlCancel` with the command id. A
frame for a command fleetd no longer tracks gets another cancel, and the node
ends any stream after six minutes, so a lost cancel cannot leave one running.
`ControlCancel` is not specific to the stream. A node stops any in-flight
command that only reads: telemetry, discovery and a miner command the
device-write gate classes as a read, other than an LED or board identify
indicator. It ignores a cancel for everything else, so a physical write, an
indicator change, pairing or a credential key change is never interrupted
part way and its ack always reports what it did. fleetd sends cancels only for
streaming commands today.

Each node relays at most four streams at once. A fifth viewer is told the node
is showing as many live ASIC views as it can, and the view starts when another
one closes. Streams use deferrable-read capacity and skip the device-write
gate. Command cancel, command frames and the stream require command protocol
V17. Older nodes are refused before enqueue with a message to update the Fleet
Node.

Module power-feed control calls `PowerService.SetPowerFeedState` (turn on or
off), `CyclePowerFeed` or `ClearPowerFeedOverride` for the one feed the
operator confirmed. It requires the `container:power_feed` permission and the
`container_power_feed_control` capability. Fleet offers an action only when
the mapping is `wiring_verified` or `configured`, the feed has a current
reading in a known state, no recovery cycle is active or unknown, and no power
feed command is pending for the container. Turn on needs a disabled feed and
turn off an enabled one. Cycle needs an enabled feed that reports no fault.
Clear override needs an overridden feed. Under its write lock, the plugin
re-derives the mapping, the module identity, the recovery cycle and the feed
state before it sends anything, and refuses with `FailedPrecondition` if any
of them changed. Those reads have a 10-second budget and the write its own
15 seconds. Fleet and the plugin take the actions a feed state allows from one
SDK rule, `PowerFeedWritesFor`. If Fleet cannot check whether controls are
available, the inspection still succeeds with controls off and a reason.
Writes are sent once and are never retried or replayed.
A write names the controller the operator inspected, and Fleet refuses it
before queueing if the module has since moved to another controller: feed IDs
repeat across containers.
Controller refusals report as failed, with the controller's reason. A Fleet
Node carries that reason back with `ACK_CODE_FAILED_PRECONDITION`. Transport
failures report as outcome unconfirmed. The confirmation dialog stays open
until the command settles. A cycle that hits its deadline has been committed, and the
controller owns its 30-second hold and restore durably. Fleet reads the feed
once when a command settles and shows the recovery cycle from that read; the
operator refreshes to follow it. Fleet Node routing requires command protocol
V16 for writes. An older node only inspects, so Fleet turns controls off for its
containers and refuses a write with an upgrade message. Feed power is separate
from mining: turning a feed off does not stop mining first. fake-ct1 does not implement power-feed
writes, so its controller refuses them as unimplemented.

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
- A poll that cannot read the container confirms and clears nothing. Fleet
  records an unreachable container as offline and still closes the errors that
  polls stop refreshing, so the errors already held are reported, all carried.
  They stay open in Fleet for as long as the container is unreachable. With no
  error held, the poll fails: a failed map read never produces an empty report.

Claims and sample order live in the plugin process, shared by every handle for
a container, including Fleet Node's per-command handles. They do not survive a
plugin restart: after one, carried errors are no longer reported, and Fleet
closes them after its staleness threshold. That includes a restart while the
container is unreachable, when the plugin holds nothing and the poll fails.

Fleet polls errors right after telemetry. Error polling reuses the outcome of a
telemetry poll that ended in the last ten seconds, so the poll cycle reads the
map once. If that poll read the container, its observation is the report. If
it could not, the error poll takes that failure as its own and does not probe
or read the controllers again. A poll the caller abandoned is not reused.
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
discovery, listing protected controllers without an identity,
per-controller bearer tokens on unary and streaming RPCs, token
rejection as an authentication failure, rejected token configuration,
status/cancellation/deadlines, receive
limits and ambiguous writes.
Real Postgres tests cover endpoint persistence, identity-based rediscovery, and
rescans of protected controllers keeping their verified identity.

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
reconnect, configured TLS, approved control readback, and one controller with
`mdk_auth` enabled. Compare bytes, latency,
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
  per-endpoint clients and their stored controller token. For a controller
  that a Fleet Node manages, the node makes these calls on its own plugin.
  Fleet Node routing requires command protocol V20; see
  [Containers behind a Fleet Node](frigate-server-updates.md#containers-behind-a-fleet-node).
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

## Allocation and mining-group mode

Allocating or delegating CT1 hashboards requires a supported, enabled mining-group
observation from the last two minutes. Suspended, changing, unknown and stale modes
are rejected. The observation must be newer than the current device fence. Pending
physical mutations also prevent allocation. Module selections expand to exact
installed boards and use the same rule. Recall remains available in every mode.
Manual and automatic board replacements follow the same readiness rule; an
automatic replacement waits without discarding the accepted inventory.
Automatic replacement skips busy device fences and mode observations, then
retries on a later inventory poll. Manual allocation returns a retryable busy
error during a conflicting device operation instead of waiting while holding
organization admission.
Fleet refreshes known idle CT1 controllers about once per minute, including
containers with no allocated boards. Recall and a return to Default therefore
keep the mode fresh for the next allocation. Allocation errors distinguish stale
observations from disabled mode and unfinished device work.
Mode reads serialize on the controller binding. Before reading the controller,
Fleet commits a not-ready marker while preserving the last display snapshot.
Only a successful observation commit restores allocation readiness. A failed
read or failed commit cannot leave a window that accepts the cached state.
After a successful refresh, the next accepted inventory poll can retry replacement.
Failed observations keep replacement blocked and retry with backoff.

Each saved mining-group request has a durable dispatch record. Fleet records
the request as unsent in the journal transaction. Before sending it, Fleet
commits a dispatch marker separately after local admission checks, so a failed
receipt save cannot erase delivery uncertainty. Each invocation has its own
token. A proven no-send refusal can restore only that invocation's marker;
an older worker cannot clear a newer worker's send after losing its SQL locks.
Tracking failures keep the saved request and record a blocked message while
the scheduler retries. Stale inventory waits for fresh data; changed bindings
can end a proven-unsent operation. Database and decoding failures retain their
original errors instead of becoming inventory warnings.
If recall changes ownership before dispatch, Fleet can
retire the unsent request after a settled controller observation. The new owner
cleanup can then proceed. Ownership changes cannot pass between validation and
dispatch. Sent requests and older requests without dispatch history keep their
recovery guards; matching readback alone does not prove that a request ended.

The container menu offers **Disable mining groups** to operators with
`miner:deactivate_mining_groups`, start-mining and stop-mining permissions. The
dialog explains why the action is blocked. All boards must be recalled, and pool
applications, removals, ownership cleanup and uncertain device writes must settle
first. A completed or terminally rejected operation does not block forever. A
clear awaiting a proven-safe retry still counts as unfinished work.

For remote controllers, upgrade the Fleet Node to command protocol V15 before
disabling mining groups. Fleet checks the live node version before committing
deactivation. Creating a group for a nonempty pool configuration and replacing
pools with explicit controller pool IDs require V18. Activation and confirming
Default pools before a return transfer also require V18. After activation, use
**Wake default mining group** from the Fleet container menu.
Fleet saves those IDs with the request before dispatch so a retry sends the
same pool IDs after the controller allocates IDs for new pools. Reads and
commands that use the original request format still require V6.

Deactivation uses the existing durable mining-group journal. Fleet commits the
exact request before sending it and retains its identity after a lost response.
Fresh disabled-mode readback completes the operation. New device commands, pool
work and allocations cannot pass admission while deactivation is pending. Frigate
also requires boards to leave non-default groups and its internal power work to
settle. After a recall to the host, Fleet offers a separate confirmation to move
the exact recalled hashboards to **Default**. Reviewed module selections keep
their original board membership. Operators can leave this step until later;
recall and loss of client access remain complete if the move fails.

The confirmation shows Default's pools and mining intent after ownership cleanup
settles. Moving boards can resume mining when Default is running; a stopped
Default stays stopped. Fleet changes membership only, without rewriting Default's
pools or starting or stopping the group. The accepted workflow retains the exact
installations, ownership revisions and confirmed settings. Lost replies replay
the saved request; changed settings require another review. Fleet rejects a
return when the source or Default group has custom power or tuning policy.
Controller observations expose only its presence, so Fleet cannot review or
confirm those values. Custom policies on unrelated groups do not block a return.
Only confirmed
returns retire the selected boards' old pool and completed-clear intents.

The Disable dialog offers the same confirmed action for all unallocated boards
still outside Default. Disable remains blocked until those moves and cleanup
settle. Returning boards requires permission to change pools and start and stop
mining. A recall between client accounts does not offer a return to host Default.

Inventory and pairing observations continue during deactivation. Automatic pool
policies recorded by those observations wait until deactivation settles, then
resume against the current controller mode. Confirmed deactivation clears the
old controller-group bindings while retaining snapshots and request receipts.

Firmware changes outside Fleet can still change mode. Whole-container ownership
clears therefore retain sibling board requests until the clear is verified. After
a pre-dispatch failure, a change to enabled mode can reprepare each original board
without losing the other boards' cleanup. Successful whole-container readback
settles only covered, earlier pending clears for the same owner.

## Controller logs and audit trail

Fleet reads a CT1 controller's own logs and audit events on demand through
`ContainerService.GetControllerLogs` and `ListControllerAuditEvents`. Fleet does
not ingest, store or record them: each read is answered from the controller, the
export is built in the browser, and record content is not written to Fleet logs
or activity. Audit events are Frigate's records, distinct from Fleet's own
command and activity history.

Direct controllers read through the Proto plugin; node-owned controllers run
the same read on their assigned Fleet Node, which requires command protocol V11.
The bounded read runs next to the controller and only a bounded result crosses
the gateway or plugin boundary:

- Logs use `SystemService.GetLogs`, a following stream with no end-of-tail
  marker. The plugin waits up to 2 s for the first entry (`no_response`), treats
  a 750 ms gap as the end of the retained tail and confirms it with a `tail: 1`
  read, re-reading once if a newer entry arrived. A 5 s deadline or a stream
  error after entries reports `interrupted` with a reason; an error before any
  entry fails the read. A full page reports `limit_reached`.
- Audit uses `AuditService.ListAuditEvents` with a window of at most 7 days and
  at most 1,000 events, newest first.
- Messages, details and error text are truncated per field and the oldest
  records are dropped to keep the encoded result under 3 MiB; either is
  reported in the result. Log entries are bounded as they arrive, so a read
  never holds more than that budget of controller messages.
- Each read holds a database transaction and a share lock on the container's
  binding until the controller answers, within a 15 s server budget, so writes
  to that container wait behind it. Read only when an operator asks; never poll
  through this path.

Reads use the read-preferred HA controller unless the caller addresses one
member by its controller ID. The result names the controller that answered and
the controllers available. A best-effort `GetSystemStatus` confirms the answering
controller's identity; a mismatch with the saved member fails the read.

The caller needs `miner:download_logs` plus `site:read`, `fleet:read` and
`miner:read` at the container's current site. The audit trail also needs
`activity:read` there, as Fleet's own activity log does; without it Fleet shows
only the logs.
