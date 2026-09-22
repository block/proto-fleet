---
name: fake-rig-fixtures
description: Update fake-antminer or fake-proto-rig fixtures and verify their shared consumers.
---

# fake-rig-fixtures

`server/fake-antminer/` simulates the cgminer API (port 4028) and Antminer
Web API (port 80). `server/fake-proto-rig/` simulates the Proto miner REST
API. They are linked into multiple test surfaces:

- Plugin contract tests (`tests/plugin-contract/miners/`)
- E2E tests (`client/e2eTests/`)
- Local `just dev` (when virtual or fake plugins are configured)

A change that "fixes" one surface can break the others without the agent
noticing.

## What to do

1. After editing handler responses, models, or default config, identify
   which surfaces consume the fixture and run the relevant suite:
   - Plugin contract: `just test-contract`
   - ProtoFleet E2E: `just test-e2e-fleet`
   - ProtoOS E2E: `just test-e2e-protoos`
2. If the change is a new field or new endpoint, check that consumers
   under `plugin/antminer/` (or `plugin/proto/`) actually parse it. A
   silently-ignored field is the most common bug here.
3. The Antminer Web API uses digest auth with `root`/`root` defaults —
   don't change auth semantics without flagging it explicitly.

## What to avoid

- Don't add request-handler logic that depends on real-time clocks or
  external services. The fixtures must be deterministic for contract
  testing.
