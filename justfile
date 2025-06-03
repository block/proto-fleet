default: 
  just --list

init: _server-init _client-init

# Run protoFleet client and server
dev:
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

gen-server:
    cd server; just gen

[working-directory: 'server']
fmt-client:
  goimports -w generated/grpc

[working-directory: 'client']
fmt-server:
  npm run format

[working-directory: 'server']
clean-build: 
  docker-compose down --rmi all --volumes && docker-compose up --build -d
