# Protobuf changes

Sources live under `proto/` and `server/sdk/v1/pb/`. Follow
[contract guidance](../../../../proto/AGENTS.md), then use root `just gen`
to drive the configured generation pipeline rather than ad-hoc protoc calls.

Review generated output by consumer (Go server, SDKs, TypeScript clients).
For service methods, removed fields, or presence changes, verify affected
handlers in `server/internal/handlers/`, client API hooks under
`client/src/{app}/api/`, and other callers agree with the contract.

SDK inputs under `server/sdk/v1/pb/` also affect ASIC-rs; use the
[build skill](../../asicrs-build/SKILL.md) when verifying that plugin.
