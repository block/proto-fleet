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

## Install

The installer targets dedicated apt/systemd hosts on amd64 or arm64 with a
4096-byte page size and the base `sudo` and `iproute2` packages:

- Debian 12 or 13
- Ubuntu 22.04 or 24.04
- 64-bit Raspberry Pi OS based on Debian 12 or 13
- Debian or Ubuntu derivatives whose reported release codename is available
  from Docker's corresponding package repository

Other apt/systemd distributions fall back to Docker's Debian repository using
their reported release codename. Installation stops if Docker does not publish
that suite. RPM-based, non-systemd, and 32-bit hosts are not supported.

Before starting, reserve each node's IPv4 address in DHCP and exclude the VIP
from the DHCP pool. Download the release archive and its official `.sha256`
file for each host's architecture. Hosts may mix `amd64` and `arm64`, but every
host must use the same release version and commit.

### 1. Prepare and install `ha-a`

From the operator machine, transfer the release and run the guided installer.
Replace `VERSION`, `ARCH`, and `ha-a` with the downloaded release and SSH host:

```bash
VERSION=v0.0.0 ARCH=arm64 HOST=ha-a; scp "proto-fleet-${VERSION}-${ARCH}.tar.gz" "proto-fleet-${VERSION}-${ARCH}.tar.gz.sha256" "${HOST}:/var/tmp/"
```

```bash
VERSION=v0.0.0 ARCH=arm64 HOST=ha-a; ssh -t "$HOST" "set -eu; umask 077; cd /var/tmp; sha256sum --check 'proto-fleet-${VERSION}-${ARCH}.tar.gz.sha256'; stage=\$(mktemp -d /var/tmp/proto-fleet-ha.XXXXXX); tar -xzf 'proto-fleet-${VERSION}-${ARCH}.tar.gz' -C \"\$stage\"; exec \"\$stage/deployment/ha/fleet-ha\" install"
```

The wizard asks for the `ha-b` address, `ha-c` address, and VIP, then derives
`ha-a`'s local address and interface from the route to `ha-b`. It validates the
host and release, shows the complete topology and planned changes, and waits
for `INSTALL` before changing the host.

The wizard then creates protected bundles for all three hosts and a separate
public service CA certificate. Run the single copy command it prints
from the operator machine. Return to the wizard and type `COPIED`; it deletes
the copied peer exports from `ha-a` and continues installing that host. Protect
the host bundles while installing the peers, then delete the operator copies
after all three installations succeed.

### 2. Install `ha-b` and `ha-c`

For each peer, transfer the release, official checksum, and matching host
bundle. For example, for `ha-b`:

```bash
VERSION=v0.0.0 ARCH=arm64 HOST=ha-b; scp "proto-fleet-${VERSION}-${ARCH}.tar.gz" "proto-fleet-${VERSION}-${ARCH}.tar.gz.sha256" proto-fleet-ha-bundles/proto-fleet-ha-ha-b.json "${HOST}:/var/tmp/"
```

```bash
VERSION=v0.0.0 ARCH=arm64 HOST=ha-b; ssh -t "$HOST" "set -eu; umask 077; cd /var/tmp; sha256sum --check 'proto-fleet-${VERSION}-${ARCH}.tar.gz.sha256'; stage=\$(mktemp -d /var/tmp/proto-fleet-ha.XXXXXX); tar -xzf 'proto-fleet-${VERSION}-${ARCH}.tar.gz' -C \"\$stage\"; exec \"\$stage/deployment/ha/fleet-ha\" install /var/tmp/proto-fleet-ha-ha-b.json"
```

Repeat with `HOST=ha-c` and the `ha-c` bundle. Each installer verifies the
bundle, derives the local interface, shows its plan, and waits for `INSTALL`.
No environment or secret file is edited by hand.

### 3. Trust the service CA on clients

The copied `proto-fleet-ha-service-ca.crt` is public and is safe to distribute
to browsers and API clients. Verify that its fingerprint matches the value
printed in the authenticated `ha-a` installer session:

```bash
openssl x509 -in proto-fleet-ha-bundles/proto-fleet-ha-service-ca.crt -noout -fingerprint -sha256
```

Import only this certificate into each client's trusted root store. On
Debian-based clients:

```bash
sudo install -m 0644 proto-fleet-ha-bundles/proto-fleet-ha-service-ca.crt /usr/local/share/ca-certificates/proto-fleet-ha-service-ca.crt
sudo update-ca-certificates
```

Use the platform trust-store UI or equivalent managed policy on other clients.
Never distribute or import a host bundle; it contains that node's private keys
and cluster credentials.

### Installer behavior and failure handling

The official archive checksum verified in the commands above is the release
integrity check. Before changing the host, the installer checks the packaged
entrypoints, Linux platform, apt/systemd prerequisites, architecture, page
size, network identity and routes, free ports, empty data paths, and the exact
host secret file set. A 16K-page host is rejected with instructions to boot a
4K-page kernel and retry after reboot.

