# Proto Fleet HA update qualification

Use this report to qualify one adjacent update, `N` to `N+1`, on the supported
three-host HA profile. Redact addresses, credentials, certificates, device
names, and customer data from committed evidence.

Run the full procedure and keep a separate result table for every architecture
enabled by the release. The initial profile requires both amd64 and arm64 to
pass before promotion.

Publish the final `N+1` tag and assets at the fixed official release origin as
a GitHub prerelease. HA does not offer UI-triggered updates, so only an explicit
local operator command can select this candidate. After every gate passes,
promote that same release without changing its tag or assets. A failed report
leaves the release marked prerelease and unsupported.

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

1. Verify the stable `N` and prerelease `N+1` artifacts and their checksums. Confirm
   the target was built from a reviewed
   `qualified-update-from.txt` containing exactly `SOURCE_RELEASE`, and that
   its manifest-covered `ha_update_from` field matches. An empty file makes the
   target clean-install-only. Confirm
   every `N+1` migration is expand-only and remains usable by `N`; reject
   drops, renames, narrowed types, and newly required values. Apply the
   migrations to a representative production-size copy of an `N` database and
   run `N`'s affected database and API integration tests against it. Require
   completed baseline reports from [QUALIFICATION.md](QUALIFICATION.md) for the
   exact `N` and `N+1` artifacts, then install `N` for this procedure and load
   a sanitized production-scale `N` dataset. Record table sizes and row counts.
   The passive-first update below must run its actual migrations against this
   dataset while the sustained reads, writes, and commands in step 3 continue;
   fail on a migration lock or probe gap beyond the stated bounds.
2. Record the etcd container ID and start time on all three hosts, the Patroni
   container ID and start time on both database hosts, and
   `pg_postmaster_start_time()` on both PostgreSQL members.
3. Before running the update command on the passive application host, start
   the external append-only recorder, gap rejection, and uncertainty-inclusive
   interval rules from [QUALIFICATION.md](QUALIFICATION.md). Confirm initial
   snapshots and gap-free streams, then continuously record direct active
   health on both hosts with monotonic timestamps at 100 ms or faster. In
   parallel, stream interface address events from both hosts and fail on any
   active or VIP overlap. Also start
   authenticated database-backed reads and uniquely identified idempotent
   commands through the VIP at least once per second. Give each request a
   two-second deadline, record command submission and terminal-result times,
   and require each background command to reach SUCCESS exactly once within 60
   seconds. FAILED is permitted only for the deliberately interrupted
   PROCESSING commands in steps 4 and 7, with the expected restart interruption
   reason.
   Before the planned handoff, fail if successful reads or command
   submissions are more than three seconds apart. Continue these probes through
   step 4. Verify the active host continues serving `N` throughout the migration
   and mixed-version window.
4. Keep the recorder and probes from step 3 running. Before the handoff, hold
   one `N` command in PROCESSING and one
   successor in PENDING. Then run the completion command. Probe
   `/api-proxy/health/active`, `/api-proxy/health`, and an authenticated
   database-backed request at least once per second. On the same monotonic
   clock, measure from the last successful pre-handoff VIP probe until all
   three VIP probes succeed and `/api-proxy/health` reports
   `X-Proto-Fleet-Version: N+1`; require less than 15 seconds. Verify the former
   active rejoins as passive. After takeover, require the PROCESSING row to
   fail once without replay, the PENDING row to dispatch on
   `N+1`, and both accepted IDs to reach exactly one terminal result within 60
   seconds. Using a client loaded from `N` before handoff,
   perform a persisted read and a uniquely identified idempotent command against
   the `N+1` VIP. Before reinstalling for step 5, confirm the infrastructure
   identities from step 2 are unchanged.
5. Reinstall the clean `N` baseline and update the passive host to `N+1`. On the
   active host, create the root-owned
   `/var/lib/proto-fleet-updater/qualification-pause-before-ha-stop` file and
   arrange to remove it on every exit. Start `--complete` and wait until updater
   status enters the activating phase; the barrier now holds the old application
   open after final preflight. Stop the updated peer's Fleet application and
   confirm it is unavailable, then remove the barrier. Require takeover to time
   out and the updater to restart release `N` automatically. Within 60 seconds,
   require the old host to serve the VIP, a persisted read, and a successful
   command, with no manual recovery command remaining. Restart the `N+1` peer as
   passive and restore full readiness before continuing.
6. Reinstall the clean `N` baseline and repeat steps 2 and 3 for a separate
   mixed-version failover run. Update the passive host to `N+1`, then terminate
   the active `N` Fleet process instead of running `--complete`. Require the
   `N+1` peer to take over within 15 seconds with no possible active or VIP
   overlap,
   recover interrupted command state, and serve a successful command. Restart
   the `N` host as passive and update it with the ordinary passive command.
   Require both hosts to run `N+1`, full failover readiness, and unchanged
   infrastructure identities from this run's step 2.
