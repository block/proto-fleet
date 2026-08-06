# Proto Fleet HA lab profile

This experimental profile exists to qualify the three-host HA substrate before
it is integrated with the supported installer. It requires manual setup and is
not a production installation guide.

The topology is:

- `ha-a` and `ha-b`: Fleet, PostgreSQL + TimescaleDB managed by Patroni, and etcd;
- `ha-c`: etcd witness only;
- one stable LAN IPv4 address per host;
- one unused LAN IPv4 address shared by keepalived on `ha-a` and `ha-b`;
- host networking on PostgreSQL `5432`, Patroni `8008`, and etcd `2379`/`2380`.

Node joins, certificate rotation, upgrades, rollback, and restore automation are
intentionally deferred to later work.

Release bundles include the `fleet-ha` host utility in this directory. From a
source checkout, build the same binary with:

```bash
go build -o deployment-files/ha/fleet-ha ./server/cmd/fleet-ha
```

## Network contract

Patroni publishes the primary's PostgreSQL and REST addresses in etcd. Fleet
compares those addresses with the server reached through its PostgreSQL
connection, so every service must use the same host LAN address.

Use `network_mode: host` with no published-port remapping. Only each host's
stable LAN address is supported.

## Generate secrets

Run once on an offline administrator machine:

```bash
./fleet-ha generate-secrets \
  /secure/proto-fleet-ha-secrets \
  10.40.0.11 \
  10.40.0.12 \
  10.40.0.13 \
  10.40.0.100
```

The command creates an `offline` directory and one directory per host. Keep the
`offline` directory away from running hosts. Copy only the matching host
directory into that host's `HA_SECRETS_DIR`. Private keys, passwords, and
environment files must be owned by the deployment user with mode `0600`.

The command generates Fleet's authentication and encryption values once and
copies the same `fleet.env` into both Fleet-host bundles. It also gives each
Fleet host a proxy certificate for the virtual IP signed by the common HA
service CA. Preflight requires and validates these files on `ha-a` and `ha-b`.

## Bootstrap

Create `node.env` from `node.env.example` on each host:

| Host | `HA_NODE_NAME` | `HA_NODE_IP` |
| --- | --- | --- |
| first Fleet host | `ha-a` | `HA_DB_A_IP` |
| second Fleet host | `ha-b` | `HA_DB_B_IP` |
| witness | `ha-c` | `HA_DCS_C_IP` |

Use the same peer IPs on every host and only the keys documented in
`node.env.example`.

Install the `arping` binary on both Fleet hosts before preflight. Preflight uses
ARP duplicate-address detection to verify that the shared address is unused.

Run the clean-host preflight before starting services:

```bash
chmod 0600 node.env
./fleet-ha preflight node.env firewall.nft.tmpl
```

Preflight validates the clean host and loads its peer-restricted firewall.
Reboot and nftables-reload recovery are unsupported in this lab: services do
not restart automatically, and recovery requires a clean redeployment until
the supported installer owns firewall persistence and boot ordering.

The profile requires a trusted L2 segment. Host rules restrict VRRP to the
expected interface, Fleet-host source addresses, and local destination, but
they cannot authenticate raw-packet peers.

Load the database images on `ha-a` and `ha-b`. All three hosts require
registry access to pull the pinned etcd image before starting it:

```bash
docker load --input ../images/timescaledb.tar.gz
./fleet-ha compose --env-file node.env pull etcd
./fleet-ha compose --env-file node.env up -d etcd
```

After all members are healthy, temporarily copy
`offline/etcd-root-password` to exactly one of `ha-a` or `ha-b` and enable etcd
authentication:

```bash
chmod 0600 /secure/proto-fleet-ha-secrets/etcd-root-password
./fleet-ha bootstrap-etcd-auth \
  node.env \
  /secure/proto-fleet-ha-secrets/etcd-root-password
rm /secure/proto-fleet-ha-secrets/etcd-root-password
```

Run this only against clean etcd state. If it partially fails, recreate the new
etcd data instead of rerunning it against partial authentication state.

Start Patroni on `ha-a` and `ha-b`:

```bash
./fleet-ha compose --env-file node.env --profile database up -d --no-build patroni
```

Patroni creates the `fleet` database, login, and required extensions. Fleet
continues to own application migrations.

## Stable Fleet endpoint

Install keepalived and curl on both Fleet hosts. Render the host-specific
unicast VRRP configuration, then install the health check and configuration:

