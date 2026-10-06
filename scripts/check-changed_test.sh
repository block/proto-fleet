#!/usr/bin/env bash
set -euo pipefail

lefthook validate

lefthook_config="$(lefthook dump --format json)"

# Git exports repository-local environment variables to hooks. Clear them before
# creating fixture repositories so their commands cannot target the caller's repo.
while IFS= read -r git_env; do
  unset "$git_env"
done < <(git rev-parse --local-env-vars)

test_tmp="$(mktemp -d)"
trap 'rm -rf "$test_tmp"' EXIT
rename_safe_diffs="$(
  jq -r '."changed-checks".files' <<<"$lefthook_config" |
    awk '/changed_paths( |$)/ { count++ } END { print count + 0 }'
)"
if [ "$rename_safe_diffs" -ne 3 ]; then
  echo "Changed-path routing must collect both sides of renames" >&2
  exit 1
fi

if ! jq -r '."changed-checks".files' <<<"$lefthook_config" |
  grep -q '\$1 == "D" { deleted = 1 }'; then
  echo "Changed-path routing must detect deleted files" >&2
  exit 1
fi

deletion_fixture="$test_tmp/deletion-routing"
mkdir -p "$deletion_fixture/repo"
git -C "$deletion_fixture/repo" init -q
touch "$deletion_fixture/repo/justfile" "$deletion_fixture/repo/deleted.txt"
git -C "$deletion_fixture/repo" add justfile deleted.txt
git -C "$deletion_fixture/repo" \
  -c user.name='Developer Workflow Test' \
  -c user.email='developer-workflow-test@example.com' \
  commit -qm 'fixture base'
git -C "$deletion_fixture/repo" rm -q deleted.txt
git -C "$deletion_fixture/repo" \
  -c user.name='Developer Workflow Test' \
  -c user.email='developer-workflow-test@example.com' \
  commit -qm 'delete fixture file'

{
  cat <<'YAML'
glob_matcher: doublestar
changed-checks:
  files: |
YAML
  jq -r '."changed-checks".files' <<<"$lefthook_config" | sed 's/^/    /'
  cat <<'YAML'
  commands:
    deletion-sentinel:
      glob: "justfile"
      run: touch deletion-routed
YAML
} > "$deletion_fixture/lefthook.yml"

(
  cd "$deletion_fixture/repo"
  CHECK_CHANGED_BASE=HEAD~1 \
    LEFTHOOK_CONFIG="$deletion_fixture/lefthook.yml" \
    lefthook run changed-checks
)
if [ ! -f "$deletion_fixture/repo/deletion-routed" ]; then
  echo "Deletion-only changes did not run changed-path checks" >&2
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

ci_global_paths=()
while IFS= read -r global_path; do
  ci_global_paths+=("$global_path")
