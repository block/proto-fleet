default:
  just --list

init: _server-init _client-init _python-gen-init

# Set up proto-python-gen venv so protoc-gen-python-grpc is available for code generation
[working-directory: 'packages/proto-python-gen']
_python-gen-init:
  just setup

# Build plugin binaries for local development
build-plugins:
  #!/usr/bin/env bash
  set -euo pipefail
  echo "Syncing Go workspace..."
  go work sync
  echo "Building Go plugins..."
  mkdir -p server/plugins
  (cd plugin/proto && go build -o ../../server/plugins/proto-plugin .)
  (cd plugin/antminer && go build -o ../../server/plugins/antminer-plugin .)
  chmod +x server/plugins/proto-plugin server/plugins/antminer-plugin
  echo "Building pyasic plugin..."
  just build-pyasic-plugin
  echo "Plugins built successfully"

# Build plugin binaries for Docker (Linux ARM64)
build-plugins-docker:
  #!/usr/bin/env bash
  set -euo pipefail
  echo "Syncing Go workspace..."
  go work sync
  echo "Building Go plugins for Docker (Linux ARM64)..."
  mkdir -p server/plugins
  (cd plugin/proto && GOOS=linux GOARCH=arm64 go build -o ../../server/plugins/proto-plugin .)
  (cd plugin/antminer && GOOS=linux GOARCH=arm64 go build -o ../../server/plugins/antminer-plugin .)
  chmod +x server/plugins/proto-plugin server/plugins/antminer-plugin
  echo "Building pyasic plugin for Docker (Linux ARM64)..."
  just build-pyasic-plugin-docker
  echo "Docker plugins built successfully"

# Build plugin binaries for multiple architectures (deployment)
build-plugins-multi-arch:
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
  echo "Building pyasic plugin for multiple architectures..."
  just build-pyasic-plugin-multi-arch
  echo "Multi-arch plugins built successfully"

# Build virtual miner plugin for Docker (Linux ARM64)
build-virtual-plugin:
  #!/usr/bin/env bash
  set -euo pipefail
  echo "Building virtual miner plugin for Docker..."
  mkdir -p server/plugins
  (cd plugin/virtual && GOOS=linux GOARCH=arm64 go build -o ../../server/plugins/virtual-plugin .)
  cp plugin/virtual/config.json server/plugins/
  chmod +x server/plugins/virtual-plugin
  echo "Virtual plugin built successfully"

# Build pyasic plugin via Docker for ARM64 (used by build-plugins-docker)
build-pyasic-plugin-docker:
  #!/usr/bin/env bash
  set -euo pipefail
  mkdir -p server/plugins
  docker buildx build \
    --platform linux/arm64 \
    --file plugin/pyasic/Dockerfile.build \
    --output type=local,dest=server/plugins \
    .
  chmod +x server/plugins/pyasic-plugin
  cp plugin/pyasic/config.yaml server/plugins/pyasic-config.yaml

# Build pyasic plugin via Docker (produces Linux binary for current arch)
build-pyasic-plugin:
  #!/usr/bin/env bash
  set -euo pipefail
  mkdir -p server/plugins
  docker buildx build \
    --file plugin/pyasic/Dockerfile.build \
    --output type=local,dest=server/plugins \
    .
  chmod +x server/plugins/pyasic-plugin
  cp plugin/pyasic/config.yaml server/plugins/pyasic-config.yaml

# Build pyasic plugin for multiple architectures (deployment)
build-pyasic-plugin-multi-arch:
  #!/usr/bin/env bash
  set -euo pipefail
  mkdir -p deployment-files/server
  for arch in amd64 arm64; do
    docker buildx build \
      --platform "linux/${arch}" \
      --file plugin/pyasic/Dockerfile.build \
      --output "type=local,dest=/tmp/pyasic-${arch}" \
      .
    cp "/tmp/pyasic-${arch}/pyasic-plugin" "deployment-files/server/pyasic-plugin-${arch}"
    rm -rf "/tmp/pyasic-${arch}"
  done
  cp plugin/pyasic/config.yaml deployment-files/server/pyasic-config.yaml
  chmod +x deployment-files/server/pyasic-plugin-*

