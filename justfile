default: 
  just --list

init: _server-init _client-init

# Build plugin binaries for local development
build-plugins:
  #!/usr/bin/env bash
  set -euo pipefail
  echo "Syncing Go workspace..."
  go work sync
  echo "Building plugins..."
  mkdir -p server/plugins
  (cd plugin/proto && go build -o ../../server/plugins/proto-plugin .)
  (cd plugin/antminer && go build -o ../../server/plugins/antminer-plugin .)
  chmod +x server/plugins/proto-plugin server/plugins/antminer-plugin
  echo "Plugins built successfully"

# Build plugin binaries for Docker (Linux ARM64)
build-plugins-docker:
  #!/usr/bin/env bash
  set -euo pipefail
  echo "Syncing Go workspace..."
  go work sync
  echo "Building plugins for Docker (Linux ARM64)..."
  mkdir -p server/plugins
  (cd plugin/proto && GOOS=linux GOARCH=arm64 go build -o ../../server/plugins/proto-plugin .)
  (cd plugin/antminer && GOOS=linux GOARCH=arm64 go build -o ../../server/plugins/antminer-plugin .)
  chmod +x server/plugins/proto-plugin server/plugins/antminer-plugin
  echo "Docker plugins built successfully"

# Build plugin binaries for multiple architectures (deployment)
build-plugins-multi-arch:
  #!/usr/bin/env bash
  set -euo pipefail
  echo "Syncing Go workspace..."
  go work sync
  echo "Building plugins for multiple architectures..."
  mkdir -p deployment-files/server
  (cd plugin/proto && GOOS=linux GOARCH=amd64 go build -o ../../deployment-files/server/proto-plugin-amd64 .)
  (cd plugin/antminer && GOOS=linux GOARCH=amd64 go build -o ../../deployment-files/server/antminer-plugin-amd64 .)
  (cd plugin/proto && GOOS=linux GOARCH=arm64 go build -o ../../deployment-files/server/proto-plugin-arm64 .)
  (cd plugin/antminer && GOOS=linux GOARCH=arm64 go build -o ../../deployment-files/server/antminer-plugin-arm64 .)
  chmod +x deployment-files/server/*-plugin-*
  echo "Multi-arch plugins built successfully"

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

[working-directory: 'minefield']
gen-minefield:
    just gen

[working-directory: 'minefield']
minefield-build:
    just build

[working-directory: 'server']
fmt-server:
  goimports -w generated/grpc

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

[working-directory: 'client/e2eTests']
install-playwright:
  npx playwright install

[working-directory: 'client/e2eTests']
test-e2e: install-playwright
  npx playwright test

[working-directory: 'client/e2eTests']
test-e2e-ui: install-playwright
  npx playwright test --ui

[working-directory: 'client/e2eTests']
test-e2e-headed: install-playwright
  npx playwright test --headed

clean-submodules:
  git submodule update --init --force --recursive --checkout && git submodule foreach --recursive "git reset --hard && git clean -ffdx" && mkdir -p miner-firmware/docker/sim/protoOS/dist/protoOS
