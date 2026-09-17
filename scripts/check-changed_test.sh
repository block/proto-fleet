#!/usr/bin/env bash
set -euo pipefail

lefthook validate

lefthook_config="$(lefthook dump --format json)"
rename_safe_diffs="$(
  jq -r '."changed-checks".files' <<<"$lefthook_config" |
    awk '/git diff --no-renames.*--name-only/ { count++ } END { print count + 0 }'
)"
if [ "$rename_safe_diffs" -ne 3 ]; then
  echo "Changed-path routing must collect both sides of renames" >&2
  exit 1
fi

for job in plugin-proto-lint plugin-antminer-lint; do
  for shared_path in server/go.mod server/go.sum 'server/sdk/**'; do
    if ! jq -e --arg job "$job" --arg path "$shared_path" \
      '."changed-checks".commands[$job].glob | index($path)' \
      <<<"$lefthook_config" >/dev/null; then
      echo "$job does not run for shared dependency: $shared_path" >&2
      exit 1
    fi
  done
done

for job in protobuf-lint client-check server-lint plugin-proto-lint plugin-antminer-lint; do
  if ! jq -e --arg job "$job" \
    '."changed-checks".commands[$job].glob | index("justfile")' \
    <<<"$lefthook_config" >/dev/null; then
    echo "$job does not run when justfile changes" >&2
    exit 1
  fi
done

client_init_recipe="$(just --dry-run _client-init 2>&1)"
for install_input in client/package.json client/package-lock.json; do
  if [[ "$client_init_recipe" != *"$install_input"* ]]; then
    echo "Client dependency fingerprint omits: $install_input" >&2
    exit 1
  fi
done

plugin_build_recipe="$(just --dry-run _build-go-plugins-cross linux arm64 server/plugins 2>&1)"
while IFS= read -r module; do
  module="${module#./}"
  for dependency_file in "$module/go.mod" "$module/go.sum"; do
    if [ -f "$dependency_file" ] && [[ "$plugin_build_recipe" != *"$dependency_file"* ]]; then
      echo "Plugin build cache omits workspace dependency: $dependency_file" >&2
      exit 1
    fi
  done
done < <(go work edit -json | jq -r '.Use[].DiskPath')

diff -u \
  <(find .agents/skills -mindepth 1 -maxdepth 1 -type d -exec basename {} \; | sort) \
  <(find .claude/skills -mindepth 1 -maxdepth 1 -exec basename {} \; | sort)

for agent_skill in .agents/skills/*; do
  name="${agent_skill##*/}"
  claude_skill=".claude/skills/$name"
  if [ -L "$agent_skill" ] || [ ! -d "$agent_skill" ]; then
    echo "Canonical agent skill is not a directory: $agent_skill" >&2
    exit 1
  fi
  if ! grep -q "^name: $name$" "$agent_skill/SKILL.md"; then
    echo "Skill name does not match its directory: $agent_skill" >&2
    exit 1
  fi
  if [ ! -L "$claude_skill" ]; then
    echo "Claude skill is not a symlink: $claude_skill" >&2
    exit 1
  fi
  expected_target="../../.agents/skills/$name"
  if [ "$(readlink "$claude_skill")" != "$expected_target" ]; then
    echo "Claude skill does not link to its canonical definition: $claude_skill" >&2
    exit 1
  fi
  if [ ! -f "$claude_skill/SKILL.md" ]; then
    echo "Claude skill symlink does not resolve: $claude_skill" >&2
    exit 1
  fi
done

echo "developer workflow configuration and agent skill parity passed"
