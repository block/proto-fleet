# Removing an equipment pool configuration

The Edit pool selector offers **Remove pool configuration** for equipment with a
Fleet assignment. Removal preserves the saved library configuration and history.
It does not change mining-group activation behavior.

The dialog reads current settings when opened. Optional hardware checks share a
ten-second deadline within the thirty-second review budget, leaving time to save
an unavailable review for assignment-only removal. Its default action clears all
configured pools and removes the assignment. **Keep the device’s current pool
settings** explicitly selects assignment-only removal, which sends no device
commands and prevents later library edits from propagating to that equipment.

Pending, applying, unresolved physical writes, and active mining-group operations
block removal. Not-verified or unavailable settings block clearing but permit
assignment-only removal when no outstanding write exists. Closing and reopening
the removal flow checks verification again. Mismatched settings may be cleared with an explicit warning
that this removes the device's current settings, including changes outside Fleet.

An unfinished removal does not block Proto's native curtailment controls, which
change mining/power state without replacing pools. Admission and dispatch check
the current driver binding. Pool updates and controls from other drivers remain
serialized because their implementation may change fallback pools.

## Durability and recovery

Each target has a removal review bound to the actor, organization/account,
assignment generation, and current physical installation/route. Reviews expire
after fifteen minutes. One password verification authorizes the exact reviewed selection,
bound to the initiating session and current credentials. Each confirmation still
rechecks permissions and physical identity. Module selections resolve to physical installations through the
existing assignment selector; future occupants never inherit removal requests.

Removal uses the shared pool configuration review, operation, and target records.
`ReviewAssignment` accepts an explicit removal target and captures fresh preflight;
`AuthorizeReviews` approves the exact selection once. `ConfirmReview` records either
`clear` (pool-free intent) or `unassign` (keep device settings). An omitted configuration
or an empty library configuration never means clearing.

Clearing queues a target for the normal pool worker. Confirmation does not wait for
hardware. The worker commits physical admission before sending the command.
Assignment-only removal completes its operation and retires the assignment in the
confirmation transaction, with no device work. `GetOperation` supplies progress for
both paths, using the same client monitor as assignments. An accepted applying or uncertain
clear can be checked through `ReviewRetry`; it only observes the original attempt
and cannot issue another clear until recovery establishes that a new review is safe.
The assignment remains in place until complete stored readback confirms all pool
settings are empty and runtime readback confirms that no pools remain active.
Whole-container clearing requires legacy pool mode throughout clearing and recovery.
Hashboard clearing uses the enabled mining-group workflow described below. During unfinished removal, other pool updates and conflicting
device commands are blocked. A failure never silently switches to assignment-only
removal. An uncertain or partially completed clear keeps its assignment and blocks
reconciliation from restoring pools.

Reopening an unfinished removal for the same target and actor restores its result.
Checking an accepted removal never directly repeats the clear command. For whole-device
clears, if the physical
attempt has completed, recovery reads stored and runtime settings: empty stacks complete
retirement; remaining settings produce a failed removal with its assignment kept.
An unresolved physical attempt must first follow [device write recovery](device-write-recovery.md).
Results whose physical attempt has unknown delivery explicitly require administrator
recovery; the removal dialog does not recommend repeated retries for those results.
Temporary verification failures after a completed physical call remain recheckable.

If ownership changes while a completed whole-device clear remains unfinished, the
current owner's next pool review can observe and supersede the obsolete removal.
It rechecks current equipment authority and the current binding, requires successful
readback, and preserves the retired or replacement assignment and physical attempt.
It does not send another clear or claim the old removal succeeded. Unknown physical
attempts and unavailable readback continue to block changes; the former owner cannot
use this path to inspect the new owner's equipment.

