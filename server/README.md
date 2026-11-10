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

## CT1 module identity

CT1 modules use the PSU-derived `hashboard.moduleId` from the firmware map,
not `hashboard.psuIdentifier`. `container_module_identity.reported_module_id`
stores that firmware value; `module_id` references Fleet's own numeric module ID.
The lookup is scoped by organization to keep each organization's records isolated.
`container_module_identity` maps that identity to a durable Fleet module ID;
moving between positions, tanks, or controllers preserves it. Replacing boards
under the same PSU changes membership, while replacing the PSU creates another
module identity. Board identities remain independent in both cases.

Missing, malformed, duplicate, or conflicting PSU claims leave an unresolved
positional record with an empty `reported_id` and unknown state. Such records
cannot receive module measurements or enable module commands. They are never
promoted into a PSU identity. Independently identified board observations still
pass through normal board reconciliation and telemetry storage. The plugin also
preserves valid board readings when only the PSU identity is conflicting.
Nonblank PSU IDs with surrounding whitespace are rejected; firmware IDs used in
commands otherwise retain their exact spelling.

An active claim from another controller conflicts until explicitly removed or
unseen for two minutes. Conflicts withdraw command eligibility. Delayed polls
cannot change newer identity or placement evidence. Commands revalidate the
controller, PSU, position, and live firmware scope before execution.

`container_module_placement` records observed placement intervals. Board
installations retain their original controller, tank, module position, and site.
Module history filters samples by their recorded site, so moving a PSU cannot
authorize its old site's readings at the new site. Cached source readings from
before a placement change remain unavailable there, including on subsequent
polls; independently fresh board readings can still contribute.
Placement changes withdraw current module readings while retaining their source
watermarks. Site reassignment also withdraws state and command confirmation and
advances the controller's inventory watermark, so a delayed pre-assignment poll
cannot restore readings under the new site's authority. Retiring or deleting a
controller does not remove authorized access to retained module history.

Migration 1038 preserves all legacy module IDs and measurements. A unique legacy
PSU claim maps to its existing record. Duplicate claims, including retired
records, retain their IDs in `legacy_module_ids` with a null canonical `module_id`;
new polls cannot silently choose or merge them. The migration establishes current
placement evidence only from its upgrade boundary.

## Mining-group reconciliation

Mining-group changes run as an HA-managed background job: the passive Fleet
server does not reconcile them, and loss of active ownership cancels the job.
Normal progress uses the three-second worker cadence. Up to eight containers
reconcile concurrently, with at most one task per container. Each tick fills
available slots without waiting for slower containers to finish. Shutdown
cancels and waits for all running tasks before the worker returns.
Consecutive failures use exponential retry delays starting at five seconds and
capped at five minutes,
with 80–100% jitter. Retry timing is stored with the operation and survives
failover; dialog observations do not reset it. Successful progress resets the
delay. A controller reporting a failed transfer remains blocked and backs off,
even when the status request itself succeeds.

Retries retain the exact saved request and controller operation identity.
Backoff never releases a device-write admission or marks unresolved work
complete. A database outage or a crash before retry bookkeeping commits can
cause an earlier retry; the saved physical operation remains unchanged.

Activation carries an existing host-owned Fleet pool assignment from the whole
container to its current hashboard installations. Before stopping mining, Fleet
records the configuration revision, resolved worker names, ownership and exact
installation identities. Once activation is confirmed, retiring the container
assignment and adopting the board assignments commit together. Activation does
not clear or rewrite device pools or transfer boards; the controller's `default`
group retains them. Adopted boards remain unverified until fresh readback after
the handoff verifies ordered pool settings and membership; the observation from
before the lock gap cannot verify them. Unavailable or different observations
do not become verified. Group passwords
remain write-only, with encrypted credentials preserved from the source revision.

The original selection survives retries and restarts. Changed ownership,
permissions, installations or assignment generations prevent further mutations,
including the final resume. Subsequent reviewed configuration edits use ordinary
mining-group updates; unchanged inherited boards stay in `default` even when a
different board receives another configuration. Activation with an existing
assignment also requires pool-update authority.

