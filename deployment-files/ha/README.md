# Proto Fleet HA lab profile

This experimental profile exists to qualify the three-host HA substrate before
it is integrated with the supported installer. It requires manual setup and is
not a production installation guide.

The topology is:

- `ha-a` and `ha-b`: PostgreSQL + TimescaleDB managed by Patroni, plus etcd;
- `ha-c`: etcd witness only;
- one stable LAN IPv4 address per host;
- host networking on PostgreSQL `5432`, Patroni `8008`, and etcd `2379`/`2380`.

VIP routing, node joins, certificate rotation, upgrades, rollback, and restore
automation are intentionally deferred to later work.

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
  10.40.0.13
```

The command creates an `offline` directory and one directory per host. Keep the
`offline` directory away from running hosts. Copy only the matching host
directory into that host's `HA_SECRETS_DIR`. Private keys, passwords, and
`node.env` must be owned by the deployment user with mode `0600`.

## Bootstrap

Create `node.env` from `node.env.example` on each host:

| Host | `HA_NODE_NAME` | `HA_NODE_IP` |
| --- | --- | --- |
| first database host | `ha-a` | `HA_DB_A_IP` |
| second database host | `ha-b` | `HA_DB_B_IP` |
| witness | `ha-c` | `HA_DCS_C_IP` |

Use the same peer IPs on every host and only the keys documented in
`node.env.example`.

Run the clean-host preflight before starting services:

```bash
chmod 0600 node.env
./fleet-ha preflight node.env firewall.nft.tmpl
```

Preflight validates the clean host and loads its peer-restricted firewall.

Load the database images on the two database hosts. All three hosts require
registry access to pull the pinned etcd image before starting it:

```bash
docker load --input ../images/timescaledb.tar.gz
docker compose --env-file node.env pull etcd
docker compose --env-file node.env up -d etcd
```

After all members are healthy, temporarily copy
`offline/etcd-root-password` to exactly one database host and enable etcd
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

Start Patroni on both database hosts:

```bash
docker compose --env-file node.env --profile database up -d --no-build patroni
```

Patroni creates the `fleet` database, login, and required extensions. Fleet
continues to own application migrations.

## Fleet connection contract

Fleet connects to both database IPs using the `fleet` login:

```text
postgresql://fleet:<url-escaped-password>@10.40.0.11:5432,10.40.0.12:5432/fleet?target_session_attrs=read-write&sslmode=verify-full&sslrootcert=/etc/proto-fleet/ha/service-ca.crt
```

The etcd observer uses all three `https://<ip>:2379` endpoints with
`fleet-observer`, `fleet-etcd-password`, and `service-ca.crt`.

Patroni requires one synchronous standby. If it disappears, commits block
instead of becoming single-copy durable.

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
Patroni primary, connected PostgreSQL writer, and synchronous standby agree.
The later qualification and installer work owns the complete failure matrix
and operator runbooks.
