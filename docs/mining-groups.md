# CT1 hashboard pool assignment

Fleet uses MDK v2 `mining-groups-v1` to assign exact physical hashboards to
independent pool configurations. Enable mining groups from the container actions
menu, then use **Edit pool** on modules or individual hashboards. The action uses
the shared configuration picker after login confirmation. Module selections expand
into currently installed boards at review, with overlapping module/board selections
deduplicated. Selections can span controllers and include ordinary miners. Missing
boards, replacements and stale inventory are rejected. Replacement boards do not
inherit an earlier installation's assignment.

Enable mining groups requires `miner:activate_mining_groups`, `miner:start_mining`,
and `miner:stop_mining` at the container's site. The activation permission is initially
granted to Admin (and SuperAdmin through the standard permission catalog). Fleet records
the saved Default mining intent, stops mining, waits for board shutdown, and
enables groups. Default keeps its pools and stays stopped until an operator
starts it. Release factory-test reservations before activation. Every configured
controller must also meet Frigate's activation prerequisites.

Activation reads the complete controller map through Fleet Node. It verifies
each MAC and serial against one stored hashboard identity and its current owner.
Unlocated boards and conflicting slot positions do not prevent this check.
Missing identities, duplicate identities, stale observations, conflicting
controller attribution, and customer ownership prevent activation.

If Fleet has a container pool assignment, activation preserves its revision and
rendered worker names. When all boards have valid installations, Fleet transfers
the assignment to those installations. Otherwise, Fleet enables mining groups
and retains the container assignment and complete board identity list. Pool
changes and ownership changes remain blocked until the transfer completes or an
operator cancels it. Placement-only inventory updates remain allowed.
State refreshes and the readiness worker retry this database-only transfer after
placement is corrected. They recheck the original identities, ownership, and
default-group membership. No replacement board receives the captured assignment.
This database-only recovery does not depend on the original operator retaining
access. Controller inventory reads occur before the organization update lock;
Fleet rechecks identity, ownership, freshness, and saved intent under the lock.

If a board is permanently removed or placement cannot be repaired, an operator
can remove the retained container assignment with **Keep device settings**.
This requires current activation and pool-update permissions at the container's
site. Confirmation cancels the pending transfer and retires its assignment in
one transaction, with the normal removal audit record. It sends no controller
write and keeps the frozen default-group settings. It cannot clear device pools
or bypass unresolved physical operations. Customer sessions cannot cancel the
transfer. A stale removal review must be reviewed again.

This requires an updated Fleet Node and Proto plugin that support the
`activation_inventory` read. Activation remains a Fleet UI operation; it does
not require an account API request from the user.

Pool assignment through mining groups requires `miner:update_pools`,
`miner:start_mining`, and `miner:stop_mining` at the container's site, in addition
to the existing read permissions. Both workflows check all required permissions
when accepting a request and again before dispatch. If access is revoked,
Fleet stops issuing writes and settles the interrupted workflow when its earlier
controller work is known to be terminal. Unresolved requests retain their identity
and write admission. State reads do not require
start/stop permissions. Ordinary miner pool updates continue to require
`miner:update_pools` without start/stop permissions; configuration creation separately
requires `pool:manage`.

Operator and hosted-account sessions use the same configuration library and
assignment records. Hosted writes require the selected account's **Change pools**
capability and current ownership of every selected module. Allocation alone grants
no write permission. Account API keys cannot apply configurations. Ownership and
the initiating user's authority are checked again before each mutation, including
all other boards affected by a group-wide change.

## Database migration

Migration `1039` creates the complete mining-group schema, including retry,
activation, and transfer-failure state. Its down migration locks the tables and
refuses rollback while operations are active or failed, or while bindings or
pool intents remain.

Migration `1051` adds immutable rendered execution snapshots and links shared
board targets to their controller workflow. Its rollback refuses to discard
snapshots retained by controller bindings. The pool-import command reports devices
with conflicting unresolved writes or unfinished mining-group operations without
adopting their assignments. Libraries and unaffected equipment still import.
Unrelated LED, log, cooling, and power-target attempts do not block pool updates;
unknown operations, pool changes, curtailment, and device-lifecycle work remain
conflicts. These attempts retain their existing ownership-recovery protections.

## Desired and observed state

