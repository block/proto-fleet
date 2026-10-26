# Frigate MDK v2 contracts

Shared MDK v2 contracts for the CT1 plugin, `server/fake-ct1`, and the ProtoOS
CT1 adapter. Generation produces native gRPC and Connect-Go bindings under
`generated/`, and protobuf-es bindings under
`client/src/protoOS/api/adapters/mdkV2/generated/proto` for the browser Connect
client. These are separate from Fleet's public/browser APIs.

`proto/mdk/v2` contains the complete, unmodified `proto/mdk/v2` sources from
[`btc-mining/frigate` at `4f3fe6ee8a991c6e136e5867b75a939638c8793c`](https://github.com/btc-mining/frigate/tree/4f3fe6ee8a991c6e136e5867b75a939638c8793c/proto/mdk/v2).
No repository-root license file was present at this pin;
these remain internal Frigate sources, not independently licensed public code.
External annotation dependencies are pinned in `buf.lock` and retain their
upstream licensing through Buf's Google APIs and grpc-gateway modules.

Keep this pin and the MDK v2 OpenAPI snapshot in `proto-rig-api/` on the same
Frigate commit. The ProtoOS Connect client encodes with these protos but types
its results with the snapshot's REST types; `proto-rig-api/VERSION.md` records
any skew between the two.

Run `just gen-mdk-protos` from the repository root after updating sources.
It uses the Hermit-managed Buf and Go generators plus the client's
`protoc-gen-es`; managed mode overrides only Go package paths. Do not edit
generated files. Commit source and generated output together. Generated
historical RPC definitions do not enable historical fetching.
