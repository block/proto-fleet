# CT1 native gRPC transport

The Proto plugin uses Frigate MDK v2 gRPC for CT1 discovery, pairing,
current telemetry, and the existing boolean equipment controls. ProtoOS and
Fleet's web proxy continue to use HTTP(S). Poll frequency is unchanged. Concurrent polling, physical-equipment rollups,
and topology persistence are described in [polling measurements](ct1-polling-metrics.md).
Retained historical ingestion remains #179.

## Firmware contract

The shared [MDK module](../../sdk/mdk/README.md) pins Frigate commit
`6acbeba0dcde568d33ca7121aee15a3b567abb4f`. This is a source compatibility
target, not a qualified hardware deployment. Both the plugin and fake-ct1 use
the same generated types.

At that revision, `internal/config/config.go` defaults `server.mdk_listen` to
`:9095` and `server.api_listen` to `:8080`. `pkg/api/server.go` registers the
public MDK v2 services, including System, Hashboard, Mining, MiningGroup,
Telemetry, Power, Pool, and RunIn, on a plaintext gRPC server. There is no
registered AuthService/login RPC. The optional
`server.mdk_auth.enabled` / `server.mdk_auth.token_file` configuration gates
native RPCs with a shared bearer credential (`pkg/api/mdkauth/auth.go`).
It is disabled by default. Audit actor metadata does not authenticate callers.

## Endpoints and credentials

SDK `DeviceInfo` and FleetNode descriptors carry `RPCPort`/`RPCTLS` independently
of the existing web `Port`/`URLScheme`, on the same host. These values persist
in `discovered_device.rpc_port` / `rpc_tls`. Zero means the CT1 driver's default
9095; false selects plaintext. Verified TLS uses the host as server name,
system trust roots, and TLS 1.2 or newer, with no insecure verification bypass
or plaintext downgrade. It requires a deployment that exposes a TLS endpoint;
the pinned Frigate server itself is plaintext.

Discovery adds 9095 to the existing R2 ports. An explicitly requested scan port
can discover a custom gRPC listener. New CT1s get a web endpoint of
`http://<host>:8080`, with these plugin-process overrides:

| Variable | Meaning |
| --- | --- |
| `CT1_WEB_PORT` | Web port for newly discovered CT1s |
| `CT1_WEB_SCHEME` | `http` or `https` for newly discovered CT1s |
| `CT1_RPC_TLS` | Explicit boolean for gRPC discovery; stored with the device |

Set these on the process that runs the Proto plugin: the server for direct
controllers, or the managing FleetNode.

Authenticated native MDK connections are not supported. The plugin never sends
bearer tokens or saved REST username/password credentials as RPC metadata,
including when probing unknown discovery candidates. A nonempty
`CT1_RPC_TOKEN_FILE` is rejected before creating a connection; the file is never
read. This prevents a shared credential from being disclosed to scanned hosts.

Pairing verifies `SystemService.GetSystemInfo` and controller identity; it does
not perform REST login or token refresh. Controllers requiring authentication
return `Unauthenticated`, with no REST fallback. Supporting authenticated
controllers requires a separate design with verified TLS and credentials bound
to explicitly trusted endpoints. The `x-frigate-actor` audit metadata remains
present and is not an authentication credential.

Existing CT1 rows use the default RPC endpoint without delete/re-pair. Creating
the device verifies the saved serial against `SystemInfo.id` before polling or
controls. Custom RPC endpoints are persisted by identity on rediscovery;
rediscovery retains an existing CT1's web port and scheme, so a discovery default
cannot replace a custom browser address. The probed host remains authoritative.
Direct rediscovery matches the organization's server-managed container by shared
IP, class, driver, model and manufacturer, independently of HTTP/RPC ports. It
rejects ambiguous matches and excludes FleetNode-owned inventory. FleetNode
discovery reports are admitted against the scanned RPC port; their separate HTTP
port is retained for the ProtoOS proxy. For new CT1s, that HTTP address is a
configured default, not a listener verified by the gRPC discovery call.
FleetNode carries the independent endpoint through pairing, telemetry, commands,
and IP recovery. Recovery scans the configured RPC port and preserves the web
port; it does not infer a changed RPC listener port. Endpoint changes invalidate
the FleetNode telemetry handle fingerprint.

## Collection and controls

Each active CT1 owns a reusable gRPC connection, closed by `Device.Close`.
Temporary discovery/pairing connections are closed on success and failure.
Client construction is lazy; a bounded SystemInfo call proves reachability.
The transport reconnects after interruptions. No CT1 REST fallback is enabled.

Collection requires `HashboardService.GetContainerMap`. Mining status, plant/PDU/fan
health and controls run concurrently as supplemental calls with individual budgets.
The outer collection deadline is ten seconds; supplements reserve 500 ms for
returning the core result. Cancellation and gRPC status errors are retained.
Structured warnings identify unavailable supplements and rejected slot data.