The host must be dedicated to Proto Fleet HA. A complete, unused Docker Engine
with compatible Compose v2 can be reused; existing images, volumes, networks,
and cache are allowed. Existing containers, custom Docker configuration,
configured or active keepalived, previous Proto Fleet paths or
units, occupied HA ports, and a claimed VIP are rejected. Missing supported
packages are installed. Unrelated nftables tables are preserved.

The installer uses `sudo` for privileged work and does not change Docker group
membership. It installs Docker from Docker's official Debian or Ubuntu
repository plus the HA networking packages, then installs the release at
`/opt/proto-fleet/deployment`, configuration at `/etc/proto-fleet/ha`, and data
at `/var/lib/proto-fleet/ha`. Docker requires the HA firewall unit, and Docker
restarts propagate through the role-aware HA service before keepalived can
return. That service starts etcd, then Patroni and Fleet on `ha-a` and `ha-b`.
keepalived is enabled only on those two Fleet hosts and remains ineligible for
the VIP until local active health passes. The witness starts etcd only.
Only allowlisted host secret files are installed. A successful install consumes
the copied host bundle. The one-time etcd root password is also removed from
`/etc/proto-fleet/ha` after authentication is enabled.

If installation fails after changing the host, the copied inputs are retained
for diagnosis. The installer intentionally has no resume or rollback state
machine. An interrupted install may require reimaging the dedicated host and
running the guided install again.

`ha-a` waits for etcd quorum before enabling authentication. The other hosts
wait for that one-time bootstrap before continuing. Once a host's systemd
service starts, it remains enabled while missing peers converge. If the SSH
session is interrupted, reconnect and use
`sudo systemctl status proto-fleet-ha.service` and
`sudo journalctl -u proto-fleet-ha.service` to observe convergence. Once a
database host is ready, use
`sudo /opt/proto-fleet/deployment/ha/fleet-ha status /etc/proto-fleet/ha/node.env`
for the cluster check. The profile requires a trusted L2 segment; its host
firewall restricts HA ports and VRRP to the fixed peer addresses. Bootstrap
fails closed if the cluster already contains one of the HA roles or users.

The Docker repository setup follows the official instructions for
[Debian](https://docs.docker.com/engine/install/debian/),
[Ubuntu](https://docs.docker.com/engine/install/ubuntu/), and
[64-bit Raspberry Pi OS](https://docs.docker.com/engine/install/raspberry-pi-os/).

## Update a passive Fleet host

HA disables application-triggered updates. On the passive database host, run:

```bash
sudo /opt/proto-fleet/deployment/ha/fleet-ha update v0.2.11
```

The local updater downloads the release from the fixed Proto Fleet GitHub
release origin, verifies its SHA256 checksum, builds and persists the staged
Fleet images, then rechecks that this host is passive. It stops and replaces
only `fleet-api` and `fleet-client`; etcd, Patroni, PostgreSQL, and keepalived
remain running. The command returns only after the target version is healthy
and passive.

The old active and new passive share the database during this rolling window.
Every migration in the target release must be expand-only and remain compatible
with the previous release; destructive contract migrations belong in a later
release after both HA hosts have advanced.

After the peer is confirmed on the target release, complete the update from
the old active host:

```bash
sudo /opt/proto-fleet/deployment/ha/fleet-ha update v0.2.11 --complete
```

If the updated host has already become active, the old host is now passive.
Run the ordinary `fleet-ha update v0.2.11` command on that passive host instead.

The source release must already contain this HA update protocol. HA is being
introduced for new deployments, so an older experimental HA installation that
predates `update --complete` must be reinstalled at the supported baseline
rather than upgraded through this workflow.

The updater stages everything first, stops the local Fleet containers, and
waits for the updated peer to serve the VIP with the target version. Only then
does it swap and restart the local application as passive. If takeover does
not complete within 15 seconds, it restarts the old local release without
swapping. This is a bounded interruption, not a zero-downtime update.

## Qualification

The distributions above are installer-compatible targets. The HA profile is
supported only after the dedicated-host qualification records successful Debian,
Ubuntu, and 64-bit Raspberry Pi OS runs. Other derivatives remain unqualified
until exercised explicitly.

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
The qualification workflow owns the complete failure matrix and evidence.

The updater accepts newer stable releases and release candidates. It verifies
the release archive and runtime safety, but it does not decide whether a
skipped-version application-only update is compatible with the installed
database and DCS substrate. Check the target release notes and migration
requirements before updating. Database, DCS, and VIP services remain pinned
and are never restarted by this command.

A passive update rechecks the local role, active peer, and control path just
before stopping Fleet. This crash-only profile accepts the small role-change
window before process exit; if the peer fails in that interval, durable update
recovery restarts the local application.
