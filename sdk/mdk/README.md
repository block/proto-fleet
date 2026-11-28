# Frigate MDK v2 contracts

Shared MDK v2 contracts for the CT1 plugin, `server/fake-ct1`, and the ProtoOS
CT1 adapter. Generation produces native gRPC and Connect-Go bindings under
`generated/`, and protobuf-es bindings under
`client/src/protoOS/api/adapters/mdkV2/generated/proto` for the browser Connect
client. These are separate from Fleet's public/browser APIs.

`proto/mdk/v2` contains the complete `proto/mdk/v2` sources from
Frigate at `41756dc6e26ce0dccc594d7794f1ca3a107d4192`, with one change:
comments and descriptions say `Proto container firmware` where the source says
`Frigate`. Identifiers, field numbers, and wire names are the same as in the
source.
No repository-root license file was present at this pin;
these remain internal Frigate sources, not independently licensed public code.
External annotation dependencies are pinned in `buf.lock` and retain their
upstream licensing through Buf's Google APIs and grpc-gateway modules.

Keep this pin and the MDK v2 OpenAPI snapshot in `proto-rig-api/` on the same
Frigate commit. The ProtoOS Connect client encodes with these protos but types
its results with the snapshot's REST types; `proto-rig-api/VERSION.md` records
any skew between the two.

After you copy new sources or a new snapshot, apply the same change to both
before you regenerate:

```sh
perl -pi -e 's/(?:\b|(?<=\\n))Frigate\b/Proto container firmware/g' \
  sdk/mdk/proto/mdk/v2/*.proto proto-rig-api/openapi/MDK-v2-API.json
```

To compare the vendored files with their sources, apply the same command to a
copy of the source files.

Run `just gen-mdk-protos` from the repository root after updating sources.
It installs client dependencies when they are missing or stale, then runs the
client's Buf CLI and `protoc-gen-es` from `client/node_modules/.bin` with the
Hermit-managed Go generators; managed mode overrides only Go package paths. Do
not edit generated files. Commit source and generated output together.
Generated historical RPC definitions do not enable historical fetching.

`SystemService.GetHAStatus` provides durable cluster identity, configured members,
and independently aged HA observations. The plugin uses confirmed cluster IDs
for new endpoint groups and live `GetSystemStatus` for command selection.

Fleet's mining group observation contains every configured pool's URL, worker,
priority, enabled flag, and weight. Passwords are write-only in MDK and are not
inferred from readback. Fleet retains an encrypted write receipt to associate
its immutable configuration revision with a group. A password-only change made
outside Fleet cannot be detected by this observation contract.