Hashboard removals with remaining runtime pools or unavailable verification persist
an actionable result and keep their workflow reservation and Fleet assignment.
The existing controller worker continues checking with its normal backoff and can
complete removal when verification succeeds, without another clear command.
Offline recovery ends that removal as interrupted while retaining the assignment; a new review uses the recovered binding.
Inspection also includes whole-device clear attempts that are already complete
but still have an unfinished removal. Their evidence template binds the exact
`pool_removal_id`. With all dispatchers stopped and operator configuration evidence,
recovery marks only that removal interrupted, retains the completed physical
attempt and Fleet assignment, records the audit, and advances the binding epoch
atomically. It does not require live readback or claim that pools were cleared.
Ordinary completed attempts and completed removals are not eligible.
Matching readback alone cannot establish that an earlier transport request finished.
A completed removal replay cannot retire a newer assignment.

Bulk requests retain per-target results. They are not a database/hardware transaction
across the entire selection. The hardware and Fleet database cannot share an atomic
transaction; the durable operation record permits recovery after lost replies or a crash
between hardware clearing and assignment retirement.

## Clear-all support

The `pool_clear` SDK capability opts a driver/firmware combination into explicit
whole-device empty-stack writes with complete stored readback. The local adapter
and Fleet Node both enforce the capability. Ordinary nonempty pool updates retain
their existing validation. Encrypted Fleet Node messages use an explicit
`clear_all` flag; an accidentally empty update is invalid.

Whole-device clearing through Fleet Node requires command protocol V9. Its
`VerifyPoolsCleared` observation reads every stored slot, including disabled slots
and credentials, plus the complete runtime pool list on the node. Only an explicit
verification result crosses the gateway; stored passwords are never included in
responses. Missing credentials or failed observations cannot prove emptiness.
Ordinary gateway pool telemetry is not used as proof because it can omit slots.
Older nodes require an update before this verification is available.