`pool_configuration_assignment` records the desired shared configuration and
revision for an exact installation. The approved target freezes its board, MAC,
controller and ownership. `hashboard_pool_intent` is retained only for legacy
assignments; new shared work replaces the selected legacy intents atomically. `container_mining_group_state` records the
last successful controller observation, timestamp, Fleet group bindings, and
current operation. The observation contains groups, memberships, revisions,
and every primary/backup pool's nonsecret settings. Observed board assignments
are derived from memberships, rather than stored as another authoritative map.

Controller observations never overwrite desired intent. A mutation response is
not a fresh observation: Fleet reports completion only after all requested board
memberships and public pool settings are observed. Completion means the
configuration is applied, not that a pool is connected or producing accepted
shares. Pool outages do not trigger fallback to another group's configuration.

Users select a named configuration containing a primary pool and up to two
backups. After authentication, assigning to a single miner, module or hashboard
uses **Assign** directly; bulk selections use a count-based confirmation. Both
paths resolve and confirm a canonical review on the server. Editing an assigned
configuration shows its affected equipment and, after password confirmation,
advances its assignments to the approved immutable revision. Saved
assignments, pending progress and results are visible on module and board views;
a module with different or partially assigned boards displays mixed configurations.

Mining groups support ordered TCP Stratum V1 pools. Fleet appends the controller's
existing worker name when the configuration uses hardware-managed worker names,
and freezes the rendered usernames in an encrypted execution snapshot. Imported
fixed usernames retain their original values. Each controller/configuration
revision/rendering has its own group identity; equal credentials in independently
owned configurations do not merge their groups. Passwords are write-only in MDK,
encrypted at rest and encrypted for Fleet Node transport. Successful application
is reported as **verified** once the readable pool settings and board membership
match the intended configuration. Passwords are compared when firmware exposes
them; write-only passwords do not prevent verification. A failed readback remains
unverified.

## Pairing, import, and worker names

Ordinary miners have a Fleet worker name independent of shared pool configurations.
Pairing initializes it from the normalized MAC address and preserves a saved name
when re-pairing. Import initializes missing names from an explicit worker-name
field when supplied, otherwise from the same MAC default. Re-import preserves
saved names. Foreman's pool `Worker` field is a full pool username, so it is never
used to infer a Fleet worker name. Dots may be part of the pool account itself.
Foreman onboarding continues importing groups, racks, assignments, and display
names with pool import disabled.

Pairing does not automatically recognize or adopt a library configuration.
Unassigned equipment displays **Device configuration** with its full observed
username unchanged. Assigning a configuration renders its username using the
miner's independent Fleet worker name when its naming rule calls for one.
Changing that worker name on assigned equipment requires a reviewed reapply.
Imported fixed pool usernames retain the full username without an inferred suffix.

## Reconciliation and recovery

After shared assignment confirmation, the worker projects the complete sequence
with the same planner used for reconciliation, before admitting a controller
workflow. Unsupported custom-policy moves or insufficient group capacity mark
the accepted operation failed without dispatching firmware writes. Whole-group pool changes that preserve
custom policy remain supported. A pending controller transfer requires a refresh
after it finishes before a new assignment can be accepted.

The planner returns one mutation at a time. It reuses the destination group,
reconfigures an entire selected group in place where possible, and otherwise
creates a group, writes its pools, starts its mining intent, then transfers exact
MACs from their current source groups. Unselected memberships are preserved.
Empty Fleet-owned groups can be reused when their previous revision has no
remaining intent. Unused empty groups are removed after reconciliation so they
do not retain unnecessary pool connections. Groups with custom power/tuning policy cannot be automatically
split or merged. Container suspension remains an independent interlock. Once groups are enabled,
Fleet labels its legacy sleep/wake action as applying to the default group only;
use the controller interface to suspend all groups.

Before I/O, Fleet commits the exact encrypted request and idempotency key. A
lost response or process restart replays that request. A confirmed revision
conflict permits a fresh observation and new plan; a timeout does not. Async
transfers retain their operation receipt until Frigate reports a terminal result.
An explicit `FAILED` result retires that transfer identity. Fleet observes current
ownership before planning another attempt with fresh revisions and a new key.
The workflow permits two automatic retries (three confirmed transfer failures
in total); this budget survives server restarts and is separate from retry backoff.
After the budget is exhausted, a fresh observation with no pending controller work
allows Fleet to mark the operation failed and release its write admission. Group bindings and shared assignment records are retained, with failed targets
excluded from future reconciliation until an explicit reviewed retry. Unapplied
legacy intents are removed. The operator can
then submit a new request. A timeout, lost response, or pending transfer never
consumes this budget or permits releasing unresolved controller work.

