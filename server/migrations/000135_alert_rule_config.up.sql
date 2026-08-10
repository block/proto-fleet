-- User alert-rule configs, keyed by the backing Grafana rule UID. Previously the config round-tripped through a Grafana rule annotation, but Grafana copies annotations onto every alert instance: a large scope (up to 600 ids) replicated per firing device could push a notification batch past the webhook body cap and drop the org's whole batch. Rows follow the alert_route_policy lifecycle: written with rule create/update, deleted with the rule. No backfill: the legacy configs live in Grafana's database, not ours, so pre-table rules keep reading from their legacy annotation (scope-less, so it cannot bloat instances) until their first update writes a row here.
CREATE TABLE alert_rule_config (
    org_id     BIGINT      NOT NULL,
    rule_uid   TEXT        NOT NULL,
    config     JSONB       NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (org_id, rule_uid)
);