done < <(awk '
  /^global: &global$/ { capture = 1; next }
  capture && /^[^[:space:]]/ { exit }
  capture && /^  - / { sub(/^  - /, ""); gsub(/^"|"$/, ""); print }
' .github/path-filters.yml)

if [ "${#ci_global_paths[@]}" -eq 0 ]; then
  echo "CI global path filter is empty or could not be parsed" >&2
  exit 1
fi

for job in protobuf-lint client-check server-lint plugin-proto-lint plugin-antminer-lint; do
  for global_path in "${ci_global_paths[@]}"; do
    if ! jq -e --arg job "$job" --arg path "$global_path" \
      '."changed-checks".commands[$job].glob | index($path)' \
      <<<"$lefthook_config" >/dev/null; then
      echo "$job does not run for CI-global path: $global_path" >&2
      exit 1
    fi
  done
done

client_init_recipe="$(just --dry-run _client-init 2>&1)"
client_init_fingerprint="${client_init_recipe%%$'\nif [ "false" != true ]'*}"
for install_input in client/package.json client/package-lock.json client/.npmrc; do
  if [[ "$client_init_fingerprint" != *"$install_input"* ]]; then
    echo "Client dependency fingerprint omits: $install_input" >&2
    exit 1
  fi
done

for install_input in \
  'client/node_modules/.package-lock.json' 'git hash-object "$TREE_LOCK"' \
  'node --version' 'npm --version' 'node=$NODE_VERSION' 'npm=$NPM_VERSION' \
  process.platform 'host-platform=$HOST_PLATFORM' \
  process.arch 'host-arch=$HOST_ARCH' \
  glibcVersionRuntime '"glibc"' '"musl"' 'host-libc=$HOST_LIBC' \
  'install_args=(--include=dev --include=optional --dry-run=false)' \
  'install_args+=(--registry "$COREPACK_NPM_REGISTRY")' \
  'npm config list --json "${install_args[@]}"' \
  '"$INSTALL_CONFIG"'; do
  if [[ "$client_init_fingerprint" != *"$install_input"* ]]; then
    echo "Client dependency fingerprint omits install context: $install_input" >&2
    exit 1
  fi
done

for cache_guard in \
  'sed -n '\''1p'\'' "$STAMP"' \
  'sed -n '\''2p'\'' "$STAMP"' \
  'printf '\''%s\n%s\n'\'' "$WANT_HASH" "$TREE_HASH"'; do
  if [[ "$client_init_recipe" != *"$cache_guard"* ]]; then
    echo "Client dependency cache omits installed-tree guard: $cache_guard" >&2
    exit 1
  fi
done

if [[ "$client_init_recipe" != *'npm clean-install "${install_args[@]}"'* ]]; then
  echo "Client dependency config snapshot and install must use the same arguments" >&2
  exit 1
fi

/bin/bash -u -c 'registry_args=(); printf "%s" ${registry_args[@]+"${registry_args[@]}"}'

go_work_sync_recipe="$(just --dry-run --no-deps _go-work-sync 2>&1)"
plugin_build_recipe="$(just --dry-run --no-deps _build-go-plugins-cross linux arm64 server/plugins 2>&1)"
workspace_dependency_files=(go.work)
if [ -f go.work.sum ]; then
  workspace_dependency_files+=(go.work.sum)
fi
while IFS= read -r module_dir; do
  module_dir="${module_dir#./}"
  workspace_dependency_files+=("$module_dir/go.mod")
  if [ -f "$module_dir/go.sum" ]; then
    workspace_dependency_files+=("$module_dir/go.sum")
  fi
done < <(go work edit -json | jq -r '.Use[].DiskPath')

for dependency_file in "${workspace_dependency_files[@]}"; do
  if [[ "$go_work_sync_recipe" != *"$dependency_file"* ]]; then
    echo "Go workspace sync cache omits dependency: $dependency_file" >&2
    exit 1
  fi
  if [[ "$plugin_build_recipe" != *"$dependency_file"* ]]; then
    echo "Plugin build cache omits shared dependency: $dependency_file" >&2
    exit 1
  fi
done

if ! jq -e '.low_risk.deny_paths | index(".agents/**")' \
  .github/review-policy.json >/dev/null; then
  echo "Review policy must deny canonical agent skill changes from low-risk classification" >&2
  exit 1
fi

if ! diff -u \
  <(jq -r '."changed-checks".commands["developer-workflow-tests"].glob[]' <<<"$lefthook_config" | sort) \
  <(awk '
    /^developer_workflows:/ { capture = 1; next }
    capture && /^[^[:space:]]/ { exit }
    capture && /^  - / { sub(/^  - /, ""); gsub(/^"|"$/, ""); print }
  ' .github/path-filters.yml | sort); then
  echo "Local and CI developer workflow path routing must stay in sync" >&2
  exit 1
fi

if ! grep -q '^  developer-workflow-tests:' .github/workflows/pr-gate.yml \
  || ! grep -A16 '^  developer-workflow-tests:' .github/workflows/pr-gate.yml | grep -q 'just test-developer-workflows'; then
  echo "PR Gate must route canonical agent skill changes to developer workflow tests" >&2
  exit 1
fi

canonical_skill_names() {
  find "$1" -mindepth 1 -maxdepth 1 -type d -exec basename {} \; | sort
}

claude_skill_names() {
  local unexpected_entry
  unexpected_entry="$(
    find "$1" -mindepth 1 -maxdepth 1 \
      ! -type l ! -name '.DS_Store' -print -quit
  )"
  if [ -n "$unexpected_entry" ]; then
    echo "Claude skill entry is not a symlink: $unexpected_entry" >&2
    return 1
  fi
  find "$1" -mindepth 1 -maxdepth 1 -type l -exec basename {} \; | sort
}

parity_fixture="$test_tmp/skill-parity"
mkdir -p "$parity_fixture/.agents/skills/example" "$parity_fixture/.claude/skills"
ln -s ../../.agents/skills/example "$parity_fixture/.claude/skills/example"
touch "$parity_fixture/.claude/skills/.DS_Store"
diff -u \
  <(canonical_skill_names "$parity_fixture/.agents/skills") \
  <(claude_skill_names "$parity_fixture/.claude/skills")

mkdir "$parity_fixture/.claude/skills/copied-skill"
if claude_skill_names "$parity_fixture/.claude/skills" >/dev/null 2>&1; then
  echo "Claude skill parity accepts a non-symlink skill entry" >&2
  exit 1
fi
rmdir "$parity_fixture/.claude/skills/copied-skill"

diff -u \
  <(canonical_skill_names .agents/skills) \
  <(claude_skill_names .claude/skills)

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
