# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Overview

Proto Fleet is a monorepo for a Bitcoin mining fleet management system with two main components:

- **Client**: TypeScript/React applications (ProtoOS dashboard and ProtoFleet management UI)
- **Server**: Go backend service (fleet API, telemetry, device management)

The system allows management of both Proto (custom firmware) and Antminer devices through a unified interface.

API specifications for Proto miner communication are vendored in `proto-rig-api/` (gRPC protos and OpenAPI spec).

## Quick Reference

For detailed development commands, testing instructions, and component-specific guidance:

- **Client development**: See `client/CLAUDE.md`
- **Server development**: See `server/CLAUDE.md`

## Tech Stack

- **Frontend**: React 19, TypeScript, Vite 7, Zustand, Tailwind CSS 4
- **Backend**: Go with Connect RPC (gRPC-compatible), PostgreSQL/TimescaleDB
- **API**: Protocol Buffers for type-safe cross-language communication
- **Build Tools**: Just (task runner), Buf (Protobuf), Docker Compose

## Go Workspace Setup

**Note**: This repository uses a Go workspace (`go.work`) for integrated development across the server and plugin modules. This is a temporary setup for pre-launch development to maximize development speed and will be removed before launch.

The workspace includes:
- `server/` - Main fleet backend service
- `plugin/proto/` - Proto miner plugin
- `plugin/antminer/` - Antminer plugin

Benefits:
- Local changes across modules are immediately available
- No need to publish module versions during development
- Shared Go module cache in CI/CD
- Simplified cross-module refactoring

The workspace is automatically active when running Go commands from the root directory.

## Initial Setup

```bash
# Activate Hermit environment (manages tool versions)
./bin/activate-hermit

# Install all dependencies
just init
```

## Common Development Workflows

### Running the Full Stack

```bash
# Start both client and server (ProtoFleet)
just dev
```

This starts the Go backend with Docker Compose and the Vite dev server for ProtoFleet at http://localhost:5173.

For running individual components or other development commands, see:

- Client commands: `client/CLAUDE.md`
- Server commands: `server/CLAUDE.md`

### Working with the Go Workspace

The workspace enables seamless development across server and plugin modules:

```bash
# Build all modules from root
go build ./...

# Test all modules from root
go test ./...

# Run tests for a specific module
go test ./server/...
go test ./plugin/proto/...
go test ./plugin/antminer/...

# Sync workspace dependencies
go work sync
```

When you make changes to the server module, the plugins automatically see those changes without needing to publish or manually update versions.

### Working with Protocol Buffers

All APIs are defined in `.proto` files in the `proto/` directory. After modifying proto definitions:

```bash
# Generate TypeScript and Go code from protobuf definitions
just gen
```

**Important**: Always commit generated code changes alongside proto definition changes.

The code generation process:

1. Validates proto files with `buf lint`
2. Generates TypeScript clients in `client/src/protoFleet/api/generated/`
3. Generates Go server code in `server/generated/`
4. Regenerates sqlc database bindings (if migrations changed)

## Monorepo Structure

```
proto-fleet/
├── client/                    # TypeScript/React applications
│   ├── src/
│   │   ├── protoOS/          # Single miner dashboard (REST API)
│   │   ├── protoFleet/       # Fleet management UI (gRPC streaming)
│   │   └── shared/           # Shared components and utilities (50+ components)
│   └── CLAUDE.md             # Detailed client development guide
├── server/                    # Go backend service
│   ├── cmd/fleetd/           # Main entry point
│   ├── internal/domain/      # Business logic (pairing, telemetry, command, etc.)
│   ├── internal/handlers/    # gRPC request handlers
│   ├── internal/infrastructure/  # Database, queue, encryption, logging
│   ├── migrations/           # Database schema migrations (sequential)
│   ├── sqlc/queries/         # SQL query definitions for code generation
│   ├── generated/            # Generated code (protobuf, sqlc)
│   └── CLAUDE.md             # Detailed server development guide
├── proto/                     # Protocol Buffer API definitions (shared)
│   ├── auth/, pairing/, telemetry/, fleetmanagement/, etc.
├── proto-rig-api/            # Vendored API specs for Proto miner communication
│   ├── grpc/                 # Protocol Buffer definitions
│   └── openapi/              # OpenAPI/Swagger specification
├── plugin/                   # External plugin support
├── deployment-files/         # Deployment configurations
└── bin/                      # Hermit-managed binaries
```

