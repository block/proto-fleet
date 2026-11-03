# Device-write recovery

Ordinary configuration and mining-control writes use an ordered, synchronous
firmware API contract. This includes pools, credentials, cooling/power settings,
start/stop, curtailment configuration/control, and tank/container control. A
successful API response completes the attempt. If the response is lost or the
write fails, Fleet preserves the command's failure but retires its attempt once
the adapter returns: an ordered incoming configuration write can supersede the
old one. `complete` means the attempt no longer blocks handoff, not that the
command succeeded. The allocation controller must fence conflicting writes,
drain active calls, and apply and verify the incoming configuration before
activating its owner. Host curtailment remains fleet-wide.

The direct miner proxy uses this same completion contract for allowlisted MDKv2
start/stop (including scoped mining), target power, and container switches and
setpoints. An explicit HTTP 202 response remains unresolved. Generic v1 writes,
run-in jobs, identify-LED calls, and isolation changes remain unclassified.
Completion still waits for response consumption, downstream flush, and body close;
a later completed control never clears an earlier unresolved attempt.

Reboot, firmware installation, Unpair, and unclassified operations retain
`unknown` attempts after dispatch, including successful acknowledgements, because
they do not use this configuration-write completion contract. A failed plugin RPC
or failed/missing Fleet Node acknowledgement also retains uncertainty: the intermediary may still queue or
dispatch the command before it reaches the firmware API. Even a failed terminal
node acknowledgement may report a timed-out plugin RPC whose backend still runs.
A successful synchronous plugin gRPC response or Fleet Node acknowledgement
completes an ordered write. Most returned RPC errors lack a backend-completion
guarantee and retain uncertainty. Preparation refusals remain known to be
undispatched.

A canceled caller cannot retire an adapter that is still running. Crashed or
hung dispatchers, panics, and failures to persist completion leave durable
unresolved attempts. These exceptional cases use the audited recovery procedure
below. A later successful command never clears an earlier unresolved
asynchronous operation or node delivery. Accepted asynchronous commands are not
retried merely because their attempts remain unresolved.

A local admission refusal is known to be undispatched when the adapter never
invoked its device or transport callback. Fleet Node `BAD_REQUEST` and
`AGENT_INCAPABLE` acknowledgements may describe early refusals, but their codes
do not certify that backend work never began. These failures retain unknown
evidence after a send, including for ordered writes, because no explicit backend-completion guarantee distinguishes them from
intermediary failure. A failed `BUSY` acknowledgement is the documented
undispatched exception: the node refuses it before device dispatch. Error-message
text alone is not evidence of an early refusal.

Uncertain device operations retain their attempt records indefinitely. Timeouts,
queue reaping, disconnected transports, a new process starting, and successful
configuration readback do not prove an earlier operation ended. Unknown attempts
block a new ownership reservation. Running attempts continue to block drain.
An unknown curtailment control on reserved or active custody quarantines its
fence; successful synchronous controls leave that custody unchanged.

`fleet-device-write-recovery` provides an offline host-operations recovery path.
It uses privileged database access, not customer API permissions. The named
operator attests to external evidence; the command verifies its binding to the
database record, not the truth of external runtime or hardware receipts. Do not
use it unless the operator can establish all the required facts.

## Dispatch binding

Direct SDK and Fleet Node mutations lock the device, discovery, pairing,
credentials and node route through the actual adapter call. Durably admitted
observations share binding locks so same-miner reads can overlap. A mutation
attempt takes its exclusive parent lock before preliminary login and retains it
through subsequent writes; it never upgrades an observation lock. Binding writers wait
until it returns; canceled callers do not release locks while a context-ignoring
adapter continues. Admission commits before these locks are taken. The lock
transaction performs no retry and reuses its connection for the final attempt
check, so it neither replays physical I/O nor requires a second pool slot.
A failure to finalize this lock-only transaction or record attempt completion is
logged without turning an accepted command into a retryable failure. Unresolved
attempt evidence still blocks ownership transfer until audited recovery.

Physical dispatches share a process-wide budget per database pool. Admission waits
with the caller’s context before leasing a connection; cancellation does not
release a slot until the adapter returns. By default, dispatch uses at most half
of `DB_MAX_OPEN_CONNS` (125 with an unbounded pool). `DB_MAX_DISPATCH_CONNS`
can set a smaller or larger cap below the pool capacity. At least one connection
must remain available for authentication, artifact downloads and attempt
completion; physical dispatch is refused for a one-connection pool.

