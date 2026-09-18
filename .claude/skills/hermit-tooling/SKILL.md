---
name: hermit-tooling
description: Diagnose missing developer tools or Hermit activation failures in this repository.
---

# Tool availability

Determine whether this checkout is using Hermit or the supported non-Hermit
setup in [CONTRIBUTING.md](../../../CONTRIBUTING.md#development-setup).

For Hermit, activate with `source bin/activate-hermit` from the repo root and
check the required tool with `command -v`. The committed proxies are under
`bin/`; they resolve packages through Hermit, not a universal `.hermit/` bin
directory. Inspect `bin/hermit.hcl` and the actual proxy when resolution fails.

Distinguish missing packages from activation, cache-permission, and network
errors. Do not install global replacements to hide a Hermit failure. If
lefthook is unavailable, restore the toolchain and run `just install-hooks`;
do not bypass hooks. For non-Hermit setups, use the documented prerequisites.
Report unresolved setup blockers without claiming that validation ran.
