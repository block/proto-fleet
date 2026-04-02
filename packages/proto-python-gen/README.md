# proto-python-gen

A protoc plugin for generating Python gRPC stubs, designed for use with Buf.

## How it works

`protoc-gen-python-grpc` is a proper protoc plugin that follows the standard protocol:
reads a `CodeGeneratorRequest` from stdin, generates `*_pb2_grpc.py` files using
`grpc_tools.protoc`, and writes a `CodeGeneratorResponse` to stdout.

Use it alongside buf's built-in `python` and `pyi` plugins for complete Python code generation:

| Plugin | Generates |
|---|---|
| `protoc_builtin: python` | `*_pb2.py` (messages) |
| `protoc_builtin: pyi` | `*_pb2.pyi` (type stubs) |
| `local: protoc-gen-python-grpc` | `*_pb2_grpc.py` (services) + `__init__.py` |

## Prerequisites

- `buf` on PATH (installed via Hermit)
- Python 3.x available (for venv creation)

## Setup

```bash
cd packages/proto-python-gen
just setup   # creates .venv with pinned grpcio-tools and protobuf
```

Local repo-managed setup defaults to ignoring machine-global pip config. In CI, ambient pip config is honored unless you explicitly set `PIP_CONFIG_FILE` or `PIP_INDEX_URL`.

## Usage

Add to your `buf.gen.yaml`:

```yaml
version: v2
plugins:
  - protoc_builtin: python
    out: gen/python
  - protoc_builtin: pyi
    out: gen/python
  - local: protoc-gen-python-grpc
    out: gen/python
```

Then run:

```bash
buf generate
```

### Plugin options

Options are passed via the `opt` field in `buf.gen.yaml`:

| Option | Default | Description |
|---|---|---|
| `import_prefix=<pkg>` | — | Rewrite `_pb2_grpc.py` imports to use package-qualified paths |
| `init_files=false` | `true` | Disable `__init__.py` generation |

Example with options:

```yaml
- local: protoc-gen-python-grpc
  out: gen/python
  opt:
    - import_prefix=my_package.generated
```

## Development

```bash
just setup    # Bootstrap venv
just test     # Run integration tests
just package  # Build distributable tarball
just clean    # Remove all generated artifacts
```

## Hermit installation

```bash
cd packages/proto-python-gen
just package
hermit install proto-python-gen
```

After installation, `protoc-gen-python-grpc` is available on PATH via Hermit.

## Pinned versions

| Tool | Version |
|---|---|
| grpcio-tools | 1.76.0 |
| protobuf | 6.33.5 |
