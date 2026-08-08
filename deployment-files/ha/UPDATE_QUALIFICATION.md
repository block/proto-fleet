# Proto Fleet HA update qualification

Use this report for one adjacent released-version update, `N` to `N+1`, on the
qualified three-host profile. Do not use development builds or skip a released
version. Redact addresses, credentials, certificates, device names, and
customer data from committed evidence.

## Test identity

| Field | Value |
| --- | --- |
| Source release (`N`) | Pending |
| Source commit SHA | Pending |
| Source archive filename and SHA-256 | Pending |
| Source release workflow run | Pending |
| Target release (`N+1`) | Pending |
| Target commit SHA | Pending |
| Target archive filename and SHA-256 | Pending |
| Target release workflow run | Pending |
| Architecture | arm64 |
| Operating system | Debian 13 |
| Started | Pending |
| Completed | Pending |

## Procedure

1. Download each arm64 archive and its `.sha256` sidecar from the published
   release, verify the sidecar, and record the exact filename and digest above.
   For both extracted archives, verify `deployment-manifest.sha256`, confirm
   that `version.txt` names the recorded release and commit, and link the
   release workflow run whose head SHA matches that commit. Install and qualify
   release `N` using [QUALIFICATION.md](QUALIFICATION.md). Confirm `N` includes
   the `fleet-ha update` and `update --complete` protocol; releases that predate
   this supported HA update baseline are out of scope.
2. Record the running etcd and Patroni/PostgreSQL container IDs on all hosts.
3. On the passive database host, run `fleet-ha update N+1` and verify that it
   remains passive on `N+1` while the active host continues serving `N`. Fail
   if the operation reports `host updater refresh needs attention`. Require
   `/usr/local/libexec/proto-fleet/proto-fleet-updater --version` to print
   `N+1`, `proto-fleet-updater.service` to be active, and an authenticated local
   request to `/v1/status` over `/run/proto-fleet-updater/updater.sock` to
   succeed. Retain the redacted operation log.
4. During the mixed-version window, use the authenticated VIP API to read
   existing database-backed configuration, submit a command, and verify its
   completion while `N` remains active.
5. On the updated passive, capture its running `fleet-api` and `fleet-client`
   container IDs, then stop keepalived so that host cannot advertise the VIP.
   Start a background watcher that polls the uncached
   `http://127.0.0.1:4000/health/active` endpoint at least every 100 milliseconds
   and stops those exact containers as soon as it reports active. Continuously
   probe the VIP from another host and run `fleet-ha update N+1 --complete` on
   the active. Verify the updated peer never serves the VIP and the operation
   log shows active validation and application stop, followed by the 35-second
   takeover timeout and release `N` restart without a deployment swap. Confirm
   `N` reacquires the VIP and can complete a command within 60 seconds of the
   first failed VIP probe. Start keepalived, restart the updated peer with
   `fleet-ha app-start N+1 passive`, and restore full readiness before
   continuing. This recovery bound is separate from the 15-second successful
   handoff target.
6. With the target artifacts and build cache warmed by step 5, block a
   PROCESSING command on one controlled test device and enqueue a second command
   behind it so per-device ordering keeps that row PENDING. Record both
   immutable queue row IDs, retry `fleet-ha update N+1 --complete`, and sample
   both rows throughout the retry. The gate fails if either state changes before
   `fleet-api` stops; retain a final timestamped sample no more than one second
   before that stop. Measure until the VIP serves `N+1`.
7. Verify those exact rows on `N+1`: the PENDING row is dispatched, the
   interrupted PROCESSING attempt reaches its documented terminal state, and a
   later command for that device succeeds. Verify the former active is passive
   on `N+1`, then repeat the updater version, service, socket, warning, and
   redacted-log checks from step 3 on this host.
8. Force an application failover to the former active and back to its peer.
   Submit a command through the VIP after each failover and verify it succeeds.
9. Compare the infrastructure container IDs recorded in step 2. Prove that the
   application update did not replace etcd, Patroni, or PostgreSQL.
10. Reboot both database hosts, one at a time, restoring full readiness between
    reboots. Confirm both return on `N+1` with the same etcd membership,
    Patroni cluster, and persisted Fleet data. On each host, repeat the updater
    version, service, and socket checks. Container IDs may change during reboot.

## Results

| Gate | Required result | Duration | Result | Evidence |
| --- | --- | --- | --- | --- |
| Passive-first update | Passive runs `N+1`; active continues serving `N` | N/A | Pending | Pending |
| Mixed-version window | Authenticated reads, writes, and command completion work while peers run `N` and `N+1` | Pending | Pending | Pending |
| Failed takeover recovery | Completion times out before swap; `N` restarts and serves commands within 60s | Pending | Pending | Pending |
| Active completion | VIP serves `N+1` within 15s | Pending | Pending | Pending |
| PENDING command handoff | Immutable pre-stop row crosses from `N` to `N+1` as PENDING; new active dispatches it | Pending | Pending | Pending |
| PROCESSING command handoff | Immutable pre-stop row crosses from `N` to `N+1` as PROCESSING; attempt finishes terminally and later work resumes | Pending | Pending | Pending |
| Failover to peer | VIP serves `N+1` within 15s | Pending | Pending | Pending |
| Failover back | VIP serves `N+1` within 15s | Pending | Pending | Pending |
| Command execution | Command succeeds after both failovers | N/A | Pending | Pending |
| Infrastructure preservation | Application update leaves etcd, Patroni, and PostgreSQL container IDs unchanged | N/A | Pending | Pending |
| Host updater refresh | Both protected updater binaries report `N+1`; services and local status APIs survive reboot; no refresh warning remains | N/A | Pending | Pending |
| Reboot recovery | Both hosts return on `N+1` with DCS, database, and Fleet data intact | Pending | Pending | Pending |

## Verdict

**Pending.** The HA application update path is qualified only when every result
above is `PASS`, every successful handoff interruption is below 15 seconds,
failed-takeover recovery is below 60 seconds, and the report binds both exact
archives to their published digests, packaged commits, and release workflow
runs.
