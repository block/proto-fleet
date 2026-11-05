-- Persist dispatch revocation and isolate automation execution per rule.
ALTER TABLE curtailment_event ADD COLUMN dispatch_rejection_reason text;
-- Retry history alone does not prove a Curtail was sent: admission skips and
-- unenqueued devices also consume retries. Only possible sends owe Uncurtail.
ALTER TABLE curtailment_target ADD COLUMN curtail_possibly_sent boolean NOT NULL DEFAULT false;
-- Before this column, retry history was the only evidence that a Curtail may
-- have reached a miner, so live events' targets with retries keep owing one.
UPDATE curtailment_target AS target
SET curtail_possibly_sent = TRUE
FROM curtailment_event AS event
WHERE event.id = target.curtailment_event_id
  AND event.state IN ('pending', 'active', 'restoring')
  AND (target.retry_count > 0 OR target.curtail_retry_count > 0);

CREATE TABLE curtailment_automation_execution_grant (
 rule_id bigint PRIMARY KEY REFERENCES curtailment_automation_rule(id) ON DELETE CASCADE,
 org_id bigint NOT NULL REFERENCES organization(id),
 response_profile_id bigint NOT NULL,
 response_profile_revision uuid NOT NULL,
 authorization_envelope_json jsonb NOT NULL,
 admin_controls_granted boolean NOT NULL DEFAULT false,
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE curtailment_automation_execution (
 rule_id bigint PRIMARY KEY REFERENCES curtailment_automation_rule(id) ON DELETE CASCADE,
 mqtt_source_id bigint NOT NULL REFERENCES curtailment_mqtt_source_config(id) ON DELETE CASCADE,
 generation bigint NOT NULL DEFAULT 1,
 direction text NOT NULL,
 target text NOT NULL CHECK (target IN ('ON','OFF')),
 target_at timestamptz,
 received_at timestamptz NOT NULL,
 received_broker text NOT NULL DEFAULT '',
 prior_edge_at timestamptz,
 status text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','retry','completed')),
 attempts integer NOT NULL DEFAULT 0,
 retry_at timestamptz NOT NULL DEFAULT now(),
 lease_expires_at timestamptz,
 last_error text
);
CREATE INDEX idx_curtailment_automation_execution_due ON curtailment_automation_execution(retry_at,rule_id) WHERE status IN ('pending','retry');
ALTER TABLE curtailment_rig_config_reconciliation ADD COLUMN delivery_attempts integer NOT NULL DEFAULT 0;
CREATE TABLE curtailment_rig_config_delivery_attempt (
 organization_id bigint NOT NULL REFERENCES organization(id) ON DELETE CASCADE,
 device_id bigint NOT NULL REFERENCES device(id) ON DELETE CASCADE,
 attempts integer NOT NULL DEFAULT 0,
 PRIMARY KEY (organization_id,device_id)
);