Each fresh reconciliation observation also checks the remaining sequence before
preparing another mutation. If controller changes make the assignment unsupported,
Fleet ends it as failed once its dispatched work is settled. Group bindings remain and shared targets are marked failed; unapplied legacy
intent is removed so a corrected request can be submitted. An unresolved saved write or pending transfer still retains its
identity and admission, even if a newer snapshot would reject the original plan.

One durable `device_write_attempt` spans the workflow. Its identity and epoch are
checked before dispatch, and active journals block configuration custody
reservation. Non-retrying binding transactions serialize each controller's
journal across replicas and hold the device route through dispatch. Permission,
profile availability, and exact board installation are checked again on every
step. A known transfer receipt continues to be polled read-only even if the
original operator loses permission or a selected board is removed. Once all
dispatched work is terminal, a fresh observation with no pending controller work
allows Fleet to end an interrupted request as failed and release its admission.
Removed or reinstalled boards lose the old installation's intent. Other applied
assignments and group bindings are preserved; unapplied desires are removed. A
currently authorized operator can then submit a new request.

Stale telemetry alone does not prove removal or erase a saved assignment. Boards
selected in the current request must remain available with fresh inventory.
Historical assignments still require the same installation, controller, and MAC,
but an unavailable historical board does not pause unrelated work. Before saving
and dispatching a step, Fleet also checks availability for historical boards that
the action affects: the moved MACs for a transfer, or all group members for pool
configuration and start. That affected membership is saved with the request so
retries retain the same checks even if later observations change.
Saved requests without a confirmed receipt remain protected: Fleet cannot safely
replay them after authority or installation changes, or forget them because of a
timeout. Activation's legacy stop is also retained until its outcome is known;
revocation never triggers an automatic resume. Restoring valid prerequisites can
resume unresolved work. There is no unsafe force-complete API.

The request ledger retains request identities after completion. Reusing an ID
with different settings is rejected, and replaying an old accepted request does
not reapply it after newer intent.

The activity log records activation and pool-assignment acceptance separately
from completion or failure. Each event commits with its workflow transition;
replays and worker retries do not duplicate it. Outcome events retain the
operator identity and site captured at acceptance, with the operation ID,
selected board IDs, and immutable pool revision reference. A failed outcome
does not imply that earlier applied steps were undone. Host pool-profile creation
uses the same audit event as customer profile creation. These records exclude
pool URLs, worker usernames, passwords, and encrypted credentials.

Fleet Node requires command protocol V6 for the original operations. Creating
a group for a nonempty pool configuration and replacing pools with explicit
controller pool IDs require V18. Activation and Return to Default also require
V18. Fleet reads the saved Default settings before stopping mining for
activation. Return reviews use committed Default settings. Fleet reads them
again before it accepts a return and while it verifies the transfer. If an
eligible review has no saved settings, Fleet refreshes them on demand.
Direct controllers use the same SDK operation contract.
The underlying MDK sources and pins are documented in [sdk/mdk](../sdk/mdk/README.md).

Module and hashboard **Edit pool** actions are disabled until mining groups are
supported and enabled with a fresh controller observation and no pending
operation. Unsupported controllers hide these actions and **Enable mining
groups**. The container activation action is also hidden after activation.
These menus refresh controller state while open; the assignment flow and server
continue to validate prerequisites before accepting changes.

Activation confirmation explains that Fleet stops mining before it enables
groups. Frigate creates the Default group while mining-group mode is disabled.
Fleet reads its saved intent before stopping and uses a durable request to enable
groups. Completion requires fresh confirmation that groups are enabled. When
ready, choose **Wake default mining group** from the Fleet container menu.
If activation is blocked after stopping, mining stays paused until the
prerequisite is resolved and activation can finish. While waiting,
known controller prerequisites produce specific guidance; unknown failures use a
neutral retry message without exposing raw controller errors. The open dialog
polls until it can display **Mining groups enabled**. Its button says **Close**
while work is pending and **Done** only after confirmation. Closing the dialog
does not cancel the durable workflow or its automatic retries; there is no
separate background completion notification.
