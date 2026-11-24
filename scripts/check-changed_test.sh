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

# The global list does not include all workflows. Thus each filter must list
# the workflow that the gate calls for it, and each composite action that the
# workflow uses. If not, a change to that file does not start the job.
ci_filter_paths() {
  awk -v filter="$1" '
    $0 == filter ":" { capture = 1; next }
    capture && /^[^[:space:]]/ { exit }
    capture && /^  - / { sub(/^  - /, ""); gsub(/^"|"$/, ""); print }
  ' .github/path-filters.yml
}

gated_workflows="$(awk '
  /^  [a-z0-9-]+:$/ { filter = ""; next }
  match($0, /needs\.changes\.outputs\.[a-z_]+ == /) { filter = substr($0, RSTART + 22, RLENGTH - 26) }
  filter != "" && /^    uses: \.\/\.github\/workflows\// { sub(/^    uses: \.\//, ""); print filter, $0 }
' .github/workflows/pr-gate.yml)"
if [ -z "$gated_workflows" ]; then
  echo "PR Gate jobs with a path filter could not be parsed" >&2
  exit 1
fi

while read -r filter workflow; do
  filter_paths="$(ci_filter_paths "$filter")"
  if ! grep -qxF "$workflow" <<<"$filter_paths"; then
    echo "CI filter '$filter' does not list its workflow: $workflow" >&2
    exit 1
  fi
  while IFS= read -r action; do
    if ! grep -qxF "$action/**" <<<"$filter_paths" \
      && ! printf '%s\n' "${ci_global_paths[@]}" | grep -qxF "$action/**"; then
      echo "CI filter '$filter' does not list an action that $workflow uses: $action" >&2
      exit 1
    fi
  done < <(grep -oE 'uses: \./\.github/actions/[a-z0-9-]+' "$workflow" | sed 's|^uses: \./||' | sort -u)
done <<<"$gated_workflows"

# The check above reads the workflows and the composite actions. Thus this test
# must run for each change to them. If not, a new `uses:` line passes the gate
# and the filter stays incorrect until a subsequent change to the filter file.
developer_workflow_paths="$(ci_filter_paths developer_workflows)"
for guard_input in '.github/workflows/**' '.github/actions/**'; do
  if ! grep -qxF "$guard_input" <<<"$developer_workflow_paths"; then
    echo "CI filter 'developer_workflows' does not list an input of this test: $guard_input" >&2
    exit 1
  fi
done

# Run the _client-init recipe in a fixture tree. Fake node and npm commands
# supply the host identity and record each install.
client_init_fixture="$test_tmp/client-init"
mkdir -p "$client_init_fixture/bin" "$client_init_fixture/tree/client"
cat > "$client_init_fixture/bin/node" <<'SH'
#!/usr/bin/env bash
case "$*" in
  --version) printf '%s\n' "$FAKE_NODE_VERSION" ;;
  '-p process.platform') printf '%s\n' "$FAKE_HOST_PLATFORM" ;;
  '-p process.arch') printf '%s\n' "$FAKE_HOST_ARCH" ;;
  -p\ *) printf '%s\n' "$FAKE_HOST_LIBC" ;;
  *) exit 1 ;;
esac
SH
cat > "$client_init_fixture/bin/npm" <<'SH'
#!/usr/bin/env bash
case "${1:-}" in
  --version) printf '%s\n' "$FAKE_NPM_VERSION" ;;
  config)
    [ "${2:-} ${3:-}" = 'list --json' ] || exit 1
    shift 3
    printf '%s\n' "$*" > "$CLIENT_INIT_FIXTURE/config-arguments"
    printf '{"arguments":"%s","config":"%s"}\n' "$*" "$FAKE_NPM_CONFIG"
    ;;
  clean-install)
    shift
    printf '%s\n' "$*" >> "$CLIENT_INIT_FIXTURE/installs"
    mkdir -p node_modules
    printf 'installed tree\n' > node_modules/.package-lock.json
    ;;
  *) exit 1 ;;
esac
SH
chmod +x "$client_init_fixture/bin/node" "$client_init_fixture/bin/npm"

