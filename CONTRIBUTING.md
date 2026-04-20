# Contributing to Proto Fleet

Thank you for your interest in contributing to Proto Fleet! This guide covers the development workflows and conventions used in this project.

## Reporting Issues

We use GitHub issue templates to keep reports consistent and actionable. When you open a new issue you will see an issue chooser with these options:

- **Bug Report** — something is broken or behaving unexpectedly
- **Feature Request** — suggest a new feature or improvement
- **Miner Compatibility Issue** — report a problem with a specific miner model or firmware version

If you have a question or want to start a discussion, head to [GitHub Discussions](https://github.com/block/proto-fleet/discussions) instead. For security vulnerabilities, follow the process described in [SECURITY.md](SECURITY.md).

## Development Setup

Start with the [README](README.md) for the basic development flow, then use the details here for contributor-specific setup requirements.

### Hermit Setup

If you use Hermit, activate the managed toolchain and install project dependencies:

```bash
source ./bin/activate-hermit
just setup
```

After your toolchain is ready, install Git hooks:

```bash
just install-hooks
```

### Non-Hermit Setup

If you are not using Hermit, install the required toolchain yourself before running project tasks. The repository recipes and hooks expect these binaries to be available in `PATH` as needed:

- `just` for top-level and per-project task runners
- `go` for server and Go plugin workflows
- `node` and `npm` for client setup, linting, testing, and Storybook
- `buf` for protobuf linting and generation
- `lefthook` for Git hook installation
- `golangci-lint` for Go linting and pre-push checks
- `goimports` for Go formatting and code generation follow-up
- `sqlc` for generating server query bindings
- `migrate` for creating and running database migrations

Python-specific tooling depends on the files you change:

- `packages/proto-python-gen`: `cd packages/proto-python-gen && just setup-dev`
- `server/sdk/v1/python`: `cd server/sdk/v1/python && just setup`
- `plugin/example-python` and other Python paths: install `ruff` in `PATH`, or set `PROTO_FLEET_RUFF=/path/to/ruff`

## Git Hooks

Install Git hooks with:

```bash
just install-hooks
```

If `lefthook` is not installed, `just install-hooks` will fail. Hermit users can run `source ./bin/activate-hermit` first to make `lefthook` available. Non-Hermit users need to install `lefthook` manually, then rerun `just install-hooks`.

### Python Hook Prerequisites

The pre-commit hooks run Ruff for staged Python files. Make sure the relevant Ruff environment is available before committing Python changes:

- `packages/proto-python-gen`: `cd packages/proto-python-gen && just setup-dev`
- `server/sdk/v1/python`: `cd server/sdk/v1/python && just setup`
- `plugin/example-python`: install `ruff` in `PATH`, or set `PROTO_FLEET_RUFF=/path/to/ruff`
- Other Python paths: install `ruff` in `PATH`, or set `PROTO_FLEET_RUFF=/path/to/ruff`

### Pre-Push Checks

The pre-push hooks also run repository checks before a branch can be pushed:

- `client`: TypeScript typechecking via `npm exec --no -- tsc --noEmit`
- `server`: `golangci-lint run -c .golangci.yaml`
- `plugin/proto`: `golangci-lint run -c .golangci.yaml`
- `plugin/antminer`: `golangci-lint run -c .golangci.yaml`

## Git Workflow

### Branch Naming

Create feature branches with descriptive names:

```bash
git checkout -b <username>/short-description
```

### Commit Messages

Follow [conventional commit](https://www.conventionalcommits.org/) format:

```bash
git commit -m "feat: add telemetry streaming to fleet UI

- Implement server-to-client streaming connection
- Add telemetry slice to fleet store
- Update MinerList to display live metrics"
```

Prefixes:

- `feat:` — New feature
- `fix:` — Bug fix
- `refactor:` — Code refactoring
- `docs:` — Documentation changes
- `test:` — Test additions or updates
- `chore:` — Build/tooling changes

### Pull Requests

Create PRs with a clear summary and test plan:

```bash
gh pr create --title "Brief description" --body "## Summary
- Bullet point summary of changes

## Test Plan
- How to verify the changes work"
```

## Cross-Component Workflows

### Adding a New API Endpoint

1. Define the API in the appropriate `.proto` file in `proto/`
2. Run `just gen` to regenerate TypeScript and Go code
3. Implement the server handler in `server/internal/handlers/`
4. Register the handler in `server/cmd/fleetd/main.go`
5. Create a client hook in `client/src/{app}/api/`
6. Update the Zustand store slice to consume the data
7. Commit proto definitions and all generated code together

### Making Database Schema Changes

1. Create a migration: `cd server && just db-migration-new <name>`
2. Write both up and down migrations in `server/migrations/`
3. Run `just gen` to regenerate sqlc bindings
4. Update queries in `server/sqlc/queries/` if needed
5. **Never modify existing migrations after they have been deployed**

### Adding Features to the Client

1. Determine the target app: ProtoOS, ProtoFleet, or shared
2. Check `client/src/shared/components/` for existing reusable components
3. Place the feature in the appropriate `client/src/{app}/features/` directory
4. Create Storybook stories for new components
5. Write tests with Vitest and Testing Library

### Adding Business Logic to the Server

1. Add domain logic to the appropriate package in `internal/domain/`
2. Create a gRPC handler in `internal/handlers/`
3. Add tests for domain logic and handlers
4. Update stores in `internal/domain/stores/sqlstores/` if database access is needed

## Code Generation

All generated code must be committed to Git. Run `just gen` after:

- Modifying protobuf definitions in `proto/`
- Changing database migrations in `server/migrations/`
- Adding or modifying sqlc queries in `server/sqlc/queries/`

Never manually edit generated files in:

- `client/src/protoOS/api/generatedApi.ts`
- `client/src/protoFleet/api/generated/`
- `server/generated/`

## Component Boundaries

Maintain strict separation between applications:

- Code in `client/src/shared/` must not import from ProtoOS or ProtoFleet
- ProtoOS and ProtoFleet must not import from each other
- Server code is completely independent of client code

This ensures applications remain decoupled and shared code stays truly reusable.

## Go Workspace

The repository uses a Go workspace (`go.work`) for integrated development:

- All Go modules (server and plugins) are included in the workspace
- Changes across modules are immediately available without version bumps
- Both `go.work` and `go.work.sum` are committed to Git for reproducible builds
- Run `go work sync` after updating dependencies

## Testing

### Client

```bash
cd client
npm test                          # Run all tests
npx vitest run <pattern>          # Run tests matching a pattern
npx vitest watch <pattern>        # Watch mode for a specific file
npm run storybook                 # Visual component testing
```

### Server

```bash
cd server
just test                         # Run all tests
just lint                         # Lint code
go test ./internal/domain/pairing -v              # Test a specific package
go test ./internal/domain/pairing -v -run TestName  # Run a specific test
```

### E2E Tests

```bash
cd server
go test -tags=e2e ./e2e           # Run e2e tests (requires docker-compose)
```

See `server/e2e/README.md` for the full e2e testing guide.
