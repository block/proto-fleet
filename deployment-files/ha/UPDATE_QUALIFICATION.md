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

1. Verify the published `N` and `N+1` artifacts and their checksums. Install and
   qualify `N` using [QUALIFICATION.md](QUALIFICATION.md).
2. Record the container IDs and start times for etcd, Patroni, and PostgreSQL on
   every host.
3. Run `fleet-ha update N+1` on the passive application host. Verify it remains
   passive on `N+1` while the active host continues serving `N`. Through the
   VIP, read persisted data and submit a command to prove the temporary mixed
   `N` and `N+1` state is usable.
4. Run `fleet-ha update N+1 --complete` on the active host. Probe the VIP from
   another host and measure from its first failed response until it returns a
   successful `/health` response reporting `N+1`. Require less than 15 seconds.
   Verify the former active rejoins as passive on `N+1`.
5. Force an application failover in each direction. After each failover,
   require the VIP to serve `N+1` within 15 seconds and submit a successful
   command.
6. Confirm the etcd, Patroni, and PostgreSQL container IDs and start times from
   step 2 are unchanged. The application update must not replace or restart
   these services.
7. Reboot the two application/database hosts one at a time, restoring full
   readiness between reboots. Verify both return on `N+1`, retain persisted
   Fleet data, and can serve a command after failover.

## Results

| Gate | Required result | Duration | Result | Evidence |
| --- | --- | --- | --- | --- |
| Passive-first update | Passive runs `N+1`; active continues serving `N` | N/A | Pending | Pending |
| Mixed-version operation | Persisted read and command succeed while hosts run `N` and `N+1` | N/A | Pending | Pending |
| Active completion | VIP serves `N+1`; former active rejoins passive | `<15s` | Pending | Pending |
| Failover to peer | VIP serves `N+1`; command succeeds | `<15s` | Pending | Pending |
| Failover back | VIP serves `N+1`; command succeeds | `<15s` | Pending | Pending |
| Infrastructure preserved | etcd, Patroni, and PostgreSQL are not replaced or restarted | N/A | Pending | Pending |
| Reboot recovery | Both hosts return on `N+1` with persisted data and working failover | Pending | Pending | Pending |

## Verdict

**Pending.** Mark the adjacent update supported only when every gate passes and
attach the redacted evidence to this report.