Miner proxy traffic has its own immediate admission cap:
`MINER_PROXY_MAX_CONCURRENT_REQUESTS` defaults to sixteen and is capped below the
shared dispatch capacity, leaving capacity for commands and curtailment. Excess
requests return 503 before creating an attempt. The proxy is disabled when the
shared dispatch capacity is below two. Each proxy operation has a 30-second
budget including request-body reads, connection and binding-lock waits; downstream
response delivery has a separate 30-second budget.

Fleet Node heartbeats write `fleet_node_heartbeat` independently of the routing
row. Enrollment, mining-asset status and unavailable-node alerts read `fleet_node_live`.
Dispatch locks the node identity and route associations, while authentication
revocation commits independently and immediately invalidates its control stream.
Route and credential cleanup follows; if interrupted, retry revocation to finish
cleanup. Heartbeats continue during dispatch. Migration 1013 backfills existing timestamps;
its rollback copies the latest heartbeat into the retained baseline column.
Deploy it during the application-stopped maintenance window, so an older writer
cannot update the retained snapshot after the backfill.

Scoped SDK references also carry an opaque backend generation. Replacing the
same external identifier cannot redirect that reference; a stale token is
rejected before backend I/O. Fleet requires scoped resolution for persisted miner
handles, including a fresh transient scope for password recovery. That specific
SDK rejection never marks the attempt dispatched; after its callback ends, the
transient direct admission is removed. Other failures follow the completion
contract above.
ASIC-rs rechecks live hardware identity before writes and on fresh telemetry,
while keeping the state needed to restore curtailment.

## Retained identity scope

Serial and MAC reports belong to their Fleet organization. Endpoint keys also
include the authenticated Fleet Node (or local dispatcher) routing scope. A
reported identity in one organization cannot fence another organization's
commands. Within an organization, rediscovery on another node still matches a
retained serial or MAC and cannot erase unresolved physical work.

Cross-organization physical handoff requires an explicit, host-verified identity
association before writes can be admitted in the destination organization. This
release does not implement that handoff; untrusted node reports cannot create
such an association. Do not move a miner with unresolved work to another
organization to bypass recovery.

Hosted ships as one feature. Migration 1012 requires empty attempt history;
upgrades from intermediate hosted development runtimes are unsupported. Runtime
admission captures identity keys and snapshots when each attempt is created.
Never reconstruct an old attempt's identity from today's device row.

## Evidence and maintenance

1. Stop every Fleet API replica, plugin process, Fleet Node dispatcher, and
   scheduled writer that could dispatch to these miners. Prevent automatic
   restart until recovery completes. Retain authoritative supervisor/container
   runtime termination evidence, including the exact instance identity. A stop
   request, deleted pod object, stale heartbeat, or database session ending is
   insufficient evidence that its process ended.
2. Inspect the exact organization and device, including a retired device when
   deletion/rediscovery left outstanding work. Match `owner_instance_id` to the
   `device-write process owner` startup log, hostname, PID, and runtime instance
   that actually ended. PID reuse and the termination of another replica do not
   resolve this owner. Keep evidence references that identify the actual runtime
   object and completed termination, rather than copying logs into the input.
3. For a mutation, also establish transport quiescence and that device-side work
   conclusively ended. An authoritative completed job or a qualified physical
   isolation/reset procedure can supply this evidence. A reboot request, elapsed
   time, HTTP error, or settings readback alone cannot. Verify the intended pool
   configuration with qualified credential readback, or a reliable stopped state.
   Keep verification receipt references; never put credentials in recovery JSON.
4. A durably classified observation needs the owner's termination evidence, but
   no mutation-completion proof. Legacy or unclassified admissions remain
   mutations. The observation context cannot dispatch a mutation.

The process nonce identifies an owner; it is not a lease or automatic permission
to clear that owner's work. Recovery never uses an expiry or infers process death.

## Inspect, preview, apply

Use the target release's executable with the normal `DB_*` connection settings.
It does not run migrations. The inspection includes an immutable identity
snapshot and a bound evidence template:

