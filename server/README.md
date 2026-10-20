# Fleet Service

For dashboard bitcoin price, estimated hashprice, network hashrate, and outbound
feed configuration, see [market data](../docs/market-data.md).

Fleet is a Go-based service for managing a fleet of Bitcoin mining devices. It provides gRPC/HTTP API endpoints for device discovery, pairing, telemetry, command execution, and fleet management. It uses PostgreSQL/TimescaleDB for persistence and supports multiple miner types (Proto, Antminer, etc.) through a plugin-based architecture.

## Automatic miner recovery

Miner discovery and recovery assume an operator-controlled LAN or VPN. Keep miner
endpoints and discovery targets within that trusted network; MAC addresses and
serial numbers are reconciliation identifiers, not cryptographic authentication.

A cloud recovery scan checks the candidate's discovery identity before reading
stored credentials, decrypts those credentials for the driver, and confirms the
identity returned by the authenticated probe. Stock Antminers cannot report MAC or
serial during unauthenticated discovery, so cloud scans may probe identity-free
Antminer candidates with stored Antminer credentials on the trusted network. The
probe must return matching stable identity before an address change is accepted;
conflicting discovery identity is always rejected. Matching miners recover at their
new address without being added or authenticated again. Rediscovery preserves the
original miner ID, handles stale discovery rows, and refuses to replace another
paired miner occupying the endpoint.

If an identity-confirmed miner rejects its saved credentials, it becomes
authentication-needed. An identity-free Antminer probe cannot attribute a rejection
to the stored miner, so it leaves that miner's pairing status unchanged. Missing or
corrupt stored credentials also leave pairing status unchanged. Fleet Node recovery
continues to use the existing local-network workflow.

Proto connections verify the miner's live identity against its expected MAC and
serial, including when authentication is renewed. A different miner reusing the
old address is rejected even if it accepts the same password, rather than having
its telemetry attributed to the original miner.

Fleet Node telemetry reuses authenticated plugin devices for up to one minute,
as direct Fleet does. Connection, credential, or plugin changes replace cached
devices; failed or canceled polls evict them. Only idle devices are cached, and
expiry cleanup continues when polling stops. Commands use independent temporary
handles; gateway requests and telemetry retain the miner's stable Fleet ID.

Registrations count toward the handle limit until the plugin confirms removal,
including those with canceled or lost creation responses. Cleanup continues after
the operation returns, with one worker per handle retrying failed closes with
backoff across control-stream reconnects. Plugin shutdown cancels retries.
The shared limit is 1,536 handles: up to 512 idle cached devices plus the existing
1,024 active or pending-cleanup allowance. At capacity, new operations return busy
before registering a handle.

The Go plugin SDK keeps a handle in a closing state until the backend finishes
cleanup. Concurrent close requests wait for that same attempt, and failed
attempts remain retryable. A timed-out RPC therefore cannot make a still-running
close appear absent and prematurely release the node's handle budget.
The SDK reserves an ID during creation and rejected-result cleanup, returning a
retryable close error while either is running. IDs being closed cannot be reused
until cleanup succeeds.

## Firmware reboot reconciliation

Proto miner telemetry clears `REBOOT_REQUIRED` when sampled uptime proves the
miner booted after the pending update's reboot boundary, including reboots outside
Fleet and reboot requests whose responses were lost. Uptime retains its observation
time while cached; Fleet Node transports its age so reconciliation does not
depend on synchronized node and server clocks. Missing or ambiguous evidence
leaves the status unchanged. The database checks the current status and reboot boundary
atomically so an older boot cannot clear a newer firmware update. Ordinary
telemetry still preserves `UPDATING` and pending `REBOOT_REQUIRED` states.
Telemetry queries the database only for devices the status writer last read as
`UPDATING` or `REBOOT_REQUIRED`, or whose cached status hint is absent. The writer
refreshes the hint as it processes status results. If a separate status read fails
before a result reaches the writer, telemetry discards the hint so the next
successful uptime sample can check for a newly recorded reboot requirement.

For firmware reporting `confirming` or `success`, Fleet retains the successful
upload time separately from the time it records `REBOOT_REQUIRED`. A proven boot
after that upload avoids a redundant reboot and can clear a late status marker.
The state name alone is insufficient: older firmware can reconstruct `confirming`
after restarting only its API service. `installed` and devices without status
reporting continue to require a boot after Fleet observes installation completion.
Reboot completion does not establish successful image activation or rollback status.

