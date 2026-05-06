# ADR-001: Defer bundling Grafana with the notifications stack

| Status   | Accepted                                                  |
| -------- | --------------------------------------------------------- |
| Date     | 2026-05-05                                                |
| Deciders | Notifications working group (Epic A — Issue 4)            |
| Context  | [notifications-issues.md](../notifications-issues.md), Issue 4 |

## Decision

We will **not** bundle Grafana with the ProtoFleet docker-compose stack in
Phase 1. The notifications open question raised in the design — "should we
ship Grafana with provisioned dashboards?" — is resolved as **defer to
Phase 2**.

The `FLEET_GRAFANA_ENABLED` environment variable is reserved for the
forthcoming Phase-2 implementation but is not honoured by any service in
Phase 1. No Grafana service, datasource, or dashboard files are added.

## Context

The design document
([gist](https://gist.github.com/ankitgoswami/57ccb84705bc032559b1eb0306506fe8))
calls out three trade-offs around bundling Grafana:

1. **Storage cost.** The official `grafana/grafana` image is roughly 200 MB
   uncompressed. ProtoFleet's full deployment image is currently around
   400 MB; adding Grafana grows the install size by ~50 %.
2. **Operational surface.** Grafana introduces another long-lived process,
   another set of credentials to manage, another health surface to monitor,
   and another upgrade cadence to track.
3. **Time-to-value.** Most operators want at-a-glance fleet health and a
   notification when something is wrong. Provisioned dashboards give that
   without anyone having to write PromQL.

## Why defer

- **Phase 1's metric contract is still landing.** Epic B (Issues 5–9) wires
  the OTel SDK, freezes the metric names, and starts emitting series. Any
  dashboard we bundle today would either render against metrics that do not
  yet exist (empty panels) or would have to be reworked the moment a metric
  name or label changes. Issue 6 explicitly calls out that renaming a metric
  means rewriting every shipped rule — the same applies to dashboards.
- **The "Firing now" panel and notification history view in Issue 30 cover
  the most common operator question** ("what is broken right now?") without
  Grafana. Operators who want visualizations beyond that today already have
  the option to point their own Grafana at victoria-metrics manually; we
  document the connection details in `deployment-files/README.md`.
- **VictoriaMetrics ships with `vmui`,** a built-in query/explore UI on
  port 8428 that is sufficient for ad-hoc investigation during Phase 1. If
  we decide later that vmui plus the in-product views are enough, we may
  never need to bundle Grafana.
- **The 200 MB and second-process cost is avoidable for the majority of
  operators.** Phase 1 already adds three new services (otel-collector,
  victoria-metrics+vmalert, alertmanager). A fourth long-lived service that
  most users never log into is hard to justify until we know the dashboards
  will actually be useful.

## Consequences

- Phase 1 ships without Grafana. Operators who want dashboards run their own
  Grafana container and attach it to the `monitoring` docker network
  declared in `server/docker-compose.base.yaml`; from inside that network
  the VictoriaMetrics datasource URL is `http://victoria-metrics:8428`.
  Per Issue 1's "no host ports" rule we do not expose VM on the host, so an
  operator-run Grafana on the host (rather than in docker) is not supported
  in Phase 1.
- The Phase-2 follow-up is tracked: when the metric contract is frozen
  (Issue 6) and at least one operator has shipped fleet metrics through to
  notifications in production, we will re-open this question and, if we
  proceed, ship the three dashboards listed in the design (Fleet overview,
  Per-device, Notifications health) gated behind `FLEET_GRAFANA_ENABLED`,
  defaulting to `false`.
- We avoid the dashboard-rewrite churn that would follow any metric-contract
  change between now and Phase 2.

## What this ADR does NOT do

- It does not preclude an operator from running Grafana out of band.
- It does not commit to bundling Grafana in Phase 2 — that remains a
  decision for the next iteration once metric usage data is available.
- It does not affect the `vmui` UI shipped by VictoriaMetrics, which remains
  available inside the `monitoring` docker network at
  `http://victoria-metrics:8428/vmui`. Per Issue 1's "no host ports" rule it
  is not bound on the host loopback in either compose file.
