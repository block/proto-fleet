# Proto Fleet HA profile

This profile installs the fixed three-host active/passive topology from a
packaged release.

The topology is:

- `ha-a` and `ha-b`: Fleet, PostgreSQL + TimescaleDB managed by Patroni, and etcd;
- `ha-c`: etcd witness only;
- one stable LAN IPv4 address per host;
- one unused LAN IPv4 address shared by keepalived on `ha-a` and `ha-b`;
- host networking on PostgreSQL `5432`, Patroni `8008`, and etcd `2379`/`2380`.

Node joins, certificate rotation, rollback, and restore automation are
intentionally deferred.

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

The command generates Fleet's database connection, authentication, and
encryption values once and copies the same `fleet.env` into both Fleet-host
bundles. It also gives each Fleet host a proxy certificate for the virtual IP
signed by the common HA service CA. Preflight requires and validates these
files on `ha-a` and `ha-b`.

## Install

The supported target is a clean Debian 13 host on amd64 or arm64 with a
4096-byte page size and the base `sudo` and `iproute2` packages. Unpack the
release and stage the host-specific install inputs in a separate directory.
Files added inside the unpacked release fail its manifest validation.

For example, on each host:

```bash
RELEASE_ROOT=/tmp/proto-fleet-release
INSTALL_INPUT_ROOT=/var/tmp/proto-fleet-ha-install

install -d -m 0700 "$INSTALL_INPUT_ROOT"
install -m 0600 /path/to/this-host/node.env "$INSTALL_INPUT_ROOT/node.env"
cp -a /path/to/generated/this-host-secrets "$INSTALL_INPUT_ROOT/host-secrets"
chmod -R go-rwx "$INSTALL_INPUT_ROOT/host-secrets"

# ha-a only
install -m 0600 /path/to/offline/etcd-root-password \
  "$INSTALL_INPUT_ROOT/etcd-root-password"
```

In the staged `node.env`, set
`HA_SECRETS_DIR=/var/tmp/proto-fleet-ha-install/host-secrets` and keep
`HA_DATA_DIR=/var/lib/proto-fleet/ha`.

Run the installs concurrently. Only `ha-a` receives the offline etcd root
password:

```bash
# ha-a
"$RELEASE_ROOT/ha/fleet-ha" install "$INSTALL_INPUT_ROOT/node.env" \
  --etcd-root-password-file "$INSTALL_INPUT_ROOT/etcd-root-password"

# ha-b and ha-c
"$RELEASE_ROOT/ha/fleet-ha" install "$INSTALL_INPUT_ROOT/node.env"
```

Before changing the host, the command validates the release manifest, Debian
version, architecture, page size, network identity and routes, free ports,
empty data paths, the exact host secret file set, and the absence of an existing
Docker or keepalived installation. A 16K-page host is rejected with
instructions to boot a 4K-page kernel and retry after reboot.

The installer uses `sudo` for privileged work and does not change Docker group
membership. It installs Docker from Docker's official Debian repository plus
the HA networking packages, then installs the release at
`/opt/proto-fleet/deployment`, configuration at `/etc/proto-fleet/ha`, and data
at `/var/lib/proto-fleet/ha`. Docker requires the HA firewall unit, and Docker
restarts propagate through the role-aware HA service before keepalived can
return. That service starts etcd, then Patroni and Fleet on `ha-a` and `ha-b`.
keepalived is enabled only on those two Fleet hosts and remains ineligible for
the VIP until local active health passes. The witness starts etcd only.
Only allowlisted host secret files are installed. A successful install consumes
the copied inputs. The one-time etcd root password is also removed from
`/etc/proto-fleet/ha` after authentication is enabled.

If installation fails after changing the host, the copied inputs are retained
for diagnosis. The first installer intentionally has no resume or rollback
state machine: restore a clean host image, copy the retained inputs back, and
run the install again.

`ha-a` waits for etcd quorum before enabling authentication. The other hosts
wait for that one-time bootstrap before continuing. Installation finishes only
after a database host passes `fleet-ha status --check`, or after the witness
has observed etcd quorum. The profile requires a trusted L2 segment; its host
firewall restricts HA ports and VRRP to the fixed peer addresses.
Bootstrap fails closed if the clean cluster already contains one of the HA
roles or users; clear the incomplete etcd data and rerun the clean install.

The Docker repository setup follows the official
[Debian installation instructions](https://docs.docker.com/engine/install/debian/).

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
The [qualification procedure](QUALIFICATION.md) owns the release qualification
matrix and redacted evidence.

Application-only HA updates support adjacent releases only. Every target
migration must be expand-only and remain compatible with the running release;
destructive contract migrations require a later release after both hosts have
advanced. Do not use `fleet-ha update` for a release pair that has not passed
separate adjacent-release migration and mixed-version qualification.

The first release containing this update workflow is the clean-install
baseline. HA deployments on an earlier experimental release must be reinstalled
instead of upgraded through this path. Release metadata allows no HA source by
default. After an adjacent pair passes qualification, set its one exact source
tag in `qualified-update-from.txt` before publishing the target. The manifest
covers the resulting `ha_update_from` field, and the updater rejects every
other source version. A qualified RC-to-stable pair can name the RC explicitly.
