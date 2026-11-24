# Command lifecycle monitoring

`command_backlog_summary` exposes queued, running, and outstanding queue messages
per organization and command class (`firmware` versus `control`). Every organization
retains both zero-valued rows when empty. Pending age and total outstanding age use
original queue creation time, so retries do not reset evidence of stalled work.
Running age starts when a worker claims the current attempt. Progress updates
refresh the timestamp that the reaper uses and leave the attempt start time
unchanged. Attempts already running at migration count as running but have no
recorded start time. The view reports zero running age if no start time is available.
Future timestamps increment `invalid_clock_count`. The view clamps negative ages to zero.
The view reads an indexed subset of queued and running commands.

The runtime endpoint exposes bounded `kind`, `command_class`, and `result` labels:

| Metric family | Meaning |
| --- | --- |
| `fleet_command_committed_outcomes_total` | Terminal queue outcomes committed, including reaped outcomes |
| `fleet_command_persistence_failures_total` | Post-execution queue/result transaction failures |
| `fleet_command_queue_wait_seconds` | Original enqueue to worker start, sampled per attempt |
| `fleet_command_attempt_duration_seconds` | Worker execution duration by result |
| `fleet_command_completion_seconds` | Original enqueue to committed terminal outcome |

Use the queue wait histogram's `_count` for attempt starts with valid queue
timestamps. Retries contribute a sample for each attempt. Use the attempt duration
histogram's `_count` for finished attempts by result. Reaped outcomes have no runtime
completion-duration sample; their counters and persisted backlog supply evidence.
Runtime counters are process-local and can be lost on a crash between commit and
emission; they are operational signals, not an exactly-once audit log. Stalled
attempts remain visible through the independent database backlog.

Queue wait is **not confirmed Node delivery latency**. The current command protocol
provides a terminal acknowledgement, not a distinct acceptance acknowledgement.
Do not relabel worker start or stream enqueue as device acceptance. Command traces
retain server/Node dispatch detail. Measuring confirmed acceptance separately would
require an explicitly versioned protocol extension; these signals establish queue
progress and completion without changing device execution behavior.

Collectors must not divide attempt failures by committed outcomes: these represent
different populations. Use completed attempt outcomes as an error-rate denominator,
with a minimum count in the same evaluation window. Cancellation is separate from
operational failure. Firmware work requires substantially longer backlog thresholds
than quick control commands. Missing collection is unknown, never an empty queue.

`fleet_command_dispatch_send_seconds` measures registered command dispatch
through completion of the server's stream write, including outgoing-buffer wait.
Its `_count` tracks `sent` and `send_failed` outcomes by `kind` and
`admission_class`. The measurement snapshots the command and connection
before writing, so replacement/cancellation cannot reattribute it to a new stream.
Canceled-before-send commands have no send sample; backlog and attempt metrics
cover stalled/canceled work. Successful write is not confirmed remote acceptance.
