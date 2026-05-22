# Proto Fleet notifications metric contract

This document is the canonical contract for every metric Proto Fleet
emits as part of the notifications stack.

The Go-side source of truth is
[`server/internal/infrastructure/metrics/contract.go`](../server/internal/infrastructure/metrics/contract.go).

## Storage and evaluation

Every contract metric is persisted to the `notification_metric_sample`
hypertable in TimescaleDB by the in-process writer in fleet-api. 

Alert evaluation runs in a Grafana sidecar. Grafana provisions a
PostgreSQL datasource pointed at TimescaleDB, evaluates the alert
rules defined under
[`server/monitoring/grafana/provisioning/alerting/`](../server/monitoring/grafana/provisioning/alerting/),
and routes firing alerts through its built-in Alertmanager to the
fleet-api webhook receiver.

## Namespace

All Proto Fleet metric names start with the `fleet_` prefix.

## Metrics

| Metric | Type | Unit | Labels | Description |
| --- | --- | --- | --- | --- |
| `fleet_device_online` | gauge (0/1) | `1` | `organization_id`, `device_id`, `device_group?`, `driver?` | 1 when the device is reachable and reporting telemetry, 0 when the telemetry pipeline has marked it unreachable. The series stops being emitted when the device is removed from the fleet (see the staleness contract below for the caveats this implies for offline alerts). |
| `fleet_device_hashrate_terahash` | gauge | `Th/s` | `organization_id`, `device_id`, `device_group?`, `driver?` | Observed hashrate of the device. |
| `fleet_device_hashrate_expected_terahash` | gauge | `Th/s` | `organization_id`, `device_id`, `device_group?`, `driver?` | Expected (nameplate) hashrate of the device. The Hashrate template compares observed against expected. |
| `fleet_device_temperature_max_celsius` | gauge | `Cel` | `organization_id`, `device_id`, `device_group?`, `driver?`, `sensor_kind` | Maximum temperature observed across the device's sensors of the given kind. |
| `fleet_device_temperature_avg_celsius` | gauge | `Cel` | `organization_id`, `device_id`, `device_group?`, `driver?`, `sensor_kind` | Average temperature across the device's sensors of the given kind. |
| `fleet_device_pool_connected` | gauge (0/1) | `1` | `organization_id`, `device_id`, `device_group?`, `driver?` | 1 when the device is connected to its primary mining pool, 0 otherwise. **Reserved — not currently emitted.** The broadcaster does not yet have an explicit pool-connectivity signal from plugins, so emitting this gauge would either miss real pool disconnects/hijacks or fire on intentionally inactive devices. The metric stays in the contract so dashboards referencing it keep compiling; samples will resume once plugins surface real pool state. |
| `fleet_command_total` | counter | `1` | `organization_id`, `kind`, `result` | Incremented every time a dispatched command reaches a terminal state. |
| `fleet_telemetry_poll_total` | counter | `1` | `organization_id?`, `device_id?`, `result` | Incremented for every telemetry poll attempt. |

Labels marked with `?` are optional — they may be empty when the
underlying data is unavailable. The hypertable stores the empty string
in those cases (the columns are `TEXT NOT NULL DEFAULT ''`), so Grafana
queries can filter on labels with simple equality.

## Closed enums

Two label values are constrained to a closed set:

| Label | Allowed values |
| --- | --- |
| `result` | `success`, `failure` |
| `sensor_kind` | `board`, `chip`, `inlet`, `outlet`, `ambient`, `hotspot` |

## Per-board / per-chip aggregation

Per-board and per-chip detail is pre-aggregated to `_max` and `_avg`.
The raw per-chip series is **not** exposed on the wire.

The aggregation lives in
[`server/internal/domain/telemetry/broadcaster_metrics.go`](../server/internal/domain/telemetry/broadcaster_metrics.go).

## Staleness contract

`fleet_device_online` is the source of truth for "is this device alive?".
The contract is:

1. While a device is reachable, the broadcaster subscriber writes
   `fleet_device_online=1` on every telemetry tick.
2. When the telemetry pipeline marks a device unreachable
   (`MinerStatusOffline`, connection error), the subscriber writes
   `fleet_device_online=0` with the same labels.
3. When a device is removed from the fleet, the subscriber stops
   emitting the series entirely.

The default `DeviceOffline` rule in the Grafana provisioning bundle
fires when the most recent `fleet_device_online` value for a device
in the last ten minutes is `0` — i.e. the telemetry pipeline is still
emitting samples and reporting unreachable. It does **not** alert on a
single device's series vanishing while the rest of the fleet keeps
reporting: there is no per-device label to recover in that case.
Detecting per-device disappearance reliably requires either an
independent offline emitter or a per-device `last_seen` timestamp.

The only path that emits this metric is `Provider.EmitDeviceOnline`.

## Retention

The hypertable compresses chunks older than two days and drops them
after thirty (see
[`migrations/000051_notification_metric_samples.up.sql`](../server/migrations/000051_notification_metric_samples.up.sql)).
Grafana's alert rules only need the last ten minutes of data; the
longer retention window exists to support ad-hoc Explore queries
without forcing a separate aggregate table.
