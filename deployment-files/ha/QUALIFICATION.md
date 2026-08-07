# Proto Fleet HA qualification

The tested release artifact and architecture are supported only after they pass
every gate in this document on three clean, same-L2 Debian hosts. End each
measurement at the gate's actual recovery signal: VIP health for routing,
writable SQL for database recovery, durable command/device state for commands,
and telemetry or independent power measurement for curtailment. Do not include
addresses, certificates, passwords, device names, or customer data in the
committed report.

Use a fourth, non-peer host on the same L2 network only for the firewall gate;
it must not be one of the three configured HA addresses.

## Test identity

| Field | Value |
| --- | --- |
| Release version | Pending |
| Commit SHA | Pending |
| Architecture | arm64 |
| Operating system | Debian 13 |
| Page size | 4096 bytes |
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
| Power off active host | Peer serves VIP within 15s | Pending | Pending | Pending |
| Stop database primary | Writable primary recovers within 30s | Pending | Pending | Pending |
| Commit uniquely identified state, then abruptly stop its acknowledged primary | The exact state exists on the promoted writer | Pending | Pending | Pending |
| Partition the current Patroni primary from DCS quorum while continuously probing pinned connections to both database hosts | At most one host accepts writes; the isolated primary is read-only or unreachable before promotion; it rejoins without divergent acknowledged state | Pending | Pending | Pending |
| Stop database standby | Service remains usable; failover readiness is degraded | Pending | Pending | Pending |
| Stop etcd witness | Service remains usable; failover readiness is degraded | Pending | Pending | Pending |
| Break active DCS path | Old active stops serving; peer takes over | Pending | Pending | Pending |
| Break active database path | Old active stops serving; peer takes over | Pending | Pending | Pending |
| Remove active VIP/interface path | Old active stops serving; peer takes over | Pending | Pending | Pending |
| Fail over with PENDING command | New active dispatches the command | Pending | Pending | Pending |
| Fail over with PROCESSING command | Interrupted attempt fails and later work resumes | Pending | Pending | Pending |
| Stall a PROCESSING device call, break the active DCS path, wait for takeover, then release the old call | The stale transition is rejected; one device effect and one terminal result remain | Pending | Pending | Pending |
| Fail over during firmware command | Transitional device state is cleared | Pending | Pending | Pending |
| Send command after failover | Command succeeds on the new active | Pending | Pending | Pending |
| Pin an authenticated active-only API request to the passive host while retaining the VIP URL and TLS SNI | Fleet returns its machine-readable `NOT_ACTIVE` response, not a TLS or transport error | Pending | Pending | Pending |
| Open an authenticated long-lived stream on the active host, then induce demotion | The established stream closes promptly after ownership loss | Pending | Pending | Pending |
| Attempt TCP connections from a non-peer to ports 2379, 2380, 5432, 8008, and 40000 before and after reboot | TCP establishment fails against the installed firewall while the configured peer connects to every required HA port | Pending | Pending | Pending |
| Send an unauthorized VRRP protocol-112 advertisement from the non-peer | The firewall drops it and VIP ownership does not change | Pending | Pending | Pending |
| Fail over during curtailment | Telemetry or independent power measurement proves load shedding within 180s | Pending | Pending | Pending |
| After takeover, publish a uniquely identified MQTT curtailment target and then its restoration target | The new active persists both source updates and physical measurement proves shedding and restoration within 180s | Pending | Pending | Pending |

## Repetition and soak

| Gate | Required result | Result | Evidence |
| --- | --- | --- | --- |
| Application failover | 5 consecutive passes | Pending | Pending |
| Database failover | 3 consecutive passes | Pending | Pending |
| Soak | 24h with no split ownership or lost failover readiness | Pending | Pending |

During the soak, sample both hosts at least every two seconds. Record local
`sudo /opt/proto-fleet/deployment/ha/fleet-ha status /etc/proto-fleet/ha/node.env --json --check`,
direct active/passive health, database lease ownership, and VIP interface
ownership with timestamps. Fail the soak on any dual-active, dual-VIP, or
lost-readiness sample. Redact the retained evidence before committing it.

## Verdict

**Pending.** Do not describe an artifact and architecture as supported until
every result above is `PASS` and the report records the tested release version
and commit. This initial Raspberry Pi run qualifies arm64 only; amd64 remains
unqualified until the same gates pass for its release artifact.
