# fleetnode

`fleetnode` is the on-prem agent that enrolls with a fleet server, holds a session, opens a `ControlStream`, executes server-issued discovery commands against the operator's local network, and reports discovered devices back. It is the on-prem counterpart to combined-mode `pairing.PairingService.Discover` — the same `DiscoverRequest` payload, executed by the agent on the operator's behalf.

## Subcommands

| Command | Purpose |
|---------|---------|
| `fleetnode enroll`  | Register with a fleet server using a one-time enrollment code. Persists keys and `api_key`. See [enroll.go](enroll.go). |
| `fleetnode status`  | Print local state (server URL, fleet_node_id, fingerprint, session expiry). See [status.go](status.go). |
| `fleetnode refresh` | Renew the session token using the stored `api_key`. See [refresh.go](refresh.go). |
| `fleetnode run`     | Long-running daemon: heartbeat + control stream. See [run.go](run.go). |

## State directory and lock

State lives in `state.yaml` under one of, in order:

1. `--state-dir <path>` (override; primarily for tests)
2. `$XDG_STATE_HOME/fleetnode`
3. `~/.local/state/fleetnode`

The file holds `server_url`, `fleet_node_id`, `identity_fingerprint`, both keypairs, the `api_key`, and the current `session_token`. It is created `0600` under a `0700` directory. See [state.go](../../internal/fleetnodebootstrap/state.go).

A `state.lock` file in the same directory serializes commands. Only one process may hold it at a time (`LOCK_EX | LOCK_NB`). The PID of the holder is written under the lock; if another invocation hits contention, the error includes that PID. To clear a stale lock, identify the owner with `ps -p <pid>`; remove the lock file only if the process is gone.

## Plugins

Discovery is plugin-driven. The agent loads every executable in `<exe-dir>/plugins` at startup (subprocess plugins over gRPC via `hashicorp/go-plugin`). If that directory is missing the agent runs in heartbeat-only mode and refuses control commands; if it is present but loosens beyond owner-only write, the agent refuses to start (`plugins dir ... must not be group- or world-writable`).

There is no `--plugins-dir` flag: the path is fixed relative to the binary so an installer always owns it. See [plugins_dir.go](plugins_dir.go) and [plugins_dir_unix.go](plugins_dir_unix.go).

The repository's `just build-fleetnode` target stages a working layout at `server/.fleetnode/`:

```
server/.fleetnode/
├── fleetnode
└── plugins/
    ├── proto-plugin
    ├── antminer-plugin
    ├── virtual-plugin
    └── asicrs-plugin
```

## TCP discovery

Network discovery uses the in-process TCP scanner in `internal/domain/netscan`. The scanner connects to the requested ports, closes each connection immediately, and hands open endpoints to the existing miner plugins for identification. IP-list and IP-range discovery send every requested endpoint directly to bounded plugin identification, including virtual miners that have no TCP listeners. No mode needs a scanner executable or raw-socket capability.

TCP connection attempts share a 512-socket process budget, with a three-second timeout each. Plugin identification uses 32 concurrent probes with ten seconds per probe. Target parsing, private DNS selection, and port validation use the same shared helpers as Fleet Server. Manual network-scan IPv4 CIDRs omit their actual network and broadcast addresses through /30; /31, /32, configured address intervals, and explicit ranges include every address. Assigned node discovery ranges support private IPv4 only; existing owned IPv6 miner routes are unaffected.

## Assigned discovery ranges

Configure each active node with disjoint private IPv4 ranges in `/etc/fleetnode/config.yaml`:

```yaml
run:
  discovery-ranges:
    - 10.40.0.1-10.40.15.254
    - 10.41.0.0/24
```

The `--discovery-ranges` flag accepts the same values. When `discovery-ranges` is
absent, the existing `--local-discovery-subnet` override configures a single range.
Explicit configuration takes precedence over ranges imported during standby setup.
An explicit empty YAML list disables discovery.
Restart the node after editing its configuration. Invalid ranges fail local
validation. Missing ranges disable discovery and new pairing while existing
miner telemetry and commands continue.