```bash
./fleet-ha render-keepalived \
  node.env \
  keepalived.conf.tmpl \
  /var/lib/proto-fleet/ha/keepalived/keepalived.conf
./fleet-ha render-keepalived \
  node.env \
  keepalived-systemd.conf.tmpl \
  /var/lib/proto-fleet/ha/keepalived/override.conf
sudo install -D -m 0755 \
  scripts/check-fleet-active.sh \
  /usr/local/libexec/proto-fleet/check-fleet-active
sudo install -d -m 0755 /run/proto-fleet-ha
sudo install -D -m 0644 \
  /var/lib/proto-fleet/ha/keepalived/keepalived.conf \
  /etc/keepalived/keepalived.conf
sudo install -D -m 0644 \
  /var/lib/proto-fleet/ha/keepalived/override.conf \
  /etc/systemd/system/keepalived.service.d/override.conf
sudo systemctl daemon-reload
sudo systemctl enable --now keepalived
```

Start Fleet on both Fleet hosts from the deployment directory. The deployment
`.env` supplies the database and other deployment settings. The generated
`fleet.env` is passed after it so the cluster-wide authentication and encryption
values override any independently created values. `node.env` supplies the local
HA identity. Use the HA wrapper so exported parent variables cannot override the
generated secrets or node identity:

```bash
./ha/fleet-ha compose \
  --env-file .env \
  --env-file /etc/proto-fleet/ha/fleet.env \
  --env-file ha/node.env \
  --file docker-compose.yaml \
  --file ha/fleet-compose.yaml \
  up -d --no-build fleet-api fleet-client
```

The overlay mounts only Fleet's CA, etcd password, and heartbeat directory into
`fleet-api`. Keepalived remains in backup while the HTTPS proxy or active-health
check is unavailable. When Fleet becomes active,
keepalived claims the VIP and refreshes the heartbeat. Fleet allows ten seconds
for initial VIP ownership, then exits and stops lease renewal if the VIP is
missing or the heartbeat is more than five seconds old. The database lease
remains the authority that makes Fleet active. Clients use the existing HTTPS
`fleet-client` proxy through the VIP; Fleet's API stays on loopback. Install the
generated `service-ca.crt` as a trusted CA on clients, then connect to the
virtual IP. Both Fleet hosts present a certificate valid for that shared
address.

## Fleet connection contract

Fleet connects to both database IPs using the `fleet` login:

```text
postgresql://fleet:<url-escaped-password>@10.40.0.11:5432,10.40.0.12:5432/fleet?target_session_attrs=read-write&sslmode=verify-full&sslrootcert=/etc/proto-fleet/ha/service-ca.crt
```

The etcd observer uses all three `https://<ip>:2379` endpoints with
`fleet-observer`, `fleet-etcd-password`, and `service-ca.crt`.

Patroni uses synchronous replication while a standby is available. If the
standby disappears, writes continue asynchronously so real-time control stays
available. Failover is not ready until synchronous replication is restored.

## Qualification

Run static profile checks in CI or locally:

```bash
./tests/test-profile.sh
```

After the cluster is healthy, run the real profile test from both database
hosts. Set `HA_PROFILE_MIGRATE=1` only for the first run:

```bash
PROTO_FLEET_HA_PROFILE_TEST=1 \
HA_PROFILE_MIGRATE=1 \
HA_PROFILE_FLEET_HOST=ha-a \
HA_PROFILE_ETCD_ENDPOINTS=https://10.40.0.11:2379,https://10.40.0.12:2379,https://10.40.0.13:2379 \
HA_PROFILE_ETCD_PASSWORD_FILE=/etc/proto-fleet/ha/fleet-etcd-password \
HA_PROFILE_SERVICE_CA=/etc/proto-fleet/ha/service-ca.crt \
HA_PROFILE_DEPLOYMENT_VERSION_FILE=/path/to/deployment/version.txt \
HA_PROFILE_DB_DSN="$DB_DSN" \
go test ./server/internal/ha -run '^TestProductionHAProfile$' -v
```

Repeat on `ha-b` without `HA_PROFILE_MIGRATE`. The emitted
`HA_PROFILE_EVIDENCE` line proves that the deployment artifacts, etcd leader,
Patroni primary, and connected PostgreSQL writer agree.
The later qualification and installer work owns the complete failure matrix
and operator runbooks.
