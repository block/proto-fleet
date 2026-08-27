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
JOIN curtailment_response_profile AS profile
  ON profile.id = rule.response_profile_id
 AND profile.org_id = rule.org_id
WHERE event.org_id = rule.org_id
  AND event.source_actor_type = 'automation'
  AND event.external_source = 'curtailment_automation'
  AND event.external_reference = rule.id::TEXT
  AND event.source_actor_id = rule.id::TEXT
  AND event.idempotency_key = 'curtailment_automation_rule:' || rule.id::TEXT
  AND event.state IN ('pending', 'active', 'restoring')
  AND event.mode = profile.mode
  AND event.strategy = profile.strategy
  AND event.level = profile.level
  AND event.priority = profile.priority
  AND event.curtail_batch_size IS NOT DISTINCT FROM profile.curtail_batch_size
  AND event.curtail_batch_interval_sec = profile.curtail_batch_interval_sec
  AND event.restore_batch_size = profile.restore_batch_size
  AND event.restore_batch_interval_sec = profile.restore_batch_interval_sec
  AND event.include_maintenance = profile.include_maintenance
  AND event.force_include_maintenance = profile.force_include_maintenance
  AND event.force_include_all_paired_miners = profile.force_include_all_paired_miners
  AND event.facility_fan_device_ids = profile.facility_fan_device_ids
  AND event.fan_off_delay_sec = profile.fan_off_delay_sec
  AND event.fan_restore_delay_sec = profile.fan_restore_delay_sec
  AND (
      (profile.mode = 'FULL_FLEET' AND event.mode_params_jsonb = '{}'::JSONB)
      OR
      (
          profile.mode = 'FIXED_KW'
          AND (event.mode_params_jsonb->>'target_kw')::NUMERIC = profile.target_kw
          AND (event.mode_params_jsonb->>'tolerance_kw')::NUMERIC = COALESCE(profile.tolerance_kw, 0)
      )
  )
  AND NOT (event.decision_snapshot_jsonb ? 'response_profile_id')
  AND NOT (event.decision_snapshot_jsonb ? 'response_profile_revision');

ALTER TABLE curtailment_automation_rule
    ALTER COLUMN response_profile_revision SET NOT NULL;
