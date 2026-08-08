# Proto Fleet HA update qualification

Use this report to qualify one adjacent released-version update, `N` to `N+1`,
on the supported three-host HA profile. Redact addresses, credentials,
certificates, device names, and customer data from committed evidence.

## Test identity

| Field | Value |
| --- | --- |
| Source release and commit (`N`) | Pending |
| Target release and commit (`N+1`) | Pending |
| Release artifacts and SHA-256 digests | Pending |
| Architecture and operating system | Pending |
| Started and completed | Pending |

## Procedure

Set `SOURCE_RELEASE` and `TARGET_RELEASE` to the recorded canonical release tags
and run Fleet commands as
`sudo /opt/proto-fleet/deployment/ha/fleet-ha update "$TARGET_RELEASE"`.

1. Verify the published `N` and `N+1` artifacts and their checksums. Confirm
   every `N+1` migration is expand-only and remains usable by `N`; reject
   drops, renames, narrowed types, and newly required values. Apply the
   migrations to a disposable copy of an `N` database and run `N`'s affected
   database and API integration tests against it. Install and qualify `N`
   using [QUALIFICATION.md](QUALIFICATION.md).
2. Record the container IDs and start times for etcd and Patroni on every host,
   plus `pg_postmaster_start_time()` on both PostgreSQL members.
3. Run the update command above on the passive application host. Verify it remains
   passive on `N+1` while the active host continues serving `N`. Through the
   VIP, read persisted data and submit a command to prove the temporary mixed
   `N` and `N+1` state is usable.
4. Prevent the updated passive from advertising the VIP, then append `--complete`
   to the update command on the active host. Verify the command
   times out without swapping deployments, restarts `N`, and restores a usable
   VIP and successful command within 60 seconds. Restore the passive host.
5. Retry the completion command. From a separate controller, use the external
   append-only recorder, gap rejection, and uncertainty-inclusive interval rules
   from [QUALIFICATION.md](QUALIFICATION.md). Continuously record direct active
   health on both hosts with monotonic timestamps at 100 ms or faster. In
   parallel, stream interface address events from both hosts and fail on any
   active or VIP overlap. Probe
   `/api-proxy/health/active`, `/api-proxy/health`, and an authenticated
   database-backed request at least once per second. On the same monotonic
   clock, measure from the last successful pre-handoff VIP probe until all
   three VIP probes succeed and `/api-proxy/health` reports
   `X-Proto-Fleet-Version: N+1`; require less than 15 seconds. Verify the former
   active rejoins as passive. Using a client loaded from `N` before handoff,
   perform a persisted read and a uniquely identified idempotent command against
   the `N+1` VIP.
6. Using a controllable test device on the current active host, stall one command
   after its exact queue row reaches PROCESSING so its successor remains PENDING.
   SIGSTOP that active `fleet-api`, let its lease expire, and wait for the passive
   peer to take over. Queue the old plugin result while its process is stopped,
   then SIGCONT it. Verify the original PROCESSING row is FAILED with the restart
   interruption reason and is never dispatched again, the old process exits, its
   stale database transition is rejected, exactly one terminal result remains,
   the PENDING successor dispatches, and later work for that device succeeds. Reuse the
   direct active-health and interface-address streams from step 5 through
   convergence and fail on any active or VIP overlap. Force a second failover
   in the other direction with the same ownership streams. For both moves,
   require the former active to reject an active-only request, measure usable
   service from the last successful pre-failover probe on the same monotonic
   clock, require less than 15 seconds, and submit a successful command.
7. Confirm the etcd and Patroni container IDs and start times, and both
   PostgreSQL postmaster start times, are unchanged from step 2. The application
   update must not replace or restart these services.
8. Confirm both updater binaries report `N+1` and their services and local
   status sockets are healthy. Reboot the two application/database hosts one
   at a time, restoring full readiness between reboots. Repeat the updater
   checks and verify both hosts retain Fleet data and can serve a command.

## Results

| Gate | Required result | Duration | Result | Evidence |
| --- | --- | --- | --- | --- |
| Migration compatibility | `N` integration tests pass against an `N` database migrated by `N+1` | N/A | Pending | Pending |
| Passive-first update | Passive runs `N+1`; active continues serving `N` | N/A | Pending | Pending |
| Mixed-version operation | Persisted read and command succeed while hosts run `N` and `N+1` | N/A | Pending | Pending |
| Failed takeover recovery | No swap; `N` restores usable service and command execution | `<60s` | Pending | Pending |
| Active completion | Active and database-backed probes serve `N+1`; former active rejoins passive; cached `N` client works against `N+1` | `<15s` | Pending | Pending |
| Handoff fencing | High-frequency direct health and interface event streams never show active or VIP overlap | N/A | Pending | Pending |
| Post-update stale completion | Interrupted row is FAILED without replay; resumed old transition is rejected; PENDING successor and later work succeed | N/A | Pending | Pending |
| Failover to peer | No active or VIP overlap; old active rejects; VIP serves `N+1`; command succeeds | `<15s` | Pending | Pending |
| Failover back | No active or VIP overlap; old active rejects; VIP serves `N+1`; command succeeds | `<15s` | Pending | Pending |
| Infrastructure preserved | etcd and Patroni identity plus PostgreSQL start times are unchanged | N/A | Pending | Pending |
| Updater refresh | Both updater binaries, services, and local status sockets run `N+1` | N/A | Pending | Pending |
| Reboot recovery | Both hosts return on `N+1` with persisted data and working failover | Pending | Pending | Pending |

## Verdict

**Pending.** Mark the adjacent update supported only when every gate passes and
attach the redacted evidence to this report.
