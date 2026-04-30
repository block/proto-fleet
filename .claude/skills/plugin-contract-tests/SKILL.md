---
name: plugin-contract-tests
description: Use when editing miner-protocol code under `plugin/proto/`, `plugin/antminer/`, or `plugin/virtual/`, or when changing test fixtures consumed by `tests/plugin-contract/`. The contract suite is the canonical check that a plugin still meets the miner driver contract; it is Docker-heavy and easy to skip, but plugin behavior regressions only surface here.
---

# plugin-contract-tests

The contract suite (`tests/plugin-contract/`) runs each miner test in its own
container so port-bound mocks (`fake-antminer`, ASIC-rs harnesses) don't
collide. Pre-push lefthook does **not** run it. Server `just lint` does **not**
run it. CI runs it on PR via `protofleet-contract-checks.yml`, but by then
it's a feedback delay.

## What to do

1. After plugin or fixture edits, run `just test-contract` from the repo
   root. It builds the Go plugins, compiles the test binary once, and runs
   `TestAntminerStock`, `TestAntminerVNish`, and `TestWhatsMinerStock` in
   isolated containers.
2. Surface the failing test name and the plugin under test (the test names
   map to the miner model, not the plugin module — read the suite output
   carefully).
3. If the change is a deliberate behavior change, add or update a contract
   testdata fixture under `tests/plugin-contract/testdata/` or the relevant
   miner harness — don't loosen the test to make it pass.

## What to avoid

- Don't run the contract tests outside Docker. The harness assumes container
  port isolation; running on the host will race with anything bound to 4028
  or 80.
