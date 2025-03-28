# Fleet Service

Fleet is a Go-based service that provides a web interface and API endpoints for managing the miner fleet.
The service uses a SQL database for data persistence and exposes grpc, gRPC-web and HTTP API endpoints.

## Features

- gRPC-web API endpoints for:
  - Greeting service
  - Authors service
- SQL database integration with migrations
- Configurable through environment variables and command-line flags

## Configuration

The service can be configured using environment variables or command-line flags, see `internal/domain/config.go`.

## Development

### Error wrapping

~~This project uses [errtrace](https://github.com/bracesdev/errtrace) for enhancing errors with stack traces.~~

### Database Migrations

The service automatically runs database migrations on startup.
Migration files are managed using [golang-migrate](https://github.com/golang-migrate).
Migrations are located in `internal/db/migrations`.

### Code Generation

All code generation can be done by running `just gen`.
Generated files are located in the `generated` directory.
All generated code should be checked in to Git following Go best practices.

#### SQL Models and Queries

The service uses [sqlc](https://docs.sqlc.dev/en/stable/tutorials/getting-started-mysql.html) to generate Go bindings for models and queries without going as far as using an ORM.

Models are generated from database schema migrations in `internal/db/migrations`.
Queries are generated from annotated SQL queries in `internal/db/queries`.
Refer to sqlc documentation for details on how to use.

To regenerate the bindings, run `just gen-db-queries` (or just `just gen`).

#### Protobuf and gRPC

This service uses [Go Protobuf](https://protobuf.dev/getting-started/gotutorial/) and [Connect RPC](https://connectrpc.com/docs/go/getting-started/), both generated using [Buf](https://buf.build/docs/cli/).
Protobuf provides type-safe interface descriptions (IDL) generated across languages.
Connect RPC is a multi-protocol implementation of RPC that supports gRPC and ConnectRPC.
We choose ConnectRPC because it's completely gRPC compatible, and is a more modern implementation that is built on top of the Go standard library's h2 server.

To regenerate the bindings, run `buf generate` (or just `just gen`).

### API Development

The service uses [Connect](https://connectrpc.com/docs/go/getting-started) for API endpoints.
The gRPC API definitions can be found in the `proto` directory.

## Running the Service

1. Start MySQL

```shell
just db-up
```

2. Build and run the service

```shell
go install ./cmd/fleetd && fleetd
```

The service will:

1. Connect to the database
2. Run any pending migrations
3. Start serving the API on the configured address (default: http://127.0.0.1:8080)

## Interacting with the service

### HTTP API

The service responds to both gRPC requests and HTTP requests. To interact via HTTP

**Add an author**

```
curl \
--header "Content-Type: application/json" \
--data '{"name":"Stephen King", "bio":"horror"}' \
http://localhost:8080/authors.v1.AuthorsService/Add
```

**List authors**

```
curl \
--header "Content-Type: application/json" \
--data '{}' \
http://localhost:8080/authors.v1.AuthorsService/List
```

### gRPC API

To interact using the gRPC API

```
grpcurl \
-protoset <(buf build -o -) -plaintext \
-d '{"name": "Jane"}' \
localhost:8080 greet.v1.GreetService/Greet
{
  "greeting": "Hello, Jane!"
}
```
