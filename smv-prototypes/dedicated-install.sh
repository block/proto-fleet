#!/usr/bin/env bash
# Restore a dedicated (non-symlinked) client/node_modules for this spike worktree.
# Run this AFTER Conductor's startup hook — it undoes the shared symlink so the
# npm-workspace reorg can own its own install. Idempotent: no-op if already real.
set -euo pipefail
SHARED="/Users/flesher/Development/proto-fleet/client/node_modules"
NM="$(cd "$(dirname "$0")/.." && pwd)/client/node_modules"
if [ -L "$NM" ] || [ ! -e "$NM" ]; then
  echo "Materializing dedicated node_modules at $NM ..."
  rm -rf "$NM"
  cp -a "$SHARED" "$NM"
  echo "Done. $(ls "$NM" | wc -l | tr -d ' ') packages (real dir)."
else
  echo "Already a real directory — leaving as-is."
fi
