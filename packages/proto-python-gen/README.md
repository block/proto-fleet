# proto-python-gen

A Hermit package for generating Python gRPC and protobuf stubs using Buf.

## How it works

1. `buf export` flattens your protos and their dependencies into a temp directory
2. `grpc_tools.protoc` generates `*_pb2.py` (messages) and `*_pb2_grpc.py` (services) in one pass
3. `__init__.py` files are created in every generated directory

This approach uses Buf for dependency resolution and proto management while using the standard Python gRPC toolchain for code generation.

## Prerequisites

- `buf` on PATH (installed via Hermit)
- Python 3.x available (for venv creation)

## Setup

```bash
cd packages/proto-python-gen
just setup   # creates .venv with pinned grpcio-tools and protobuf
```

## Usage

```bash
# Generate from any buf-configured proto source
buf-gen-python --output <dir> [--buf-config <dir>] [--path <subdir>] [--clean]
```

### Arguments

| Flag | Required | Default | Description |
|---|---|---|---|
| `--output <dir>` | Yes | — | Output directory for generated Python files |
| `--buf-config <dir>` | No | `.` | Directory containing `buf.yaml` |
| `--path <subdir>` | No | — | Only export protos under this subdirectory |
| `--clean` | No | — | Remove output directory before generating |
| `--help` | No | — | Show help message |

### Examples

```bash
# Generate from protos in current directory
buf-gen-python --output gen/python

# Generate from a specific module
buf-gen-python --output gen/python --buf-config server --path sdk/v1

# Clean and regenerate
buf-gen-python --output gen/python --clean
```

### Environment variables

| Variable | Description |
|---|---|
| `PROTO_PYTHON_GEN_ROOT` | Override package root directory |
| `PROTO_PYTHON_GEN_VENV` | Override Python venv path |

## Development

```bash
just setup    # Bootstrap venv
just test     # Run integration tests
just demo     # Generate stubs from plugin SDK proto
just package  # Build distributable tarball
just clean    # Remove all generated artifacts
```

## Hermit installation

```bash
# Build the package
cd packages/proto-python-gen
just package

# Install via Hermit
hermit install proto-python-gen
```

After installation, `buf-gen-python` is available on PATH via Hermit.

## Pinned versions

| Tool | Version |
|---|---|
| grpcio-tools | 1.76.0 |
| protobuf | 6.33.5 |
