# Execution Plan: gRPC → OpenAPI Migration

Reference: [RFC — DASH-1338](https://linear.app/dash/issue/DASH-1338)

---

## Architecture Context

Understanding the internal firmware architecture is critical for scoping this work:

```
                        Rig Firmware (on device)
┌──────────────────────────────────────────────────────────┐
│                                                          │
│   mcdd (core daemon)                                     │
│   ├── gRPC server on port 2121 (external — fleet talks   │
│   │   to this directly via Connect-RPC)                  │
│   │   ├── MinerDataApi, MinerCommandApi, MinerSystemApi  │
│   │   ├── MinerPairingApi (unauthenticated)              │
│   │   └── Auth: EdDSA JWT via AuthInterceptor            │
│   │                                                      │
│   └── gRPC server on port 2122 (internal loopback only)  │
│       └── Same services, no auth                         │
│                                                          │
│   miner-api-server (REST frontend)                       │
│   ├── HTTP server on port 80/8080                        │
│   ├── Proxies to mcdd via gRPC on 127.0.0.1:2122        │
│   ├── Auth: HS256 JWT via middleware.rs                   │
│   └── Serves ProtoOS web UI + REST API                   │
│                                                          │
└──────────────────────────────────────────────────────────┘

        Fleet (proto-fleet)
┌──────────────────────────────────────────────────────────┐
│   plugin/proto/pkg/proto/client.go                       │
│   └── Connect-RPC to rig port 2121 (EdDSA JWT)          │
│   └── Raw HTTP to rig port 80 (login/password only)     │
│                                                          │
│   client/src/protoOS/                                    │
│   └── REST to rig port 80 (HS256 JWT)                   │
└──────────────────────────────────────────────────────────┘
```

Key insight: `miner-api-server` is already a REST-to-gRPC proxy. The internal gRPC between `miner-api-server` → `mcdd` on `127.0.0.1:2122` is **not in scope** — that's an internal implementation detail. We're only eliminating the **external** gRPC on port 2121 that the fleet plugin talks to.

---

## Work Streams

### Stream 1: Firmware — Add Pairing REST Endpoints
**Repo:** `miner-firmware` · **Crate:** `miner-api-server`

The three pairing operations currently only exist as gRPC RPCs in `mcdd`. The REST server needs new endpoints that proxy to mcdd's existing pairing logic.

#### 1a. Add gRPC pairing client methods to `client.rs`

`miner-api-server/src/client.rs` already has clients for `MinerCommandApi`, `MinerDataApi`, `MinerSystemApi`, and `MinerDebugApi`. Add a `MinerPairingApi` client that connects to mcdd on `127.0.0.1:2122`:

```rust
// New in client.rs
pub miner_pairing_client: Option<MinerPairingApiClient<tonic::transport::Channel>>,

pub async fn get_pairing_info(&mut self) -> Result<GetPairingInfoResponse, Status> { ... }
pub async fn set_auth_key(&mut self, public_key: String) -> Result<SetAuthKeyResponse, Status> { ... }
pub async fn clear_auth_key(&mut self) -> Result<ApiResultResponse, Status> { ... }
```

#### 1b. Add REST controller `controllers/pairing.rs`

New file with three handler functions:

| Endpoint | Handler | Auth | Notes |
|---|---|---|---|
| `GET /api/v1/pairing/info` | `get_pairing_info` | None | Returns `{ mac, cb_sn }` |
| `POST /api/v1/pairing/auth-key` | `post_auth_key` | Conditional* | Body: `{ public_key }` |
| `DELETE /api/v1/pairing/auth-key` | `delete_auth_key` | Auth required | Clears the key |

*`POST` is unauthenticated on first pair, authenticated on key rotation (mirrors gRPC `set_auth_key` logic in `mcdd/src/api/pairing.rs:20-41`).

#### 1c. Register routes in `main.rs`

Add to the actix-web app builder:

```rust
.service(
    web::scope("/api/v1/pairing")
        .route("/info", web::get().to(pairing::get_pairing_info))
        .route("/auth-key", web::post().to(pairing::post_auth_key))
        .route(
            "/auth-key",
            web::delete()
                .to(pairing::delete_auth_key)
                .wrap(Authentication { jwt_auth_data: jwt_auth_data.clone() }),
        ),
)
```

#### 1d. Update OpenAPI spec

Add the three endpoints to `miner-api-server/docs/MDK-API.json`. Copy updated spec to `proto-rig-api/openapi/MDK-API.json` in proto-fleet.

#### Files touched (miner-firmware)

| File | Change |
|---|---|
| `crates/miner-api-server/src/client.rs` | Add pairing client methods |
| `crates/miner-api-server/src/controllers/pairing.rs` | **New file** |
| `crates/miner-api-server/src/controllers/mod.rs` | Add `pub mod pairing;` |
| `crates/miner-api-server/src/main.rs` | Register pairing routes |
| `crates/miner-api-server/docs/MDK-API.json` | Add pairing endpoint specs |

---

### Stream 2: Firmware — EdDSA Auth on REST Middleware
**Repo:** `miner-firmware` · **Crate:** `miner-api-server`

*Only needed if team chooses Option A. Skip if Option B.*

The REST middleware (`middleware.rs:62-98`) currently only validates HS256 JWTs. Update `authenticate_request` in `controllers/authentication.rs` to also try EdDSA validation.

#### 2a. Add EdDSA validation path to `authenticate_request`

In `controllers/authentication.rs`, modify `authenticate_request` (line 127-142):

```rust
pub fn authenticate_request(
    bearer_token: Option<String>,
    secret: &[u8],
    blacklist: &HashSet<JwtClaims>,
) -> Result<(), String> {
    if let Some(auth) = bearer_token {
        // Try HS256 first (existing path)
        if validate_jwt_token(&auth, Some(JwtTokenType::Access), secret, blacklist).is_ok() {
            return Ok(());
        }
        // Fall back to EdDSA (fleet-signed JWT)
        if validate_eddsa_jwt(&auth).is_ok() {
            return Ok(());
        }
        Err("Invalid authentication token".to_string())
    } else {
        Err("No authentication token provided".to_string())
    }
}
```

#### 2b. Add `validate_eddsa_jwt` function

Reuse the existing EdDSA logic from `mcdd/src/api/auth.rs` (`verify_jwt_with_auth_key`). The function needs to:
1. Read the public key from `/etc/mcdd/api_pub_key.pem`
2. Decode with `Algorithm::EdDSA`
3. Validate `miner_sn` claim against device serial

This can import directly from `mcdd::api::auth` or duplicate the ~20 lines.

#### 2c. Add `jsonwebtoken` EdDSA + `ed25519` deps to `miner-api-server/Cargo.toml`

The `jsonwebtoken` crate already supports EdDSA — just need to ensure the feature is enabled.

#### Files touched (miner-firmware)

| File | Change |
|---|---|
| `crates/miner-api-server/src/controllers/authentication.rs` | Add EdDSA fallback in `authenticate_request`, add `validate_eddsa_jwt` |
| `crates/miner-api-server/Cargo.toml` | Possibly add deps for ed25519 key loading |

---

### Stream 3: Fleet — OpenAPI Go Client Generation
**Repo:** `proto-fleet`

#### 3a. Set up `oapi-codegen`

```bash
go install github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@latest
```

Create config file `plugin/proto/pkg/minerapi/oapi-codegen.yaml`:

```yaml
package: minerapi
output: client_gen.go
generate:
  client: true
  models: true
```

Generate:

```bash
oapi-codegen -config plugin/proto/pkg/minerapi/oapi-codegen.yaml \
  proto-rig-api/openapi/MDK-API.json
```

#### 3b. Add to build system

Add generation target to justfile/Makefile replacing the miner-api `buf gen` step.

#### Files touched (proto-fleet)

| File | Change |
|---|---|
| `plugin/proto/pkg/minerapi/oapi-codegen.yaml` | **New file** |
| `plugin/proto/pkg/minerapi/client_gen.go` | **New file** (generated) |
| `proto-rig-api/openapi/MDK-API.json` | Updated with pairing endpoints from Stream 1 |

---

### Stream 4: Fleet — Rewrite Plugin Client
**Repo:** `proto-fleet` · **Largest work item**

Replace all Connect-RPC calls in the plugin client with REST/HTTP calls using the generated OpenAPI client.

#### 4a. Rewrite `plugin/proto/pkg/proto/client.go`

| Current method | gRPC call | New REST call |
|---|---|---|
| `GetSoftwareInfo` | `dataClient.GetSoftwareInfo` | `GET /api/v1/system` |
| `GetDeviceInfo` | `pairingClient.GetPairingInfo` | `GET /api/v1/pairing/info` |
| `GetStatus` | `dataClient.GetMiningStatus` | `GET /api/v1/mining` |
| `GetPools` | `dataClient.GetPools` | `GET /api/v1/pools` |
| `GetTelemetryValues` | `dataClient.GetTelemetryValues` | `GET /api/v1/telemetry` |
| `Pair` | `pairingClient.SetAuthKey` | `POST /api/v1/pairing/auth-key` |
| `ClearAuthKey` | `pairingClient.ClearAuthKey` | `DELETE /api/v1/pairing/auth-key` |
| `StartMining` | `commandClient.StartMining` | `POST /api/v1/mining/start` |
| `StopMining` | `commandClient.StopMining` | `POST /api/v1/mining/stop` |
| `SetPowerTarget` | `commandClient.SetPowerTarget` | `PUT /api/v1/mining/target` |
| `SetCoolingMode` | `commandClient.SetCoolingMode` | `PUT /api/v1/cooling` |
| `GetCoolingMode` | `dataClient.GetCoolingMode` | `GET /api/v1/cooling` |
| `GetPowerTarget` | `dataClient.GetPowerTarget` | `GET /api/v1/mining/target` |
| `UpdatePools` | `commandClient.RemovePools` + `AddPools` | `POST /api/v1/pools` |
| `BlinkLED` | `commandClient.PlayLocateSequence` | `POST /api/v1/system/locate` |
| `GetLogs` | `systemClient.GetLogs` | `GET /api/v1/system/logs` |
| `GetErrors` | `dataClient.GetErrors` | `GET /api/v1/errors` |
| `Reboot` | `systemClient.Reboot` | `POST /api/v1/system/reboot` |
| `UpdateFirmware` | `systemClient.Update` | `POST /api/v1/system/update` |
| `loginWithPassword` | raw HTTP (already REST) | No change |
| `ChangePassword` | raw HTTP (already REST) | No change |

Key structural changes:
- Remove `dataClient`, `commandClient`, `systemClient`, `pairingClient` (Connect-RPC)
- Remove `webUIClient` / `webUIBaseURL` split — single HTTP client
- Remove h2c/HTTP2 transport config
- **Auth (Option A):** keep `SetCredentials(bearerToken)` — same EdDSA JWT sent as `Authorization: Bearer` on HTTP requests
- **Auth (Option B):** change `SetCredentials` to accept password, add login/refresh lifecycle
- Default port changes from 2121 → 80 (or 443 for HTTPS)

#### 4b. Rewrite `plugin/proto/internal/device/errors.go`

Currently maps protobuf enum types (`miner_error_code.RigErrorCode_*`, `miner_fan_api.FanErrorCode_*`, etc.) to SDK error types. REST errors come as JSON with a `message` field and HTTP status codes — significantly simpler. The 737 LOC should shrink substantially.

However: the `GET /api/v1/errors` endpoint returns structured error objects with codes. Need to verify the JSON schema matches what the SDK needs, and map JSON error codes instead of protobuf enums.

#### 4c. Rewrite `plugin/proto/internal/device/device.go`

Update `convertStatus`, `convertHashboards`, `convertASICs`, `convertPSUs` to work with JSON response types instead of protobuf messages. The structure is similar but field names follow JSON conventions (camelCase) rather than protobuf conventions (snake_case).

#### 4d. Update `plugin/proto/internal/driver/driver.go`

- `DiscoverDevice`: change required port from 2121 → 80
- `discoverWithScheme`: update `GetSoftwareInfo` call to use REST
- `PairDevice`: update to use REST pairing endpoint
- `getAndValidateDeviceInfo`: update to use REST pairing info endpoint

#### 4e. Update all tests

- `plugin/proto/pkg/proto/client_test.go` (963 LOC)
- `plugin/proto/internal/device/errors_test.go` (692 LOC)

#### Files touched (proto-fleet)

| File | Change |
|---|---|
| `plugin/proto/pkg/proto/client.go` | Full rewrite (992 LOC) |
| `plugin/proto/pkg/proto/client_test.go` | Full rewrite (963 LOC) |
| `plugin/proto/internal/device/errors.go` | Full rewrite (737 LOC) |
| `plugin/proto/internal/device/errors_test.go` | Full rewrite (692 LOC) |
| `plugin/proto/internal/device/device.go` | Moderate rewrite (conversion functions) |
| `plugin/proto/internal/driver/driver.go` | Update discovery/pairing (port, API calls) |
| `plugin/proto/go.mod` | Remove `connectrpc.com/connect` |

---

### Stream 5: Fleet — Simplify Fake Proto Rig
**Repo:** `proto-fleet`

#### 5a. Delete gRPC handlers

- `server/fake-proto-rig/command_api_handler.go` (270 LOC)
- `server/fake-proto-rig/data_api_handler.go` (626 LOC)
- `server/fake-proto-rig/system_api_handler.go` (241 LOC)

#### 5b. Add pairing REST endpoints to `rest_api_handler.go`

```go
mux.HandleFunc("/api/v1/pairing/info", h.handlePairingInfo)
mux.HandleFunc("/api/v1/pairing/auth-key", h.handleAuthKey)
```

#### 5c. Simplify `main.go`

- Remove Connect-RPC imports and handler registration
- Remove h2c/HTTP2 setup — plain HTTP server
- Change default port from 2121 → 80
- Rename env var `GRPC_PORT` → `HTTP_PORT`

#### 5d. Clean up `models.go`

Remove all protobuf type imports. Use plain Go structs.

#### 5e. Update `docker-compose.yaml`

- Port mappings: 2121 → 80
- Env vars: `GRPC_PORT` → `HTTP_PORT`
- Health check URLs

#### Files touched (proto-fleet)

| File | Change |
|---|---|
| `server/fake-proto-rig/command_api_handler.go` | **Delete** |
| `server/fake-proto-rig/data_api_handler.go` | **Delete** |
| `server/fake-proto-rig/system_api_handler.go` | **Delete** |
| `server/fake-proto-rig/main.go` | Simplify (remove gRPC, h2c) |
| `server/fake-proto-rig/models.go` | Remove protobuf imports |
| `server/fake-proto-rig/rest_api_handler.go` | Add pairing endpoints |
| `server/docker-compose.yaml` | Update ports, env vars |

---

### Stream 6: Fleet — Cleanup
**Repo:** `proto-fleet`

#### 6a. Delete generated code

- `server/generated/miner-api/` (25,285 LOC — entire directory)

#### 6b. Update `server/buf.gen.yaml`

Remove lines 3-12 and 41-78 (all miner-api generation rules). Keep SDK proto generation.

#### 6c. Delete or archive gRPC protos

- `proto-rig-api/grpc/` (13 proto files)

#### 6d. Clean up dependencies

- `plugin/proto/go.mod`: remove `connectrpc.com/connect`
- Run `go mod tidy` in both `server/` and `plugin/proto/`

#### 6e. Update documentation

- `proto-rig-api/README.md`
- `proto-rig-api/VERSION.md`
- `server/fake-proto-rig/README.md`
- `server/README.md`
- `.github/copilot-instructions.md`

#### 6f. Decide on `miner-debug-cli`

`server/cmd/miner-debug-cli/main.go` (307 LOC) — only consumer of `MinerDebugApi`. Options:
- Delete it (if firmware team has own debug tools)
- Keep it as sole gRPC consumer (retain debug protos only)
- Rewrite to REST (if firmware adds debug REST endpoints)

---

## Dependency Graph

```
Stream 1 (firmware: pairing endpoints)
    │
    ├──► Stream 2 (firmware: EdDSA auth) ──── only if Option A
    │
    ├──► Stream 3 (fleet: codegen)
    │        │
    │        ├──► Stream 4 (fleet: client rewrite)
    │        │
    │        └──► Stream 5 (fleet: simulator simplify)
    │
    └──────────────────────► Stream 6 (fleet: cleanup)
                              depends on 4 + 5
```

Streams 4 and 5 can run in parallel. Stream 2 can run in parallel with everything on the fleet side.

## Sequencing

| Order | Stream | Owner | Estimate |
|---|---|---|---|
| 1 | Stream 1 — Firmware pairing endpoints | Firmware team | 1–2 weeks |
| 1 | Stream 2 — Firmware EdDSA auth (if Option A) | Firmware team | 1 week (parallel with Stream 1) |
| 2 | Stream 3 — Fleet codegen setup | Fleet team | 1 week |
| 3 | Stream 4 — Fleet client rewrite | Fleet team | 2–3 weeks |
| 3 | Stream 5 — Fleet simulator simplify | Fleet team | 1 week (parallel with Stream 4) |
| 4 | Stream 6 — Fleet cleanup | Fleet team | 1 week |

**Total: ~6–8 weeks** with parallelization.

## Validation Checkpoints

1. **After Stream 1:** Firmware CI passes. New pairing REST endpoints return correct data in sim mode.
2. **After Stream 3:** Generated Go client compiles and type-checks against updated MDK-API.json.
3. **After Stream 4:** Plugin integration tests pass against `fake-proto-rig` (REST-only mode).
4. **After Stream 5:** `fake-proto-rig` starts on port 80, all REST endpoints respond correctly, `docker-compose up` works.
5. **After Stream 6:** `go mod tidy` clean, no references to `miner-api` generated code, CI green.
6. **Final:** End-to-end test with real firmware running updated `miner-api-server` + fleet plugin using REST only.
