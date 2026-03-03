# Seed Telemetry

Generates synthetic telemetry data for testing Proto Fleet dashboard charts.

## Overview

This tool populates the `device_metrics` TimescaleDB hypertable with simulated mining device
metrics over a configurable time range. After insertion, it refreshes all continuous aggregates
so hourly and daily charts reflect the new data immediately.

Synthetic devices are named `seed-device-001`, `seed-device-002`, etc. The tool always clears
previous seed data before inserting, so repeated runs produce a clean dataset.

## Usage

### Quick Start

Requires TimescaleDB running locally (`just db-up` from the server directory).

```bash
# Generate 10 days of data for 3 devices (defaults)
just seed-telemetry

# Generate 30 days with outlier spikes for chart-scaling testing
just seed-telemetry --days 30 --outliers

# Preview what would be generated without inserting
just seed-telemetry --dry-run

# Remove all seed data and refresh aggregates
just seed-telemetry --clean-up
```

### Running Directly

From the `server/` directory:

```bash
go run ./devtools/seedtelemetry --days 7 --devices 5 --interval 1m
```

### Cleanup

Delete all synthetic rows (identified by the `seed-device-` prefix) and refresh the affected
aggregate windows:

```bash
just seed-telemetry --clean-up
```

Combine with `--dry-run` to preview what would be deleted.

## Flags

| Flag | Description | Default |
|------|-------------|---------|
| `--days` | Days of historical data to generate | `10` |
| `--interval` | Time between data points per device | `30s` |
| `--devices` | Number of synthetic devices | `3` |
| `--batch-size` | Rows per INSERT batch | `2800` |
| `--dry-run` | Print stats without inserting | `false` |
| `--clean-up` | Delete existing seed data without generating new rows | `false` |
| `--outliers` | Inject random outlier spikes for chart testing | `false` |

## Database

The tool connects to PostgreSQL/TimescaleDB using these environment variables (or flags):

| Variable | Flag | Default |
|----------|------|---------|
| `DB_HOST` | `--db-host` | `127.0.0.1` |
| `DB_PORT` | `--db-port` | `5432` |
| `DB_USER` | `--db-user` | `fleet` |
| `DB_PASSWORD` | `--db-password` | `fleet` |
| `DB_NAME` | `--db-name` | `fleet` |

After insertion, the following continuous aggregates are refreshed:

- `device_metrics_hourly`
- `device_metrics_daily`
- `device_status_hourly`
- `device_status_daily`

## Generated Data

Each data point includes correlated metrics modeled after an ASIC miner:

- **Hash rate** (~190 TH/s base): per-device jitter, per-point noise, occasional performance dips
- **Temperature**: sinusoidal 24-hour cycle peaking at 15:00 UTC, with ambient/inlet/outlet offsets
- **Power**: scales proportionally with hash rate
- **Fan RPM**: scales with chip temperature
- **Efficiency** (J/TH): derived from power and hash rate
- **Voltage/Current**: base values with noise
- **Chip count/frequency**: varies per device model
- **Health status**: weighted random (95% active, 3% inactive, 1.5% warning, 0.5% critical);
  dips trigger elevated warning/critical probability

When `--outliers` is enabled, ~0.3% of data points receive a 5x hash rate spike for testing
chart auto-scaling behavior.
