# Fake Antminer

This is a fake Antminer implementation that simulates both the cgminer API (port 4028) and Web API (port 80) of real Antminer devices.

## Features

- Simulates cgminer API on port 4028
  - `version`: Get miner version info
  - `summary`: Get mining summary
  - `stats`: Get miner stats
  - `pools`: Get mining pools
  - `devs`: Get device details
  
- Simulates Web API on port 80
  - `/cgi-bin/get_system_info.cgi`: Get system information
  - `/cgi-bin/summary.cgi`: Get mining summary
  - `/cgi-bin/get_miner_conf.cgi`: Get miner configuration
  - `/cgi-bin/get_network_info.cgi`: Get network configuration
  - `/cgi-bin/get_kernel_log.cgi`: Get kernel log
  - `/cgi-bin/set_miner_conf.cgi`: Set miner configuration
  - `/cgi-bin/reboot.cgi`: Reboot miner
  - `/cgi-bin/blink.cgi`: Control LED blinking

## Usage

### Configuration

The miner can be configured using environment variables:

- `MINER_TYPE`: Model name (default: "Antminer S19j Pro")
- `SERIAL_NUMBER`: Serial number (default: "fake-antminer-1")
- `MAC_ADDRESS`: MAC address (default: "00:11:22:33:44:55")
- `FIRMWARE_VERSION`: Firmware version (default: "2.0.0")

### Authentication

Web API endpoints use digest authentication with these default credentials:
- Username: root
- Password: root

### Docker Compose

The fake Antminer can be started using Docker Compose:

```yaml
fake-antminer:
  build:
    context: ./fake-antminer
    dockerfile: Dockerfile
  environment:
    MINER_TYPE: "Antminer S19 FAKE"
    FIRMWARE_VERSION: "Antminer S19 XP 140Th 15/01/2023 10:30:25"
  mem_limit: 128M
  mem_reservation: 64M
  cpus: 0.25
  expose:
    - 4028
    - 80
```

### Scaling

The service is designed to be scaled using Docker Compose:

```bash
# Scale to 100 instances
docker-compose up -d --scale fake-antminer=100
```

## Development

To build and run the fake Antminer locally:

```bash
GOWORK=off go build -o fake-antminer
./fake-antminer
```

## Testing

To test the RPC API:

```bash
echo '{"command":"version"}' | nc localhost 4028
```

To test the Web API, set `FAKE_MINER_AUTH` to the fixture's configured username
and password separated by a colon:

```bash
curl --user "$FAKE_MINER_AUTH" --digest http://localhost:80/cgi-bin/get_system_info.cgi
``` 

### Multiple destination addresses

Set `FAKE_MINER_ADDRESSES_FILE` to a newline-delimited file of unique IPv4
addresses to serve independent miners from one process. The inventory is loaded
once; empty files, duplicate entries and invalid addresses fail startup. Blank
lines are ignored. Configure the host's local routes or addresses so traffic for
these destinations reaches this process. The listeners remain TCP 4028 and HTTP
80. Only listed local destination addresses are served, including `/health`;
other destinations are rejected. The HTTP `Host` header never selects a miner.

Each destination has an independent pool configuration, password, work mode,
firmware and LED state shared by both protocols. MAC addresses, miner serials
and hashboard serials are stable and unique across IPv4 destinations. Load mode
uses the normal model and `root`/`root` defaults, with full MD5 digest response
validation so credentials cannot leak between miners. The existing single-miner
fixture retains its defaults and legacy authentication behavior when the address
file is omitted. Load telemetry varies deterministically on observation using a
sequence seeded by the destination IPv4 address; it stays within healthy bounds.
Existing explicit error environment variables still apply to every listed miner.

Optional load controls:

- `FAKE_MINER_RESPONSE_DELAY`: nonnegative Go duration, default `0`. Set `5ms`
  for the load campaign. Delay occurs before authentication and state locks;
  cancelled HTTP writes do not reach their handlers. RPC waits are bounded by
  the existing 30-second connection limit and process shutdown.
- `FAKE_MINER_METRICS_FILE`: append cumulative JSONL aggregates every ten
  seconds and at graceful shutdown. Each record contains UTC `timestamp`,
  `requests_total`, `errors_total` and `operations`. Fixed `http:<path>` and
  `rpc:<command>` operation keys contain `requests_total`, `errors_total` and
  `latency_sum_ms`; unknown operations share one key per protocol. There are no
  per-address metric labels or retained request samples. HTTP error counts
  include expected digest challenges (401). Metrics file errors are reported
  on stderr. Load mode suppresses per-request logs.

The file determines the exact miner count: for example, a shard's 4,000 address
lines create exactly 4,000 miners, without creating miners for other protocol
addresses routed to the same host. Provision and calibrate shards separately;
local fixture tests do not qualify a 40,000-miner campaign.

Run fixture tests from the repository root with Hermit:

```sh
. ./bin/activate-hermit
(cd server/fake-antminer && GOWORK=off go test -race ./...)
go test ./plugin/antminer/pkg/antminer -run TestLoadFixtureContract -count=1
```

The real-plugin contract starts this fixture on ephemeral test-only ports and
requires two locally routable IPv4 addresses. It exercises discovery, digest
authentication, identity, telemetry, pool/password isolation, HTTP writes with
TCP readback and deadlines. No Docker service or privileged port is required.

In multi-address load mode, authenticated `GET /fake-api/v1/load/led`
returns `{"active": true|false}` for command readback. This fixture audit is
separate from the hardware API and is absent from single-miner mode.

### Real Fleet Node sandbox discovery

The mixed-miner sandbox requires the discovery/pairing stack merged in #326–328.
Its preparation writes identical explicit discovery ranges on both peers and
uses bounded IP-list commands: a full shard exceeds the 4,096-target automatic
scan limit. Sandbox PR [squareup/tf-protofleet#82](https://github.com/squareup/tf-protofleet/pull/82)
contains the cross-repository test command in `fleet-sandbox/LOADTEST.md`. That
test runs `TestLoadtestDiscoveryConfigContract` in the real Node package against
all twenty generated configurations and full endpoint inventories.