## Architecture Overview

### Protobuf-First API Design

All APIs are defined in Protocol Buffer format in the `proto/` directory. This ensures:

- Type safety across TypeScript and Go
- Automatic client/server code generation
- Clear API contracts and versioning
- Support for both gRPC and Connect protocols

### Client Architecture

Two separate React applications sharing common components:

**ProtoOS** (Single Miner Dashboard):

- REST API with polling for updates
- Generated types from OpenAPI/Swagger definitions in `proto-rig-api/openapi/`
- Zustand state management with slice-based architecture
- Served by embedded API server on mining device

**ProtoFleet** (Fleet Management UI):

- gRPC-Web with Connect-RPC
- Server-to-client streaming for real-time telemetry
- Zustand state management with fleet-specific slices
- Connects to Go backend service

**Shared Component Library**:

- 50+ production-ready UI components in `src/shared/components/`
- 40+ Storybook stories for visual documentation
- Reusable across both applications

See `client/CLAUDE.md` for detailed state management, API integration patterns, and component organization.

### Server Architecture

Go service following Domain-Driven Design principles:

**Core Domains**:

- **Pairing**: Device discovery and registration (supports Proto and Antminer via plugins)
- **Telemetry**: Real-time and historical metrics collection (stored in TimescaleDB)
- **Commands**: Asynchronous command execution with queue-based system
- **Fleet Management**: High-level operations for managing groups of devices
- **Authentication**: Token-based auth for clients (users) and miners (devices)

**Data Layer**:

- **PostgreSQL/TimescaleDB**: Primary data store with golang-migrate for schema migrations
- **TimescaleDB**: Time-series database for telemetry metrics (PostgreSQL extension)
- **sqlc**: Type-safe Go code generation from SQL queries

**Plugin System**:

- External plugins provide custom discovery and pairing logic for new miner types
- Plugins are loaded at startup and take priority over internal implementations
- Supports extensibility without modifying core codebase

See `server/CLAUDE.md` for detailed domain architecture, handler patterns, and database workflows.

### Data Flow

1. **Device Discovery**: Nmap-based network scanning or plugin-based discovery identifies devices
2. **Pairing**: Device authentication and registration with fleet database
3. **Telemetry Collection**: Scheduled polling collects metrics and stores in TimescaleDB
4. **Command Execution**: Queue-based system for asynchronous command dispatch
5. **Real-time Updates**: gRPC streaming pushes telemetry to connected ProtoFleet clients

### API Integration Patterns

**ProtoOS → Miner API** (REST):

- Polling-based updates
- Generated client from Swagger definitions
- Custom hooks abstract API calls and update Zustand store

**ProtoFleet → Fleet Service** (gRPC):

- Server-to-client streaming for live telemetry
- Generated client from Protocol Buffers
- Custom hooks handle streaming connections and store updates

Key difference: ProtoOS uses REST polling while ProtoFleet uses gRPC streaming for live data.

## Cross-Component Development Workflows

### Adding a New API Endpoint

1. Define the API in appropriate `.proto` file in `proto/` directory
2. Run `just gen` to regenerate TypeScript and Go code
3. Implement server handler in `server/internal/handlers/`
4. Register handler in `server/cmd/fleetd/main.go`
5. Create client hook in `client/src/{app}/api/`
6. Update Zustand store slice to consume the data
7. Commit proto definitions and all generated code together

