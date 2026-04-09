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

# build virtual miner plugin for Docker (Linux ARM64)
build-virtual-plugin:
  #!/usr/bin/env bash
  set -euo pipefail
  echo "Building virtual miner plugin for Docker..."
  mkdir -p server/plugins
  (cd plugin/virtual && GOOS=linux GOARCH=arm64 go build -o ../../server/plugins/virtual-plugin .)
  cp plugin/virtual/config.json server/plugins/
  chmod +x server/plugins/virtual-plugin
  echo "Virtual plugin built successfully"

# --- Tests ---

# run plugin contract tests (each test suite in its own container for port isolation)
test-contract: _asicrs-build
  #!/usr/bin/env bash
  set -euo pipefail
  GO_VERSION=$(grep '^go ' tests/plugin-contract/go.mod | awk '{print $2}')
  IMAGE="golang:${GO_VERSION}-alpine"

  # Build Go plugins once (shared via volume mount)
  docker run --rm \
    -v "$(pwd)":/work \
    -w /work \
    -e GOFLAGS=-buildvcs=false \
    "$IMAGE" \
    sh -c '\
      mkdir -p server/plugins && \
      (cd plugin/proto && go build -o ../../server/plugins/proto-plugin .) && \
      (cd plugin/antminer && go build -o ../../server/plugins/antminer-plugin .) \
    '

  # Run each test suite in its own container (isolated network namespace)
  # so mocks binding port 4028/80 don't conflict between suites.
  FAILED=0
  for test in TestAntminerStock TestAntminerVNish TestWhatsMinerStock; do
    echo "=== Running ${test} ==="
    docker run --rm \
      -v "$(pwd)":/work \
      -w /work \
      -e GOFLAGS=-buildvcs=false \
      "$IMAGE" \
      go test -v -count=1 -timeout=2m -run "^${test}$" ./tests/plugin-contract/miners/ \
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

_build-go-plugins-native outdir:
  #!/usr/bin/env bash
  set -euo pipefail
  echo "Syncing Go workspace..."
  go work sync
  echo "Building Go plugins..."
  mkdir -p {{outdir}}
  (cd plugin/proto && go build -o ../../{{outdir}}/proto-plugin .)
  (cd plugin/antminer && go build -o ../../{{outdir}}/antminer-plugin .)
  chmod +x {{outdir}}/proto-plugin {{outdir}}/antminer-plugin

_build-go-plugins-cross goos goarch outdir:
  #!/usr/bin/env bash
  set -euo pipefail
  echo "Syncing Go workspace..."
  go work sync
  echo "Building Go plugins for {{goos}}/{{goarch}}..."
  mkdir -p {{outdir}}
  (cd plugin/proto && GOOS={{goos}} GOARCH={{goarch}} go build -o ../../{{outdir}}/proto-plugin .)
  (cd plugin/antminer && GOOS={{goos}} GOARCH={{goarch}} go build -o ../../{{outdir}}/antminer-plugin .)
  chmod +x {{outdir}}/proto-plugin {{outdir}}/antminer-plugin

_build-go-plugins-multi-arch:
  #!/usr/bin/env bash
  set -euo pipefail
  echo "Syncing Go workspace..."
  go work sync
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
  echo "Building asicrs plugin..."
  mkdir -p server/plugins
  docker buildx build \
    --file plugin/asicrs/Dockerfile.build \
    --output type=local,dest=server/plugins \
    .
  chmod +x server/plugins/asicrs-plugin

_asicrs-build-docker:
  #!/usr/bin/env bash
  set -euo pipefail
  echo "Building asicrs plugin for Docker (Linux ARM64)..."
  mkdir -p server/plugins
  docker buildx build \
    --platform linux/arm64 \
    --file plugin/asicrs/Dockerfile.build \
    --output type=local,dest=server/plugins \
    .
  chmod +x server/plugins/asicrs-plugin

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
