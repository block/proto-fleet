-- User alert-rule configs keyed by Grafana rule UID. The previous annotation round-trip is unusable for scopes: Grafana copies annotations onto every alert instance, so a large scope could push a notification batch past the webhook body cap.
-- No backfill (legacy configs live in Grafana's database): pre-table rules keep their annotation until their first update writes a row. Rows follow the alert_route_policy lifecycle.
CREATE TABLE alert_rule_config (
    org_id     BIGINT      NOT NULL,
    rule_uid   TEXT        NOT NULL,
    config     JSONB       NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (org_id, rule_uid)
);