### Making Database Schema Changes

1. Create migration: `cd server && just new-migration <name>`
2. Write both up and down migrations in `server/migrations/`
3. Run `just gen` to regenerate sqlc bindings
4. Update queries in `server/sqlc/queries/` if needed
5. Never modify existing migrations after deployment

### Adding Features to Client

1. Determine target app: ProtoOS, ProtoFleet, or shared
2. Check `src/shared/components/` for existing components (50+ available)
3. Place feature in appropriate `src/{app}/features/` directory
4. Create Storybook stories for new components
5. Write tests with Vitest and Testing Library

See `client/CLAUDE.md` for detailed component organization, import rules, and development patterns.

### Adding Business Logic to Server

1. Add domain logic to appropriate package in `internal/domain/`
2. Create gRPC handler in `internal/handlers/`
3. Add tests for domain logic and handlers
4. Update stores in `internal/domain/stores/sqlstores/` if database access needed

See `server/CLAUDE.md` for detailed domain patterns, testing infrastructure, and database workflows.

## Important Development Notes

### Go Workspace

The repository uses a Go workspace for integrated development:

- The `go.work` file at the root defines the workspace
- All Go modules (server and plugins) are included in the workspace
- Changes across modules are immediately available without version bumps
- Both `go.work` and `go.work.sum` are committed to Git for reproducible builds
- Run `go work sync` after updating dependencies to sync the workspace

**Important**: This workspace is temporary for pre-launch development speed and will be removed before launch.

### Code Generation

All generated code must be committed to Git. Run `just gen` after:

- Modifying protobuf definitions in `proto/`
- Changing database migrations in `server/migrations/`
- Adding/modifying sqlc queries in `server/sqlc/queries/`

Never manually edit generated files in:

- `client/src/protoOS/api/generatedApi.ts`
- `client/src/protoFleet/api/generated/`
- `server/generated/`

### Multi-App Build System

The client uses Vite with mode-based builds to support two separate applications:

- ProtoOS: `vite build --mode protoOS` → `dist/protoOS/`
- ProtoFleet: `vite build --mode protoFleet` → `dist/protoFleet/`

Each app has its own `index.html` entry point in `src/{app}/`.

### Component Boundaries

Maintain strict separation between applications:

- Code in `client/src/shared/` cannot import from ProtoOS or ProtoFleet
- ProtoOS and ProtoFleet cannot import from each other
- Server code is completely independent of client code

This ensures applications remain decoupled and shared code stays truly reusable.

## Git Workflow

### Creating Branches

Create feature branches from Linear issue numbers:

```bash
# From Linear issue DASH-123
git checkout -b <username>/dash-123-short-description
```

### Committing Changes

Write clear, logical commit messages:

```bash
git add .
git commit -m "feat: add telemetry streaming to fleet UI

- Implement server-to-client streaming connection
- Add telemetry slice to fleet store
- Update MinerList to display live metrics"
```

Follow conventional commit format:

- `feat:` - New feature
- `fix:` - Bug fix
- `refactor:` - Code refactoring
- `docs:` - Documentation changes
- `test:` - Test additions or updates
- `chore:` - Build/tooling changes

### Creating Pull Requests

**Important**: Always use the Linear MCP to retrieve the correct issue URL before creating a PR.

When creating a PR, follow these steps:

1. Use the Linear MCP `get_issue` tool to retrieve the issue details and URL
2. Use the returned URL in the PR body
3. Create the PR with `gh` CLI using the consistent format below

Example workflow:

```bash
# First, use Linear MCP to get issue details (this returns the correct URL)
# mcp__linear-server__get_issue with id: "DASH-123"

# Then create PR with the URL from Linear
gh pr create --title "[DASH-123] Brief description" --body "$(cat <<'EOF'
## [[DASH-123] Brief description](https://linear.app/squareup/issue/DASH-123/brief-description)

## Summary
- Bullet point summary of changes

## Test Plan
- How to verify the changes work

## Related
- Linear issue: DASH-123
EOF
)"
```

