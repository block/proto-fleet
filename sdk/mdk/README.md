# Frigate MDK v2 contracts

Shared MDK v2 contracts for the CT1 plugin, `server/fake-ct1`, and the ProtoOS
CT1 adapter. Generation produces native gRPC and Connect-Go bindings under
`generated/`, and protobuf-es bindings under
`client/src/protoOS/api/adapters/mdkV2/generated/proto` for the browser Connect
client. These are separate from Fleet's public/browser APIs.

`proto/mdk/v2` contains the complete, unmodified `proto/mdk/v2` sources from
[`btc-mining/frigate` at `6acbeba0dcde568d33ca7121aee15a3b567abb4f`](https://github.com/btc-mining/frigate/tree/6acbeba0dcde568d33ca7121aee15a3b567abb4f/proto/mdk/v2).
No repository-root license file was present at this pin;
these remain internal Frigate sources, not independently licensed public code.
External annotation dependencies are pinned in `buf.lock` and retain their
upstream licensing through Buf's Google APIs and grpc-gateway modules.

Keep this pin and the MDK v2 OpenAPI snapshot in `proto-rig-api/` on the same
Frigate commit. The ProtoOS Connect client encodes with these protos but types
its results with the snapshot's REST types; `proto-rig-api/VERSION.md` records
any skew between the two.

Run `just gen-mdk-protos` from the repository root after updating sources.
It installs client dependencies when they are missing or stale, then runs the
client's Buf CLI and `protoc-gen-es` from `client/node_modules/.bin` with the
Hermit-managed Go generators; managed mode overrides only Go package paths. Do
not edit generated files. Commit source and generated output together.
Generated historical RPC definitions do not enable historical fetching.

Fleet's mining group observation contains every configured pool's URL, worker,
priority, enabled flag, and weight. Passwords are write-only in MDK and are not
inferred from readback. Fleet retains an encrypted write receipt to associate
its immutable configuration revision with a group. A password-only change made
outside Fleet cannot be detected by this observation contract.
