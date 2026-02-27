---
id: "NNNN"
title: "Spec Title"
status: draft          # draft | active | spec-ahead | deprecated
parent: ""             # path to parent spec (if this is a child spec)
related:               # paths to related specs in other domains
  - specs/server/0011-telemetry-domain.md
last-verified: ""      # ISO date when spec was last verified against code
code-refs:             # primary source files/directories (supports globs, e.g. server/**/*.go)
  - server/internal/domain/telemetry/
  - client/src/protoFleet/features/telemetry/
---

# {Title}

> One-sentence summary of what this spec covers.

## Overview

**Purpose**: What this subsystem does and why it exists.

**Scope**: What is and isn't covered by this spec. Reference other specs for out-of-scope topics.

**Audience**: Who should read this spec (e.g., "developers working on telemetry features").

<!-- Skip this section only if the purpose is obvious from the title alone. -->

## Context & Background

Why this subsystem exists, what problem it solves, and key decisions that shaped its current design.

<!-- Skip for straightforward subsystems where the Overview section is sufficient. -->

## Architecture

### Components

Describe the major components, their responsibilities, and how they interact.

### Patterns

Architectural patterns in use (e.g., repository pattern, event-driven, polling-based).

### Technology Stack

Specific technologies, libraries, and frameworks used by this subsystem.

```
                    ┌──────────────┐
                    │   Diagram    │
                    └──────────────┘
```

<!-- Always include for parent specs. Can be brief for leaf specs. -->

## Interfaces & Contracts

### Public API

External-facing interfaces (gRPC services, REST endpoints, exported functions).

### Internal Interfaces

Interfaces between components within this subsystem.

### Data Structures

Key types, DTOs, and domain objects.

<!-- Include for any subsystem that has consumers. Skip for pure leaf implementations. -->

## Examples

Canonical usage patterns for this subsystem. Include the minimum viable example that demonstrates correct usage.

<!-- Include for subsystems with a public API or SDK. Skip for internal-only subsystems. -->

## Data Flow

### Primary Workflows

Step-by-step description of the main data flows through this subsystem.

### Error Handling

How errors propagate, what error types are used, and recovery strategies.

### State Transitions

State machines or lifecycle states if applicable.

<!-- Include when the subsystem processes data through multiple stages. -->

## Data Storage

### Schema

Database tables, indexes, and constraints relevant to this subsystem.

### Data Lifecycle

Retention policies, cleanup processes, and data growth expectations.

### Performance

Query patterns, known bottlenecks, and optimization strategies.

<!-- Include only for subsystems with persistent storage. -->

## Configuration

### Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `EXAMPLE_VAR` | `value` | What it controls |

### Config Files

Configuration files and their format.

<!-- Include when the subsystem has configurable behavior. -->

## Security

### Authentication

How this subsystem authenticates requests.

### Authorization

Access control and permission model.

### Data Protection

Encryption, secrets handling, and sensitive data management.

<!-- Include for subsystems that handle auth, secrets, or user data. -->

## Testing

### Strategy

How this subsystem is tested (unit, integration, e2e).

### Test Fixtures

Simulators, mocks, or test data used.

### Known Gaps

Areas with insufficient test coverage.

<!-- Include when testing approach is non-obvious or uses special infrastructure. -->

## Verification

Commands to verify this subsystem works correctly:

| Command | What it checks |
|---------|----------------|
| `go test ./server/internal/domain/example/...` | Unit and integration tests |
| `just dev` then check streaming | Live behavior verification |

<!-- Include when specific test or verification commands exist. Enables agents to self-correct: run command → observe failure → iterate. -->

## Deployment

### Build Process

How this subsystem is built and packaged.

### Deployment Steps

How it gets deployed to production.

### Rollback

How to roll back a bad deployment.

<!-- Include for independently deployable components. -->

## Limitations & Known Issues

Current limitations, technical debt, and planned improvements. Link to relevant issues where applicable.

<!-- Always include if there are known limitations. This is one of the most valuable sections. -->

## Open Questions

Unresolved design questions or areas needing investigation.

<!-- Include during draft status. Remove or convert to Limitations when resolved. -->

## Related Specifications

| Spec | Relationship |
|------|-------------|
| [0000-system-architecture](../0000-system-architecture.md) | Parent system context |

## Changelog

| Date | Author | Change | Reason |
|------|--------|--------|--------|
| YYYY-MM-DD | Name | Initial draft | — |
