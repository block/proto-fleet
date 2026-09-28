# External load-balancer endpoint

This profile keeps the fixed two Fleet/database hosts and one etcd witness but
does not install keepalived or move a virtual IP. Host addresses may be in
different routed subnets. Provision the hosts, network, load balancer, public DNS,
and public TLS certificate separately.

Fleet verifies its own nginx over loopback using that host's private certificate.
Public endpoint availability never grants leadership: etcd/Patroni observations
and the database lease still fence every active runtime. Passive and stale hosts
must reject application requests even if a load balancer sends them traffic.

## Prepare and install

Use the `fleet-ha` binary from the same verified release that will be installed.
On a trusted operator machine, prepare a new protected directory:

```sh
fleet-ha prepare-external /secure/fleet-ha \
  --database-a 10.0.1.10 --database-b 10.0.2.10 --witness 10.0.3.10 \
  --public-url https://fleet.example.com
```

The command captures the same feature settings as guided installation, including
`ENABLE_BETA_ALERTS`, `ENABLE_SYSTEM_MONITORING`, and `ENABLE_TRACING` with its
Datadog settings. It creates shared application identity and distinct per-host
service keys. Each nginx certificate identifies its own host IP, not the public
hostname; the load balancer terminates public TLS.

Transfer each `ha-a`, `ha-b`, or `ha-c` directory only to its matching host through
your protected provisioning channel. Set `HA_SECRETS_DIR` in its `node.env` to
the directory's destination path. For the `sudo` commands below, stage the
directory and every file as root-owned, retaining generated permissions:
directories are `0700`; `node.env`, private keys, and passwords are `0600`.
Do not distribute the `offline` directory. Deliver its `etcd-root-password` only
to `ha-a` for bootstrap, also root-owned with mode `0600`.

Run on `ha-a` from the verified release:

```sh
sudo ./ha/fleet-ha install-prepared /secure/ha-a/node.env \
  --etcd-root-password-file /secure/etcd-root-password
```

Run on `ha-b` and `ha-c` with their respective `node.env`, omitting the root
password argument. The three hosts may start concurrently; service startup waits
for quorum and authenticated bootstrap. The installer can return while services
are converging. No peer SSH or interactive terminal is required.

Configuration is installed at `/etc/proto-fleet/ha`, the release at
`/opt/proto-fleet/deployment`, and persistent state at `/var/lib/proto-fleet/ha`.
Cloud provisioning should attach retained storage and its fail-closed mount
dependencies before installation. Do not replace a missing data mount with an
empty directory. Preserve the original host identity when recovering state.

Keep public forwarding closed while initializing the administrator privately.
Then enable forwarding and inspect both hosts with:

```sh
sudo /opt/proto-fleet/deployment/ha/fleet-ha status /etc/proto-fleet/ha/node.env
```

Status checks the public URL using system certificate roots, while direct peer
checks use the cluster CA and each peer's own IP certificate. Public endpoint
failure appears as `endpoint_unavailable`; it does not change election behavior.

## Load-balancer and storage behavior

- Send HTTPS backend traffic to nginx port `443` and check
  `/api-proxy/health/active`. Allow TCP 443 only from the load balancer and the
  two Fleet/database hosts: authenticated peer status and rolling updates use
  direct HTTPS between those hosts.
- Preserve native HTTP/2 gRPC and bidirectional streams. A redirect or SSO login
  response is not a working Node endpoint. Do not expose `/health/ha` publicly.
- Run Compose through `fleet-ha compose`. It derives endpoint settings only from
  the protected `node.env` and adds `fleet-compose.external.yaml` automatically
  for application commands, including updates and password recovery.
- Firmware, command artifacts, and logs bind to `HA_DATA_DIR/artifacts/` with
  automatic host-directory creation disabled. Installation creates these paths;
  subsequent application updates reuse them. They are host-local, not replicated.
  Failover may require re-upload/retry.
- Passive-first updates reuse the existing updater. Public takeover verification
  permits up to 180 seconds in external mode; VIP mode retains its 35-second
  deadline. Neither mode makes incompatible database migrations rollback-safe.
- Uninstall does not manage keepalived in external mode. If provisioning bind
  mounts the install/config/data roots, coordinate mount removal with teardown;
  do not run `--purge-data` against retained cloud storage.

Public certificate renewal belongs to the load-balancer provisioner. Cluster
service certificate rotation and artifact HA remain outside this profile.