Activation holds a shared organization lock during controller I/O, allowing
hosted-account reads to proceed. Admission and the database-only assignment
handoff acquire the exclusive organization lock before identity/device locks.
The handoff releases observation locks first, then rechecks the journal,
ownership, permissions and captured installations after reacquiring locks; a
changed journal or expired observation is retried with fresh controller state.
Database downgrade 1070 refuses to discard any retained handoff evidence,
including completed handoffs; ordinary application rollback retains the schema.

Containers already enabled without a captured pre-activation selection require
reviewed recovery. **Container pool assignment** only removes the old assignment
and always keeps device settings. Then assign the desired configuration through the
hashboard/module pool action. Matching URLs or the `default` group name alone
never establish Fleet ownership. An unresolved saved controller request must
first be settled through the existing device-write recovery workflow.

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

See the [curtailment schema notes](../docs/architecture/curtailment-schema.md)
for persisted scopes, target ownership, states and timestamp behavior.

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

The Server Checks workflow divides `go list ./...` across five runners: hosted
accounts, storage (SQL stores and TimescaleDB), device control (commands, write
fences, miner access, mining groups, and rollouts), remaining domain packages,
and all other packages. Each runner has its own database and runs one package at a
time to avoid database contention inside dispatch and admission deadline tests.
New packages fall into the domain or other shard automatically.

Each shard streams package progress, uploads `go test -json` results, and lists
its slowest tests in the job summary. Build caches are scoped by shard and saved
per revision so source-only changes can reuse compiled code from earlier runs.
The domain shards can restore the original domain cache when their own cache is
not yet available.

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

### Invitation codes

Customer account invitations (`HostedAccountService`) and host team invitations
(`invitation.v1.InvitationService`) share one code format and namespace: issuance
reserves each candidate digest with a transaction-scoped advisory lock, then
checks both `hosted_account_invitation` and `organization_invitation` before
storing it, so issuers in different organizations cannot commit the same code.
`InvitationService` previews and redeems codes of either kind through one public
surface. Invitation creation returns eight cryptographically
random characters as `K7MP-4X9R`. The 32-symbol alphabet
`ABCDEFGHJKLMNPQRSTUVWXYZ23456789` excludes `I`, `O`, `0`, and `1` and provides
**40 bits of entropy** (about 1.1 trillion codes). The server accepts ASCII
lowercase, ASCII spaces, and an optional hyphen at the four-character group
boundary. It hashes the canonical eight uppercase characters with
SHA-256; only the digest is stored. Digest conflicts retry generation up to five
times without replacing an existing invitation. Codes remain single-use,
revocable, and subject to live inviter authority. Logs, audit records, and later
list responses omit the secret. Optional links carry the same code using the
fragment-only transport described in the client guide.

The default and maximum lifetime are **24 hours**. Issuers can select one hour
for a shorter handoff.

The stored digest is unsalted, so read access to the database, a replica or a
backup allows an offline search of the 40-bit code space. Such access is outside
this threat model because it is already a database breach; respond by revoking
outstanding invitations along with the rest of the incident response.

Releases `v0.5.1-internal.1` through `v0.5.1-internal.5` served customer
invitation preview and redemption from `HostedAccountService` with 16-character
codes. Those RPCs are removed in favor of `invitation.v1.InvitationService`, and
earlier code formats are not accepted, so hosts upgrading from those releases
must reissue outstanding customer invitations.

