# Device-write recovery

Dispatched device mutations retain their attempt records indefinitely unless
physical completion is established. Current error-only SDK and Fleet Node
results, including HTTP 200/202 and successful command acknowledgements, do not
certify physical completion. Fleet records successful requests as command
success while retaining the physical attempt as `unknown`. This conservatively
applies to all mutations through the write fence, not just reboot: firmware,
pool/configuration writes and mining control have the same evidence requirement.
There is no automatic completion qualification in this release; use the audited
recovery procedure below before a later ownership transfer.

Acceptance alone does not quarantine host custody: ordinary host commands remain
available, and accepted commands are not retried. A failed dispatched call still
quarantines custody. Completing a later request, polling an install before its
follow-on reboot, or rejecting a later call before dispatch cannot clear an
earlier unresolved mutation.

A local admission refusal is known to be undispatched when the adapter never
invoked its device or transport callback. Fleet Node `BAD_REQUEST` and
`AGENT_INCAPABLE` acknowledgements may describe early refusals, but their codes
do not certify that backend work never began. They retain unknown evidence after
a send. A failed `BUSY` acknowledgement is the documented exception: the node
refuses it before device dispatch. Broader automatic release requires an explicit
node execution-stage guarantee; error-message text is not that guarantee.

Uncertain device operations retain their attempt records indefinitely. Timeouts,
queue reaping, disconnected transports, a new process starting, and successful
configuration readback do not prove an earlier operation ended. Unknown attempts
block a new ownership reservation. Running attempts continue to block drain.

`fleet-device-write-recovery` provides an offline host-operations recovery path.
It uses privileged database access, not customer API permissions. The named
operator attests to external evidence; the command verifies its binding to the
database record, not the truth of external runtime or hardware receipts. Do not
use it unless the operator can establish all the required facts.

## Dispatch binding

Direct SDK and Fleet Node mutations lock the device, discovery, pairing,
credentials and node route through the actual adapter call. Binding writers wait
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
SDK rejection is recorded as an
undispatched attempt. Other transport/device errors retain unknown evidence.
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
of pending stale commands together. The original attempt remains retained;
operator resolution is distinguishable from successful physical execution.
Reusing stale evidence is refused. A database failure rolls the transaction back.

Inspect again before resolving another attempt because each resolution advances
the fence epoch. Resolving one attempt never clears another outstanding attempt.
Processing queue status is not changed into proof of physical completion; old
processing/retried commands fail their epoch check before device I/O.

Host custody remains host custody, so after its last unknown attempt is resolved,
a new reservation can succeed. To abandon reserved or quarantined custody, set
`restore_host` only after the host configuration or reliable stopped state is
verified and include its verification reference. Restoration is refused while
other attempts are unresolved, or when custody is active or quarantined from an
active allocation. The persisted quarantine origin preserves that distinction. It does not release an
active customer allocation. Otherwise the existing custody state is retained.

Keep dispatchers stopped until the result and audit record have been reviewed,
then restart them. Retain backup and recovery evidence under normal operational
retention. Never delete attempt history, force migration versions, or directly
update statuses to get out of a block.
