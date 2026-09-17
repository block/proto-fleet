#!/usr/bin/env bash
set -euo pipefail

lefthook validate

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
