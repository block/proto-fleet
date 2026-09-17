#!/usr/bin/env bash
set -euo pipefail

lefthook validate

lefthook_config="$(lefthook dump --format json)"
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