This requires firmware to report actual host uptime or omit it when unavailable.
Older Go MDK firmware that substitutes API-process uptime after a failed
`/proc/uptime` read must receive the corresponding firmware correction; Fleet
cannot distinguish that fabricated value from a real host boot through MDKv1.

## Fleet Node bootstrap rate limits

| RPC                                            | Requests per source IP per minute |
| ---------------------------------------------- | --------------------------------: |
| `Register`                                     |                               120 |
| `BeginAuthHandshake` + `CompleteAuthHandshake` |                      600 combined |

The budgets allow 100 Nodes to enroll and authenticate behind one public IP.
Successful and failed attempts count. Heartbeats, control streams, discovery
reports, and artifact transfers are excluded.

Fleet rejects excess requests before decoding or database work with
`ResourceExhausted` and `Retry-After` in seconds. The Node daemon keeps its
credentials and retries on its next heartbeat tick, including after WAF HTTP 429.

Each budget uses fixed 60-second windows from an IP's first request and holds
at most 10,000 IPs, counting IPv6 clients per /64. At capacity, only expired
entries are removed; untracked IPs share one overflow budget with the same
limit. Counters reset on restart or HA failover.
Unresolved IPs share a counter. Fleet logs at most one throttle event per budget per minute, without
IPs or credentials.

Set `HTTP_TRUSTED_PROXY_CIDRS` to the actual ingress proxies, such as loopback
nginx and ALB subnet CIDRs. Headers from untrusted peers are ignored; incorrect
proxy settings can make unrelated clients share a quota. See the infrastructure
repository's `fleet-ha/SECURITY.md` for WAF configuration and monitoring.

## Worker-name pool updates

Worker-name changes require complete stored pool readback so rewriting usernames
preserves every slot's password, including an explicitly empty password. Fleet
preflights the selected miners before creating a batch. If any miner lacks the
reader, rejects the read, or returns unknown credentials or invalid slots, the
whole request fails without enqueueing or reporting updated miners. Reads run
with bounded concurrency and a five-second per-miner timeout. Execution reads and
validates the configuration again before writing; preflight results are not cached.

Proto, ASIC-rs, and Fleet Node miners currently lack this readback capability, so
worker-name changes are rejected at request time. Full pool configuration writes
remain available. Saved organization pool credentials do not substitute for
actual device readback.

The Antminer reader uses authenticated web configuration. Tests cover missing
passwords, all-asterisk masks, `<redacted>` in either case, and literal passwords
such as `x*`. These fixtures do not qualify stock or VNish firmware versions:
qualification must confirm that the endpoint exposes complete stored slots and
exact passwords, including explicit empty values, rather than another mask or
placeholder. Live firmware qualification remains outstanding.

## Hosted ownership and curtailment

Host curtailment applies across the fleet regardless of hosted account ownership.
Curtail, restore, and fallback-config delivery remain available during ownership
handoffs and in reserved, active, or quarantined ownership states. Handoffs retain
these queued commands while canceling superseded configuration commands.

Curtailment attempts bind to the exact command and physical miner. They retain
connection, credential, identity, and deletion checks. Returned synchronous
controls complete under the shared ordered firmware contract; explicit delivery
uncertainty still blocks handoff completion. Pool, reboot, firmware,
and other configuration writes retain their ownership and epoch checks. Customer
account transports cannot invoke host curtailment RPCs.

An unknown curtailment control quarantines reserved or active custody. Successful
synchronous plugin gRPC responses and Fleet Node acknowledgements complete these
controls; most returned RPC errors retain uncertainty. Audited recovery preserves
pending curtailment controls, so restart can dispatch them and create fresh
uncertainty.
Active-origin quarantine cannot use `restore_host`; restoring active allocation
custody requires future allocation controller integration. See the
[device-write recovery procedure](../docs/development/device-write-recovery.md)
for evidence requirements and checks before restarting dispatchers.

## Development Commands

### Build and Run

```bash
just dev              # Run local server with Docker Compose (watch mode)
just start            # Start all services (without watch)
just stop             # Stop services
just build            # Build all Go packages
just install          # Install fleetd binary
just rebuild-all      # Clean rebuild (wipes all data, rebuilds plugins)
just rebuild-services # Clean rebuild of docker services only (reuses existing plugin binaries)
just rebuild-fleet-api  # Rebuild just fleet-api
```