```sh
fleet-device-write-recovery inspect --organization-id 1 --device-id 42 > inspection.json
jq '.[0].evidence_template' inspection.json > evidence.json
```

Fill in `operator`, `maintenance_termination_evidence`, and
`owner_termination_evidence`. For mutations, also fill in
`transport_quiescence_evidence`, `device_quiescence_evidence`, and
`configuration_verification_evidence`. Use specific receipts or ticket references
that identify the operations actually completed; do not change the binding fields.

```sh
fleet-device-write-recovery resolve --evidence evidence.json
fleet-device-write-recovery resolve --evidence evidence.json --apply --acknowledge-all-dispatchers-stopped
```

Preview performs the binding checks without changing safety evidence. Apply
locks the fence and attempt, checks the exact device/organization, immutable
identity digest, process owner, operation class, status, and both epochs, then
commits the audit record, `resolved` status, epoch advancement, and cancellation
of pending stale ordinary commands together. The original attempt remains retained;
operator resolution is distinguishable from successful physical execution.
Reusing stale evidence is refused. A database failure rolls the transaction back.

For a mining-group attempt, that same transaction marks its exact active
controller workflow failed and its unfinished board update results interrupted.
It discards legacy pool intent belonging to that workflow and excludes the failed
shared configuration targets from future controller plans. The interrupted step,
bindings, and recovery evidence remain available for diagnosis; recovery neither
claims that the requested configuration succeeded nor replays hardware writes.
Unrelated workflows and settled results remain unchanged. A new pool
update requires a fresh, explicitly admitted request.

Inspect again before resolving another attempt because each resolution advances
the fence epoch. Resolving one attempt never clears another outstanding attempt.
Processing queue status is not changed into proof of physical completion; old
ordinary processing/retried commands fail their epoch check before device I/O.
Global host curtailment remains independent of configuration ownership and can
run at the current custody state; resolving one attempt does not revoke that
fleet authority. Pending curtail, restore, and fallback-config commands remain
queued and can dispatch after restart. A fresh uncertain delivery creates a new
unknown attempt and drain block; it does not undo the earlier audited resolution.
Its physical uncertainty still requires mutation recovery evidence.

Host custody remains host custody, so after its last unknown attempt is resolved,
a new reservation can succeed. To abandon reserved or quarantined custody, set
`restore_host` only after the host configuration or reliable stopped state is
verified and include its verification reference. Restoration is refused while
other attempts are unresolved, or when custody is active or quarantined from an
active allocation. The persisted quarantine origin preserves that distinction. It does not release an
active customer allocation. Otherwise the existing custody state is retained.
Restoring active allocation custody after recovery requires future allocation
controller integration to reconcile the allocation, apply and verify its intended
configuration, and restore the appropriate fence state. The offline command
cannot restore active-origin quarantine to host custody.

Keep dispatchers stopped until the result, audit record, and pending curtailment
controls have been reviewed. Account for their redispatch before restarting;
recovery preserves global curtailment authority. Retain backup and recovery
evidence under normal operational retention. Never delete attempt history, force migration versions, or directly
update statuses to get out of a block.

### Interrupted proxy reads

The miner proxy commits `observation` admission before forwarding endpoints
classified as reads by its authorization contract. The admitted context cannot
dispatch a mutation. The captured binding stays locked through login, forwarding, response reads,
downstream flush and upstream body close. All of these remain inside admission. A completed read removes its transient
admission, as does a completed direct mutation admission that never dispatched.
Dispatched, queued and claimed deletion-permit evidence remains retained. Process
interruption or failed completion leaves a running record.

Recover that record only after authoritative termination of its recorded owner
and the maintenance shutdown described above. No device mutation verification
is required for a durably classified observation, unless restoring host custody.
Deletion and rediscovery do not clear the block. Other direct calls and queued
commands remain conservatively classified as mutations. Direct admissions that
finish without dispatch are removed; interruption cannot establish that fact.

Fleet handles SIGINT and SIGTERM by stopping new HTTP admissions and canceling
active request contexts. After graceful shutdown, it closes remaining sockets,
including hijacked h2c connections, to interrupt blocked transport I/O. It waits
for HTTP/1 and h2c handlers to finish before stopping runtime dependencies,
allowing completion bookkeeping to retire reads.
The graceful socket deadline does not prove callback completion; crash or forced
termination still requires the interrupted-read recovery above.
