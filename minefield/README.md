# Minefield - Error Injection Proxy for ProtoOS

Minefield is an HTTP proxy service that sits between ProtoOS and a miner, intercepting and modifying the `/api/v1/errors` endpoint responses to inject simulated errors. This allows testing of ProtoOS error handling UI without needing to trigger real hardware errors.

## Architecture

```
ProtoOS Client → Minefield Proxy (:7070) → Actual Miner
                        ↓
                 Injects errors into
                 /api/v1/errors responses
                        ↑
                 Control API (:7071)
                 CLI & Web UI
```

## Features

- **Transparent Proxy**: Passes through all requests except `/api/v1/errors`
- **Error Injection**: Injects errors into the errors endpoint response
- **In-Memory Storage**: No database required, errors persist while proxy runs
- **Control API**: REST API for triggering and managing errors
- **CLI Tool**: Command-line interface for error management
- **Web UI**: React-based web interface (TODO)
- **Pre-defined Error Types**: Common miner error types with proper structure

## Installation

```bash
# Build the proxy server and CLI
cd minefield
go build -o bin/minefield ./cmd/minefield
go build -o bin/minefield-cli ./cmd/minefield-cli
```

## Usage

### Starting the Proxy

```bash
# Set target miner URL via environment variable
export PROXY_URL=http://192.168.2.3:8000
./bin/minefield

# Or via command-line flag
./bin/minefield -target http://192.168.2.3:8000

# Options:
#   -proxy   :7070   Address for proxy server (what ProtoOS connects to)
#   -control :7071   Address for control API
#   -target  URL     Target miner URL
#   -verbose         Enable verbose logging
```

### Configure ProtoOS

#### Automatic Setup (Recommended)

ProtoOS will automatically start minefield when `MINEFIELD_URL` is set in your `.env`:

```bash
# In client/.env file:
PROXY_URL=http://192.168.2.3:8000      # The actual miner
MINEFIELD_URL=http://localhost:7070    # Enable minefield (optional)

# Then just run ProtoOS normally:
npm run dev:protoOS
```

When `MINEFIELD_URL` is set:
- Minefield automatically starts on the specified port
- Minefield proxies to the miner specified in `PROXY_URL`
- ProtoOS connects to minefield instead of directly to the miner
- Control UI is available at http://localhost:7071

When `MINEFIELD_URL` is not set:
- ProtoOS connects directly to `PROXY_URL` (normal behavior)
- No minefield proxy is started

#### Manual Setup

You can also run minefield manually if preferred:

```bash
# Start minefield manually
./bin/minefield -target http://192.168.2.3:8000

# Then run ProtoOS with MINEFIELD_URL
MINEFIELD_URL=http://localhost:7070 npm run dev:protoOS
```

### Using the CLI

```bash
# List available error types
./bin/minefield-cli definitions

# Trigger an error
./bin/minefield-cli trigger FanSlow -p fan_bay_index=1 -p fan_id=2 -p actual_rpm=2000

# Trigger with TTL (auto-expires)
./bin/minefield-cli trigger AsicOverTemp -p hashboard_index=1 -p asic_index=5 -p temperature=95 --ttl 60

# List active errors
./bin/minefield-cli list

# List all errors (including expired)
./bin/minefield-cli list --all

# Clear a specific error
./bin/minefield-cli clear <error-id>

# Clear all errors
./bin/minefield-cli clear --all

# Check status
./bin/minefield-cli status
```

### Control API

The control API is available at `http://localhost:7071/api`:

#### Trigger Error
```bash
POST /api/errors/trigger
{
  "error_code": "FanSlow",
  "error_level": "Warning",  # Optional: Error|Warning
  "message": "Custom message", # Optional
  "details": {
    "fan_bay_index": 1,
    "fan_id": 2,
    "actual_rpm": 2000
  },
  "ttl_seconds": 60  # Optional: auto-expire after N seconds
}
```

#### List Errors
```bash
GET /api/errors/active    # Active errors only
GET /api/errors/all       # All errors including expired
```

#### Clear Errors
```bash
DELETE /api/errors/{id}   # Clear specific error
DELETE /api/errors        # Clear all errors
```

#### Get Definitions
```bash
GET /api/errors/definitions  # List all error types
GET /api/errors/categories   # List error categories
```

#### Status
```bash
GET /api/status          # Proxy status
```

## Available Error Types

### Hashboard Errors
- `HashboardOverheat` - Temperature exceeds limits
- `HashboardPowerLost` - Lost power to hashboard
- `HashboardUSBConnectionLost` - USB connection lost

### ASIC Errors
- `AsicEnumerationFailure` - Wrong number of ASICs detected
- `AsicOverTemp` - ASIC temperature too high
- `AsicsNotHashing` - ASICs not producing hashes

### PSU Errors
- `PSUHardwareFault` - PSU hardware fault
- `PSUCommunicationLost` - Lost PSU communication

### Cooling Errors
- `FanSlow` - Fan running below target speed
- `FanNotSpinning` - Fan stopped
- `InsufficientCooling` - Not enough operational fans

### Pool Errors
- `PoolConnectionLost` - Lost pool connection
- `NoPoolConfigured` - No pool configured

### System Errors
- `MixedHashboardTypes` - Mixed hashboard types detected
- `NetworkInterfaceDown` - Network interface down

## Development

### Project Structure
```
minefield/
├── cmd/
│   ├── minefield/        # Proxy server
│   └── minefield-cli/    # CLI tool
├── internal/
│   ├── proxy/           # HTTP proxy logic
│   ├── errors/          # Error injection engine
│   └── api/             # Control API handlers
├── web/                 # React web UI (TODO)
└── README.md
```

### Building
```bash
# Build all
go build ./...

# Run tests
go test ./...

# Format code
go fmt ./...
```

## How It Works

1. **Proxy Setup**: Minefield creates an HTTP reverse proxy to the target miner
2. **Request Routing**: All requests are passed through unchanged
3. **Response Interception**: For `/api/v1/errors` GET requests, the response is intercepted
4. **Error Injection**: Active errors from the store are injected into the JSON response
5. **Response Modification**: The modified response is sent to ProtoOS

The proxy is completely transparent except for the errors endpoint, ensuring all other ProtoOS functionality works normally.

## Limitations

- Errors are stored in memory only (cleared on restart)
- Only modifies `/api/v1/errors` endpoint responses
- Requires ProtoOS to poll the errors endpoint to see changes
- Web UI not yet implemented

## Future Enhancements

- [ ] React web UI for easier error management
- [ ] Parse miner-firmware swagger spec at build time
- [ ] WebSocket support for real-time error updates
- [ ] Persistent storage option
- [ ] Error templates/scenarios
- [ ] Multi-miner support (proxy multiple miners)