# Run plugin contract tests
contract-tests: build-pyasic-plugin
  #!/usr/bin/env bash
  set -euo pipefail
  GO_VERSION=$(grep '^go ' tests/plugin-contract/go.mod | awk '{print $2}')
  docker run --rm \
    -v "$(pwd)":/work \
    -w /work \
    -e GOFLAGS=-buildvcs=false \
    "golang:${GO_VERSION}-alpine" \
    sh -c '\
      mkdir -p server/plugins && \
      (cd plugin/proto && go build -o ../../server/plugins/proto-plugin .) && \
      (cd plugin/antminer && go build -o ../../server/plugins/antminer-plugin .) && \
      go test -v -count=1 -timeout=5m ./tests/plugin-contract/... \
    '

# Update all Go dependencies across workspace
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

# Run protoFleet client and server
dev: build-plugins
  ./dev.sh

[working-directory: 'server']
_server-init:
  go mod download

[working-directory: 'client']
_client-init:
  npm clean-install

lint: 
  buf lint

gen: _server-init _client-init lint gen-protos gen-server fmt-client fmt-server

gen-protos: 
  PATH="$(pwd)/client/node_modules/.bin:$PATH" buf generate

[working-directory: 'server']
gen-server:
    just gen


[working-directory: 'server']
fmt-server:
  goimports -w generated/grpc

[working-directory: 'server']
seed-telemetry *args:
  just seed-telemetry {{args}}

[working-directory: 'client']
fmt-client:
  npm run format

clean-build: build-plugins-docker
  #!/usr/bin/env bash
  set -euo pipefail
  cd server
  # Generate a random JWT secret (44 characters to match original length)
  export AUTH_CLIENT_SECRET_KEY=$(openssl rand -hex 22)
  echo "AUTH_CLIENT_SECRET_KEY=${AUTH_CLIENT_SECRET_KEY}" > .env
  echo "Generated new JWT secret for clean build"
  docker-compose down --rmi all --volumes && docker-compose up --build -d

[working-directory: 'server']
rebuild-fleet-api:
  docker compose up fleet-api -d --build --force-recreate

[working-directory: 'client/e2eTests/protoFleet']
test-fleet-setup:
  npx playwright install

[working-directory: 'client/e2eTests/protoFleet']
test-fleet: test-fleet-setup
  npx playwright test --project=desktop

[working-directory: 'client/e2eTests/protoFleet']
test-fleet-ui: test-fleet-setup
  npx playwright test --ui --project=desktop

[working-directory: 'client/e2eTests/protoFleet']
test-fleet-headed: test-fleet-setup
  npx playwright test --headed --project=desktop

[working-directory: 'client/e2eTests/protoFleet']
test-fleet-wip: test-fleet-setup
  npx playwright test --headed --grep @wip --project=desktop
  
[working-directory: 'client/e2eTests/protoOS']
test-proto-os-setup:
  npx playwright install

[working-directory: 'client/e2eTests/protoOS']
test-proto-os: test-proto-os-setup
  npx playwright test --project=desktop

[working-directory: 'client/e2eTests/protoOS']
test-proto-os-ui: test-proto-os-setup
  npx playwright test --ui --project=desktop

[working-directory: 'client/e2eTests/protoOS']
test-proto-os-headed: test-proto-os-setup
  npx playwright test --headed --project=desktop

[working-directory: 'client/e2eTests/protoOS']
test-proto-os-wip: test-proto-os-setup
  npx playwright test --headed --grep @wip --project=desktop
  
[working-directory: 'deployment-files/windows']
build-windows-installer:
  powershell -NoProfile -ExecutionPolicy Bypass -File ./build-fleet-installer.ps1
