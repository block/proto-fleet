---
title: "Release-channel suppression query review benchmark"
date: 2026-09-11
status: completed
type: plan
tracker: https://github.com/block/proto-fleet/pull/1015#discussion_r3981388329
---

# Release-channel suppression query review benchmark

Keep historical suppression and its window ranking. Add an index that narrows
history to the assignment and supplies newest-first rollout order. Filtering
only active rollouts would restart miners that operators stopped or that failed
in a finished rollout.

## Change

```sql
CREATE INDEX idx_firmware_rollout_assignment_history
    ON firmware_rollout (
        channel_id,
        release_channel_pair_key(manufacturer),
        release_channel_pair_key(model),
        assignment_generation,
        created_at DESC,
        id DESC
    );
```

The existing `firmware_rollout_suppressed_device` view and its three consumers
keep their semantics: use the newest rollout **holding that miner** within the
same channel, normalized model pair, and assignment generation. Newer rollouts
that omit a miner do not erase its halt; explicit retry can requeue it.

## Fixture and method

`TestReleaseChannelSuppressionQueryPlans` in
`server/internal/domain/stores/sqlstores/release_channel_suppression_performance_test.go`
uses the repository's isolated, migrated test-database harness. It creates:

- 100 current miners in one channel, all mismatched and halted as failed.
- 100 finished rollouts of their current assignment generation.
- 10 historical channels, each with 10 generations and 100 finished rollouts
  per generation; historical targets reference the same miners.
- **10,100 rollouts and 1,010,000 target rows** in total. Only the current
  channel has a live assignment and scope.

This exercises a reconcile pass that must inspect every current miner and
correctly decide there is no work. After `ANALYZE`, the test reads the actual
three named SQL queries from `server/sqlc/queries/release_channel.sql` and runs
`EXPLAIN (ANALYZE, BUFFERS)`. It measures three executions without the index and
three with it in the same database. It also explicitly prepares each query and
measures `EXPLAIN ... EXECUTE` with `plan_cache_mode = force_generic_plan` on a
pinned connection.

Before and after adding the index, the generated query methods must return
zero mismatched miners, 100 suppressed miners, and zero assignments needing a
rollout. Existing `TestReleaseChannelQueries_MismatchAndSuppression` covers
historical halts, scope re-entry, and retry behavior with newer rollouts.

## Results

Measured on September 11, 2026 in the local TimescaleDB container running
PostgreSQL 18.6, against the
old schema checkout plus the candidate index. The final reproducible test
passed in 8.65 seconds, including fixture construction and cleanup.
It also passed against the integrated review changes and migration index in
6.32 seconds; both runs verified unchanged query results.

| Actual SQL consumer | Without index, three runs | With index, three runs | Forced generic plan, without → with |
| --- | ---: | ---: | ---: |
| `ListReleaseChannelMismatchedMembers` | 48.91–53.95 ms | 4.82–6.77 ms | 77.41 → 5.20 ms |
| `ListReleaseChannelSuppressedMembers` | 79.34–104.16 ms | 5.03–9.08 ms | 124.43 → 4.52 ms |
| `ListReleaseChannelFirmwareNeedingRollout` | 79.03–125.99 ms | 1.25–1.40 ms | 84.38 → 1.24 ms |

For the no-work reconcile query, the unindexed suppression subplan scanned all
10,100 rollouts once per current miner, discarded 10,000 unrelated rollouts on
each scan, and looked up 10,000 historical targets. It used **109,700 shared
buffer hits**. The index allowed the existing `row_number() <= 1` run condition
to stop after the newest matching rollout and target for each miner: **100
rollout lookups, 100 target lookups, and 700 shared buffer hits**. No historical
rows or lifecycle states were omitted.

These are synthetic, mostly warm-cache local measurements, not production
latency targets. Timing varies with other local work, PostgreSQL statistics,
and data distribution. Sparse historical membership can require scanning more
than one rollout per miner. The result supports this index and retention of
the existing semantics; it does not establish a fleet-wide capacity limit.

## Reproduce

With the repository's local TimescaleDB test service running, from the repo root:

```bash
DB_DSN= DB_NAME=fleet DB_ADDRESS=127.0.0.1:5432 \
  DB_USERNAME=fleet DB_PASSWORD=fleet \
  ROLLOUT_SUPPRESSION_QUERY_PLANS=/tmp/release-channel-query-plans \
  bin/go test ./server/internal/domain/stores/sqlstores \
    -run '^TestReleaseChannelSuppressionQueryPlans$' -count=1 -v
```

The test drops and recreates only the history index inside its disposable test
database, then the harness removes that database. It writes complete plans to
the requested output directory. Ordinary test runs skip the million-row
fixture; there are no machine-specific timing assertions.
