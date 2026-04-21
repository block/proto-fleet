# Proto Fleet Python SDK

**Version:** 1.0.0

Python SDK for building Proto Fleet mining device plugins. This SDK enables developers to create plugins in Python that integrate seamlessly with the Proto Fleet management system.

## Features

- **Native Python Architecture** - Uses asyncio and modern Python patterns (no HashiCorp go-plugin)
- **gRPC Communication** - Direct gRPC server implementation for Fleet ↔ Plugin communication
- **Full Type Safety** - Python 3.11+ type hints compatible with mypy strict mode
- **Async/Await Throughout** - All I/O operations use asyncio for efficient concurrent device management
- **Immutable Data Structures** - Frozen dataclasses ensure thread-safe state management
- **Protocol-Based Interfaces** - Structural typing using `typing.Protocol` for flexible implementations
- **Rich Telemetry Model** - Hierarchical component metrics with statistical metadata
- **Standardized Error Taxonomy** - 50+ predefined error codes with severity classification

## Requirements

- Python 3.11 or higher
- grpcio >= 1.60.0
- grpcio-tools >= 1.60.0
- protobuf >= 4.25.0

## Installation

### From Source

```bash
cd server/sdk/v1/python
pip install -e .
```

### With Development Dependencies

```bash
pip install -e ".[dev]"
```

Local repo-managed setup defaults to ignoring machine-global pip config. In CI, ambient pip config is honored unless you explicitly set `PIP_CONFIG_FILE` or `PIP_INDEX_URL`.

## Quick Start

Here's a minimal plugin implementation:

```python
import grpc
from proto_fleet_sdk import DriverIdentifier, Capabilities
from proto_fleet_sdk.server import PluginServer

class MyDriver:
    async def handshake(self, ctx: grpc.ServicerContext) -> DriverIdentifier:
        return DriverIdentifier(driver_name="my-plugin", api_version="v1")

    async def describe_driver(self, ctx: grpc.ServicerContext):
        ident = DriverIdentifier(driver_name="my-plugin", api_version="v1")
        caps: Capabilities = {
            "discover_device": True,
            "pair_device": True,
            "device_status": True,
        }
        return ident, caps

    # ... implement other required methods

if __name__ == "__main__":
    driver = MyDriver()
    server = PluginServer(driver, port=50051)
    server.run()
```

## Architecture

The SDK provides:

1. **Type Definitions** - Immutable dataclasses for device info, telemetry, errors, etc.
2. **Protocol Interfaces** - `Driver` and `Device` protocols defining required methods
3. **gRPC Server** - Plugin server that handles communication with Fleet
4. **Type Conversions** - Automatic conversion between protobuf and Python types
5. **Error Handling** - Standardized exceptions mapped to gRPC status codes

Your plugin implements the `Driver` protocol:

```python
class Driver(Protocol):
    async def handshake(self, ctx: grpc.ServicerContext) -> DriverIdentifier: ...
    async def describe_driver(self, ctx: grpc.ServicerContext) -> tuple[DriverIdentifier, Capabilities]: ...
    async def discover_device(self, ctx: grpc.ServicerContext, ip: str, port: int) -> DeviceInfo: ...
    async def pair_device(self, ctx: grpc.ServicerContext, info: DeviceInfo, secret: SecretBundle) -> DeviceInfo: ...
    async def new_device(self, ctx: grpc.ServicerContext, id: str, info: DeviceInfo, secret: SecretBundle) -> NewDeviceResult: ...
```

And creates `Device` instances that implement device-level operations:

```python
class Device(Protocol):
    @property
    def id(self) -> str: ...
    async def status(self, ctx: grpc.ServicerContext) -> DeviceMetrics: ...
    async def start_mining(self, ctx: grpc.ServicerContext) -> None: ...
    async def stop_mining(self, ctx: grpc.ServicerContext) -> None: ...
    # ... other control and configuration methods
```

## API Overview

### Core Types

- **DriverIdentifier** - Plugin identification (name, API version)
- **DeviceInfo** - Device connection and identification info
- **SecretBundle** - Authentication credentials (API key, username/password, bearer token, TLS cert)
- **Capabilities** - Feature flags indicating supported operations
- **grpc.ServicerContext** - gRPC request context (passed through from the framework)

### Telemetry Model

- **DeviceMetrics** - Complete device telemetry snapshot
- **MetricValue** - Single metric with value, kind, and optional metadata
- **Component Metrics** - HashBoard, ASIC, PSU, Fan, ControlBoard, Sensor
- **Health Status** - Device and component health classification

### Error Handling

All SDK exceptions inherit from `SDKError`:

- **UnsupportedCapabilityError** - Requested feature not supported
- **DeviceNotFoundError** - Device not found or unreachable
- **InvalidConfigError** - Configuration is invalid
- **DeviceUnavailableError** - Device temporarily unavailable
- **AuthenticationFailedError** - Authentication failed
- **DriverShutdownError** - Driver is shutting down

### Utilities

- **parse_port()** - Parse and validate port strings
- **ths_to_hs() / hs_to_ths()** - Hashrate unit conversions
- **jth_to_jh() / jh_to_jth()** - Power efficiency unit conversions
- **safe_uint_to_int32()** - Safe integer conversions

## Development

### Type Checking

```bash
mypy proto_fleet_sdk/ --strict
```

### Linting

```bash
ruff check proto_fleet_sdk/
ruff format proto_fleet_sdk/
```

### Testing

```bash
pytest tests/ -v --cov=proto_fleet_sdk
```

## Documentation

- **Protobuf Definitions**: See `../pb/driver.proto` for gRPC service definitions

## Project Structure

```
proto_fleet_sdk/
├── __init__.py              # Package initialization
├── types.py                 # Core data types
├── enums.py                 # Enumerations
├── capabilities.py          # Capability constants
├── auth.py                  # Authentication types
├── errors.py                # Exception hierarchy
├── error_codes.py           # Miner error taxonomy
├── protocols/               # Protocol interfaces
│   ├── driver.py           # Driver protocol
│   └── device.py           # Device protocols
├── telemetry/              # Telemetry model
│   ├── metrics.py          # Metric types
│   ├── components.py       # Component metrics
│   └── converters.py       # Unit conversions
├── utils/                  # Utility functions
│   ├── port_utils.py
│   ├── type_converters.py
│   └── validation.py
├── servicer.py             # gRPC servicer adapter
├── server.py               # Plugin server
└── generated/              # Generated protobuf code
    ├── driver_pb2.py
    └── driver_pb2_grpc.py
```

## License

MIT License

## Contributing

See the main Proto Fleet repository for contribution guidelines.

## Support

For questions, issues, or feedback:
- GitHub Issues: [proto-fleet/issues](https://github.com/proto-at-block/proto-fleet/issues)