Fleet Server reads each node's effective ranges from its live control connection.
It does not store an authoritative configuration in the database. Node management
shows the reported ranges while connected and marks them unavailable when
disconnected.
Operators prevent overlap between active nodes, including nodes on separate
networks. Separate networks reusing the same private addresses are not supported
by this partitioning workflow.

The regular Add Miners flow scans configured ranges through their assigned active
nodes, without requiring a Fleet Server miner subnet. Fleet limits manual IP
targets to each node's ranges. Each node receives sequential commands with at most
256 addresses and 1,024 address/port combinations. Nodes run in parallel and
restrict discovery to their local configuration. IP lists and ranges retain
direct plugin identification, while network scans retain TCP probing. Source
warnings identify incomplete discovery; completed results remain available.

Each discovery request has a 12-minute overall deadline and admits at most
262,144 node reports across all participating nodes and partitions. Add Miners
keeps at most 65,536 distinct devices in its result list. Reaching a limit retains
completed results and shows an incomplete-discovery warning. Narrow the targets
and run discovery again to continue; discovery does not resume automatically.

A new pairing must keep every address in a grouped endpoint within the discovering
node's current ranges. Pairing runs across nodes in parallel, in sequential
batches of at most 128 miners per node. The UI displays paired and selected counts
when the caller can read node details, warns above 4,000, and flags totals above
5,000. Fleet does not reserve capacity or enforce a transactional count limit.
Plan approximately 13 active nodes for 50,000 miners at the 4,000-miner target,
plus standbys as required.

Changing ranges does not rebalance or re-pair existing miners. When a node
reconnects, Fleet retires its out-of-scope unpaired discoveries so another
configured node can claim them. Existing owners and authentication-repair routes
remain intact. Finish or resolve outstanding discovery/pairing work before moving
a partition to another node. If interrupted, reopen Add Miners and continue the
remaining work. Accepted batches finish server-side. Inspect unknown results
before retrying.
There is no durable background operation or automatic restart recovery.

### Standby inheritance

When adding a standby, Fleet copies the active node's reported ranges once and
persists them in the standby's local state directory. Fleet waits for the standby
to acknowledge that the ranges were saved before marking it ready. The active must
be connected with ranges configured, and the standby must support command protocol
V8. Repeating the same installation is safe. Operators must correct any conflicting
local or previously imported configuration.

Imported ranges survive restart and promotion. Later edits do not propagate
automatically. Update both peers manually. Node management warns when both
connected peers report different effective ranges. Only the active peer scans.
Existing ready HA pairs retain their failover behavior.

Deploy Fleet Server support first, then upgrade and configure nodes before new
discovery or pairing. Partitioned discovery supports IPv4. Existing owned-miner
operations and separate direct-server discovery keep their existing routing
behavior.

### Scale qualification

Run the opt-in gateway/Postgres integration with an isolated local test database:

```bash
cd server
DB_PASSWORD=fleet FLEETNODE_SCALE_TEST=1 go test ./internal/handlers/fleetnode/gateway \
  -run '^TestFleetNodeScaleDiscoveryAndPairing$' -count=1 -v -timeout 5m
```

The test discovers and pairs 50,000 simulated miners across 13 nodes using the
production gateway's persistence code. It verifies unique ownership and reports
discovery/pairing time. It does not qualify physical drivers, node hardware,
sustained telemetry cadence, or deployment-specific database sizing. Qualify those
with representative hardware and steady telemetry/control load before claiming
production capacity.

A full command can report up to 40,960 endpoints in forty 1,024-report uploads. Those batches and the terminal ACK fit the existing 64-event command queue without requiring the consumer to drain during upload. The complete upload has a 90-second budget, leaving 30 seconds for dispatch and ACK delivery within the server’s 12-minute wait after a ten-minute scan. This capacity does not guarantee completion within the command deadline.

## Control stream

`run` opens a bidirectional `ControlStream` over the gateway:

