-- Canonicalize API-created profiles before execution starts comparing the
-- persisted selector schema with the request schema. Version-zero scopes were
-- valid before topology selectors existed; after this migration all profiles
-- use the current explicit selector contract.
UPDATE curtailment_response_profile
SET scope_json = CASE
    WHEN scope_json = '{}'::JSONB AND site_id IS NOT NULL THEN jsonb_build_object(
        'site_ids', jsonb_build_array(site_id),
        'scope_schema_version', 1
    )
    WHEN scope_json = '{}'::JSONB THEN jsonb_build_object(
        'whole_org', TRUE,
        'scope_schema_version', 1
    )
    ELSE jsonb_set(scope_json, '{scope_schema_version}', '1'::JSONB, TRUE)
END
WHERE NOT (scope_json ? 'scope_schema_version');

-- Revision state deliberately lives beside the existing tables instead of in
-- them. HA upgrades start the new passive binary (and its migrations) while
-- the previous active binary is still serving. That binary uses SELECT * and
-- positional scans for both base tables, so even nullable additive columns
-- would break the standard passive-first rollout.
CREATE TABLE curtailment_response_profile_revision (
    response_profile_id BIGINT PRIMARY KEY
        REFERENCES curtailment_response_profile(id) ON DELETE CASCADE,
    revision UUID NOT NULL DEFAULT gen_random_uuid()
);

CREATE TABLE curtailment_automation_rule_profile_revision (
    automation_rule_id BIGINT PRIMARY KEY
        REFERENCES curtailment_automation_rule(id) ON DELETE CASCADE,
    response_profile_revision UUID NOT NULL
);

INSERT INTO curtailment_response_profile_revision (response_profile_id)
SELECT id
FROM curtailment_response_profile;

INSERT INTO curtailment_automation_rule_profile_revision (
    automation_rule_id,
    response_profile_revision
)
SELECT rule.id, profile_revision.revision
FROM curtailment_automation_rule AS rule
JOIN curtailment_response_profile_revision AS profile_revision
  ON profile_revision.response_profile_id = rule.response_profile_id;

CREATE FUNCTION canonicalize_curtailment_response_profile_scope()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF NOT (NEW.scope_json ? 'scope_schema_version') THEN
        NEW.scope_json := CASE
            WHEN NEW.scope_json = '{}'::JSONB AND NEW.site_id IS NOT NULL THEN jsonb_build_object(
                'site_ids', jsonb_build_array(NEW.site_id),
                'scope_schema_version', 1
            )
            WHEN NEW.scope_json = '{}'::JSONB THEN jsonb_build_object(
                'whole_org', TRUE,
                'scope_schema_version', 1
            )
            ELSE jsonb_set(NEW.scope_json, '{scope_schema_version}', '1'::JSONB, TRUE)
        END;
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER canonicalize_curtailment_response_profile_scope
    BEFORE INSERT OR UPDATE OF site_id, scope_json ON curtailment_response_profile
    FOR EACH ROW
    EXECUTE FUNCTION canonicalize_curtailment_response_profile_scope();

-- Keep companion revision rows complete when the previous active binary writes
-- during the rolling update. New writes use the same triggers, so there is one
-- revision policy for both binary versions.
CREATE FUNCTION sync_curtailment_response_profile_revision()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'INSERT' THEN
        INSERT INTO curtailment_response_profile_revision (response_profile_id)
        VALUES (NEW.id);
    ELSIF OLD.site_id IS DISTINCT FROM NEW.site_id
       OR OLD.scope_json IS DISTINCT FROM NEW.scope_json
       OR OLD.authorization_envelope_jsonb IS DISTINCT FROM NEW.authorization_envelope_jsonb
       OR OLD.mode IS DISTINCT FROM NEW.mode
       OR OLD.strategy IS DISTINCT FROM NEW.strategy
       OR OLD.level IS DISTINCT FROM NEW.level
       OR OLD.priority IS DISTINCT FROM NEW.priority
       OR OLD.target_kw IS DISTINCT FROM NEW.target_kw
       OR COALESCE(OLD.tolerance_kw, 0) IS DISTINCT FROM COALESCE(NEW.tolerance_kw, 0)
       OR OLD.curtail_batch_size IS DISTINCT FROM NEW.curtail_batch_size
       OR OLD.curtail_batch_interval_sec IS DISTINCT FROM NEW.curtail_batch_interval_sec
       OR OLD.restore_batch_size IS DISTINCT FROM NEW.restore_batch_size
       OR OLD.restore_batch_interval_sec IS DISTINCT FROM NEW.restore_batch_interval_sec
       OR OLD.include_maintenance IS DISTINCT FROM NEW.include_maintenance
       OR OLD.force_include_maintenance IS DISTINCT FROM NEW.force_include_maintenance
       OR OLD.post_event_cooldown_sec IS DISTINCT FROM NEW.post_event_cooldown_sec
       OR OLD.force_include_all_paired_miners IS DISTINCT FROM NEW.force_include_all_paired_miners
       OR OLD.facility_fan_device_ids IS DISTINCT FROM NEW.facility_fan_device_ids
       OR OLD.fan_off_delay_sec IS DISTINCT FROM NEW.fan_off_delay_sec
       OR OLD.fan_restore_delay_sec IS DISTINCT FROM NEW.fan_restore_delay_sec THEN
        UPDATE curtailment_response_profile_revision
        SET revision = gen_random_uuid()
        WHERE response_profile_id = NEW.id;
    END IF;
    RETURN NULL;
