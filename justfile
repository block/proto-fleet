default: 
  just --list

init: _server-init _client-init

[working-directory: 'server']
_server-init:
  go mod download

[working-directory: 'client']
_client-init:
  npm install

lint: 
  buf lint

gen: lint gen-protos gen-server fmt-client fmt-server

gen-protos: 
  PATH="$PATH:$(pwd)/client/node_modules/@bufbuild/protoc-gen-es/bin" buf generate

gen-server:
    cd server; just gen

[working-directory: 'server']
fmt-client:
  goimports -w generated/grpc

[working-directory: 'client']
fmt-server:
  npm run format
