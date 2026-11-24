# Node operation and error signals

The `fleetnode run` command can expose OpenTelemetry metrics on a private
Prometheus endpoint at `/metrics`. Enable it with `--runtime-metrics-enabled`
or `FLEETNODE_RUNTIME_METRICS_ENABLED=true`. The endpoint is disabled by default.
Set `--runtime-metrics-address` or `FLEETNODE_RUNTIME_METRICS_ADDRESS` to change
the address. The default is `127.0.0.1:9090`. Use a literal loopback address.
If fleetd and fleetnode share a host, give them different ports and configure
the host collector for both. One-shot CLI commands do not start a listener.

## Fleet Node command metrics

| Prometheus series | Type | Labels |
| --- | --- | --- |
| `fleet_node_operation_attempts_total` | Counter | `kind`, `admission_class` |
| `fleet_node_operation_outcomes_total` | Counter | `kind`, `admission_class`, `result` |
| `fleet_node_operation_duration_seconds` | Histogram | `kind`, `admission_class`, `result` |
| `fleet_node_operations_inflight` | Gauge | `kind`, `admission_class` |

- `kind` uses the values from [`AdmissionForCommand` and `AdmissionForMinerCommand`](../server/internal/domain/fleetnode/control/admission.go).
- `admission_class` values are `general`, `deferrable_read`, and `exclusive`.
- `result` values are `success`, `failure`, `partial`, `busy`, `bad_request`,
  `failed_precondition`, `incapable`, `unsupported`, `miner_auth_rejected`,
  `miner_forbidden`, `scan_failed`, `report_failed`, `internal`, `missing_ack`,
  `unknown_ack`, and `ack_send_failed`.

Each received command increments the attempt counter, including a `busy`
rejection. Each completion records one outcome and one duration sample.
The in-flight gauge decreases at completion. A `busy` rejection records a zero
duration. If the acknowledgement fails to send, the result is `ack_send_failed`
regardless of the command outcome. A batch counts as one command.

## Fleet server metrics

| Prometheus series | Type | Labels and values |
| --- | --- | --- |
| `fleet_node_auth_outcomes_total` | Counter | `operation`: `begin_handshake`, `complete_handshake`, `session`. `result`: `success`, `credential_rejected`, `challenge_rejected`, `session_rejected`, `enrollment_required`, `canceled`, `timeout`, `internal`. |
| `fleet_node_protocol_checks_total` | Counter | `result`: `compatible`, `upgrade_required`. |

An expired, rotated, or unknown session returns `session_rejected`. An expired
or replayed challenge returns `challenge_rejected`. An invalid signature after
a valid challenge returns `credential_rejected`. Invalid API keys and identities
also return `credential_rejected`. Enrollment that still needs confirmation
returns `enrollment_required`. Malformed Authorization headers stop before
session lookup, so the service counter excludes them.

The protocol counter records checks when commands reach dispatch. It also
records `upgrade_required` when an admission check rejects a connected Node
before dispatch. A successful admission check leaves the count to dispatch.
Disconnected Nodes, capability display, and periodic HA readiness checks
are outside the counter. The counter has no protocol version label.

## Monitor setup

Both processes expose the `fleet_runtime_start_time_seconds` gauge without a
database connection. Counters reset on restart. Use completed outcomes as the
denominator for operation failure rates. Track `busy` separately from execution
failures. Set minimum event volume and duration, and monitor missing scrapes
separately. Exclude `session_rejected` and `challenge_rejected` from credential
alerts. Configure collector mappings and monitor policies in the
[infrastructure repository](https://github.com/squareup/tf-protofleet/pull/110).
