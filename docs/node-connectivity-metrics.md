# Node connectivity monitoring

`fleet_node_connectivity_summary` uses the heartbeats from `fleet_node_live` for
confirmed Nodes that are not deleted. The view has one row for each organization
that is not deleted, even after removal of its last Node. The view includes standby
Nodes so that it can show a loss of redundancy.

The grace period is five minutes from the latest confirmed enrollment. For Nodes
without a confirmed enrollment record, use the Node creation time. A disconnected
Node remains expected. Use the privileged database administration workflow to set
an exclusion in `fleet_node_monitoring_exclusion`. Each exclusion requires a
reason and an expiration at most seven days after its creation time. The exclusion
applies between creation and expiration. Routing and heartbeats continue during
the exclusion.

A heartbeat is recent through two minutes of age. Older known heartbeats are
overdue. The view allows heartbeat times up to five seconds ahead of the database
clock. A heartbeat is unknown if it is missing, precedes admission, or exceeds that
clock allowance. `expected_count` equals `recent_count` plus `overdue_count` plus
`unknown_count`. The view also reports `excluded_count` and `grace_count`.

Alert when `recent_count < expected_count` persists. Use `unknown_count` to identify
missing or invalid heartbeats. `oldest_heartbeat_age_seconds` is zero when no
expected Node has a known heartbeat. The view also reports zero age for heartbeats
within the allowed clock skew that are ahead of the database clock.

An external SQL collector queries the view with SELECT-only permissions and
primary-only evaluation. It must bound result cardinality and independently
monitor query/export health. Do not treat missing rows as healthy or aggregate
duplicate database hosts across failover. A sustained threshold supplies reconnect
grace in addition to heartbeat age; normal brief reconnects should not page.

`fleet_node_control_streams` on the optional runtime metrics endpoint is a current
process-wide count of authenticated control streams. It reads the registry under
a short lock, does not query the database, and emits zero when empty. It is a
diagnostic signal, not the expected-inventory denominator: maintenance/grace and
HA process roles differ. Heartbeat coverage detects vanished Nodes even if the
Fleet process cannot emit its runtime metrics.
