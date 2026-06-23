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
├── nmap         (symlink to system nmap, if present)
└── plugins/
    ├── proto-plugin
    ├── antminer-plugin
    ├── virtual-plugin
    └── asicrs-plugin
```

## Nmap

When a server-issued `DiscoverRequest` arrives in `NmapModeRequest` form, the agent shells out to `<exe-dir>/nmap` if that file exists and is executable, otherwise to `nmap` on `PATH`. Install via `brew install nmap` (macOS) or your distro package manager.

The target is validated against a strict grammar before invocation: bare IPv4/IPv6, CIDR, `A.B.C.D-N` range, or hostname. Leading dashes, whitespace, and shell metacharacters are rejected — this defends against a compromised server crafting a target like `-iL/etc/passwd` that nmap would otherwise interpret as a flag. See `validateNmapTarget` in [nmap.go](nmap.go).

For automatic "local subnet" discovery commands, the server sends the reserved `fleetnode-local-subnet` target and the agent chooses what to scan. By default it detects the host's local private IPv4 subnet. On multi-NIC, NAT, or containerized hosts, set the subnet explicitly:

```bash
fleetnode run --local-discovery-subnet=10.90.0.0/24
FLEETNODE_LOCAL_DISCOVERY_SUBNET=10.90.0.0/24 fleetnode run
```

The configured subnet is validated the same way as an auto-detected local subnet: it must be a private IPv4 CIDR and no broader than the supported nmap scan-size limit.

## Control stream

`run` opens a bidirectional `ControlStream` over the gateway:

1. Agent dials gateway, sends `ControlHello`.
2. Server replies `ControlAccepted`; stream stays open.
3. Server pushes `ControlCommand{command_id, payload}`. Payload is a serialized `pairing.v1.DiscoverRequest`.
4. Agent runs the scan locally (plugin probes for `IPList`/`Mdns`, nmap for `Nmap`), batches results, and sends each batch via `ReportDiscoveredDevices` with `command_id` set.
5. Agent sends `ControlAck{command_id, succeeded}` on completion.

If the server side is older than RFC-0001 phase 2, the stream returns `Unimplemented`. The agent reconnects with exponential backoff (1s → 30s), so older servers degrade quietly. See [control.go](control.go).

Reconnect is newest-wins on the server: a freshly opened stream evicts any prior registration for the same `fleet_node_id`.

## Build

```bash
just build-fleetnode               # produces server/.fleetnode/{fleetnode, nmap, plugins/}
go build -o fleetnode ./server/cmd/fleetnode   # fast iteration
```

## Run

```bash
fleetnode enroll --server-url=https://fleet.example.com
fleetnode run     # long-running daemon; logs to stdout
```

Stop with `SIGINT`, `SIGTERM`, or `SIGHUP` (terminal close). On shutdown the agent closes the control stream, drains in-flight probes, and reaps plugin subprocesses. At startup it also reaps any orphan plugin processes left under `<exe-dir>/plugins/` by a previous crash — see [orphan_reaper.go](orphan_reaper.go).

## Enrollment flow

1. Operator mints an enrollment code in the UI and shares it.
2. `fleetnode enroll --server-url=...` prompts for the code, registers, prints a fingerprint.
3. Operator verifies the fingerprint in the UI and clicks confirm; the UI displays the `api_key`.
4. Agent prompts for the `api_key`, completes the handshake, persists session.
5. `fleetnode run` keeps the session refreshed and the control stream open.

If anything is interrupted between Register and Complete, `fleetnode refresh` resumes from the persisted state.

## Security model

- **Transport.** `ValidateServerURL` requires `https://` for non-loopback servers; `--allow-insecure-transport` permits `http://` for testing only. The HTTP/2 transport is scheme-aware: `https://` goes through a TLS-validating `http2.Transport`, `http://` goes through h2c. See [client.go](../../internal/fleetnodebootstrap/client.go).
- **State file.** `state.yaml` is `0600` under a `0700` directory; the writer fsyncs the temp file, renames, then fsyncs the directory. Symlinks at the state dir leaf are refused.
- **Lock contention.** PID is written under the lock so contention reports are actionable.
- **Plugins.** The directory must be owned by root or the running uid and must not be group- or world-writable; the agent refuses to load otherwise. The Windows build performs an existence-only check — production Windows installs must place the binary under an Administrator-only directory (e.g., `%ProgramFiles%\fleetnode\`) so the `plugins\` subdirectory inherits a safe ACL.
- **Nmap targets.** Server-supplied targets are restricted by `validateNmapTarget` (no leading dashes, no whitespace, no shell metacharacters).
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
