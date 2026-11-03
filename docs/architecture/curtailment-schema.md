# Curtailment schema notes

These notes describe the current curtailment schema and runtime. The SQL in
`server/migrations/current/`, domain types in
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

Event responses include `source_actor_type`. List responses omit
`external_source`, so clients use the actor type to identify automation and
scheduled events in history.

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

An event has a maximum duration only when its Start sets `max_duration_seconds`;
otherwise it is stored with `allow_unbounded` and runs until it is stopped or
aborted. Any caller with curtailment access may start an unbounded event or set
a cap up to the absolute ceiling. `curtailment_org_config.max_duration_default_sec`
no longer supplies a default cap.

A finite cap includes pending time. It starts at `created_at` (or an earlier
`started_at`) and does not reset when confirmation promotes the event to active
or when re-curtailment reverses restoration. Expiry starts restoration even when
some targets never confirm. The cap must be at least the minimum curtailed
duration. Only admins may shorten an existing cap, and unbounded events reject
cap updates. Operator updates recheck duration policy against the current row
so a concurrent extension cannot turn a non-admin update into an early restore.

The readiness deadline is the event's curtail batch interval plus
`CurtailMaxRetries × max(tick interval, curtail confirmation timeout)`, counted
from the latest curtail dispatch or confirmation (or `created_at` before any).
It bounds stalled pending events that are not explicitly unbounded, without
cutting off a paced plan that is still making progress. Deadline expiry records failed curtail evidence for the
stalled targets. A stalled miner never restores miners that are already curtailed: when any target is confirmed, the
event becomes active and in-flight targets keep confirming or retrying there. Only an event with no confirmed target
starts restoration at the deadline. Explicitly unbounded events, including all-paired events whose
miners never become commandable, keep waiting until they are stopped or
aborted. It does not extend the finite maximum duration.

Curtail command failures and confirmation timeouts consume `CurtailMaxRetries`
(default 50). Restore failures use the separate `MaxRetries` budget (default 10).
A curtail phase that exhausts its budget retains `restore_failed` history and is
not automatically reopened for more curtail attempts within the same event.
A failed curtail that may have reached the miner still receives a compensating restore:
missing confirmation does not prove that the hardware stayed active. That covers an
enqueued command, an ambiguous send (an enqueue error, an interrupted dispatch, or a
lost result write), recorded in `curtail_possibly_sent`, and a prior restore cycle.
Retry history alone is not evidence of a send: admission skips and unenqueued devices
consume retries without reaching the miner, so those targets release or stay
`restore_failed` without an Uncurtail. Before this column existed retry history was the
only evidence, so the upgrade marks every target of a live event that has retries as a
possible send. Completion
reports failures even when that later restore succeeds. Deleted all-paired
topology restore targets terminalize as failures; live temporarily unavailable
policy targets can remain parked until commandable.

Every curtail dispatch reloads the execution principal's authorization. Event,
permission and current device-site locks cover command enqueueing; topology
scopes additionally fence selected resources and membership. A moved target
cannot enlarge a bounded authorization envelope. Infrastructure failures defer
dispatch without starting restoration or spending device retry budget. A policy
or permission denial first persists `dispatch_rejection_reason`, then begins
restoration. The denial survives restart and failover even if that transition
initially fails. It is permanent for the event: re-curtailment of a rejected event
is refused, so automation waits for it to finish restoring and then starts a new event.

MQTT source creation, edits, enable/disable changes, connection tests and
deletion require both administrator status and `curtailment:manage`. Listing
and reading sources require `curtailment:manage`.

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

## Facility fan operations

Fan commands pin a database connection under the shared dispatch budget. Sorted
per-fan session advisory locks and a lifecycle session key serialize fan owners
and event transitions. Infrastructure endpoint rows are also locked in sorted
order to prevent a fan from moving or being deleted during physical I/O. Those
narrow row locks use an uncancelled transaction and remain held until the adapter
returns, including when the caller cancels. Hardware calls hold no event row
lock; event checks and result writes use short database operations.

Persisted successful fan state prevents repeated ON or OFF commands on every
tick. OFF intent records a pending error before hardware I/O, so a lost result
write triggers recovery rather than being mistaken for success. Terminal
recovery commits its pending marker before sending ON and retains fan ownership
until recovery succeeds. Miner restores still observe the configured airflow
delay, including the existing failed fan-ON fallback and alert behavior.

## Automation bindings and execution

Automation execution is authorized by an organization-owned, revocable grant for each
rule, rather than by the MQTT source creator's current user role. Saving or enabling a
rule requires the caller to manage every site in the profile's persisted authorization
envelope, which can include sites its miners have since left. It then copies that profile
revision and envelope into the grant, and records whether that caller could authorize
admin controls. A grant cannot
expand beyond that scope when topology changes. Human event dispatch continues to use
the user's current permissions. Disabling a rule or source revokes further automation
dispatch. A source cannot be disabled while one of its rules owns a pending, active, or
restoring event; restore or finish those events first.

After upgrading to this schema, existing rules show `rebind_required` until a permitted
operator saves or enables each rule with its current profile revision. Historical rules
receive no implicit execution grant, so an automation event that is live during the
upgrade has its next dispatch rejected and is restored. An enabled binding prevents profile edits; disable
all of that profile's rules, edit the profile, then enable each rule with the new
revision. Rule responses expose `binding_status` (`bound`, `rebind_required`,
`disabled`) and `execution_status` (`idle`, `pending`, `retry`, `completed`). Profile
responses include the number of enabled bindings.

