default: 
  just --list

lint: 
  buf lint

gen: lint gen-protos fmt-web fmt-go

gen-protos: 
  buf generate

[working-directory: 'server']
fmt-web:
  goimports -w generated/grpc

[working-directory: 'client']
fmt-go:
  npm run format