Team invitations grant an organization role. Issuance requires `user:manage` and
the same role hierarchy as direct role assignment; Super Admin cannot be invited.
Each invitation stores an **authority fingerprint** from the SQL function
`team_invitation_authority_fingerprint` (migration 1036). It covers the invited
role's `permission_revision` and the inviter's live role assignments, including
each assigned role's revision. A trigger increments `role.permission_revision`
on every `role_permission` write, so revisions only grow. Issuance, preview,
acceptance and listing all compare against that one function. Any later change
to either side invalidates the code **permanently**: role changes replace
assignment rows, and a reverted permission edit, including one that empties a
role again, still leaves a higher revision. Re-promoting an inviter or reverting
a role edit therefore never revives a code. Preview and acceptance also recheck
the inviter's live hierarchy and reject a deleted role. Startup reconciliation
writes no `role_permission` row for permissions a role already has, so restarts
keep codes valid, but a release that adds permissions to a built-in role invalidates
outstanding invitations for that role, and for inviters who hold it; release
notes for such releases must say so. Acceptance runs under the organization lock
shared with membership and role writes. A signed-in login in the same organization without a team role keeps its
password and customer memberships and gains the role; logins that already hold
a team role are rejected rather than reassigned. Registration creates the login,
role assignment, consumed invitation and session in one transaction. Listing
omits accepted invitations, which remain stored as the link to the joining login.
Revoked and expired invitations can be deleted permanently; pending codes must be
revoked first. Issuance, revocation, deletion and acceptance write `auth`
activity records without the code.

Migration 1034 adds a PostgreSQL **per-client rate limit**, shared across every
API replica using the database. Public preview, signed-in acceptance, and
registration together allow **60 requests per client per one-minute window**
and **100 per one-hour window**, shared by both invitation kinds.
Each window starts with the client's first admitted request and resets when a
request arrives after it ends. One statement updates both client counters, and
a request the minute window rejects does not count against the hour. Invalid codes and rejected redemption
requests count; registration is charged once at the public handler, before its
internal preview and consumption steps. Throttled requests return
`ResourceExhausted`. Per-process preview concurrency and registration CPU limits
remain additional resource protections.

Client identity comes only from the trusted client-IP middleware. IPv4-mapped
addresses are normalized, and IPv6 addresses share a /64 allowance to prevent
trivial address rotation. Recipients behind the same public address share an
allowance. The table stores a SHA-256 client-address digest, window start, and
request count. Idle rows older than an hour are pruned in batches of at most 100
per admitted request, without waiting on locked rows. Admission has a two-second
timeout and database failures fail closed.

Every admitted request then spends one unit of a **deployment-wide allowance of
1,000 requests per hour**, stored in the same table under a fixed key and shared
by every replica. A client's own limit is checked first, so a throttled client
never spends the shared allowance. This ceiling, not client throttling, supplies
the guessing margin for eight-character codes: a code lives at most 24 hours, so
it faces at most 24,000 guesses however many addresses an attacker controls.
Against `A` simultaneously active invitations, the chance that any guess in a day
succeeds is at most `24,000 × A / 2^40`, about `2.2e-6` for 100 active
invitations and `2.2e-4` for 10,000. Without the ceiling, one IPv6 /48 (65,536
client allowances) could make 5.7 billion guesses a day and would reach one of
100 active invitations with probability near 40%.

