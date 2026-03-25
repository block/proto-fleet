# Virtual Miner Plugin

A simulated miner plugin for testing and demonstration purposes. Virtual miners don't require any network hardware and can be configured via JSON.

## Quick Start

1. Build the plugin:
   ```bash
   just build-virtual-plugin
   ```

2. Restart the fleet API:
   ```bash
   cd server && just rebuild-fleet-api
   ```

3. Discover virtual miners using **IP List** discovery mode with IPs in the `10.255.x.x` range.

4. Pair discovered miners (any credentials work, or use defaults: `virtual`/`virtual`).

## IP Address Range

Virtual miners use the reserved `10.255.x.x` IP range to avoid conflicts with real network devices:

- **Range**: `10.255.0.2` - `10.255.255.254`
- **Capacity**: ~65,000 unique addresses
- **Discovery port**: `4028` (standard CGMiner API port)

The `10.255.0.0/16` range is part of the private IP space (RFC 1918) and is unlikely to conflict with real miners on most networks.

## Configuration

Edit `config.json` in this directory to configure virtual miners.

### Auto-Generation (Recommended)

Generate a fleet of miners with randomized but realistic telemetry:

```json
{
  "generate": {
    "count": 1000,
    "serial_prefix": "VM",
    "ip_start": "10.255.0.2",
    "baseline_variance_percent": 10,
    "profiles": [
      {
        "name": "S19 XP",
        "weight": 40,
        "model": "Antminer S19 XP",
        "manufacturer": "Bitmain",
        "hashboards": 3,
        "asics_per_board": 76,
        "fan_count": 4,
        "baseline_hashrate_ths": 140,
        "baseline_power_w": 3010,
        "baseline_temp_c": 75,
        "fan_rpm_min": 3000,
        "fan_rpm_max": 6000
      },
      {
        "name": "S21",
        "weight": 30,
        "model": "Antminer S21",
        "manufacturer": "Bitmain",
        "hashboards": 3,
        "asics_per_board": 114,
        "fan_count": 4,
        "baseline_hashrate_ths": 200,
        "baseline_power_w": 3500,
        "baseline_temp_c": 60,
        "fan_rpm_min": 3200,
        "fan_rpm_max": 5200
      }
    ]
  },
  "miners": []
}
```

- **count**: Number of miners to generate
- **serial_prefix**: Prefix for serial numbers (e.g., "VM" → "VM0001")
- **ip_start**: First IP address (increments sequentially)
- **baseline_variance_percent**: Random variance applied to telemetry values (±%)
- **profiles**: Miner profiles with weighted selection (weights are relative)

### Manual Configuration

Define specific miners individually:

```json
{
  "generate": null,
  "miners": [
    {
      "serial_number": "VIRTUAL001",
      "ip_address": "10.255.0.2",
      "port": 4028,
      "model": "Antminer S19 XP",
      "manufacturer": "Bitmain",
      "mac_address": "02:00:00:00:00:01",
      "hashboards": 3,
      "asics_per_board": 76,
      "fan_count": 4,
      "baseline_hashrate_ths": 140.5,
      "baseline_power_w": 3010,
      "baseline_temp_c": 75,
      "fan_rpm_min": 3000,
      "fan_rpm_max": 6000
    }
  ]
}
```

## Capabilities

Virtual miners support all standard device capabilities:

- **Discovery & Pairing**: Auto-discovery via IP list, accepts any credentials
- **Commands**: Reboot, start/stop mining, LED blink, pool configuration
- **Cooling modes**: Air and immersion cooling
- **Telemetry**: Hashrate, power, temperature, fan speed, efficiency, per-board stats, PSU stats

## Testing Large Fleets

To test with 1000+ miners:

1. Set `"count": 1000` in config.json
2. Rebuild and restart: `just build-virtual-plugin && cd server && just rebuild-fleet-api`
3. Use IP List discovery with range: `10.255.0.2` - `10.255.3.234` (for 1000 miners)

Discovery of 1000 miners takes approximately 2-5 minutes depending on system load and timeout settings.
