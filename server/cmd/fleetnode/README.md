# fleetnode

`fleetnode` is the on-prem agent that enrolls with a fleet server, holds a session, and (in later stack PRs) opens a `ControlStream` to execute server-issued discovery commands against the operator's local network.

## Subcommands

| Command | Purpose |
|---------|---------|
| `fleetnode enroll`  | Register with a fleet server using a one-time enrollment code. Persists keys and `api_key`. See [enroll.go](enroll.go). |
| `fleetnode status`  | Print local state (server URL, fleet_node_id, fingerprint, session expiry). See [status.go](status.go). |
| `fleetnode refresh` | Renew the session token using the stored `api_key`. See [refresh.go](refresh.go). |
| `fleetnode run`     | Long-running daemon: heartbeat. See [run.go](run.go). |

## State directory and lock

State lives in `state.yaml` under one of, in order:

1. `--state-dir <path>` (override; primarily for tests)
2. `$XDG_STATE_HOME/fleetnode`
3. `~/.local/state/fleetnode`

The file holds `server_url`, `fleet_node_id`, `identity_fingerprint`, both keypairs, the `api_key`, and the current `session_token`. It is created `0600` under a `0700` directory. See [state.go](../../internal/fleetnodebootstrap/state.go).

A `state.lock` file in the same directory serializes commands. Only one process may hold it at a time (`LOCK_EX | LOCK_NB`). The PID of the holder is written under the lock; if another invocation hits contention, the error includes that PID. To clear a stale lock, identify the owner with `ps -p <pid>`; remove the lock file only if the process is gone.

## Plugins

Discovery (added in the next stack PR) is plugin-driven. The agent loads every executable in `<exe-dir>/plugins` at startup (subprocess plugins over gRPC via `hashicorp/go-plugin`). If that directory is missing the agent runs in heartbeat-only mode; if it is present but loosens beyond owner-only write, the agent refuses to start (`plugins dir ... must not be group- or world-writable`).

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

Stop with `SIGINT`, `SIGTERM`, or `SIGHUP` (terminal close). On shutdown the agent closes any open streams and reaps plugin subprocesses. At startup it also reaps any orphan plugin processes left under `<exe-dir>/plugins/` by a previous crash — see [orphan_reaper.go](orphan_reaper.go).

## Enrollment flow

1. Operator mints an enrollment code in the UI and shares it.
2. `fleetnode enroll --server-url=...` prompts for the code, registers, prints a fingerprint.
3. Operator verifies the fingerprint in the UI and clicks confirm; the UI displays the `api_key`.
4. Agent prompts for the `api_key`, completes the handshake, persists session.

If anything is interrupted between Register and Complete, `fleetnode refresh` resumes from the persisted state.

## Security model

- **Transport.** `ValidateServerURL` requires `https://` for non-loopback servers; `--allow-insecure-transport` permits `http://` for testing only. The HTTP/2 transport is scheme-aware: `https://` goes through a TLS-validating `http2.Transport`, `http://` goes through h2c. See [client.go](../../internal/fleetnodebootstrap/client.go).
- **State file.** `state.yaml` is `0600` under a `0700` directory; the writer fsyncs the temp file, renames, then fsyncs the directory. Symlinks at the state dir leaf are refused.
- **Lock contention.** PID is written under the lock so contention reports are actionable.
- **Plugins.** The directory must be owned by root or the agent uid and not group- or world-writable; the agent refuses to load otherwise. Individual plugin files must be regular executables (no symlinks) with the same ownership + write-bit rules. The Windows build performs an existence-only check — production Windows installs must place the binary under an Administrator-only directory (e.g., `%ProgramFiles%\fleetnode\`).

## Development

```bash
go test ./server/cmd/fleetnode -race -count=1
go test ./server/internal/fleetnodebootstrap/... -race -count=1
```

## Troubleshooting

| Symptom | Cause | Action |
|---------|-------|--------|
| `exec format error` from plugin | Plugins were built for a different OS/arch (often Linux ELF on macOS). | Re-run `just build-fleetnode` on the host that will execute the agent. |
| `state lock held by PID N` | Another `fleetnode` process is holding the lock; or a stale lock file with a dead PID. | `ps -p N`. If dead, remove the lock file. |
| `BeginAuth rejected` | Revoked `api_key`, identity_pubkey mismatch, expired challenge, or server clock drift. | Verify the `api_key` in the operator UI; re-enroll if revoked. Check clock skew. |