**Note**: Never manually construct Linear URLs. Always use the Linear MCP `get_issue` tool to get the correct URL format.

**GIF in PR Descriptions**: Include a relevant GIF from Giphy to make PRs more engaging:

- Search Giphy for a GIF that relates to the PR theme (cleanup, feature, fix, etc.)
- Use the direct `.gif` URL from `media*.giphy.com`
- Add it after the Summary section: `![Description](https://media*.giphy.com/media/.../giphy.gif)`
- Examples: Marie Kondo for cleanup/removal PRs, celebration GIFs for new features

## Code Quality Standards

### Architectural Principles

**Respect Abstraction Layers**
- Each layer should only interact with its immediate dependencies
- Example: In `plugin/proto`, only `client.go` should import miner API types
- `device.go` should work with domain types, never directly with external APIs
- When in doubt, check existing patterns before adding cross-layer imports

**Magic Numbers Are Forbidden**
- ALL numeric literals must be named constants, even if used 2-3 times
- Constants should be grouped by purpose (conversions, timeouts, limits, etc.)
- Include units in constant names: `timeoutSeconds`, `maxRetries`, `hashToTeraHashConversion`
- Document what each constant represents in comments

**Use Standard Library Constants**
- Use `math.MaxInt32`, `math.MaxInt64`, `math.MaxUint16`, etc. instead of hardcoded values
- Use `math.MaxFloat64`, `math.MinInt32`, etc. for numeric boundaries
- Example: Replace `65535` with `math.MaxUint16` for port validation
- Example: Replace `2147483647` with `math.MaxInt32` for int32 boundaries
- This makes code self-documenting and prevents typos

**Guidelines for Specific Value Types**

*Port Numbers and Network Values:*
- Always define as named constants: `const defaultProtoPort = 2121`
- Use `math.MaxUint16` for max port validation (not `65535`)
- Include port number in error messages via constant, not hardcoded

*Time Intervals:*
- Always use `time.Duration` constants: `const pollInterval = 5 * time.Second`
- Group related intervals together with clear names
- Example: `defaultHeartbeatInterval`, `defaultPollInterval`, `defaultRetryDelay`

*Buffer and Channel Sizes:*
- Define buffer sizes as constants: `const defaultChannelBuffer = 100`
- Use consistent buffer sizes across similar operations
- Document why a specific buffer size was chosen

*Conversion Factors:*
- Always name unit conversion factors: `const mhsToThsConversion = 1e6`
- Include units in constant name: `wattsToKilowatts`, `secondsToMilliseconds`
- Group conversion factors by domain (power, hashrate, time, etc.)

*Percentages and Ratios:*
- Name percentile/ratio values: `const percentile25 = 0.25`, `const halfCapacity = 0.5`
- Use descriptive names that explain intent

*String Parsing:*
- Use named constants for strconv parameters: `const decimalBase = 10`, `const int32Bits = 32`
- Makes parsing code self-documenting

**Linter Suppressions = Last Resort**
- `#nosec`, `//nolint`, etc. should be rare and justified
- Before adding a suppression, ask: "Can I validate this properly instead?"
- Example: Instead of `#nosec G115` on int conversions, add range validation
- If suppression is necessary, include a detailed comment explaining why

**Comments: Less Is More**

Prioritize self-documenting code. The best code is clear enough that it requires few comments. Use descriptive variable, function, and class names, and break down complex logic into smaller, focused methods to express intent directly in the code.

- **Explain why, not what**: Comments should provide context that the code itself cannot. Explain the rationale behind a specific approach, a complex business rule, or a workaround for a non-obvious edge case.
- **Avoid redundancy**: Do not restate in comments what is already obvious from the code.
- **Be clear and brief**: If a comment is extensive, it often indicates that the underlying code is too complex and needs refactoring. Use concise and precise language.
- **Use sparingly**: Comments are an "apology" for a failure to express intent in code. Use them as a last resort.

