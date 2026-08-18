---
title: One-Click Upgrade Host Executor - Plan
date: 2026-07-29
status: completed
type: plan
topic: one-click-upgrade
artifact_contract: ce-unified-plan/v1
artifact_readiness: implementation-ready
---

# One-Click Upgrade Host Executor

## Outcome

Proto Fleet now lets an authorized operator confirm an eligible upgrade in the
Updates settings route. A privileged process outside the Compose stack
validates and activates the release, while the client follows durable progress
through the expected Fleet restart. Hosts without a reachable executor retain
the release-specific manual install command as the authoritative fallback.

## Final architecture

### Client ownership

- The normal shell owns only a passive version pill. It performs lightweight
  release discovery and routes the operator to `/settings/updates`; it does not
  retain installer commands or own upgrade progress globally.
- The Updates route fetches authoritative status before exposing actions. It
  shows one-click controls only when Fleet reports a reachable host executor,
  and otherwise keeps the manual command path intact.
- Confirmation names the exact target and explains the restart window. Release
  candidates add the forward-only migration and no-downgrade warning.
- Confirmation, progress, reconnect, terminal error, recovery, and success are
  route-owned states. Leaving the route stops only browser polling; it does not
  cancel the durable host operation. Returning to the route recovers current
  state through the upgrade-status RPC.
- An ambiguous trigger keeps conflicting controls locked while the executor is
  unreachable. After a bounded wait, the manual path can be unlocked only by
  explicitly confirming on-host that no upgrade is still running.

### Privilege and execution boundary

- `fleetd` re-checks `instance:update`, the selected release channel, and the
  currently eligible target. The browser cannot provide a download URL,
  command, downgrade, or arbitrary release tag.
- The root systemd service runs outside the Compose stack it restarts. Fleet
  reaches its narrow HTTP API through
  `/run/proto-fleet-updater/updater.sock`; the application container never
  receives the host Docker socket.
- Before teardown, the updater downloads and verifies the release, safely
  extracts it, preserves deployment configuration, builds release-specific
  images, and completes preflight. Activation revalidates the staged manifest
  and image identities before switching the deployment.
- The supported topology is one Proto Fleet installation per host on Linux
  with systemd and rootful Docker, including WSL distributions configured with
  systemd. macOS, rootless Docker, Linux without systemd, and alternate
  multi-install layouts use the manual flow.

### Trust and recovery boundary

- The SHA-256 sidecar detects corruption and binds the expected asset name to
  its digest. The bundle and sidecar share a GitHub Release origin, so GitHub
  remains the publisher trust anchor; independent release signing is outside
  this phase.
- Runtime configuration (`.env`, TLS material, Influx configuration, and
  persisted optional-overlay settings) is carried into the staged deployment.
- Updater state and per-operation logs survive Fleet restarts at
  `/var/lib/proto-fleet-updater/state.json` and
  `/var/lib/proto-fleet-updater/logs/<operation-id>.log`.
- A terminal failure exposes the host log path and an explicit recovery
  command. The previous deployment remains at
  `<install-root>/deployment.previous` for inspection, but automatic binary
  rollback is disabled because migrations are forward-only.

## End-to-end flow

```mermaid
sequenceDiagram
  participant U as "Authorized operator"
  participant S as "Fleet shell"
  participant R as "Updates route"
  participant F as "fleetd"
  participant X as "Host updater (systemd)"
  participant G as "GitHub Releases"

  S->>F: "Discover eligible version"
  F-->>S: "Version-only indicator data"
  U->>S: "Select update pill"
  S->>R: "Navigate to /settings/updates"
  R->>F: "GetUpdateStatus"
  F-->>R: "Eligible release, capability, manual command"
  U->>R: "Confirm exact target"
  R->>F: "TriggerUpgrade(target version)"
  F->>F: "Re-check permission, channel, and eligibility"
  F->>X: "Start operation over Unix socket"
  X->>G: "Download bundle and checksum"
  X->>X: "Verify, stage, preflight, and activate"
  R-->>F: "Poll durable status; restart disconnect is expected"
  opt "Operator leaves and later returns"
    U->>R: "Open /settings/updates"
    R->>F: "GetUpgradeStatus"
  end
  F-->>R: "Active or terminal durable operation"
  R-->>U: "Progress, reload, or recovery guidance"
```

## Completed implementation units

1. Durable single-flight updater manager and Unix-socket API.
2. Packaged updater binary, hardened systemd service, and installer lifecycle.
3. Non-interactive preflight and activation with immutable staging checks.
4. Release checksum publication and bounded download/extraction validation.
5. Permission-gated trigger/status RPCs with server-side target validation.
6. Passive shell discovery plus route-owned confirmation and operation states.
7. Focused host, API, installer, and client lifecycle tests.