END;
$$;

CREATE TRIGGER sync_curtailment_response_profile_revision
    AFTER INSERT OR UPDATE ON curtailment_response_profile
    FOR EACH ROW
    EXECUTE FUNCTION sync_curtailment_response_profile_revision();

CREATE FUNCTION sync_curtailment_automation_rule_profile_revision()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'INSERT' THEN
        INSERT INTO curtailment_automation_rule_profile_revision (
            automation_rule_id,
            response_profile_revision
        )
        SELECT NEW.id, revision
        FROM curtailment_response_profile_revision
        WHERE response_profile_id = NEW.response_profile_id;
    ELSIF OLD.response_profile_id IS DISTINCT FROM NEW.response_profile_id THEN
        UPDATE curtailment_automation_rule_profile_revision AS rule_revision
        SET response_profile_revision = profile_revision.revision
        FROM curtailment_response_profile_revision AS profile_revision
        WHERE rule_revision.automation_rule_id = NEW.id
          AND profile_revision.response_profile_id = NEW.response_profile_id;
    END IF;
    RETURN NULL;
END;
$$;

CREATE TRIGGER sync_curtailment_automation_rule_profile_revision
    AFTER INSERT OR UPDATE OF response_profile_id ON curtailment_automation_rule
    FOR EACH ROW
    EXECUTE FUNCTION sync_curtailment_automation_rule_profile_revision();

-- sqlc reads a stable, revision-bearing relation while the previous release
-- continues reading the unchanged base table.
CREATE VIEW curtailment_response_profile_with_revision AS
SELECT profile.*, profile_revision.revision
FROM curtailment_response_profile AS profile
JOIN curtailment_response_profile_revision AS profile_revision
  ON profile_revision.response_profile_id = profile.id;

-- Preserve replay provenance for an automation event created by the previous
-- active binary after this migration has run on the new passive host. New code
-- already supplies these keys, so the trigger only fills an absent binding
-- after proving that the persisted execution snapshot still matches the rule's
-- current profile.
CREATE FUNCTION bind_legacy_curtailment_automation_event_revision()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    bound_profile_id BIGINT;
    bound_profile_revision UUID;