1. Agent dials gateway, sends `ControlHello`.
2. Server replies `ControlAccepted`; stream stays open. For nodes that speak command protocol V7, `ControlAccepted.keepalive_interval` is set (five seconds): the node sends `ControlPing` at that interval, and either side closes the stream after three intervals without receiving anything, so a half-open connection reconnects within seconds.
3. Server pushes `ControlCommand{command_id, payload}`. Payload is a serialized `AgentCommand` envelope containing discovery, pairing, telemetry, or a per-miner command.
4. Agent executes the command locally and sends any command-specific reports or acknowledgement payload. For discovery, it scans the requested TCP endpoints and identifies open ports using plugins, then sends results via `ReportDiscoveredDevices` in batches of 1,024 under one 90-second upload budget. Each batch carries `command_id`; identified results survive a late scan failure or deadline. Fleet Nodes do not support mDNS discovery.
5. Agent sends `ControlAck{command_id, succeeded}` on completion.

Discovery and pairing share one exclusive process-wide slot. All other commands share 512 process-wide slots. Fleet and Fleet Node use the same fixed admission policy:

| Admission class | Commands | Why |
|-----------------|----------|-----|
| Exclusive | Discovery and pairing | These are heavy, report-bearing operations and retain their separate single-flight slot. |
| Deferrable read | Telemetry, `GetErrors`, and `GetCoolingMode` | These reads are observation-only and safe to retry. Telemetry includes scheduled collection and curtailment confirmation; error polling accompanies telemetry; cooling-mode reads only prefill the settings UI. |
| General | Every other command, including `GetMiningPools`, `GetFirmwareUpdateStatus`, malformed or empty envelopes, unknown commands, and future command types | Pool reads can be prerequisites for an operator pool change, and firmware-status reads advance an operator-initiated update. Unknown work defaults to general so new commands do not accidentally consume reserved read capacity. |

Deferrable reads additionally share a 504-slot limit, leaving eight ordinary slots available when those reads are stuck. General commands can use all otherwise-idle ordinary slots. Fleet mirrors both limits per node and queues work before sending it, which keeps normal load from turning into `BUSY` retry storms. Commands that still exceed either Fleet Node limit receive `BUSY` immediately instead of blocking the stream receive loop. These limits persist across reconnects until the admitted handlers return.

The ceilings are deliberately fixed and optimistic for load testing: 512 commands per node, 504 deferrable reads, and eight reserved general slots. Fleet's global telemetry worker default is 1,500. Bulk startup discovery is deterministically distributed across the 15-second telemetry window instead of releasing every miner at once, while a single newly paired miner remains immediately eligible.

For load-test observability, `control stream opened` logs the configured limits. Capacity rejections include `command_kind`, `admission_class`, `rejection_reason`, and current shared/deferrable/exclusive occupancy. Command completions are available at debug level with the same classification, duration, acknowledgement outcome, and post-release occupancy.

If the server side is older than RFC-0001 phase 2, the stream returns `Unimplemented`. The agent reconnects with exponential backoff (1s → 30s), so older servers degrade quietly. See [control.go](control.go).

Reconnect is newest-wins on the server: a freshly opened stream evicts any prior registration for the same `fleet_node_id`.

## Build

```bash
just build-fleetnode               # produces server/.fleetnode/{fleetnode, plugins/}
go build -o fleetnode ./server/cmd/fleetnode   # fast iteration
```

Release builds embed the release tag and publishing repository with `-ldflags`. `fleetnode --version` and `fleetnode --print-release-repository` print them, and the daemon logs both in its `daemon started` line. Source builds report `dev` and `block/proto-fleet`.

## Run

```bash
fleetnode enroll --server-url=https://fleet.example.com
fleetnode run     # long-running daemon; logs to stdout, or the journal under systemd
```

### Command tracing

Fleet Node command tracing is disabled by default. To export command spans,
point Fleet Node at an OTLP HTTP receiver running on the node, such as a local
OpenTelemetry Collector or a Datadog Agent with OTLP ingest enabled:

```bash
FLEETNODE_TRACING_ENABLED=true \
FLEETNODE_TRACING_ENDPOINT=http://127.0.0.1:4318 \
FLEETNODE_TRACING_SAMPLE_RATE=1.0 \
fleetnode run
```

