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

## Recover the Fleet lease after an etcd rebuild

The restored Fleet database can contain a lease that identifies the source etcd
cluster. Fleet must acquire a lease for the replacement cluster before it can
become active.

### Before you rebuild etcd

Close Fleet ingress. Stop Fleet in the source environment and disable its
automatic restart. Verify that it cannot write to the recovered database or
issue commands to Fleet Nodes. Keep that fence in effect when you reopen ingress
on the recovery cluster.

Record the source etcd cluster ID if it is available. Choose a new, unique
`--initial-cluster-token` and use the same value on all three replacement hosts.
Use fresh etcd data directories for the rebuild.

Set the token in `ha/compose.yaml` in each verified, extracted release before
you install. Use the patched release's `./ha/fleet-ha install` or
`./ha/fleet-ha install-prepared` on each host. The installer copies that file
to `/etc/proto-fleet/ha/compose.yaml` and starts etcd during installation. If
your rebuild uses the installed Compose file, set the token there before the
first etcd start. Etcd uses the token when it initializes its data directory.
Reusing the source token with the same peer URLs can reproduce the source
cluster ID.

### Before you restore the Fleet database

The HA installer starts Fleet as well as etcd and Patroni. On both recovered
database hosts, stop the `fleet-api` containers and disable any automatic
restart of Fleet. Keep etcd and Patroni running. Run the container check in
step 1 before you restore the database.

### After you restore the Fleet database

Follow these steps when Patroni is running on the replacement etcd cluster.

1. Verify that Fleet ingress is closed and the writer fences are in effect.
   Run this check on both recovered database hosts. Confirm that it lists no
   running Fleet API containers.

   ```bash
   sudo docker ps --filter label=com.docker.compose.service=fleet-api
   ```

   On both recovered database hosts, restore the source values for
   `AUTH_CLIENT_SECRET_KEY` and `ENCRYPT_SERVICE_MASTER_KEY` from protected
   storage. Set only these values in the installed `/etc/proto-fleet/ha/fleet.env`.
   Keep the file root-owned with mode `0600`. Verify that `DB_DSN` uses the
   replacement endpoints and service CA. Verify that its password matches the
   `fleet` role on the recovered PostgreSQL cluster.

   Keep the replacement HA passwords, TLS certificates and keys, and etcd JWT
   keys. The installed `/etc/proto-fleet/ha/fleet-etcd-password` must match the
   replacement etcd cluster's `fleet-observer` password.
2. Verify that the replacement etcd quorum is healthy. Verify that Patroni
   has one writable primary and the recovered standby is synchronous. From a
   trusted administration host, set `ETCDCTL_ENDPOINTS` to the replacement
   endpoints. Set `ETCDCTL_CACERT` to their CA certificate. Run this command
   and enter the etcd root password at the prompt.

   ```bash
   etcdctl --user root endpoint status --write-out=json
   ```

   Verify that all members report the same `header.cluster_id`. Record its full
   decimal value.

3. On either recovered database host, open the `ha/` directory of a verified
   release that includes the recovery command. The Fleet application release
   that matches the restored schema can be older. Read the stored and current
   cluster IDs.

   ```bash
   sudo ./fleet-ha recover-fleet-lease
   ```

   Verify that `current_etcd_cluster_id` matches the replacement cluster ID
   from step 2. Stop if the IDs differ. Stop if the replacement ID matches the
   recorded source ID. Also stop if the stored and current IDs match and
   `current_writer_generation` is lower than `stored_writer_generation`. This
   can indicate reuse of the source cluster ID.

   If you confirm reuse, keep ingress closed and the writers fenced. Repeat the
   approved HA rebuild and database restore. Use fresh etcd data directories
   and a new shared token as described above. Then resume at step 1.

   If no lease exists, continue at step 5. If the stored and current IDs match,
   verify that the replacement ID differs from the source ID before you
   continue at step 5. Use the recorded source ID or evidence of a successful
   rebind to this replacement cluster. If you cannot verify this, keep ingress
   closed and the writers fenced while you investigate.

   If the stored ID differs from the replacement ID, continue at step 4 when
   `lease_expired` is `true`. If the lease is still active, wait for expiry and
   repeat the inspection.

4. Verify the conditions in step 1 again. Set `OLD_CLUSTER_ID` to the stored
   ID from the report. Set `NEW_CLUSTER_ID` to the independently verified
   replacement ID from step 2. Run the recovery command.

   ```bash
   sudo ./fleet-ha recover-fleet-lease --apply --writers-fenced \
     --old-cluster-id "$OLD_CLUSTER_ID" --new-cluster-id "$NEW_CLUSTER_ID"
   ```

   The command verifies the writer and etcd leader, checks the lease, and
   keeps its epoch and expiry. A successful write returns `rebound: true`.
   If the command fails or the result is uncertain, keep ingress closed.
   Run step 3 again before any retry.
5. Start the Fleet release that matches the recovered schema. Keep normal
   ingress closed. Allow access from the operator and one enrolled canary Fleet Node. For a
   routed deployment, use a source-restricted load balancer route for the
   canary. For a LAN deployment, allow the operator and canary to reach the
   virtual IP. Run `fleet-ha status` on both hosts. Verify one active Fleet
   host, a passive peer, and `failover_ready: true`. Verify administrator
   login, the canary control stream, and fresh telemetry before reopening
   normal ingress.

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