| Driver / mode | Current evidence | Clear through Fleet |
| --- | --- | --- |
| Virtual | Empty stacks are supported; complete stored settings are readable | Enabled |
| Antminer | Persistent last-pool removal is not established; [follow-up #424](https://github.com/btc-mining/proto-fleet-internal/issues/424) | Disabled |
| Proto rig with durable MDKv2 pool API | Stable-ID deletion persists before acknowledgement; complete configuration and runtime diagnostics are checked | Enabled after live preflight |
| MDKv1-only Proto rig | Removal acknowledges before saving and hides save failures; [follow-up #426](https://github.com/btc-mining/proto-fleet-internal/issues/426) | Disabled; assignment-only remains available |
| CT1 legacy priority-pool mode | Deletes all stored rows, including disabled pools; verifies empty storage and runtime diagnostics | Enabled while groups are disabled |
| CT1 enabled mining groups / hashboards | Reconciles selected boards into a stopped pool-free group; durable revision-guarded writes and board-covered runtime diagnostics | Enabled after live preflight |
| ASIC-rs | Delegates replacement to miner implementations; complete stored readback is currently unimplemented | Disabled |

To enable another firmware combination, establish and test its empty configuration
semantics, mining effect, actual write scope, fallback behavior, partial failures,
and complete readback. Advertise `pool_clear` only for that supported combination.
Do not infer clear support from `pool_config`, active-pool telemetry, or an API that
can delete an individual pool. Group clearing must not affect unselected siblings
or another tenant. Assignment-only removal remains available independently.

### Proto firmware evidence

Proto rig clearing uses `DELETE /api/v2/pools/{id}` for every stable pool ID,
then checks `GET /api/v2/pools` and `/api/v2/pools/diagnostics`. Normal nonempty
pool updates retain the existing MDKv1 path. There is no fallback to an empty
MDKv1 POST. Review checks live readback support, so older firmware remains
eligible for assignment-only removal even when its current pools are verified.

At miner-firmware commit `10bc4cdf7e3ab3cc8bc9a3558baf10f66d12cbb8`,
`services/mdk-server/internal/adapter/mcddlegacy/native_pools.go` delegates
MDKv2 deletion through stable identity and configuration-revision guards.
`crates/mcdd/src/pool_interface/pool_management.rs` persists the desired
configuration before changing runtime and acknowledging a guarded deletion.
Its legacy `handle_remove_pools` instead replies before `save_pools_config`,
which only logs persistence failures. Empty legacy telemetry cannot prove
persistence; supporting older firmware requires a stronger firmware contract.

CT1 uses MDKv2 gRPC. Frigate commit
`6acbeba0dcde568d33ca7121aee15a3b567abb4f` deletes the stored pool row before
reloading the pool manager; the manager accepts an empty enabled set and
removes runtime pools. The plugin retains its existing controller-selection,
priority-mode, mining-group, and runtime-override checks. An error after any
mutation remains uncertain; neither driver replays a partially completed clear.
Mining-group activation and migration of whole-container assignments remain deferred.

The fake Proto rig models the consumed MDKv2 list, diagnostics, and deletion
endpoints with stable IDs and write-only passwords. Its state is in memory;
fixture tests validate the protocol and isolation, not physical persistence.

## Clearing selected CT1 hashboards

The `mining_group_pool_clear` capability enables clearing physical hashboards when
mining groups are already enabled. The normal group reconciler treats clearing as
an explicit pool-free desired state, partitioned by account. For A/B on config 1
and C/D on config 2, clearing B leaves A on config 1, moves B into a stopped group
with zero pools, and leaves C/D on config 2. The firmware's default group remains
present even if it has no boards.

The planner reuses compatible empty groups and existing pool-free groups, or
reconfigures a managed group in place when all of its boards request clearing.
Capacity limits and custom operator policies still apply. Every affected board is
checked for current installation, inventory freshness, ownership and permissions.
The default group is never emptied through the hosted-group API.
Boards still in the firmware's default group are eligible only for assignment-only
removal: that scope has no supported diagnostics preflight. Fleet refuses clearing
before creating a group or moving a board, including if membership changes after
review. Clearing remains supported for boards in managed groups with working
runtime diagnostics. Preflight probes both controllers in a paired CT1 and
requires diagnostics coverage for every selected board, even when the primary
reports nonempty pools.

The existing encrypted workflow journal persists exact idempotency keys before
`ReplaceMiningGroupPools` (with zero pools), stop and transfer requests. Lost
acknowledgements retry that same request; transfers are polled to a terminal
receipt before another mutation is planned. The plugin checks stable stored
membership and pools plus runtime diagnostics covering every selected MAC, including
its peer controller when present. Missing board coverage or leftover runtime pools
keeps the Fleet assignment and the workflow reservation in place.

Only successful verification retires the Fleet assignment. The completed operation target
retains pool-free intent for that exact retired assignment generation and physical
installation, so later library edits do not restore pools. Explicit reassignment
replaces this intent and can move the board back into a configured group. Unused
Fleet-managed groups are then removed. Module removal progresses one board at a
time. The dialog polls accepted work and automatically submits the remaining
reviewed boards after each clear finishes, refreshing assignment tables on
completion. Polling is bounded. When monitoring stops, the dialog removes activity
spinners, explains how to check and continue with the same action, and labels later
boards as not started. Reopening unfinished work shows the same actionable state.
The action label stays unchanged; success or warning icons identify terminal or
uncertain results. Every target is named, including single-device selections.
Confirmation and Close use the standard modal actions and width. After removal
is accepted, Close and Escape remain available during polling; closing exits the
pool flow rather than returning to assignment. Closing or changing the account/session
stops client-side follow-up requests; an accepted workflow continues on the server.

Frigate `6acbeba0dcde568d33ca7121aee15a3b567abb4f` accepts an empty hosted-group
replacement transactionally and stores an idempotency receipt. Its group diagnostics
identify local observed MACs; an empty diagnostics response without the selected
board does not establish success. The CT1 simulator implements this readback in
memory and excludes disconnected boards; it does not establish firmware persistence.
