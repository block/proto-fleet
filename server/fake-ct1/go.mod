module github.com/block/proto-fleet/server/fake-ct1

go 1.26.0

require github.com/google/uuid v1.6.0

require (
	connectrpc.com/connect v1.21.1
	github.com/block/proto-fleet/sdk/mdk v0.0.0
	google.golang.org/genproto/googleapis/api v0.0.0-20261110000943-09e21b237f2c
	google.golang.org/genproto/googleapis/rpc v0.0.0-20261104224025-d1e00e9f33ab
	google.golang.org/grpc v1.85.0
	google.golang.org/protobuf v1.36.12
)

require (
	github.com/grpc-ecosystem/grpc-gateway/v2 v2.34.0 // indirect
	github.com/vmihailenco/msgpack/v5 v5.4.1 // indirect
	github.com/vmihailenco/tagparser/v2 v2.0.0 // indirect
	go.yaml.in/yaml/v3 v3.0.6 // indirect
	golang.org/x/net v0.63.0 // indirect
	golang.org/x/sys v0.50.0 // indirect
	golang.org/x/text v0.44.0 // indirect
)

replace github.com/block/proto-fleet/sdk/mdk => ../../sdk/mdk
