---
title: "Discovery: ProtoFleet Single-Miner mode"
date: 2026-07-29
status: draft
type: plan
---

# Discovery: ProtoFleet Single-Miner mode

> Sub-discovery of
> [Fleet-native single-miner view](./2026-07-28-single-miner-views-on-fleet-backend-plan.md).
> Explores: if the single-miner view becomes fleet-native, can we start
> ProtoFleet in a "Single-Miner mode" that connects to one device and renders
> just that miner's view — as the replacement for ProtoOS-on-miner?

## TL;DR

**The single-miner *experience* is essentially already built and this is much
closer to a thin client flag than a new server profile — with one caveat.**
There are two very different interpretations, and they differ wildly in cost:

- **(A) "Point the normal fleet server at one device."** Near-zero backend work.
  Pair one device over the LAN; the existing `/miners/:id` route +
  `minerproxy` already render its single-miner view. The mode is mostly a
  **client entry-point + nav-gating + runtime config** change.
- **(B) "A stripped-down single-miner appliance"** (a true ProtoOS-on-miner
  replacement running on/near the device). **Medium-to-large.** Blocked by two
  hard server dependencies with no lite path today: **TimescaleDB/Postgres is
  unconditional** (no embedded/in-memory option) and the **proto plugin is
  mandatory**. A `MODE` config stub exists but is dead code — no working profile
  system to hang a "lite" mode on.

Recommendation: scope Single-Miner mode as **(A) a client mode over the standard
server** first; treat (B) as a separate infrastructure track if we want a genuine
on-device appliance.

## What already exists

- **Fleet-native single-miner view is live.** `client/src/protoFleet/router.tsx`
  mounts `/miners/:id` → `SingleMinerWrapper`, which renders the ProtoOS routes
  in `mode="fleet"`. (Today it proxies to the device via `minerproxy`; the main
  plan's job is to make it render from fleet data instead — but the *route and
  shell already exist*.)
- **The server can already reach one device with no fleet-node fabric.**
  Discovery (`ipscanner`) + pairing (`pairingDomain` + `plugins.NewPairer`) probe
  and pair a device over the LAN, server-local. `minerproxy` `resolveTarget`
  reads the device row's `IpAddress` and proxies straight to it. Fleet nodes are
  only needed for devices the server can't route to directly.
- **Single-device is already a supported dev/demo shape.** The `virtual` plugin
  (`plugin/virtual/`, `ENABLE_VIRTUAL_MINERS`, `VIRTUAL_MINER_COUNT`) simulates N
  miners — set count to 1 for a single device end-to-end. Fake device servers
  (`server/fake-antminer/`, `server/fake-proto-rig/`) exist for dev.

## Client bootstrap — where a mode flag would live

- Entry `client/src/protoFleet/main.tsx` → `mainWrapper.tsx` → `router.tsx`.
- **Backend selection is not runtime-configurable today.** Transport
  (`client/src/protoFleet/api/transport.ts`) hardcodes `baseUrl` to
  `${API_PROXY_BASE}/` = `/api-proxy` (`api/constants.ts`); dev Vite proxies it to
  `FLEET_PROXY_URL` (default `http://localhost:4000`, `client/vite.config.ts`).
  The client always assumes one fleet server on the same origin.
- **Runtime config hook already exists:** `window.__RUNTIME_CONFIG__`
  (`client/src/shared/observability/runtimeConfig.ts`), rendered by nginx at
  container start — so a prebuilt artifact can be reconfigured without a rebuild.
  This is the natural place for a `SINGLE_MINER` flag + target device id.
- Build-time flags live in `client/src/protoFleet/constants/featureFlags.ts`
  (`VITE_INFRASTRUCTURE_DEVICES_ENABLED`, `VITE_ALERTS_ENABLED`); polling in
  `constants/polling.ts` (`VITE_POLL_INTERVAL_MS`).

## Startup gating that Single-Miner mode must pass

- **First boot:** `GetFleetInitStatus` → `status.adminCreated`
  (`router.tsx` `authLoader`/`welcomeLoader`; also a health probe in
  `App.tsx` → `/fleet-down` on failure). Admin created via `CreateAdminLogin`.
- **Post-login onboarding:** `GetFleetOnboardingStatus` →
  `{devicePaired, poolConfigured}` (`api/useOnboardedStatus.ts`), gating
  `/onboarding/miners|security|settings`. Server derives this from device/pool
  rows (`onboardingDomain.NewService(deviceStore, poolStore, userStore)`).