BEGIN
    IF NEW.source_actor_type = 'automation'
       AND NEW.external_source = 'curtailment_automation'
       AND NEW.state IN ('pending', 'active', 'restoring')
       AND NOT (NEW.decision_snapshot_jsonb ? 'response_profile_id')
       AND NOT (NEW.decision_snapshot_jsonb ? 'response_profile_revision') THEN
        SELECT rule.response_profile_id, rule_revision.response_profile_revision
        INTO bound_profile_id, bound_profile_revision
        FROM curtailment_automation_rule AS rule
        JOIN curtailment_automation_rule_profile_revision AS rule_revision
          ON rule_revision.automation_rule_id = rule.id
        JOIN curtailment_response_profile AS profile
          ON profile.id = rule.response_profile_id
         AND profile.org_id = rule.org_id
        JOIN curtailment_response_profile_revision AS profile_revision
          ON profile_revision.response_profile_id = profile.id
         AND profile_revision.revision = rule_revision.response_profile_revision
        WHERE NEW.org_id = rule.org_id
          AND NEW.external_reference = rule.id::TEXT
          AND NEW.source_actor_id = rule.id::TEXT
          AND NEW.idempotency_key = 'curtailment_automation_rule:' || rule.id::TEXT
          AND NEW.mode = profile.mode
          AND NEW.strategy = profile.strategy
          AND NEW.level = profile.level
          AND NEW.priority = profile.priority
          AND NEW.curtail_batch_size IS NOT DISTINCT FROM profile.curtail_batch_size
          AND NEW.curtail_batch_interval_sec = profile.curtail_batch_interval_sec
          AND NEW.restore_batch_size = profile.restore_batch_size
          AND NEW.restore_batch_interval_sec = profile.restore_batch_interval_sec
          AND NEW.include_maintenance = profile.include_maintenance
          AND NEW.force_include_maintenance = profile.force_include_maintenance
          AND NEW.force_include_all_paired_miners = profile.force_include_all_paired_miners
          AND NEW.facility_fan_device_ids = profile.facility_fan_device_ids
          AND NEW.fan_off_delay_sec = profile.fan_off_delay_sec
          AND NEW.fan_restore_delay_sec = profile.fan_restore_delay_sec
          AND NEW.scope_type = CASE
              WHEN profile.scope_json @> '{"whole_org": true}'::JSONB THEN 'whole_org'
              WHEN profile.scope_json ? 'site_id' THEN 'site'
              WHEN jsonb_typeof(profile.scope_json->'site_ids') = 'array'
                  AND jsonb_array_length(profile.scope_json->'site_ids') = 1 THEN 'site'
              WHEN profile.scope_json ? 'device_identifiers' THEN 'device_list'
              ELSE 'mixed'
          END
          AND NEW.scope_jsonb - 'scope_schema_version' = CASE
              WHEN profile.scope_json @> '{"whole_org": true}'::JSONB THEN '{}'::JSONB
              WHEN jsonb_typeof(profile.scope_json->'site_ids') = 'array'
                  AND jsonb_array_length(profile.scope_json->'site_ids') = 1
                  THEN jsonb_build_object('site_id', profile.scope_json->'site_ids'->0)
              ELSE profile.scope_json - 'scope_schema_version'
          END
          AND (
              (profile.mode = 'FULL_FLEET' AND NEW.mode_params_jsonb = '{}'::JSONB)
              OR
              (
                  profile.mode = 'FIXED_KW'
                  AND (NEW.mode_params_jsonb->>'target_kw')::NUMERIC = profile.target_kw
                  AND (NEW.mode_params_jsonb->>'tolerance_kw')::NUMERIC = COALESCE(profile.tolerance_kw, 0)
              )
          );

        IF FOUND THEN
            NEW.decision_snapshot_jsonb := NEW.decision_snapshot_jsonb || jsonb_build_object(
                'response_profile_id', bound_profile_id,
                'response_profile_revision', bound_profile_revision::TEXT
            );
        END IF;
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER bind_legacy_curtailment_automation_event_revision
    BEFORE INSERT ON curtailment_event
    FOR EACH ROW
    EXECUTE FUNCTION bind_legacy_curtailment_automation_event_revision();

-- A crash after event creation but before the rule records active_event_uuid
-- is recovered through the event's idempotency key. Stamp the backfilled rule
-- binding into those live event snapshots so replay can enforce the same
-- revision fence after an upgrade. The full ownership tuple prevents unrelated
-- or malformed events from inheriting a rule binding.
UPDATE curtailment_event AS event
SET decision_snapshot_jsonb = event.decision_snapshot_jsonb || jsonb_build_object(
    'response_profile_id', rule.response_profile_id,
    'response_profile_revision', rule_revision.response_profile_revision::TEXT
)
FROM curtailment_automation_rule AS rule
JOIN curtailment_automation_rule_profile_revision AS rule_revision
  ON rule_revision.automation_rule_id = rule.id
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
  AND event.scope_type = CASE
      WHEN profile.scope_json @> '{"whole_org": true}'::JSONB THEN 'whole_org'
      WHEN profile.scope_json ? 'site_id' THEN 'site'
      WHEN jsonb_typeof(profile.scope_json->'site_ids') = 'array'
          AND jsonb_array_length(profile.scope_json->'site_ids') = 1 THEN 'site'
      WHEN profile.scope_json ? 'device_identifiers' THEN 'device_list'
      ELSE 'mixed'
  END
  AND event.scope_jsonb - 'scope_schema_version' = CASE
      WHEN profile.scope_json @> '{"whole_org": true}'::JSONB THEN '{}'::JSONB
      WHEN jsonb_typeof(profile.scope_json->'site_ids') = 'array'
          AND jsonb_array_length(profile.scope_json->'site_ids') = 1
          THEN jsonb_build_object('site_id', profile.scope_json->'site_ids'->0)
      ELSE profile.scope_json - 'scope_schema_version'
  END
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