(
  unset COREPACK_NPM_REGISTRY
  export CLIENT_INIT_FIXTURE="$client_init_fixture"
  export FAKE_NODE_VERSION=v1.0.0 FAKE_NPM_VERSION=1.0.0 FAKE_NPM_CONFIG=default
  export FAKE_HOST_PLATFORM=linux FAKE_HOST_ARCH=x64 FAKE_HOST_LIBC=glibc
  PATH="$client_init_fixture/bin:$PATH"
  client_init_tree="$client_init_fixture/tree"
  client_init_stamp="$client_init_tree/.cache/client-deps/install-inputs.hash"
  client_init_tree_lock="$client_init_tree/client/node_modules/.package-lock.json"
  printf '{}\n' > "$client_init_tree/client/package.json"
  printf '{}\n' > "$client_init_tree/client/package-lock.json"
  : > "$client_init_tree/client/.npmrc"
  : > "$client_init_fixture/installs"

  # Run the recipe. Fail unless the number of recorded installs is "$1".
  expect_client_installs() {
    just --justfile "$PWD/justfile" --working-directory "$client_init_tree" \
      _client-init >/dev/null
    if [ "$(wc -l < "$client_init_fixture/installs" | tr -d '[:space:]')" -ne "$1" ]; then
      echo "Client dependency cache: $2" >&2
      exit 1
    fi
    if [ "$(tail -n 1 "$client_init_fixture/installs")" != "$(cat "$client_init_fixture/config-arguments")" ]; then
      echo "Client dependency config snapshot and install must use the same arguments" >&2
      exit 1
    fi
    if [ "$(sed -n '2p' "$client_init_stamp")" != "$(git hash-object "$client_init_tree_lock")" ]; then
      echo "Client dependency stamp does not record the installed tree" >&2
      exit 1
    fi
  }

  installs=1
  expect_client_installs "$installs" "the first run did not install"
  if [ "$(cat "$client_init_fixture/installs")" != '--include=dev --include=optional --dry-run=false' ]; then
    echo "Client install must include dev and optional dependencies" >&2
    exit 1
  fi
  expect_client_installs "$installs" "a run with no change installed again"

  for install_input in \
    client/package.json client/package-lock.json client/.npmrc \
    FAKE_NODE_VERSION FAKE_NPM_VERSION \
    FAKE_HOST_PLATFORM FAKE_HOST_ARCH FAKE_HOST_LIBC \
    FAKE_NPM_CONFIG COREPACK_NPM_REGISTRY; do
    previous_hash="$(sed -n '1p' "$client_init_stamp")"
    case "$install_input" in
      client/*) printf 'changed\n' >> "$client_init_tree/$install_input" ;;
      *) export "$install_input=changed" ;;
    esac
    installs=$((installs + 1))
    expect_client_installs "$installs" "a change of $install_input did not install"
    if [ "$(sed -n '1p' "$client_init_stamp")" = "$previous_hash" ]; then
      echo "Client dependency fingerprint omits: $install_input" >&2
      exit 1
    fi
  done
  if [ "$(tail -n 1 "$client_init_fixture/installs")" != '--include=dev --include=optional --dry-run=false --registry changed' ]; then
    echo "Client install must use the configured registry" >&2
    exit 1
  fi

  printf 'changed\n' >> "$client_init_tree_lock"
  installs=$((installs + 1))
  expect_client_installs "$installs" "a changed installed tree did not install"
  rm -rf "$client_init_tree/client/node_modules"
  installs=$((installs + 1))
  expect_client_installs "$installs" "a missing installed tree did not install"
)

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

# The fleetnode UI-test recipes use the Compose project of `just dev`. If the
# overlay changes the render of a shared database or network, Compose recreates
# them and restarts the dev stack. The render does not need a Docker daemon.
dev_compose=(docker compose -f server/docker-compose.yaml)
ui_test_compose=("${dev_compose[@]}" -f server/docker-compose.fleetnode-ui-test.yaml)
if [ "$("${dev_compose[@]}" config --hash timescaledb)" != "$("${ui_test_compose[@]}" config --hash timescaledb)" ]; then
  echo "The fleetnode UI-test overlay must not change the timescaledb service of the dev stack" >&2
  exit 1
fi
if ! diff -u \
  <("${dev_compose[@]}" config --format json | jq '.networks."fleet-network"') \
  <("${ui_test_compose[@]}" config --format json | jq '.networks."fleet-network"'); then
  echo "The fleetnode UI-test overlay must not change fleet-network of the dev stack" >&2
  exit 1
fi
if [ "$("${ui_test_compose[@]}" config --format json \
  | jq -r '.services."fleet-api".ports | map("\(.host_ip):\(.published):\(.target)") | join(" ")')" \
  != '127.0.0.1:4000:4000' ]; then
  echo "The fleetnode UI-test overlay must publish fleet-api on 127.0.0.1:4000" >&2
  exit 1
fi

echo "developer workflow configuration and agent skill parity passed"
