default: 
  just --list

init: _server-init _client-init

[working-directory: 'server']
_server-init:
  go mod download

[working-directory: 'client']
_client-init:
  npm install -g @bufbuild/buf @bufbuild/protoc-gen-es
  npm install

lint: 
  buf lint

gen: lint gen-protos fmt-client fmt-server

gen-protos: 
  buf generate

[working-directory: 'server']
fmt-client:
  goimports -w generated/grpc

[working-directory: 'client']
fmt-server:
  npm run format
