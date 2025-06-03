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

#### Creating new migration files

Migration files are generated with sequential prefix. Instead of manualy creating the sequence and up/down migration files you can run the following command. Replace `<migration_name>` with the name of your migration e.g. `create_signals_table`

```
just new-migration <migration_name>
```

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

```shell
just run
```

This will:

1. Connect to the database
2. Run any pending migrations
3. Start serving the API on the configured address (default: http://127.0.0.1:8080)

## Running the Service via Docker

```shell
just dev
```

This will:

1. Connect to the database
2. Run any pending migrations
3. Run the server in docker compose with watch enabled on the fleet service
4. Start serving the API on the configured address (default: http://localhost:4000)

## Interacting with the service

### HTTP API

The service responds to both gRPC requests and HTTP requests. To interact via HTTP see [testing.http](testing.http) NB: You can make requests from this file directly if you are using [GoLand](https://blog.jetbrains.com/idea/2021/10/intellij-idea-2021-3-eap-6-enhanced-http-client-kotlin-support-for-cdi-and-more/#:~:text=Like%20in%20ordinary%20HTTP%20requests,proto%20files.) or the [Rest Client](https://marketplace.visualstudio.com/items?itemName=humao.rest-client) vscode extension.
