module github.com/block/proto-fleet/server/fake-ct1

go 1.26.0

require github.com/google/uuid v1.6.0

require (
	connectrpc.com/connect v1.21.1
	github.com/block/proto-fleet/sdk/mdk v0.0.0
	google.golang.org/grpc v1.84.0
	google.golang.org/protobuf v1.36.12
)

require (
	github.com/grpc-ecosystem/grpc-gateway/v2 v2.30.0 // indirect
	golang.org/x/net v0.59.0 // indirect
	golang.org/x/sys v0.48.0 // indirect
	golang.org/x/text v0.42.0 // indirect
	google.golang.org/genproto/googleapis/api v0.0.0-20260928230214-8a89bd6388cc // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260928230214-8a89bd6388cc // indirect
)

replace github.com/block/proto-fleet/sdk/mdk => ../../sdk/mdk