*Examples of obvious comments to avoid:*
```go
// ❌ Bad - obvious from code
// Parse port as int64 first to avoid overflow issues
portInt64, err := strconv.ParseInt(port, 10, 32)

// ❌ Bad - obvious from code
// Check for valid port range
if portInt < 0 || portInt > maxValidPortNumber {

// ❌ Bad - obvious from code
// Clear cached data
d.lastStatus = nil

// ❌ Bad - obvious from field name
type Driver struct {
    // devices tracks all active device instances
    devices map[string]sdk.Device
}
```

*Examples of valuable comments to keep:*
```go
// ✅ Good - explains context and reasoning
// Note: In integration tests, we may use different ports due to Docker port mapping
if portInt != d.requiredPort && d.requiredPort != 0 {

// ✅ Good - explains why with reference
// #nosec G115 -- Loop index inherently safe: bounded by slice length (max ~200)
Index: int32(i),

// ✅ Good - documents important contract
// Hardware indices (hashboards, ASICs, PSUs) are bounded by physical constraints,
// so this conversion is safe in practice.
func safeUint32ToInt32(value uint32) int32 {
```

*Guidelines for comment quality:*
- Keep package and exported function documentation comments (godoc)
- Remove comments that just describe variable assignment or obvious operations
- Remove inline comments that restate the operation (`// Create client`, `// Convert to int32`)
- Keep comments that explain non-obvious behavior, edge cases, or reasoning
- Keep comments that reference RFCs, tickets, or external documentation
- Keep TODO comments with ticket numbers
- If you need to explain basic operations, consider refactoring for clarity instead

*To review comments in your changes:*
Invoke the `@remove-obvious-comments` agent to automatically identify and remove obvious comments from the current branch changes.

### Code Refactoring

**Apply Rule of 3**
- When you see the same pattern 3+ times, extract it into a helper function
- Common patterns to watch for:
  - Unit conversions with repeated math
  - Type conversions with the same validation logic
  - Similar data structure transformations
- After initial implementation, actively scan for repetition before submitting PR

**Data Mapping Validation**
- Never assume data structure contracts without investigation
- When dealing with arrays/indices from external systems:
  1. Check the source code to understand how arrays are populated
  2. Verify if indices are stable across calls/reboots
  3. Document your findings in comments
  4. Add defensive validation if contracts are implicit

### Writing Valuable Tests

Tests should verify behavior that could realistically break in production. A valuable test:

- Tests business logic, edge cases, or error handling that requires careful thought
- Verifies non-obvious behavior that a developer might accidentally break
- Catches bugs that wouldn't be caught by the compiler or type system
- Tests integration points between components

#### Low-Value Tests to Avoid

Trivial nil/empty checks:

```go
// ❌ Low value - tests obvious nil behavior
func TestFoo_NilInput(t *testing.T) {
    result := foo(nil)
    assert.Nil(t, result)
}

// ❌ Low value - tests obvious empty behavior
func TestFoo_EmptyString(t *testing.T) {
    result := foo("")
    assert.Equal(t, "", result)
}
```

Testing standard library behavior:

```go
// ❌ Low value - tests base64 library, not our code
func TestDecode_InvalidBase64(t *testing.T) {
    _, err := decode("not-valid-base64!!!")
    assert.Error(t, err)
}

// ❌ Low value - tests JSON library, not our code
func TestDecode_InvalidJSON(t *testing.T) {
    _, err := decode(base64.Encode([]byte("not json")))
    assert.Error(t, err)
}
```

Redundant enumeration tests:

