# GitHub Copilot Instructions

## Project Overview

This is a monorepo for a miner management system with the following structure:

- **Client**: TypeScript, React, and Tailwind CSS applications
- **Server**: Go-based backend service
- **Miner Firmware**: Rust-based firmware for mining hardware

## General Guidelines

### What NOT to Review or Modify

- Auto-generated code in directories like:
  - `generated/`, `gen/`, `.generated/`
  - `build/`, `dist/`, `target/`, `out/`
  - `node_modules/`, `vendor/`
  - `*.pb.go`, `*.pb.ts`, `*.generated.*`
  - Build artifacts and compiled binaries
  - Package lock files (package-lock.json, yarn.lock, go.sum, etc.)

### Code Review Focus

- Focus review on meaningful code changes
- Check for potential security vulnerabilities
- Ensure proper error handling is implemented
- Verify that new functions include appropriate unit tests
- Review for performance optimizations
- Note any potential accessibility issues

## Server (Go Backend)

### Tech Stack

- Go
- Connect RPC (gRPC-compatible) for fleet API endpoints
- PostgreSQL/TimescaleDB with golang-migrate for migrations
- sqlc for type-safe SQL query generation
- Protobuf for fleet API definitions
- Docker for containerization

### Key Directories

- `cmd/`: Entry points for the service
- `internal/`: Core business logic and domain models
- `generated/`: Auto-generated code (sqlc, fleet gRPC)
- `migrations/`: Database migration files
- `sqlc/`: SQL query definitions for code generation
- `sdk/`: Language SDKs and shared generated client surfaces
- `plugin/`: Device driver plugins and plugin SDK consumers

### Server Development Guidelines

- Ensure proper error handling with context-aware error messages
- Check that all database queries use prepared statements to prevent SQL injection
- Verify that context is properly propagated through all function calls
- Ensure proper use of goroutines and channels with no race conditions
- Check for proper resource cleanup (defer statements for closing connections, etc.)
- Verify that API endpoints have proper authentication and authorization
- Ensure sensitive data is not logged or exposed in error messages
- Check that database transactions are properly committed or rolled back
- Ensure proper validation of all user inputs at API boundaries
- Verify that migrations are backward compatible and include rollback logic
- Ensure proper use of interfaces for testability and dependency injection
- Check that configuration values are validated and have sensible defaults
- Ensure proper handling of concurrent requests and data races
- Check that API responses follow consistent error formats
- Verify that all public functions have appropriate documentation
- Ensure that unit tests cover critical business logic
- Check for proper use of context cancellation and deadlines

## Client (React Applications)

### Applications

- **ProtoOS**: Mining dashboard UI served by the miner-api-server
- **ProtoFleet**: Fleet management UI for managing multiple miners

### Tech Stack

- TypeScript with React
- Vite for build tooling
- Tailwind CSS for styling
- Recharts for data visualization
- Storybook for component development
- ESLint for code quality
- PostCSS for CSS processing

### Key Directories

- `src/protoOS/`: Mining dashboard application
- `src/protoFleet/`: Fleet management application
- `src/shared/`: Shared components, utilities, and types
- `dist/`: Production builds (auto-generated)

### API Integration

- ProtoOS uses auto-generated TypeScript types from `src/protoOS/api/types.ts` (this file is auto-generated and should **not** be reviewed or modified; it is referenced here for context only)
- ProtoFleet connects to the Go backend service via gRPC-web
- Proto plugin communicates with miners via REST API (MDK-API OpenAPI spec)
- Development proxies configured in vite.config.ts

### Client Development Guidelines

- Ensure all React components follow functional component patterns with hooks
- Check that TypeScript types are properly defined and avoid using 'any'
- Verify Tailwind classes are used consistently and follow the design system
- Ensure proper error boundaries are implemented for critical UI sections
- Verify that API calls include proper error handling and loading states
- Ensure components are properly memoized where performance is critical
- Check that shared components are truly reusable between both apps
- Verify that environment variables are properly typed and validated
- Ensure proper cleanup in useEffect hooks to prevent memory leaks
- Check for proper form validation and user input sanitization
- Verify that responsive design is implemented for all screen sizes
- Ensure proper code splitting and lazy loading for optimal performance
- Check that Storybook stories exist for new components
- Verify that console.log statements are removed from production code. console.error statements are fine.

## Pull Request Guidelines

When reviewing or creating pull requests:

1. Review the high level architecture.
2. Focus on the specific changes being made, not unrelated code
3. Ensure all tests pass before submitting
4. Include relevant documentation updates
5. Follow the project's commit message conventions

## Development Best Practices

### General

- Write clean, self-documenting code
- Add comments only for complex business logic or non-obvious implementations
- Follow the existing code style and conventions
- Ensure proper error handling throughout
- Write comprehensive tests for new functionality
- Keep functions small and focused on a single responsibility

### Security

- Never hardcode credentials or sensitive information
- Validate all user inputs
- Use prepared statements for database queries
- Implement proper authentication and authorization
- Avoid logging sensitive data
- Use HTTPS/TLS for all network communications

### Performance

- Profile before optimizing
- Use appropriate data structures
- Implement proper caching strategies
- Avoid premature optimization
- Consider concurrent request handling
- Monitor resource usage

## Project-Specific Notes

Each project in this monorepo may have their own README.md files which contain additional relevant information. Project-specific configurations should be treated as additions to these instructions.
