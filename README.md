<p align="center">
  <a href="https://github.com/block/proto-fleet" target="_blank" rel="noopener noreferrer">
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
  <a href="https://github.com/block/proto-fleet/actions/workflows/protofleet-client-checks.yml">
    <img src="https://github.com/block/proto-fleet/actions/workflows/protofleet-client-checks.yml/badge.svg" alt="Client checks status." />
  </a>
  <a href="https://github.com/block/proto-fleet/actions/workflows/protofleet-server-checks.yml">
    <img src="https://github.com/block/proto-fleet/actions/workflows/protofleet-server-checks.yml/badge.svg" alt="Server checks status." />
  </a>
  <a href="https://github.com/block/proto-fleet/actions/workflows/protofleet-e2e-tests.yml">
    <img src="https://github.com/block/proto-fleet/actions/workflows/protofleet-e2e-tests.yml/badge.svg" alt="E2E tests status." />
  </a>
</p>

**Proto Fleet** is open-source fleet management software for bitcoin miners. It helps operators pair devices, monitor telemetry, and manage mining infrastructure without giving up control. Built with React and TypeScript clients, Go services, Connect RPC, Protocol Buffers, and TimescaleDB. For architecture details, see [docs/architecture.md](docs/architecture.md).

## Install

Proto Fleet deploys into Docker on Linux and macOS, or into WSL2 on Windows.

### Linux and macOS

Requires Docker and Docker Compose. On macOS (and Windows via Docker Desktop), enable host networking under **Settings → Resources → Network → Enable host networking**. See [deployment-files/README.md](deployment-files/README.md) for the full prerequisites.

#### Latest Version

```bash
bash <(curl -fsSL https://fleet.proto.xyz/install.sh)
```

#### Specific Version

```bash
bash <(curl -fsSL https://fleet.proto.xyz/install.sh) v0.1.0
```

#### Uninstall

```bash
bash <(curl -fsSL https://fleet.proto.xyz/uninstall.sh)
```

If Proto Fleet was installed in a non-default location, pass it explicitly:

```bash
bash <(curl -fsSL https://fleet.proto.xyz/uninstall.sh) --deployment-path /path/to/install/root
```

### Windows

Requires Windows 10 (build 19041 or newer) or Windows 11 (x64), local Administrator access, and virtualization enabled in BIOS/UEFI. Docker Desktop is **not** required — the installer enables WSL2, installs an Ubuntu distro and Docker Engine inside it, and deploys Proto Fleet at `~/proto-fleet` within the distro.

`installer.exe` is not standalone. It resolves the deployment payload from files packaged alongside it in the release bundle, so you need to download and extract the full bundle before running it. Windows is supported on x64 only.

