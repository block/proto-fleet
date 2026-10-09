# Device-write recovery

Uncertain device operations retain their attempt records indefinitely. Timeouts,
queue reaping, disconnected transports, a new process starting, and successful
configuration readback do not prove an earlier operation ended. Unknown attempts
block a new ownership reservation. Running attempts continue to block drain.

`fleet-device-write-recovery` provides an offline host-operations recovery path.
It uses privileged database access, not customer API permissions. The named
operator attests to external evidence; the command verifies its binding to the
database record, not the truth of external runtime or hardware receipts. Do not
use it unless the operator can establish all the required facts.

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
