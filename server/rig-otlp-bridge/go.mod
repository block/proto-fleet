module github.com/block/proto-fleet/server/rig-otlp-bridge

go 1.25.4

require (
	connectrpc.com/connect v1.20.0
	github.com/block/proto-fleet/server/generated/grpc v0.0.0-00010101000000-000000000000
	go.opentelemetry.io/proto/otlp v1.10.0
	google.golang.org/grpc v1.81.1
	google.golang.org/protobuf v1.36.11
)

require (
	buf.build/gen/go/bufbuild/protovalidate/protocolbuffers/go v1.36.11-20260415201107-50325440f8f2.1 // indirect
	github.com/grpc-ecosystem/grpc-gateway/v2 v2.28.0 // indirect
	golang.org/x/net v0.56.0 // indirect
	golang.org/x/sys v0.46.0 // indirect
	golang.org/x/text v0.38.0 // indirect
	google.golang.org/genproto/googleapis/api v0.0.0-20260226221140-a57be14db171 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260226221140-a57be14db171 // indirect
)

// Fleet API stubs come from the server's shared generated module.
replace github.com/block/proto-fleet/server/generated/grpc => ../generated/grpc
