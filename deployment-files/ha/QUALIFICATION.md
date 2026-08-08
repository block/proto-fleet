# Proto Fleet HA qualification

The tested release artifact, host package versions, and architecture are
supported only after they pass every gate in this document on three clean,
same-L2 Debian hosts. End each
measurement at the gate's actual recovery signal: VIP health for routing,
writable SQL for database recovery, durable command/device state for commands,
and telemetry or independent power measurement for curtailment. Do not include
addresses, certificates, passwords, device names, or customer data in the
committed report.

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
| Send an active-only request directly to the passive host | Passive rejects it as not active | Pending | Pending | Pending |
| Hold an active-only request across demotion | The old active cancels it before the peer serves active traffic | Pending | Pending | Pending |
| Probe HA-only ports from a non-peer before and after reboot | Protected ports reject the probe while peer traffic remains healthy | Pending | Pending | Pending |
| Send a VRRP advertisement from a non-peer | The packet is dropped and VIP ownership does not change | Pending | Pending | Pending |
| Fail over with PENDING command | New active dispatches the command | Pending | Pending | Pending |
| Fail over with PROCESSING command | Interrupted attempt fails and later work resumes | Pending | Pending | Pending |
| Fail over during firmware command | Transitional device state is cleared | Pending | Pending | Pending |
| Send command after failover | Command succeeds on the new active | Pending | Pending | Pending |
| Publish unique curtailment and restoration targets after failover | Both inputs persist and measured load follows them within 180s | Pending | Pending | Pending |

The PROCESSING-command gate proves server-side recovery, not exactly-once
device effects. Device-side fencing is outside this profile's support claim.

## Repetition and soak

| Gate | Required result | Result | Evidence |
| --- | --- | --- | --- |
| Application failover | 5 consecutive passes | Pending | Pending |
| Database failover | 3 consecutive passes | Pending | Pending |
| Soak | 24h with no observed dual-active state or lost failover readiness | Pending | Pending |

During the soak, sample both hosts at least every two seconds. Record local
`sudo /opt/proto-fleet/deployment/ha/fleet-ha status /etc/proto-fleet/ha/node.env --json --check`,
direct active/passive health, and VIP interface ownership with timestamps. Fail
the soak on any dual-active, dual-VIP, or lost-readiness sample. Redact the
retained evidence before committing it.

## Verdict

**Pending.** Do not describe an artifact and architecture as supported until
every result above is `PASS` and the report records the tested release, commit,
and host package versions. A package-version change requires requalification.
This initial Raspberry Pi run qualifies arm64 only; amd64 remains unqualified
until the same gates pass for its release artifact.
