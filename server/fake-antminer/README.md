# Fake Antminer

This is a fake Antminer implementation that simulates both the cgminer API (port 4028) and Web API (port 80) of real Antminer devices.

## Features

- Simulates cgminer API on port 4028
  - `version`: Get miner version info
  - `summary`: Get mining summary
  - `stats`: Get miner stats
  - `pools`: Get mining pools
  - `devs`: Get device details
  - `config`: Get miner configuration
  
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
- `FIRMWARE_VERSION`: Firmware version (default: "Antminer S19j Pro 110Th 28/11/2022 16:51:53")

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
go build -o fake-antminer
./fake-antminer
```

## Testing

To test the RPC API:

```bash
echo '{"command":"version"}' | nc localhost 4028
```

To test the Web API:

```bash
curl -u root:root --digest http://localhost:80/cgi-bin/get_system_info.cgi
``` 