The exported service name is `fleetnode`. Server-issued commands continue the
`fleetd` dispatch trace when both processes have tracing enabled; missing or
invalid incoming trace context starts a new Fleet Node trace. The Fleet Node
installer loads these variables from `/etc/fleetnode/fleetnode.env` when set.

### Logs

The daemon logs at `info` and above; lower it with `--log-level` /
`FLEETNODE_LOG_LEVEL` (`debug`, `info`, `warn`, `error`). `--log-output` /
`FLEETNODE_LOG_OUTPUT` picks the sink:

| Value | Behavior |
|-------|----------|
| `auto` (default) | `journal` when systemd connected stdout to the journal (`JOURNAL_STREAM`), otherwise `stdout`. |
| `stdout` | logfmt text lines with `time=` and `level=`, as in a terminal or the dev containers. |
| `journal` | Native journald entries over `/run/systemd/journal/socket`; falls back to `stdout` with a warning if the socket is unavailable. |

Journal entries carry the level as `PRIORITY` (so `journalctl -p warning -u
fleet-node.service` and journal-tailing shippers such as the Datadog Agent see
the real severity instead of the stdout default of `info`), each attribute as
an uppercase field (`fleet_node_id` → `FLEET_NODE_ID`, groups joined with `_`;
see `journalctl -o verbose`), and the message with its logfmt attributes in
`MESSAGE`, so `journalctl -u fleet-node.service` reads as before. The handler
lives in [`internal/infrastructure/logging`](../../internal/infrastructure/logging/journal.go)
so the other systemd-run binaries can adopt it.

Stop with `SIGINT`, `SIGTERM`, or `SIGHUP` (terminal close). On shutdown the agent closes the control stream, drains in-flight probes, and reaps plugin subprocesses. At startup it also reaps any orphan plugin processes left under `<exe-dir>/plugins/` by a previous crash — see [orphan_reaper.go](orphan_reaper.go).

## Enrollment flow

1. Operator mints an enrollment code in the UI and shares it.
2. `fleetnode enroll --server-url=...` prompts for the code, registers, prints a fingerprint.
3. Operator verifies the fingerprint in the UI and clicks confirm; the UI displays the `api_key`.
4. Agent prompts for the `api_key`, completes the handshake, persists session.
5. `fleetnode run` keeps the session refreshed and the control stream open.

If anything is interrupted between Register and Complete, `fleetnode refresh` resumes from the persisted state.

## High availability

A node can have one standby. In **Settings → Nodes → Set up a node**, select **Enroll as a standby**, choose a confirmed node that is not already in a pair, and complete normal enrollment on a second machine that can reach Fleet and the same miners. Each node keeps its own identity and API key.

You can enroll a standby before upgrading the nodes; the pair stays **Preparing** until both support HA (V7). Once both nodes are connected and run an HA-capable version, Fleet asks the active node to seal its miner-credential key to the standby's enrollment encryption key, relays the sealed key, and has the standby install it in `state.yaml`. Fleet persists the sealed payload before dispatch and retries that same payload after a timeout or server restart, even if the active node is no longer available. Fleet relays only the sealed key and compares key fingerprints from both nodes before marking it **Standby**. The active node seals the key to whichever recipient Fleet names, so pairing trusts the Fleet server with the credential key: a compromised Fleet server could request it. A standby does not discover or pair miners.

When the active node's control stream has been down, and a ready standby connected, for 30 seconds, Fleet moves the active node's device assignments and discovery attribution to the standby and swaps the roles. Fleet waits 90 seconds after it starts, or becomes the active Fleet instance, so nodes can reconnect first. There is no automatic failback: the old active node returns as the ready standby. Commands in flight on the failed node are not replayed, so check device state before repeating an uncertain write. Password updates and curtailment configs are encrypted to a device's node when they are queued, so any still queued at failover fail on the new node and must be run again. Plugin-local state is not copied between nodes.

