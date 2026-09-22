---
name: asicrs-build
description: Rebuild ASIC-rs after changes to plugin/asicrs, sdk/rust, or server/sdk/v1/pb inputs.
---

# ASIC-rs builds

The `justfile` checks source mtimes against the plugin binary and its
`.asicrs-platform` marker. Branch switches or checkouts can leave a newer
binary beside different, older sources. Force a rebuild when verifying
changes to these inputs rather than trusting that cache.

- Docker dev runtime (Linux ARM64): `just rebuild-plugin asicrs` clears the
  marker and invokes the Docker build.
- Native runtime: remove only the generated
  `server/plugins/.asicrs-platform` marker, then run `just _asicrs-build`.
  On macOS this uses local Cargo; on Linux the recipe uses Docker.
  Do not treat the Docker ARM64
  artifact as a macOS-native binary.

Run `just test-contract` for miner-driver behavior changes on a compatible
Linux runner. The current recipe depends on `_asicrs-build` but executes the
plugin inside Linux containers. On macOS that dependency produces a Mach-O
binary, so the ASIC-rs contract checks are blocked by the recipe's platform
mismatch. A preceding Docker build does not fix this: the dependency switches
the marker back to native. Report the blocker or use an authorized Linux
runner; do not claim native build success verifies the container contracts.

Switching between
native and Docker runtimes changes the marker and requires a rebuild.
Do not hand-edit `server/plugins/asicrs-config.yaml` to pass tests; the
contract harness rewrites it for each suite.
