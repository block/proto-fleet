# Proto Fleet HA qualification

The tested release artifact, host package versions, and architecture are
supported only after they pass every gate in this document on three clean,
same-L2 Debian hosts. End each
measurement at the gate's actual recovery signal: VIP health for routing,
writable SQL for database recovery, durable command/device state for commands,
and telemetry or independent power measurement for curtailment. Do not include
addresses, certificates, passwords, device names, or customer data in the
committed report.

This report qualifies the fixed Fleet application, database, DCS, and VIP
profile only. Adjacent application updates are not qualified here. Fleet Node
HA, reconnect scale, and alert delivery remain outside this support claim.

## Test identity

| Field | Value |
| --- | --- |
| Release version | Pending |
| Commit SHA | Pending |
| Architecture | arm64 |
| Operating system | Debian 13 |
| Page size | 4096 bytes |
| Docker, containerd, keepalived, nftables, and arping package versions | Pending |
| Started | Pending |
| Completed | Pending |

## Clean installation

1. Start with three freshly provisioned Debian 13 hosts. Do not qualify by
   deleting directories from a previous HA installation; reimage the hosts so
   no old services, containers, firewall rules, VIP, or data remain.
2. Copy the same release, host-specific `node.env`, and only that node's
   matching secret directory to each host. Never copy the generated `offline`
   directory to a running host. Copy the etcd root password separately to
   `ha-a` only.
3. Run `fleet-ha install` concurrently on `ha-a`, `ha-b`, and `ha-c` as
   described in [README.md](README.md).
4. Reboot all three hosts. On both database hosts, run
   `sudo /opt/proto-fleet/deployment/ha/fleet-ha status
   /etc/proto-fleet/ha/node.env --check`.
5. Confirm exactly one active Fleet, one passive Fleet, one Patroni primary,
   one Patroni replica, three etcd members, and one VIP owner.

## Failure matrix

Restore full readiness before starting each row. Record the observed recovery
time and a short redacted evidence reference.

| Gate | Required result | Duration | Result | Evidence |
| --- | --- | --- | --- | --- |
| Kill active Fleet process | Peer serves VIP within 15s | Pending | Pending | Pending |
| Power off the active Fleet host after confirming it is the Patroni replica | Peer serves VIP within 15s | Pending | Pending | Pending |
| Power off a host that is both active Fleet and Patroni primary | Peer restores writable VIP service within 30s | Pending | Pending | Pending |
| Abruptly lose the database primary after an acknowledged uniquely identified write | Writable primary recovers within 30s and retains the write | Pending | Pending | Pending |
| Isolate the database primary from DCS quorum | At most one primary accepts writes; the old primary rejoins without divergence | Pending | Pending | Pending |
| Stop database standby | Service remains usable; failover readiness is degraded | Pending | Pending | Pending |
| Stop etcd witness | Service remains usable; failover readiness is degraded | Pending | Pending | Pending |
| Break active DCS path | Old active stops serving; peer takes over | Pending | Pending | Pending |
| Break active database path | Old active stops serving; peer takes over | Pending | Pending | Pending |
| Remove active VIP/interface path | Old active stops serving; peer takes over | Pending | Pending | Pending |
| Fail one active-runtime job without killing Fleet | Active health fails, the process exits, and the peer takes over | Pending | Pending | Pending |
| Send Connect RPC, non-RPC HTTP, and ControlStream traffic directly to the passive host | Each product transport rejects it as not active; only health and local status remain available | Pending | Pending | Pending |
| Hold an active-only request across demotion | The old active cancels it before the peer serves active traffic | Pending | Pending | Pending |
| Probe public health through the VIP | Public responses reveal no HA topology and `/api-proxy/health/ha` returns 404 | Pending | Pending | Pending |
| With Grafana and telemetry ingestion stopped, stop the etcd witness | Active health remains usable and local HA status still reports degraded failover readiness | Pending | Pending | Pending |
| Probe HA-only ports from a non-peer before and after reboot | Protected ports reject the probe while peer traffic remains healthy | Pending | Pending | Pending |
| Send a VRRP advertisement from a non-peer | The packet is dropped and VIP ownership does not change | Pending | Pending | Pending |
| Fail over with PENDING command | New active dispatches the command | Pending | Pending | Pending |
| Demote a live old active with a stalled PROCESSING plugin call, recover on the peer, then release the old call | The stale transition is rejected, exactly one terminal database result remains, and later work resumes | Pending | Pending | Pending |
| Fail over during firmware command | Transitional device state is cleared | Pending | Pending | Pending |
| Send command after failover | Command succeeds on the new active | Pending | Pending | Pending |
| Publish a unique MQTT curtailment target, confirm shedding starts, fail the active before completion, then restore after takeover | The peer resumes MQTT intake and retains or reasserts curtailment within 180s, then measured load follows the restoration target | Pending | Pending | Pending |

The PROCESSING-command gate proves server-side recovery, not exactly-once
device effects. Device-side fencing is outside this profile's support claim.

## Repetition and soak

| Gate | Required result | Result | Evidence |
| --- | --- | --- | --- |
| Application failover | 5 consecutive passes | Pending | Pending |
| Database failover | 3 consecutive passes | Pending | Pending |
| Soak | 24h with no observed dual-active state or lost failover readiness | Pending | Pending |

Before the soak, start an external append-only recorder on a separate
controller. Continuously record each host's direct active-health result and
interface-address events with synchronized monotonic timestamps at 100 ms or
faster; evidence stored only inside the HA database is insufficient. Treat an
unreachable host, write failure, or gap longer than 250 ms as a failed soak.
Also sample both hosts at least every two seconds for availability. Record local
`sudo /opt/proto-fleet/deployment/ha/fleet-ha status /etc/proto-fleet/ha/node.env --json --check`,
and retain the status, health, and address streams. Reconstruct each active
interval from the first active result through the first passive result, and
each VIP interval from address-add through address-delete. Fail on overlapping
host intervals, any collection gap, or lost readiness. Export redacted
evidence after the soak.

## Verdict

**Pending.** Do not describe an artifact and architecture as supported until
every result above is `PASS` and the report records the tested release, commit,
and host package versions. A package-version change requires requalification.
This initial Raspberry Pi run qualifies arm64 only; amd64 remains unqualified
until the same gates pass for its release artifact.
