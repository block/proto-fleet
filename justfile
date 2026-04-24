set shell := ["bash", "-euo", "pipefail", "-c"]

default:
  just --list

# install all project dependencies
setup: _server-init _client-init _python-gen-init

# run protoFleet client and server
dev: build-plugins-docker
  ./dev.sh

# lint all project code (non-mutating)
lint: _lint-protos _lint-client _lint-server _lint-plugins

# format all project code (writes files)
format: _format-server _format-client _format-plugins

# run all non-mutating quality checks
check: lint

# run all code generation
gen: _server-init _client-init _lint-protos _gen-protos _gen-server _format-client _format-server

# --- Plugin builds ---

# build plugin binaries for local development
build-plugins: (_build-go-plugins-native "server/plugins") _asicrs-build

# build plugin binaries for Docker (Linux ARM64)
build-plugins-docker: (_build-go-plugins-cross "linux" "arm64" "server/plugins") _asicrs-build-docker

# build plugin binaries for multiple architectures (deployment)
build-plugins-release: _build-go-plugins-multi-arch _asicrs-build-release

# rebuild a specific plugin for the Docker runtime (linux/arm64): proto, antminer, virtual, or asicrs
rebuild-plugin name:
  #!/usr/bin/env bash
  set -euo pipefail
  case "{{name}}" in
    proto|antminer|virtual|asicrs) ;;
    *)
      echo "Unknown plugin: {{name}}. Valid: proto, antminer, virtual, asicrs" >&2
      exit 1
      ;;
  esac
  # Fleet loads every executable in PLUGINS_DIR, so ensure all siblings are
  # present and built for linux/arm64 before force-rebuilding the named one.
  just build-plugins-docker
  case "{{name}}" in
    proto|antminer)
      (cd plugin/{{name}} && GOOS=linux GOARCH=arm64 go build -o ../../server/plugins/{{name}}-plugin .)
      chmod +x server/plugins/{{name}}-plugin
      ;;
    virtual)
      (cd plugin/virtual && GOOS=linux GOARCH=arm64 go build -o ../../server/plugins/virtual-plugin .)
      cp plugin/virtual/config.json server/plugins/
      chmod +x server/plugins/virtual-plugin
      ;;
    asicrs)
      rm -f server/plugins/.asicrs-platform
      just _asicrs-build-docker
      ;;
  esac
  echo "Rebuilt {{name}} plugin."

# build virtual miner plugin for Docker (Linux ARM64) — alias for `just rebuild-plugin virtual`
build-virtual-plugin: (rebuild-plugin "virtual")

# --- Tests ---

# run plugin contract tests (each test suite in its own container for port isolation)
test-contract: _asicrs-build
  #!/usr/bin/env bash
  set -euo pipefail
  GO_VERSION=$(grep '^go ' tests/plugin-contract/go.mod | awk '{print $2}')
  IMAGE="golang:${GO_VERSION}-alpine"

  # Share the host Go build + module cache with every container so the
  # actions/cache restore on CI (and the local dev cache) isn't wasted.
  mkdir -p "${HOME}/.cache/go-build" "${HOME}/go/pkg/mod"
  DOCKER_RUN=(
    docker run --rm
    -v "$(pwd):/work"
    -v "${HOME}/.cache/go-build:/gocache"
    -v "${HOME}/go/pkg/mod:/gomodcache"
    --user "$(id -u):$(id -g)"
    -e GOCACHE=/gocache
    -e GOMODCACHE=/gomodcache
    -e GOFLAGS=-buildvcs=false
    -e HOME=/tmp
    -w /work
  )

  # Build Go plugins and compile the contract-test binary in a single container
  # so the three test runs below only need to execute it, not recompile it.
  mkdir -p tests/plugin-contract/bin
  "${DOCKER_RUN[@]}" "$IMAGE" sh -c '
    mkdir -p server/plugins && \
    (cd plugin/proto && go build -o ../../server/plugins/proto-plugin .) && \
    (cd plugin/antminer && go build -o ../../server/plugins/antminer-plugin .) && \
    (cd tests/plugin-contract && go test -c -o bin/miners.test ./miners/)
  '

  # Run each test suite in its own container (isolated network namespace)
  # so mocks binding port 4028/80 don't conflict between suites. Keep the
  # loop sequential: the asicrs harness rewrites a shared config file next
  # to the plugin binary, and parallel containers bind-mounting the same
  # /work would race on server/plugins/asicrs-config.yaml.
  FAILED=0
  for test in TestAntminerStock TestAntminerVNish TestWhatsMinerStock; do
    echo "=== Running ${test} ==="
    # cd into the miners package so the tests' relative testdata paths (../testdata/...) resolve.
    "${DOCKER_RUN[@]}" "$IMAGE" sh -c \
      "cd tests/plugin-contract/miners && ../bin/miners.test -test.v -test.timeout=2m -test.run '^${test}\$'" \
    || FAILED=1
  done

  if [ "$FAILED" -ne 0 ]; then
    echo "Some contract tests failed"
    exit 1
  fi

