# fleetnode

`fleetnode` is the on-prem agent that enrolls with a fleet server, holds a session, and (in later stack PRs) opens a `ControlStream` to execute server-issued discovery commands against the operator's local network.

## Subcommands

| Command | Purpose |
|---------|---------|
| `fleetnode enroll`  | Register with a fleet server using a one-time enrollment code. Persists keys and `api_key`. See [enroll.go](enroll.go). |
| `fleetnode status`  | Print local state (server URL, fleet_node_id, fingerprint, session expiry). See [status.go](status.go). |
| `fleetnode refresh` | Renew the session token using the stored `api_key`. See [refresh.go](refresh.go). |

`fleetnode run` lands in the run-heartbeat PR.

## State directory and lock

State lives in `state.yaml` under one of, in order:

1. `--state-dir <path>` (override; primarily for tests)
2. `$XDG_STATE_HOME/fleetnode`
3. `~/.local/state/fleetnode`

The file holds `server_url`, `fleet_node_id`, `identity_fingerprint`, both keypairs, the `api_key`, and the current `session_token`. It is created `0600` under a `0700` directory. See [state.go](../../internal/fleetnodebootstrap/state.go).

A `state.lock` file in the same directory serializes commands. Only one process may hold it at a time (`LOCK_EX | LOCK_NB`). The PID of the holder is written under the lock; if another invocation hits contention, the error includes that PID. To clear a stale lock, identify the owner with `ps -p <pid>`; remove the lock file only if the process is gone.

## Build

```bash
just build-fleetnode               # produces server/.fleetnode/{fleetnode, nmap, plugins/}
go build -o fleetnode ./server/cmd/fleetnode   # fast iteration
```

The staged layout reserves a `plugins/` subdirectory and a `nmap` symlink for the discovery PR. The agent does not yet exec anything from those locations.

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

## Development

```bash
go test ./server/internal/fleetnodebootstrap/... -race -count=1
```
