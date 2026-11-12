# Default pool configurations

Each operator organization and client/subclient library has one default while it
contains active configurations. The first configuration becomes the default.
Migration 1076 chooses the oldest active configuration for existing libraries,
with configuration ID as the tie breaker. Library managers can select another
default. Deleting a default promotes the oldest remaining configuration; deleting
the last leaves no default. Pending transitions prevent deletion of a configuration
they reference. Changing the default does not reconfigure existing assignments.

| Event | Destination has a default | Destination has no configuration |
| --- | --- | --- |
| Initial pairing or deliberate re-pairing | Apply the current owner's captured default | Preserve device settings |
| Authentication or endpoint recovery | Preserve existing assignment and device settings | Preserve device settings |
| Allocation/delegation | Apply the receiving account's captured default | Remove previous assignment and clear device pools |
| Recall/unallocation | Apply the parent account's default, or operator default at the root | Remove previous assignment and clear device pools |

Pairing and authentication are operator responsibilities, including for allocated
hardware. Repairing authentication does not change client/subclient allocation or
reapply either the operator's or the client's default. This applies to miners and
containers. A private pairing-cycle marker records the first successful pairing,
survives authentication failures, and resets on explicit unpairing. The marker and
the pairing pool transition commit in the same transaction. Each cycle has a
unique ID carried by its pool transitions, including expanded CT1 board work.
Explicit unpairing retires unclaimed pairing work. Accepted work rechecks that ID
before each new device mutation; old cycles cannot replace the next cycle's
default. Already-dispatched writes keep their evidence and recovery protections.

Migration 1076 initializes that marker for existing devices unless they are
explicitly unpaired. Historical pairing timestamps cannot reliably distinguish
first-time authentication from recovery, so those devices conservatively preserve
their settings on their next authentication. An operator can apply a configuration
manually, or explicitly unpair and re-pair to apply the current owner's default.
New pairing records after the upgrade apply the default on their first success,
including when credentials are required first. The migration itself queues no work.

Ownership updates and their pool-transition records commit together. Ownership
can change before device writes finish; old credentials may remain on offline or
unsupported hardware until the transition completes. No default change grants
manual pool-change permission. Automatic operations are attributed to the system,
and use the captured owner generation, configuration revision, physical identity,
and normal write-admission fencing. A later ownership change or explicit pool
assignment invalidates older queued work.

For CT1s, Fleet observes mining-group mode before preparing work and checks it
again during execution. Enabled groups use physical hashboard installations.
Disabled groups use one whole-container operation only when every installed board
has the same destination owner. If several pending policies cover that container,
preparation uses the newest valid captured policy and supersedes only older
unclaimed requests. Later library default changes do not alter that capture,
and accepted writes retain their normal execution and recovery protections.
Mixed ownership with groups disabled leaves pool
work waiting for mining groups to be enabled; Fleet does not activate groups
automatically. Unknown mode, incomplete inventory, group capacity limits, custom
group policies, and unresolved writes also prevent unsafe dispatch.

Clearing removes both stored and runtime settings where the driver supports
verified clearing. A board in the default mining group can be moved into a
stopped, pool-free group; the default group's configuration remains unchanged.
Other owners' boards must remain outside the affected set. Unsupported clearing
remains pending with an unavailable-readiness result, rather than reporting success.

The pool worker gives accepted updates priority over lifecycle preparation when
assigning its eight device-processing slots. Preparation uses spare capacity;
already-running work finishes normally before its slot can be reused.

Pending transitions appear in equipment pool status. Applied configurations retain
the existing operation results. With mining groups disabled and one owner across
the container, board rows also show its whole-container operation and link to its
details. This does not create individual board assignments. Enabled, pending, or
suspended mining-group observations, or an unfinished mining-group workflow, stop
that fallback; board-specific assignments keep their own status. Automatic clears
report completion only after empty stored and runtime settings are verified. Pre-dispatch read failures retry
only after the journal proves no mutation was sent. Failure recording and retry
scheduling commit together; polling also recovers recorded pre-dispatch failures
left by an interrupted process. Unclaimed clears repeat preparation using current
placement and mining-group mode before dispatch. A dispatched clear is never sent
again automatically: completed attempts can be observed again, while unknown
attempts require the existing administrator device-write recovery procedure.
Their operation details explicitly report “Administrator recovery required.”

Ownership changes remain available during pool work. An obsolete automatic board
clear ends only after saved requests have settled and fresh controller readback
confirms no pending work. Its transition is superseded and its controller
reservation released in one transaction, preserving the new owner's policy and
any device changes already made. Unknown requests retain their reservation.

Deleting a device supersedes its outstanding automatic transitions in the same
transaction as assignment retirement. This releases captured defaults for archival
when no live equipment still needs them, including defaults captured during CT1
pairing. Worker reconciliation also retires transitions left by earlier deletions.
Completed history and physical-write evidence remain intact; unknown writes still
protect the hardware if it is rediscovered.

Confirmed board removal retires automatic transitions that have not been claimed
by an operation or physical attempt. A replacement can return the outgoing board
to the operator after its installation has closed; that absent-board policy is
retained as retired history and does not pin the captured configuration. Stale or
incomplete inventory alone does not retire work. Current-equipment status reads
omit transitions without a resolvable display target, including before cleanup.

When a board with that retired work is installed again, Fleet consumes the retired
intent once and captures its current owner's current default, or a clear when no
default exists. It does not replay the old configuration or physical binding.
Controller moves reuse an already-queued ownership policy, and a subsequent
replacement allocation in the same inventory transaction supersedes any temporary
operator policy. Repeated inventory reports do not enqueue more work. Claimed or
uncertain writes keep their existing recovery workflow and write-fence protection.

## Upgrade and rollback

Stop Fleet API replicas, pool workers, telemetry/inventory writers, Fleet Node
writers, and scheduled database writers before applying migration 1076. Take a
coordinated application/database backup and rehearse its restore. This is a
stop-writers upgrade: the migration takes an exclusive organization table lock,
backfills defaults and pairing markers, and installs lifecycle triggers.

Restart only compatible Fleet builds after the checkpoint and startup checks
pass. Do not mix old and new writers in a rolling deployment. Older code does
not maintain the default invariant; for example, archiving a default through the
old archive path violates the new active-default constraint. An application-only
rollback is not supported: older builds also reject the newer checkpoint.

Transitions and operation history survive process restarts. Prefer fixing forward.
Downgrade locks lifecycle producers and the history table before checking for
retained work; the check and complete schema removal are one atomic statement.
Checkpoint 1076 refuses downgrade once any transition history exists, including
completed work. Restore the coordinated backup with its matching application if
a rollback is necessary; do not delete history or force the migration version.
