-- Canonicalize API-created profiles before execution starts comparing the
-- persisted selector schema with the request schema. Version-zero scopes were
-- valid before topology selectors existed; after this migration all profiles
-- use the current explicit selector contract.
UPDATE curtailment_response_profile
SET scope_json = CASE
    WHEN scope_json = '{}'::JSONB THEN jsonb_build_object(
        'whole_org', TRUE,
        'scope_schema_version', 1
    )
    ELSE jsonb_set(scope_json, '{scope_schema_version}', '1'::JSONB, TRUE)
END
WHERE NOT (scope_json ? 'scope_schema_version');

ALTER TABLE curtailment_response_profile
    ADD COLUMN revision UUID NOT NULL DEFAULT gen_random_uuid();

ALTER TABLE curtailment_automation_rule
    ADD COLUMN response_profile_revision UUID;

UPDATE curtailment_automation_rule AS rule
SET response_profile_revision = profile.revision
FROM curtailment_response_profile AS profile
WHERE profile.id = rule.response_profile_id
  AND profile.org_id = rule.org_id;

-- A crash after event creation but before the rule records active_event_uuid
-- is recovered through the event's idempotency key. Stamp the backfilled rule
-- binding into those live event snapshots so replay can enforce the same
-- revision fence after an upgrade. The full ownership tuple prevents unrelated
-- or malformed events from inheriting a rule binding.
UPDATE curtailment_event AS event
SET decision_snapshot_jsonb = event.decision_snapshot_jsonb || jsonb_build_object(
    'response_profile_id', rule.response_profile_id,
    'response_profile_revision', rule.response_profile_revision::TEXT
)
FROM curtailment_automation_rule AS rule
WHERE event.org_id = rule.org_id
  AND event.source_actor_type = 'automation'
  AND event.external_source = 'curtailment_automation'
  AND event.external_reference = rule.id::TEXT
  AND event.source_actor_id = rule.id::TEXT
  AND event.idempotency_key = 'curtailment_automation_rule:' || rule.id::TEXT
  AND event.state IN ('pending', 'active', 'restoring')
  AND NOT (event.decision_snapshot_jsonb ? 'response_profile_id')
  AND NOT (event.decision_snapshot_jsonb ? 'response_profile_revision');

ALTER TABLE curtailment_automation_rule
    ALTER COLUMN response_profile_revision SET NOT NULL;
