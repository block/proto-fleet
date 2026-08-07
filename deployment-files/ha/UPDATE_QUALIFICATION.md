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
| Target release (`N+1`) | Pending |
| Target commit SHA | Pending |
| Architecture | arm64 |
| Operating system | Debian 13 |
| Started | Pending |
| Completed | Pending |

## Procedure

1. Install and qualify release `N` using [QUALIFICATION.md](QUALIFICATION.md).
   Confirm `N` includes the `fleet-ha update` and `update --complete` protocol;
   releases that predate this supported HA update baseline are out of scope.
2. Record the running etcd and Patroni/PostgreSQL container IDs on all hosts.
3. On the passive database host, run `fleet-ha update N+1` and verify that it
   remains passive on `N+1` while the active host continues serving `N`.
4. During the mixed-version window, use the authenticated VIP API to read
   existing database-backed configuration, submit a command, and verify its
   completion while `N` remains active.
5. Stop keepalived on the updated passive, then run
   `fleet-ha update N+1 --complete` on the active. Verify it times out before
   swapping, restarts release `N`, serves the VIP within 45 seconds, and can
   complete a command. Restart keepalived and restore full readiness. This
   recovery bound is separate from the 15-second successful-handoff target.
6. Retry `fleet-ha update N+1 --complete`. Measure from the first failed VIP
   health probe until the VIP serves `N+1`.
7. Verify the former active is now passive on `N+1`, then force an application
   failover in each direction.
8. Submit a command through the VIP after each failover and verify it succeeds.
9. Reboot both database hosts, one at a time, restoring full readiness between
   reboots. Confirm both return on `N+1`.
10. Compare the recorded infrastructure container IDs and prove that etcd,
   Patroni, and PostgreSQL were not replaced by the application update.

## Results

| Gate | Required result | Duration | Result | Evidence |
| --- | --- | --- | --- | --- |
| Passive-first update | Passive runs `N+1`; active continues serving `N` | N/A | Pending | Pending |
| Mixed-version window | Authenticated reads, writes, and command completion work while peers run `N` and `N+1` | Pending | Pending | Pending |
| Failed takeover recovery | Completion times out before swap; `N` restarts and serves commands within 45s | Pending | Pending | Pending |
| Active completion | VIP serves `N+1` within 15s | Pending | Pending | Pending |
| Failover to peer | VIP serves `N+1` within 15s | Pending | Pending | Pending |
| Failover back | VIP serves `N+1` within 15s | Pending | Pending | Pending |
| Command execution | Command succeeds after both failovers | N/A | Pending | Pending |
| Reboot recovery | Both database hosts return on `N+1` | Pending | Pending | Pending |
| Infrastructure preservation | etcd, Patroni, and PostgreSQL container IDs are unchanged | N/A | Pending | Pending |

## Verdict

**Pending.** The HA application update path is qualified only when every result
above is `PASS`, every successful handoff interruption is below 15 seconds,
failed-takeover recovery is below 45 seconds, and the report identifies both
released artifacts and commits.