# run ProtoFleet E2E tests
test-e2e-fleet: (_e2e "protoFleet" "--project=desktop")

# run ProtoFleet E2E tests in UI mode
test-e2e-fleet-ui: (_e2e "protoFleet" "--ui" "--project=desktop")

# run ProtoFleet E2E tests headed
test-e2e-fleet-headed: (_e2e "protoFleet" "--headed" "--project=desktop")

# run ProtoFleet WIP E2E tests
test-e2e-fleet-wip: (_e2e "protoFleet" "--headed" "--grep" "@wip" "--project=desktop")

# run ProtoOS E2E tests
test-e2e-protoos: (_e2e "protoOS" "--project=desktop")

# run ProtoOS E2E tests in UI mode
test-e2e-protoos-ui: (_e2e "protoOS" "--ui" "--project=desktop")

# run ProtoOS E2E tests headed
test-e2e-protoos-headed: (_e2e "protoOS" "--headed" "--project=desktop")

# run ProtoOS WIP E2E tests
test-e2e-protoos-wip: (_e2e "protoOS" "--headed" "--grep" "@wip" "--project=desktop")

# --- Dependency management ---

# update all Go dependencies across workspace
update-go-deps:
  #!/usr/bin/env bash
  set -euo pipefail
  echo "Updating server dependencies..."
  (cd server && go get -u ./... && go mod tidy)
  echo "Updating plugin/proto dependencies..."
  (cd plugin/proto && go get -u ./... && go mod tidy)
  echo "Updating plugin/antminer dependencies..."
  (cd plugin/antminer && go get -u ./... && go mod tidy)
  echo "Updating plugin/virtual dependencies..."
  (cd plugin/virtual && go get -u ./... && go mod tidy)
  echo "Updating server/fake-proto-rig dependencies..."
  (cd server/fake-proto-rig && go get -u ./... && go mod tidy)
  echo "Syncing Go workspace..."
  go work sync
  mkdir -p .cache/go-work-sync && touch .cache/go-work-sync/stamp
  echo "All Go dependencies updated successfully"

# --- Packaging ---

# build Windows installer
[working-directory: 'deployment-files/windows']
build-windows-installer:
  powershell -NoProfile -ExecutionPolicy Bypass -File ./build-fleet-installer.ps1

# install git hooks via lefthook
install-hooks:
  #!/usr/bin/env bash
  set -euo pipefail
  if ! command -v lefthook >/dev/null 2>&1; then
    echo "lefthook is required to install git hooks." >&2
    echo "If you use Hermit, run ./bin/activate-hermit first." >&2
    echo "Otherwise install lefthook manually, then rerun 'just install-hooks'." >&2
    exit 1
  fi
  lefthook install

# --- Private helpers ---

[working-directory: 'server']
_server-init:
  go mod download

[working-directory: 'client']
_client-init:
  npm clean-install

[working-directory: 'packages/proto-python-gen']
_python-gen-init:
  just setup

_lint-protos:
  buf lint

[working-directory: 'client']
_lint-client:
  npm run lint

[working-directory: 'server']
_lint-server:
  golangci-lint run -c .golangci.yaml

_lint-plugins:
  #!/usr/bin/env bash
  set -euo pipefail
  (cd plugin/proto && golangci-lint run -c .golangci.yaml)
  (cd plugin/antminer && golangci-lint run -c .golangci.yaml)

[working-directory: 'server']
_format-server:
  goimports -w .

[working-directory: 'client']
_format-client:
  npm run format

_format-plugins:
  #!/usr/bin/env bash
  set -euo pipefail
  (cd plugin/proto && goimports -w .)
  (cd plugin/antminer && goimports -w .)

_gen-protos:
  PATH="$(pwd)/client/node_modules/.bin:$PATH" buf generate

[working-directory: 'server']
_gen-server:
    just gen

