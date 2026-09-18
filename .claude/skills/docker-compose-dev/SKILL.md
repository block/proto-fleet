---
name: docker-compose-dev
description: Edit the development Compose stack or diagnose just dev startup and networking failures.
---

# Development Compose

`just dev` builds Docker plugins and invokes `dev.sh`. Inspect that script
and the relevant Compose files when diagnosing startup:

- `server/docker-compose.base.yaml`: shared definitions consumed by dev and
  deployment overrides.
- `server/docker-compose.yaml`: local development services and overrides.

The current dev stack uses `fleet-network` with explicit port mappings.
Do not require Docker Desktop host networking or change the network mode
based on older setup instructions. Verify the effective Compose service,
container address, published host port, and plugin configuration involved.

Keep shared definitions in the base and dev-only behavior in the override.
Compose arrays such as `command` replace the base array; check that an
override retains necessary settings. Preserve miner protocol ports inside
containers (for example cgminer 4028 and Antminer HTTP 80), distinguishing
them from published host ports. Avoid overlapping host bindings when adding
fake rigs or other services.
