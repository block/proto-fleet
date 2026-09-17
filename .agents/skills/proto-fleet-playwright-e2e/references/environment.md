# E2E environment

Read the relevant suite's README and `config/test.config.ts`, defaults, and
applicable overrides before running state-changing scenarios. ProtoFleet can
select fake or real targets through configuration, including `E2E_TARGET`.
Verify the actual target rather than inferring isolation from localhost.

Use the current environment when it fits the authorized task. Onboarding and
auth state may be required even when the app is running; use existing setup
specs/helpers. Do not skip prerequisites and then claim a verified scenario.
For authorized local simulator work, restore setup as needed. Ask only when
setup would affect a real/shared target, destroy existing data, or require a
choice the request does not settle. Report the exact blocker if unavailable.

Prefer simulator-friendly scenarios. Fresh environments may support shell
and state-transition checks before stable telemetry assertions. Assert exact
telemetry or topology only when fixtures guarantee it. For auth-gated ProtoOS
flows, use realistic authentication rather than exposing stores/globals; report
any coverage gaps that cannot be exercised cleanly.

After an environment restart, rerun the previously blocked targeted test.
Shared fake-antminer/fake-proto-rig changes also affect contract tests and
local dev; use the fixture skill when those behavior changes are authorized.
