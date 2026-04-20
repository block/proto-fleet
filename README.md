<p align="center">
  <a href="https://github.com/btc-mining/proto-fleet" target="_blank" rel="noopener noreferrer">
    <img width="64" src="docs/logo.svg" alt="Proto logo">
  </a>
</p>
<h1 align="center">
  Proto Fleet
</h1>
<h3 align="center">
  Mining management software. Evolved.
</h3>
<p align="center">
  No fees. No training. Full control.<br/>
  Open source fleet management for bitcoin miners.
</p>
<p align="center">
  <a href="LICENSE">
    <img src="https://img.shields.io/badge/license-Apache%202.0-blue.svg" alt="Proto Fleet is released under the Apache 2.0 license." />
  </a>
  <a href="https://github.com/btc-mining/proto-fleet/actions/workflows/protofleet-client-checks.yml">
    <img src="https://github.com/btc-mining/proto-fleet/actions/workflows/protofleet-client-checks.yml/badge.svg" alt="Client checks status." />
  </a>
  <a href="https://github.com/btc-mining/proto-fleet/actions/workflows/protofleet-server-checks.yml">
    <img src="https://github.com/btc-mining/proto-fleet/actions/workflows/protofleet-server-checks.yml/badge.svg" alt="Server checks status." />
  </a>
  <a href="https://github.com/btc-mining/proto-fleet/actions/workflows/protofleet-e2e-tests.yml">
    <img src="https://github.com/btc-mining/proto-fleet/actions/workflows/protofleet-e2e-tests.yml/badge.svg" alt="E2E tests status." />
  </a>
</p>

**Proto Fleet** is open-source fleet management software for bitcoin miners. It helps operators pair devices, monitor telemetry, and manage mining infrastructure without giving up control. Built with React and TypeScript clients, Go services, Connect RPC, Protocol Buffers, and TimescaleDB. For architecture details, see [docs/architecture.md](docs/architecture.md).

## Getting Started

### Prerequisites

- Docker and Docker Compose
- [Hermit](https://cashapp.github.io/hermit/), or a local installation of the required development tools

### Initial Setup

```bash
source ./bin/activate-hermit
just setup
```

To install Git hooks after your toolchain is ready:

```bash
just install-hooks
```

For non-Hermit setup details, `lefthook` and Ruff hook prerequisites, and `go.work` guidance, see [CONTRIBUTING.md](CONTRIBUTING.md).

### Start Development

```bash
just dev
```

This starts the Go backend with Docker Compose and the Vite dev server for ProtoFleet at http://localhost:5173.

### Protocol Buffer Code Generation

After modifying definitions in `proto/`, regenerate generated clients and server code:

```bash
just gen
```

## Supported Hardware

| Hardware | Firmware variants | Discovery port |
| --- | --- | --- |
| MicroBT WhatsMiner | Stock | 4028 |
| Bitmain Antminer | Stock | 4028 |
| Bitmain Antminer | VNish, Braiins OS, LuxOS, Marathon | 80 |
| Canaan AvalonMiner | Stock | 4028 |
| BitAxe | Stock (AxeOS) | 80 |
| NerdAxe | Stock | 80 |
| ePIC | Stock | 80 |
| Auradine | Stock | 80 |
| Proto | Stock | 443 |

## Production Install

### Latest Version

```bash
bash <(curl -fsSL https://fleet.proto.xyz/install.sh)
```

### Specific Version

```bash
bash <(curl -fsSL https://fleet.proto.xyz/install.sh) v0.1.0
```

### Uninstall

```bash
bash <(curl -fsSL https://fleet.proto.xyz/uninstall.sh)
```

If Proto Fleet was installed in a non-default location, pass it explicitly:

```bash
bash <(curl -fsSL https://fleet.proto.xyz/uninstall.sh) --deployment-path /path/to/install/root
```

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for development workflows and contribution guidelines. Project standards and community expectations are documented in [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md), [GOVERNANCE.md](GOVERNANCE.md), and [SECURITY.md](SECURITY.md).

## License

This project is licensed under the Apache 2.0 License. See the [LICENSE](LICENSE) file for details.