MQTT receipt persists each enabled rule's latest desired assertion in its own mailbox
before acknowledging the source edge. Assertions are ordered by receive time. Creating
or enabling a rule seeds its mailbox with the source's current state, stamped with when
that state was received, so a newer signal still being processed replaces it, and the
seed never replaces a newer queued assertion. A leased worker drains those mailboxes
independently. Generation and lease-expiry fences prevent an old attempt from completing
or retrying replacement work. Errors retain only the failing rule for retry with
backoff. Same-target OFF reassertions preserve a pending cycle's original direction,
receipt time, attempt count, and retry deadline, so heartbeats cannot erase a genuine
ON-to-OFF cycle or defeat its backoff. A new opposite assertion or genuine demand cycle
replaces the desired state without interrupting an active attempt. New or enabled rules
immediately queue the currently persisted source state. Debounced ON observations
remain eligible for processing after the debounce window.

MQTT automation events have no duration cap, because response profiles carry none; they
end when the source sends ON, or when an admin force-stops or aborts them.

If an automation event does carry a cap, a continuing OFF assertion cannot
immediately start another event after that cap expires; an intervening ON and then a
new OFF are required to start a new demand cycle. The latest mailbox
preserves the new cycle even when the worker has not processed its intervening ON, and
waits for the previous capped event to finish restoring. A watchdog OFF from an ON
source is also a new demand cycle.

### Schedule rules

`curtailment_automation_rule.trigger_type` is `MQTT`, which requires
`mqtt_source_id`, or `SCHEDULE`, which has no source. A rule's trigger type cannot
change after it is created. Each schedule rule has one
`curtailment_automation_schedule` row. A `ONE_TIME` schedule is a start and end date
and time at most seven days apart. A `RECURRING` schedule repeats `DAILY`, `WEEKLY` on
selected days, or `MONTHLY` on a day of the month (skipping months without that day),
from `start_date` until the optional `until_date`. Its end time may be earlier than its
start time, in which case each window ends the next day. Dates and times are local to the
schedule's IANA `timezone`. `service_user_id` is the user who last saved the schedule;
scheduled events and their commands are attributed to that user.

Schedule rules use the same grants, binding status, and per-rule mailbox as MQTT rules.
Every five seconds the automation service asserts each enabled schedule's current
state: OFF stamped with the window's start while a window is open, otherwise ON stamped
with the most recent window's end. The mailbox accepts only a newer boundary, so each
boundary runs once. A schedule saved, enabled, or recovered mid-window starts at once
for the rest of that window; windows that have already ended are skipped. Saving or
enabling a schedule clears its mailbox so its current state is asserted again. The
schedule assertions run on their own ticker, and up to four rule executions run at once,
so a slow or failing rule cannot hold back another rule's window.

Every save, and enabling a disabled schedule, gives the schedule a new `revision`, and
each queued boundary carries the
revision it was produced from in `curtailment_automation_execution.schedule_revision`.
A boundary from an earlier revision is not queued, is dropped by the worker, and cannot
start an event: the event insert rechecks the revision under the rule's execution lock.
A boundary from the saved revision replaces an earlier revision's row even when its time
is earlier. A scheduled event records its schedule revision in `decision_snapshot`, and a
window's end restores only the event that window started under the same revision, so a
delayed end boundary cannot stop a later window's or an edited schedule's event. The
event insert also rejects a start whose window has already ended and caps the event so
that, counted from its `created_at`, it ends at the window's end.

Each window starts its own event with `source_actor_type` `scheduler` and
`scheduled_start_at` set to the window's start. The event is never unbounded: its
`max_duration_seconds` is the time left in the window, as a backstop for the window's
end restore. A window does not take over a previous window's event; it waits until that
event is no longer live. Any user with `curtailment:manage` can stop a scheduled event
early, and the schedule curtails again at its next window; a window's event that already
ran under the current revision is not started again, even after it was stopped. An admin
force release disables the schedule, as it does an MQTT rule; enabling it again runs its
current window. Rig fallback configuration includes only MQTT rules.

Rig fallback configuration is built per device from enabled, granted rules whose profile
scopes select that device. A device outside all current scopes receives a disabled
config to clear previous providers. Full source/rule configuration changes rebuild these
scoped snapshots. In an organization with an enabled rule, database triggers also
request delivery for the affected devices when a device's site or building changes, when
rack or group membership changes, when a rack moves buildings or loses its placement, and
when a rack, group, or building is deleted. Upgrading to this schema requests a full
rebuild for every organization that may hold an older organization-wide config, so
rigs drop providers whose rules have not been rebound. Device placement changes request
delivery once per organization per statement, so a bulk reassignment does not repeat the
request for every moved rig. After an organization enqueue failure, the first nine
retries back off linearly (5 to 45 seconds); from the tenth consecutive failure,
delivery retries every ten minutes until it succeeds, and reaching that slow period is
logged. After a device's config command fails terminally, the first ten requeues retry
after five seconds and later ones every ten minutes, so a rig that was unreachable for
longer still receives its current config once it reconnects. A slow retry never
postpones other devices' work that is already due. A new source/rule settings change
restarts delivery, and an explicit device reapply on pairing/recovery resets that
device's terminal retry count. Failed device work remains
persisted with its error after its budget is exhausted.
