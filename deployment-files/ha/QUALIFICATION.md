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
profile at the recorded fleet scale and topology only. Adjacent application
updates are not qualified here. Fleet Node HA, larger fleets, reconnect scale,
schedule recovery during failover, and alert delivery remain outside this
support claim.

## Test identity

| Field | Value |
| --- | --- |
| Release version | Pending |
| Commit SHA | Pending |
| Release bundle SHA-256 | Pending |
| `deployment-manifest.sha256` SHA-256 | Pending |
| Deployed API, client, and Patroni image IDs/digests on every host | Pending |
| Architecture | arm64 |
| Operating system | Debian 13 |
| Page size | 4096 bytes |
| Docker, containerd, keepalived, nftables, and arping package versions | Pending |
| Test miner count and plugin mix | Pending |
| Miner connection topology | Pending |
| Command backlog at curtailment | Pending |
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
6. Confirm every host uses the recorded deployment manifest and container image
   identities from the qualified release bundle.

## Failure matrix

Restore full readiness before starting each row. Run the external append-only
recorder described under Repetition and soak from before fault injection through
full recovery. Against a test device, continuously submit uniquely identified
idempotent command probes and retain database results. Fail any row on possible
active, VIP, or writable-primary overlap, a collection gap, duplicate terminal
results, or a stale transition. Record the observed recovery time and a short
redacted evidence reference. For database isolation and failover rows, also
record host-pinned writable SQL probe results against both database hosts at
100 ms or faster. Before isolating the old primary, begin a writable transaction
on it, write a unique probe identifier, and hold the transaction open. After the
peer is confirmed promoted and writable, begin a fresh transaction there and
write a different identifier. Use a controller barrier to attempt both commits
concurrently. Treat an ambiguous result as potentially committed and fail if
both transactions can commit. Record transaction start, promotion, commit, and
failure bounds on the controller clock. After the old primary rejoins, verify
every possibly committed probe identifier exists at most once and every
acknowledged identifier exists exactly once.

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
| From a fourth non-peer, probe each listening HA-only service port before and after reboot | Peer traffic remains healthy and every non-peer connection is blocked | Pending | Pending | Pending |
| Send a valid winning VRRP advertisement from a non-peer | VIP ownership does not change | Pending | Pending | Pending |
| Fail over with PENDING command | New active dispatches the command | Pending | Pending | Pending |
| SIGSTOP the old active with a stalled PROCESSING plugin call, let the peer recover it, queue the old result, then SIGCONT | The resumed stale transition is rejected, exactly one terminal database result remains, and later work resumes | Pending | Pending | Pending |
| Fail over during firmware command | Transitional device state is cleared | Pending | Pending | Pending |
| Send command after failover | Command succeeds on the new active | Pending | Pending | Pending |
| Publish a unique MQTT curtailment target, confirm shedding starts, fail the active before completion, then restore after takeover | The peer resumes MQTT intake and retains or reasserts curtailment within 180s, then measured load follows the restoration target | Pending | Pending | Pending |

The 180-second curtailment result applies only to the miner count, plugin mix,
connection topology, and command backlog recorded above.

The PROCESSING-command gate proves server-side recovery, not exactly-once
device effects. Device-side fencing is outside this profile's support claim.

## Repetition and soak

| Gate | Required result | Result | Evidence |
| --- | --- | --- | --- |
| Application failover | 5 consecutive passes | Pending | Pending |
| Database failover | 3 consecutive passes | Pending | Pending |
| Soak | 24h with no observed dual-active state or lost failover readiness | Pending | Pending |

Before the soak, start an external append-only recorder on a separate
controller. Start and confirm both host subscriptions first, buffering active
health samples and interface-address events at 100 ms or faster. Timestamp all
probe sends, responses, and event arrivals on that controller's monotonic clock;
do not compare host clocks. Treat ownership as beginning no later than the last
preceding non-owner probe started and ending no earlier than the first following
non-owner probe completed. Include measured request and delivery error in those
bounds. Then take timestamped state snapshots, replay all buffered
events that can intersect each snapshot, and begin the observation window. Fail
if the collector cannot prove that subscription, snapshot, replay, and timing
bounds were gap-free. Evidence stored only inside the HA database is
insufficient. Seed intervals owned at the initial snapshot from the
observation-window start, and extend intervals still owned at the final
snapshot through the observation-window end. Fail if either boundary state is
unknown. Treat an
unreachable host, write failure, or gap longer than 250 ms as a failed soak.
For a power-off gate, also record the switched power outlet or an independent
power monitor on the same clock. The first confirmed power-off sample closes
that host's possible active and VIP intervals; network unreachability alone
does not. Stream termination after that sample is expected, not a collection
gap. Fail the gate if independent power-state evidence is unavailable.
Also sample both hosts at least every two seconds for availability. Record local
`sudo /opt/proto-fleet/deployment/ha/fleet-ha status /etc/proto-fleet/ha/node.env --json --check`,
and retain the status, health, and address streams. Conservatively reconstruct
each active interval from its last preceding passive sample through its first
following passive sample, and each VIP interval from address-add through
address-delete. Fail when these uncertainty-inclusive host intervals could
overlap, or on any collection gap or lost readiness. Export redacted evidence
after the soak.

## Verdict

**Pending.** Do not describe an artifact and architecture as supported until
every result above is `PASS` and the report records the tested release, commit,
and host package versions. A package-version change requires requalification.
This initial Raspberry Pi run qualifies arm64 only; amd64 remains unqualified
until the same gates pass for its release artifact.
