# Proto-Fleet Installation Scripts

This document provides instructions for using the Proto-Fleet installation scripts.

## Prerequisites

Before running either script:

1. Enable host networking in Docker:
   - Open Docker Desktop
   - Go to Settings -> Resources -> Network
   - Check "Enable host networking"

## Installation Order

**Always install fleet first, then sim miners.**

## Installing Proto-Fleet

```bash
bash <(curl -fsSL https://proto-fleet.s3.us-east-1.amazonaws.com/releases/fleet/latest/install.sh)
```

The `install.sh` script sets up the Proto-Fleet server components.

### Proto-Fleet Installation Options

```bash
Usage: install.sh [VERSION]

If you omit VERSION or pass "latest", installs the latest release by picking the first tarball found in the latest folder.
You can override by doing, e.g.:
  install.sh v0.1.0-beta-5
```

Examples:

```bash
# Install the latest version
bash <(curl -fsSL https://proto-fleet.s3.us-east-1.amazonaws.com/releases/fleet/latest/install.sh)

# Install a specific version
bash <(curl -fsSL https://proto-fleet.s3.us-east-1.amazonaws.com/releases/fleet/latest/install.sh) v0.1.0-beta-5
```

The script will:

- Check system compatibility (page size)
- Download and extract the specified version
- Preserve existing configuration files if present
- Run the deployment script automatically

## Installing Simulator Miners

```bash
bash <(curl -fsSL https://proto-fleet.s3.us-east-1.amazonaws.com/releases/fleet/<release-version>/install-sim-miners.sh)
```

Replace `<release-version>` with the desired version.

### Important Notes

- The sim miners script only supports MacOS.
- The sim miners script must keep running for as long as you need the miners.
- You will need to press Ctrl+C to terminate when done.
- The script will display a comma-separated list of miner IP addresses.
- Note that nmap doesn't work for discovering miners - use the IP list provided by the script.

### Sim Miners Options

```bash
Usage: install-sim-miners.sh [OPTIONS]

Options:
  -n, --num NUM         Number of miners to create (default: 5)
  -s, --start-ip NUM    Starting IP offset from subnet (default: 10)
  -b, --subnet SUBNET   Subnet to use for miner IPs (default: 172.20.0)
  -h, --help            Show this help message
```

Example:

```bash
# Create 10 miners starting at IP address 172.20.0.100
bash <(curl -fsSL https://proto-fleet.s3.us-east-1.amazonaws.com/releases/fleet/latest/install-sim-miners.sh) -n 10 -s 100
```