```go
// ❌ Low value - 22 tests that just verify serialization works for every enum value
// If one works, they all work - this is testing the serializer, not business logic
func TestRoundTrip_AllFields(t *testing.T) {
    for _, field := range allFields {
        for _, dir := range allDirections {
            // ... same serialization test 22 times
        }
    }
}
```

Tests that just verify compilation:

```go
// ❌ Low value - if types don't match, the compiler catches it
func TestNewClient_ReturnsClient(t *testing.T) {
    c := NewClient()
    assert.NotNil(t, c)
}
```

#### High-Value Tests to Write

Business logic with edge cases:

```go
// ✅ High value - tests specific business rule about config mismatch
func TestDecodeCursor_ConfigMismatchRejected(t *testing.T) {
    // Changing sort config between pages should be rejected
    cursor := encodeCursor(&sortedCursor{Field: Name, Direction: Asc})
    _, err := decodeCursor(cursor, &SortConfig{Field: IP, Direction: Desc})
    assert.ErrorContains(t, err, "cursor sort config mismatch")
}
```

Non-obvious special cases:

```go
// ✅ High value - tests that ERROR status triggers additional filter logic
func TestBuildFilterParams_ErrorStatusTriggersNeedsAttention(t *testing.T) {
    filter := &MinerFilter{DeviceStatusFilter: []Status{StatusError}}
    params := buildFilterParams(filter)
    assert.True(t, params.needsAttentionFilter)
}
```

Integration between components:

```go
// ✅ High value - tests that sort config properly flows through to SQL generation
func TestBuildKeysetSQL_TelemetrySortWithNullHandling(t *testing.T) {
    // Tests the complex NULL handling in telemetry sorts
    cursor := &sortedCursor{SortValue: "", CursorID: 25}  // NULL telemetry
    config := &SortConfig{Field: Hashrate, Direction: Asc}

    sql, args := buildKeysetSQL(cursor, config, 2)

    assert.Contains(t, sql, "IS NULL")
    assert.Contains(t, sql, "dd.id > $2")
}
```

#### Test Guidelines Summary

1. Don't test nil/empty inputs unless there's specific business logic for them
2. Don't test that libraries work (base64, JSON, etc.)
3. Don't write enumeration tests that run the same logic N times for N enum values
4. Do test business rules, especially non-obvious ones
5. Do test edge cases that could break in production
6. One good integration test > 20 trivial unit tests

### Self-Review Checklist for Claude Code

Before marking work as complete or asking the user to review, verify:

1. ✅ **Architecture**: No abstraction layer violations (check imports match established patterns)
2. ✅ **Magic Numbers**: All numeric literals replaced with named constants
3. ✅ **Linter Clean**: `just lint` passes without suppressions (or suppressions are properly justified with validation)
4. ✅ **Rule of 3**: Repeated patterns (3+ occurrences) extracted into helper functions
5. ✅ **Tests Pass**: `just test` succeeds for all affected modules
6. ✅ **Data Contracts**: External data mappings are investigated and validated (not assumed safe)
7. ✅ **Comments**: No obvious comments that just restate what the code does (use `@remove-obvious-comments` agent to check)

This checklist helps catch common issues before user review rather than requiring corrections afterward.

## Testing

The client and server each have their own testing approach:

**Client**: Vitest + Testing Library for unit/integration tests, Storybook for visual component testing
**Server**: Go test framework with Docker Compose providing test environment (PostgreSQL/TimescaleDB, simulated miners)

For detailed testing commands and patterns, see `client/CLAUDE.md` and `server/CLAUDE.md`.

## Additional Resources

- **Root README**: `README.md` - High-level project introduction
- **Client README**: `client/README.md` - Client build and directory structure
- **Server README**: `server/README.md` - Server features and API overview
- **Copilot Instructions**: `.github/copilot-instructions.md` - Code review guidelines
- **ProtoOS Store Documentation**: `client/src/protoOS/store/README.md` - Comprehensive state management guide

Each subdirectory contains additional component-specific documentation and README files.
