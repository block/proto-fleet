# Curtailment schema notes

These notes describe the current curtailment schema and runtime. The SQL in
`server/migrations/current/001000_shared_baseline.up.sql`, domain types in
`server/internal/domain/curtailment/models/models.go`, and writes in
`server/sqlc/queries/curtailment.sql` are the sources of truth. Comments in
immutable migrations 000042 and 000072 record earlier behavior and are not a
current schema reference. See [database migrations](../development/migrations.md)
for upgrade and index-build procedures.

## Events and scopes

`curtailment_event` stores one persisted event, keyed internally by `id` and
externally by unique `event_uuid`. `org_id` identifies its organization.
Preview does not persist an event. The runtime uses these text values:

| Field | Current values |
| --- | --- |
| `state` | `pending`, `active`, `restoring`, `completed`, `completed_with_failures`, `cancelled`, `failed` |
| `scope_type` | `whole_org`, `site`, `device_list`, `mixed` |
| `source_actor_type` | `user`, `api_key`, `webhook`, `scheduler`, `automation` |

`scope_jsonb` stores the normalized scope contract. `mixed` is also the storage
representation for building, rack or group selectors and multiple site IDs;
the service validates the selected terminal resource type. `device_sets` is not
a current scope-type value. The persisted authorization envelope records the
scope required for later reuse and dispatch authorization; it is separate from
the executable scope and is checked against live permissions.

Nonterminal events are `pending`, `active` or `restoring`. The partial unique
indexes for `(org_id, idempotency_key)` and
`(org_id, external_source, external_reference)` apply only to those states and
only when their optional keys are present. Terminal history therefore does not
permanently reserve either key. Multiple events may run in one organization;
target ownership provides the device-level exclusion.

Finite maximum durations also apply while an event is pending. Events without a
`started_at` measure the cap from `created_at`, because dispatch timestamps advance
on retries; otherwise they measure it from `started_at`. Expiry starts restoration
even when some targets never confirm. The cap must be at least the minimum curtailed
duration. Only admins may shorten an existing cap, and unbounded events reject
cap updates. Operator updates recheck duration policy against the current row
so a concurrent extension cannot turn a non-admin update into an early restore.

## Targets and identity

`curtailment_target` has primary key `(curtailment_event_id, device_identifier)`
and an event foreign key with cascading deletion. It retains both the current
`state`/`desired_state` and separate curtail/restore phase summaries, dispatch
timestamps, completion timestamps, retries, failures and errors.

Current target states are `pending`, `dispatching`, `dispatched`, `confirmed`,
`drifted`, `unavailable`, `resolved`, `released` and `restore_failed`.
`dispatching` records the in-flight pre-command stage; `unavailable` retains an
unavailable target for reconciliation. The phase-state check constraints allow
these values; `restore_state` may also be null before restore begins.
`desired_state` is `curtailed` during curtailment and `active` during restoration.

`uq_curtailment_target_one_non_terminal_per_device` is unique on
`device_identifier` where state is not `resolved`, `restore_failed` or `released`.
It spans events and organizations, so `unavailable` and `dispatching` still hold
ownership. A device identifier can recur across terminal target history.

`device.uq_device_device_identifier` is also a partial unique index: it enforces
global uniqueness only among device rows with `deleted_at IS NULL`. Soft-deleted
rows may share an identifier with a later live device. The target identifier is
not a foreign key to `device`, and an identifier alone is not an immutable
physical-device identity. Do not infer ownership or historical organization from
an arbitrary device row sharing that identifier.

## Release and completion timestamps

`released` means the event relinquished target ownership; it does not prove a
restore command was dispatched or the miner is active. Force release and the
never-dispatched target release path set the relevant phase state and phase
completion time. Normal confirmation updates the corresponding phase completion
time as well.

Force release, never-dispatched release (including targets that leave a
topology scope before Curtail dispatch), and all-paired policy release populate
`released_at` on the first release; repeated writes preserve that timestamp.
Reacquiring a terminal target for the same event clears it to null along with
the new curtail phase's dispatch/retry state. Older released rows can still have
a null timestamp, so use the target state to determine ownership. Use
`curtail_completed_at`/`restore_completed_at` for phase completion; release must
not be interpreted as confirmed restoration.
