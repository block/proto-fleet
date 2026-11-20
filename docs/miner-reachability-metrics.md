# Miner reachability monitoring

`device_reachability_summary` exposes expected/reachable/unreachable/unknown counts
by organization, site, source Node, and device class. A container controller counts
once; this is not a census of its individual boards or physical ASICs. Site/source
zero mean unassigned site/direct polling respectively.

Expected inventory is undeleted paired-like devices after five minutes of pairing
or assignment grace and outside explicit, expiring `device_reachability_exclusion`
rows. Paused hashing and disconnected Nodes do not shrink expected inventory.
The exclusion table is managed through the privileged database administration
workflow and is independent of telemetry-freshness exclusions.

The summary uses status from the telemetry polling path. `OFFLINE` counts as
unreachable when a status probe confirms a failure to connect to the miner.
Unknown health and failures to reach a Fleet Node or plugin count as unknown.
Fleet Nodes and plugins must send probe evidence to count a connection failure
as unreachable. During a rolling update, peers that omit this evidence can
still report device status, and the summary counts those failures as unknown.
`ACTIVE`, `INACTIVE`, `MAINTENANCE`, `ERROR`, and `NEEDS_MINING_POOL` count as
reachable. An observation is unknown if it is missing, is from the future, is
older than two minutes, or precedes pairing or assignment. Other statuses also
count as unknown.

Fleet records when it saves a telemetry status. Status written by pairing or a
command, such as `ACTIVE` after firmware cleanup, counts as unknown. The first
telemetry write after cleanup records a new observation even if it reports the
same status. Status rows that existed before this migration count as unknown
until telemetry records an observation.

Expected always equals reachable + unreachable + unknown. Alert on unreachable /
expected and separately on unknown / expected; never remove unknowns from the
denominator or turn a telemetry gap into an explicit unreachable observation.
Collect from the database primary with query-health checks and bounded scope count.
When the last device leaves a scope its row disappears; collectors must document
missing-group recovery rather than claiming it proves devices became reachable.

This summary does not add device polling or per-device export labels. Committed
measurement freshness, Node heartbeat coverage, and process health remain separate
signals needed to interpret an outage.
