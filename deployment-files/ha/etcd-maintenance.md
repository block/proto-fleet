# etcd capacity and retention

Both VIP and external-endpoint HA use periodic compaction with one-hour
retention. Successful cycles run hourly, so history can approach two hours.
Current keys and leases remain. Fleet reads current snapshots;
[Patroni 4.1.4](https://github.com/patroni/patroni/blob/v4.1.4/patroni/dcs/etcd3.py)
re-lists its cache after watch cancellation. Check other consumers before
adoption; this default is not a measured capacity guarantee or backup policy.

Compaction makes pages reusable but does not shrink the backend. Defrag reclaims
space and blocks that member's reads/writes. It remains operator-controlled;
never schedule per-host defrag jobs. See [etcd maintenance](https://etcd.io/docs/v3.6/op-guide/maintenance/).

## Monitor capacity

```bash
sudo /opt/proto-fleet/deployment/ha/fleet-ha etcd-status
```

This bounded, read-only check uses the installed CA and protected
`fleet-observer` credential. It works on the witness without Fleet or PostgreSQL.
Each member reports allocated `db_size_bytes`, `db_size_in_use_bytes` (current
data plus retained history), and `db_size_quota_bytes`. Allocated minus in-use
bytes is reusable space. Failed probes report `available: false`, zero sizes and
`warning: true`; zero does not mean free space.

Nonzero exit means a missing/unhealthy member, an etcd error/alarm, unknown
quota, or **at least 70% allocated quota**. At 85%, arrange urgent maintenance.
Inspect all three members. Investigate in-use growth after compaction; reclaim
fragmentation with defrag, not key deletion.

`fleet-ha status` exposes the same sizes. Measured pressure adds
`etcd_space_pressure` and clears `failover_ready`, triggering the existing HA
alert without changing `control_ready` or the active lease. Missing members
report quorum/redundancy degradation instead of measured pressure.
Target-release `update-preflight` rejects unhealthy etcd or pressure before the
installed binary stops an application, including upgrades from older binaries.
Recover capacity before retrying; pressure arising after preflight still blocks
rolling-update readiness.

**Rollout requires independent monitoring:** stage the new checker on the
monitoring host (prefer the witness; application updates do not update it).
Run every minute, retain JSON, and alert on failed/nonzero probes, results missing
for three minutes, and etcd auto-compaction errors. Test alert delivery with
Fleet stopped. Cloud host-monitor, alarm and log-routing wiring is a separate
`tf-protofleet` follow-up; the Fleet alert alone does not satisfy this gate.

## Adopt on an existing cluster

This separately approved operation changes installed infrastructure; application
updates do not. Use a verified release staged at `/secure/verified-release/ha`
with a matching-architecture checker. **Do not rerun the installer or copy the
release's whole Compose file**, which can upgrade datastore images.

1. Reserve one cluster-wide operator/window; exclude concurrent installers,
   restarts, failovers and defrags. Record member IDs, leader, terms, applied
   indices, versions, quota and disk headroom. Require three distinct healthy
   members in one cluster and Fleet/Patroni readiness on both database hosts.
   Verify a protected snapshot using the recovery procedure below. If near
   quota, recover capacity before waiting for automatic compaction.
2. On each host, back up and patch the installed Compose file:

   ```bash
   sudo cp -pn /etc/proto-fleet/ha/compose.yaml /etc/proto-fleet/ha/compose.before-retention.yaml
   sudo python3 /secure/verified-release/ha/scripts/configure-etcd-retention.py
   sudo diff -u /etc/proto-fleet/ha/compose.before-retention.yaml /etc/proto-fleet/ha/compose.yaml
   ```

   Expect only `--auto-compaction-mode=periodic` and
   `--auto-compaction-retention=1h`. The idempotent helper refuses unfamiliar or
   partial settings, preserves all other bytes and permissions, and never
   restarts services. Preserve the original backup on retries.
3. Recreate **one etcd member at a time, followers first**, re-reading leadership
   before each operation. Keep the installed image:

   ```bash
   sudo /opt/proto-fleet/deployment/ha/fleet-ha compose \
     --env-file /etc/proto-fleet/ha/node.env \
     --file /etc/proto-fleet/ha/compose.yaml \
     up -d --no-deps --no-build --pull never etcd
   ```

   Before proceeding, verify both running flags, all three authenticated
   endpoints healthy/caught up/alarm-free with agreed leadership, and Fleet and
   Patroni readiness. Reclassify followers if leadership changes; stop on any
   failure or ambiguous recovery. Never restart the HA systemd unit or run an
   unscoped `compose up`.
4. Wait at least one hour after the last recreation, then verify completed
   automatic compaction in logs and stable current keys/capacity. Wire and test
   independent monitoring above. Rollback restores the saved Compose and
   recreates members serially; disabling retention cannot restore discarded
   history. Current keys need no restore.

## Administrator credential

New guided VIP installs export a mode-0600 `etcd-root-password` in the protected
bundle directory printed by the installer. Copy it to encrypted off-host storage,
verify the copy, then remove that local export. Peer bundles and the installed
bootstrap password are still deleted. External-endpoint installs retain their
existing `offline/etcd-root-password` export.

Older guided installs may have deleted every root-password copy. In an approved
single-operator maintenance window, use a verified release's matching-architecture
`fleet-ha` on an installed host to rotate only the root password:

```bash
sudo sh -c 'umask 077; set -C; openssl rand -base64 32 > /root/etcd-root-recovered'
sudo /secure/verified-release/ha/fleet-ha recover-etcd-root /root/etcd-root-recovered
```

The replacement file must exist with mode 0600 and belong to the invoking user.
The command uses the installed JWT signing key to sign a one-minute root token
in memory, deriving the authentication revision from a verified observer login.
It verifies TLS,
requires authentication already enabled, changes the password, and verifies the
new login. It does not restart etcd, change service accounts, or print secrets.
Treat the signing key as administrator access; never copy it off-host.

Keep the replacement file even on failure: a timed-out change may have committed.
Verify that password before retrying; do not generate another one over the file.
Stop if the installed signing key is missing or does not match the cluster, or
if the installed observer credential no longer authenticates.
Do not disable authentication, reset data, or rerun bootstrap. Escrow the verified
password off-host and remove its local copy after the approved work. Existing
tokens become stale when the authentication revision changes; verify that
Fleet and Patroni re-authenticate and remain ready before proceeding.

## Recover capacity (separate approval)

Use matching etcd 3.6 `etcdctl`/`etcdutl` on a trusted administration host.
Set `ETCDCTL_CACERT` to the protected CA and `ETCDCTL_ENDPOINTS` to the three
HTTPS private endpoints. `--user root` prompts for the offline password; never
put it in arguments, enable shell tracing, or log key values. Keep monitoring
on the read-only observer. Use a private directory, `umask 077`, and sufficient
disk for snapshots and defrag temporary space. Serialize under the same
single-operator window as adoption.

1. Inspect authenticated `endpoint status --write-out=json`, `endpoint health`
   and `alarm list`; record revision and sizes. NOSPACE can fail the health
   write check: require responding members, agreed membership/leader and
   caught-up Raft indices instead. Stop for other faults.
2. Snapshot one healthy endpoint before compaction:

   ```bash
   etcdctl --user root --endpoints="$SNAPSHOT_ENDPOINT" snapshot save snapshot.db
   etcdutl snapshot status snapshot.db --write-out=table
   ```

   Verify hash, revision, key count and size; keep an encrypted off-host copy,
   never publish it. Stop if verification fails. This is not a restore drill;
   never reset the cluster or restore over a live member to reclaim space.
3. Choose `SAFE_REVISION` recorded at least one hour ago; otherwise record one
   and wait if capacity permits. At imminent NOSPACE, explicitly approve shorter
   retention only after checking all clients can re-list. Do not compact blindly
   to latest. Compact once:

   ```bash
   etcdctl --user root compact "$SAFE_REVISION"
   ```

4. Re-read leadership; defrag each follower separately, then the leader:

   ```bash
   etcdctl --user root --endpoints="$ONE_MEMBER_ENDPOINT" --command-timeout=60s defrag
   ```

   Never use `--cluster` or multiple endpoints. Require the other two members
   healthy before each operation; check space, leadership and Fleet/Patroni
   recovery between members. Stop on new faults; allow no concurrent restart or
   failover. A timeout does not stop server-side defrag: do not retry or advance
   until the member responds, catches up and logs confirm completion. Escalate
   if recovery cannot be established.
5. Recheck every member's capacity, membership, health and alarms. Disarm only
   if NOSPACE was present and all members have restored headroom:

   ```bash
   etcdctl --user root alarm disarm
   ```

   Verify writes, empty alarms, leader lease, Patroni synchronous replication,
   Fleet active/passive roles, `control_ready` and `failover_ready` on both hosts.
   HTTP success alone is insufficient. Adopt retention separately and retain
   before/after evidence without credentials or keys.
