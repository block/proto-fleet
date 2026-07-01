---
title: "Current-membership uptime rollup for large fleets"
date: 2026-07-01
status: implementing
type: plan
---

## Summary

The closed uptime-counts PR optimized site dashboard uptime history by
pre-counting snapshots by stamped historical `site_id`/`building_id`. Review
surfaced a scope mismatch: normal telemetry metrics and the live uptime bar use
current device membership, while the site-count aggregate used historical
placement. A single `GetCombinedMetrics` response should not mix those models.

This plan replaces that design with a compact per-device uptime rollup. The
query path still resolves current device IDs for site/building/rack/group
requests, but historical uptime reads from compact per-device 1-minute, hourly,
or daily rollups instead of raw snapshot rows. Aggregating those rows at read
time preserves the current membership semantics already used by the rest of the
response.

## Lessons From The Closed PR

- `site_id`/`building_id` stamped on historical rows answers a different product
  question: "what happened in this place at that time?"
- Existing ProtoFleet performance charts answer "what happened to the devices
  currently in this selected scope?"
- A pre-counted site CAGG is fast but cannot honor arbitrary current device
  selectors after miners move.
- The durable optimization layer should compact per-device state first, then
  let the service-selected device IDs define the response scope.
- Uptime count work should only run when `MeasurementType.UPTIME` is requested,
  except for omitted measurement filters where legacy/default behavior applies.

## Scope

- Add Timescale continuous aggregates or equivalent rollups with one row per
  organization, device, and 1-minute/hour/day bucket carrying the latest uptime
  state for that device. Include last-seen `site_id` and `building_id` metadata
  for diagnostics/future use, but do not use it to scope current-membership
  chart responses.
- Use that rollup for explicit device-list uptime history, including site and
  unassigned scopes after the service resolves them to current device IDs.
- Keep the live bar sourced from current `GetMinerStateCounts`, but only when
  uptime is requested.
- Remove `MeasurementType.UPTIME` from rack, building, and group performance
  requests that do not render uptime.
- Retain raw `miner_state_snapshots` for 14 days, keep the 1-minute rollup for
  14 days, and keep compact hourly/daily uptime rollups for longer history.

## Non-goals

- Do not change public RPC or proto shapes.
- Do not switch the rest of telemetry to historical placement semantics.
- Do not add building/site-scoped pre-count rows in this PR.
- Do not optimize unrelated metrics beyond avoiding unused uptime work.

## Implementation Notes

- Keep DB access through sqlc queries.
- Use the 1-minute rollup for raw/short-range metric queries, the hourly rollup
  for 1-10 day queries, and the daily rollup for longer ranges so uptime
  history scales with the same data-source routing as the line metrics.
- The rollup query should deduplicate duplicate device IDs in selectors.
- Query semantics should choose the latest per-device rollup row inside each
  requested chart bucket, then count hashing, broken, offline, and sleeping.
- If the rollup has not refreshed yet, fall back to the existing raw snapshot
  query; the live bar still covers the newest point for uptime-rendering
  requests.
- For omitted measurement type lists, preserve the existing broad/default
  response shape and include uptime counts.

## Validation Plan

- Add migration up/down coverage through the existing migration test harness.
- Add Timescale store tests for:
  - explicit duplicate device IDs do not inflate counts;
  - site/current device-list semantics are honored after historical movement;
  - non-uptime requests skip historical and live uptime counts;
  - uptime requests still receive history plus a live bar.
- Add client tests asserting rack/building/group pages do not request uptime.
- Run `just gen-db-queries` if sqlc queries change.
- Run targeted server tests for telemetry domain and Timescale store.
- Run targeted client page tests for changed request payloads.
