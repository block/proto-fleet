# Antminer Plugin

Fleet SDK v1 plugin for Bitmain Antminer devices.

## Features

### Supported Operations
- Device discovery via RPC port 4028
- Username/password authentication
- Status monitoring via RPC API
- Telemetry collection (hashrate, temperature, power, uptime)
- Web interface access

### Limitations
- Mining control requires web API implementation
- Firmware updates not implemented
- Pool configuration requires web API implementation
- No streaming support
- No batch operations

## Architecture

```
plugin/antminer/
├── main.go                 # Plugin entry point
├── internal/
│   ├── types/             # Shared types
│   ├── driver/            # Driver implementation
│   └── device/            # Device implementation
├── pkg/
│   ├── antminer/         # Client and RPC implementation
│   │   └── web/          # Web API types
│   └── auth/             # Authentication service
└── tests/                # Integration tests
```

## Usage

### Build
```bash
go build -o antminer .
```

### Credentials
Plugin requires `sdk.UsernamePassword` credentials for device pairing.

### Discovery
Connects to port 4028 and issues RPC `version` command to identify Antminer devices.

## RPC Commands
- `version` - Device information
- `summary` - Mining statistics  
- `devs` - ASIC device data
- `pools` - Pool configuration

## Status Mapping
| Antminer State | SDK Health | Description |
|----------------|------------|-------------|
| Mining + hashrate | HealthyActive | Normal operation |
| Mining + no hashrate | Warning | Performance issue |
| Hardware errors | Warning | Hardware problems |
| RPC error | Critical | Communication failure |

## Testing
```bash
# Unit tests
go test ./internal/... ./pkg/...

# Integration tests  
go test ./tests/...
```
