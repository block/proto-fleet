# CT1 native gRPC transport

The Proto plugin uses Frigate MDK v2 gRPC for CT1 discovery, pairing,
current telemetry, and the existing boolean equipment controls. ProtoOS and
Fleet's web proxy continue to use HTTP(S). Poll frequency is unchanged. Concurrent polling, physical-equipment rollups,
and topology persistence are described in [polling measurements](ct1-polling-metrics.md).
Retained historical ingestion remains #179.

## Firmware contract

The shared [MDK module](../../sdk/mdk/README.md) pins Frigate commit
`e069860da77f91857d664d26bbadd6672e0463ba`, the upstream main revision selected
at implementation start. The additive ServerUpdateService contract comes from
`4677049b24e9428ebf01b17323682e2a16d332d7` for partner identity discovery. These are
source compatibility targets, not qualified hardware deployments. Both the plugin and fake-ct1 use the same generated types.

At that revision, `internal/config/config.go` defaults `server.mdk_listen` to
`:9095` and `server.api_listen` to `:8080`. `pkg/api/server.go` registers the
System, Hashboard, Mining, Telemetry and Power services on a plaintext gRPC
server. There is no registered AuthService/login RPC. The optional
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

## Endpoint groups and HA failover

The Proto plugin reads `SystemService.GetSystemInfo` and
`ServerUpdateService.GetServerUpdateSupport`. The latter reports `instanceId`
and `peerInstanceId`. The plugin hashes the sorted pair into an opaque device
key. This key groups discoveries; it is not a container serial number. Neither
Docker MAC addresses nor hashboard inventory determines the pair.
Discovery allows five seconds for both identity calls, within the caller's
deadline. If the optional partner service fails, a controller with no saved
endpoint group can still use the single-address path. Cancellation still stops
discovery. A saved group requires a successful partner response on every probe.

Fleet stores generic `DeviceEndpoint` records in `discovered_device.endpoints`.
Each record contains an endpoint ID, the plugin's device key, and independent
web/RPC transport settings. It also carries known member IDs, including a peer
whose address has not been scanned. Fleet scopes that key by organization, driver, and
network owner (direct Fleet Server or a Fleet Node). Fleet Node reports and direct
container or endpoint-group scans serialize database writes per organization
before locking groups and inventory rows. This
prevents direct scans and report batches from taking those locks in opposite
orders. Direct plain-rig scans do not take this lock; network probes remain concurrent.
Before pairing, scans merge addresses
by endpoint ID. A later scan of one address retains the
other address. If an unpaired controller takes its peer's saved address, Fleet discards that
stale peer endpoint. Scan the peer's new address to restore both endpoints.
The remaining endpoint keeps its web settings; set any custom web settings for
the discarded endpoint again after discovery. If the browser target was discarded,
Fleet uses the remaining verified endpoint.
If an unpaired controller reports a replacement partner at its saved RPC route,
Fleet keeps the inventory row and discards the old partner's routes. Scan the new
partner before pairing. A changed group without a matching saved controller route
is rejected. A paired container must be unpaired before a partner change.
Fleet passes the complete saved list to the plugin for pairing, telemetry, and
commands, including through Fleet Nodes. Endpoint changes alter the cached
plugin configuration fingerprint.
Pairing updates identity metadata without replacing addresses from later scans.
Direct and Fleet Node pairing use the same endpoint-group check.
Pairing cannot introduce a new endpoint group. If pairing reports a new group,
run discovery before pairing again so the inventory checks can reconcile it.
Before committing pairing state or identity, Fleet locks the inventory and
compares its current network owner, group, and routes with the snapshot used for pairing.
Fleet Node commands retain their dispatched snapshot for this check. A changed
binding rejects the result and requires a new pairing attempt. An older metadata
save also preserves the current identity when the endpoint group has changed.
Once paired, the saved endpoint routes are pinned. Discovery cannot add an
address, relocate a controller, or exchange endpoint identities at an address.
Controller IDs and grouping keys are not authentication credentials. Unpair the device,
scan both addresses, and pair again to approve changed routes. Initial discovery
and pairing still require a trusted network; this does not authenticate a server
that can impersonate a controller at the same saved address.
Removing only its Fleet Node binding does not release the saved routes while
the device remains paired.

