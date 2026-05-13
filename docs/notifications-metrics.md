# ProtoFleet notifications metric contract

This document is the canonical contract for every metric ProtoFleet
emits as part of the notifications stack.

The Go-side source of truth is
[`server/internal/infrastructure/metrics/contract.go`](../server/internal/infrastructure/metrics/contract.go).

## Namespace

All ProtoFleet metric names start with the `fleet_` prefix.

## Metrics

| Metric | Type | Unit | Labels | Description |
| --- | --- | --- | --- | --- |
| `fleet_device_online` | gauge (0/1) | `1` | `organization_id`, `device_id`, `device_group?`, `driver?` | 1 when the device is reachable and reporting telemetry, 0 when the telemetry pipeline has marked it unreachable. The series stops being emitted when the device is removed from the fleet (see the staleness contract below for the caveats this implies for offline alerts). |
| `fleet_device_hashrate_terahash` | gauge | `Th/s` | `organization_id`, `device_id`, `device_group?`, `driver?` | Observed hashrate of the device. |
| `fleet_device_hashrate_expected_terahash` | gauge | `Th/s` | `organization_id`, `device_id`, `device_group?`, `driver?` | Expected (nameplate) hashrate of the device. The Hashrate template compares observed against expected. |
| `fleet_device_temperature_max_celsius` | gauge | `Cel` | `organization_id`, `device_id`, `device_group?`, `driver?`, `sensor_kind` | Maximum temperature observed across the device's sensors of the given kind. |
| `fleet_device_temperature_avg_celsius` | gauge | `Cel` | `organization_id`, `device_id`, `device_group?`, `driver?`, `sensor_kind` | Average temperature across the device's sensors of the given kind. |
| `fleet_device_pool_connected` | gauge (0/1) | `1` | `organization_id`, `device_id`, `device_group?`, `driver?` | 1 when the device is connected to its primary mining pool, 0 otherwise. |
| `fleet_command_total` | counter | `1` | `organization_id`, `kind`, `result` | Incremented every time a dispatched command reaches a terminal state. |
| `fleet_telemetry_poll_total` | counter | `1` | `organization_id?`, `device_id?`, `result` | Incremented for every telemetry poll attempt. |

Labels marked with `?` are optional — they may be empty when the underlying
data is unavailable, in which case the label is dropped (it does not become
an empty-string series).

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
3. When a device is removed from the fleet, the subscriber stops emitting
   the series entirely.

The default `DeviceOffline` alert fires on `fleet_device_online == 0`. It
does **not** alert on a single device's series vanishing while the rest of
the fleet keeps reporting: `absent_over_time` operates on the whole
selector and can't recover a per-device label in that case.

The only path that emits this metric is `Provider.EmitDeviceOnline`.