Revoking either node dissolves the pair once any pending credential-key installation is reconciled. Access is revoked immediately. If installation on a surviving standby is still unresolved, that node remains reserved and cannot discover, pair miners, or receive another standby; reconnect it and retry revocation after installation finishes. Revoking the standby itself can dissolve the pair immediately because that revoked identity cannot pair miners. Revoking an active node with a ready standby denies the node access, then moves its devices to the standby. If the standby is disconnected before or during hand-off, the pair, device assignments, and stored credentials remain intact across retries; reconnect the standby and retry revocation. An active node whose standby is not ready and has no pending installation is unpaired as usual. Revocation cannot erase the credential key from a machine: for a compromised node, revoke both nodes and rotate the affected miner credentials. Deploy the Fleet server before upgrading nodes. Before rolling back the HA migration, remove all HA pairs and cancel unexpired standby enrollments; the down migration refuses to discard that state. Rollback does not reverse completed device ownership moves or erase keys already installed on nodes.

## Security model

- **Transport.** `ValidateServerURL` requires `https://` for non-loopback servers; `--allow-insecure-transport` permits `http://` for testing only. The HTTP/2 transport is scheme-aware: `https://` goes through a TLS-validating `http2.Transport`, `http://` goes through h2c. See [client.go](../../internal/fleetnodebootstrap/client.go).
- **State file.** `state.yaml` is `0600` under a `0700` directory; the writer fsyncs the temp file, renames, then fsyncs the directory. Symlinks at the state dir leaf are refused.
- **Lock contention.** PID is written under the lock so contention reports are actionable.
- **Plugins.** The directory must be owned by root or the running uid and must not be group- or world-writable; the agent refuses to load otherwise. The Windows build performs an existence-only check — production Windows installs must place the binary under an Administrator-only directory (e.g., `%ProgramFiles%\fleetnode\`) so the `plugins\` subdirectory inherits a safe ACL.
- **Scan targets.** Server-supplied targets use the shared `netscan` grammar, private-address policy, and per-command limits. Leading dashes, whitespace, and shell metacharacters are rejected.
- **Server compatibility.** The control stream depends on RFC-0001 phase 2 server handlers; older servers return `Unimplemented` and the agent reconnects with backoff without crashing.

## Development

```bash
go test ./server/cmd/fleetnode -race -count=1
```

[fake_gateway_test.go](fake_gateway_test.go) provides an in-process h2c gateway for handler tests. Pair the agent with a local server via `just dev` (see [server/README.md](../../README.md)).

For manual end-to-end UI testing of fleet-node discovery and pairing, run from the repository root:

```bash
just fleetnode-ui-test-up
```

This starts the backend, isolated fake miners, an enrolled fleet node with `FLEETNODE_LOCAL_DISCOVERY_SUBNET=10.90.0.0/24`, and the ProtoFleet client. On a fresh dev database it creates `admin` / `Pass123!`; if your local database already has a different admin user, run with `FLEET_ADMIN_USERNAME` and `FLEET_ADMIN_PASSWORD` set. In the UI, use Fleet → Miners → Add miners → Scan network. The fake miners live on a Docker network that only the fleet node can reach, so the existing UI exercises the fleet-node routing path without cloud discovery competing for the same rows. Stop the stack with `just fleetnode-ui-test-down`; reset the fleetnode state with `just fleetnode-ui-test-reset`.

## Troubleshooting

| Symptom | Cause | Action |
|---------|-------|--------|
| `exec format error` from plugin | Plugins were built for a different OS/arch (often Linux ELF on macOS). | Re-run `just build-fleetnode` on the host that will execute the agent. |
| `state lock held by PID N` | Another `fleetnode` process is holding the lock; or a stale lock file with a dead PID. | `ps -p N`. If dead, remove the lock file. |
| `control stream returned Unimplemented` | Server is older than RFC-0001 phase 2. | Upgrade fleetd to a build that implements `ControlStream` and `ReportDiscoveredDevices`. Until then the agent stays in heartbeat-only mode. |
| `fleet node already has an active control stream` | A prior stream is still being reaped on the server. | Self-resolves: the server now applies newest-wins, evicting the prior stream on the next connect. |
| `BeginAuth rejected` | Revoked `api_key`, identity_pubkey mismatch, expired challenge, or server clock drift. | Verify the `api_key` in the operator UI; re-enroll if revoked. Check clock skew. |
