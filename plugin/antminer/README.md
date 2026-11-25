# Antminer Plugin

Fleet SDK v1 plugin for Bitmain Antminer devices.

## Features

### Supported Operations
- Device discovery via RPC port 4028
- Username/password authentication
- Status monitoring via RPC API
- Telemetry collection (hashrate, temperature, power, uptime)
- Web interface access
- Mining start/stop, reboot, pool configuration, password update, log download, and file-based firmware upload

### Limitations
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
└── pkg/
    └── antminer/         # Client implementation
        ├── rpc/          # RPC API client
        ├── web/          # Web API client and types
        └── networking/   # Network helpers
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
go test ./...
```
