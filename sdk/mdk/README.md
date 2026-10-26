# Frigate MDK v2 Go contracts

Shared native gRPC contracts for the CT1 plugin and `server/fake-ct1`.
These are separate from Fleet's public/browser APIs.

`proto/mdk/v2` contains unmodified sources from
[`btc-mining/frigate` at `e069860da77f91857d664d26bbadd6672e0463ba`](https://github.com/btc-mining/frigate/tree/e069860da77f91857d664d26bbadd6672e0463ba/proto/mdk/v2),
the upstream `main` revision selected at migration implementation start.
The files include the import closure of System, Hashboard, Mining, Power, and
Telemetry services. No repository-root license file was present at this pin;
these remain internal Frigate sources, not independently licensed public code.
External annotation dependencies are pinned in `buf.lock` and retain their
upstream licensing through Buf's Google APIs and grpc-gateway modules.

`mining_group.proto` and its `pool.proto` import are pinned separately to
[`4677049b24e9428ebf01b17323682e2a16d332d7`](https://github.com/btc-mining/frigate/tree/4677049b24e9428ebf01b17323682e2a16d332d7/proto/mdk/v2)
for the physical-hashboard mining groups v1 contract. The remaining files retain
the pin above.

Run `just gen-mdk-protos` from the repository root after updating sources.
The repository's existing Hermit-managed Buf/Go generators override only Go
package paths; do not edit generated files. Commit source and generated output
together. Generated historical RPC definitions do not enable historical fetching.

Fleet's mining group observation contains every configured pool's URL, worker,
priority, enabled flag, and weight. Passwords are write-only in MDK and are not
inferred from readback. Fleet retains an encrypted write receipt to associate
its immutable configuration revision with a group. A password-only change made
outside Fleet cannot be detected by this observation contract.
