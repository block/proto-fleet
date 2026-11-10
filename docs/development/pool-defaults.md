# Default pool configurations

Each operator organization and client/subclient library has one default while it
contains active configurations. The first configuration becomes the default.
Migration 1073 chooses the oldest active configuration for existing libraries,
with configuration ID as the tie breaker. Library managers can select another
default. Deleting a default promotes the oldest remaining configuration; deleting
the last leaves no default. Pending transitions prevent deletion of a configuration
they reference. Changing the default does not reconfigure existing assignments.

| Event | Destination has a default | Destination has no configuration |
| --- | --- | --- |
| Pairing | Apply the current owner's captured default | Preserve device settings |
| Allocation/delegation | Apply the receiving account's captured default | Remove previous assignment and clear device pools |
| Recall/unallocation | Apply the parent account's default, or operator default at the root | Remove previous assignment and clear device pools |

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
has the same destination owner. Mixed ownership with groups disabled leaves pool
work waiting for mining groups to be enabled; Fleet does not activate groups
automatically. Unknown mode, incomplete inventory, group capacity limits, custom
group policies, and unresolved writes also prevent unsafe dispatch.

Clearing removes both stored and runtime settings where the driver supports
verified clearing. A board in the default mining group can be moved into a
stopped, pool-free group; the default group's configuration remains unchanged.
Other owners' boards must remain outside the affected set. Unsupported clearing
remains pending with an unavailable-readiness result, rather than reporting success.

Pending transitions appear in equipment pool status. Applied configurations retain
the existing operation results. Automatic clears report completion only after
empty stored and runtime settings are verified. Pre-dispatch read failures retry
only after the journal proves no mutation was sent. A dispatched clear is never
sent again automatically: completed attempts can be observed again, while unknown
attempts require the existing administrator device-write recovery procedure.
Transitions and operation history survive process restarts. Rollback of checkpoint
1075 refuses to discard retained transition history; use a coordinated backup.