_e2e suite *args:
  #!/usr/bin/env bash
  set -euo pipefail
  cd "client/e2eTests/{{suite}}"
  npx playwright install
  npx playwright test {{args}}

# sync Go workspace only when go.work / go.work.sum has changed since last sync
_go-work-sync:
  #!/usr/bin/env bash
  set -euo pipefail
  STAMP=.cache/go-work-sync/stamp
  if [ -f "$STAMP" ] && ! [ go.work -nt "$STAMP" ] && { [ ! -f go.work.sum ] || ! [ go.work.sum -nt "$STAMP" ]; }; then
    exit 0
  fi
  echo "Syncing Go workspace..."
  go work sync
  mkdir -p "$(dirname "$STAMP")"
  # Ensure stamp mtime strictly exceeds any file `go work sync` just wrote (same-second race).
  sleep 1
  touch "$STAMP"

_build-go-plugins-native outdir: _go-work-sync
  #!/usr/bin/env bash
  set -euo pipefail
  # Plugins import from ../../server, so server module files also affect the graph.
  SOURCES="plugin/proto plugin/antminer server/sdk/v1 go.work go.work.sum server/go.mod server/go.sum plugin/proto/go.mod plugin/proto/go.sum plugin/antminer/go.mod plugin/antminer/go.sum"
  PROTO_BIN={{outdir}}/proto-plugin
  ANT_BIN={{outdir}}/antminer-plugin
  PLATFORM_MARKER={{outdir}}/.go-plugins-platform
  WANT_PLATFORM="native"
  if [ -f "$PROTO_BIN" ] && [ -f "$ANT_BIN" ] \
     && [ -f "$PLATFORM_MARKER" ] && [ "$(cat "$PLATFORM_MARKER")" = "$WANT_PLATFORM" ] \
     && [ -z "$(find $SOURCES -newer "$PROTO_BIN" -type f 2>/dev/null | head -1)" ] \
     && [ -z "$(find $SOURCES -newer "$ANT_BIN" -type f 2>/dev/null | head -1)" ]; then
    echo "Go plugins up to date, skipping build."
    exit 0
  fi
  echo "Building Go plugins..."
  mkdir -p {{outdir}}
  (cd plugin/proto && go build -o ../../{{outdir}}/proto-plugin .)
  (cd plugin/antminer && go build -o ../../{{outdir}}/antminer-plugin .)
  chmod +x {{outdir}}/proto-plugin {{outdir}}/antminer-plugin
  echo "$WANT_PLATFORM" > "$PLATFORM_MARKER"

_build-go-plugins-cross goos goarch outdir: _go-work-sync
  #!/usr/bin/env bash
  set -euo pipefail
  # Plugins import from ../../server, so server module files also affect the graph.
  SOURCES="plugin/proto plugin/antminer server/sdk/v1 go.work go.work.sum server/go.mod server/go.sum plugin/proto/go.mod plugin/proto/go.sum plugin/antminer/go.mod plugin/antminer/go.sum"
  PROTO_BIN={{outdir}}/proto-plugin
  ANT_BIN={{outdir}}/antminer-plugin
  PLATFORM_MARKER={{outdir}}/.go-plugins-platform
  WANT_PLATFORM="{{goos}}/{{goarch}}"
  if [ -f "$PROTO_BIN" ] && [ -f "$ANT_BIN" ] \
     && [ -f "$PLATFORM_MARKER" ] && [ "$(cat "$PLATFORM_MARKER")" = "$WANT_PLATFORM" ] \
     && [ -z "$(find $SOURCES -newer "$PROTO_BIN" -type f 2>/dev/null | head -1)" ] \
     && [ -z "$(find $SOURCES -newer "$ANT_BIN" -type f 2>/dev/null | head -1)" ]; then
    echo "Go plugins up to date for {{goos}}/{{goarch}}, skipping build."
    exit 0
  fi
  echo "Building Go plugins for {{goos}}/{{goarch}}..."
  mkdir -p {{outdir}}
  (cd plugin/proto && GOOS={{goos}} GOARCH={{goarch}} go build -o ../../{{outdir}}/proto-plugin .)
  (cd plugin/antminer && GOOS={{goos}} GOARCH={{goarch}} go build -o ../../{{outdir}}/antminer-plugin .)
  chmod +x {{outdir}}/proto-plugin {{outdir}}/antminer-plugin
  echo "$WANT_PLATFORM" > "$PLATFORM_MARKER"