1. Go to the [latest release](https://github.com/block/proto-fleet/releases/latest) and download `proto-fleet-<version>-amd64.tar.gz`.
2. Extract the archive. From PowerShell:

   ```powershell
   tar -xzf proto-fleet-<version>-amd64.tar.gz
   ```

   Or right-click the file in File Explorer and choose **Extract All**.
3. Open the extracted `deployment\install\` folder and double-click `installer.exe`.

The installer self-elevates via UAC. If it has to enable Windows features for WSL, it may prompt for a reboot and then resume automatically.

#### Uninstall

Run `uninstall.exe` from the `deployment\install\` folder of the bundle you extracted during install. If you no longer have it, re-download and extract the latest release bundle and use the `uninstall.exe` inside it.

For Windows installer/uninstaller build and test details, see [`deployment-files/windows/README.md`](deployment-files/windows/README.md).

## Supported Hardware

Per-device feature support.

- **✅** — supported and tested.
- **❌** — not supported.
- **🟡** — supported by [asic-rs](https://github.com/256foundation/asic-rs), but not yet tested on this combination.

<!-- prettier-ignore-start -->
<table>
<tr align="center"><th>Manufacturer</th><th>Proto</th><th>MicroBT</th><th colspan="6">Bitmain</th><th>Canaan</th><th>Bitaxe</th><th>NerdAxe</th><th>ePIC</th><th>Auradine</th></tr>
<tr align="center"><td>Model line</td><td>Rig</td><td>WhatsMiner</td><td colspan="6">Antminer</td><td>AvalonMiner</td><td>BitAxe</td><td>NerdAxe</td><td>ePIC</td><td>Auradine</td></tr>
<tr align="center"><td>Firmware</td><td>ProtoOS</td><td>Stock</td><td>Stock</td><td>VNish</td><td>ePIC</td><td>Braiins OS</td><td>LuxOS</td><td>Marathon</td><td>Stock</td><td>AxeOS</td><td>Stock</td><td>Stock</td><td>Stock</td></tr>
<tr align="center"><td>Telemetry</td><td>✅</td><td>✅</td><td>✅</td><td>✅</td><td>✅</td><td>✅</td><td>✅</td><td>🟡</td><td>✅</td><td>🟡</td><td>🟡</td><td>🟡</td><td>🟡</td></tr>
<tr align="center"><td>Reboot</td><td>✅</td><td>✅</td><td>✅</td><td>✅</td><td>✅</td><td>🟡</td><td>🟡</td><td>🟡</td><td>🟡</td><td>🟡</td><td>🟡</td><td>🟡</td><td>🟡</td></tr>
<tr align="center"><td>Pause/Resume</td><td>✅</td><td>✅</td><td>✅</td><td>✅</td><td>✅</td><td>🟡</td><td>🟡</td><td>🟡</td><td>🟡</td><td>🟡</td><td>🟡</td><td>🟡</td><td>🟡</td></tr>
<tr align="center"><td>Edit Pools</td><td>✅</td><td>✅</td><td>✅</td><td>✅</td><td>🟡</td><td>🟡</td><td>🟡</td><td>🟡</td><td>🟡</td><td>🟡</td><td>🟡</td><td>🟡</td><td>🟡</td></tr>
<tr align="center"><td>FW Update</td><td>✅</td><td>❌</td><td>✅</td><td>❌</td><td>🟡</td><td>❌</td><td>❌</td><td>❌</td><td>❌</td><td>❌</td><td>❌</td><td>❌</td><td>❌</td></tr>
<tr align="center"><td>Power Mode</td><td>✅</td><td>✅</td><td>✅</td><td>✅</td><td>🟡</td><td>🟡</td><td>🟡</td><td>🟡</td><td>🟡</td><td>🟡</td><td>🟡</td><td>🟡</td><td>🟡</td></tr>
<tr align="center"><td>Cooling Mode</td><td>✅</td><td>❌</td><td>❌</td><td>❌</td><td>🟡</td><td>❌</td><td>❌</td><td>❌</td><td>❌</td><td>❌</td><td>❌</td><td>❌</td><td>❌</td></tr>
<tr align="center"><td>Update Password</td><td>✅</td><td>❌</td><td>✅</td><td>❌</td><td>❌</td><td>❌</td><td>❌</td><td>❌</td><td>❌</td><td>❌</td><td>❌</td><td>❌</td><td>❌</td></tr>
<tr align="center"><td>Download Logs</td><td>✅</td><td>❌</td><td>✅</td><td>❌</td><td>❌</td><td>❌</td><td>❌</td><td>❌</td><td>❌</td><td>❌</td><td>❌</td><td>❌</td><td>❌</td></tr>
<tr align="center"><td>Blink LED</td><td>✅</td><td>✅</td><td>✅</td><td>✅</td><td>✅</td><td>🟡</td><td>🟡</td><td>🟡</td><td>🟡</td><td>🟡</td><td>🟡</td><td>🟡</td><td>🟡</td></tr>
</table>
<!-- prettier-ignore-end -->

## Powered by asic-rs

Multi-manufacturer miner support in Proto Fleet is built on top of [asic-rs](https://github.com/256foundation/asic-rs), a Rust library from the [256 Foundation](https://github.com/256foundation) that abstracts discovery, monitoring, and control across ASIC miner vendors and firmwares. It is the reason the 🟡 cells above exist at all: every new vendor or firmware that asic-rs learns to speak is one that Proto Fleet can drive with no extra plugin work.

If you work on miner firmware or operate a heterogeneous fleet, go give them a star. Upstream contributions to asic-rs flow straight back into Proto Fleet via the [`plugin/asicrs/`](plugin/asicrs/) plugin.

## Local Development

### Prerequisites

- Docker and Docker Compose
- [Hermit](https://cashapp.github.io/hermit/) (recommended) — activates the full toolchain. For a manual tool setup, see [CONTRIBUTING.md](CONTRIBUTING.md).

### Initial Setup

```bash
source ./bin/activate-hermit
just setup
```

To install Git hooks after your toolchain is ready:

```bash
just install-hooks
```

For `lefthook` and Ruff hook prerequisites and `go.work` guidance, see [CONTRIBUTING.md](CONTRIBUTING.md).

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

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for development workflows and contribution guidelines. Project standards and community expectations are documented in [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md), [GOVERNANCE.md](GOVERNANCE.md), and [SECURITY.md](SECURITY.md).

## License

This project is licensed under the Apache 2.0 License. See the [LICENSE](LICENSE) file for details.
