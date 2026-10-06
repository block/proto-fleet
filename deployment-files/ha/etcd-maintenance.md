# etcd capacity and retention

Applies to both VIP and external-endpoint HA. The shared etcd Compose service
targets one hour of revision history with periodic automatic compaction. Successful
cycles run hourly, so retained history can approach two hours between cycles. Current
keys and leases remain; this is not a key deletion or a backup policy. Fleet
reads current DCS snapshots and lease TTLs, without historical revisions.
[Patroni 4.1.4](https://github.com/patroni/patroni/blob/v4.1.4/patroni/dcs/etcd3.py)
rebuilds its prefix cache and resumes watching from the new revision after a
canceled watch. One hour leaves a reconnect window while bounding ordinary DCS
history. Additional consumers requiring older revisions must be checked before
adoption. It is a default for this profile, not a measured capacity guarantee.

Compaction makes old pages reusable; it does not shrink the backend file.
Defragmentation returns unused pages to the filesystem and blocks that member's
reads/writes while running. We do not schedule automatic defragmentation: steady
DCS churn can reuse compacted pages, and a background blocking operation needs
separate operational qualification. Never schedule per-host defrag jobs.
See [etcd 3.6 maintenance](https://etcd.io/docs/v3.6/op-guide/maintenance/).

## Check capacity without Fleet

```bash
sudo /opt/proto-fleet/deployment/ha/fleet-ha etcd-status
```

This bounded, read-only check works on any host, including the witness, without
Fleet or PostgreSQL. It uses the installed CA and `fleet-observer` credential
from protected files. JSON reports allocated `db_size_bytes`,
`db_size_in_use_bytes` (current data **plus retained history**), and
`db_size_quota_bytes` for each member. Their difference is reusable space.
A failed probe has `available: false`, zero measurements and `warning: true`;
zero is not free space. Missing members report quorum/redundancy degradation
without claiming measured space pressure.
Nonzero exit means a missing/unhealthy member, an etcd-reported error/alarm,
unknown quota, or **70% allocated quota** on any member. At 85%, arrange urgent
maintenance; do not wait for NOSPACE. Inspect all three members, not just an
average. In-use growth after compaction needs investigation of current key sizes,
write rate and retention; fragmentation alone calls for defrag, not deletion.

`fleet-ha status` includes the same member values and `etcd_space_pressure`.
Pressure makes `failover_ready` false and reaches the existing HA readiness
alert; it does not revoke Fleet's active lease or set `control_ready` false.
This warning can also block application updates that require failover readiness.

The Fleet alert pipeline is not an independent infrastructure monitor. Configure
an existing external host monitor to run `fleet-ha etcd-status` every minute on a
host with this binary (prefer the witness), alert on nonzero exit and on missing
results for three minutes, and retain the JSON. Alert separately on failed
scheduled probes and etcd auto-compaction errors in the container journal/logs.
Check monitoring with Fleet stopped during disposable/staging qualification.
Do not mark rollout complete until this independent alert is wired and tested.
For cloud deployments the smallest separate `tf-protofleet` follow-up is the
existing host-monitor job plus its failed/stale-result alarm and log routing;
this PR does not change cloud infrastructure or install a second monitoring
stack. Existing installations need the new checker binary explicitly staged on
the monitored host; the application updater does not update the witness.

## Adopt on an existing cluster

Application updates leave `/etc/proto-fleet/ha/compose.yaml` and the running etcd
container unchanged. **Do not copy a new release's whole infrastructure Compose
file**: that can silently upgrade etcd or Patroni. Retention adoption is a
separately approved infrastructure operation. Use a verified release containing
this helper and a matching-architecture `fleet-ha` binary; commands below use
`/secure/verified-release/ha` as that staged directory. Do not run the installer
again on an existing cluster.

1. Reserve one cluster-wide maintenance window/operator; prevent concurrent
   installers, restarts, failovers and defrags. Record all member IDs, leader,
   terms, applied indices, versions, quota and disk headroom. Require all three
   distinct members healthy in one cluster, plus both database hosts' Fleet and
   Patroni readiness. If close to quota, complete the recovery procedure below
   before waiting for automatic compaction. Take and verify a protected snapshot.
2. On each host, back up the installed Compose file, then patch **only that file**:

   ```bash
   sudo cp -p /etc/proto-fleet/ha/compose.yaml /etc/proto-fleet/ha/compose.before-retention.yaml
   sudo python3 /secure/verified-release/ha/scripts/configure-etcd-retention.py
   sudo diff -u /etc/proto-fleet/ha/compose.before-retention.yaml /etc/proto-fleet/ha/compose.yaml
   ```

   Expect only `--auto-compaction-mode=periodic` and
   `--auto-compaction-retention=1h`. The helper refuses unfamiliar/partial
   settings and is idempotent. It preserves the image, permissions and all other
   bytes; it does not restart anything. Preserve the original backup on retries.
3. Recreate **one etcd member at a time**, followers first, re-reading leadership
   before each operation. Use the installed image; never pull or build:

   ```bash
   sudo /opt/proto-fleet/deployment/ha/fleet-ha compose \
     --env-file /etc/proto-fleet/ha/node.env \
     --file /etc/proto-fleet/ha/compose.yaml \
     up -d --no-deps --no-build --pull never etcd
   ```

   Verify the running command contains both flags, and all three authenticated
   endpoints are healthy, caught up, alarm-free and agree on leadership before
   proceeding. Recheck Fleet and Patroni readiness. If leadership changes,
   reclassify remaining members; stop on any failure or ambiguous recovery.
   Never restart the HA systemd unit or run an unscoped `compose up` for adoption.
4. Allow at least one hour after the last recreation for the initial retention
   warm-up, then require a completed automatic compaction in the logs and stable
   current keys and capacity. Successful cycles run hourly; six minutes is the
   revision-sampling and failure-retry interval, not the compaction cycle. Stage the new checker on the monitoring host and wire the
   independent monitor above. Rollback restores the saved Compose and recreates
   members serially; discarded historical revisions cannot be restored by
   disabling retention. Current keys do not need restoring.

## Immediate capacity recovery (separate approval)

Use matching etcd 3.6 `etcdctl` and `etcdutl` binaries on a trusted administration
host. Set `ETCDCTL_CACERT` to the protected cluster CA and `ETCDCTL_ENDPOINTS` to
the three HTTPS private endpoints. Use `--user root` to **prompt** for the offline
root password; never use `root:password`, shell tracing, a password argument, or
publish snapshots. Routine monitoring must keep the read-only observer identity.
Run in a private directory with `umask 077` and enough disk for the snapshot and
defrag temporary space. Do not log key values.

1. Inspect authenticated `endpoint status --write-out=json`, `endpoint health`
   and `alarm list`. Record current revision and sizes. NOSPACE can make the
   health write check fail; require responding, agreed membership/leader and
   caught-up Raft indices instead, and stop for other faults. Serialize the
   entire procedure under the same single-operator maintenance window.
2. Snapshot **one** healthy endpoint and verify it before compaction:

   ```bash
   etcdctl --user root --endpoints="$SNAPSHOT_ENDPOINT" snapshot save snapshot.db
   etcdutl snapshot status snapshot.db --write-out=table
   ```

   Verify hash, revision, key count and size; retain an encrypted/off-host copy.
   A successful status check is not a restore drill. If snapshot/verification
   fails, stop. Never reset the cluster or restore over a live member to reclaim
   space.
3. Choose `SAFE_REVISION` from a recorded revision at least one hour old. If none
   exists and capacity permits, record one now and wait an hour. At imminent
   NOSPACE, explicitly approve a shorter recovery retention after checking all
   clients can re-list; do not blindly compact to latest. Compact once:

   ```bash
   etcdctl --user root compact "$SAFE_REVISION"
   ```

4. Re-read leadership, then defrag each follower individually, health/space
   checking after each, and the leader last:

   ```bash
   etcdctl --user root --endpoints="$ONE_MEMBER_ENDPOINT" --command-timeout=60s defrag
   ```

   Never use `--cluster` or multiple endpoints for this step. Require the other
   two members healthy before each operation. Re-read leadership before choosing
   each next member; stop on any new fault. A timeout/disconnection does **not**
   prove server-side defrag stopped: do not retry or move to another member until
   the affected member responds, catches up, and its completion is confirmed in
   logs. Escalate if that cannot be established. Check Patroni and Fleet recovery
   between members; allow no concurrent restart or failover.
5. Recheck all three sizes, quota headroom, membership, health and alarms. Only
   if NOSPACE was actually present and every member has restored capacity:

   ```bash
   etcdctl --user root alarm disarm
   ```

   Recheck writes, empty alarms, leader lease, Patroni synchronous replication,
   and Fleet active/passive, `control_ready` and `failover_ready` on both hosts.
   VIP or external endpoint HTTP success alone is insufficient. Adopt retention
   separately above and retain before/after evidence without credentials/keys.
