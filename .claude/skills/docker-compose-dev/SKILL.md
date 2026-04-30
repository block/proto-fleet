---
name: docker-compose-dev
description: Use when editing `server/docker-compose.yaml` or `server/docker-compose.base.yaml`, or when troubleshooting a `just dev` startup. Proto Fleet requires Docker host networking on macOS/Windows; service definitions split between `base` (shared) and the runtime override file.
---

# docker-compose-dev

`just dev` runs the stack via Docker Compose. The split is:

- `docker-compose.base.yaml` — shared service definitions
- `docker-compose.yaml` — local-development overrides that extend `base`

On macOS and Windows (via Docker Desktop), **host networking must be enabled**
in Docker Desktop settings (`Settings → Resources → Network → Enable host
networking`) — without it, the server cannot reach plugins or fake rigs on
their bound ports. Linux gets this implicitly.

## What to do

1. When adding or modifying a service, check whether it belongs in `base`
   (shared with deployment artifacts) or only in the dev override.
2. Preserve port bindings that match what plugins and `fake-antminer` /
   `fake-proto-rig` expect (4028 for cgminer, 80 for Antminer Web API).
3. If a service fails to come up, first verify host networking is enabled
   on Docker Desktop before debugging YAML.

## What to avoid

- Don't switch services from host networking to bridge networking to "fix"
  a port conflict — it breaks plugin↔miner communication on Mac/Win.
- Don't add new exposed-port mappings that overlap the cgminer/web API
  ports already consumed by fake rigs.