Scan both server addresses to learn both endpoints. The partner ID is not an
address, and the plugin does not dial an unverified URL supplied by a controller.
The plugin retains both gRPC connections. Before an operation, it probes both
connections in parallel with a shared probe budget of at most five seconds.
When the caller has a deadline, probes use at most half the remaining time so
collection or the command can use the rest. It verifies each
server's ID and declared pair, then reads `GetSystemStatus`. Reads prefer a
settled write-primary and can use a reachable standby. Writes require one
controller with primary role and explicit write authority. Recovery health
does not override that authority. A conflicting write claim rejects the command. Fleet
never promotes a server; Frigate performs that operation. A failure during a
read is retried on the next poll. A failed or uncertain mutation is never
replayed on the partner. The next new command performs a fresh selection.
Device creation reuses the verified identity from this probe. Paired tank and
module commands do not repeat identity discovery after selection.

The plugin reports healthy HA only when both expected servers respond with:

| Field | Primary | Standby |
| --- | --- | --- |
| `haRole` | `HA_ROLE_PRIMARY` | `HA_ROLE_STANDBY` |
| `containerWritePrimary` | `true` | `false` |
| `databaseConnected` | `true` | `true` |
| `haRecovery.state` | `HA_RECOVERY_STATE_STEADY` | `HA_RECOVERY_STATE_STEADY` |

A missing server, invalid identity, missing/unknown field, different role, or
recovery state reports degraded HA. An explicitly false `dcsHealthy` also
reports degraded HA. Older firmware may omit that optional DCS field. Servers
without partner metadata remain usable through the existing single-address
path, but cannot report healthy HA.

Fleet stores the plugin's HA observation without interpreting CT1 APIs. The
container's Issues column shows **HA degraded** for a missing, degraded, or stale
HA observation. A current healthy observation clears it. Observations become stale
after two minutes; an older sample cannot replace a newer recovery sample.
If all endpoints are unreachable, ordinary device polling reports the outage,
and the previous HA observation becomes stale.

### Boundaries and rollout

- Upgrade Fleet Server, the managing Fleet Nodes, and the Proto plugin together.
  Scan both addresses before pairing to populate the saved endpoint list. For
  existing paired inventory, unpair before adding the partner address.
- Existing inventory identity is retained when a scan matches one compatible
  saved device. Known member IDs let either server match a legacy controller's
  saved serial before both addresses are scanned. Fleet retains that controller's
  saved address and custom web settings. For an old unpaired Fleet Node discovery
  with no saved serial, scan its saved address first to establish this identity.
  Multiple matching legacy records are rejected rather than merged blindly.
  Resolve duplicate legacy inventory before pairing. An ambiguous Fleet Node
  discovery is rejected on its own; unrelated reports in the batch can proceed.
  Database failures still roll back the whole batch.
- A paired legacy row with RPC port zero retains an unspecified driver default.
  Fleet does not interpret that default as an explicit port from a new scan.
  Unpair the device, scan both addresses, and pair again to upgrade that row.
  For unpaired inventory, a scanned explicit route replaces an unknown legacy
  route at the same host rather than retaining two conflicting identities.
- Discovery lists log and skip invalid stored endpoints, including across page
  boundaries. Unpaired totals use the same validation as their lists. Direct
  route lookups still reject invalid endpoints. A grouped
  device cannot silently lose its endpoint list when HA metadata disappears.
- A replacement Fleet Node can reclaim one matching endpoint group from a revoked
  node at its saved addresses. Route changes require unpairing first. Fleet
  retains the existing inventory identifier and placement. Groups
  owned by live nodes remain separate. Without a current group, multiple revoked
  matches require duplicate inventory cleanup before discovery can proceed.
- Native telemetry and SDK controls use plugin-selected endpoints. The HTTP
  ProtoOS proxy still uses its saved web address; HTTP proxy failover is separate.
  The plugin's web URL also uses that saved address and does not probe controllers.
- The legacy single-address recovery job does not rewrite endpoint groups.
  After an address changes, unpair, scan both addresses, and pair again. A scan
  cannot move an existing paired endpoint based on self-reported IDs.
- Authenticated production MDK discovery remains unsupported as described above.
  The opt-in `TestFrigateHALab` uses a local lab credential only for loopback test
  endpoints. It does not enable credential transmission during production scans.

## Collection and controls

Each active CT1 owns a reusable gRPC connection per saved endpoint, closed by
`Device.Close`.
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
