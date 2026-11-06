# Fake Proto Rig

A simulator for Proto Bitcoin mining devices, implementing the REST API interface.

## Overview

This simulator allows the fleet management system to be tested without physical hardware. It implements:

**REST API** (~45 endpoints matching the MDK OpenAPI spec):
- Pools, mining, system, hardware, telemetry, cooling, network, auth, and pairing endpoints

## Features

- Stateful simulation of mining state, pools, and configuration
- Realistic telemetry data with random variation
- Authentication: protected REST endpoints require a Bearer token (access token from `/api/v1/auth/login` or a paired EdDSA JWT), with a small public surface for login, password setup, system status, network info, hardware discovery, and pairing discovery
- Error injection via environment variables
- REST API for both ProtoFleet plugin and ProtoOS dashboard

### Firmware Update Simulation

Firmware uploads move through `downloaded`, `installing`, and `installed`.
Rebooting after `installed` promotes the staged version to the running version.
A second upload is rejected while any update is pending.

The simulator detects an uploaded version in this order:

1. A semantic version in the filename, such as `protoos-1.4.4.swu`.
2. A marker in the first 64 KiB of the file:
   - `firmware_version=1.4.4`
   - `{"firmware_version":"1.4.4"}`
   - A line containing only `1.4.4` or `v1.4.4`
3. A one-patch increment from the currently running version.

Fake uploads are streamed with a 512 MiB request limit. Only the version scan
prefix is retained in memory.

Tests can set the one-shot outcome for the next update through the fake-only
`PUT /fake-api/v1/test/firmware-update/outcome` endpoint. Set
`FAKE_RIG_ENABLE_TEST_CONTROLS=true` to register this endpoint, then authenticate
with a bearer token issued by `/api/v1/auth/login`. The endpoint is unavailable
by default.

```json
{"outcome":"success"}
```

Supported outcomes are `success`, `error`, and `attention`. Both failure
outcomes expose the real API's `error` update state with a deterministic error
message, which `message` repeats as its one-line summary. This endpoint is not
part of the real device API.

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
| `TLS_CERT_FILE` / `TLS_KEY_FILE` | PEM certificate and private key; both are required to enable HTTPS | Unset (HTTP) |
| `FAKE_MINER_ADDRESSES_FILE` | Newline-separated exact IPv4 allowlist for independent rigs selected by socket destination | Unset (one rig) |
| `FAKE_MINER_RESPONSE_DELAY` | Nonnegative Go duration applied before request dispatch; canceled request contexts stop waiting | `0s` |
| `FAKE_MINER_METRICS_FILE` | Append cumulative aggregate JSONL every ten seconds and at graceful shutdown | Unset (disabled) |
| `SERIAL_NUMBER` | Device serial number | `PROTO-SIM-<uuid>` |
| `MAC_ADDRESS` | Device MAC address | Generated from instance ID |
| `FAKE_RIG_ENABLE_TEST_CONTROLS` | Enable authenticated fake-only test control endpoints | `false` |

### Multiple destination addresses

One wildcard listener can serve many independent rigs. Each address in
`FAKE_MINER_ADDRESSES_FILE` receives its own current REST handler, authentication,
pools, mining settings and command state. Selection uses the accepted socket's
local destination address; the HTTP `Host` header cannot select a rig. Unlisted
addresses return 404, including for `/health`, and never create miners.

The file accepts one IPv4 address per line, with blank lines ignored and duplicate
addresses collapsed. CIDRs, IPv6 and invalid lines fail startup. The host must
already route the listed addresses locally; this process does not configure
interfaces or routes.

Multi-rig serial numbers and MACs are stable across process restarts and derive
from all four destination octets: `192.0.2.3` becomes `PROTO-SIM-C0000203` and
`02:00:C0:00:02:03`. `SERIAL_NUMBER` and `MAC_ADDRESS` apply only in single-rig
mode. Startup password and explicit error settings apply independently to each
rig; existing authentication and command contracts remain in effect.

For an HTTPS load generator with a prepared address file and certificate:

```bash
FAKE_MINER_ADDRESSES_FILE=/run/proto-rigs.txt \
TLS_CERT_FILE=/run/tls/cert.pem TLS_KEY_FILE=/run/tls/key.pem \
HTTP_PORT=443 FAKE_MINER_RESPONSE_DELAY=5ms \
FAKE_MINER_METRICS_FILE=/var/log/proto-rig-metrics.jsonl \
GOWORK=off go run .
```

The port remains 8080 and the delay remains zero unless configured. The load
profile explicitly selects 443 and a 5 ms delay. Certificate/key loading errors
fail startup; the simulator does not generate certificates.

