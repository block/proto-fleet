# Fake Proto Rig

A simulator for Proto Bitcoin mining devices, implementing the same gRPC/Connect-RPC interfaces as real Proto miners.

## Overview

This simulator allows the fleet management system to be tested without physical hardware. It implements:

- **MinerDataApi** - 14 methods for telemetry and status
- **MinerCommandApi** - 9 methods for controlling mining operations
- **MinerSystemApi** - 9 methods for system operations
- **MinerPairingApi** - 3 methods for device pairing (no auth required)

## Features

- Stateful simulation of mining state, pools, and configuration
- Realistic telemetry data with random variation
- Authentication via Bearer tokens (when auth key is set)
- Error injection via environment variables
- HTTP/2 cleartext (h2c) support for gRPC communication

## Usage

### Running Directly

```bash
go run .
```

### Running with Docker

```bash
docker build -t fake-proto-rig -f Dockerfile ../..
docker run -p 2121:2121 fake-proto-rig
```

### Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `GRPC_PORT` | Port to listen on | `2121` |
| `SERIAL_NUMBER` | Device serial number | `PROTO-SIM-<uuid>` |
| `MAC_ADDRESS` | Device MAC address | Generated from instance ID |

### Error Injection

Inject errors for testing error handling:

| Variable | Description | Example |
|----------|-------------|---------|
| `ERROR_TEMPERATURE` | Override temperature reading (°C) | `95.0` |
| `ERROR_HASHBOARD_MISSING` | Comma-separated list of missing hashboard indices | `0,2` |
| `ERROR_HASHBOARD_ERROR` | Comma-separated list of hashboards in error state | `1` |
| `ERROR_PSU_MISSING` | Comma-separated list of missing PSU indices | `0` |
| `ERROR_PSU_ERROR` | Comma-separated list of PSUs in error state | `1` |
| `ERROR_POOLS_OFFLINE` | Simulate all pools being offline | `true` |

### Example: Simulating Hardware Issues

```bash
# Run with one hashboard missing and high temperature
docker run -p 2121:2121 \
  -e ERROR_HASHBOARD_MISSING=2 \
  -e ERROR_TEMPERATURE=92.5 \
  fake-proto-rig
```

## API Endpoints

### Health Check

```bash
curl http://localhost:2121/health
# Returns: OK
```

### gRPC Services

All services are available at `http://localhost:2121`:

- `miner_data_api.MinerDataApi`
- `miner_command_api.MinerCommandApi`
- `miner_system_api.MinerSystemApi`
- `miner_system_api.MinerPairingApi`

### Testing with grpcurl

```bash
# Get pairing info (no auth required)
grpcurl -plaintext localhost:2121 miner_system_api.MinerPairingApi/GetPairingInfo

# Get mining status (requires auth if key is set)
grpcurl -plaintext \
  -H "Authorization: Bearer <token>" \
  localhost:2121 miner_data_api.MinerDataApi/GetMiningStatus
```

## Default Values

The simulator uses realistic default values for a Proto B4 miner:

| Metric | Default Value |
|--------|---------------|
| Total Hashrate | 140 TH/s |
| Power Consumption | 3400 W |
| Efficiency | 24.3 J/TH |
| Temperature | 55°C |
| Hashboards | 4 |
| ASICs per Hashboard | 120 |
| PSUs | 2 |
| Fans | 4 |

## Architecture

```
fake-proto-rig/
├── main.go                 # Entry point, server setup, auth interceptor
├── models.go               # MinerState and configuration structs
├── data_api_handler.go     # MinerDataApi implementation (14 methods)
├── command_api_handler.go  # MinerCommandApi implementation (9 methods)
├── system_api_handler.go   # MinerSystemApi & MinerPairingApi (12 methods)
├── Dockerfile              # Docker build configuration
└── README.md               # This file
```
