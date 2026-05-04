# Fake Proto Rig

A simulator for Proto Bitcoin mining devices, implementing the REST API interface.

## Overview

This simulator allows the fleet management system to be tested without physical hardware. It implements:

**REST API** (~45 endpoints matching the MDK OpenAPI spec):
- Pools, mining, system, hardware, telemetry, cooling, network, auth, and pairing endpoints

## Features

- Stateful simulation of mining state, pools, and configuration
- Realistic telemetry data with random variation
- Authentication: protected REST endpoints require a Bearer token (access token from `/api/v1/auth/login` or a paired EdDSA JWT), with a small public surface for login, password setup, system status, network info, and pairing discovery
- Error injection via environment variables
- REST API for both ProtoFleet plugin and ProtoOS dashboard

## Usage

### Running Directly

```bash
go run .
```

### Running with Docker

```bash
docker build -t fake-proto-rig -f Dockerfile ../..
docker run -p 8080:8080 fake-proto-rig
```

### Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `HTTP_PORT` | Port to listen on | `8080` |
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
docker run -p 8080:8080 \
  -e ERROR_HASHBOARD_MISSING=2 \
  -e ERROR_TEMPERATURE=92.5 \
  fake-proto-rig
```

## API Endpoints

### Health Check

```bash
curl http://localhost:8080/health
# Returns: OK
```

### REST API

The REST API implements endpoints matching the MDK OpenAPI spec:

```bash
# System info
curl http://localhost:8080/api/v1/system

# System status
curl http://localhost:8080/api/v1/system/status

# Network info
curl http://localhost:8080/api/v1/network

# Pairing info
curl http://localhost:8080/api/v1/pairing/info
```

Most data and configuration endpoints require a bearer token:

```bash
# Login
TOKEN=$(curl -s http://localhost:8080/api/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{"password":"your-password"}' | jq -r '.access_token')

# Mining status
curl http://localhost:8080/api/v1/mining \
  -H "Authorization: Bearer ${TOKEN}"

# Pool configuration
curl http://localhost:8080/api/v1/pools \
  -H "Authorization: Bearer ${TOKEN}"

# Hardware info
curl http://localhost:8080/api/v1/hardware \
  -H "Authorization: Bearer ${TOKEN}"

# Telemetry data
curl "http://localhost:8080/api/v1/telemetry?level=miner" \
  -H "Authorization: Bearer ${TOKEN}"
```

**Available REST endpoints:**
- `/api/v1/pools` - Pool configuration (GET, POST)
- `/api/v1/pools/{id}` - Individual pool (GET, PUT, DELETE)
- `/api/v1/mining` - Mining status (GET)
- `/api/v1/mining/target` - Power target (GET, PUT)
- `/api/v1/mining/start`, `/api/v1/mining/stop` - Mining control (POST)
- `/api/v1/system` - System information (GET)
- `/api/v1/hardware` - Hardware info (GET)
- `/api/v1/hashboards` - Hashboard stats (GET)
- `/api/v1/cooling` - Cooling status and control (GET, PUT)
- `/api/v1/network` - Network configuration (GET, PUT)
- `/api/v1/telemetry` - Telemetry data (GET)
- `/api/v1/auth/*` - Authentication endpoints
- `/api/v1/pairing/info` - Pairing info (GET)
- `/api/v1/pairing/auth-key` - Auth key management (POST, DELETE)

## Default Values

The simulator uses realistic default values for a Proto Rig miner:

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
├── main.go                 # Entry point, HTTP server setup
├── models.go               # MinerState and configuration structs
├── rest_api_handler.go     # REST API implementation (~45 endpoints)
├── rest_api_handler_test.go # Tests
├── Dockerfile              # Docker build configuration
└── README.md               # This file
```

## Maintenance

### Updating the REST API

The REST API is manually implemented based on the OpenAPI spec at `proto-rig-api/openapi/MDK-API.json`. When the spec is updated:

1. **Compare changes** to identify new/modified endpoints:
   ```bash
   git diff proto-rig-api/openapi/MDK-API.json
   ```

2. **Update `rest_api_handler.go`**:
   - Add new endpoints to `RegisterRoutes()`
   - Add corresponding handler functions
   - Update JSON struct types if response shapes changed

3. **Test the changes**:
   ```bash
   # Build and run
   cd server/fake-proto-rig && GOWORK=off go build .

   # Or rebuild Docker
   cd server && docker compose build proto-sim && docker compose up proto-sim -d

   # Test endpoints
   curl http://localhost:8080/api/v1/<endpoint>
   ```

### OpenAPI Compliance Checklist

When implementing or updating endpoints, verify these common patterns from the OpenAPI spec:

| Pattern | Correct | Incorrect |
|---------|---------|-----------|
| **Slot numbering** | 1-based (`"slot": 1, 2, 3`) | 0-based (`"slot": 0, 1, 2`) |
| **Status field name** | `"status": "Mining"` | `"state": "Mining"` |
| **Response wrappers** | Check if response uses wrapper (e.g., `"mining-status": {...}`) | Flat response when wrapper expected |
| **PSU serial field** | `"psu_sn"` | `"serial_number"` |
| **PSU firmware** | Nested object: `"firmware": {"app_version": "...", "bootloader_version": "..."}` | Flat string: `"firmware_version": "..."` |
| **Hashboards list wrapper** | `"hashboards-info": [...]` | `"hashboards": [...]` |
| **PSUs list wrapper** | `"psus-info": [...]` | `"psus": [...]` |
| **Hashboard status enum** | `"Running"`, `"Stopped"`, `"Error"`, `"Overheated"`, `"Unknown"` | `"Mining"`, `"Off"` |
| **Mining status enum** | `"PoweringOn"`, `"Uninitialized"` | `"Starting"`, `"Unknown"` |
| **PerformanceMode enum** | `"MaximumHashrate"`, `"Efficiency"` | `"MaximumEfficiency"` |
| **Port field** | `json:"port"` (0 is valid) | `json:"port,omitempty"` (omits 0) |
| **TimeSeriesRequest** | Validate `start_time` and `levels` are required (return 422) | Accept missing required fields |

### Cross-Reference with miner-firmware

The reference implementation is in the private `miner-firmware` repository at:
- `crates/miner-api-server/src/models/` - Rust struct definitions with `#[serde(rename = "...")]`
- `crates/miner-api-server/src/controllers/` - Handler implementations

When in doubt, check the Rust models to see the exact JSON field names expected.

### Source of Truth

- **REST API**: Manually implemented from `proto-rig-api/openapi/MDK-API.json`

The OpenAPI spec is vendored from the private `miner-firmware` repository. See `proto-rig-api/VERSION.md` for the source commit.