Metrics contain UTC `timestamp`, cumulative `requests_total`, `errors_total`, and
an `operations` map keyed by `http:<registered-route>` (unknown paths use
`http:unknown`). Each operation contains `requests_total`, `errors_total`, and
`latency_sum_ms`, including the configured delay. HTTP errors and canceled
request contexts count as errors. Route patterns group dynamic pool IDs, keeping
aggregate cardinality fixed; addresses, credentials and request bodies are not
logged. The optional JSONL file grows by one aggregate record per ten seconds,
plus a final record after in-flight requests finish on graceful shutdown.

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

# Hardware discovery
curl http://localhost:8080/api/v1/hardware
curl http://localhost:8080/api/v1/hashboards
curl http://localhost:8080/api/v1/hardware/psus
curl http://localhost:8080/api/v1/power-supplies

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

# Telemetry data
curl "http://localhost:8080/api/v1/telemetry?level=miner" \
  -H "Authorization: Bearer ${TOKEN}"
```

Locator LED requests require bearer auth:

```bash
# Blink for 30 seconds
curl -X POST "http://localhost:8080/api/v1/system/locate?led_on_time=30" \
  -H "Authorization: Bearer ${TOKEN}"

# Keep locating until disabled
curl -X POST "http://localhost:8080/api/v1/system/locate?led_on_time=0" \
  -H "Authorization: Bearer ${TOKEN}"

# Disable locate mode
curl -X POST "http://localhost:8080/api/v1/system/locate?enable=false" \
  -H "Authorization: Bearer ${TOKEN}"
```

**Available REST endpoints:**
- `/api/v1/pools` - Pool configuration (GET, POST)
- `/api/v1/pools/{id}` - Individual pool (GET, PUT, DELETE)
- `/api/v1/mining` - Mining status (GET)
- `/api/v1/mining/target` - Power target (GET, PUT)
- `/api/v1/mining/start`, `/api/v1/mining/stop` - Mining control (POST)
- `/api/v1/system` - System information (GET)
- `/api/v1/hardware` - Hardware info (GET, public)
- `/api/v1/hardware/psus` - PSU hardware info (GET, public)
- `/api/v1/hashboards` - Hashboard hardware info (GET, public)
- `/api/v1/power-supplies` - PSU telemetry/status (GET, public)
- `/api/v1/system/locate` - Locator LED control (POST; `led_on_time=0` or negative values persist until `enable=false`)
- `/api/v1/system/secure` - Secure status (GET, public) and secure override (PUT, auth)
- `/api/v1/curtailment/config` - Curtailment service configuration (GET, PUT; bearer auth)
- `/api/v1/curtailment/status` - Latest curtailment status (GET, bearer auth; always reports no status received)
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
| **Board enum** | `"B4_128"`, `"B4_192"` | `"B4"` (split in MDK-API 1.8.1) |
| **Telemetry-service status** | GET/PUT `/system/telemetry` return `{"enabled", "message"}` (TelemetryResponse) | bare `{"enabled"}` or a message-only body |
| **PSU update body** | optional `psu_types` map (slot ID → PSU type enum), validated (422 on bad value/slot) | ignore body or accept unknown PSU types |
| **Port field** | `json:"port"` (0 is valid) | `json:"port,omitempty"` (omits 0) |
| **Pool URL ports** | explicit port or protocol default (`3333` for SV1, `3336` for SV2) | reject a valid pool URL because its port is omitted |
| **SV2 authority key** | require a valid `v2_authority_pubkey` for remote SV2 pools; allow omission only for loopback | accept an unauthenticated remote SV2 pool |
| **System status fields** | `{"onboarded", "password_set"}` | including `default_password_active` (removed in MDK-API 1.8.2) |
| **SecureResponse** | `{"secure", "state": {sshd, nats-service, secureboot, certificate-validity}}` | bare `{"secure"}` (state added in MDK-API 1.8.2) |
| **TimeSeriesRequest** | Validate `start_time` and `levels` are required (return 422) | Accept missing required fields |
| **Logs `lines` query** | `lines=0` returns an empty result | treat `0` as the 100-entry default |
| **Update status message** | when `error` is present, `message` is its one-line summary (at most 512 bytes) | generic `message` that hides the failure text |

### Cross-Reference with miner-firmware

The reference implementation is in the private `miner-firmware` repository at:
- `services/mdk-server/` - MDK server, including the MDK v1 handlers

When in doubt, check the MDK v1 handlers to see the exact JSON field names expected.

### Source of Truth

- **REST API**: Manually implemented from `proto-rig-api/openapi/MDK-API.json`

The OpenAPI spec is vendored from the private `miner-firmware` repository. See `proto-rig-api/VERSION.md` for the source commit.

In multi-address load mode, authenticated `GET /fake-api/v1/load/led`
returns `{"active": true|false}` for command readback. This fixture audit is
separate from the hardware API and is absent from single-miner mode.
