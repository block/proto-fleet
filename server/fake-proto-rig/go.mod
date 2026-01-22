module github.com/btc-mining/proto-fleet/server/fake-proto-rig

go 1.25.4

require (
	connectrpc.com/connect v1.19.1
	github.com/btc-mining/proto-fleet/server v0.0.0
	github.com/google/uuid v1.6.0
	golang.org/x/net v0.48.0
)

require (
	golang.org/x/text v0.32.0 // indirect
	google.golang.org/protobuf v1.36.11 // indirect
)

replace github.com/btc-mining/proto-fleet/server => ../
