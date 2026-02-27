# Proto Fleet Specdocs

Living technical specifications for the Proto Fleet repository.

## What Are Specdocs?

Specdocs describe how a system works **right now** (or, in spec-ahead mode, how it should work next). They are the authoritative source of truth for each subsystem's architecture, interfaces, data flows, and constraints — not READMEs, design docs, or post-hoc documentation.

- A **README** says "here's how to get started."
- A **CLAUDE.md** says "here's how to work here."
- A **specdoc** says "here's exactly how this system works, why it works that way, and what contracts it maintains."

## Quick Start

**Creating a new spec (agent-assisted, recommended):**
```
Create a spec for the <domain> domain. Use specs/TEMPLATE.md as the template.
The code is in <path>. Put it in specs/<category>/NNNN-<name>.md.
```

**Creating a new spec (manual):**
```bash
cp specs/TEMPLATE.md specs/<category>/NNNN-<name>.md
```

## Index

### System-Level

| Spec | Status | Description |
|------|--------|-------------|
| [0000-system-architecture](./0000-system-architecture.md) | — | Top-level system overview |

### Client (`client/`)

| Spec | Status | Description |
|------|--------|-------------|
| [0001-protofleet-app](./client/0001-protofleet-app.md) | — | ProtoFleet management UI |
| [0002-protoos-app](./client/0002-protoos-app.md) | — | ProtoOS single-miner dashboard |
| [0003-shared-component-library](./client/0003-shared-component-library.md) | — | Shared UI components (50+) |
| [0004-miner-renaming](./client/0004-miner-renaming.md) | spec-ahead | Miner display name customization |

### Server (`server/`)

| Spec | Status | Description |
|------|--------|-------------|
| [0010-fleet-server-architecture](./server/0010-fleet-server-architecture.md) | — | Server architecture overview |
| [0011-telemetry-domain](./server/0011-telemetry-domain.md) | — | Telemetry collection & TimescaleDB |
| [0012-pairing-domain](./server/0012-pairing-domain.md) | — | Device discovery & registration |
| [0013-command-domain](./server/0013-command-domain.md) | — | Async command execution queue |
| [0014-auth-domain](./server/0014-auth-domain.md) | — | Token-based auth (client + miner) |
| [0015-fleet-management-domain](./server/0015-fleet-management-domain.md) | — | Fleet operations, filtering, sorting |
| [0016-miner-domain](./server/0016-miner-domain.md) | — | Core miner entity & lifecycle |
| [0017-miner-renaming](./server/0017-miner-renaming.md) | spec-ahead | Miner display name persistence |

### Plugins (`plugins/`)

| Spec | Status | Description |
|------|--------|-------------|
| [0020-plugin-sdk-v1](./plugins/0020-plugin-sdk-v1.md) | — | SDK interfaces & contracts (parent) |
| [0021-proto-plugin](./plugins/0021-proto-plugin.md) | — | Proto miner plugin (child) |
| [0022-antminer-plugin](./plugins/0022-antminer-plugin.md) | — | Antminer plugin (child) |

### Deployment (`deployment/`)

| Spec | Status | Description |
|------|--------|-------------|
| [0030-windows-installer](./deployment/0030-windows-installer.md) | — | Windows Fleet Installer (C#/WPF) |
| [0031-server-deployment](./deployment/0031-server-deployment.md) | — | Server deployment & Docker Compose |

### API (`api/`)

| Spec | Status | Description |
|------|--------|-------------|
| [0040-grpc-api](./api/0040-grpc-api.md) | — | gRPC/Connect API surface |
| [0041-protobuf-schema](./api/0041-protobuf-schema.md) | — | Proto definitions & code generation |

## Numbering Convention

| Range | Category |
|-------|----------|
| `0000` | System-level (cross-cutting) |
| `0001–0009` | Client applications |
| `0010–0019` | Server domains |
| `0020–0029` | Plugin system |
| `0030–0039` | Deployment & infrastructure |
| `0040–0049` | API & protocols |

Numbers are identifiers, not priorities. Leave gaps for future specs (e.g., `0011`, `0012`, `0015` — not `0011`, `0012`, `0013`).

## Status Values

| Status | Meaning |
|--------|---------|
| `draft` | Spec is being written. Not yet authoritative. |
| `active` | Spec accurately describes current system behavior. |
| `spec-ahead` | Spec updated ahead of code. Implementation needed. |
| `deprecated` | Subsystem removed or replaced. Kept for reference. |

## Relationship to Other Docs

| Document | Purpose |
|----------|---------|
| `README.md` | Quick start, project overview, "how to use" |
| `CLAUDE.md` | Development workflow, "how to work here" |
| **Specdoc** | Technical specification, "how it works and why" |
| Plugin guides | Tutorial-style development guidance |
| API docs | Generated API reference |

Existing READMEs and CLAUDE.md files are not replaced — specdocs complement them by providing deep technical detail they link to.

## Maintenance

- **Update specs alongside code.** If a PR changes behavior covered by a spec, the spec must be updated in the same PR.
- **Set `last-verified`** when confirming a spec matches the code.
- **Quarterly reviews**: assign team members to spot-check specs older than 6 months.
- **Agent-assisted review**: "Verify that `specs/server/0011-telemetry-domain.md` is still accurate." The agent reads the spec, reads `code-refs`, and flags discrepancies.
