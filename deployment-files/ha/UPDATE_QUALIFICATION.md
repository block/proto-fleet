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

1. Verify the published `N` and `N+1` artifacts and their checksums. Confirm
   every `N+1` migration is expand-only and remains usable by `N`; reject
   drops, renames, narrowed types, and newly required values. Install and
   qualify `N` using [QUALIFICATION.md](QUALIFICATION.md).
2. Record the container IDs and start times for etcd and Patroni on every host,
   plus `pg_postmaster_start_time()` on both PostgreSQL members.
3. Run `fleet-ha update N+1` on the passive application host. Verify it remains
   passive on `N+1` while the active host continues serving `N`. Through the
   VIP, read persisted data and submit a command to prove the temporary mixed
   `N` and `N+1` state is usable.
4. Prevent the updated passive from advertising the VIP, then run
   `fleet-ha update N+1 --complete` on the active host. Verify the command
   times out without swapping deployments, restarts `N`, and restores a usable
   VIP and successful command within 60 seconds. Restore the passive host.
5. Retry `fleet-ha update N+1 --complete`. From another host, probe
   `/api-proxy/health/active`, `/api-proxy/health`, and an authenticated
   database-backed request at least once per second. Measure from the first
   failed probe until all three succeed and `/api-proxy/health` reports
   `X-Proto-Fleet-Version: N+1`; require less than 15 seconds. Verify the former
   active rejoins as passive on `N+1`.
6. Force an application failover in each direction. Use the same active and
   database-backed probes, require usable service within 15 seconds, and
   submit a successful command after each move.
7. Confirm the etcd and Patroni container IDs and start times, and both
   PostgreSQL postmaster start times, are unchanged from step 2. The application
   update must not replace or restart these services.
8. Confirm both updater binaries report `N+1` and their services and local
   status sockets are healthy. Reboot the two application/database hosts one
   at a time, restoring full readiness between reboots. Repeat the updater
   checks and verify both hosts retain Fleet data and can serve a command.
   readiness between reboots. Verify both return on `N+1`, retain persisted
   Fleet data, and can serve a command after failover.

## Results

| Gate | Required result | Duration | Result | Evidence |
| --- | --- | --- | --- | --- |
| Migration compatibility | `N+1` migrations are expand-only and usable by `N` | N/A | Pending | Pending |
| Passive-first update | Passive runs `N+1`; active continues serving `N` | N/A | Pending | Pending |
| Mixed-version operation | Persisted read and command succeed while hosts run `N` and `N+1` | N/A | Pending | Pending |
| Failed takeover recovery | No swap; `N` restores usable service and command execution | `<60s` | Pending | Pending |
| Active completion | Active and database-backed probes serve `N+1`; former active rejoins passive | `<15s` | Pending | Pending |
| Failover to peer | VIP serves `N+1`; command succeeds | `<15s` | Pending | Pending |
| Failover back | VIP serves `N+1`; command succeeds | `<15s` | Pending | Pending |
| Infrastructure preserved | etcd and Patroni identity plus PostgreSQL start times are unchanged | N/A | Pending | Pending |
| Updater refresh | Both updater binaries, services, and local status sockets run `N+1` | N/A | Pending | Pending |
| Reboot recovery | Both hosts return on `N+1` with persisted data and working failover | Pending | Pending | Pending |

## Verdict

**Pending.** Mark the adjacent update supported only when every gate passes and
attach the redacted evidence to this report.