7. Using a controllable test device on the current active host, stall one command
   after its exact queue row reaches PROCESSING so its successor remains PENDING.
   SIGSTOP that active `fleet-api`, let its lease expire, and wait for the passive
   peer to take over. Queue the old plugin result while its process is stopped,
   then SIGCONT it. Verify the original PROCESSING row is FAILED with the restart
   interruption reason and is never dispatched again, the old process exits, its
   stale database transition is rejected, exactly one terminal result remains,
   the PENDING successor dispatches, and later work for that device succeeds. Reuse the
   direct active-health and interface-address streams from step 3 through
   convergence and fail on any active or VIP overlap. Force a second failover
   in the other direction with the same ownership streams. For both moves,
   require the former active to reject an active-only request, measure usable
   service from the last successful pre-failover probe on the same monotonic
   clock, require less than 15 seconds, and submit a successful command.
8. Confirm the etcd and Patroni container IDs and start times, and both
   PostgreSQL postmaster start times, are unchanged from step 2. The application
   update must not replace or restart these services.
9. Confirm both updater binaries report `N+1` and their services and local
   status sockets are healthy. Reboot the two application/database hosts one
   at a time, restoring full readiness between reboots. Repeat the updater
   checks and verify both hosts retain Fleet data and can serve a command.
10. From a clean `N` baseline, run three separate interruption cases. First,
    use the root-owned pre-stop barrier from step 5 and SIGKILL the updater
    after final preflight; the old active must keep serving and the updater must
    restart cleanly. Second, power-cycle the updating host while
    `/var/lib/proto-fleet-updater/activation-swap.json` and
    `/opt/proto-fleet/deployment.previous` prove a swap is in progress. Third,
    SIGKILL the updater while
    `/usr/local/libexec/proto-fleet/proto-fleet-updater.handoff` exists during
    self-replacement. Repeat a case if its durable marker clears before the
    fault lands. After each restart, require one valid deployment directory,
    no pending recovery marker, a healthy updater socket, restored control and
    failover readiness, and either the intact old release or fully verified
    target release according to the recorded recovery state. When recovery
    retains the target release, require the installed updater's `--version` to
    report the same target version; startup repair must not leave the previous
    updater paired with the new application.

## Results

| Gate | Required result | Duration | Result | Evidence |
| --- | --- | --- | --- | --- |
| Baseline qualification | Exact `N` and `N+1` artifacts pass the clean-install HA report | N/A | Pending | Pending |
| Migration compatibility | `N` integration tests pass against an `N` database migrated by `N+1` | N/A | Pending | Pending |
| Live production-scale migration | The real passive updater migrates the recorded sanitized dataset without exceeding traffic or lock bounds | N/A | Pending | Pending |
| Passive-first update | Passive runs `N+1`; active continues serving `N` | N/A | Pending | Pending |
| Mixed-version operation | Continuous persisted reads and commands succeed throughout migration while hosts run `N` and `N+1` | N/A | Pending | Pending |
| Active completion | Active and database-backed probes serve `N+1`; former active rejoins passive; cached `N` client works against `N+1` | `<15s` | Pending | Pending |
| Takeover timeout recovery | Updated peer is unavailable; old `N` restarts automatically and serves reads and commands | `<60s` | Pending | Pending |
| Handoff fencing | High-frequency direct health and interface event streams never show active or VIP overlap | N/A | Pending | Pending |
| Mixed-version forced takeover | `N+1` takes over safely while its peer is `N`; the old host then completes through the ordinary passive update | `<15s` | Pending | Pending |
| Post-update stale completion | Interrupted row is FAILED without replay; resumed old transition is rejected; PENDING successor and later work succeed | N/A | Pending | Pending |
| Failover to peer | No active or VIP overlap; old active rejects; VIP serves `N+1`; command succeeds | `<15s` | Pending | Pending |
| Failover back | No active or VIP overlap; old active rejects; VIP serves `N+1`; command succeeds | `<15s` | Pending | Pending |
| Infrastructure preserved | etcd and Patroni identity plus PostgreSQL start times are unchanged | N/A | Pending | Pending |
| Updater refresh | Both updater binaries, services, and local status sockets run `N+1` | N/A | Pending | Pending |
| Updater interruption recovery | Pre-activation, post-swap, and self-update faults recover automatically | N/A | Pending | Pending |
| Reboot recovery | Both hosts return on `N+1` with persisted data and working failover | Pending | Pending | Pending |
| Promotion drift guard | Source is still latest stable; target tag, commit, and asset digests still match this report | N/A | Pending | Pending |

## Verdict

**Pending.** Mark the adjacent update supported only when every gate passes on
both amd64 and arm64 and attach the redacted evidence to each report.
Immediately before promotion, re-fetch both releases and re-download every
target asset into a clean directory. Abort unless `SOURCE_RELEASE` is still the
latest stable release, the target is still a prerelease, its tag resolves to
the recorded commit, and its asset names and SHA-256 digests exactly match the
report. Then promote it with
`gh release edit "$TARGET_RELEASE" --prerelease=false --latest`. Do not rebuild,
retag, replace assets, or promote after any drift.