From the repo root, `just rebuild-plugin <name>` rebuilds a single plugin (`proto`, `antminer`, `virtual`, or `asicrs`) without touching the others.

### Delve debugging

`fleet-api` runs under `dlv` and exposes it on `:40000` inside the container. By default the dev image builds with Go's usual optimizations and inlining, which makes breakpoints land unreliably (some lines get folded into their callers, locals get optimized away). For reliable breakpoints, opt into a debug build by disabling optimizations and inlining:

```bash
GO_GCFLAGS="all=-N -l" just dev
```

Attach from the host with `dlv connect` or your IDE. Requires mapping port 40000 in `docker-compose.yaml` (not mapped by default).

### Profiling

In `docker-compose.base.yaml`, uncomment `HTTP_PPROF_ADDR` and the matching `ports` entry under `fleet-api` (the port mapping binds to host loopback only), then restart:

```bash
just rebuild-fleet-api
```

Capture profiles while the server is running:

```bash
# CPU (30s sample) — open interactive flame graph in browser
curl "http://localhost:6060/debug/pprof/profile?seconds=30" > cpu.pprof
go tool pprof -http=:8000 cpu.pprof

# Heap allocations
curl http://localhost:6060/debug/pprof/heap > heap.pprof
go tool pprof -http=:8000 heap.pprof

# Goroutine trace — shows GC pauses, scheduler latency, I/O stalls
curl "http://localhost:6060/debug/pprof/trace?seconds=5" > trace.out
go tool trace trace.out

# Live goroutine dump (useful for detecting leaks or deadlocks)
curl "http://localhost:6060/debug/pprof/goroutine?debug=2"
```

> **Note:** pprof exposes sensitive runtime data. Only bind to loopback (`127.0.0.1`) unless you intentionally want remote access. Never enable in production.

### Testing and Quality

```bash
just test             # Run all tests
just lint             # Lint code
just format           # Format code
```

To run tests for a specific package:

```bash
go test ./internal/domain/pairing -v
go test ./internal/domain/pairing -v -run TestFunctionName
```

### Code Generation

```bash
just gen              # Run all code generation
just gen-db-queries   # Generate database query bindings (sqlc)
just gen-sdk-protos   # Generate SDK protobuf code
just gen-go           # Run go:generate directives
```

All generated code must be checked into Git. Run `just gen` after modifying protobuf definitions, database migrations, or sqlc queries.

### Database Operations

```bash
just db-up            # Start PostgreSQL/TimescaleDB
just db-migrate       # Run migrations
just db-migration-new <name>  # Create new migration
just db-shell         # Interactive PostgreSQL shell
just db-down          # Stop database
just db-reset         # Run down migrations
```

Migrations are sequential and run automatically on startup. **Never modify existing migrations after they have been deployed.**

## Architecture

### Project Structure

The codebase follows a domain-driven design with clear separation of concerns:

- **`cmd/fleetd/`** — Main application entry point and configuration
- **`internal/domain/`** — Core business logic organized by domain (auth, pairing, telemetry, command, etc.)
- **`internal/handlers/`** — gRPC/HTTP request handlers that delegate to domain services
- **`internal/infrastructure/`** — Infrastructure concerns (database, queue, encryption, logging, networking)
- **`generated/`** — All generated code (protobuf, sqlc bindings)
- **`migrations/`** — Database schema migrations (sequential numbered files)
- **`sqlc/queries/`** — SQL query definitions for sqlc code generation
- **`sdk/v1/`** — SDK for external plugin integrations
- **`fake-antminer/`** — Development simulator for testing Antminer plugin integration

### Core Domains

