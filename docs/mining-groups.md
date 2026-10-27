# CT1 hashboard pool assignment

Fleet uses MDK v2 `mining-groups-v1` to assign exact physical hashboards to
independent pool configurations. Enable mining groups from the container actions
menu, then use **Edit pool** on modules or individual hashboards. The action uses the
same login confirmation and full-screen **Assign pools** page as miner actions. Module selections
are expanded into existing board identities before confirmation. Missing boards,
replacements, stale inventory, and boards belonging to another container are
rejected by the server.

Enable mining groups requires `miner:activate_mining_groups`, `miner:start_mining`,
and `miner:stop_mining` at the container's site. The activation permission is initially
granted to Admin (and SuperAdmin through the standard permission catalog). Fleet records
whether mining is running, stops it automatically, waits for board shutdown,
and enables groups. If mining was running, Fleet resumes the default group with
its existing pools; a stopped container stays stopped. Factory-test reservations
must still be released and every configured controller must satisfy Frigate's
activation prerequisites. The action does not override those prerequisites.

Pool assignment through mining groups requires `miner:update_pools`,
`miner:start_mining`, and `miner:stop_mining` at the container's site, in addition
to the existing read permissions. Both workflows check all required permissions
when accepting a request and again before dispatch. If access is revoked,
Fleet stops issuing writes and settles the interrupted workflow when its earlier
controller work is known to be terminal. Unresolved requests retain their identity
and write admission. State reads do not require
start/stop permissions. Ordinary miner pool updates continue to require
`miner:update_pools` without start/stop permissions; profile creation separately
requires `pool:manage`.

This implementation is available to operator sessions. Customer allocations and
API-key workflow authority are not implemented. Reporting-only account/module
allocations never authorize these writes. Host pool profiles occupy the host's
root account in the existing encrypted, immutable pool-revision store. Future
customer authorization must resolve authoritative board allocations and select
only revisions belonging to the authorized account before admitting intent.

## Database migration

Migration `1039` creates the complete mining-group schema, including retry,
activation, and transfer-failure state. Its down migration locks the tables and
refuses rollback while operations are active or failed, or while bindings or
pool intents remain.

Earlier development versions of this branch used separate migrations through
`1042`. Those checkpoints are not an upgrade path to the consolidated migration:
recreate disposable development databases. Preserve any database with real
operation history for an explicit migration decision; do not force its version
or delete state to make it start.

## Desired and observed state

`hashboard_pool_intent` records the desired revision for an exact board,
installation, MAC, and controller. `container_mining_group_state` records the
last successful controller observation, timestamp, Fleet group bindings, and
current operation. The observation contains groups, memberships, revisions,
and every primary/backup pool's nonsecret settings. Observed board assignments
are derived from memberships, rather than stored as another authoritative map.

Controller observations never overwrite desired intent. A mutation response is
not a fresh observation: Fleet reports completion only after all requested board
memberships and public pool settings are observed. Completion means the
configuration is applied, not that a pool is connected or producing accepted
shares. Pool outages do not trigger fallback to another group's configuration.

The Assign pools page selects saved Fleet pools in primary/backup order. Fleet
snapshots those selections into an encrypted immutable host profile, reusing the
revision for the same ordered saved pool IDs and update versions. Editing a saved
pool does not change an already accepted assignment; assigning it again captures
a new revision. Assignment submission verifies the current operator’s login
credentials again on the server.

Profiles currently support up to three ordered TCP Stratum V1 pools and fixed
worker usernames. Each Fleet profile revision has its own grouping identity;
equal credentials in two revisions do not merge their groups. Passwords are
write-only in MDK, encrypted at rest in Fleet, and encrypted for Fleet Node
transport. A password-only out-of-band change cannot be detected by readback.

## Reconciliation and recovery

Before accepting a pool assignment, Fleet projects its complete sequence with
the same planner used for reconciliation. Unsupported custom-policy moves and
insufficient group capacity reject the submission without committing new intent,
request identity, or write admission. Whole-group pool changes that preserve
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
allows Fleet to mark the operation failed and release its write admission. Applied
assignments and group bindings are retained; unapplied assignment intents are
removed so later requests cannot silently retry abandoned moves. The operator can
then submit a new request. A timeout, lost response, or pending transfer never
consumes this budget or permits releasing unresolved controller work.

Each fresh reconciliation observation also checks the remaining sequence before
preparing another mutation. If controller changes make the assignment unsupported,
Fleet ends it as failed once its dispatched work is settled. Applied assignments
and group bindings remain; unapplied intent is removed so a corrected request can
be submitted. An unresolved saved write or pending transfer still retains its
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

Fleet Node requires command protocol v6 for these operations. Older nodes reject
them before dispatch. Direct controllers use the same SDK operation contract.
The underlying MDK sources and pins are documented in [sdk/mdk](../sdk/mdk/README.md).

Module and hashboard **Edit pool** actions are disabled until mining groups are
supported and enabled with a fresh controller observation and no pending
operation. Unsupported controllers hide these actions and **Enable mining
groups**. The container activation action is also hidden after activation.
These menus refresh controller state while open; the assignment flow and server
continue to validate prerequisites before accepting changes.

Activation confirmation explains the automatic pause and conditional resume.
The original running state is durable before the stop command, so retries and
server restarts cannot overwrite it with a later stopped observation. Stop is
an idempotent desired-state command; activation and resume retain their exact
journaled request identities. Completion requires fresh confirmation of enabled
groups and, when needed, the default group's running intent (not proof of pool
connectivity or hashrate). If activation is blocked after stopping, mining stays
paused until the prerequisite is resolved and activation can finish. While waiting,
known controller prerequisites produce specific guidance; unknown failures use a
neutral retry message without exposing raw controller errors. The open dialog
polls until it can display **Mining groups enabled**. Its button says **Close**
while work is pending and **Done** only after confirmation. Closing the dialog
does not cancel the durable workflow or its automatic retries; there is no
separate background completion notification.