The shared allowance is a deliberate trade-off. One client can spend at most 100
units of it per hour, so exhausting it takes at least ten distinct client
allowances (IPv4 addresses or IPv6 /64s). An attacker with that many addresses
can still block every preview and redemption until the hour resets. Existing
sessions, issued codes and all other Fleet operations are unaffected, and
recipients can retry later. Each request updates the shared counter in its
own short statement, so admission briefly serializes on that row. Per-client
throttling limits abuse; neither limit identifies people or stops a distributed
denial-of-service attack. See [OWASP token guidance](https://cheatsheetseries.owasp.org/cheatsheets/Forgot_Password_Cheat_Sheet.html)
for random, expiring, single-use credentials and [RFC 8628 §5.1](https://www.rfc-editor.org/rfc/rfc8628.html#section-5.1)
for the relationship between code entropy, lifetime, and guessing limits.

### Hosted mining reporting and allocation

`hosted_account_asset` stores an exclusive typed mining leaf: conventional miner
or stable hashboard identity, its primary customer and optional immediate subclient.
Both account references empty means available to the host. Recalled rows retain
monotonic revisions, so an old selection cannot overwrite a later recall and
reassignment. Modules are derived from established slot membership and ownership
of every installed board. Confirmed empty slots are allowed; empty modules,
unknown slots and conflicting identities do not qualify. Board allocations survive
module retirement and discovery gaps. Accepted controller movement updates placement
without changing ownership.

Module selections include all reviewed board IDs and revisions. Assignment expands
them into atomic board mutations and rejects changed membership or overlapping
selections. After reconciling each accepted controller inventory, the server
detects replacements in the same slot of a module with a confirmed PSU identity.
It uses durable board identities resolved from MAC addresses and serial numbers.
The replacement inherits the outgoing board's client and subclient. The outgoing
board returns to the host. Both allocation changes and a system activity event
commit with the inventory update under the organization lock.

The outgoing board must have no later installation or active observation. The
incoming board must be unallocated. Later manual allocation decisions, suspended
accounts, and destination site restrictions prevent automatic transfer. Empty or
unknown gaps preserve ownership. Later identity confirmation can complete a
transfer, using that accepted poll's timestamp for both history boundaries.
Ownership is never backfilled to an earlier unconfirmed observation.

Before a transfer, the source must have a complete normalized map, and every other
registered CT1 controller in the organization must
provide a complete inventory accepted after the candidate installation. Its
observation time must also reach that installation time. Missing, incomplete, or
older reports leave the old board's allocation in place. A report locating the
old board elsewhere prevents the transfer, regardless of controller poll order.
The source controller retries pending replacements on later polls. Offline peers
can delay automatic replacement; the host-operated fallback remains available.

Migration 1049 records allocation decisions, installations, and removals in database
sequence order. Device clock skew cannot override a later manual decision.
Placement-only updates retain allocation decision order. Reporting history still
uses the accepted observation time and waits if that time would overlap an earlier
ownership period. Existing pending replacements have no reliable ordering markers
and require the manual fallback. New replacements of existing boards are automatic.

Host operators can still transfer an old board's complete allocation chain to an
available, unallocated replacement with `ReplaceAccountHashboard`. This manual
fallback also uses one history boundary. Neither path applies pool settings.

Migration 1047 converts established current module allocations to board allocations.
It closes the old module periods and starts board periods at one upgrade boundary.
It preserves the old module history without inferring earlier board ownership.
Migration refuses allocated modules with unresolved membership. Before retrying,
restore a complete accepted inventory, or explicitly recall the unresolved module
allocation using the previous application version. The error leaves ownership
unchanged. Do not mark slots empty merely to pass the migration. Rollback refuses
once board allocations or board history exist; use a coordinated backup or fix
forward.

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
allocations. Assignment records a durable pool transition in the same transaction.
The pool worker applies the destination owner’s default, or clears device pools
when that owner has no configuration. Device writes happen after commit using
normal physical fencing and readback. See [pool defaults](../docs/development/pool-defaults.md).

`GetAccountDashboard` supplies the shared client/preview dashboard, with current
scoped totals, allocated-site summaries and historical readings. Requests require
an explicit selected account and a Fleet session. Allocation and site filters
are resolved on the server; selecting a subclient is a reporting filter and does
not resolve its private workspace authority. `GetAccountAssetDetail` independently
requires a current, available allocation and live site access.

Migration 1031 records half-open assignment/delegation intervals, including device
site and identity changes. Existing assignments establish ownership only from the
migration's recorded start; no earlier samples are backfilled from current owners.
Raw miner/board and legacy module samples join their typed identity and established intervals before
aggregation, so buckets crossing reassignment exclude other accounts' samples.
Recall leaves intervals intact. Current site permissions gate historical access;
restoration exposes retained samples again. Rollback refuses to discard retained
intervals. Module readings require each metric's full source-time window to fit an
established interval; collection time does not establish ownership. Current
readings use the same provenance checks before inventory sorting or aggregation.

`GetAccountDashboard` accepts ranges up to 366 days. Recent windows up to 24 hours
use authorized raw telemetry; older or longer windows use allocation-specific
hourly/daily summaries. See [hosted-account charts](../docs/development/ct1-polling-metrics.md#hosted-account-charts)
for availability, retention and work limits. Raw requests are
bounded to 300 weighted asset-days after allocation/site authorization: miners and boards
count once, legacy modules eight times including contributor-window scan margins.
Over-budget requests return InvalidArgument with guidance to shorten the range
or narrow allocation/site filters. Module history scans
collections with the source freshness and clock-skew margins using the
module/collection-time index and collection-first compressed ordering (1033), then authorizes and buckets independent metric
source windows. A stale PSU source time cannot exclude fresh board readings.

Current account totals, site summaries and allocation counts use miners and
hashboards, matching new historical buckets. Modules remain inventory groups;
their module/PSU readings are separate from account totals and never replace
missing board measurements. Legacy buckets retain module reporting and are
labeled in chart tooltips and the history view.
Board history joins the installation's recorded controller and site, not the
controller's current identifier or the board's current module. Independent board
diagnostic observations supply historical operating states.
Migration 1047 also captures authorized board observations in the retained
hourly/daily history introduced by migration 1044. Corrections and deletions
update the captured contribution. Both raw and retained charts split board
replacement periods so sequential boards do not count as simultaneous capacity.
Each retained metric must have its full source time range inside the displayed
period. An aggregate that spans a replacement boundary leaves a gap because its
samples cannot be separated from the retained sum and count alone.
Each summary checks at most four candidate periods, one per metric. Replacement
boundaries do not multiply query work for every unaffected board.

Power totals contain only measured equipment readings with reporting counts;
controller plant power never substitutes for assigned-module power. Efficiency
requires complete valid power and hashrate coverage across the selected assets,
both for current totals and every historical bucket. Missing values remain absent.

### Client activity

`ListAccountActivities` and `ExportAccountActivities` require a selected account
and a session with live account View access. The shared organization read lock
holds admission and site policy through each query; every export page repeats
admission. Organization activity APIs remain host-only.

The client workspace and read-only host preview use the existing Activity filters,
table and details. Rows, totals, options, search and CSV share one SQL projection:

- Selected-account creation, rename, capability/site-policy changes, team
  membership and invitation changes, ancestor grants, and account API-key changes.
- Selected-account pool configuration creation, revision and archival, and
  incoming-profile changes.
- Assignment and recall, including host actions targeting the selected account.
  A parent sees its own delegation actions; child-private account events require
  opening that child's independently authorized workspace.
- Conventional miner reboot, start/stop, power/cooling, pool, firmware and
  curtailment command completions. Both command creation and outcome timestamps
  must fit the same established half-open allocation interval. Parent estate
  events include permitted delegated miners, once per miner. Current account site
  policy and the previewing host's physical read authority also apply. Recall
  preserves permitted events from the account's allocation periods; unknown,
  pre-assignment and post-recall periods cannot be inferred from current owners.

Only fixed descriptions/types, event IDs/times, results and authorized scope
counts cross the boundary. Command metadata contains only recomputed
`success_count` and `failure_count` for visible miners. Account metadata is empty;
assignment counts are exposed as asset scope counts. Actor identities, audit
free text, historical account names, network locations, credentials, pool slots,
raw errors and organization batch IDs are omitted. There is no actor filter or
organization batch-detail drill-down. Account assignment/recall is independently
visible even when there is no retained miner interval.

Organization alerts and controller/module command outcomes lack this account-safe
evidence and are excluded. Module assignments/recalls remain visible. New event
types require an explicit projection decision. Account/authority changes clear
scoped rows and details and abort reads/downloads; denied reads withdraw rows,
filters and open details so late responses cannot restore them.

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

Shutdown cleans up plugins concurrently and returns as soon as their processes
and client cleanup finish. The per-plugin grace period and shared shutdown
context bound graceful shutdown; a pending process is force-killed when either
expires, and only that plugin reports an interruption. Forced cleanup has an
additional two-second bound to reap processes even after context cancellation.

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