- **Pairing**: Device discovery and registration. Supports multiple miner types through a pluggable pairer interface.
- **Telemetry**: Real-time and historical metrics collection. Uses TimescaleDB and a scheduler for periodic data collection.
- **Commands**: Asynchronous command execution with a database-backed message queue.
- **Fleet Management**: High-level operations for managing groups of devices (listing, filtering, monitoring status).
- **Authentication**: Token-based auth with separate token types for clients (users) and miners (devices).
- **Hosted Accounts**: Account hierarchy and fixed customer memberships, separate from organization RBAC, with scoped administration, invitations, pool revisions and reporting allocations. Live authority applies ancestor ceilings, site restrictions and suspension; allocation changes and audit commit together. Session-only mining reports aggregate the account’s allocated estate, including subclients, without granting child-private account access. See [the client workspace contract](../client/README.md#hosted-account-workspaces) for persona and reporting boundaries.

### Hosted account invitation codes

Invitation creation returns eight cryptographically random characters as
`K7MP-4X9R`. The 32-symbol alphabet `ABCDEFGHJKLMNPQRSTUVWXYZ23456789` excludes
`I`, `O`, `0`, and `1` and provides 40 bits of entropy. The server accepts ASCII
lowercase, ASCII spaces, and an optional single hyphen between the two groups.
It hashes the canonical eight uppercase characters with SHA-256; only the digest
is stored. Digest conflicts retry generation up to five times without replacing
an existing invitation. Codes remain single-use, revocable, and subject to live
inviter authority. Logs, audit records, and later list responses omit the secret.
Optional links retain the fragment-only transport described in the client guide.

The default and maximum lifetime are **24 hours**. The default accommodates
manual sharing across time zones; the former seven-day option is removed to
bound exposure. Issuers can select one hour for a shorter handoff.

Migration 1034 adds a PostgreSQL rate limit shared by all organizations
and every API replica using the database. The `hosted_invitation_rate_limit`
row coordinates access to the bounded `hosted_invitation_rate_limit_attempt`
ledger. In any rolling hour, preview,
signed-in acceptance, and registration together may perform at most **1,000
checks** and spend at most **100,000 exposure units**. Each check costs
`max(1, active invitations)` units, counting all unexpired, unrevoked,
unaccepted invitations, even when current inviter authority makes some unusable.
For example, 10 active invitations permit 1,000 checks/hour; 1,000 active
invitations permit 100. An excessive population fails closed rather than
weakening the bound.

Admission and issuance serialize through one database row. Admission counts
targets and looks up the digest before releasing that lock, and commits the
charge even for an unknown code. Successful previews and rejected redemptions
also spend budget. Registration spends one check during preview and another
before consumption, after password hashing. Malformed values never reach a
digest lookup and do not spend budget. Per-process preview concurrency and
registration CPU limits remain additional resource protections, not the
guessing defense. Rotating IPs, users, codes, services, or replicas cannot reset
the durable budget. Database errors fail closed; admission transactions have
a two-second timeout. Expired attempt rows are pruned lazily; storage is bounded
by the 1,000 admitted checks, with no caller identifiers or secrets in those rows.

For uniformly random guesses, summing the active target count over admitted
checks bounds success at `100,000 / 2^40`, about **1 in 11 million per hour**
across the deployment, or `2,400,000 / 2^40`, about **1 in 458,000 over 24 hours**.
These are conservative online guessing bounds, not protection against stolen
codes or offline attacks after a database compromise. The entropy/budget
tradeoff follows [RFC 8628 §5.1](https://www.rfc-editor.org/rfc/rfc8628.html#section-5.1).
The global limit deliberately trades availability during abuse for a fixed
guessing bound: exhausted requests return `ResourceExhausted` and recipients
must retry as older checks leave the rolling hour. Increasing replicas does not
increase the allowance.

### Hosted mining reporting and allocation

`hosted_account_asset` stores an exclusive typed mining leaf: conventional miner
or container module, its primary customer and optional immediate subclient.
Both account references empty means available to the host. Recalled rows retain
monotonic revisions, so an old selection cannot overwrite a later recall and
reassignment. Replacing/retiring a module does not transfer its reporting
allocation to another module ID.

Account View permits the primary customer estate aggregate and scoped inventory.
Membership-management or delegation permits relative child buckets/owner labels;
only delegation permits assignment and recall. These reporting permissions do
not alter the existing private account resolver. A host needs account management
and physical site/fleet/miner read authority; organization roles never supplement
customer authority in a selected customer workspace.

Allocation mutations validate an exact bounded list of typed leaves and expected
revisions. The transaction serializes with account administration, physical
identity bindings and container reconciliation, then rechecks eligibility and
site scope before writing the ledger and audit. New allocations require active
eligible hardware and destination policy. Recall can release retained unavailable
allocations. Assignment does not reserve/activate device-write fences, dispatch
firmware changes or apply incoming pool profiles.

`GetAccountDashboard` supplies the shared client/preview dashboard, with current
scoped totals, allocated-site summaries and historical readings. Requests require
an explicit selected account and a Fleet session. Allocation and site filters
are resolved on the server; selecting a subclient is a reporting filter and does
not resolve its private workspace authority. `GetAccountAssetDetail` independently
requires a current, available allocation and live site access.

Migration 1031 records half-open assignment/delegation intervals, including device
site and identity changes. Existing assignments establish ownership only from the
migration's recorded start; no earlier samples are backfilled from current owners.
Raw miner/module samples join their typed identity and established intervals before
aggregation, so buckets crossing reassignment exclude other accounts' samples.
Recall leaves intervals intact. Current site permissions gate historical access;
restoration exposes retained samples again. Rollback refuses to discard retained
intervals. Module readings require each metric's full source-time window to fit an
established interval; collection time does not establish ownership. Current
readings use the same provenance checks before inventory sorting or aggregation.

Charts use retained raw telemetry. Existing rollups lack the site or independent
source-time provenance needed for this authorization check, so aggregate-only
periods remain unavailable. Raw telemetry is retained for ten days;
`GetAccountDashboard` rejects ranges longer than 24 hours and reports availability
from the later of the tracking epoch and retention horizon. The account UI offers
1h (default) and 24h; host dashboard ranges remain unchanged. Raw requests are
bounded to 300 weighted asset-days after allocation/site authorization: miners
count once, modules eight times including contributor-window scan margins.
Over-budget requests return InvalidArgument with guidance to shorten the range
or narrow allocation/site filters. Module history scans
collections with the source freshness and clock-skew margins using the
module/collection-time index and collection-first compressed ordering (1033), then authorizes and buckets independent metric
source windows. A stale PSU source time cannot exclude fresh board readings.

Power totals contain only measured equipment readings with reporting counts;
controller plant power never substitutes for assigned-module power. Efficiency
requires complete valid power and hashrate coverage across the selected assets,
both for current totals and every historical bucket. Missing values remain absent.

### Rig Curtailment Configuration Delivery

MQTT settings changes durably request configuration delivery to every fully
paired Proto rig in the organization. Pairing requests delivery only to the
identified devices, and terminal command failures retry only the affected
device. Both paths use the same organization lease and generation counters so
concurrent requests coalesce without losing work that arrives during delivery.
The outbox records the latest whole-fleet generation with each worker claim,
so settings changes retain full scope when they coalesce with device requests.
Target requests are filtered by organization, manufacturer, and pairing status
before enqueueing. An empty target set never expands to the whole organization.

The reconciler acknowledges durable command enqueue; the command queue handles
device execution and retries. A disabled configuration is still delivered when
there are no enabled MQTT sources, clearing stale fallback settings on rigs.
Pairing retains a best-effort post-commit request, so a request persistence
failure is logged without undoing a successfully paired device.

### Plugin System

Plugins are external processes that communicate with the fleet service:

- **Discovery**: Plugins implement custom device discovery logic for new miner types
- **Pairing**: Plugins implement custom pairing logic for new miner types
- **Priority**: Plugin-based discoverers and pairers take priority over internal implementations

Configuration is in `cmd/fleetd/config.go` with options for plugin directories, startup timeouts, and health check behavior.

### Data Layer

- **PostgreSQL/TimescaleDB**: Primary data store with schema defined in `migrations/`. Migrations run automatically on startup.
- **sqlc**: Type-safe Go code generated from SQL queries in `sqlc/queries/`. Regenerate with `just gen-db-queries` from the server directory.
- **Stores**: Repository pattern implementations in `internal/domain/stores/sqlstores/`.
- **Transactions**: Transactor pattern (`sqlstores.NewSQLTransactor`) for managing transactions across multiple operations.

### API Layer

- **Protocol**: [Connect RPC](https://connectrpc.com/) supporting both gRPC and Connect protocols over HTTP/1.1 and HTTP/2.
- **Interceptors**: Authentication, error mapping, logging, and validation in `internal/handlers/interceptors/`.
- **API definitions**: Fleet Connect RPC contracts live in `../proto/`. The miner-hosted ProtoOS REST contract is vendored at `../proto-rig-api/openapi/MDK-API.json`.
- **Firmware integrations**: The [firmware rollout REST API guide](../docs/development/firmware-rollout-api.md) covers API keys, file uploads, channels, delegated control, and event polling.

## Running via Docker

The service runs in Docker with host networking mode. On non-Linux systems, enable host networking in Docker Desktop: **Settings → Resources → Network → Enable host networking**.

```bash
just dev
```

This will:

1. Connect to the database
2. Run any pending migrations
3. Start the server in Docker Compose with watch enabled
4. Serve the API on http://localhost:4000

### Test Simulators

Docker Compose includes simulated miners for development:

- **fake-proto-rig**: Proto firmware simulator (5 replicas by default)
- **fake-antminer**: Antminer simulator (2 replicas by default)
- **proto-sim**: Single Proto simulator on fixed ports
- **antminer-sim**: Single Antminer simulator on fixed ports

Scale replicas with: `docker compose up --scale fake-proto-rig=10`

## Configuration

The service is configured using environment variables or command-line flags. See `internal/domain/config.go`. For local development, create a `.env` file in this directory.

At startup, `fleetd` also loads `/etc/fleetd/config.yaml` if that file exists. The YAML file can use nested groups that mirror the config sections, for example:

```yaml
auth:
  client:
    secret-key: "replace-me"
    expiration-period: "1h"
  miner-token-expiration-period: "30m"
encrypt:
  service-master-key: "replace-me"
http:
  address: "0.0.0.0:4000"
```

Command-line flags override values from the YAML file.

## API Testing

## Error Query Service (Testing/Development Only)

The Error Query Service provides a mock error management system for testing. It can return controlled seed data or randomly generated errors for devices.

> **Note:** This is temporary scaffolding to enable client development while server-side error query logic is being implemented.

### Seed Data Configuration

```bash
# Via environment variable
ERROR_QUERY_TEST_ERROR_SEED_FILE=./seed_errors.yaml fleetd
```

**Docker Compose:** In `server/docker-compose.yaml`, uncomment the seed file lines:

```yaml
environment:
  ERROR_QUERY_TEST_ERROR_SEED_FILE: "/app/testdata/seed_errors.yaml"
volumes:
  - ./internal/domain/errorquery/testdata:/app/testdata:ro
```

**Behavior:**

- Devices listed in the seed file use **only** the errors defined in the file
- Devices **not** in the seed file get randomly generated errors (30% probability per device)
- To force a device to have no errors, include it with an empty errors list: `errors: []`

### Seed File Format

```yaml
devices:
  - device_id: 1
    device_type: proto
    errors:
      - canonical_error: HASHBOARD_OVER_TEMPERATURE
        severity: major
        first_seen_ago: 2h
        last_seen_ago: 5m

  - device_id: 2
    device_type: proto
    errors: [] # Healthy device with no errors
```

### Available Options

| Field                | Description                                        | Example                                |
| -------------------- | -------------------------------------------------- | -------------------------------------- |
| `canonical_error`    | Error code (with or without `MINER_ERROR_` prefix) | `HASHBOARD_OVER_TEMPERATURE`           |
| `severity`           | `critical`, `major`, `minor`, or `info`            | `major`                                |
| `cause_summary`      | Human-readable cause description                   | `"PSU temperature exceeded threshold"` |
| `recommended_action` | Suggested remediation                              | `"Check cooling system"`               |
| `impact`             | Effect on operations                               | `"Reduced hashrate"`                   |
| `component_id`       | Format: `{device_id}_{type}_{index}`               | `"1_psu_0"`                            |
| `first_seen_ago`     | Duration since first occurrence                    | `2h`, `7d`, `30m`                      |
| `last_seen_ago`      | Duration since last occurrence                     | `5m`, `1h`                             |
| `closed`             | Mark error as resolved                             | `true`                                 |
| `vendor_attributes`  | Custom key-value metadata                          | `psu_temp_c: "87"`                     |

### Batch ownership admission

An ordinary selected command batch is rejected in full if any selected miner is unavailable
or fenced by configuration ownership. The caller receives `FailedPrecondition`
with the blocked device ID; no part of that selected batch is enqueued and no
successful dispatch count is returned. This admission rule also applies to
per-device payloads. Filters may still report their existing skipped-device
results before queue admission. See the [migration maintenance procedure](../docs/development/migrations.md#hosted-write-fence-upgrade)
for the write-fence upgrade. Host curtailment commands remain fleet-wide and
bypass configuration ownership; a retired or unavailable miner still rejects the
selected batch.