_build-go-plugins-multi-arch: _go-work-sync
  #!/usr/bin/env bash
  set -euo pipefail
  echo "Building Go plugins for multiple architectures..."
  mkdir -p deployment-files/server
  (cd plugin/proto && GOOS=linux GOARCH=amd64 go build -o ../../deployment-files/server/proto-plugin-amd64 .)
  (cd plugin/antminer && GOOS=linux GOARCH=amd64 go build -o ../../deployment-files/server/antminer-plugin-amd64 .)
  (cd plugin/proto && GOOS=linux GOARCH=arm64 go build -o ../../deployment-files/server/proto-plugin-arm64 .)
  (cd plugin/antminer && GOOS=linux GOARCH=arm64 go build -o ../../deployment-files/server/antminer-plugin-arm64 .)
  chmod +x deployment-files/server/*-plugin-*

_asicrs-build:
  #!/usr/bin/env bash
  set -euo pipefail
  BIN=server/plugins/asicrs-plugin
  PLATFORM_MARKER=server/plugins/.asicrs-platform
  WANT_PLATFORM="native"
  if [ -f "$BIN" ] \
     && [ -f "$PLATFORM_MARKER" ] && [ "$(cat "$PLATFORM_MARKER")" = "$WANT_PLATFORM" ] \
     && [ -z "$(find plugin/asicrs sdk/rust server/sdk/v1/pb -newer "$BIN" -type f 2>/dev/null | head -1)" ]; then
    echo "asicrs plugin up to date, skipping build."
    exit 0
  fi
  echo "Building asicrs plugin..."
  mkdir -p server/plugins
  CACHE_ARGS=()
  if [ -n "${GITHUB_ACTIONS:-}" ]; then
    CACHE_ARGS+=(--cache-from 'type=gha,scope=asicrs-native')
    CACHE_ARGS+=(--cache-to 'type=gha,mode=max,scope=asicrs-native')
  fi
  docker buildx build \
    ${CACHE_ARGS[@]+"${CACHE_ARGS[@]}"} \
    --file plugin/asicrs/Dockerfile.build \
    --output type=local,dest=server/plugins \
    .
  chmod +x "$BIN"
  # buildx --output type=local preserves the in-image mtime; touch so freshness checks see "now".
  touch "$BIN"
  echo "$WANT_PLATFORM" > "$PLATFORM_MARKER"

_asicrs-build-docker:
  #!/usr/bin/env bash
  set -euo pipefail
  BIN=server/plugins/asicrs-plugin
  PLATFORM_MARKER=server/plugins/.asicrs-platform
  WANT_PLATFORM="linux/arm64"
  if [ -f "$BIN" ] \
     && [ -f "$PLATFORM_MARKER" ] && [ "$(cat "$PLATFORM_MARKER")" = "$WANT_PLATFORM" ] \
     && [ -z "$(find plugin/asicrs sdk/rust server/sdk/v1/pb -newer "$BIN" -type f 2>/dev/null | head -1)" ]; then
    echo "asicrs plugin up to date for Docker (Linux ARM64), skipping build."
    exit 0
  fi
  echo "Building asicrs plugin for Docker (Linux ARM64)..."
  mkdir -p server/plugins
  docker buildx build \
    --platform linux/arm64 \
    --file plugin/asicrs/Dockerfile.build \
    --output type=local,dest=server/plugins \
    .
  chmod +x "$BIN"
  # buildx --output type=local preserves the in-image mtime; touch so freshness checks see "now".
  touch "$BIN"
  echo "$WANT_PLATFORM" > "$PLATFORM_MARKER"

_asicrs-build-release:
  #!/usr/bin/env bash
  set -euo pipefail
  echo "Building asicrs plugin for multiple architectures..."
  mkdir -p deployment-files/server
  for arch in amd64 arm64; do
    docker buildx build \
      --platform "linux/${arch}" \
      --file plugin/asicrs/Dockerfile.build \
      --output "type=local,dest=/tmp/asicrs-${arch}" \
      .
    cp "/tmp/asicrs-${arch}/asicrs-plugin" "deployment-files/server/asicrs-plugin-${arch}"
    cp "/tmp/asicrs-${arch}/asicrs-config.yaml" "deployment-files/server/asicrs-config.yaml"
    rm -rf "/tmp/asicrs-${arch}"
  done
  chmod +x deployment-files/server/asicrs-plugin-*
