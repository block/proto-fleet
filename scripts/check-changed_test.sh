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
  <(find .claude/skills -mindepth 1 -maxdepth 1 -type d -exec basename {} \; | sort) \
  <(find .agents/skills -mindepth 1 -maxdepth 1 -type l -exec basename {} \; | sort)

for claude_skill in .claude/skills/*; do
  name="${claude_skill##*/}"
  codex_skill=".agents/skills/$name"
  if [ ! -L "$codex_skill" ]; then
    echo "Codex skill is not a symlink: $codex_skill" >&2
    exit 1
  fi
  if ! grep -q "^name: $name$" "$claude_skill/SKILL.md"; then
    echo "Skill name does not match its directory: $claude_skill" >&2
    exit 1
  fi
  cmp "$claude_skill/SKILL.md" "$codex_skill/SKILL.md"
done

echo "developer workflow configuration and agent skill parity passed"
