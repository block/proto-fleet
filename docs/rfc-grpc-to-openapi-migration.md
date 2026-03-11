# RFC: Deprecate gRPC — Consolidate on OpenAPI for Rig Communication

| Field | Value |
|-------|-------|
| **Status** | Draft |
| **Author** | mcharles |
| **Date** | 2026-03-06 |

## Problem

We maintain **two** API specs for the same rig firmware interface — gRPC (13 protos, 97 RPCs) and OpenAPI (58 REST endpoints) — each with its own auth model. This means:

- **Double the testing surface** — every firmware API change must be verified against both protocols
- **Drift risk** — the two specs can (and do) fall out of sync, creating subtle behavioral differences
- **Confusing for contributors** — as we open-source the fleet SDK, new developers encounter two ways to do the same thing with no obvious reason to prefer one over the other
- **Redundant code** — the simulator implements every endpoint twice, and we carry ~25K lines of generated protobuf code

**Proposal:** Consolidate on OpenAPI. Rewrite the plugin client to use REST, delete gRPC protos and generated code.

## Coverage Gaps (Firmware Prerequisites)

Most gRPC RPCs already have REST equivalents. **Three pairing endpoints do not** — these block migration:

| Missing REST Endpoint | Used For |
|---|---|
| `GET /api/v1/pairing/info` | Device discovery (returns MAC, serial) |
| `POST /api/v1/pairing/auth-key` | Set ed25519 public key during pairing |
| `DELETE /api/v1/pairing/auth-key` | Clear auth key during unpairing |

`FactoryReset` and `ClearUserSettings` also exist only in gRPC, but the fleet plugin already marks these as unsupported (`CapabilityFactoryReset: false`) — no functional loss. Debug API (29 RPCs) is only used by `miner-debug-cli` — can be handled separately.

## Auth Decision: Two Options

The gRPC and REST ports use **different JWT systems** — tokens are not interchangeable:

| | gRPC (port 2121) | REST (port 80/443) |
|---|---|---|
| Algorithm | EdDSA (Ed25519) | HS256 |
| Key | Asymmetric — pubkey set during pairing | Symmetric — shared secret |
| Token origin | Fleet signs JWT with ed25519 private key | Firmware issues JWT after password login |
| Lifetime | Fleet-controlled expiry | Access: 1h, refresh: 30d |

### Option A: EdDSA on REST *(lower fleet effort)*

Firmware REST middleware updated (~20 lines in `middleware.rs`) to accept EdDSA tokens alongside existing HS256. Fleet continues signing JWTs exactly as today, sends them to REST endpoints instead of gRPC.

| Pros | Cons |
|---|---|
| No SDK/SecretBundle changes | Requires firmware change |
| No JWT refresh logic needed | Two auth paths in firmware middleware |
| No password storage in fleet | |
| Plugin client swap is mechanical | |

### Option B: Password-based REST JWT *(lower firmware effort)*

Fleet stores the device password, calls `POST /api/v1/auth/login` to get JWTs, manages token refresh lifecycle.

| Pros | Cons |
|---|---|
| No firmware auth changes needed | Fleet must store device passwords |
| Uses auth exactly as ProtoOS web UI does | JWT refresh lifecycle adds complexity |
| Single auth path in firmware | SDK `SecretBundle` migration (BearerToken → UsernamePassword) |
| | Re-auth needed if user changes password via web UI |

## Key Risks

- **Firmware pairing endpoints** are the critical path blocker — start coordination now
- **Error format differences** — protobuf enums → JSON; the 737-LOC error mapper needs porting but should shrink (HTTP status codes are simpler)
- **Streaming upload** — gRPC client-streaming becomes multipart form upload (simpler, already spec'd in OpenAPI)

## Decisions Needed

1. **Auth: Option A (EdDSA on REST) or Option B (password-based JWT)?**
2. **Debug CLI:** migrate to REST, keep as gRPC exception, or deprecate?
3. **Firmware timeline:** when can pairing REST endpoints ship?