- **Implication:** a single-miner appliance still needs an admin + one paired
  device. For a smooth appliance UX you'd want to **auto-pair the one device**
  and **bypass the pool-config onboarding step** — new gating logic, but the RPCs
  already exist.

## Server bootstrap & the (B) blockers

- Entry `server/cmd/fleetd/main.go`; config `server/cmd/fleetd/config.go` (Kong,
  env + `/etc/fleetd/config.yaml`, prefixes `DB_`, `HTTP_`, `AUTH_`, `SESSION_`,
  `PLUGINS_`, `TELEMETRY_`, `TIMESCALEDB_`, …).
- **Dead mode stub:** `config.go` `Mode string enum:"server,agent,combined"
  default:"combined" env:"MODE"` is defined but **never read** at startup. No
  execution-mode branching exists to build on.
- **Hard deps (won't boot without):**
  - **Postgres/TimescaleDB** — `db.ConnectAndMigrate(&config.DB)`; every store is
    built on it; one DB serves relational + time-series. **No embedded/in-memory
    option** — Postgres is unconditional.
  - **proto plugin** — `main.go` fatally errors if `DriverNameProto` isn't loaded
    ("proto plugin is required").
- **Not required:** **no NATS anywhere** (command dispatch is a DB-backed queue,
  `queue.NewDatabaseMessageQueue`); metrics/Grafana/alerts, system monitoring,
  OTel, MQTT curtailment are all optional/gated. Fleet nodes are separate
  binaries (`server/cmd/fleetnode/`), not part of core.
- **Minimum footprint** = `fleetd` + one TimescaleDB/Postgres + the proto plugin
  binary — i.e. the existing `just dev` / `dev.sh` footprint. That's the floor
  for interpretation (B) unless we build a lite profile.

## Lift estimate

| Track | Scope | Lift |
| --- | --- | --- |
| Client mode (A) | Boot flag → land on `/miners/:id` for the one device, hide fleet-scope nav, target device via `window.__RUNTIME_CONFIG__` | **Small** (routing/entry + nav gating + runtime knob) |
| Onboarding/auth | Still pass init + onboarding gates; auto-pair the device, bypass pool-config step | **Small–medium** (RPCs exist; new gating) |
| Lite server appliance (B) | Shed/shrink the TimescaleDB + proto-plugin hard deps; build a real `MODE=single`/lite profile | **Medium–large** (unconditional Postgres has no embedded path; `MODE` stub is non-functional) |

## Recommendation & open questions

- **Recommend:** define Single-Miner mode as **interpretation (A)** — a client
  mode running against a standard (possibly co-located) fleet server, landing
  directly on the one device's fleet-native view with fleet-scope nav hidden.
  This is small and rides entirely on machinery that already exists.
- Depends on the main plan reaching fleet-native parity for the single-miner view
  (otherwise (A) still leans on `minerproxy`, which is fine as an interim).
- **(B) is a separate infra decision:** is the goal a lightweight *appliance*
  that runs on/near the miner? If so, the Postgres/plugin footprint — not the UI —
  is the real project, and it overlaps with "what replaces ProtoOS-on-miner"
  (main-plan assumption 1, explicitly deferred).
- Open questions: how is the target device selected/persisted in (A)? Does the
  appliance auto-pair on first boot? Do we hide *all* fleet nav or keep a minimal
  settings surface? Single-user vs full RBAC on an appliance?

## Key references

- `client/src/protoFleet/router.tsx`, `.../SingleMinerWrapper/SingleMinerWrapper.tsx`
- `client/src/protoFleet/api/transport.ts` + `constants.ts`; `client/vite.config.ts`
- `client/src/shared/observability/runtimeConfig.ts`; `client/src/protoFleet/constants/featureFlags.ts`
- `client/src/protoFleet/api/{useAuth,useOnboardedStatus}.ts`; `proto/onboarding/v1/onboarding.proto`
- `server/cmd/fleetd/main.go` (deps: DB connect, proto-plugin required, pairer, minerproxy registration), `server/cmd/fleetd/config.go` (`Mode` stub)
- `server/internal/handlers/minerproxy/handler.go`; `server/internal/domain/{pairing,ipscanner}`
- `plugin/virtual/`; `server/fake-antminer/`, `server/fake-proto-rig/`