The maximum received RPC message is 8 MiB. No gRPC message compression is enabled.
Controls retain the existing mutex/sequence: validate desired state, read live
support and known state, write once, then require authoritative readback. General
controls require a confirmed controller-register observation with the requested
value. The pinned breaker producer returns success only after authoritative PLC
status readback, so its false scalar is a valid confirmed-open state. Transport
failures report unconfirmed state; configured retry policies are disabled and no
application retry/hedging replays writes. gRPC may transparently retry only calls
known not to have reached application processing.

## Protobuf presence and diagnostic fidelity

Normalization reads typed protobuf values. Diagnostic JSON is generated separately
with the pinned schema's field names and units. Known diagnostic fields, nested
issues and shutdown evidence are retained; future unknown protobuf fields cannot
be represented by name until the pin is updated.

- Slot arrays are not physical order. Explicit hashboard/configured locations
  determine positions; absent, conflicting, duplicate or out-of-range placement
  remains unavailable. An explicit configured empty slot remains empty.
- Optional measurements preserve absent versus zero. Board and legacy container hashrate have no
  presence/validity field in this contract, so ambiguous scalar zero remains
  unavailable. A present PSU electrical message is defined upstream as a complete
  snapshot, including zero power/sample age. Legacy mining power is used only
  with complete shared-assignment coverage from `current_measurements`.
- Unknown enums do not become a known healthy state. Source timestamp freshness,
  invalid diagnostic rejection, identity conflicts, shared-PSU deduplication,
  ASIC temperature, and module state rules remain in the existing normalizer.
- Container hashrate still comes from mining status; facility/plant power still
  takes precedence over the mining fallback; container efficiency still uses
  that final selection. These definitions are deliberately not replaced by the
  planned polling/storage corrections.

## Validation and performance boundary

`server/fake-ct1` exposes both transports over the same state/clock. The plugin
integration test builds and launches it with independent ephemeral ports, then
checks all 288 occupied boards, collection, pairing and the HTTP system endpoint.
Real gRPC tests cover restart/reconnect, TLS certificate rejection, credential-free
discovery, rejected token configuration, status/cancellation/deadlines, receive
limits and ambiguous writes.
Real Postgres tests cover endpoint persistence and identity-based rediscovery.

On an Apple M4 Max, a 96-module/288-board simulator map measured 110,248 protobuf
bytes versus 382,898 equivalent protojson bytes, uncompressed. The reproducible
`BenchmarkContainerMapCodec` measured the following single local run; timings
vary and exclude network, normalization, diagnostic JSON generation and hardware:

| Codec operation | Time/op | Bytes allocated/op | Allocations/op |
| --- | ---: | ---: | ---: |
| Protobuf encode | 197 µs | 114,692 | 1 |
| Protobuf decode | 310 µs | 455,321 | 10,506 |
| Protojson encode | 1,798 µs | 2,554,702 | 15,552 |
| Protojson decode | 3,334 µs | 964,559 | 51,223 |

Run from `server/fake-ct1` with `GOWORK=off go test -run
TestGRPCSharesContainerState -bench BenchmarkContainerMapCodec -benchmem -v`.
This measures equivalent wire codecs, not the old plugin's JSON decoder or an
end-to-end speedup. Hardware latency, timeout rates and controller CPU/load at
the existing cadence remain rollout measurements.

## Local development upgrades

Rebuild/recreate the running `fake-ct1` service when adopting this transport:
`docker compose -f server/docker-compose.yaml up -d --build --no-deps fake-ct1`.
An older simulator may still pass its HTTP health check while having no listener
on 9095. The collector logs `Unavailable` / `connection refused`, and stale
module observations then appear unknown. If polling has already stopped after
repeated failures, restart the local Fleet API after the simulator is ready.

The development configuration uses `PROTO_MINER_PORT=0` so discovery includes
both R2 HTTP(S) and CT1 gRPC. A fixed `8080` override excludes native CT1 discovery.
For shared or custom Compose projects, use the same project/override files that
started the simulator, and rebuild only that service; retain its controller ID
and network address. REST/ProtoOS continues to use its existing HTTP listener.

Board serials now use a stable prefix derived from `SERVER_ID`. When upgrading
an already-paired legacy simulator, set `CT1_INSTANCE_ID` to its original random
prefix (for example, `eb22b1c8` for `CT1-eb22b1c8-0001`) to preserve existing board
identities. Keep this override across subsequent recreates.

## Rollout and rollback

Apply the additive endpoint migration and deploy compatible server, FleetNode and
Proto plugin builds together. Before rollout, validate one direct and one
FleetNode-managed controller running the selected firmware contract: SystemInfo
identity, the full map/diagnostics, supplemental fields, both web links/proxying,
reconnect, configured TLS, and approved control readback. Authenticated controllers
remain outside the supported rollout configuration. Compare bytes, latency,
timeouts and controller load against the previous build at the same cadence. No hardware validation is implied by simulator success.

Monitor failed required reads, supplemental gRPC status warnings, unknown slots,
and unconfirmed writes during the canary. Roll back application/plugin builds to
restore the previous REST path; keep the additive columns while rolling back.
The down migration removes only RPC endpoint configuration after all consumers
have rolled back. It does not modify collected telemetry/history.
