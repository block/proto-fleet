-- Shared fresh-install baseline: final public schema through legacy 153.
-- Existing installations must use the checked offline reconciliation procedure.
-- These are final definitions, not a replay of the historical alterations.
-- PostgreSQL objects were checked against empty legacy replay; Timescale objects
-- are created through extension APIs without persisted internal IDs/catalogs.
-- Ownership and installation roles remain installer responsibilities.

CREATE EXTENSION IF NOT EXISTS timescaledb;
CREATE EXTENSION IF NOT EXISTS timescaledb_toolkit;
CREATE EXTENSION IF NOT EXISTS pg_stat_statements;

-- Functions can refer to tables defined later in this same migration.
SET LOCAL check_function_bodies = false;


-- Type: batch_status_enum
CREATE TYPE public.batch_status_enum AS ENUM (
    'PENDING',
    'PROCESSING',
    'FINISHED'
);

-- Type: device_command_status_enum
CREATE TYPE public.device_command_status_enum AS ENUM (
    'SUCCESS',
    'FAILED'
);

-- Type: device_set_type
CREATE TYPE public.device_set_type AS ENUM (
    'group',
    'rack'
);

-- Type: device_status_enum
CREATE TYPE public.device_status_enum AS ENUM (
    'ACTIVE',
    'INACTIVE',
    'OFFLINE',
    'MAINTENANCE',
    'ERROR',
    'UNKNOWN',
    'NEEDS_MINING_POOL',
    'UPDATING',
    'REBOOT_REQUIRED'
);

-- Type: pairing_status_enum
CREATE TYPE public.pairing_status_enum AS ENUM (
    'PENDING',
    'PAIRED',
    'UNPAIRED',
    'FAILED',
    'AUTHENTICATION_NEEDED',
    'DEFAULT_PASSWORD'
);

-- Type: queue_status_enum
CREATE TYPE public.queue_status_enum AS ENUM (
    'PENDING',
    'PROCESSING',
    'SUCCESS',
    'FAILED'
);

-- Type: worker_name_pool_sync_status_enum
CREATE TYPE public.worker_name_pool_sync_status_enum AS ENUM (
    'POOL_UPDATED_SUCCESSFULLY'
);

-- Function: activity_count_label(bigint, text, text)
CREATE FUNCTION public.activity_count_label(item_count bigint, singular text, plural text) RETURNS text
    LANGUAGE sql IMMUTABLE PARALLEL SAFE
    AS $$
SELECT item_count || ' ' || CASE WHEN item_count = 1 THEN singular ELSE plural END
$$;

-- Function: activity_display_label(text, text, text, jsonb, text)
CREATE FUNCTION public.activity_display_label(event_type text, scope_type text, scope_label text, metadata jsonb, description text) RETURNS text
    LANGUAGE sql IMMUTABLE PARALLEL SAFE
    AS $$
SELECT CASE
    WHEN event_type IN ('rollout_advanced', 'rollout_devices_skipped',
                        'rollout_device_failed', 'rollout_controller_timed_out')
        THEN CONCAT(
            CASE event_type
                WHEN 'rollout_advanced'             THEN 'Advanced firmware update'
                WHEN 'rollout_devices_skipped'      THEN 'Skipped firmware update targets'
                WHEN 'rollout_device_failed'        THEN 'Firmware update target failed'
                WHEN 'rollout_controller_timed_out' THEN 'Firmware update controller timed out'
            END,
            COALESCE(
                ': ' || NULLIF(CONCAT_WS(' ',
                    metadata->>'channel_name',
                    metadata->>'manufacturer',
                    metadata->>'model',
                    CASE WHEN metadata->>'firmware_version' IS NOT NULL
                         THEN '→ ' || (metadata->>'firmware_version') END
                ), ''),
                ''
            )
        )
    ELSE activity_display_label_v150(event_type, scope_type, scope_label, metadata, description)
END
$$;

-- Function: activity_display_label_v114(text, text, text, jsonb, text)
CREATE FUNCTION public.activity_display_label_v114(event_type text, scope_type text, scope_label text, metadata jsonb, description text) RETURNS text
    LANGUAGE sql IMMUTABLE PARALLEL SAFE
    AS $_$
WITH counts AS (
    SELECT
        CASE WHEN jsonb_typeof(metadata->'success_count') = 'number'
             THEN floor((metadata->>'success_count')::numeric)::bigint END AS success_count,
        CASE WHEN jsonb_typeof(metadata->'failure_count') = 'number'
             THEN floor((metadata->>'failure_count')::numeric)::bigint END AS failure_count,
        CASE WHEN jsonb_typeof(metadata->'skipped_count') = 'number'
             THEN floor((metadata->>'skipped_count')::numeric)::bigint END AS skipped_count,
        CASE WHEN jsonb_typeof(metadata->'site_id') = 'number'
             THEN floor((metadata->>'site_id')::numeric)::bigint END AS site_id,
        CASE WHEN jsonb_typeof(metadata->'building_id') = 'number'
             THEN floor((metadata->>'building_id')::numeric)::bigint END AS building_id,
        CASE WHEN jsonb_typeof(metadata->'deleted_building_count') = 'number'
             THEN floor((metadata->>'deleted_building_count')::numeric)::bigint END AS deleted_building_count,
        CASE WHEN jsonb_typeof(metadata->'unassigned_rack_count') = 'number'
             THEN floor((metadata->>'unassigned_rack_count')::numeric)::bigint END AS unassigned_rack_count,
        CASE WHEN jsonb_typeof(metadata->'unassigned_device_count') = 'number'
             THEN floor((metadata->>'unassigned_device_count')::numeric)::bigint END AS unassigned_device_count,
        CASE WHEN jsonb_typeof(metadata->'deleted_response_profile_count') = 'number'
             THEN floor((metadata->>'deleted_response_profile_count')::numeric)::bigint END AS deleted_response_profile_count
),
base AS (
    SELECT CASE
        WHEN event_type = 'login' THEN 'Logged in'
        WHEN event_type = 'login_failed' THEN 'Couldn''t log in'
        WHEN event_type = 'logout' THEN 'Logged out'
        WHEN event_type = 'create_admin_user' THEN 'Created admin account'
        WHEN event_type = 'create_user' THEN CONCAT('Created user', COALESCE(': ' || COALESCE(metadata->>'target_username', scope_label), ''))
        WHEN event_type = 'update_username' THEN 'Updated username'
        WHEN event_type = 'step_up_auth_failed' THEN 'Couldn''t verify authentication'
        WHEN event_type = 'update_password' THEN 'Updated password'
        WHEN event_type = 'reset_password' THEN CONCAT('Reset password', COALESCE(' for ' || COALESCE(metadata->>'target_username', scope_label), ''))
        WHEN event_type = 'deactivate_user' THEN CONCAT('Deactivated user', COALESCE(': ' || COALESCE(metadata->>'target_username', scope_label), ''))
        WHEN event_type = 'update_user_role' THEN COALESCE('Updated role for ' || COALESCE(metadata->>'target_username', scope_label), 'Updated user role')
        WHEN event_type = 'create_api_key' THEN 'Created API key'
        WHEN event_type = 'revoke_api_key' THEN 'Revoked API key'

        WHEN event_type = 'start_mining.completed' THEN 'Started mining'
        WHEN event_type = 'stop_mining.completed' THEN 'Stopped mining'
        WHEN event_type = 'reboot.completed' THEN 'Rebooted miners'
        WHEN event_type = 'blink_led.completed' THEN 'Blinked LEDs'
        WHEN event_type = 'download_logs.completed' THEN 'Downloaded logs'
        WHEN event_type = 'set_power_target.completed' THEN 'Updated power target'
        WHEN event_type = 'set_cooling_mode.completed' THEN 'Updated cooling mode'
        WHEN event_type = 'update_mining_pools.completed' THEN 'Updated mining pools'
        WHEN event_type = 'update_miner_password.completed' THEN 'Updated miner password'
        WHEN event_type = 'firmware_update.completed' THEN 'Updated firmware'
        WHEN event_type = 'unpair.completed' THEN 'Unpaired miners'
        WHEN event_type = 'curtail.completed' THEN 'Started curtailment'
        WHEN event_type = 'uncurtail.completed' THEN 'Ended curtailment'

        WHEN event_type = 'start_mining' THEN 'Starting mining'
        WHEN event_type = 'stop_mining' THEN 'Stopping mining'
        WHEN event_type = 'reboot' THEN 'Rebooting miners'
        WHEN event_type = 'blink_led' THEN 'Blinking LEDs'
        WHEN event_type = 'download_logs' THEN 'Downloading logs'
        WHEN event_type = 'set_power_target' THEN 'Updating power target'
        WHEN event_type = 'set_cooling_mode' THEN 'Updating cooling mode'
        WHEN event_type = 'update_mining_pools' THEN 'Updating mining pools'
        WHEN event_type = 'update_miner_password' THEN 'Updating miner password'
        WHEN event_type = 'firmware_update' THEN 'Updating firmware'
        WHEN event_type = 'unpair' THEN 'Unpairing miners'
        WHEN event_type = 'curtail' THEN 'Starting curtailment'
        WHEN event_type = 'uncurtail' THEN 'Ending curtailment'

        WHEN event_type = 'create_collection' THEN CONCAT('Created ', COALESCE(scope_type, 'collection'), COALESCE(': ' || scope_label, ''))
        WHEN event_type = 'update_collection' THEN CONCAT('Updated ', COALESCE(scope_type, 'collection'), COALESCE(': ' || scope_label, ''))
        WHEN event_type = 'delete_collection' THEN CONCAT('Deleted ', COALESCE(scope_type, 'collection'), COALESCE(': ' || scope_label, ''))
        WHEN event_type = 'add_devices' THEN CONCAT('Added miners to group', COALESCE(': ' || scope_label, ''))
        WHEN event_type = 'remove_devices' THEN CONCAT('Removed miners from group', COALESCE(': ' || scope_label, ''))
        -- The server reuses assign_devices_to_rack for the clear-rack path
        -- ("Cleared devices from rack"); mirror the client and don't report
        -- the opposite action.
        WHEN event_type = 'assign_devices_to_rack' THEN
            CASE WHEN description ~* '^cleared\y'
                 THEN CONCAT('Cleared miners from rack', COALESCE(': ' || COALESCE(scope_label, TRIM(substring(description from ':\s*(.+)$'))), ''))
                 ELSE CONCAT('Assigned miners to rack', COALESCE(': ' || COALESCE(scope_label, TRIM(substring(description from ':\s*(.+)$'))), ''))
            END
        WHEN event_type IN ('set_rack_slot', 'clear_rack_slot') THEN CONCAT('Updated rack position', COALESCE(': ' || scope_label, ''))
        WHEN event_type = 'save_rack' THEN CONCAT('Saved rack', COALESCE(': ' || scope_label, ''))
        WHEN event_type = 'unpair_miners' THEN 'Unpaired miners'
        WHEN event_type = 'rename_miners' THEN 'Renamed miners'

        WHEN event_type = 'create_pool' THEN CONCAT('Created pool', COALESCE(': ' || COALESCE(metadata->>'pool_name', scope_label), ''))
        WHEN event_type = 'update_pool' THEN CONCAT('Updated pool', COALESCE(': ' || COALESCE(metadata->>'pool_name', scope_label), ''))
        WHEN event_type = 'delete_pool' THEN CONCAT('Deleted pool', COALESCE(': ' || COALESCE(metadata->>'pool_name', scope_label), ''))
        WHEN event_type = 'create_role' THEN CONCAT('Created role', COALESCE(': ' || COALESCE(metadata->>'role_name', scope_label), ''))
        WHEN event_type = 'update_role' THEN CONCAT('Updated role', COALESCE(': ' || COALESCE(metadata->>'role_name', scope_label), ''))
        WHEN event_type = 'delete_role' THEN CONCAT('Deleted role', COALESCE(': ' || COALESCE(metadata->>'role_name', scope_label), ''))
        WHEN event_type = 'site.created' THEN CONCAT('Created site', COALESCE(': ' || COALESCE(metadata->>'site_name', scope_label), ''))
        WHEN event_type = 'site.updated' THEN CONCAT('Updated site', COALESCE(': ' || COALESCE(metadata->>'site_name', scope_label), ''))
        -- Mirrors formatDeletedSite: "Deleted site 42: 1 building, 4 racks
        -- unassigned, 9 miners unassigned, 2 response profiles deleted".
        -- DeleteSite logs the site ID only in the description, so fall back
        -- to parsing it there, matching the client.
        WHEN event_type = 'site.deleted' THEN
            CASE WHEN counts.deleted_building_count IS NULL
                      AND counts.unassigned_rack_count IS NULL
                      AND counts.unassigned_device_count IS NULL
                      AND counts.deleted_response_profile_count IS NULL
                 THEN 'Deleted site'
                 ELSE CONCAT(
                     'Deleted site',
                     COALESCE(' ' || COALESCE(counts.site_id::text, substring(description from 'Deleted site (\d+)')), ''),
                     ': ',
                     CONCAT_WS(', ',
                         CASE WHEN counts.deleted_building_count IS NOT NULL
                              THEN activity_count_label(counts.deleted_building_count, 'building', 'buildings') END,
                         CASE WHEN counts.unassigned_rack_count IS NOT NULL
                              THEN activity_count_label(counts.unassigned_rack_count, 'rack', 'racks') || ' unassigned' END,
                         CASE WHEN counts.unassigned_device_count IS NOT NULL
                              THEN activity_count_label(counts.unassigned_device_count, 'miner', 'miners') || ' unassigned' END,
                         CASE WHEN counts.deleted_response_profile_count IS NOT NULL
                              THEN activity_count_label(counts.deleted_response_profile_count, 'response profile', 'response profiles') || ' deleted' END
                     )
                 )
            END
        WHEN event_type = 'building.created' THEN CONCAT('Created building', COALESCE(': ' || COALESCE(metadata->>'building_name', scope_label), ''))
        WHEN event_type = 'building.updated' THEN CONCAT('Updated building', COALESCE(': ' || COALESCE(metadata->>'building_name', scope_label), ''))
        -- Mirrors formatDeletedBuilding: "Deleted building 7: 3 racks unassigned".
        WHEN event_type = 'building.deleted' THEN
            CASE WHEN counts.unassigned_rack_count IS NULL
                 THEN 'Deleted building'
                 ELSE CONCAT(
                     'Deleted building',
                     COALESCE(' ' || COALESCE(counts.building_id::text, substring(description from 'Deleted building (\d+)')), ''),
                     ': ',
                     activity_count_label(counts.unassigned_rack_count, 'rack', 'racks'), ' unassigned'
                 )
            END
        WHEN event_type = 'building.assigned_to_site' THEN 'Assigned building to site'
        WHEN event_type = 'racks.assigned_to_site' THEN 'Assigned racks to site'
        WHEN event_type = 'building.rack_assigned' THEN 'Assigned racks to building'
        WHEN event_type = 'devices.reassigned_to_site' THEN 'Reassigned miners to site'
        WHEN event_type = 'devices.reassigned_to_building' THEN 'Reassigned miners to building'

        -- Schedule descriptions carry the name in quotes ('Schedule "Night
        -- Shift" executed ...'); the client appends it via quotedTarget().
        WHEN event_type = 'schedule_executed' THEN CONCAT('Ran schedule', COALESCE(': ' || substring(description from '"([^"]+)"'), ''))
        WHEN event_type = 'schedule_window_ended' THEN CONCAT('Ended schedule window', COALESCE(': ' || substring(description from '"([^"]+)"'), ''))
        WHEN event_type = 'schedule_completed' THEN CONCAT('Completed schedule', COALESCE(': ' || substring(description from '"([^"]+)"'), ''))
        WHEN event_type = 'schedule_conflict_skip' THEN CONCAT('Skipped schedule conflict', COALESCE(': ' || substring(description from '"([^"]+)"'), ''))
        WHEN event_type = 'schedule_skipped_due_to_curtailment' THEN CONCAT('Skipped schedule during curtailment', COALESCE(': ' || substring(description from '"([^"]+)"'), ''))
        WHEN event_type = 'curtailment_started' THEN 'Started curtailment'
        WHEN event_type = 'curtailment_admin_terminated' THEN 'Stopped curtailment'
        WHEN event_type = 'curtailment_admin_terminated_replay' THEN 'Curtailment already stopped'
        WHEN event_type = 'curtailment_updated' THEN 'Updated curtailment'
        WHEN event_type = 'curtailment_force_released' THEN 'Released curtailment ownership'
        -- Mirror the client's skipped-count suffixes.
        WHEN event_type = 'command_preflight_blocked' THEN
            CASE WHEN counts.skipped_count IS NULL
                 THEN 'Command couldn''t run'
                 ELSE CONCAT('Command couldn''t run: ', activity_count_label(counts.skipped_count, 'miner', 'miners'), ' excluded by filters')
            END
        WHEN event_type = 'command_filter_skip' THEN
            CASE WHEN counts.skipped_count IS NULL
                 THEN 'Command ran with skipped miners'
                 ELSE CONCAT('Command ran with ', activity_count_label(counts.skipped_count, 'miner', 'miners'), ' skipped')
            END
    END AS label
    FROM counts
)
-- Completed commands render with a completion ratio in the client
-- (formatCompletedCommand: "Rebooted miners: 2/3 miners completed"), so mirror
-- the metadata-derived suffix for searchability.
SELECT CASE
    WHEN base.label IS NULL THEN NULL
    WHEN event_type LIKE '%.completed'
         AND counts.success_count IS NOT NULL
         AND counts.failure_count IS NOT NULL
         AND counts.success_count + counts.failure_count > 0
    THEN CONCAT(
        base.label, ': ',
        counts.success_count, '/', counts.success_count + counts.failure_count,
        CASE WHEN counts.success_count + counts.failure_count = 1
             THEN ' miner completed' ELSE ' miners completed' END
    )
    ELSE base.label
END
FROM base, counts
$_$;

-- Function: activity_display_label_v146(text, text, text, jsonb, text)
CREATE FUNCTION public.activity_display_label_v146(event_type text, scope_type text, scope_label text, metadata jsonb, description text) RETURNS text
    LANGUAGE sql IMMUTABLE PARALLEL SAFE
    AS $$
SELECT CASE
    WHEN event_type = 'cli_reset_password'
        THEN CONCAT(
            'Break-glass password reset',
            COALESCE(' for ' || COALESCE(metadata->>'target_username', scope_label), '')
        )
    ELSE activity_display_label_v114(event_type, scope_type, scope_label, metadata, description)
END
$$;

-- Function: activity_display_label_v147(text, text, text, jsonb, text)
CREATE FUNCTION public.activity_display_label_v147(event_type text, scope_type text, scope_label text, metadata jsonb, description text) RETURNS text
    LANGUAGE sql IMMUTABLE PARALLEL SAFE
    AS $$
SELECT CASE event_type
    WHEN 'maintenance.ticket_created' THEN 'Created repair ticket'
    WHEN 'maintenance.ticket_updated' THEN 'Updated repair ticket'
    WHEN 'maintenance.ticket_deleted' THEN 'Deleted repair ticket'
    WHEN 'maintenance.ticket_bulk_update' THEN 'Bulk updated repair tickets'
    WHEN 'maintenance.comment_created' THEN 'Added repair ticket comment'
    WHEN 'maintenance.comment_deleted' THEN 'Deleted repair ticket comment'
    WHEN 'inventory.part_created' THEN 'Created inventory part'
    WHEN 'inventory.part_updated' THEN 'Updated inventory part'
    WHEN 'inventory.part_deleted' THEN 'Deleted inventory part'
    WHEN 'inventory.parts_imported' THEN 'Imported inventory parts'
    ELSE activity_display_label_v146(event_type, scope_type, scope_label, metadata, description)
END
$$;

-- Function: activity_display_label_v150(text, text, text, jsonb, text)
CREATE FUNCTION public.activity_display_label_v150(event_type text, scope_type text, scope_label text, metadata jsonb, description text) RETURNS text
    LANGUAGE sql IMMUTABLE PARALLEL SAFE
    AS $$
SELECT CASE
    WHEN event_type LIKE 'rollout_%'
        THEN CONCAT(
            CASE event_type
                WHEN 'rollout_started'                 THEN 'Started firmware update'
                WHEN 'rollout_review_ready'            THEN 'Firmware update ready for review'
                WHEN 'rollout_continued'               THEN 'Continued firmware update'
                WHEN 'rollout_paused'                  THEN 'Paused firmware update'
                WHEN 'rollout_resumed'                 THEN 'Resumed firmware update'
                WHEN 'rollout_canceled'                THEN 'Canceled remaining firmware updates'
                WHEN 'rollout_completed'               THEN 'Completed firmware update'
                WHEN 'rollout_completed_with_failures' THEN 'Completed firmware update with failures'
                WHEN 'rollout_retried'                 THEN 'Retried failed firmware updates'
                ELSE 'Firmware update'
            END,
            COALESCE(
                ': ' || NULLIF(CONCAT_WS(' ',
                    metadata->>'channel_name',
                    metadata->>'manufacturer',
                    metadata->>'model',
                    CASE WHEN metadata->>'firmware_version' IS NOT NULL
                         THEN '→ ' || (metadata->>'firmware_version') END
                ), ''),
                ''
            )
        )
    ELSE activity_display_label_v147(event_type, scope_type, scope_label, metadata, description)
END
$$;

-- Function: bind_legacy_curtailment_automation_event_revision()
CREATE FUNCTION public.bind_legacy_curtailment_automation_event_revision() RETURNS trigger
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

-- Function: canonicalize_curtailment_response_profile_scope()
CREATE FUNCTION public.canonicalize_curtailment_response_profile_scope() RETURNS trigger
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

-- Function: clear_infrastructure_rack_on_placement_change()
CREATE FUNCTION public.clear_infrastructure_rack_on_placement_change() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
    IF OLD.site_id IS DISTINCT FROM NEW.site_id
        OR OLD.building_id IS DISTINCT FROM NEW.building_id THEN
        UPDATE infrastructure_device AS infrastructure
        SET rack_name = ''
        FROM device_set AS rack
        WHERE rack.id = NEW.device_set_id
          AND infrastructure.org_id = rack.org_id
          AND infrastructure.rack_name = rack.label
          AND infrastructure.deleted_at IS NULL;
    END IF;

    RETURN NEW;
END;
$$;

-- Function: firmware_rollout_bump_revision()
CREATE FUNCTION public.firmware_rollout_bump_revision() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
DECLARE
    txid BIGINT := pg_current_xact_id()::text::bigint;
BEGIN
    IF TG_OP = 'INSERT' THEN
        NEW.revision_txid = txid;
        NEW.updated_at = clock_timestamp();
    ELSE
        IF OLD.revision_txid <> txid THEN
            NEW.revision = OLD.revision + 1;
            NEW.revision_txid = txid;
        END IF;
        -- Later statements in this transaction can also touch the rollout.
        -- Keep their timestamps monotonic even though revision does not bump.
        NEW.updated_at = GREATEST(OLD.updated_at, clock_timestamp());
    END IF;
    RETURN NEW;
END;
$$;

-- Function: firmware_rollout_touch_from_rows()
CREATE FUNCTION public.firmware_rollout_touch_from_rows() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
    IF TG_OP = 'UPDATE' THEN
        UPDATE firmware_rollout
        SET revision = revision + 1
        WHERE id IN (SELECT rollout_id FROM changed_rows UNION SELECT rollout_id FROM previous_rows)
          AND revision_txid <> pg_current_xact_id()::text::bigint;
    ELSE
        UPDATE firmware_rollout
        SET revision = revision + 1
        WHERE id IN (SELECT rollout_id FROM changed_rows)
          AND revision_txid <> pg_current_xact_id()::text::bigint;
    END IF;
    RETURN NULL;
END;
$$;

-- Function: fleet_slow_statements()
CREATE FUNCTION public.fleet_slow_statements() RETURNS TABLE(query text, calls bigint, total_exec_time double precision, mean_exec_time double precision, max_exec_time double precision, rows bigint)
    LANGUAGE sql SECURITY DEFINER
    SET search_path TO 'pg_catalog', 'pg_temp'
    AS $$
    SELECT s.query, s.calls, s.total_exec_time, s.mean_exec_time,
           s.max_exec_time, s.rows
    FROM public.pg_stat_statements s
    WHERE s.dbid = (SELECT oid FROM pg_catalog.pg_database WHERE datname = current_database())
$$;

-- Function: notification_active_sync()
CREATE FUNCTION public.notification_active_sync() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
DECLARE
    key TEXT;
    ev  TIMESTAMPTZ;
BEGIN
    -- Unscoped (NULL org) alerts never surface in the per-org active card; skip them.
    IF NEW.organization_id IS NULL THEN
        RETURN NEW;
    END IF;
    -- Only a forged post breaches these. Skip rather than truncate: the rollup groups on these columns, so a
    -- prefix would merge two rules, and a chr(31) in one part hashes onto the identity of another split.
    IF LENGTH(NEW.alert_name) > 190 OR LENGTH(NEW.rule_group) > 190 OR LENGTH(NEW.device_id) > 255
       OR strpos(NEW.alert_name, chr(31)) > 0
       OR strpos(NEW.rule_group, chr(31)) > 0
       OR strpos(NEW.device_id, chr(31)) > 0 THEN
        RETURN NEW;
    END IF;
    key := md5(COALESCE(
        NULLIF(NEW.fingerprint, ''),
        NEW.alert_name || chr(31) || NEW.rule_group || chr(31) || NEW.device_id
    ));
    ev := CASE
              WHEN NEW.status = 'firing' THEN COALESCE(NEW.starts_at, NEW.received_at)
              ELSE COALESCE(NEW.ends_at, NEW.received_at)
          END;
    INSERT INTO notification_active (
        organization_id, alert_key, history_id, received_at, status, event_at, alert_name,
        severity, rule_group, fingerprint, device_id, template, summary, starts_at, ends_at
    ) VALUES (
        NEW.organization_id, key, NEW.id, NEW.received_at, NEW.status, ev, NEW.alert_name,
        NEW.severity, NEW.rule_group, NEW.fingerprint, NEW.device_id, NEW.template, NEW.summary,
        NEW.starts_at, NEW.ends_at
    )
    ON CONFLICT (organization_id, alert_key) DO UPDATE SET
        history_id  = EXCLUDED.history_id,
        received_at = EXCLUDED.received_at,
        status      = EXCLUDED.status,
        event_at    = EXCLUDED.event_at,
        alert_name  = EXCLUDED.alert_name,
        severity    = EXCLUDED.severity,
        rule_group  = EXCLUDED.rule_group,
        fingerprint = EXCLUDED.fingerprint,
        device_id   = EXCLUDED.device_id,
        template    = EXCLUDED.template,
        summary     = EXCLUDED.summary,
        starts_at   = EXCLUDED.starts_at,
        ends_at     = EXCLUDED.ends_at
    WHERE notification_active.event_at < EXCLUDED.event_at
       OR (notification_active.event_at = EXCLUDED.event_at
           AND notification_active.history_id < EXCLUDED.history_id);
    RETURN NEW;
END;
$$;

-- Function: release_channel_pair_key(text)
CREATE FUNCTION public.release_channel_pair_key(text) RETURNS text
    LANGUAGE sql IMMUTABLE PARALLEL SAFE
    AS $_$
    SELECT lower(btrim(COALESCE($1, ''),
        E' \t\n\u000b\f\r\u0085\u00a0\u1680\u2000\u2001\u2002\u2003\u2004\u2005\u2006\u2007\u2008\u2009\u200a\u2028\u2029\u202f\u205f\u3000') COLLATE "C")
$_$;

-- Function: sync_curtailment_automation_rule_profile_revision()
CREATE FUNCTION public.sync_curtailment_automation_rule_profile_revision() RETURNS trigger
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

-- Function: sync_curtailment_response_profile_revision()
CREATE FUNCTION public.sync_curtailment_response_profile_revision() RETURNS trigger
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

-- Function: sync_infrastructure_rack_label()
CREATE FUNCTION public.sync_infrastructure_rack_label() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
    IF OLD.type = 'rack' AND OLD.deleted_at IS NULL AND NEW.deleted_at IS NOT NULL THEN
        UPDATE infrastructure_device
        SET rack_name = ''
        WHERE org_id = OLD.org_id
          AND rack_name = OLD.label
          AND deleted_at IS NULL;
    ELSIF OLD.type = 'rack' AND OLD.label IS DISTINCT FROM NEW.label THEN
        UPDATE infrastructure_device
        SET rack_name = NEW.label
        WHERE org_id = OLD.org_id
          AND rack_name = OLD.label
          AND deleted_at IS NULL;
    END IF;

    RETURN NEW;
END;
$$;

-- Function: update_last_seen_column()
CREATE FUNCTION public.update_last_seen_column() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
    NEW.last_seen = CURRENT_TIMESTAMP;
    RETURN NEW;
END;
$$;

-- Function: update_repair_ticket_version()
CREATE FUNCTION public.update_repair_ticket_version() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
    NEW.updated_at = GREATEST(clock_timestamp(), OLD.updated_at + INTERVAL '1 microsecond');
    RETURN NEW;
END;
$$;

-- Function: update_updated_at_column()
CREATE FUNCTION public.update_updated_at_column() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
    NEW.updated_at = CURRENT_TIMESTAMP;
    RETURN NEW;
END;
$$;



SET default_table_access_method = heap;

-- Table: notification_metric_sample
CREATE TABLE public.notification_metric_sample (
    "time" timestamp with time zone NOT NULL,
    metric text NOT NULL,
    organization_id text DEFAULT ''::text NOT NULL,
    site_id text DEFAULT ''::text NOT NULL,
    device_id text DEFAULT ''::text NOT NULL,
    device_group text DEFAULT ''::text NOT NULL,
    driver text DEFAULT ''::text NOT NULL,
    sensor_kind text DEFAULT ''::text NOT NULL,
    kind text DEFAULT ''::text NOT NULL,
    result text DEFAULT ''::text NOT NULL,
    value double precision NOT NULL
);

-- Table: miner_state_snapshots
CREATE TABLE public.miner_state_snapshots (
    "time" timestamp with time zone NOT NULL,
    org_id bigint NOT NULL,
    device_identifier text NOT NULL,
    state smallint NOT NULL,
    site_id bigint
);

-- Table: device_metrics
CREATE TABLE public.device_metrics (
    "time" timestamp with time zone NOT NULL,
    device_identifier text NOT NULL,
    hash_rate_hs double precision,
    hash_rate_hs_kind text,
    temp_c double precision,
    temp_c_kind text,
    fan_rpm double precision,
    fan_rpm_kind text,
    power_w double precision,
    power_w_kind text,
    efficiency_jh double precision,
    efficiency_jh_kind text,
    voltage_v double precision,
    voltage_v_kind text,
    current_a double precision,
    current_a_kind text,
    inlet_temp_c double precision,
    outlet_temp_c double precision,
    ambient_temp_c double precision,
    chip_count integer,
    chip_count_kind text,
    chip_frequency_mhz double precision,
    health text,
    site_id bigint
);

-- Table: activity_log
CREATE TABLE public.activity_log (
    id bigint NOT NULL,
    event_id uuid NOT NULL,
    event_category text NOT NULL,
    event_type text NOT NULL,
    description text NOT NULL,
    result text DEFAULT 'success'::text NOT NULL,
    error_message text,
    scope_type text,
    scope_label text,
    scope_count integer,
    actor_type text DEFAULT 'user'::text NOT NULL,
    user_id text,
    username text,
    organization_id bigint,
    metadata jsonb,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    batch_id text,
    site_id bigint,
    multi_site boolean DEFAULT false NOT NULL,
    CONSTRAINT ck_activity_log_multi_site_requires_null_site CHECK (((NOT multi_site) OR (site_id IS NULL))),
    CONSTRAINT ck_activity_log_site_requires_org CHECK (((site_id IS NULL) OR (organization_id IS NOT NULL)))
);

-- Sequence: activity_log_id_seq
CREATE SEQUENCE public.activity_log_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

-- Sequence Owned By: activity_log_id_seq
ALTER SEQUENCE public.activity_log_id_seq OWNED BY public.activity_log.id;

-- Table: activity_log_site
CREATE TABLE public.activity_log_site (
    activity_log_id bigint NOT NULL,
    org_id bigint NOT NULL,
    site_id bigint
);

-- Table: fleet_node
CREATE TABLE public.fleet_node (
    id bigint CONSTRAINT agent_id_not_null NOT NULL,
    org_id bigint CONSTRAINT agent_org_id_not_null NOT NULL,
    name character varying(255) CONSTRAINT agent_name_not_null NOT NULL,
    identity_pubkey bytea CONSTRAINT agent_identity_pubkey_not_null NOT NULL,
    enrollment_status character varying(32) DEFAULT 'PENDING'::character varying CONSTRAINT agent_enrollment_status_not_null NOT NULL,
    last_seen_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP CONSTRAINT agent_created_at_not_null NOT NULL,
    updated_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP CONSTRAINT agent_updated_at_not_null NOT NULL,
    deleted_at timestamp with time zone,
    encryption_pubkey bytea NOT NULL,
    CONSTRAINT ck_fleet_node_encryption_pubkey_len CHECK ((length(encryption_pubkey) = 32)),
    CONSTRAINT ck_fleet_node_enrollment_status CHECK (enrollment_status IN ('PENDING', 'CONFIRMED', 'REVOKED'))
);

-- Sequence: agent_id_seq
CREATE SEQUENCE public.agent_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

-- Sequence Owned By: agent_id_seq
ALTER SEQUENCE public.agent_id_seq OWNED BY public.fleet_node.id;

-- Table: alert_channel
CREATE TABLE public.alert_channel (
    id bigint NOT NULL,
    org_id bigint NOT NULL,
    name text NOT NULL,
    kind text NOT NULL,
    encrypted_config text NOT NULL,
    validation_state text DEFAULT 'pending'::text NOT NULL,
    validated_at timestamp with time zone,
    validation_error text DEFAULT ''::text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    deleted_at timestamp with time zone
);

-- Sequence: alert_channel_id_seq
CREATE SEQUENCE public.alert_channel_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

-- Sequence Owned By: alert_channel_id_seq
ALTER SEQUENCE public.alert_channel_id_seq OWNED BY public.alert_channel.id;

-- Table: alert_maintenance_window
CREATE TABLE public.alert_maintenance_window (
    id bigint NOT NULL,
    org_id bigint NOT NULL,
    rule_uids text[] DEFAULT '{}'::text[] NOT NULL,
    channel_ids bigint[] DEFAULT '{}'::bigint[] NOT NULL,
    starts_at timestamp with time zone NOT NULL,
    ends_at timestamp with time zone NOT NULL,
    comment text DEFAULT ''::text NOT NULL,
    created_by text DEFAULT ''::text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);

-- Sequence: alert_maintenance_window_id_seq
CREATE SEQUENCE public.alert_maintenance_window_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

-- Sequence Owned By: alert_maintenance_window_id_seq
ALTER SEQUENCE public.alert_maintenance_window_id_seq OWNED BY public.alert_maintenance_window.id;

-- Table: alert_route_channel
CREATE TABLE public.alert_route_channel (
    policy_id bigint NOT NULL,
    channel_id bigint NOT NULL
);

-- Table: alert_route_policy
CREATE TABLE public.alert_route_policy (
    id bigint NOT NULL,
    org_id bigint NOT NULL,
    rule_uid text NOT NULL,
    mode text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);

-- Sequence: alert_route_policy_id_seq
CREATE SEQUENCE public.alert_route_policy_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

-- Sequence Owned By: alert_route_policy_id_seq
ALTER SEQUENCE public.alert_route_policy_id_seq OWNED BY public.alert_route_policy.id;

-- Table: alert_rule_config
CREATE TABLE public.alert_rule_config (
    org_id bigint NOT NULL,
    rule_uid text NOT NULL,
    config jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);

-- Table: api_key
CREATE TABLE public.api_key (
    id bigint NOT NULL,
    key_id character varying(36) NOT NULL,
    name character varying(255) NOT NULL,
    prefix character varying(12) NOT NULL,
    key_hash text NOT NULL,
    user_id bigint,
    organization_id bigint NOT NULL,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    expires_at timestamp with time zone,
    revoked_at timestamp with time zone,
    last_used_at timestamp with time zone,
    fleet_node_id bigint,
    subject_kind character varying(16) DEFAULT 'user'::character varying NOT NULL,
    CONSTRAINT ck_api_key_subject CHECK (((((subject_kind)::text = 'user'::text) AND (user_id IS NOT NULL) AND (fleet_node_id IS NULL)) OR (((subject_kind)::text = 'fleet_node'::text) AND (user_id IS NULL) AND (fleet_node_id IS NOT NULL))))
);

-- Sequence: api_key_id_seq
CREATE SEQUENCE public.api_key_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

-- Sequence Owned By: api_key_id_seq
ALTER SEQUENCE public.api_key_id_seq OWNED BY public.api_key.id;

-- Table: building
CREATE TABLE public.building (
    id bigint NOT NULL,
    org_id bigint NOT NULL,
    site_id bigint,
    name character varying(255) NOT NULL,
    description text,
    power_kw numeric(10,3),
    overhead_kw numeric(10,3),
    aisles integer,
    physical_rack_count integer,
    racks_per_aisle integer,
    default_rack_rows integer,
    default_rack_columns integer,
    default_rack_order_index smallint DEFAULT 0 NOT NULL,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    updated_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    deleted_at timestamp with time zone,
    CONSTRAINT ck_building_aisles_nonneg CHECK (((aisles IS NULL) OR (aisles >= 0))),
    CONSTRAINT ck_building_default_rack_dims CHECK ((((default_rack_rows IS NULL) AND (default_rack_columns IS NULL)) OR ((default_rack_rows IS NOT NULL) AND (default_rack_columns IS NOT NULL) AND (default_rack_rows > 0) AND (default_rack_columns > 0)))),
    CONSTRAINT ck_building_default_rack_order_index CHECK (((default_rack_order_index >= 0) AND (default_rack_order_index <= 4))),
    CONSTRAINT ck_building_physical_rack_count_nonneg CHECK (((physical_rack_count IS NULL) OR (physical_rack_count >= 0))),
    CONSTRAINT ck_building_racks_per_aisle_nonneg CHECK (((racks_per_aisle IS NULL) OR (racks_per_aisle >= 0)))
);

-- Sequence: building_id_seq
CREATE SEQUENCE public.building_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

-- Sequence Owned By: building_id_seq
ALTER SEQUENCE public.building_id_seq OWNED BY public.building.id;

-- Table: command_batch_log
CREATE TABLE public.command_batch_log (
    id bigint NOT NULL,
    uuid character varying(36) NOT NULL,
    type text NOT NULL,
    created_by bigint NOT NULL,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    started_at timestamp with time zone,
    finished_at timestamp with time zone,
    status public.batch_status_enum NOT NULL,
    devices_count integer DEFAULT 0 NOT NULL,
    payload jsonb,
    organization_id bigint
);

-- Sequence: command_batch_log_id_seq
CREATE SEQUENCE public.command_batch_log_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

-- Sequence Owned By: command_batch_log_id_seq
ALTER SEQUENCE public.command_batch_log_id_seq OWNED BY public.command_batch_log.id;

-- Table: command_on_device_log
CREATE TABLE public.command_on_device_log (
    id bigint NOT NULL,
    command_batch_log_id bigint NOT NULL,
    device_id bigint NOT NULL,
    status public.device_command_status_enum NOT NULL,
    updated_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    error_info text,
    custom_name text,
    manufacturer text,
    model text,
    ip_address text,
    mac_address text,
    org_id bigint NOT NULL,
    site_id bigint
);

-- Sequence: command_on_device_log_id_seq
CREATE SEQUENCE public.command_on_device_log_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

-- Sequence Owned By: command_on_device_log_id_seq
ALTER SEQUENCE public.command_on_device_log_id_seq OWNED BY public.command_on_device_log.id;

-- View: connected_postgres_identity
CREATE VIEW public.connected_postgres_identity AS
 SELECT COALESCE(host(inet_server_addr()), '')::TEXT AS server_address,
    COALESCE(inet_server_port(), 0)::INTEGER AS server_port,
    pg_is_in_recovery() AS in_recovery,
    ((pg_control_checkpoint()).timeline_id)::bigint AS timeline;

-- Table: curtailment_automation_rule
CREATE TABLE public.curtailment_automation_rule (
    id bigint NOT NULL,
    org_id bigint NOT NULL,
    rule_name character varying(64) NOT NULL,
    trigger_type text DEFAULT 'MQTT'::text NOT NULL,
    mqtt_source_id bigint NOT NULL,
    response_profile_id bigint NOT NULL,
    enabled boolean DEFAULT true NOT NULL,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    updated_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    CONSTRAINT ck_curtailment_automation_rule_name_nonempty CHECK ((btrim((rule_name)::text) <> ''::text)),
    CONSTRAINT ck_curtailment_automation_rule_trigger_type CHECK ((trigger_type = 'MQTT'::text))
);

-- Sequence: curtailment_automation_rule_id_seq
CREATE SEQUENCE public.curtailment_automation_rule_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

-- Sequence Owned By: curtailment_automation_rule_id_seq
ALTER SEQUENCE public.curtailment_automation_rule_id_seq OWNED BY public.curtailment_automation_rule.id;

-- Table: curtailment_automation_rule_profile_revision
CREATE TABLE public.curtailment_automation_rule_profile_revision (
    automation_rule_id bigint CONSTRAINT curtailment_automation_rule_profile_automation_rule_id_not_null NOT NULL,
    response_profile_revision uuid CONSTRAINT curtailment_automation_rule__response_profile_revision_not_null NOT NULL
);

-- Table: curtailment_automation_rule_state
CREATE TABLE public.curtailment_automation_rule_state (
    rule_id bigint NOT NULL,
    last_signal text,
    last_signal_at timestamp with time zone,
    active_event_uuid uuid,
    last_started_at timestamp with time zone,
    last_restored_at timestamp with time zone,
    last_error text,
    last_error_at timestamp with time zone,
    updated_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    CONSTRAINT ck_curtailment_automation_rule_state_signal CHECK (((last_signal IS NULL) OR (last_signal = ANY (ARRAY['OFF'::text, 'ON'::text]))))
);

-- Table: curtailment_event
CREATE TABLE public.curtailment_event (
    id bigint NOT NULL,
    event_uuid uuid NOT NULL,
    org_id bigint NOT NULL,
    state text NOT NULL,
    mode text NOT NULL,
    strategy text NOT NULL,
    level text NOT NULL,
    priority text NOT NULL,
    loop_type text NOT NULL,
    scope_type text NOT NULL,
    scope_jsonb jsonb NOT NULL,
    mode_params_jsonb jsonb DEFAULT '{}'::jsonb NOT NULL,
    restore_batch_size integer NOT NULL,
    restore_batch_interval_sec integer NOT NULL,
    effective_batch_size integer,
    min_curtailed_duration_sec integer DEFAULT 0 NOT NULL,
    max_duration_seconds integer,
    allow_unbounded boolean DEFAULT false NOT NULL,
    include_maintenance boolean DEFAULT false NOT NULL,
    force_include_maintenance boolean DEFAULT false NOT NULL,
    decision_snapshot_jsonb jsonb DEFAULT '{}'::jsonb NOT NULL,
    source_actor_type text NOT NULL,
    source_actor_id text,
    external_source text,
    external_reference text,
    idempotency_key text,
    supersedes_event_id bigint,
    reason text NOT NULL,
    scheduled_start_at timestamp with time zone,
    started_at timestamp with time zone,
    ended_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    updated_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    created_by_user_id bigint NOT NULL,
    curtail_batch_size integer,
    curtail_batch_interval_sec integer DEFAULT 0 NOT NULL,
    force_include_all_paired_miners boolean DEFAULT false NOT NULL,
    facility_fan_device_ids bigint[] DEFAULT '{}'::bigint[] NOT NULL,
    facility_fan_site_ids bigint[] DEFAULT '{}'::bigint[] NOT NULL,
    fan_off_delay_sec integer DEFAULT 0 NOT NULL,
    fan_restore_delay_sec integer DEFAULT 0 NOT NULL,
    fan_off_sent_at timestamp with time zone,
    fan_on_sent_at timestamp with time zone,
    fan_airflow_reopened_at timestamp with time zone,
    fan_last_error text,
    last_curtail_pending_dispatch_at timestamp with time zone,
    authorization_envelope_jsonb jsonb NOT NULL,
    CONSTRAINT ck_curtailment_event_all_paired_full_fleet CHECK (((NOT force_include_all_paired_miners) OR (mode = 'FULL_FLEET'::text))),
    CONSTRAINT ck_curtailment_event_curtail_batch_interval CHECK (((curtail_batch_interval_sec >= 0) AND (curtail_batch_interval_sec <= 3600))),
    CONSTRAINT ck_curtailment_event_curtail_batch_size CHECK (((curtail_batch_size IS NULL) OR ((curtail_batch_size > 0) AND (curtail_batch_size <= 10000)))),
    CONSTRAINT ck_curtailment_event_external_reference_nonempty CHECK (((external_reference IS NULL) OR (external_reference <> ''::text))),
    CONSTRAINT ck_curtailment_event_external_source_nonempty CHECK (((external_source IS NULL) OR (external_source <> ''::text))),
    CONSTRAINT ck_curtailment_event_fan_off_delay CHECK ((fan_off_delay_sec >= 0)),
    CONSTRAINT ck_curtailment_event_fan_restore_delay CHECK ((fan_restore_delay_sec >= 0)),
    CONSTRAINT ck_curtailment_event_fan_site_alignment CHECK ((cardinality(facility_fan_device_ids) = cardinality(facility_fan_site_ids))),
    CONSTRAINT ck_curtailment_event_idempotency_key_nonempty CHECK (((idempotency_key IS NULL) OR (idempotency_key <> ''::text))),
    CONSTRAINT ck_curtailment_event_maintenance_consistency CHECK ((include_maintenance = force_include_maintenance)),
    CONSTRAINT ck_curtailment_event_max_duration_bounds CHECK (((max_duration_seconds IS NULL) OR ((max_duration_seconds > 0) AND (max_duration_seconds <= 604800)))),
    CONSTRAINT ck_curtailment_event_reason_nonempty CHECK ((length(TRIM(BOTH FROM reason)) > 0)),
    CONSTRAINT ck_curtailment_event_restore_interval_bounds CHECK (((restore_batch_interval_sec >= 0) AND (restore_batch_interval_sec <= 3600))),
    CONSTRAINT curtailment_event_authorization_envelope_shape CHECK (((jsonb_typeof(authorization_envelope_jsonb) = 'object'::text) AND (authorization_envelope_jsonb ?& ARRAY['schema_version'::text, 'selected_resource_site_ids'::text, 'current_member_site_ids'::text, 'miner_scope_unbounded'::text, 'facility_fan_site_ids'::text, 'facility_fan_scope_unbounded'::text]) AND (jsonb_typeof((authorization_envelope_jsonb -> 'schema_version'::text)) = 'number'::text) AND ((authorization_envelope_jsonb ->> 'schema_version'::text) = '1'::text) AND (jsonb_typeof((authorization_envelope_jsonb -> 'selected_resource_site_ids'::text)) = 'array'::text) AND (jsonb_typeof((authorization_envelope_jsonb -> 'current_member_site_ids'::text)) = 'array'::text) AND (jsonb_typeof((authorization_envelope_jsonb -> 'miner_scope_unbounded'::text)) = 'boolean'::text) AND (jsonb_typeof((authorization_envelope_jsonb -> 'facility_fan_site_ids'::text)) = 'array'::text) AND (jsonb_typeof((authorization_envelope_jsonb -> 'facility_fan_scope_unbounded'::text)) = 'boolean'::text)))
);

-- Sequence: curtailment_event_id_seq
CREATE SEQUENCE public.curtailment_event_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

-- Sequence Owned By: curtailment_event_id_seq
ALTER SEQUENCE public.curtailment_event_id_seq OWNED BY public.curtailment_event.id;

-- Table: curtailment_mqtt_source_config
CREATE TABLE public.curtailment_mqtt_source_config (
    id bigint NOT NULL,
    organization_id bigint NOT NULL,
    service_user_id bigint NOT NULL,
    source_name character varying(64) NOT NULL,
    topic character varying(255) NOT NULL,
    broker_primary_host character varying(255) NOT NULL,
    broker_secondary_host character varying(255) NOT NULL,
    broker_port integer,
    broker_transport text DEFAULT 'tcp'::text NOT NULL,
    mqtt_username character varying(255) NOT NULL,
    mqtt_password_enc text NOT NULL,
    payload_format text DEFAULT 'target_timestamp'::text NOT NULL,
    staleness_threshold_sec integer,
    enabled boolean DEFAULT true NOT NULL,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    updated_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    CONSTRAINT ck_curtailment_mqtt_source_config_brokers_distinct CHECK ((btrim((broker_primary_host)::text) <> btrim((broker_secondary_host)::text))),
    CONSTRAINT ck_curtailment_mqtt_source_config_password_nonempty CHECK ((btrim(mqtt_password_enc) <> ''::text)),
    CONSTRAINT ck_curtailment_mqtt_source_config_port_positive CHECK (((broker_port IS NULL) OR ((broker_port > 0) AND (broker_port < 65536)))),
    CONSTRAINT ck_curtailment_mqtt_source_config_primary_host_nonempty CHECK ((btrim((broker_primary_host)::text) <> ''::text)),
    CONSTRAINT ck_curtailment_mqtt_source_config_secondary_host_nonempty CHECK ((btrim((broker_secondary_host)::text) <> ''::text)),
    CONSTRAINT ck_curtailment_mqtt_source_config_source_name_nonempty CHECK ((btrim((source_name)::text) <> ''::text)),
    CONSTRAINT ck_curtailment_mqtt_source_config_staleness_positive CHECK (((staleness_threshold_sec IS NULL) OR (staleness_threshold_sec > 0))),
    CONSTRAINT ck_curtailment_mqtt_source_config_topic_nonempty CHECK ((btrim((topic)::text) <> ''::text)),
    CONSTRAINT ck_curtailment_mqtt_source_config_transport CHECK ((broker_transport = ANY (ARRAY['tcp'::text, 'tls'::text]))),
    CONSTRAINT ck_curtailment_mqtt_source_config_username_nonempty CHECK ((btrim((mqtt_username)::text) <> ''::text))
);

-- Sequence: curtailment_mqtt_source_config_id_seq
CREATE SEQUENCE public.curtailment_mqtt_source_config_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

-- Sequence Owned By: curtailment_mqtt_source_config_id_seq
ALTER SEQUENCE public.curtailment_mqtt_source_config_id_seq OWNED BY public.curtailment_mqtt_source_config.id;

-- Table: curtailment_mqtt_source_state
CREATE TABLE public.curtailment_mqtt_source_state (
    source_config_id bigint NOT NULL,
    last_target text,
    last_target_at timestamp with time zone,
    last_processed_target text,
    last_processed_targets text[],
    last_received_at timestamp with time zone,
    last_received_broker character varying(255),
    last_edge_at timestamp with time zone,
    pending_direction text,
    pending_target text,
    pending_target_at timestamp with time zone,
    pending_received_at timestamp with time zone,
    pending_received_broker character varying(255),
    pending_prior_edge_at timestamp with time zone,
    updated_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    pending_retry_at timestamp with time zone,
    CONSTRAINT ck_curtailment_mqtt_source_state_pending_direction_valid CHECK (((pending_direction IS NULL) OR (pending_direction = ANY (ARRAY['on_to_off'::text, 'reassert_off'::text, 'off_to_on'::text, 'watchdog_off'::text])))),
    CONSTRAINT ck_curtailment_mqtt_source_state_pending_target_valid CHECK (((pending_target IS NULL) OR (pending_target = ANY (ARRAY['OFF'::text, 'ON'::text])))),
    CONSTRAINT ck_curtailment_mqtt_source_state_processed_target_valid CHECK (((last_processed_target IS NULL) OR (last_processed_target = ANY (ARRAY['OFF'::text, 'ON'::text])))),
    CONSTRAINT ck_curtailment_mqtt_source_state_processed_targets_valid CHECK (((last_processed_targets IS NULL) OR (last_processed_targets <@ ARRAY['OFF'::text, 'ON'::text]))),
    CONSTRAINT ck_curtailment_mqtt_source_state_target_valid CHECK (((last_target IS NULL) OR (last_target = ANY (ARRAY['OFF'::text, 'ON'::text]))))
);

COMMENT ON COLUMN curtailment_mqtt_source_state.pending_retry_at IS
    'Earliest retry time for durable pending MQTT edge dispatches that intentionally throttle retryable outcomes.';

-- Table: curtailment_org_config
CREATE TABLE public.curtailment_org_config (
    org_id bigint NOT NULL,
    max_duration_default_sec integer DEFAULT 14400 NOT NULL,
    candidate_min_power_w integer DEFAULT 1500 NOT NULL,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    updated_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    CONSTRAINT ck_curtailment_org_config_candidate_power_positive CHECK ((candidate_min_power_w > 0)),
    CONSTRAINT ck_curtailment_org_config_max_duration_positive CHECK ((max_duration_default_sec > 0))
);

-- Table: curtailment_reconciler_heartbeat
CREATE TABLE public.curtailment_reconciler_heartbeat (
    id smallint DEFAULT 1 NOT NULL,
    last_tick_at timestamp with time zone NOT NULL,
    last_tick_uuid uuid NOT NULL,
    last_tick_duration_ms integer,
    active_event_count integer DEFAULT 0 NOT NULL,
    CONSTRAINT ck_curtailment_reconciler_heartbeat_singleton CHECK ((id = 1))
);

-- Table: curtailment_response_profile
CREATE TABLE public.curtailment_response_profile (
    id bigint NOT NULL,
    org_id bigint NOT NULL,
    profile_name character varying(64) NOT NULL,
    site_id bigint,
    mode text NOT NULL,
    strategy text DEFAULT 'LEAST_EFFICIENT_FIRST'::text NOT NULL,
    level text DEFAULT 'FULL'::text NOT NULL,
    priority text DEFAULT 'NORMAL'::text NOT NULL,
    target_kw numeric(12,3),
    tolerance_kw numeric(12,3),
    curtail_batch_size integer,
    curtail_batch_interval_sec integer DEFAULT 0 CONSTRAINT curtailment_response_profil_curtail_batch_interval_sec_not_null NOT NULL,
    restore_batch_size integer DEFAULT 0 NOT NULL,
    restore_batch_interval_sec integer DEFAULT 0 CONSTRAINT curtailment_response_profil_restore_batch_interval_sec_not_null NOT NULL,
    include_maintenance boolean DEFAULT false NOT NULL,
    force_include_maintenance boolean DEFAULT false NOT NULL,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    updated_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    post_event_cooldown_sec integer DEFAULT 0 NOT NULL,
    scope_json jsonb DEFAULT '{}'::jsonb NOT NULL,
    force_include_all_paired_miners boolean DEFAULT false CONSTRAINT curtailment_response_profil_force_include_all_paired_m_not_null NOT NULL,
    facility_fan_device_ids bigint[] DEFAULT '{}'::bigint[] NOT NULL,
    fan_off_delay_sec integer DEFAULT 0 NOT NULL,
    fan_restore_delay_sec integer DEFAULT 0 NOT NULL,
    authorization_envelope_jsonb jsonb CONSTRAINT curtailment_response_profil_authorization_envelope_jso_not_null NOT NULL,
    CONSTRAINT ck_curtailment_response_profile_all_paired_full_fleet CHECK (((NOT force_include_all_paired_miners) OR (mode = 'FULL_FLEET'::text))),
    CONSTRAINT ck_curtailment_response_profile_curtail_batch_interval CHECK (((curtail_batch_interval_sec >= 0) AND (curtail_batch_interval_sec <= 3600))),
    CONSTRAINT ck_curtailment_response_profile_curtail_batch_size CHECK (((curtail_batch_size IS NULL) OR ((curtail_batch_size > 0) AND (curtail_batch_size <= 10000)))),
    CONSTRAINT ck_curtailment_response_profile_fan_off_delay CHECK ((fan_off_delay_sec >= 0)),
    CONSTRAINT ck_curtailment_response_profile_fan_restore_delay CHECK ((fan_restore_delay_sec >= 0)),
    CONSTRAINT ck_curtailment_response_profile_level CHECK ((level = 'FULL'::text)),
    CONSTRAINT ck_curtailment_response_profile_maintenance_consistency CHECK ((include_maintenance = force_include_maintenance)),
    CONSTRAINT ck_curtailment_response_profile_mode CHECK ((mode = ANY (ARRAY['FIXED_KW'::text, 'FULL_FLEET'::text]))),
    CONSTRAINT ck_curtailment_response_profile_mode_params CHECK ((((mode = 'FIXED_KW'::text) AND (target_kw IS NOT NULL)) OR ((mode = 'FULL_FLEET'::text) AND (target_kw IS NULL) AND (tolerance_kw IS NULL)))),
    CONSTRAINT ck_curtailment_response_profile_name_nonempty CHECK ((btrim((profile_name)::text) <> ''::text)),
    CONSTRAINT ck_curtailment_response_profile_post_event_cooldown CHECK (((post_event_cooldown_sec >= 0) AND (post_event_cooldown_sec <= 86400))),
    CONSTRAINT ck_curtailment_response_profile_priority CHECK ((priority = ANY (ARRAY['NORMAL'::text, 'EMERGENCY'::text]))),
    CONSTRAINT ck_curtailment_response_profile_restore_batch_interval CHECK (((restore_batch_interval_sec >= 0) AND (restore_batch_interval_sec <= 3600))),
    CONSTRAINT ck_curtailment_response_profile_restore_batch_size CHECK (((restore_batch_size >= 0) AND (restore_batch_size <= 10000))),
    CONSTRAINT ck_curtailment_response_profile_scope_json_object CHECK ((jsonb_typeof(scope_json) = 'object'::text)),
    CONSTRAINT ck_curtailment_response_profile_strategy CHECK ((strategy = 'LEAST_EFFICIENT_FIRST'::text)),
    CONSTRAINT ck_curtailment_response_profile_target_positive CHECK (((target_kw IS NULL) OR (target_kw > (0)::numeric))),
    CONSTRAINT ck_curtailment_response_profile_tolerance_less_than_target CHECK (((tolerance_kw IS NULL) OR ((target_kw IS NOT NULL) AND (tolerance_kw < target_kw)))),
    CONSTRAINT ck_curtailment_response_profile_tolerance_nonnegative CHECK (((tolerance_kw IS NULL) OR (tolerance_kw >= (0)::numeric))),
    CONSTRAINT curtailment_response_profile_authorization_envelope_shape CHECK (((jsonb_typeof(authorization_envelope_jsonb) = 'object'::text) AND (authorization_envelope_jsonb ?& ARRAY['schema_version'::text, 'selected_resource_site_ids'::text, 'current_member_site_ids'::text, 'miner_scope_unbounded'::text, 'facility_fan_site_ids'::text, 'facility_fan_scope_unbounded'::text]) AND (jsonb_typeof((authorization_envelope_jsonb -> 'schema_version'::text)) = 'number'::text) AND ((authorization_envelope_jsonb ->> 'schema_version'::text) = '1'::text) AND (jsonb_typeof((authorization_envelope_jsonb -> 'selected_resource_site_ids'::text)) = 'array'::text) AND (jsonb_typeof((authorization_envelope_jsonb -> 'current_member_site_ids'::text)) = 'array'::text) AND (jsonb_typeof((authorization_envelope_jsonb -> 'miner_scope_unbounded'::text)) = 'boolean'::text) AND (jsonb_typeof((authorization_envelope_jsonb -> 'facility_fan_site_ids'::text)) = 'array'::text) AND (jsonb_typeof((authorization_envelope_jsonb -> 'facility_fan_scope_unbounded'::text)) = 'boolean'::text)))
);

-- Sequence: curtailment_response_profile_id_seq
CREATE SEQUENCE public.curtailment_response_profile_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

-- Sequence Owned By: curtailment_response_profile_id_seq
ALTER SEQUENCE public.curtailment_response_profile_id_seq OWNED BY public.curtailment_response_profile.id;

-- Table: curtailment_response_profile_revision
CREATE TABLE public.curtailment_response_profile_revision (
    response_profile_id bigint CONSTRAINT curtailment_response_profile_revis_response_profile_id_not_null NOT NULL,
    revision uuid DEFAULT gen_random_uuid() NOT NULL
);

-- View: curtailment_response_profile_with_revision
CREATE VIEW public.curtailment_response_profile_with_revision AS
 SELECT profile.id,
    profile.org_id,
    profile.profile_name,
    profile.site_id,
    profile.mode,
    profile.strategy,
    profile.level,
    profile.priority,
    profile.target_kw,
    profile.tolerance_kw,
    profile.curtail_batch_size,
    profile.curtail_batch_interval_sec,
    profile.restore_batch_size,
    profile.restore_batch_interval_sec,
    profile.include_maintenance,
    profile.force_include_maintenance,
    profile.created_at,
    profile.updated_at,
    profile.post_event_cooldown_sec,
    profile.scope_json,
    profile.force_include_all_paired_miners,
    profile.facility_fan_device_ids,
    profile.fan_off_delay_sec,
    profile.fan_restore_delay_sec,
    profile.authorization_envelope_jsonb,
    profile_revision.revision
   FROM (public.curtailment_response_profile profile
     JOIN public.curtailment_response_profile_revision profile_revision ON ((profile_revision.response_profile_id = profile.id)));

-- Table: curtailment_rig_config_reconciliation
CREATE TABLE public.curtailment_rig_config_reconciliation (
    organization_id bigint NOT NULL,
    requested_by bigint NOT NULL,
    desired_generation bigint DEFAULT 1 CONSTRAINT curtailment_rig_config_reconciliati_desired_generation_not_null NOT NULL,
    enqueued_generation bigint DEFAULT 0 CONSTRAINT curtailment_rig_config_reconciliat_enqueued_generation_not_null NOT NULL,
    retry_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    lease_expires_at timestamp with time zone,
    last_error text,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    updated_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    full_reconcile_generation bigint DEFAULT 0 CONSTRAINT curtailment_rig_config_recon_full_reconcile_generation_not_null NOT NULL,
    CONSTRAINT ck_curtailment_rig_config_reconciliation_full_generation CHECK (((full_reconcile_generation >= 0) AND (full_reconcile_generation <= desired_generation))),
    CONSTRAINT ck_curtailment_rig_config_reconciliation_generation CHECK (((desired_generation > 0) AND (enqueued_generation >= 0) AND (enqueued_generation <= desired_generation)))
);

-- Table: curtailment_rig_config_target
CREATE TABLE public.curtailment_rig_config_target (
    organization_id bigint NOT NULL,
    device_id bigint NOT NULL,
    requested_generation bigint NOT NULL,
    CONSTRAINT curtailment_rig_config_target_requested_generation_check CHECK ((requested_generation > 0))
);

-- Table: curtailment_target
CREATE TABLE public.curtailment_target (
    curtailment_event_id bigint NOT NULL,
    device_identifier character varying NOT NULL,
    target_type text DEFAULT 'miner'::text NOT NULL,
    state text NOT NULL,
    desired_state text NOT NULL,
    baseline_power_w numeric(12,3),
    added_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    released_at timestamp with time zone,
    last_dispatched_at timestamp with time zone,
    last_batch_uuid character varying(36),
    observed_power_w numeric(12,3),
    observed_at timestamp with time zone,
    confirmed_at timestamp with time zone,
    retry_count integer DEFAULT 0 NOT NULL,
    last_error text,
    selector_rationale_jsonb jsonb,
    curtail_state text DEFAULT 'pending'::text NOT NULL,
    curtail_dispatched_at timestamp with time zone,
    curtail_batch_uuid character varying(36),
    curtail_completed_at timestamp with time zone,
    curtail_retry_count integer DEFAULT 0 NOT NULL,
    curtail_failure_count integer DEFAULT 0 NOT NULL,
    curtail_last_error text,
    restore_state text,
    restore_started_at timestamp with time zone,
    restore_dispatched_at timestamp with time zone,
    restore_batch_uuid character varying(36),
    restore_completed_at timestamp with time zone,
    restore_retry_count integer DEFAULT 0 NOT NULL,
    restore_failure_count integer DEFAULT 0 NOT NULL,
    restore_last_error text,
    CONSTRAINT ck_curtailment_target_curtail_state CHECK ((curtail_state = ANY (ARRAY['pending'::text, 'dispatching'::text, 'dispatched'::text, 'confirmed'::text, 'drifted'::text, 'unavailable'::text, 'resolved'::text, 'released'::text, 'restore_failed'::text]))),
    CONSTRAINT ck_curtailment_target_phase_counts CHECK (((curtail_retry_count >= 0) AND (curtail_failure_count >= 0) AND (restore_retry_count >= 0) AND (restore_failure_count >= 0))),
    CONSTRAINT ck_curtailment_target_restore_state CHECK (((restore_state IS NULL) OR (restore_state = ANY (ARRAY['pending'::text, 'dispatching'::text, 'dispatched'::text, 'confirmed'::text, 'drifted'::text, 'unavailable'::text, 'resolved'::text, 'released'::text, 'restore_failed'::text]))))
);

-- Table: device
CREATE TABLE public.device (
    id bigint NOT NULL,
    device_identifier character varying(255) NOT NULL,
    mac_address character varying(17) NOT NULL,
    serial_number character varying(255),
    org_id bigint NOT NULL,
    discovered_device_id bigint NOT NULL,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP,
    updated_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP,
    deleted_at timestamp with time zone,
    custom_name text,
    worker_name character varying(255),
    worker_name_pool_sync_status public.worker_name_pool_sync_status_enum,
    site_id bigint,
    building_id bigint
);

-- Table: device_set
CREATE TABLE public.device_set (
    id bigint CONSTRAINT device_collection_id_not_null NOT NULL,
    org_id bigint CONSTRAINT device_collection_org_id_not_null NOT NULL,
    type public.device_set_type CONSTRAINT device_collection_type_not_null NOT NULL,
    label text CONSTRAINT device_collection_label_not_null NOT NULL,
    description text,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP CONSTRAINT device_collection_created_at_not_null NOT NULL,
    updated_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP CONSTRAINT device_collection_updated_at_not_null NOT NULL,
    deleted_at timestamp with time zone
);

-- Sequence: device_collection_id_seq
CREATE SEQUENCE public.device_collection_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

-- Sequence Owned By: device_collection_id_seq
ALTER SEQUENCE public.device_collection_id_seq OWNED BY public.device_set.id;

-- Table: device_set_membership
CREATE TABLE public.device_set_membership (
    id bigint CONSTRAINT device_collection_membership_id_not_null NOT NULL,
    org_id bigint CONSTRAINT device_collection_membership_org_id_not_null NOT NULL,
    device_set_id bigint CONSTRAINT device_collection_membership_collection_id_not_null NOT NULL,
    device_set_type public.device_set_type CONSTRAINT device_collection_membership_collection_type_not_null NOT NULL,
    device_id bigint CONSTRAINT device_collection_membership_device_id_not_null NOT NULL,
    device_identifier text CONSTRAINT device_collection_membership_device_identifier_not_null NOT NULL,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP CONSTRAINT device_collection_membership_created_at_not_null NOT NULL
);

-- Sequence: device_collection_membership_id_seq
CREATE SEQUENCE public.device_collection_membership_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

-- Sequence Owned By: device_collection_membership_id_seq
ALTER SEQUENCE public.device_collection_membership_id_seq OWNED BY public.device_set_membership.id;

-- Table: device_firmware_deployment
CREATE TABLE public.device_firmware_deployment (
    device_id bigint NOT NULL,
    firmware_checksum text NOT NULL,
    firmware_version text NOT NULL,
    rollout_id bigint,
    deployed_at timestamp with time zone DEFAULT now() NOT NULL,
    last_command_batch_uuid text
);

-- Sequence: device_id_seq
CREATE SEQUENCE public.device_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

-- Sequence Owned By: device_id_seq
ALTER SEQUENCE public.device_id_seq OWNED BY public.device.id;

-- Table: device_pairing
CREATE TABLE public.device_pairing (
    id bigint NOT NULL,
    device_id bigint NOT NULL,
    pairing_token character varying(255),
    pairing_status public.pairing_status_enum NOT NULL,
    paired_at timestamp with time zone,
    unpaired_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP,
    updated_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP
);

-- Sequence: device_pairing_id_seq
CREATE SEQUENCE public.device_pairing_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

-- Sequence Owned By: device_pairing_id_seq
ALTER SEQUENCE public.device_pairing_id_seq OWNED BY public.device_pairing.id;

-- Table: device_set_rack
CREATE TABLE public.device_set_rack (
    device_set_id bigint CONSTRAINT device_collection_rack_collection_id_not_null NOT NULL,
    zone text,
    rows integer CONSTRAINT device_collection_rack_rows_not_null NOT NULL,
    columns integer CONSTRAINT device_collection_rack_columns_not_null NOT NULL,
    order_index smallint DEFAULT 0 CONSTRAINT device_collection_rack_order_index_not_null NOT NULL,
    cooling_type smallint DEFAULT 0 CONSTRAINT device_collection_rack_cooling_type_not_null NOT NULL,
    org_id bigint NOT NULL,
    building_id bigint,
    site_id bigint,
    aisle_index integer,
    position_in_aisle integer,
    CONSTRAINT ck_device_set_rack_aisle_index_nonneg CHECK (((aisle_index IS NULL) OR (aisle_index >= 0))),
    CONSTRAINT ck_device_set_rack_position_in_aisle_nonneg CHECK (((position_in_aisle IS NULL) OR (position_in_aisle >= 0))),
    CONSTRAINT ck_device_set_rack_position_paired CHECK ((((aisle_index IS NULL) AND (position_in_aisle IS NULL)) OR ((aisle_index IS NOT NULL) AND (position_in_aisle IS NOT NULL)))),
    CONSTRAINT ck_device_set_rack_position_requires_building CHECK ((((aisle_index IS NULL) AND (position_in_aisle IS NULL)) OR (building_id IS NOT NULL))),
    CONSTRAINT positive_dimensions CHECK (((rows > 0) AND (columns > 0)))
);

-- Table: device_status
CREATE TABLE public.device_status (
    id bigint NOT NULL,
    device_id bigint NOT NULL,
    status public.device_status_enum DEFAULT 'ACTIVE'::public.device_status_enum NOT NULL,
    status_timestamp timestamp with time zone DEFAULT CURRENT_TIMESTAMP,
    status_details text,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP
)
WITH (autovacuum_vacuum_scale_factor='0.02', autovacuum_analyze_scale_factor='0.01');

-- Sequence: device_status_id_seq
CREATE SEQUENCE public.device_status_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

-- Sequence Owned By: device_status_id_seq
ALTER SEQUENCE public.device_status_id_seq OWNED BY public.device_status.id;

-- Table: discovered_device
CREATE TABLE public.discovered_device (
    id bigint NOT NULL,
    org_id bigint NOT NULL,
    device_identifier character varying(255) NOT NULL,
    model character varying(255),
    manufacturer character varying(255),
    firmware_version character varying(255),
    ip_address character varying(45) NOT NULL,
    port character varying(10) NOT NULL,
    url_scheme character varying(32) NOT NULL,
    discovery_metadata text,
    first_discovered timestamp with time zone DEFAULT CURRENT_TIMESTAMP,
    last_seen timestamp with time zone DEFAULT CURRENT_TIMESTAMP,
    is_active boolean DEFAULT false NOT NULL,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP,
    updated_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP,
    deleted_at timestamp with time zone,
    driver_name character varying(255) NOT NULL,
    ip_address_inet inet GENERATED ALWAYS AS ((NULLIF((ip_address)::text, ''::text))::inet) STORED,
    discovered_by_fleet_node_id bigint
);

-- Sequence: discovered_device_id_seq
CREATE SEQUENCE public.discovered_device_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

-- Sequence Owned By: discovered_device_id_seq
ALTER SEQUENCE public.discovered_device_id_seq OWNED BY public.discovered_device.id;

-- Table: errors
CREATE TABLE public.errors (
    id bigint NOT NULL,
    error_id character varying(36) NOT NULL,
    org_id bigint NOT NULL,
    miner_error integer NOT NULL,
    severity integer NOT NULL,
    summary text NOT NULL,
    impact text,
    cause_summary text,
    recommended_action text,
    first_seen_at timestamp with time zone NOT NULL,
    last_seen_at timestamp with time zone NOT NULL,
    closed_at timestamp with time zone,
    device_id bigint NOT NULL,
    component_id character varying(255),
    component_type integer,
    vendor_code character varying(255),
    firmware character varying(255),
    extra jsonb,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP,
    updated_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP,
    site_id bigint
)
WITH (autovacuum_vacuum_scale_factor='0.05', autovacuum_analyze_scale_factor='0.02');

-- Sequence: errors_id_seq
CREATE SEQUENCE public.errors_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

-- Sequence Owned By: errors_id_seq
ALTER SEQUENCE public.errors_id_seq OWNED BY public.errors.id;

-- Table: firmware_rollout
CREATE TABLE public.firmware_rollout (
    id bigint NOT NULL,
    org_id bigint NOT NULL,
    channel_id bigint NOT NULL,
    manufacturer text NOT NULL,
    model text NOT NULL,
    firmware_checksum text NOT NULL,
    firmware_version text NOT NULL,
    previous_firmware_checksum text DEFAULT ''::text NOT NULL,
    previous_firmware_version text DEFAULT ''::text NOT NULL,
    assignment_generation bigint NOT NULL,
    status text DEFAULT 'active'::text NOT NULL,
    cancel_reason text DEFAULT ''::text NOT NULL,
    stage text DEFAULT 'rest'::text NOT NULL,
    behavior_snapshot jsonb DEFAULT '{"method": "all_at_once", "order_by": "least_efficient_first"}'::jsonb NOT NULL,
    batch_count integer DEFAULT 0 NOT NULL,
    current_batch integer DEFAULT 0 NOT NULL,
    stage_changed_at timestamp with time zone DEFAULT now() NOT NULL,
    stage_paused_microseconds bigint DEFAULT 0 NOT NULL,
    paused_at timestamp with time zone,
    revision bigint DEFAULT 1 NOT NULL,
    revision_txid bigint DEFAULT 0 NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    started_by_type text DEFAULT 'system'::text NOT NULL,
    started_by_id bigint DEFAULT 0 NOT NULL,
    started_by_name text DEFAULT ''::text NOT NULL,
    last_action_by_type text DEFAULT 'system'::text NOT NULL,
    last_action_by_id bigint DEFAULT 0 NOT NULL,
    last_action_by_name text DEFAULT ''::text NOT NULL,
    created_at timestamp with time zone DEFAULT clock_timestamp() NOT NULL,
    finished_at timestamp with time zone,
    controller_waiting_since timestamp with time zone,
    CONSTRAINT firmware_rollout_behavior_snapshot_check CHECK (((jsonb_typeof(behavior_snapshot) = 'object'::text) AND COALESCE(((behavior_snapshot ->> 'method'::text) = ANY (ARRAY['all_at_once'::text, 'batched'::text, 'pilot_then_continue'::text, 'delegated'::text])), false) AND COALESCE(((behavior_snapshot ->> 'order_by'::text) = ANY (ARRAY['least_efficient_first'::text, 'random'::text])), false))),
    CONSTRAINT firmware_rollout_cancel_reason_check CHECK ((cancel_reason = ANY (ARRAY[''::text, 'superseded'::text, 'canceled_remaining'::text, 'rolled_back'::text, 'cleared'::text]))),
    CONSTRAINT firmware_rollout_last_action_by_type_check CHECK ((last_action_by_type = ANY (ARRAY['user'::text, 'api_key'::text, 'system'::text]))),
    CONSTRAINT firmware_rollout_stage_check CHECK ((stage = ANY (ARRAY['batch'::text, 'awaiting_review'::text, 'waiting'::text, 'rest'::text]))),
    CONSTRAINT firmware_rollout_stage_paused_microseconds_check CHECK ((stage_paused_microseconds >= 0)),
    CONSTRAINT firmware_rollout_started_by_type_check CHECK ((started_by_type = ANY (ARRAY['user'::text, 'api_key'::text, 'system'::text]))),
    CONSTRAINT firmware_rollout_status_check CHECK ((status = ANY (ARRAY['active'::text, 'completed'::text, 'completed_with_failures'::text, 'canceled'::text])))
);

-- Table: firmware_rollout_device
CREATE TABLE public.firmware_rollout_device (
    rollout_id bigint NOT NULL,
    device_id bigint NOT NULL,
    batch_index integer,
    "position" integer,
    attempts integer DEFAULT 0 NOT NULL,
    first_sent_at timestamp with time zone,
    last_sent_at timestamp with time zone,
    last_dispatched_at timestamp with time zone,
    last_dispatched_batch_uuid character varying(36),
    verified_at timestamp with time zone,
    halted_at timestamp with time zone,
    halt_reason text DEFAULT ''::text NOT NULL,
    last_error text DEFAULT ''::text NOT NULL,
    skip_note text DEFAULT ''::text NOT NULL,
    excluded_at timestamp with time zone,
    baseline_status text,
    baseline_hash_rate_hs double precision,
    baseline_power_w double precision,
    baseline_efficiency_jh double precision,
    baseline_temp_c double precision,
    baseline_open_errors integer,
    baseline_at timestamp with time zone,
    added_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT firmware_rollout_device_halt_reason_check CHECK ((halt_reason = ANY (ARRAY[''::text, 'failed'::text, 'canceled'::text, 'skipped'::text])))
);

-- Table: firmware_rollout_event
CREATE TABLE public.firmware_rollout_event (
    id bigint NOT NULL,
    org_id bigint NOT NULL,
    rollout_id bigint NOT NULL,
    channel_id bigint NOT NULL,
    type text NOT NULL,
    occurred_at timestamp with time zone DEFAULT clock_timestamp() NOT NULL,
    actor_type text NOT NULL,
    actor_id bigint NOT NULL,
    actor_name text NOT NULL,
    rollout_revision bigint NOT NULL,
    note text DEFAULT ''::text NOT NULL,
    device_identifiers text[] DEFAULT '{}'::text[] NOT NULL,
    CONSTRAINT firmware_rollout_event_actor_type_check CHECK ((actor_type = ANY (ARRAY['user'::text, 'api_key'::text, 'system'::text]))),
    CONSTRAINT firmware_rollout_event_rollout_revision_check CHECK ((rollout_revision > 0))
);

-- Sequence: firmware_rollout_event_id_seq
CREATE SEQUENCE public.firmware_rollout_event_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

-- Sequence Owned By: firmware_rollout_event_id_seq
ALTER SEQUENCE public.firmware_rollout_event_id_seq OWNED BY public.firmware_rollout_event.id;

-- Sequence: firmware_rollout_id_seq
CREATE SEQUENCE public.firmware_rollout_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

-- Sequence Owned By: firmware_rollout_id_seq
ALTER SEQUENCE public.firmware_rollout_id_seq OWNED BY public.firmware_rollout.id;

-- Table: firmware_rollout_reservation
CREATE TABLE public.firmware_rollout_reservation (
    channel_id bigint NOT NULL,
    device_id bigint NOT NULL,
    batch_uuid character varying(36) NOT NULL,
    observed_offline boolean DEFAULT false NOT NULL
);

-- View: firmware_rollout_suppressed_device
CREATE VIEW public.firmware_rollout_suppressed_device AS
 SELECT channel_id,
    manufacturer_key,
    model_key,
    assignment_generation,
    device_id
   FROM ( SELECT r.channel_id,
            public.release_channel_pair_key(r.manufacturer) AS manufacturer_key,
            public.release_channel_pair_key(r.model) AS model_key,
            r.assignment_generation,
            rd.device_id,
            rd.halted_at,
            row_number() OVER (PARTITION BY r.channel_id, (public.release_channel_pair_key(r.manufacturer)), (public.release_channel_pair_key(r.model)), r.assignment_generation, rd.device_id ORDER BY r.id DESC) AS recency
           FROM (public.firmware_rollout_device rd
             JOIN public.firmware_rollout r ON ((r.id = rd.rollout_id)))) latest
  WHERE ((recency = 1) AND (halted_at IS NOT NULL));

-- Table: organization
CREATE TABLE public.organization (
    id bigint NOT NULL,
    org_id character varying(36) NOT NULL,
    name character varying(255) NOT NULL,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    updated_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    deleted_at timestamp with time zone
);

-- View: fleet_active_organization
CREATE VIEW public.fleet_active_organization AS
 SELECT (id)::text AS organization_id
   FROM public.organization
  WHERE (deleted_at IS NULL);

-- View: fleet_device_placement
CREATE VIEW public.fleet_device_placement AS
 SELECT d.org_id,
    d.device_identifier AS device_id,
    d.site_id,
    COALESCE(dcr.building_id, d.building_id) AS building_id,
    rs.id AS rack_id,
    gm.device_set_id AS group_id
   FROM ((((public.device d
     LEFT JOIN public.device_set_membership rm ON (((rm.org_id = d.org_id) AND (rm.device_id = d.id) AND (rm.device_set_type = 'rack'::public.device_set_type))))
     LEFT JOIN public.device_set rs ON (((rs.id = rm.device_set_id) AND (rs.deleted_at IS NULL))))
     LEFT JOIN public.device_set_rack dcr ON ((dcr.device_set_id = rs.id)))
     LEFT JOIN public.device_set_membership gm ON (((gm.org_id = d.org_id) AND (gm.device_id = d.id) AND (gm.device_set_type = 'group'::public.device_set_type) AND (EXISTS ( SELECT 1
           FROM public.device_set gs
          WHERE ((gs.id = gm.device_set_id) AND (gs.deleted_at IS NULL)))))))
  WHERE (d.deleted_at IS NULL);

-- Table: fleet_metric_rollup_90s
CREATE TABLE public.fleet_metric_rollup_90s (
    bucket timestamp with time zone NOT NULL,
    org_id bigint NOT NULL,
    site_id bigint DEFAULT 0 NOT NULL,
    avg_hash_rate double precision,
    min_hash_rate double precision,
    max_hash_rate double precision,
    latest_hash_rate double precision,
    hash_rate_device_count bigint DEFAULT 0 NOT NULL,
    min_temp double precision,
    max_temp double precision,
    sum_temp double precision,
    temp_points bigint DEFAULT 0 NOT NULL,
    temp_device_count bigint DEFAULT 0 NOT NULL,
    temp_cold_count integer DEFAULT 0 NOT NULL,
    temp_ok_count integer DEFAULT 0 NOT NULL,
    temp_hot_count integer DEFAULT 0 NOT NULL,
    temp_critical_count integer DEFAULT 0 NOT NULL,
    min_fan_rpm double precision,
    max_fan_rpm double precision,
    sum_fan_rpm double precision,
    fan_rpm_points bigint DEFAULT 0 NOT NULL,
    fan_rpm_device_count bigint DEFAULT 0 NOT NULL,
    avg_power double precision,
    min_power double precision,
    max_power double precision,
    latest_power double precision,
    power_device_count bigint DEFAULT 0 NOT NULL,
    min_efficiency double precision,
    max_efficiency double precision,
    sum_efficiency double precision,
    efficiency_points bigint DEFAULT 0 NOT NULL,
    efficiency_device_count bigint DEFAULT 0 NOT NULL
);

-- Table: fleet_metric_rollup_progress
CREATE TABLE public.fleet_metric_rollup_progress (
    id boolean DEFAULT true NOT NULL,
    earliest_bucket timestamp with time zone NOT NULL,
    latest_bucket timestamp with time zone NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT fleet_metric_rollup_progress_id_check CHECK (id)
);

-- Table: fleet_node_auth_challenge
CREATE TABLE public.fleet_node_auth_challenge (
    challenge bytea CONSTRAINT agent_auth_challenge_challenge_not_null NOT NULL,
    fleet_node_id bigint CONSTRAINT agent_auth_challenge_agent_id_not_null NOT NULL,
    expires_at timestamp with time zone CONSTRAINT agent_auth_challenge_expires_at_not_null NOT NULL,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP CONSTRAINT agent_auth_challenge_created_at_not_null NOT NULL
);

-- Table: fleet_node_device
CREATE TABLE public.fleet_node_device (
    fleet_node_id bigint CONSTRAINT agent_device_agent_id_not_null NOT NULL,
    device_id bigint CONSTRAINT agent_device_device_id_not_null NOT NULL,
    org_id bigint CONSTRAINT agent_device_org_id_not_null NOT NULL,
    assigned_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP CONSTRAINT agent_device_assigned_at_not_null NOT NULL,
    assigned_by bigint
);

-- Table: fleet_node_session
CREATE TABLE public.fleet_node_session (
    token_hash text CONSTRAINT agent_session_token_hash_not_null NOT NULL,
    fleet_node_id bigint CONSTRAINT agent_session_agent_id_not_null NOT NULL,
    expires_at timestamp with time zone CONSTRAINT agent_session_expires_at_not_null NOT NULL,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP CONSTRAINT agent_session_created_at_not_null NOT NULL
);

-- View: fleet_pollable_device_presence
CREATE VIEW public.fleet_pollable_device_presence AS
 SELECT DISTINCT organization_id
   FROM ( SELECT (d.org_id)::text AS organization_id
           FROM (public.device d
             JOIN public.device_pairing dp ON ((d.id = dp.device_id)))
          WHERE ((dp.pairing_status = ANY (ARRAY['PAIRED'::public.pairing_status_enum, 'DEFAULT_PASSWORD'::public.pairing_status_enum])) AND (d.deleted_at IS NULL) AND (NOT (EXISTS ( SELECT 1
                   FROM public.fleet_node_device fnd
                  WHERE ((fnd.device_id = d.id) AND (fnd.org_id = d.org_id))))))
        UNION ALL
         SELECT (d.org_id)::text AS organization_id
           FROM (((public.device d
             JOIN public.fleet_node_device fnd ON (((fnd.device_id = d.id) AND (fnd.org_id = d.org_id))))
             JOIN public.device_pairing dp ON ((dp.device_id = fnd.device_id)))
             JOIN public.fleet_node fn ON (((fn.id = fnd.fleet_node_id) AND (fn.org_id = fnd.org_id))))
          WHERE ((d.deleted_at IS NULL) AND (dp.pairing_status = ANY (ARRAY['PAIRED'::public.pairing_status_enum, 'DEFAULT_PASSWORD'::public.pairing_status_enum])) AND (fn.deleted_at IS NULL) AND ((fn.enrollment_status)::text = 'CONFIRMED'::text))) pollable;

-- Table: fleet_runtime_lease
CREATE TABLE public.fleet_runtime_lease (
    lease_name text NOT NULL,
    dcs_cluster_id text NOT NULL,
    highest_writer_generation bigint NOT NULL,
    lease_epoch bigint NOT NULL,
    holder_id uuid NOT NULL,
    expires_at timestamp with time zone NOT NULL,
    CONSTRAINT fleet_runtime_lease_epoch_positive CHECK ((lease_epoch > 0)),
    CONSTRAINT fleet_runtime_lease_singleton CHECK ((lease_name = 'fleet-active'::text)),
    CONSTRAINT fleet_runtime_lease_writer_generation_positive CHECK ((highest_writer_generation > 0))
);

-- Table: infrastructure_device
CREATE TABLE public.infrastructure_device (
    id bigint NOT NULL,
    org_id bigint NOT NULL,
    site_id bigint NOT NULL,
    building_name character varying(255) DEFAULT ''::character varying NOT NULL,
    name character varying(255) NOT NULL,
    device_kind character varying(32) NOT NULL,
    fan_count integer DEFAULT 1 NOT NULL,
    enabled boolean DEFAULT true NOT NULL,
    driver_type character varying(64) NOT NULL,
    driver_config jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    updated_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    deleted_at timestamp with time zone,
    rack_name character varying(100) DEFAULT ''::character varying NOT NULL,
    CONSTRAINT ck_infrastructure_device_fan_count CHECK (((((device_kind)::text = 'single_fan'::text) AND (fan_count = 1)) OR (((device_kind)::text = 'fan_group'::text) AND (fan_count >= 2)))),
    CONSTRAINT ck_infrastructure_device_kind CHECK (device_kind IN ('single_fan', 'fan_group'))
);

-- Sequence: infrastructure_device_id_seq
CREATE SEQUENCE public.infrastructure_device_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

-- Sequence Owned By: infrastructure_device_id_seq
ALTER SEQUENCE public.infrastructure_device_id_seq OWNED BY public.infrastructure_device.id;

-- Table: inventory_part
CREATE TABLE public.inventory_part (
    id bigint NOT NULL,
    org_id bigint NOT NULL,
    name character varying(255) NOT NULL,
    type character varying(64) NOT NULL,
    manufacturer character varying(255),
    part_number character varying(128),
    site_id bigint,
    on_hand integer DEFAULT 0 NOT NULL,
    allocated integer DEFAULT 0 NOT NULL,
    reorder_point integer DEFAULT 0 NOT NULL,
    bin_location character varying(64),
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    updated_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    deleted_at timestamp with time zone,
    CONSTRAINT ck_inventory_allocated CHECK ((allocated >= 0)),
    CONSTRAINT ck_inventory_allocation_within_stock CHECK ((allocated <= on_hand)),
    CONSTRAINT ck_inventory_on_hand CHECK ((on_hand >= 0)),
    CONSTRAINT ck_inventory_reorder CHECK ((reorder_point >= 0))
);

-- Sequence: inventory_part_id_seq
CREATE SEQUENCE public.inventory_part_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

-- Sequence Owned By: inventory_part_id_seq
ALTER SEQUENCE public.inventory_part_id_seq OWNED BY public.inventory_part.id;

-- Table: miner_credentials
CREATE TABLE public.miner_credentials (
    id bigint NOT NULL,
    device_id bigint NOT NULL,
    username_enc text NOT NULL,
    password_enc text NOT NULL,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP,
    updated_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP
);

-- Sequence: miner_credentials_id_seq
CREATE SEQUENCE public.miner_credentials_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

-- Sequence Owned By: miner_credentials_id_seq
ALTER SEQUENCE public.miner_credentials_id_seq OWNED BY public.miner_credentials.id;

-- Table: notification_active
CREATE TABLE public.notification_active (
    organization_id bigint NOT NULL,
    alert_key text NOT NULL,
    history_id bigint NOT NULL,
    received_at timestamp with time zone NOT NULL,
    status text NOT NULL,
    event_at timestamp with time zone NOT NULL,
    alert_name text NOT NULL,
    severity text DEFAULT ''::text NOT NULL,
    rule_group text DEFAULT ''::text NOT NULL,
    fingerprint text DEFAULT ''::text NOT NULL,
    device_id text DEFAULT ''::text NOT NULL,
    template text DEFAULT ''::text NOT NULL,
    summary text DEFAULT ''::text NOT NULL,
    starts_at timestamp with time zone,
    ends_at timestamp with time zone
);

-- Table: notification_history
CREATE TABLE public.notification_history (
    id bigint NOT NULL,
    received_at timestamp with time zone DEFAULT now() NOT NULL,
    alert_name text NOT NULL,
    status text NOT NULL,
    severity text DEFAULT ''::text NOT NULL,
    rule_group text DEFAULT ''::text NOT NULL,
    fingerprint text DEFAULT ''::text NOT NULL,
    organization_id bigint,
    device_id text DEFAULT ''::text NOT NULL,
    template text DEFAULT ''::text NOT NULL,
    summary text DEFAULT ''::text NOT NULL,
    starts_at timestamp with time zone,
    ends_at timestamp with time zone,
    labels jsonb DEFAULT '{}'::jsonb NOT NULL,
    annotations jsonb DEFAULT '{}'::jsonb NOT NULL
);

-- Sequence: notification_history_id_seq
CREATE SEQUENCE public.notification_history_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

-- Sequence Owned By: notification_history_id_seq
ALTER SEQUENCE public.notification_history_id_seq OWNED BY public.notification_history.id;

-- Sequence: organization_id_seq
CREATE SEQUENCE public.organization_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

-- Sequence Owned By: organization_id_seq
ALTER SEQUENCE public.organization_id_seq OWNED BY public.organization.id;

-- Table: pending_enrollment
CREATE TABLE public.pending_enrollment (
    id bigint NOT NULL,
    code_hash text NOT NULL,
    org_id bigint NOT NULL,
    created_by bigint NOT NULL,
    fleet_node_id bigint,
    status character varying(32) DEFAULT 'PENDING'::character varying NOT NULL,
    expires_at timestamp with time zone NOT NULL,
    consumed_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    CONSTRAINT ck_pending_enrollment_fleet_node_states CHECK ((fleet_node_id IS NULL AND status IN ('PENDING', 'CANCELLED', 'EXPIRED')) OR (fleet_node_id IS NOT NULL AND status IN ('AWAITING_CONFIRMATION', 'CONFIRMED', 'CANCELLED', 'EXPIRED'))),
    CONSTRAINT ck_pending_enrollment_status CHECK (status IN ('PENDING', 'AWAITING_CONFIRMATION', 'CONFIRMED', 'EXPIRED', 'CANCELLED'))
);

-- Sequence: pending_enrollment_id_seq
CREATE SEQUENCE public.pending_enrollment_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

-- Sequence Owned By: pending_enrollment_id_seq
ALTER SEQUENCE public.pending_enrollment_id_seq OWNED BY public.pending_enrollment.id;

-- Table: permission
CREATE TABLE public.permission (
    id bigint NOT NULL,
    key character varying(128) NOT NULL,
    description text NOT NULL,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL
);

-- Sequence: permission_id_seq
CREATE SEQUENCE public.permission_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

-- Sequence Owned By: permission_id_seq
ALTER SEQUENCE public.permission_id_seq OWNED BY public.permission.id;

-- Table: pool
CREATE TABLE public.pool (
    id bigint NOT NULL,
    org_id bigint NOT NULL,
    pool_name character varying(255) NOT NULL,
    url character varying(255) NOT NULL,
    username character varying(255) NOT NULL,
    password_enc text NOT NULL,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    updated_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    deleted_at timestamp with time zone
);

-- Sequence: pool_id_seq
CREATE SEQUENCE public.pool_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

-- Sequence Owned By: pool_id_seq
ALTER SEQUENCE public.pool_id_seq OWNED BY public.pool.id;

-- Table: queue_message
CREATE TABLE public.queue_message (
    id bigint NOT NULL,
    command_batch_log_uuid character varying(36) NOT NULL,
    device_id bigint NOT NULL,
    command_type text NOT NULL,
    status public.queue_status_enum NOT NULL,
    retry_count integer NOT NULL,
    error_info text,
    payload jsonb,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    updated_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL
);

-- Sequence: queue_message_id_seq
CREATE SEQUENCE public.queue_message_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

-- Sequence Owned By: queue_message_id_seq
ALTER SEQUENCE public.queue_message_id_seq OWNED BY public.queue_message.id;

-- Table: rack_slot
CREATE TABLE public.rack_slot (
    device_set_id bigint CONSTRAINT rack_slot_collection_id_not_null NOT NULL,
    device_id bigint NOT NULL,
    "row" integer NOT NULL,
    col integer NOT NULL,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    CONSTRAINT valid_position CHECK ((("row" >= 0) AND (col >= 0)))
);

-- Table: release_channel
CREATE TABLE public.release_channel (
    id bigint NOT NULL,
    org_id bigint NOT NULL,
    name text NOT NULL,
    description text DEFAULT ''::text NOT NULL,
    method text DEFAULT 'all_at_once'::text NOT NULL,
    order_by text DEFAULT 'least_efficient_first'::text NOT NULL,
    batch_size integer DEFAULT 0 NOT NULL,
    pilot_size integer DEFAULT 0 NOT NULL,
    wait_between_batches_seconds integer DEFAULT 0 NOT NULL,
    review_after_each_batch boolean DEFAULT false NOT NULL,
    auto_continue boolean DEFAULT false NOT NULL,
    stabilization_seconds integer DEFAULT 0 NOT NULL,
    max_hashrate_drop_percent double precision,
    max_efficiency_increase_percent double precision,
    max_temp_increase_c double precision,
    max_new_errors integer,
    min_sample_coverage_percent double precision,
    max_concurrent_offline integer DEFAULT 0 NOT NULL,
    controller_timeout_seconds integer DEFAULT 0 NOT NULL,
    created_by bigint NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT release_channel_method_check CHECK ((method = ANY (ARRAY['all_at_once'::text, 'batched'::text, 'pilot_then_continue'::text, 'delegated'::text]))),
    CONSTRAINT release_channel_order_by_check CHECK ((order_by = ANY (ARRAY['least_efficient_first'::text, 'random'::text])))
);

-- View: release_channel_placement
CREATE VIEW public.release_channel_placement AS
 SELECT d.org_id,
    d.id AS device_id,
    d.device_identifier,
    d.site_id,
    COALESCE(dsr.building_id, d.building_id) AS building_id,
    rs.id AS rack_id
   FROM ((((public.device d
     JOIN public.discovered_device dd ON (((dd.id = d.discovered_device_id) AND (dd.deleted_at IS NULL))))
     LEFT JOIN public.device_set_membership rm ON (((rm.org_id = d.org_id) AND (rm.device_id = d.id) AND (rm.device_set_type = 'rack'::public.device_set_type))))
     LEFT JOIN public.device_set rs ON (((rs.id = rm.device_set_id) AND (rs.deleted_at IS NULL))))
     LEFT JOIN public.device_set_rack dsr ON ((dsr.device_set_id = rs.id)))
  WHERE (d.deleted_at IS NULL);

-- Table: release_channel_target
CREATE TABLE public.release_channel_target (
    channel_id bigint NOT NULL,
    target_type text NOT NULL,
    target_id bigint,
    device_identifier text,
    CONSTRAINT release_channel_target_check CHECK (
CASE
    WHEN (target_type = 'miner'::text) THEN ((target_id IS NULL) AND (device_identifier IS NOT NULL))
    ELSE ((target_id IS NOT NULL) AND (device_identifier IS NULL))
END),
    CONSTRAINT release_channel_target_target_type_check CHECK ((target_type = ANY (ARRAY['site'::text, 'building'::text, 'rack'::text, 'group'::text, 'miner'::text])))
);

-- View: release_channel_match
CREATE VIEW public.release_channel_match AS
 SELECT t.channel_id,
    c.org_id,
    p.device_id,
    1 AS specificity
   FROM ((public.release_channel_target t
     JOIN public.release_channel c ON ((c.id = t.channel_id)))
     JOIN public.release_channel_placement p ON (((p.org_id = c.org_id) AND ((p.device_identifier)::text = t.device_identifier))))
  WHERE (t.target_type = 'miner'::text)
UNION ALL
 SELECT t.channel_id,
    c.org_id,
    p.device_id,
    2 AS specificity
   FROM ((((public.release_channel_target t
     JOIN public.release_channel c ON ((c.id = t.channel_id)))
     JOIN public.device_set gs ON (((gs.id = t.target_id) AND (gs.org_id = c.org_id) AND (gs.type = 'group'::public.device_set_type) AND (gs.deleted_at IS NULL))))
     JOIN public.device_set_membership gm ON (((gm.device_set_id = gs.id) AND (gm.device_set_type = 'group'::public.device_set_type))))
     JOIN public.release_channel_placement p ON ((p.device_id = gm.device_id)))
  WHERE (t.target_type = 'group'::text)
UNION ALL
 SELECT t.channel_id,
    c.org_id,
    p.device_id,
    3 AS specificity
   FROM ((public.release_channel_target t
     JOIN public.release_channel c ON ((c.id = t.channel_id)))
     JOIN public.release_channel_placement p ON (((p.org_id = c.org_id) AND (p.rack_id = t.target_id))))
  WHERE (t.target_type = 'rack'::text)
UNION ALL
 SELECT t.channel_id,
    c.org_id,
    p.device_id,
    4 AS specificity
   FROM ((public.release_channel_target t
     JOIN public.release_channel c ON ((c.id = t.channel_id)))
     JOIN public.release_channel_placement p ON (((p.org_id = c.org_id) AND (p.building_id = t.target_id))))
  WHERE (t.target_type = 'building'::text)
UNION ALL
 SELECT t.channel_id,
    c.org_id,
    p.device_id,
    5 AS specificity
   FROM ((public.release_channel_target t
     JOIN public.release_channel c ON ((c.id = t.channel_id)))
     JOIN public.release_channel_placement p ON (((p.org_id = c.org_id) AND (p.site_id = t.target_id))))
  WHERE (t.target_type = 'site'::text);

-- View: release_channel_resolution
CREATE VIEW public.release_channel_resolution AS
 SELECT channel_id,
    org_id,
    device_id,
    specificity,
    min(specificity) OVER (PARTITION BY org_id, device_id) AS best,
    count(*) OVER (PARTITION BY org_id, device_id, specificity) AS at_level,
    count(*) OVER (PARTITION BY org_id, device_id) AS channels
   FROM ( SELECT release_channel_match.channel_id,
            release_channel_match.org_id,
            release_channel_match.device_id,
            min(release_channel_match.specificity) AS specificity
           FROM public.release_channel_match
          GROUP BY release_channel_match.channel_id, release_channel_match.org_id, release_channel_match.device_id) per_channel;

-- View: release_channel_conflict
CREATE VIEW public.release_channel_conflict AS
 SELECT channel_id,
    org_id,
    device_id,
    specificity,
        CASE
            WHEN ((specificity = best) AND (at_level = 1)) THEN 'winner'::text
            WHEN (specificity = best) THEN 'excluded_tie'::text
            ELSE 'loser'::text
        END AS resolution
   FROM public.release_channel_resolution
  WHERE (channels > 1);

-- Table: release_channel_firmware
CREATE TABLE public.release_channel_firmware (
    channel_id bigint NOT NULL,
    manufacturer text NOT NULL,
    model text NOT NULL,
    firmware_checksum text DEFAULT ''::text NOT NULL,
    firmware_version text DEFAULT ''::text NOT NULL,
    firmware_target_manufacturer text DEFAULT ''::text NOT NULL,
    firmware_target_model text DEFAULT ''::text NOT NULL,
    assignment_generation bigint DEFAULT 0 NOT NULL,
    assigned_by bigint NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    previous_firmware_checksum text DEFAULT ''::text NOT NULL,
    previous_firmware_version text DEFAULT ''::text NOT NULL
);

-- Sequence: release_channel_id_seq
CREATE SEQUENCE public.release_channel_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

-- Sequence Owned By: release_channel_id_seq
ALTER SEQUENCE public.release_channel_id_seq OWNED BY public.release_channel.id;

-- View: release_channel_member
CREATE VIEW public.release_channel_member AS
 SELECT channel_id,
    org_id,
    device_id,
    (channels > 1) AS conflicted
   FROM public.release_channel_resolution
  WHERE ((specificity = best) AND (at_level = 1));

-- Table: release_channel_setting
CREATE TABLE public.release_channel_setting (
    organization_id bigint NOT NULL,
    channel text NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT ck_release_channel_setting_channel CHECK ((channel = ANY (ARRAY['stable'::text, 'stable_and_rc'::text])))
);

-- Table: repair_ticket
CREATE TABLE public.repair_ticket (
    id bigint NOT NULL,
    org_id bigint NOT NULL,
    ticket_number character varying(16) NOT NULL,
    category smallint NOT NULL,
    status smallint DEFAULT 1 NOT NULL,
    urgent boolean DEFAULT false NOT NULL,
    component character varying(255) NOT NULL,
    diagnosis text,
    miner_identifier character varying(256),
    alert_id character varying(64),
    assignee_user_id bigint,
    warranty_status smallint DEFAULT 0 NOT NULL,
    site_id bigint,
    building_id bigint,
    zone character varying(255),
    rack_id bigint,
    rack_label character varying(255),
    group_label character varying(255),
    resolution smallint DEFAULT 0 NOT NULL,
    repair_location smallint DEFAULT 0 NOT NULL,
    notes text,
    daily_impact_usd numeric(10,2) DEFAULT 0,
    rma_vendor character varying(255),
    rma_tracking character varying(255),
    rma_eta timestamp with time zone,
    completed_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    updated_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    deleted_at timestamp with time zone,
    idempotency_key character varying(64),
    create_request_hash character(64),
    CONSTRAINT ck_repair_ticket_category CHECK (((category >= 1) AND (category <= 2))),
    CONSTRAINT ck_repair_ticket_create_request_hash_length CHECK (((create_request_hash IS NULL) OR (length(create_request_hash) = 64))),
    CONSTRAINT ck_repair_ticket_idempotency_key_nonempty CHECK (((idempotency_key IS NULL) OR (length((idempotency_key)::text) > 0))),
    CONSTRAINT ck_repair_ticket_repair_location CHECK (((repair_location >= 0) AND (repair_location <= 2))),
    CONSTRAINT ck_repair_ticket_resolution CHECK (((resolution >= 0) AND (resolution <= 5))),
    CONSTRAINT ck_repair_ticket_status CHECK (((status >= 1) AND (status <= 5))),
    CONSTRAINT ck_repair_ticket_warranty CHECK (((warranty_status >= 0) AND (warranty_status <= 3)))
);

-- Table: repair_ticket_comment
CREATE TABLE public.repair_ticket_comment (
    id bigint NOT NULL,
    org_id bigint NOT NULL,
    ticket_id bigint NOT NULL,
    user_id bigint NOT NULL,
    user_name character varying(255) NOT NULL,
    text text NOT NULL,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    deleted_at timestamp with time zone,
    idempotency_key character varying(64),
    create_request_hash character(64),
    CONSTRAINT ck_repair_ticket_comment_idempotency_key_nonempty CHECK (((idempotency_key IS NULL) OR (length((idempotency_key)::text) > 0))),
    CONSTRAINT ck_repair_ticket_comment_request_hash_length CHECK (((create_request_hash IS NULL) OR (length(create_request_hash) = 64)))
);

-- Sequence: repair_ticket_comment_id_seq
CREATE SEQUENCE public.repair_ticket_comment_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

-- Sequence Owned By: repair_ticket_comment_id_seq
ALTER SEQUENCE public.repair_ticket_comment_id_seq OWNED BY public.repair_ticket_comment.id;

-- Table: repair_ticket_counter
CREATE TABLE public.repair_ticket_counter (
    org_id bigint NOT NULL,
    next_number bigint NOT NULL,
    CONSTRAINT repair_ticket_counter_next_number_check CHECK ((next_number > 0))
);

-- Sequence: repair_ticket_id_seq
CREATE SEQUENCE public.repair_ticket_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

-- Sequence Owned By: repair_ticket_id_seq
ALTER SEQUENCE public.repair_ticket_id_seq OWNED BY public.repair_ticket.id;

-- Table: repair_ticket_part
CREATE TABLE public.repair_ticket_part (
    id bigint NOT NULL,
    org_id bigint NOT NULL,
    ticket_id bigint NOT NULL,
    inventory_part_id bigint NOT NULL,
    part_name character varying(255) NOT NULL,
    quantity integer NOT NULL,
    consumed_at timestamp with time zone,
    CONSTRAINT repair_ticket_part_quantity_check CHECK ((quantity > 0))
);

-- Sequence: repair_ticket_part_id_seq
CREATE SEQUENCE public.repair_ticket_part_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

-- Sequence Owned By: repair_ticket_part_id_seq
ALTER SEQUENCE public.repair_ticket_part_id_seq OWNED BY public.repair_ticket_part.id;

-- Table: role
CREATE TABLE public.role (
    id bigint NOT NULL,
    name character varying(255) NOT NULL,
    description text,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    updated_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    deleted_at timestamp with time zone,
    is_builtin boolean DEFAULT false NOT NULL,
    builtin_key character varying(64),
    organization_id bigint,
    CONSTRAINT chk_role_builtin_key_matches_flag CHECK ((((is_builtin = true) AND (builtin_key IS NOT NULL)) OR ((is_builtin = false) AND (builtin_key IS NULL)))),
    CONSTRAINT chk_role_custom_name_not_reserved CHECK (((is_builtin = true) OR (organization_id IS NULL) OR (lower(btrim((name)::text)) <> ALL (ARRAY['super_admin'::text, 'admin'::text, 'field_tech'::text]))))
);

-- Sequence: role_id_seq
CREATE SEQUENCE public.role_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

-- Sequence Owned By: role_id_seq
ALTER SEQUENCE public.role_id_seq OWNED BY public.role.id;

-- Table: role_permission
CREATE TABLE public.role_permission (
    role_id bigint NOT NULL,
    permission_id bigint NOT NULL
);

-- Table: schedule
CREATE TABLE public.schedule (
    id bigint NOT NULL,
    org_id bigint NOT NULL,
    name character varying(100) NOT NULL,
    action text NOT NULL,
    action_config jsonb DEFAULT '{}'::jsonb NOT NULL,
    schedule_type text NOT NULL,
    recurrence jsonb,
    start_date date NOT NULL,
    start_time time without time zone NOT NULL,
    end_time time without time zone,
    end_date date,
    timezone text NOT NULL,
    status text DEFAULT 'active'::text NOT NULL,
    priority integer NOT NULL,
    created_by bigint NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    deleted_at timestamp with time zone,
    last_run_at timestamp with time zone,
    next_run_at timestamp with time zone
);

-- Sequence: schedule_id_seq
CREATE SEQUENCE public.schedule_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

-- Sequence Owned By: schedule_id_seq
ALTER SEQUENCE public.schedule_id_seq OWNED BY public.schedule.id;

-- Table: schedule_target
CREATE TABLE public.schedule_target (
    id bigint NOT NULL,
    schedule_id bigint NOT NULL,
    target_type text NOT NULL,
    target_id text NOT NULL
);

-- Sequence: schedule_target_id_seq
CREATE SEQUENCE public.schedule_target_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

-- Sequence Owned By: schedule_target_id_seq
ALTER SEQUENCE public.schedule_target_id_seq OWNED BY public.schedule_target.id;

-- Table: session
CREATE TABLE public.session (
    id bigint NOT NULL,
    session_id character varying(64) NOT NULL,
    user_id bigint NOT NULL,
    organization_id bigint NOT NULL,
    user_agent character varying(512),
    ip_address character varying(45),
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    last_activity timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    expires_at timestamp with time zone NOT NULL,
    revoked_at timestamp with time zone
);

-- Sequence: session_id_seq
CREATE SEQUENCE public.session_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

-- Sequence Owned By: session_id_seq
ALTER SEQUENCE public.session_id_seq OWNED BY public.session.id;

-- Table: site
CREATE TABLE public.site (
    id bigint NOT NULL,
    org_id bigint NOT NULL,
    name character varying(255) NOT NULL,
    location_city character varying(255),
    location_state character varying(255),
    power_capacity_mw numeric(10,3),
    network_config text,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    updated_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    deleted_at timestamp with time zone,
    address text,
    postal_code text,
    country text DEFAULT 'US'::text NOT NULL,
    notes text,
    timezone text,
    slug character varying(63) NOT NULL,
    infrastructure_control_subnets text DEFAULT ''::text NOT NULL
);

-- Sequence: site_id_seq
CREATE SEQUENCE public.site_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

-- Sequence Owned By: site_id_seq
ALTER SEQUENCE public.site_id_seq OWNED BY public.site.id;

-- Table: user
CREATE TABLE public."user" (
    id bigint NOT NULL,
    user_id character varying(36) NOT NULL,
    username character varying(255) NOT NULL,
    password_hash text NOT NULL,
    password_updated_at timestamp with time zone,
    last_login_at timestamp with time zone,
    requires_password_change boolean DEFAULT false NOT NULL,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    updated_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    deleted_at timestamp with time zone
);

-- Sequence: user_id_seq
CREATE SEQUENCE public.user_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

-- Sequence Owned By: user_id_seq
ALTER SEQUENCE public.user_id_seq OWNED BY public."user".id;

-- Table: user_organization
CREATE TABLE public.user_organization (
    id bigint NOT NULL,
    user_id bigint NOT NULL,
    organization_id bigint NOT NULL,
    role_id bigint NOT NULL,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    updated_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    deleted_at timestamp with time zone
);

-- Sequence: user_organization_id_seq
CREATE SEQUENCE public.user_organization_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

-- Sequence Owned By: user_organization_id_seq
ALTER SEQUENCE public.user_organization_id_seq OWNED BY public.user_organization.id;

-- Table: user_organization_role
CREATE TABLE public.user_organization_role (
    id bigint CONSTRAINT user_organization_role_id_not_null1 NOT NULL,
    user_id bigint NOT NULL,
    organization_id bigint NOT NULL,
    role_id bigint NOT NULL,
    scope_type character varying(16) NOT NULL,
    scope_id bigint,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    updated_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    deleted_at timestamp with time zone,
    CONSTRAINT chk_user_org_role_scope_id_matches_type CHECK (((((scope_type)::text = 'org'::text) AND (scope_id IS NULL)) OR (((scope_type)::text = 'site'::text) AND (scope_id IS NOT NULL)))),
    CONSTRAINT chk_user_org_role_scope_type CHECK (scope_type IN ('org', 'site'))
);

-- Sequence: user_organization_role_id_seq
CREATE SEQUENCE public.user_organization_role_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

-- Sequence Owned By: user_organization_role_id_seq
ALTER SEQUENCE public.user_organization_role_id_seq OWNED BY public.user_organization_role.id;

-- Default: activity_log id
ALTER TABLE ONLY public.activity_log ALTER COLUMN id SET DEFAULT nextval('public.activity_log_id_seq'::regclass);

-- Default: alert_channel id
ALTER TABLE ONLY public.alert_channel ALTER COLUMN id SET DEFAULT nextval('public.alert_channel_id_seq'::regclass);

-- Default: alert_maintenance_window id
ALTER TABLE ONLY public.alert_maintenance_window ALTER COLUMN id SET DEFAULT nextval('public.alert_maintenance_window_id_seq'::regclass);

-- Default: alert_route_policy id
ALTER TABLE ONLY public.alert_route_policy ALTER COLUMN id SET DEFAULT nextval('public.alert_route_policy_id_seq'::regclass);

-- Default: api_key id
ALTER TABLE ONLY public.api_key ALTER COLUMN id SET DEFAULT nextval('public.api_key_id_seq'::regclass);

-- Default: building id
ALTER TABLE ONLY public.building ALTER COLUMN id SET DEFAULT nextval('public.building_id_seq'::regclass);

-- Default: command_batch_log id
ALTER TABLE ONLY public.command_batch_log ALTER COLUMN id SET DEFAULT nextval('public.command_batch_log_id_seq'::regclass);

-- Default: command_on_device_log id
ALTER TABLE ONLY public.command_on_device_log ALTER COLUMN id SET DEFAULT nextval('public.command_on_device_log_id_seq'::regclass);

-- Default: curtailment_automation_rule id
ALTER TABLE ONLY public.curtailment_automation_rule ALTER COLUMN id SET DEFAULT nextval('public.curtailment_automation_rule_id_seq'::regclass);

-- Default: curtailment_event id
ALTER TABLE ONLY public.curtailment_event ALTER COLUMN id SET DEFAULT nextval('public.curtailment_event_id_seq'::regclass);

-- Default: curtailment_mqtt_source_config id
ALTER TABLE ONLY public.curtailment_mqtt_source_config ALTER COLUMN id SET DEFAULT nextval('public.curtailment_mqtt_source_config_id_seq'::regclass);

-- Default: curtailment_response_profile id
ALTER TABLE ONLY public.curtailment_response_profile ALTER COLUMN id SET DEFAULT nextval('public.curtailment_response_profile_id_seq'::regclass);

-- Default: device id
ALTER TABLE ONLY public.device ALTER COLUMN id SET DEFAULT nextval('public.device_id_seq'::regclass);

-- Default: device_pairing id
ALTER TABLE ONLY public.device_pairing ALTER COLUMN id SET DEFAULT nextval('public.device_pairing_id_seq'::regclass);

-- Default: device_set id
ALTER TABLE ONLY public.device_set ALTER COLUMN id SET DEFAULT nextval('public.device_collection_id_seq'::regclass);

-- Default: device_set_membership id
ALTER TABLE ONLY public.device_set_membership ALTER COLUMN id SET DEFAULT nextval('public.device_collection_membership_id_seq'::regclass);

-- Default: device_status id
ALTER TABLE ONLY public.device_status ALTER COLUMN id SET DEFAULT nextval('public.device_status_id_seq'::regclass);

-- Default: discovered_device id
ALTER TABLE ONLY public.discovered_device ALTER COLUMN id SET DEFAULT nextval('public.discovered_device_id_seq'::regclass);

-- Default: errors id
ALTER TABLE ONLY public.errors ALTER COLUMN id SET DEFAULT nextval('public.errors_id_seq'::regclass);

-- Default: firmware_rollout id
ALTER TABLE ONLY public.firmware_rollout ALTER COLUMN id SET DEFAULT nextval('public.firmware_rollout_id_seq'::regclass);

-- Default: firmware_rollout_event id
ALTER TABLE ONLY public.firmware_rollout_event ALTER COLUMN id SET DEFAULT nextval('public.firmware_rollout_event_id_seq'::regclass);

-- Default: fleet_node id
ALTER TABLE ONLY public.fleet_node ALTER COLUMN id SET DEFAULT nextval('public.agent_id_seq'::regclass);

-- Default: infrastructure_device id
ALTER TABLE ONLY public.infrastructure_device ALTER COLUMN id SET DEFAULT nextval('public.infrastructure_device_id_seq'::regclass);

-- Default: inventory_part id
ALTER TABLE ONLY public.inventory_part ALTER COLUMN id SET DEFAULT nextval('public.inventory_part_id_seq'::regclass);

-- Default: miner_credentials id
ALTER TABLE ONLY public.miner_credentials ALTER COLUMN id SET DEFAULT nextval('public.miner_credentials_id_seq'::regclass);

-- Default: notification_history id
ALTER TABLE ONLY public.notification_history ALTER COLUMN id SET DEFAULT nextval('public.notification_history_id_seq'::regclass);

-- Default: organization id
ALTER TABLE ONLY public.organization ALTER COLUMN id SET DEFAULT nextval('public.organization_id_seq'::regclass);

-- Default: pending_enrollment id
ALTER TABLE ONLY public.pending_enrollment ALTER COLUMN id SET DEFAULT nextval('public.pending_enrollment_id_seq'::regclass);

-- Default: permission id
ALTER TABLE ONLY public.permission ALTER COLUMN id SET DEFAULT nextval('public.permission_id_seq'::regclass);

-- Default: pool id
ALTER TABLE ONLY public.pool ALTER COLUMN id SET DEFAULT nextval('public.pool_id_seq'::regclass);

-- Default: queue_message id
ALTER TABLE ONLY public.queue_message ALTER COLUMN id SET DEFAULT nextval('public.queue_message_id_seq'::regclass);

-- Default: release_channel id
ALTER TABLE ONLY public.release_channel ALTER COLUMN id SET DEFAULT nextval('public.release_channel_id_seq'::regclass);

-- Default: repair_ticket id
ALTER TABLE ONLY public.repair_ticket ALTER COLUMN id SET DEFAULT nextval('public.repair_ticket_id_seq'::regclass);

-- Default: repair_ticket_comment id
ALTER TABLE ONLY public.repair_ticket_comment ALTER COLUMN id SET DEFAULT nextval('public.repair_ticket_comment_id_seq'::regclass);

-- Default: repair_ticket_part id
ALTER TABLE ONLY public.repair_ticket_part ALTER COLUMN id SET DEFAULT nextval('public.repair_ticket_part_id_seq'::regclass);

-- Default: role id
ALTER TABLE ONLY public.role ALTER COLUMN id SET DEFAULT nextval('public.role_id_seq'::regclass);

-- Default: schedule id
ALTER TABLE ONLY public.schedule ALTER COLUMN id SET DEFAULT nextval('public.schedule_id_seq'::regclass);

-- Default: schedule_target id
ALTER TABLE ONLY public.schedule_target ALTER COLUMN id SET DEFAULT nextval('public.schedule_target_id_seq'::regclass);

-- Default: session id
ALTER TABLE ONLY public.session ALTER COLUMN id SET DEFAULT nextval('public.session_id_seq'::regclass);

-- Default: site id
ALTER TABLE ONLY public.site ALTER COLUMN id SET DEFAULT nextval('public.site_id_seq'::regclass);

-- Default: user id
ALTER TABLE ONLY public."user" ALTER COLUMN id SET DEFAULT nextval('public.user_id_seq'::regclass);

-- Default: user_organization id
ALTER TABLE ONLY public.user_organization ALTER COLUMN id SET DEFAULT nextval('public.user_organization_id_seq'::regclass);

-- Default: user_organization_role id
ALTER TABLE ONLY public.user_organization_role ALTER COLUMN id SET DEFAULT nextval('public.user_organization_role_id_seq'::regclass);

-- Constraint: activity_log activity_log_event_id_key
ALTER TABLE ONLY public.activity_log
    ADD CONSTRAINT activity_log_event_id_key UNIQUE (event_id);

-- Constraint: activity_log activity_log_pkey
ALTER TABLE ONLY public.activity_log
    ADD CONSTRAINT activity_log_pkey PRIMARY KEY (id);

-- Constraint: alert_channel alert_channel_pkey
ALTER TABLE ONLY public.alert_channel
    ADD CONSTRAINT alert_channel_pkey PRIMARY KEY (id);

-- Constraint: alert_maintenance_window alert_maintenance_window_pkey
ALTER TABLE ONLY public.alert_maintenance_window
    ADD CONSTRAINT alert_maintenance_window_pkey PRIMARY KEY (id);

-- Constraint: alert_route_channel alert_route_channel_pkey
ALTER TABLE ONLY public.alert_route_channel
    ADD CONSTRAINT alert_route_channel_pkey PRIMARY KEY (policy_id, channel_id);

-- Constraint: alert_route_policy alert_route_policy_pkey
ALTER TABLE ONLY public.alert_route_policy
    ADD CONSTRAINT alert_route_policy_pkey PRIMARY KEY (id);

-- Constraint: alert_rule_config alert_rule_config_pkey
ALTER TABLE ONLY public.alert_rule_config
    ADD CONSTRAINT alert_rule_config_pkey PRIMARY KEY (org_id, rule_uid);

-- Constraint: api_key api_key_pkey
ALTER TABLE ONLY public.api_key
    ADD CONSTRAINT api_key_pkey PRIMARY KEY (id);

-- Constraint: building building_pkey
ALTER TABLE ONLY public.building
    ADD CONSTRAINT building_pkey PRIMARY KEY (id);

-- Constraint: command_batch_log command_batch_log_pkey
ALTER TABLE ONLY public.command_batch_log
    ADD CONSTRAINT command_batch_log_pkey PRIMARY KEY (id);

-- Constraint: command_on_device_log command_on_device_log_pkey
ALTER TABLE ONLY public.command_on_device_log
    ADD CONSTRAINT command_on_device_log_pkey PRIMARY KEY (id);

-- Constraint: curtailment_automation_rule curtailment_automation_rule_pkey
ALTER TABLE ONLY public.curtailment_automation_rule
    ADD CONSTRAINT curtailment_automation_rule_pkey PRIMARY KEY (id);

-- Constraint: curtailment_automation_rule_profile_revision curtailment_automation_rule_profile_revision_pkey
ALTER TABLE ONLY public.curtailment_automation_rule_profile_revision
    ADD CONSTRAINT curtailment_automation_rule_profile_revision_pkey PRIMARY KEY (automation_rule_id);

-- Constraint: curtailment_automation_rule_state curtailment_automation_rule_state_pkey
ALTER TABLE ONLY public.curtailment_automation_rule_state
    ADD CONSTRAINT curtailment_automation_rule_state_pkey PRIMARY KEY (rule_id);

-- Constraint: curtailment_event curtailment_event_event_uuid_key
ALTER TABLE ONLY public.curtailment_event
    ADD CONSTRAINT curtailment_event_event_uuid_key UNIQUE (event_uuid);

-- Constraint: curtailment_event curtailment_event_pkey
ALTER TABLE ONLY public.curtailment_event
    ADD CONSTRAINT curtailment_event_pkey PRIMARY KEY (id);

-- Constraint: curtailment_mqtt_source_config curtailment_mqtt_source_config_pkey
ALTER TABLE ONLY public.curtailment_mqtt_source_config
    ADD CONSTRAINT curtailment_mqtt_source_config_pkey PRIMARY KEY (id);

-- Constraint: curtailment_mqtt_source_state curtailment_mqtt_source_state_pkey
ALTER TABLE ONLY public.curtailment_mqtt_source_state
    ADD CONSTRAINT curtailment_mqtt_source_state_pkey PRIMARY KEY (source_config_id);

-- Constraint: curtailment_org_config curtailment_org_config_pkey
ALTER TABLE ONLY public.curtailment_org_config
    ADD CONSTRAINT curtailment_org_config_pkey PRIMARY KEY (org_id);

-- Constraint: curtailment_reconciler_heartbeat curtailment_reconciler_heartbeat_pkey
ALTER TABLE ONLY public.curtailment_reconciler_heartbeat
    ADD CONSTRAINT curtailment_reconciler_heartbeat_pkey PRIMARY KEY (id);

-- Constraint: curtailment_response_profile curtailment_response_profile_pkey
ALTER TABLE ONLY public.curtailment_response_profile
    ADD CONSTRAINT curtailment_response_profile_pkey PRIMARY KEY (id);

-- Constraint: curtailment_response_profile_revision curtailment_response_profile_revision_pkey
ALTER TABLE ONLY public.curtailment_response_profile_revision
    ADD CONSTRAINT curtailment_response_profile_revision_pkey PRIMARY KEY (response_profile_id);

-- Constraint: curtailment_rig_config_reconciliation curtailment_rig_config_reconciliation_pkey
ALTER TABLE ONLY public.curtailment_rig_config_reconciliation
    ADD CONSTRAINT curtailment_rig_config_reconciliation_pkey PRIMARY KEY (organization_id);

-- Constraint: curtailment_rig_config_target curtailment_rig_config_target_pkey
ALTER TABLE ONLY public.curtailment_rig_config_target
    ADD CONSTRAINT curtailment_rig_config_target_pkey PRIMARY KEY (organization_id, device_id);

-- Constraint: curtailment_target curtailment_target_pkey
ALTER TABLE ONLY public.curtailment_target
    ADD CONSTRAINT curtailment_target_pkey PRIMARY KEY (curtailment_event_id, device_identifier);

-- Constraint: device_set_membership device_collection_membership_pkey
ALTER TABLE ONLY public.device_set_membership
    ADD CONSTRAINT device_collection_membership_pkey PRIMARY KEY (id);

-- Constraint: device_set device_collection_pkey
ALTER TABLE ONLY public.device_set
    ADD CONSTRAINT device_collection_pkey PRIMARY KEY (id);

-- Constraint: device_set_rack device_collection_rack_pkey
ALTER TABLE ONLY public.device_set_rack
    ADD CONSTRAINT device_collection_rack_pkey PRIMARY KEY (device_set_id);

-- Constraint: device_firmware_deployment device_firmware_deployment_pkey
ALTER TABLE ONLY public.device_firmware_deployment
    ADD CONSTRAINT device_firmware_deployment_pkey PRIMARY KEY (device_id);

-- Constraint: device_metrics device_metrics_pkey
ALTER TABLE ONLY public.device_metrics
    ADD CONSTRAINT device_metrics_pkey PRIMARY KEY ("time", device_identifier);

-- Constraint: device_pairing device_pairing_pkey
ALTER TABLE ONLY public.device_pairing
    ADD CONSTRAINT device_pairing_pkey PRIMARY KEY (id);

-- Constraint: device device_pkey
ALTER TABLE ONLY public.device
    ADD CONSTRAINT device_pkey PRIMARY KEY (id);

-- Constraint: device_status device_status_pkey
ALTER TABLE ONLY public.device_status
    ADD CONSTRAINT device_status_pkey PRIMARY KEY (id);

-- Constraint: discovered_device discovered_device_pkey
ALTER TABLE ONLY public.discovered_device
    ADD CONSTRAINT discovered_device_pkey PRIMARY KEY (id);

-- Constraint: errors errors_error_id_key
ALTER TABLE ONLY public.errors
    ADD CONSTRAINT errors_error_id_key UNIQUE (error_id);

-- Constraint: errors errors_pkey
ALTER TABLE ONLY public.errors
    ADD CONSTRAINT errors_pkey PRIMARY KEY (id);

-- Constraint: firmware_rollout_device firmware_rollout_device_pkey
ALTER TABLE ONLY public.firmware_rollout_device
    ADD CONSTRAINT firmware_rollout_device_pkey PRIMARY KEY (rollout_id, device_id);

-- Constraint: firmware_rollout_event firmware_rollout_event_pkey
ALTER TABLE ONLY public.firmware_rollout_event
    ADD CONSTRAINT firmware_rollout_event_pkey PRIMARY KEY (id);

-- Constraint: firmware_rollout firmware_rollout_pkey
ALTER TABLE ONLY public.firmware_rollout
    ADD CONSTRAINT firmware_rollout_pkey PRIMARY KEY (id);

-- Constraint: firmware_rollout_reservation firmware_rollout_reservation_pkey
ALTER TABLE ONLY public.firmware_rollout_reservation
    ADD CONSTRAINT firmware_rollout_reservation_pkey PRIMARY KEY (channel_id, device_id, batch_uuid);

-- Constraint: fleet_metric_rollup_90s fleet_metric_rollup_90s_pkey
ALTER TABLE ONLY public.fleet_metric_rollup_90s
    ADD CONSTRAINT fleet_metric_rollup_90s_pkey PRIMARY KEY (org_id, site_id, bucket);

-- Constraint: fleet_metric_rollup_progress fleet_metric_rollup_progress_pkey
ALTER TABLE ONLY public.fleet_metric_rollup_progress
    ADD CONSTRAINT fleet_metric_rollup_progress_pkey PRIMARY KEY (id);

-- Constraint: fleet_node_auth_challenge fleet_node_auth_challenge_pkey
ALTER TABLE ONLY public.fleet_node_auth_challenge
    ADD CONSTRAINT fleet_node_auth_challenge_pkey PRIMARY KEY (challenge);

-- Constraint: fleet_node_device fleet_node_device_pkey
ALTER TABLE ONLY public.fleet_node_device
    ADD CONSTRAINT fleet_node_device_pkey PRIMARY KEY (fleet_node_id, device_id);

-- Constraint: fleet_node fleet_node_pkey
ALTER TABLE ONLY public.fleet_node
    ADD CONSTRAINT fleet_node_pkey PRIMARY KEY (id);

-- Constraint: fleet_node_session fleet_node_session_pkey
ALTER TABLE ONLY public.fleet_node_session
    ADD CONSTRAINT fleet_node_session_pkey PRIMARY KEY (token_hash);

-- Constraint: fleet_runtime_lease fleet_runtime_lease_pkey
ALTER TABLE ONLY public.fleet_runtime_lease
    ADD CONSTRAINT fleet_runtime_lease_pkey PRIMARY KEY (lease_name);

-- Constraint: infrastructure_device infrastructure_device_pkey
ALTER TABLE ONLY public.infrastructure_device
    ADD CONSTRAINT infrastructure_device_pkey PRIMARY KEY (id);

-- Constraint: inventory_part inventory_part_pkey
ALTER TABLE ONLY public.inventory_part
    ADD CONSTRAINT inventory_part_pkey PRIMARY KEY (id);

-- Constraint: miner_credentials miner_credentials_pkey
ALTER TABLE ONLY public.miner_credentials
    ADD CONSTRAINT miner_credentials_pkey PRIMARY KEY (id);

-- Constraint: miner_state_snapshots miner_state_snapshots_pkey
ALTER TABLE ONLY public.miner_state_snapshots
    ADD CONSTRAINT miner_state_snapshots_pkey PRIMARY KEY ("time", device_identifier);

-- Constraint: notification_active notification_active_pkey
ALTER TABLE ONLY public.notification_active
    ADD CONSTRAINT notification_active_pkey PRIMARY KEY (organization_id, alert_key);

-- Constraint: notification_history notification_history_pkey
ALTER TABLE ONLY public.notification_history
    ADD CONSTRAINT notification_history_pkey PRIMARY KEY (id);

-- Constraint: organization organization_pkey
ALTER TABLE ONLY public.organization
    ADD CONSTRAINT organization_pkey PRIMARY KEY (id);

-- Constraint: pending_enrollment pending_enrollment_pkey
ALTER TABLE ONLY public.pending_enrollment
    ADD CONSTRAINT pending_enrollment_pkey PRIMARY KEY (id);

-- Constraint: permission permission_pkey
ALTER TABLE ONLY public.permission
    ADD CONSTRAINT permission_pkey PRIMARY KEY (id);

-- Constraint: pool pool_pkey
ALTER TABLE ONLY public.pool
    ADD CONSTRAINT pool_pkey PRIMARY KEY (id);

-- Constraint: queue_message queue_message_pkey
ALTER TABLE ONLY public.queue_message
    ADD CONSTRAINT queue_message_pkey PRIMARY KEY (id);

-- Constraint: rack_slot rack_slot_pkey
ALTER TABLE ONLY public.rack_slot
    ADD CONSTRAINT rack_slot_pkey PRIMARY KEY (device_set_id, device_id);

-- Constraint: release_channel_firmware release_channel_firmware_pkey
ALTER TABLE ONLY public.release_channel_firmware
    ADD CONSTRAINT release_channel_firmware_pkey PRIMARY KEY (channel_id, manufacturer, model);

-- Constraint: release_channel release_channel_org_id_name_key
ALTER TABLE ONLY public.release_channel
    ADD CONSTRAINT release_channel_org_id_name_key UNIQUE (org_id, name);

-- Constraint: release_channel release_channel_pkey
ALTER TABLE ONLY public.release_channel
    ADD CONSTRAINT release_channel_pkey PRIMARY KEY (id);

-- Constraint: release_channel_setting release_channel_setting_pkey
ALTER TABLE ONLY public.release_channel_setting
    ADD CONSTRAINT release_channel_setting_pkey PRIMARY KEY (organization_id);

-- Constraint: repair_ticket_comment repair_ticket_comment_pkey
ALTER TABLE ONLY public.repair_ticket_comment
    ADD CONSTRAINT repair_ticket_comment_pkey PRIMARY KEY (id);

-- Constraint: repair_ticket_counter repair_ticket_counter_pkey
ALTER TABLE ONLY public.repair_ticket_counter
    ADD CONSTRAINT repair_ticket_counter_pkey PRIMARY KEY (org_id);

-- Constraint: repair_ticket_part repair_ticket_part_pkey
ALTER TABLE ONLY public.repair_ticket_part
    ADD CONSTRAINT repair_ticket_part_pkey PRIMARY KEY (id);

-- Constraint: repair_ticket repair_ticket_pkey
ALTER TABLE ONLY public.repair_ticket
    ADD CONSTRAINT repair_ticket_pkey PRIMARY KEY (id);

-- Constraint: role_permission role_permission_pkey
ALTER TABLE ONLY public.role_permission
    ADD CONSTRAINT role_permission_pkey PRIMARY KEY (role_id, permission_id);

-- Constraint: role role_pkey
ALTER TABLE ONLY public.role
    ADD CONSTRAINT role_pkey PRIMARY KEY (id);

-- Constraint: schedule schedule_pkey
ALTER TABLE ONLY public.schedule
    ADD CONSTRAINT schedule_pkey PRIMARY KEY (id);

-- Constraint: schedule_target schedule_target_pkey
ALTER TABLE ONLY public.schedule_target
    ADD CONSTRAINT schedule_target_pkey PRIMARY KEY (id);

-- Constraint: session session_pkey
ALTER TABLE ONLY public.session
    ADD CONSTRAINT session_pkey PRIMARY KEY (id);

-- Constraint: site site_pkey
ALTER TABLE ONLY public.site
    ADD CONSTRAINT site_pkey PRIMARY KEY (id);

-- Constraint: device_set_membership uk_device_set_device
ALTER TABLE ONLY public.device_set_membership
    ADD CONSTRAINT uk_device_set_device UNIQUE (device_set_id, device_id);

-- Constraint: rack_slot uk_rack_slot_position
ALTER TABLE ONLY public.rack_slot
    ADD CONSTRAINT uk_rack_slot_position UNIQUE (device_set_id, "row", col);

-- Constraint: schedule_target uk_schedule_target
ALTER TABLE ONLY public.schedule_target
    ADD CONSTRAINT uk_schedule_target UNIQUE (schedule_id, target_type, target_id);

-- Constraint: command_on_device_log unique_batch_device
ALTER TABLE ONLY public.command_on_device_log
    ADD CONSTRAINT unique_batch_device UNIQUE (command_batch_log_id, device_id);

-- Constraint: alert_route_policy uq_alert_route_policy_org_rule
ALTER TABLE ONLY public.alert_route_policy
    ADD CONSTRAINT uq_alert_route_policy_org_rule UNIQUE (org_id, rule_uid);

-- Constraint: api_key uq_api_key_key_hash
ALTER TABLE ONLY public.api_key
    ADD CONSTRAINT uq_api_key_key_hash UNIQUE (key_hash);

-- Constraint: api_key uq_api_key_key_id
ALTER TABLE ONLY public.api_key
    ADD CONSTRAINT uq_api_key_key_id UNIQUE (key_id);

-- Constraint: api_key uq_api_key_prefix_org
ALTER TABLE ONLY public.api_key
    ADD CONSTRAINT uq_api_key_prefix_org UNIQUE (prefix, organization_id);

-- Constraint: building uq_building_id_org_id
ALTER TABLE ONLY public.building
    ADD CONSTRAINT uq_building_id_org_id UNIQUE (id, org_id);

-- Constraint: curtailment_automation_rule uq_curtailment_automation_rule_org_name
ALTER TABLE ONLY public.curtailment_automation_rule
    ADD CONSTRAINT uq_curtailment_automation_rule_org_name UNIQUE (org_id, rule_name);

-- Constraint: curtailment_mqtt_source_config uq_curtailment_mqtt_source_config_id_org
ALTER TABLE ONLY public.curtailment_mqtt_source_config
    ADD CONSTRAINT uq_curtailment_mqtt_source_config_id_org UNIQUE (id, organization_id);

-- Constraint: curtailment_mqtt_source_config uq_curtailment_mqtt_source_config_org_name
ALTER TABLE ONLY public.curtailment_mqtt_source_config
    ADD CONSTRAINT uq_curtailment_mqtt_source_config_org_name UNIQUE (organization_id, source_name);

-- Constraint: curtailment_response_profile uq_curtailment_response_profile_id_org
ALTER TABLE ONLY public.curtailment_response_profile
    ADD CONSTRAINT uq_curtailment_response_profile_id_org UNIQUE (id, org_id);

-- Constraint: curtailment_response_profile uq_curtailment_response_profile_org_name
ALTER TABLE ONLY public.curtailment_response_profile
    ADD CONSTRAINT uq_curtailment_response_profile_org_name UNIQUE (org_id, profile_name);

-- Constraint: device uq_device_id_org_id
ALTER TABLE ONLY public.device
    ADD CONSTRAINT uq_device_id_org_id UNIQUE (id, org_id);

-- Constraint: device_pairing uq_device_pairing_device_id
ALTER TABLE ONLY public.device_pairing
    ADD CONSTRAINT uq_device_pairing_device_id UNIQUE (device_id);

-- Constraint: device_set uq_device_set_id_org_id
ALTER TABLE ONLY public.device_set
    ADD CONSTRAINT uq_device_set_id_org_id UNIQUE (id, org_id);

-- Constraint: fleet_node_auth_challenge uq_fleet_node_auth_challenge_fleet_node_id
ALTER TABLE ONLY public.fleet_node_auth_challenge
    ADD CONSTRAINT uq_fleet_node_auth_challenge_fleet_node_id UNIQUE (fleet_node_id);

-- Constraint: fleet_node_device uq_fleet_node_device_device_id
ALTER TABLE ONLY public.fleet_node_device
    ADD CONSTRAINT uq_fleet_node_device_device_id UNIQUE (device_id);

-- Constraint: fleet_node uq_fleet_node_id_org_id
ALTER TABLE ONLY public.fleet_node
    ADD CONSTRAINT uq_fleet_node_id_org_id UNIQUE (id, org_id);

-- Constraint: fleet_node_session uq_fleet_node_session_fleet_node_id
ALTER TABLE ONLY public.fleet_node_session
    ADD CONSTRAINT uq_fleet_node_session_fleet_node_id UNIQUE (fleet_node_id);

-- Constraint: infrastructure_device uq_infrastructure_device_id_org_id
ALTER TABLE ONLY public.infrastructure_device
    ADD CONSTRAINT uq_infrastructure_device_id_org_id UNIQUE (id, org_id);

-- Constraint: inventory_part uq_inventory_part_id_org
ALTER TABLE ONLY public.inventory_part
    ADD CONSTRAINT uq_inventory_part_id_org UNIQUE (id, org_id);

-- Constraint: miner_credentials uq_miner_credentials_device_id
ALTER TABLE ONLY public.miner_credentials
    ADD CONSTRAINT uq_miner_credentials_device_id UNIQUE (device_id);

-- Constraint: organization uq_organization_org_id
ALTER TABLE ONLY public.organization
    ADD CONSTRAINT uq_organization_org_id UNIQUE (org_id);

-- Constraint: pending_enrollment uq_pending_enrollment_code_hash
ALTER TABLE ONLY public.pending_enrollment
    ADD CONSTRAINT uq_pending_enrollment_code_hash UNIQUE (code_hash);

-- Constraint: permission uq_permission_key
ALTER TABLE ONLY public.permission
    ADD CONSTRAINT uq_permission_key UNIQUE (key);

-- Constraint: repair_ticket uq_repair_ticket_id_org
ALTER TABLE ONLY public.repair_ticket
    ADD CONSTRAINT uq_repair_ticket_id_org UNIQUE (id, org_id);

-- Constraint: role uq_role_id_org_id
ALTER TABLE ONLY public.role
    ADD CONSTRAINT uq_role_id_org_id UNIQUE (id, organization_id);

-- Constraint: session uq_session_session_id
ALTER TABLE ONLY public.session
    ADD CONSTRAINT uq_session_session_id UNIQUE (session_id);

-- Constraint: site uq_site_id_org_id
ALTER TABLE ONLY public.site
    ADD CONSTRAINT uq_site_id_org_id UNIQUE (id, org_id);

-- Constraint: repair_ticket_part uq_ticket_inventory_part
ALTER TABLE ONLY public.repair_ticket_part
    ADD CONSTRAINT uq_ticket_inventory_part UNIQUE (ticket_id, inventory_part_id);

-- Constraint: user_organization uq_user_organization
ALTER TABLE ONLY public.user_organization
    ADD CONSTRAINT uq_user_organization UNIQUE (user_id, organization_id);

-- Constraint: user uq_user_user_id
ALTER TABLE ONLY public."user"
    ADD CONSTRAINT uq_user_user_id UNIQUE (user_id);

-- Constraint: user_organization user_organization_pkey
ALTER TABLE ONLY public.user_organization
    ADD CONSTRAINT user_organization_pkey PRIMARY KEY (id);

-- Constraint: user_organization_role user_organization_role_pkey
ALTER TABLE ONLY public.user_organization_role
    ADD CONSTRAINT user_organization_role_pkey PRIMARY KEY (id);

-- Constraint: user user_pkey
ALTER TABLE ONLY public."user"
    ADD CONSTRAINT user_pkey PRIMARY KEY (id);

-- Constraint: device_status ux_device_status_device_id
ALTER TABLE ONLY public.device_status
    ADD CONSTRAINT ux_device_status_device_id UNIQUE (device_id);

-- Index: device_metrics_time_idx
CREATE INDEX device_metrics_time_idx ON public.device_metrics USING btree ("time" DESC);

-- Index: firmware_rollout_event_channel_id
CREATE INDEX firmware_rollout_event_channel_id ON public.firmware_rollout_event USING btree (org_id, channel_id, id);

-- Index: firmware_rollout_event_org_id
CREATE INDEX firmware_rollout_event_org_id ON public.firmware_rollout_event USING btree (org_id, id);

-- Index: firmware_rollout_event_rollout_id
CREATE INDEX firmware_rollout_event_rollout_id ON public.firmware_rollout_event USING btree (org_id, rollout_id, id);

-- Index: fleet_metric_rollup_90s_bucket_idx
CREATE INDEX fleet_metric_rollup_90s_bucket_idx ON public.fleet_metric_rollup_90s USING btree (bucket DESC);

-- Index: idx_activity_log_batch_id
CREATE INDEX idx_activity_log_batch_id ON public.activity_log USING btree (batch_id) WHERE (batch_id IS NOT NULL);

-- Index: idx_activity_log_org_created
CREATE INDEX idx_activity_log_org_created ON public.activity_log USING btree (organization_id, created_at DESC, id DESC);

-- Index: idx_activity_log_org_site_created
CREATE INDEX idx_activity_log_org_site_created ON public.activity_log USING btree (organization_id, site_id, created_at DESC, id DESC);

-- Index: idx_activity_log_site_site
CREATE INDEX idx_activity_log_site_site ON public.activity_log_site USING btree (site_id, org_id) WHERE (site_id IS NOT NULL);

-- Index: idx_alert_channel_org
CREATE INDEX idx_alert_channel_org ON public.alert_channel USING btree (org_id) WHERE (deleted_at IS NULL);

-- Index: idx_alert_maintenance_window_org_ends
CREATE INDEX idx_alert_maintenance_window_org_ends ON public.alert_maintenance_window USING btree (org_id, ends_at);

-- Index: idx_alert_route_channel_channel
CREATE INDEX idx_alert_route_channel_channel ON public.alert_route_channel USING btree (channel_id);

-- Index: idx_api_key_fleet_node_id
CREATE INDEX idx_api_key_fleet_node_id ON public.api_key USING btree (fleet_node_id) WHERE (fleet_node_id IS NOT NULL);

-- Index: idx_api_key_organization_id
CREATE INDEX idx_api_key_organization_id ON public.api_key USING btree (organization_id);

-- Index: idx_api_key_user_id
CREATE INDEX idx_api_key_user_id ON public.api_key USING btree (user_id) WHERE (user_id IS NOT NULL);

-- Index: idx_building_org_deleted
CREATE INDEX idx_building_org_deleted ON public.building USING btree (org_id, deleted_at);

-- Index: idx_building_site_deleted
CREATE INDEX idx_building_site_deleted ON public.building USING btree (site_id, deleted_at);

-- Index: idx_command_batch_log_created_by
CREATE INDEX idx_command_batch_log_created_by ON public.command_batch_log USING btree (created_by);

-- Index: idx_command_batch_log_organization_id
CREATE INDEX idx_command_batch_log_organization_id ON public.command_batch_log USING btree (organization_id) WHERE (organization_id IS NOT NULL);

-- Index: idx_command_batch_log_status
CREATE INDEX idx_command_batch_log_status ON public.command_batch_log USING btree (status);

-- Index: idx_command_batch_log_type
CREATE INDEX idx_command_batch_log_type ON public.command_batch_log USING btree (type);

-- Index: idx_command_batch_log_uuid
CREATE UNIQUE INDEX idx_command_batch_log_uuid ON public.command_batch_log USING btree (uuid);

-- Index: idx_command_on_device_log_batch_id
CREATE INDEX idx_command_on_device_log_batch_id ON public.command_on_device_log USING btree (command_batch_log_id);

-- Index: idx_command_on_device_log_device_id
CREATE INDEX idx_command_on_device_log_device_id ON public.command_on_device_log USING btree (device_id);

-- Index: idx_command_on_device_log_site
CREATE INDEX idx_command_on_device_log_site ON public.command_on_device_log USING btree (site_id);

-- Index: idx_curtailment_automation_rule_mqtt_source
CREATE INDEX idx_curtailment_automation_rule_mqtt_source ON public.curtailment_automation_rule USING btree (mqtt_source_id) WHERE (enabled = true);

-- Index: idx_curtailment_automation_rule_org
CREATE INDEX idx_curtailment_automation_rule_org ON public.curtailment_automation_rule USING btree (org_id);

-- Index: idx_curtailment_automation_rule_response_profile
CREATE INDEX idx_curtailment_automation_rule_response_profile ON public.curtailment_automation_rule USING btree (response_profile_id);

-- Index: idx_curtailment_event_active
CREATE INDEX idx_curtailment_event_active ON public.curtailment_event USING btree (org_id, state, started_at DESC) WHERE (state = ANY (ARRAY['pending'::text, 'active'::text, 'restoring'::text]));

-- Index: idx_curtailment_event_active_facility_fans
CREATE INDEX idx_curtailment_event_active_facility_fans ON public.curtailment_event USING gin (facility_fan_device_ids) WHERE ((state = ANY (ARRAY['pending'::text, 'active'::text, 'restoring'::text])) OR (fan_last_error IS NOT NULL));

-- Index: idx_curtailment_event_org_created
CREATE INDEX idx_curtailment_event_org_created ON public.curtailment_event USING btree (org_id, created_at);

-- Index: idx_curtailment_event_org_id_desc
CREATE INDEX idx_curtailment_event_org_id_desc ON public.curtailment_event USING btree (org_id, id DESC);

-- Index: idx_curtailment_event_org_recent_end
CREATE INDEX idx_curtailment_event_org_recent_end ON public.curtailment_event USING btree (org_id, ended_at DESC, id) WHERE (ended_at IS NOT NULL);

-- Index: idx_curtailment_mqtt_source_config_enabled
CREATE INDEX idx_curtailment_mqtt_source_config_enabled ON public.curtailment_mqtt_source_config USING btree (enabled) WHERE (enabled = true);

-- Index: idx_curtailment_response_profile_facility_fans
CREATE INDEX idx_curtailment_response_profile_facility_fans ON public.curtailment_response_profile USING gin (facility_fan_device_ids);

-- Index: idx_curtailment_response_profile_org_site
CREATE INDEX idx_curtailment_response_profile_org_site ON public.curtailment_response_profile USING btree (org_id, site_id);

-- Index: idx_curtailment_rig_config_reconciliation_due
CREATE INDEX idx_curtailment_rig_config_reconciliation_due ON public.curtailment_rig_config_reconciliation USING btree (retry_at, organization_id);

-- Index: idx_curtailment_target_active_by_device
CREATE INDEX idx_curtailment_target_active_by_device ON public.curtailment_target USING btree (device_identifier, curtailment_event_id) WHERE (state <> ALL (ARRAY['resolved'::text, 'restore_failed'::text, 'released'::text]));

-- Index: idx_curtailment_target_event_state
CREATE INDEX idx_curtailment_target_event_state ON public.curtailment_target USING btree (curtailment_event_id, state);

-- Index: idx_curtailment_target_pending_work
CREATE INDEX idx_curtailment_target_pending_work ON public.curtailment_target USING btree (curtailment_event_id, state) WHERE (state = ANY (ARRAY['pending'::text, 'dispatched'::text, 'drifted'::text]));

-- Index: idx_curtailment_target_terminal_by_device
CREATE INDEX idx_curtailment_target_terminal_by_device ON public.curtailment_target USING btree (device_identifier, curtailment_event_id) WHERE (state = ANY (ARRAY['resolved'::text, 'restore_failed'::text]));

-- Index: idx_curtailment_target_terminal_by_event
CREATE INDEX idx_curtailment_target_terminal_by_event ON public.curtailment_target USING btree (curtailment_event_id, device_identifier) WHERE (state = ANY (ARRAY['resolved'::text, 'restore_failed'::text]));

-- Index: idx_curtailment_target_unavailable_reason
CREATE INDEX idx_curtailment_target_unavailable_reason ON public.curtailment_target USING btree (curtailment_event_id, last_error) WHERE (state = 'unavailable'::text);

-- Index: idx_dcm_device_identifier
CREATE INDEX idx_dcm_device_identifier ON public.device_set_membership USING btree (device_identifier);

-- Index: idx_dcm_org_collection
CREATE INDEX idx_dcm_org_collection ON public.device_set_membership USING btree (org_id, device_set_id);

-- Index: idx_dcm_org_device
CREATE INDEX idx_dcm_org_device ON public.device_set_membership USING btree (org_id, device_id);

-- Index: idx_dcm_org_type
CREATE INDEX idx_dcm_org_type ON public.device_set_membership USING btree (org_id, device_set_type);

-- Index: idx_device_collection_org_deleted
CREATE INDEX idx_device_collection_org_deleted ON public.device_set USING btree (org_id, deleted_at);

-- Index: idx_device_collection_org_type
CREATE INDEX idx_device_collection_org_type ON public.device_set USING btree (org_id, type);

-- Index: idx_device_discovered_device_id
CREATE INDEX idx_device_discovered_device_id ON public.device USING btree (discovered_device_id);

-- Index: idx_device_metrics_device_identifier
CREATE INDEX idx_device_metrics_device_identifier ON public.device_metrics USING btree (device_identifier, "time" DESC);

-- Index: idx_device_metrics_site_time
CREATE INDEX idx_device_metrics_site_time ON public.device_metrics USING btree (site_id, "time" DESC) WHERE (site_id IS NOT NULL);

-- Index: idx_device_org_building
CREATE INDEX idx_device_org_building ON public.device USING btree (org_id, building_id);

-- Index: idx_device_org_mac_active
CREATE INDEX idx_device_org_mac_active ON public.device USING btree (org_id, mac_address) WHERE (deleted_at IS NULL);

-- Index: idx_device_org_site
CREATE INDEX idx_device_org_site ON public.device USING btree (org_id, site_id);

-- Index: idx_device_pairing_device_id_pairing_status
CREATE INDEX idx_device_pairing_device_id_pairing_status ON public.device_pairing USING btree (device_id, pairing_status);

-- Index: idx_device_set_rack_building
CREATE INDEX idx_device_set_rack_building ON public.device_set_rack USING btree (building_id);

-- Index: idx_device_set_rack_site
CREATE INDEX idx_device_set_rack_site ON public.device_set_rack USING btree (org_id, site_id);

-- Index: idx_device_set_rack_zone
CREATE INDEX idx_device_set_rack_zone ON public.device_set_rack USING btree (zone);

-- Index: idx_device_sort_mac
CREATE INDEX idx_device_sort_mac ON public.device USING btree (mac_address, discovered_device_id);

-- Index: idx_device_sort_worker_name
CREATE INDEX idx_device_sort_worker_name ON public.device USING btree (org_id, worker_name, discovered_device_id) WHERE (deleted_at IS NULL);

-- Index: idx_device_status_device_id_status_timestamp
CREATE INDEX idx_device_status_device_id_status_timestamp ON public.device_status USING btree (device_id, status_timestamp);

-- Index: idx_device_status_sort_status
CREATE INDEX idx_device_status_sort_status ON public.device_status USING btree (status, device_id);

-- Index: idx_discovered_device_fleet_node_active
CREATE INDEX idx_discovered_device_fleet_node_active ON public.discovered_device USING btree (org_id, discovered_by_fleet_node_id, id) WHERE ((is_active = true) AND (deleted_at IS NULL));

-- Index: idx_discovered_device_fleet_node_attribution
CREATE INDEX idx_discovered_device_fleet_node_attribution ON public.discovered_device USING btree (discovered_by_fleet_node_id) WHERE (discovered_by_fleet_node_id IS NOT NULL);

-- Index: idx_discovered_device_ip
CREATE INDEX idx_discovered_device_ip ON public.discovered_device USING btree (ip_address, port, url_scheme);

-- Index: idx_discovered_device_ip_inet_gist
CREATE INDEX idx_discovered_device_ip_inet_gist ON public.discovered_device USING gist (ip_address_inet inet_ops);

-- Index: idx_discovered_device_org_active
CREATE INDEX idx_discovered_device_org_active ON public.discovered_device USING btree (org_id, is_active, deleted_at);

-- Index: idx_discovered_device_sort_firmware
CREATE INDEX idx_discovered_device_sort_firmware ON public.discovered_device USING btree (org_id, firmware_version, id);

-- Index: idx_discovered_device_sort_ip
CREATE INDEX idx_discovered_device_sort_ip ON public.discovered_device USING btree (org_id, ((COALESCE(NULLIF((ip_address)::text, ''::text), '0.0.0.0'::text))::inet), id);

-- Index: idx_discovered_device_sort_model
CREATE INDEX idx_discovered_device_sort_model ON public.discovered_device USING btree (org_id, model, id);

-- Index: idx_errors_dedup
CREATE INDEX idx_errors_dedup ON public.errors USING btree (org_id, device_id, miner_error, component_id, component_type);

-- Index: idx_errors_device_open
CREATE INDEX idx_errors_device_open ON public.errors USING btree (device_id) WHERE (closed_at IS NULL);

-- Index: idx_errors_open
CREATE INDEX idx_errors_open ON public.errors USING btree (org_id, closed_at, severity);

-- Index: idx_errors_org_component
CREATE INDEX idx_errors_org_component ON public.errors USING btree (org_id, component_id, component_type);

-- Index: idx_errors_org_last_seen
CREATE INDEX idx_errors_org_last_seen ON public.errors USING btree (org_id, last_seen_at DESC);

-- Index: idx_errors_org_miner_error
CREATE INDEX idx_errors_org_miner_error ON public.errors USING btree (org_id, miner_error);

-- Index: idx_errors_org_severity
CREATE INDEX idx_errors_org_severity ON public.errors USING btree (org_id, severity);

-- Index: idx_errors_org_site_last_seen
CREATE INDEX idx_errors_org_site_last_seen ON public.errors USING btree (org_id, site_id, last_seen_at DESC);

-- Index: idx_errors_pagination
CREATE INDEX idx_errors_pagination ON public.errors USING btree (org_id, last_seen_at DESC, id DESC);

-- Index: idx_firmware_rollout_assignment_history
CREATE INDEX idx_firmware_rollout_assignment_history ON public.firmware_rollout USING btree (channel_id, public.release_channel_pair_key(manufacturer), public.release_channel_pair_key(model), assignment_generation, id DESC);

-- Index: idx_firmware_rollout_device_device
CREATE INDEX idx_firmware_rollout_device_device ON public.firmware_rollout_device USING btree (device_id);

-- Index: idx_firmware_rollout_org_created
CREATE INDEX idx_firmware_rollout_org_created ON public.firmware_rollout USING btree (org_id, created_at DESC, id DESC);

-- Index: idx_firmware_rollout_org_revision_txid
CREATE INDEX idx_firmware_rollout_org_revision_txid ON public.firmware_rollout USING btree (org_id, revision_txid);

-- Index: idx_firmware_rollout_org_updated
CREATE INDEX idx_firmware_rollout_org_updated ON public.firmware_rollout USING btree (org_id, updated_at);

-- Index: idx_fleet_metric_rollup_90s_org_bucket
CREATE INDEX idx_fleet_metric_rollup_90s_org_bucket ON public.fleet_metric_rollup_90s USING btree (org_id, bucket DESC);

-- Index: idx_fleet_node_auth_challenge_expires_at
CREATE INDEX idx_fleet_node_auth_challenge_expires_at ON public.fleet_node_auth_challenge USING btree (expires_at);

-- Index: idx_fleet_node_device_org_id
CREATE INDEX idx_fleet_node_device_org_id ON public.fleet_node_device USING btree (org_id);

-- Index: idx_fleet_node_org_id
CREATE INDEX idx_fleet_node_org_id ON public.fleet_node USING btree (org_id);

-- Index: idx_fleet_node_session_expires_at
CREATE INDEX idx_fleet_node_session_expires_at ON public.fleet_node_session USING btree (expires_at);

-- Index: idx_infrastructure_device_org_deleted
CREATE INDEX idx_infrastructure_device_org_deleted ON public.infrastructure_device USING btree (org_id, deleted_at);

-- Index: idx_infrastructure_device_site_deleted
CREATE INDEX idx_infrastructure_device_site_deleted ON public.infrastructure_device USING btree (site_id, deleted_at);

-- Index: idx_inventory_part_org_site
CREATE INDEX idx_inventory_part_org_site ON public.inventory_part USING btree (org_id, site_id) WHERE (deleted_at IS NULL);

-- Index: idx_miner_credentials_device_id
CREATE INDEX idx_miner_credentials_device_id ON public.miner_credentials USING btree (device_id);

-- Index: idx_miner_state_snapshots_device_time
CREATE INDEX idx_miner_state_snapshots_device_time ON public.miner_state_snapshots USING btree (device_identifier, "time" DESC);

-- Index: idx_miner_state_snapshots_org_site_time
CREATE INDEX idx_miner_state_snapshots_org_site_time ON public.miner_state_snapshots USING btree (org_id, site_id, "time" DESC) WHERE (site_id IS NOT NULL);

-- Index: idx_miner_state_snapshots_org_time
CREATE INDEX idx_miner_state_snapshots_org_time ON public.miner_state_snapshots USING btree (org_id, "time" DESC);

-- Index: idx_notification_active_org_alert_key
CREATE INDEX idx_notification_active_org_alert_key ON public.notification_active USING btree (organization_id, alert_name, rule_group, alert_key) WHERE (status = 'firing'::text);

-- Index: idx_notification_active_org_recent
CREATE INDEX idx_notification_active_org_recent ON public.notification_active USING btree (organization_id, received_at DESC, history_id DESC) WHERE (status = 'firing'::text);

-- Index: idx_notification_active_org_rollup
CREATE INDEX idx_notification_active_org_rollup ON public.notification_active USING btree (organization_id, alert_name, rule_group, device_id) INCLUDE (starts_at, received_at) WHERE (status = 'firing'::text);

-- Index: idx_notification_history_fingerprint
CREATE INDEX idx_notification_history_fingerprint ON public.notification_history USING btree (fingerprint) WHERE (fingerprint <> ''::text);

-- Index: idx_notification_history_org_id
CREATE INDEX idx_notification_history_org_id ON public.notification_history USING btree (organization_id, id DESC);

-- Index: idx_notification_history_org_received
CREATE INDEX idx_notification_history_org_received ON public.notification_history USING btree (organization_id, received_at DESC);

-- Index: idx_notification_history_org_unresolved_id
CREATE INDEX idx_notification_history_org_unresolved_id ON public.notification_history USING btree (organization_id, id DESC) WHERE (status <> 'resolved'::text);

-- Index: idx_notification_history_received
CREATE INDEX idx_notification_history_received ON public.notification_history USING btree (received_at DESC);

-- Index: idx_notification_metric_sample_metric_time
CREATE INDEX idx_notification_metric_sample_metric_time ON public.notification_metric_sample USING btree (metric, "time" DESC);

-- Index: idx_one_active_rollout_per_pair
CREATE UNIQUE INDEX idx_one_active_rollout_per_pair ON public.firmware_rollout USING btree (channel_id, public.release_channel_pair_key(manufacturer), public.release_channel_pair_key(model)) WHERE (status = 'active'::text);

-- Index: idx_one_rack_per_device
CREATE UNIQUE INDEX idx_one_rack_per_device ON public.device_set_membership USING btree (device_id) WHERE (device_set_type = 'rack'::public.device_set_type);

-- Index: idx_pending_enrollment_expires_at
CREATE INDEX idx_pending_enrollment_expires_at ON public.pending_enrollment USING btree (expires_at);

-- Index: idx_pending_enrollment_fleet_node_id
CREATE INDEX idx_pending_enrollment_fleet_node_id ON public.pending_enrollment USING btree (fleet_node_id) WHERE (fleet_node_id IS NOT NULL);

-- Index: idx_pending_enrollment_org_status
CREATE INDEX idx_pending_enrollment_org_status ON public.pending_enrollment USING btree (org_id, status);

-- Index: idx_pool_org_id_url
CREATE INDEX idx_pool_org_id_url ON public.pool USING btree (org_id, url);

-- Index: idx_queue_message_batch_uuid
CREATE INDEX idx_queue_message_batch_uuid ON public.queue_message USING btree (command_batch_log_uuid);

-- Index: idx_queue_message_device_status_created
CREATE INDEX idx_queue_message_device_status_created ON public.queue_message USING btree (device_id, status, created_at);

-- Index: idx_queue_message_pending_created
CREATE INDEX idx_queue_message_pending_created ON public.queue_message USING btree (created_at) WHERE (status = 'PENDING'::public.queue_status_enum);

-- Index: idx_queue_message_reaper
CREATE INDEX idx_queue_message_reaper ON public.queue_message USING btree (updated_at) WHERE (status = 'PROCESSING'::public.queue_status_enum);

-- Index: idx_release_channel_firmware_pair
CREATE UNIQUE INDEX idx_release_channel_firmware_pair ON public.release_channel_firmware USING btree (channel_id, public.release_channel_pair_key(manufacturer), public.release_channel_pair_key(model));

-- Index: idx_release_channel_target_id
CREATE UNIQUE INDEX idx_release_channel_target_id ON public.release_channel_target USING btree (channel_id, target_type, target_id) WHERE (target_type <> 'miner'::text);

-- Index: idx_release_channel_target_lookup
CREATE INDEX idx_release_channel_target_lookup ON public.release_channel_target USING btree (target_type, target_id);

-- Index: idx_release_channel_target_miner
CREATE UNIQUE INDEX idx_release_channel_target_miner ON public.release_channel_target USING btree (channel_id, device_identifier) WHERE (target_type = 'miner'::text);

-- Index: idx_repair_ticket_assignee
CREATE INDEX idx_repair_ticket_assignee ON public.repair_ticket USING btree (org_id, assignee_user_id) WHERE ((deleted_at IS NULL) AND (assignee_user_id IS NOT NULL));

-- Index: idx_repair_ticket_miner
CREATE INDEX idx_repair_ticket_miner ON public.repair_ticket USING btree (org_id, miner_identifier) WHERE ((deleted_at IS NULL) AND (miner_identifier IS NOT NULL));

-- Index: idx_repair_ticket_org_building
CREATE INDEX idx_repair_ticket_org_building ON public.repair_ticket USING btree (org_id, building_id) WHERE ((deleted_at IS NULL) AND (building_id IS NOT NULL));

-- Index: idx_repair_ticket_org_site
CREATE INDEX idx_repair_ticket_org_site ON public.repair_ticket USING btree (org_id, site_id) WHERE ((deleted_at IS NULL) AND (site_id IS NOT NULL));

-- Index: idx_repair_ticket_org_status
CREATE INDEX idx_repair_ticket_org_status ON public.repair_ticket USING btree (org_id, status) WHERE (deleted_at IS NULL);

-- Index: idx_repair_ticket_rack
CREATE INDEX idx_repair_ticket_rack ON public.repair_ticket USING btree (org_id, rack_id) WHERE ((deleted_at IS NULL) AND (rack_id IS NOT NULL));

-- Index: idx_schedule_next_run
CREATE INDEX idx_schedule_next_run ON public.schedule USING btree (next_run_at, status) WHERE ((status = 'active'::text) AND (deleted_at IS NULL));

-- Index: idx_schedule_org_status
CREATE INDEX idx_schedule_org_status ON public.schedule USING btree (org_id, status) WHERE (deleted_at IS NULL);

-- Index: idx_session_expires_at
CREATE INDEX idx_session_expires_at ON public.session USING btree (expires_at);

-- Index: idx_session_user_id
CREATE INDEX idx_session_user_id ON public.session USING btree (user_id);

-- Index: idx_site_org_deleted
CREATE INDEX idx_site_org_deleted ON public.site USING btree (org_id, deleted_at);

-- Index: idx_ticket_comment_ticket
CREATE INDEX idx_ticket_comment_ticket ON public.repair_ticket_comment USING btree (ticket_id) WHERE (deleted_at IS NULL);

-- Index: idx_ticket_part_inventory
CREATE INDEX idx_ticket_part_inventory ON public.repair_ticket_part USING btree (inventory_part_id);

-- Index: idx_ticket_part_ticket
CREATE INDEX idx_ticket_part_ticket ON public.repair_ticket_part USING btree (ticket_id);

-- Index: idx_user_organization_role_user_org
CREATE INDEX idx_user_organization_role_user_org ON public.user_organization_role USING btree (user_id, organization_id) WHERE (deleted_at IS NULL);

-- Index: miner_state_snapshots_time_idx
CREATE INDEX miner_state_snapshots_time_idx ON public.miner_state_snapshots USING btree ("time" DESC);

-- Index: notification_metric_sample_time_idx
CREATE INDEX notification_metric_sample_time_idx ON public.notification_metric_sample USING btree ("time" DESC);

-- Index: uk_building_site_name
CREATE UNIQUE INDEX uk_building_site_name ON public.building USING btree (site_id, name) WHERE ((site_id IS NOT NULL) AND (deleted_at IS NULL));

-- Index: uk_device_collection_org_type_label
CREATE UNIQUE INDEX uk_device_collection_org_type_label ON public.device_set USING btree (org_id, type, label) WHERE (deleted_at IS NULL);

-- Index: uk_device_set_rack_building_position
CREATE UNIQUE INDEX uk_device_set_rack_building_position ON public.device_set_rack USING btree (building_id, aisle_index, position_in_aisle) WHERE ((building_id IS NOT NULL) AND (aisle_index IS NOT NULL) AND (position_in_aisle IS NOT NULL));

-- Index: uk_discovered_device_org_identifier
CREATE UNIQUE INDEX uk_discovered_device_org_identifier ON public.discovered_device USING btree (org_id, device_identifier) WHERE (deleted_at IS NULL);

-- Index: uk_infrastructure_device_site_name
CREATE UNIQUE INDEX uk_infrastructure_device_site_name ON public.infrastructure_device USING btree (site_id, name) WHERE (deleted_at IS NULL);

-- Index: uk_inventory_part_site_name
CREATE UNIQUE INDEX uk_inventory_part_site_name ON public.inventory_part USING btree (org_id, COALESCE(site_id, (0)::bigint), lower((name)::text)) WHERE (deleted_at IS NULL);

-- Index: uk_pool_org_url_username
CREATE UNIQUE INDEX uk_pool_org_url_username ON public.pool USING btree (org_id, url, username) WHERE (deleted_at IS NULL);

-- Index: uk_repair_ticket_number_org
CREATE UNIQUE INDEX uk_repair_ticket_number_org ON public.repair_ticket USING btree (org_id, ticket_number) WHERE (deleted_at IS NULL);

-- Index: uk_schedule_org_priority
CREATE UNIQUE INDEX uk_schedule_org_priority ON public.schedule USING btree (org_id, priority) WHERE (deleted_at IS NULL);

-- Index: uk_site_org_name
CREATE UNIQUE INDEX uk_site_org_name ON public.site USING btree (org_id, name) WHERE (deleted_at IS NULL);

-- Index: uk_site_org_slug
CREATE UNIQUE INDEX uk_site_org_slug ON public.site USING btree (org_id, slug) WHERE (deleted_at IS NULL);

-- Index: uq_activity_log_batch_completed
CREATE UNIQUE INDEX uq_activity_log_batch_completed ON public.activity_log USING btree (batch_id, event_type) WHERE ((batch_id IS NOT NULL) AND (event_type ~~ '%.completed'::text));

-- Index: uq_activity_log_site
CREATE UNIQUE INDEX uq_activity_log_site ON public.activity_log_site USING btree (activity_log_id, site_id);

-- Index: uq_alert_channel_org_name
CREATE UNIQUE INDEX uq_alert_channel_org_name ON public.alert_channel USING btree (org_id, name) WHERE (deleted_at IS NULL);

-- Index: uq_curtailment_event_external_ref
CREATE UNIQUE INDEX uq_curtailment_event_external_ref ON public.curtailment_event USING btree (org_id, external_source, external_reference) WHERE ((external_source IS NOT NULL) AND (external_reference IS NOT NULL) AND (state = ANY (ARRAY['pending'::text, 'active'::text, 'restoring'::text])));

-- Index: uq_curtailment_event_idempotency
CREATE UNIQUE INDEX uq_curtailment_event_idempotency ON public.curtailment_event USING btree (org_id, idempotency_key) WHERE ((idempotency_key IS NOT NULL) AND (state = ANY (ARRAY['pending'::text, 'active'::text, 'restoring'::text])));

-- Index: uq_curtailment_target_one_non_terminal_per_device
CREATE UNIQUE INDEX uq_curtailment_target_one_non_terminal_per_device ON public.curtailment_target USING btree (device_identifier) WHERE (state <> ALL (ARRAY['resolved'::text, 'restore_failed'::text, 'released'::text]));

-- Index: uq_device_device_identifier
CREATE UNIQUE INDEX uq_device_device_identifier ON public.device USING btree (device_identifier) WHERE (deleted_at IS NULL);

-- Index: uq_device_serial_number
CREATE UNIQUE INDEX uq_device_serial_number ON public.device USING btree (serial_number) WHERE (deleted_at IS NULL);

-- Index: uq_fleet_node_identity_pubkey
CREATE UNIQUE INDEX uq_fleet_node_identity_pubkey ON public.fleet_node USING btree (identity_pubkey) WHERE (deleted_at IS NULL);

-- Index: uq_fleet_node_org_name
CREATE UNIQUE INDEX uq_fleet_node_org_name ON public.fleet_node USING btree (org_id, name) WHERE (deleted_at IS NULL);

-- Index: uq_repair_ticket_comment_org_idempotency_key
CREATE UNIQUE INDEX uq_repair_ticket_comment_org_idempotency_key ON public.repair_ticket_comment USING btree (org_id, idempotency_key) WHERE (idempotency_key IS NOT NULL);

-- Index: uq_repair_ticket_org_idempotency_key
CREATE UNIQUE INDEX uq_repair_ticket_org_idempotency_key ON public.repair_ticket USING btree (org_id, idempotency_key) WHERE (idempotency_key IS NOT NULL);

-- Index: uq_role_org_builtin_key
CREATE UNIQUE INDEX uq_role_org_builtin_key ON public.role USING btree (organization_id, builtin_key) WHERE ((is_builtin = true) AND (deleted_at IS NULL));

-- Index: uq_role_org_custom_name
CREATE UNIQUE INDEX uq_role_org_custom_name ON public.role USING btree (organization_id, lower(btrim((name)::text))) WHERE ((is_builtin = false) AND (deleted_at IS NULL));

-- Index: uq_user_org_role_org_scope
CREATE UNIQUE INDEX uq_user_org_role_org_scope ON public.user_organization_role USING btree (user_id, organization_id, role_id) WHERE (((scope_type)::text = 'org'::text) AND (deleted_at IS NULL));

-- Index: uq_user_org_role_site_scope
CREATE UNIQUE INDEX uq_user_org_role_site_scope ON public.user_organization_role USING btree (user_id, organization_id, role_id, scope_id) WHERE (((scope_type)::text = 'site'::text) AND (deleted_at IS NULL));

-- Index: uq_user_org_single_live_org_scope
CREATE UNIQUE INDEX uq_user_org_single_live_org_scope ON public.user_organization_role USING btree (user_id, organization_id) WHERE (((scope_type)::text = 'org'::text) AND (deleted_at IS NULL));

-- Index: uq_user_username
CREATE UNIQUE INDEX uq_user_username ON public."user" USING btree (username) WHERE (deleted_at IS NULL);

-- Trigger: curtailment_event bind_legacy_curtailment_automation_event_revision
CREATE TRIGGER bind_legacy_curtailment_automation_event_revision BEFORE INSERT ON public.curtailment_event FOR EACH ROW EXECUTE FUNCTION public.bind_legacy_curtailment_automation_event_revision();

-- Trigger: curtailment_response_profile canonicalize_curtailment_response_profile_scope
CREATE TRIGGER canonicalize_curtailment_response_profile_scope BEFORE INSERT OR UPDATE OF site_id, scope_json ON public.curtailment_response_profile FOR EACH ROW EXECUTE FUNCTION public.canonicalize_curtailment_response_profile_scope();

-- Trigger: device_set_rack clear_infrastructure_rack_on_placement_change
CREATE TRIGGER clear_infrastructure_rack_on_placement_change AFTER UPDATE OF site_id, building_id ON public.device_set_rack FOR EACH ROW EXECUTE FUNCTION public.clear_infrastructure_rack_on_placement_change();

-- Trigger: device_firmware_deployment device_firmware_deployment_deleted
CREATE TRIGGER device_firmware_deployment_deleted AFTER DELETE ON public.device_firmware_deployment REFERENCING OLD TABLE AS changed_rows FOR EACH STATEMENT EXECUTE FUNCTION public.firmware_rollout_touch_from_rows();

-- Trigger: device_firmware_deployment device_firmware_deployment_inserted
CREATE TRIGGER device_firmware_deployment_inserted AFTER INSERT ON public.device_firmware_deployment REFERENCING NEW TABLE AS changed_rows FOR EACH STATEMENT EXECUTE FUNCTION public.firmware_rollout_touch_from_rows();

-- Trigger: device_firmware_deployment device_firmware_deployment_updated
CREATE TRIGGER device_firmware_deployment_updated AFTER UPDATE ON public.device_firmware_deployment REFERENCING OLD TABLE AS previous_rows NEW TABLE AS changed_rows FOR EACH STATEMENT EXECUTE FUNCTION public.firmware_rollout_touch_from_rows();

-- Trigger: firmware_rollout_device firmware_rollout_device_deleted
CREATE TRIGGER firmware_rollout_device_deleted AFTER DELETE ON public.firmware_rollout_device REFERENCING OLD TABLE AS changed_rows FOR EACH STATEMENT EXECUTE FUNCTION public.firmware_rollout_touch_from_rows();

-- Trigger: firmware_rollout_device firmware_rollout_device_inserted
CREATE TRIGGER firmware_rollout_device_inserted AFTER INSERT ON public.firmware_rollout_device REFERENCING NEW TABLE AS changed_rows FOR EACH STATEMENT EXECUTE FUNCTION public.firmware_rollout_touch_from_rows();

-- Trigger: firmware_rollout_device firmware_rollout_device_updated
CREATE TRIGGER firmware_rollout_device_updated AFTER UPDATE ON public.firmware_rollout_device REFERENCING OLD TABLE AS previous_rows NEW TABLE AS changed_rows FOR EACH STATEMENT EXECUTE FUNCTION public.firmware_rollout_touch_from_rows();

-- Trigger: firmware_rollout firmware_rollout_revision
CREATE TRIGGER firmware_rollout_revision BEFORE INSERT OR UPDATE ON public.firmware_rollout FOR EACH ROW EXECUTE FUNCTION public.firmware_rollout_bump_revision();

-- Trigger: notification_history notification_history_active_sync
CREATE TRIGGER notification_history_active_sync AFTER INSERT ON public.notification_history FOR EACH ROW EXECUTE FUNCTION public.notification_active_sync();

-- Trigger: curtailment_automation_rule sync_curtailment_automation_rule_profile_revision
CREATE TRIGGER sync_curtailment_automation_rule_profile_revision AFTER INSERT OR UPDATE OF response_profile_id ON public.curtailment_automation_rule FOR EACH ROW EXECUTE FUNCTION public.sync_curtailment_automation_rule_profile_revision();

-- Trigger: curtailment_response_profile sync_curtailment_response_profile_revision
CREATE TRIGGER sync_curtailment_response_profile_revision AFTER INSERT OR UPDATE ON public.curtailment_response_profile FOR EACH ROW EXECUTE FUNCTION public.sync_curtailment_response_profile_revision();

-- Trigger: device_set sync_infrastructure_rack_label
CREATE TRIGGER sync_infrastructure_rack_label AFTER UPDATE OF label, deleted_at ON public.device_set FOR EACH ROW EXECUTE FUNCTION public.sync_infrastructure_rack_label();

-- Trigger: building update_building_updated_at
CREATE TRIGGER update_building_updated_at BEFORE UPDATE ON public.building FOR EACH ROW EXECUTE FUNCTION public.update_updated_at_column();

-- Trigger: command_on_device_log update_command_on_device_log_updated_at
CREATE TRIGGER update_command_on_device_log_updated_at BEFORE UPDATE ON public.command_on_device_log FOR EACH ROW EXECUTE FUNCTION public.update_updated_at_column();

-- Trigger: curtailment_automation_rule_state update_curtailment_automation_rule_state_updated_at
CREATE TRIGGER update_curtailment_automation_rule_state_updated_at BEFORE UPDATE ON public.curtailment_automation_rule_state FOR EACH ROW EXECUTE FUNCTION public.update_updated_at_column();

-- Trigger: curtailment_automation_rule update_curtailment_automation_rule_updated_at
CREATE TRIGGER update_curtailment_automation_rule_updated_at BEFORE UPDATE ON public.curtailment_automation_rule FOR EACH ROW EXECUTE FUNCTION public.update_updated_at_column();

-- Trigger: curtailment_event update_curtailment_event_updated_at
CREATE TRIGGER update_curtailment_event_updated_at BEFORE UPDATE ON public.curtailment_event FOR EACH ROW EXECUTE FUNCTION public.update_updated_at_column();

-- Trigger: curtailment_mqtt_source_config update_curtailment_mqtt_source_config_updated_at
CREATE TRIGGER update_curtailment_mqtt_source_config_updated_at BEFORE UPDATE ON public.curtailment_mqtt_source_config FOR EACH ROW EXECUTE FUNCTION public.update_updated_at_column();

-- Trigger: curtailment_mqtt_source_state update_curtailment_mqtt_source_state_updated_at
CREATE TRIGGER update_curtailment_mqtt_source_state_updated_at BEFORE UPDATE ON public.curtailment_mqtt_source_state FOR EACH ROW EXECUTE FUNCTION public.update_updated_at_column();

-- Trigger: curtailment_org_config update_curtailment_org_config_updated_at
CREATE TRIGGER update_curtailment_org_config_updated_at BEFORE UPDATE ON public.curtailment_org_config FOR EACH ROW EXECUTE FUNCTION public.update_updated_at_column();

-- Trigger: curtailment_response_profile update_curtailment_response_profile_updated_at
CREATE TRIGGER update_curtailment_response_profile_updated_at BEFORE UPDATE ON public.curtailment_response_profile FOR EACH ROW EXECUTE FUNCTION public.update_updated_at_column();

-- Trigger: curtailment_rig_config_reconciliation update_curtailment_rig_config_reconciliation_updated_at
CREATE TRIGGER update_curtailment_rig_config_reconciliation_updated_at BEFORE UPDATE ON public.curtailment_rig_config_reconciliation FOR EACH ROW EXECUTE FUNCTION public.update_updated_at_column();

-- Trigger: device_pairing update_device_pairing_updated_at
CREATE TRIGGER update_device_pairing_updated_at BEFORE UPDATE ON public.device_pairing FOR EACH ROW EXECUTE FUNCTION public.update_updated_at_column();

-- Trigger: device_set update_device_set_updated_at
CREATE TRIGGER update_device_set_updated_at BEFORE UPDATE ON public.device_set FOR EACH ROW EXECUTE FUNCTION public.update_updated_at_column();

-- Trigger: device update_device_updated_at
CREATE TRIGGER update_device_updated_at BEFORE UPDATE ON public.device FOR EACH ROW EXECUTE FUNCTION public.update_updated_at_column();

-- Trigger: discovered_device update_discovered_device_last_seen
CREATE TRIGGER update_discovered_device_last_seen BEFORE UPDATE ON public.discovered_device FOR EACH ROW EXECUTE FUNCTION public.update_last_seen_column();

-- Trigger: discovered_device update_discovered_device_updated_at
CREATE TRIGGER update_discovered_device_updated_at BEFORE UPDATE ON public.discovered_device FOR EACH ROW EXECUTE FUNCTION public.update_updated_at_column();

-- Trigger: errors update_errors_updated_at
CREATE TRIGGER update_errors_updated_at BEFORE UPDATE ON public.errors FOR EACH ROW EXECUTE FUNCTION public.update_updated_at_column();

-- Trigger: fleet_node update_fleet_node_updated_at
CREATE TRIGGER update_fleet_node_updated_at BEFORE UPDATE ON public.fleet_node FOR EACH ROW EXECUTE FUNCTION public.update_updated_at_column();

-- Trigger: infrastructure_device update_infrastructure_device_updated_at
CREATE TRIGGER update_infrastructure_device_updated_at BEFORE UPDATE ON public.infrastructure_device FOR EACH ROW EXECUTE FUNCTION public.update_updated_at_column();

-- Trigger: inventory_part update_inventory_part_updated_at
CREATE TRIGGER update_inventory_part_updated_at BEFORE UPDATE ON public.inventory_part FOR EACH ROW EXECUTE FUNCTION public.update_updated_at_column();

-- Trigger: miner_credentials update_miner_credentials_updated_at
CREATE TRIGGER update_miner_credentials_updated_at BEFORE UPDATE ON public.miner_credentials FOR EACH ROW EXECUTE FUNCTION public.update_updated_at_column();

-- Trigger: organization update_organization_updated_at
CREATE TRIGGER update_organization_updated_at BEFORE UPDATE ON public.organization FOR EACH ROW EXECUTE FUNCTION public.update_updated_at_column();

-- Trigger: queue_message update_queue_message_updated_at
CREATE TRIGGER update_queue_message_updated_at BEFORE UPDATE ON public.queue_message FOR EACH ROW EXECUTE FUNCTION public.update_updated_at_column();

-- Trigger: repair_ticket update_repair_ticket_updated_at
CREATE TRIGGER update_repair_ticket_updated_at BEFORE UPDATE ON public.repair_ticket FOR EACH ROW EXECUTE FUNCTION public.update_repair_ticket_version();

-- Trigger: role update_role_updated_at
CREATE TRIGGER update_role_updated_at BEFORE UPDATE ON public.role FOR EACH ROW EXECUTE FUNCTION public.update_updated_at_column();

-- Trigger: schedule update_schedule_updated_at
CREATE TRIGGER update_schedule_updated_at BEFORE UPDATE ON public.schedule FOR EACH ROW EXECUTE FUNCTION public.update_updated_at_column();

-- Trigger: site update_site_updated_at
CREATE TRIGGER update_site_updated_at BEFORE UPDATE ON public.site FOR EACH ROW EXECUTE FUNCTION public.update_updated_at_column();

-- Trigger: user_organization_role update_user_organization_role_updated_at
CREATE TRIGGER update_user_organization_role_updated_at BEFORE UPDATE ON public.user_organization_role FOR EACH ROW EXECUTE FUNCTION public.update_updated_at_column();

-- Trigger: user_organization update_user_organization_updated_at
CREATE TRIGGER update_user_organization_updated_at BEFORE UPDATE ON public.user_organization FOR EACH ROW EXECUTE FUNCTION public.update_updated_at_column();

-- Trigger: user update_user_updated_at
CREATE TRIGGER update_user_updated_at BEFORE UPDATE ON public."user" FOR EACH ROW EXECUTE FUNCTION public.update_updated_at_column();

-- Fk Constraint: activity_log_site activity_log_site_activity_log_id_fkey
ALTER TABLE ONLY public.activity_log_site
    ADD CONSTRAINT activity_log_site_activity_log_id_fkey FOREIGN KEY (activity_log_id) REFERENCES public.activity_log(id) ON DELETE CASCADE;

-- Fk Constraint: alert_route_channel alert_route_channel_channel_id_fkey
ALTER TABLE ONLY public.alert_route_channel
    ADD CONSTRAINT alert_route_channel_channel_id_fkey FOREIGN KEY (channel_id) REFERENCES public.alert_channel(id) ON DELETE CASCADE;

-- Fk Constraint: alert_route_channel alert_route_channel_policy_id_fkey
ALTER TABLE ONLY public.alert_route_channel
    ADD CONSTRAINT alert_route_channel_policy_id_fkey FOREIGN KEY (policy_id) REFERENCES public.alert_route_policy(id) ON DELETE CASCADE;

-- Fk Constraint: curtailment_automation_rule_profile_revision curtailment_automation_rule_profile_rev_automation_rule_id_fkey
ALTER TABLE ONLY public.curtailment_automation_rule_profile_revision
    ADD CONSTRAINT curtailment_automation_rule_profile_rev_automation_rule_id_fkey FOREIGN KEY (automation_rule_id) REFERENCES public.curtailment_automation_rule(id) ON DELETE CASCADE;

-- Fk Constraint: curtailment_response_profile_revision curtailment_response_profile_revision_response_profile_id_fkey
ALTER TABLE ONLY public.curtailment_response_profile_revision
    ADD CONSTRAINT curtailment_response_profile_revision_response_profile_id_fkey FOREIGN KEY (response_profile_id) REFERENCES public.curtailment_response_profile(id) ON DELETE CASCADE;

-- Fk Constraint: curtailment_rig_config_target curtailment_rig_config_target_device_id_fkey
ALTER TABLE ONLY public.curtailment_rig_config_target
    ADD CONSTRAINT curtailment_rig_config_target_device_id_fkey FOREIGN KEY (device_id) REFERENCES public.device(id) ON DELETE CASCADE;

-- Fk Constraint: curtailment_rig_config_target curtailment_rig_config_target_organization_id_fkey
ALTER TABLE ONLY public.curtailment_rig_config_target
    ADD CONSTRAINT curtailment_rig_config_target_organization_id_fkey FOREIGN KEY (organization_id) REFERENCES public.curtailment_rig_config_reconciliation(organization_id) ON DELETE CASCADE;

-- Fk Constraint: device_set_rack device_collection_rack_collection_id_fkey
ALTER TABLE ONLY public.device_set_rack
    ADD CONSTRAINT device_collection_rack_collection_id_fkey FOREIGN KEY (device_set_id) REFERENCES public.device_set(id) ON DELETE CASCADE;

-- Fk Constraint: device_firmware_deployment device_firmware_deployment_device_id_fkey
ALTER TABLE ONLY public.device_firmware_deployment
    ADD CONSTRAINT device_firmware_deployment_device_id_fkey FOREIGN KEY (device_id) REFERENCES public.device(id) ON DELETE CASCADE;

-- Fk Constraint: device_firmware_deployment device_firmware_deployment_rollout_id_fkey
ALTER TABLE ONLY public.device_firmware_deployment
    ADD CONSTRAINT device_firmware_deployment_rollout_id_fkey FOREIGN KEY (rollout_id) REFERENCES public.firmware_rollout(id) ON DELETE SET NULL;

-- Fk Constraint: firmware_rollout firmware_rollout_channel_id_fkey
ALTER TABLE ONLY public.firmware_rollout
    ADD CONSTRAINT firmware_rollout_channel_id_fkey FOREIGN KEY (channel_id) REFERENCES public.release_channel(id) ON DELETE CASCADE;

-- Fk Constraint: firmware_rollout_device firmware_rollout_device_device_id_fkey
ALTER TABLE ONLY public.firmware_rollout_device
    ADD CONSTRAINT firmware_rollout_device_device_id_fkey FOREIGN KEY (device_id) REFERENCES public.device(id) DEFERRABLE INITIALLY DEFERRED;

-- Fk Constraint: firmware_rollout_device firmware_rollout_device_rollout_id_fkey
ALTER TABLE ONLY public.firmware_rollout_device
    ADD CONSTRAINT firmware_rollout_device_rollout_id_fkey FOREIGN KEY (rollout_id) REFERENCES public.firmware_rollout(id) ON DELETE CASCADE;

-- Fk Constraint: firmware_rollout_event firmware_rollout_event_channel_id_fkey
ALTER TABLE ONLY public.firmware_rollout_event
    ADD CONSTRAINT firmware_rollout_event_channel_id_fkey FOREIGN KEY (channel_id) REFERENCES public.release_channel(id) ON DELETE CASCADE;

-- Fk Constraint: firmware_rollout_event firmware_rollout_event_org_id_fkey
ALTER TABLE ONLY public.firmware_rollout_event
    ADD CONSTRAINT firmware_rollout_event_org_id_fkey FOREIGN KEY (org_id) REFERENCES public.organization(id) ON DELETE CASCADE;

-- Fk Constraint: firmware_rollout_event firmware_rollout_event_rollout_id_fkey
ALTER TABLE ONLY public.firmware_rollout_event
    ADD CONSTRAINT firmware_rollout_event_rollout_id_fkey FOREIGN KEY (rollout_id) REFERENCES public.firmware_rollout(id) ON DELETE CASCADE;

-- Fk Constraint: firmware_rollout firmware_rollout_org_id_fkey
ALTER TABLE ONLY public.firmware_rollout
    ADD CONSTRAINT firmware_rollout_org_id_fkey FOREIGN KEY (org_id) REFERENCES public.organization(id) ON DELETE CASCADE;

-- Fk Constraint: firmware_rollout_reservation firmware_rollout_reservation_channel_id_fkey
ALTER TABLE ONLY public.firmware_rollout_reservation
    ADD CONSTRAINT firmware_rollout_reservation_channel_id_fkey FOREIGN KEY (channel_id) REFERENCES public.release_channel(id) ON DELETE CASCADE;

-- Fk Constraint: firmware_rollout_reservation firmware_rollout_reservation_device_id_fkey
ALTER TABLE ONLY public.firmware_rollout_reservation
    ADD CONSTRAINT firmware_rollout_reservation_device_id_fkey FOREIGN KEY (device_id) REFERENCES public.device(id) ON DELETE CASCADE;

-- Fk Constraint: activity_log fk_activity_log_organization
ALTER TABLE ONLY public.activity_log
    ADD CONSTRAINT fk_activity_log_organization FOREIGN KEY (organization_id) REFERENCES public.organization(id) ON DELETE RESTRICT;

-- Fk Constraint: activity_log fk_activity_log_site
ALTER TABLE ONLY public.activity_log
    ADD CONSTRAINT fk_activity_log_site FOREIGN KEY (site_id, organization_id) REFERENCES public.site(id, org_id) ON DELETE SET NULL (site_id);

-- Fk Constraint: activity_log_site fk_activity_log_site_membership_site
ALTER TABLE ONLY public.activity_log_site
    ADD CONSTRAINT fk_activity_log_site_membership_site FOREIGN KEY (site_id, org_id) REFERENCES public.site(id, org_id) ON DELETE CASCADE;

-- Fk Constraint: api_key fk_api_key_fleet_node
ALTER TABLE ONLY public.api_key
    ADD CONSTRAINT fk_api_key_fleet_node FOREIGN KEY (fleet_node_id, organization_id) REFERENCES public.fleet_node(id, org_id) ON DELETE CASCADE;

-- Fk Constraint: api_key fk_api_key_organization
ALTER TABLE ONLY public.api_key
    ADD CONSTRAINT fk_api_key_organization FOREIGN KEY (organization_id) REFERENCES public.organization(id) ON DELETE CASCADE;

-- Fk Constraint: api_key fk_api_key_user
ALTER TABLE ONLY public.api_key
    ADD CONSTRAINT fk_api_key_user FOREIGN KEY (user_id) REFERENCES public."user"(id) ON DELETE CASCADE;

-- Fk Constraint: building fk_building_organization
ALTER TABLE ONLY public.building
    ADD CONSTRAINT fk_building_organization FOREIGN KEY (org_id) REFERENCES public.organization(id) ON DELETE RESTRICT;

-- Fk Constraint: building fk_building_site
ALTER TABLE ONLY public.building
    ADD CONSTRAINT fk_building_site FOREIGN KEY (site_id, org_id) REFERENCES public.site(id, org_id) ON DELETE RESTRICT;

-- Fk Constraint: command_batch_log fk_command_batch_log_created_by
ALTER TABLE ONLY public.command_batch_log
    ADD CONSTRAINT fk_command_batch_log_created_by FOREIGN KEY (created_by) REFERENCES public."user"(id);

-- Fk Constraint: command_batch_log fk_command_batch_log_org
ALTER TABLE ONLY public.command_batch_log
    ADD CONSTRAINT fk_command_batch_log_org FOREIGN KEY (organization_id) REFERENCES public.organization(id) ON DELETE RESTRICT;

-- Fk Constraint: command_on_device_log fk_command_on_device_log_batch
ALTER TABLE ONLY public.command_on_device_log
    ADD CONSTRAINT fk_command_on_device_log_batch FOREIGN KEY (command_batch_log_id) REFERENCES public.command_batch_log(id);

-- Fk Constraint: command_on_device_log fk_command_on_device_log_device
ALTER TABLE ONLY public.command_on_device_log
    ADD CONSTRAINT fk_command_on_device_log_device FOREIGN KEY (device_id) REFERENCES public.device(id);

-- Fk Constraint: command_on_device_log fk_command_on_device_log_organization
ALTER TABLE ONLY public.command_on_device_log
    ADD CONSTRAINT fk_command_on_device_log_organization FOREIGN KEY (org_id) REFERENCES public.organization(id) ON DELETE RESTRICT;

-- Fk Constraint: command_on_device_log fk_command_on_device_log_site
ALTER TABLE ONLY public.command_on_device_log
    ADD CONSTRAINT fk_command_on_device_log_site FOREIGN KEY (site_id, org_id) REFERENCES public.site(id, org_id) ON DELETE SET NULL (site_id);

-- Fk Constraint: curtailment_automation_rule fk_curtailment_automation_rule_mqtt_source
ALTER TABLE ONLY public.curtailment_automation_rule
    ADD CONSTRAINT fk_curtailment_automation_rule_mqtt_source FOREIGN KEY (mqtt_source_id, org_id) REFERENCES public.curtailment_mqtt_source_config(id, organization_id) ON DELETE RESTRICT;

-- Fk Constraint: curtailment_automation_rule fk_curtailment_automation_rule_org
ALTER TABLE ONLY public.curtailment_automation_rule
    ADD CONSTRAINT fk_curtailment_automation_rule_org FOREIGN KEY (org_id) REFERENCES public.organization(id) ON DELETE RESTRICT;

-- Fk Constraint: curtailment_automation_rule fk_curtailment_automation_rule_response_profile
ALTER TABLE ONLY public.curtailment_automation_rule
    ADD CONSTRAINT fk_curtailment_automation_rule_response_profile FOREIGN KEY (response_profile_id, org_id) REFERENCES public.curtailment_response_profile(id, org_id) ON DELETE RESTRICT;

-- Fk Constraint: curtailment_automation_rule_state fk_curtailment_automation_rule_state_event
ALTER TABLE ONLY public.curtailment_automation_rule_state
    ADD CONSTRAINT fk_curtailment_automation_rule_state_event FOREIGN KEY (active_event_uuid) REFERENCES public.curtailment_event(event_uuid) ON DELETE SET NULL;

-- Fk Constraint: curtailment_automation_rule_state fk_curtailment_automation_rule_state_rule
ALTER TABLE ONLY public.curtailment_automation_rule_state
    ADD CONSTRAINT fk_curtailment_automation_rule_state_rule FOREIGN KEY (rule_id) REFERENCES public.curtailment_automation_rule(id) ON DELETE CASCADE;

-- Fk Constraint: curtailment_event fk_curtailment_event_created_by
ALTER TABLE ONLY public.curtailment_event
    ADD CONSTRAINT fk_curtailment_event_created_by FOREIGN KEY (created_by_user_id) REFERENCES public."user"(id);

-- Fk Constraint: curtailment_event fk_curtailment_event_org
ALTER TABLE ONLY public.curtailment_event
    ADD CONSTRAINT fk_curtailment_event_org FOREIGN KEY (org_id) REFERENCES public.organization(id) ON DELETE RESTRICT;

-- Fk Constraint: curtailment_event fk_curtailment_event_supersedes
ALTER TABLE ONLY public.curtailment_event
    ADD CONSTRAINT fk_curtailment_event_supersedes FOREIGN KEY (supersedes_event_id) REFERENCES public.curtailment_event(id) ON DELETE SET NULL;

-- Fk Constraint: curtailment_mqtt_source_config fk_curtailment_mqtt_source_config_org
ALTER TABLE ONLY public.curtailment_mqtt_source_config
    ADD CONSTRAINT fk_curtailment_mqtt_source_config_org FOREIGN KEY (organization_id) REFERENCES public.organization(id) ON DELETE RESTRICT;

-- Fk Constraint: curtailment_mqtt_source_config fk_curtailment_mqtt_source_config_service_user
ALTER TABLE ONLY public.curtailment_mqtt_source_config
    ADD CONSTRAINT fk_curtailment_mqtt_source_config_service_user FOREIGN KEY (service_user_id) REFERENCES public."user"(id) ON DELETE RESTRICT;

-- Fk Constraint: curtailment_mqtt_source_state fk_curtailment_mqtt_source_state_config
ALTER TABLE ONLY public.curtailment_mqtt_source_state
    ADD CONSTRAINT fk_curtailment_mqtt_source_state_config FOREIGN KEY (source_config_id) REFERENCES public.curtailment_mqtt_source_config(id) ON DELETE CASCADE;

-- Fk Constraint: curtailment_org_config fk_curtailment_org_config_org
ALTER TABLE ONLY public.curtailment_org_config
    ADD CONSTRAINT fk_curtailment_org_config_org FOREIGN KEY (org_id) REFERENCES public.organization(id) ON DELETE CASCADE;

-- Fk Constraint: curtailment_response_profile fk_curtailment_response_profile_org
ALTER TABLE ONLY public.curtailment_response_profile
    ADD CONSTRAINT fk_curtailment_response_profile_org FOREIGN KEY (org_id) REFERENCES public.organization(id) ON DELETE RESTRICT;

-- Fk Constraint: curtailment_response_profile fk_curtailment_response_profile_site
ALTER TABLE ONLY public.curtailment_response_profile
    ADD CONSTRAINT fk_curtailment_response_profile_site FOREIGN KEY (site_id, org_id) REFERENCES public.site(id, org_id) ON DELETE RESTRICT;

-- Fk Constraint: curtailment_rig_config_reconciliation fk_curtailment_rig_config_reconciliation_org
ALTER TABLE ONLY public.curtailment_rig_config_reconciliation
    ADD CONSTRAINT fk_curtailment_rig_config_reconciliation_org FOREIGN KEY (organization_id) REFERENCES public.organization(id) ON DELETE CASCADE;

-- Fk Constraint: curtailment_rig_config_reconciliation fk_curtailment_rig_config_reconciliation_user
ALTER TABLE ONLY public.curtailment_rig_config_reconciliation
    ADD CONSTRAINT fk_curtailment_rig_config_reconciliation_user FOREIGN KEY (requested_by) REFERENCES public."user"(id) ON DELETE RESTRICT;

-- Fk Constraint: curtailment_target fk_curtailment_target_event
ALTER TABLE ONLY public.curtailment_target
    ADD CONSTRAINT fk_curtailment_target_event FOREIGN KEY (curtailment_event_id) REFERENCES public.curtailment_event(id) ON DELETE CASCADE;

-- Fk Constraint: device fk_device_building
ALTER TABLE ONLY public.device
    ADD CONSTRAINT fk_device_building FOREIGN KEY (building_id, org_id) REFERENCES public.building(id, org_id) ON DELETE SET NULL (building_id);

-- Fk Constraint: device fk_device_discovered_device
ALTER TABLE ONLY public.device
    ADD CONSTRAINT fk_device_discovered_device FOREIGN KEY (discovered_device_id) REFERENCES public.discovered_device(id);

-- Fk Constraint: device fk_device_organization
ALTER TABLE ONLY public.device
    ADD CONSTRAINT fk_device_organization FOREIGN KEY (org_id) REFERENCES public.organization(id);

-- Fk Constraint: device_pairing fk_device_pairing_device_id
ALTER TABLE ONLY public.device_pairing
    ADD CONSTRAINT fk_device_pairing_device_id FOREIGN KEY (device_id) REFERENCES public.device(id);

-- Fk Constraint: device_set fk_device_set_org
ALTER TABLE ONLY public.device_set
    ADD CONSTRAINT fk_device_set_org FOREIGN KEY (org_id) REFERENCES public.organization(id) ON DELETE RESTRICT;

-- Fk Constraint: device_set_rack fk_device_set_rack_building
ALTER TABLE ONLY public.device_set_rack
    ADD CONSTRAINT fk_device_set_rack_building FOREIGN KEY (building_id, org_id) REFERENCES public.building(id, org_id) ON DELETE SET NULL (building_id);

-- Fk Constraint: device_set_rack fk_device_set_rack_device_set_org
ALTER TABLE ONLY public.device_set_rack
    ADD CONSTRAINT fk_device_set_rack_device_set_org FOREIGN KEY (device_set_id, org_id) REFERENCES public.device_set(id, org_id) ON DELETE CASCADE;

-- Fk Constraint: device_set_rack fk_device_set_rack_site
ALTER TABLE ONLY public.device_set_rack
    ADD CONSTRAINT fk_device_set_rack_site FOREIGN KEY (site_id, org_id) REFERENCES public.site(id, org_id) ON DELETE SET NULL (site_id);

-- Fk Constraint: device fk_device_site
ALTER TABLE ONLY public.device
    ADD CONSTRAINT fk_device_site FOREIGN KEY (site_id, org_id) REFERENCES public.site(id, org_id) ON DELETE SET NULL (site_id);

-- Fk Constraint: device_status fk_device_status_device_id
ALTER TABLE ONLY public.device_status
    ADD CONSTRAINT fk_device_status_device_id FOREIGN KEY (device_id) REFERENCES public.device(id);

-- Fk Constraint: discovered_device fk_discovered_device_fleet_node
ALTER TABLE ONLY public.discovered_device
    ADD CONSTRAINT fk_discovered_device_fleet_node FOREIGN KEY (discovered_by_fleet_node_id) REFERENCES public.fleet_node(id) ON DELETE SET NULL;

-- Fk Constraint: discovered_device fk_discovered_device_org
ALTER TABLE ONLY public.discovered_device
    ADD CONSTRAINT fk_discovered_device_org FOREIGN KEY (org_id) REFERENCES public.organization(id);

-- Fk Constraint: errors fk_errors_device
ALTER TABLE ONLY public.errors
    ADD CONSTRAINT fk_errors_device FOREIGN KEY (device_id) REFERENCES public.device(id) ON DELETE CASCADE;

-- Fk Constraint: errors fk_errors_organization
ALTER TABLE ONLY public.errors
    ADD CONSTRAINT fk_errors_organization FOREIGN KEY (org_id) REFERENCES public.organization(id) ON DELETE RESTRICT;

-- Fk Constraint: errors fk_errors_site
ALTER TABLE ONLY public.errors
    ADD CONSTRAINT fk_errors_site FOREIGN KEY (site_id, org_id) REFERENCES public.site(id, org_id) ON DELETE SET NULL (site_id);

-- Fk Constraint: fleet_node_auth_challenge fk_fleet_node_auth_challenge_fleet_node
ALTER TABLE ONLY public.fleet_node_auth_challenge
    ADD CONSTRAINT fk_fleet_node_auth_challenge_fleet_node FOREIGN KEY (fleet_node_id) REFERENCES public.fleet_node(id) ON DELETE CASCADE;

-- Fk Constraint: fleet_node_device fk_fleet_node_device_assigned_by
ALTER TABLE ONLY public.fleet_node_device
    ADD CONSTRAINT fk_fleet_node_device_assigned_by FOREIGN KEY (assigned_by) REFERENCES public."user"(id) ON DELETE SET NULL;

-- Fk Constraint: fleet_node_device fk_fleet_node_device_device
ALTER TABLE ONLY public.fleet_node_device
    ADD CONSTRAINT fk_fleet_node_device_device FOREIGN KEY (device_id, org_id) REFERENCES public.device(id, org_id) ON DELETE CASCADE;

-- Fk Constraint: fleet_node_device fk_fleet_node_device_fleet_node
ALTER TABLE ONLY public.fleet_node_device
    ADD CONSTRAINT fk_fleet_node_device_fleet_node FOREIGN KEY (fleet_node_id, org_id) REFERENCES public.fleet_node(id, org_id) ON DELETE CASCADE;

-- Fk Constraint: fleet_node fk_fleet_node_org
ALTER TABLE ONLY public.fleet_node
    ADD CONSTRAINT fk_fleet_node_org FOREIGN KEY (org_id) REFERENCES public.organization(id) ON DELETE RESTRICT;

-- Fk Constraint: fleet_node_session fk_fleet_node_session_fleet_node
ALTER TABLE ONLY public.fleet_node_session
    ADD CONSTRAINT fk_fleet_node_session_fleet_node FOREIGN KEY (fleet_node_id) REFERENCES public.fleet_node(id) ON DELETE CASCADE;

-- Fk Constraint: infrastructure_device fk_infrastructure_device_organization
ALTER TABLE ONLY public.infrastructure_device
    ADD CONSTRAINT fk_infrastructure_device_organization FOREIGN KEY (org_id) REFERENCES public.organization(id) ON DELETE RESTRICT;

-- Fk Constraint: infrastructure_device fk_infrastructure_device_site
ALTER TABLE ONLY public.infrastructure_device
    ADD CONSTRAINT fk_infrastructure_device_site FOREIGN KEY (site_id, org_id) REFERENCES public.site(id, org_id) ON DELETE RESTRICT;

-- Fk Constraint: inventory_part fk_inventory_part_org
ALTER TABLE ONLY public.inventory_part
    ADD CONSTRAINT fk_inventory_part_org FOREIGN KEY (org_id) REFERENCES public.organization(id) ON DELETE RESTRICT;

-- Fk Constraint: inventory_part fk_inventory_part_site
ALTER TABLE ONLY public.inventory_part
    ADD CONSTRAINT fk_inventory_part_site FOREIGN KEY (site_id, org_id) REFERENCES public.site(id, org_id) ON DELETE RESTRICT;

-- Fk Constraint: device_set_membership fk_membership_device
ALTER TABLE ONLY public.device_set_membership
    ADD CONSTRAINT fk_membership_device FOREIGN KEY (device_id) REFERENCES public.device(id) ON DELETE CASCADE;

-- Fk Constraint: device_set_membership fk_membership_device_set
ALTER TABLE ONLY public.device_set_membership
    ADD CONSTRAINT fk_membership_device_set FOREIGN KEY (device_set_id) REFERENCES public.device_set(id) ON DELETE CASCADE;

-- Fk Constraint: device_set_membership fk_membership_org
ALTER TABLE ONLY public.device_set_membership
    ADD CONSTRAINT fk_membership_org FOREIGN KEY (org_id) REFERENCES public.organization(id) ON DELETE RESTRICT;

-- Fk Constraint: miner_credentials fk_miner_credentials_device_id
ALTER TABLE ONLY public.miner_credentials
    ADD CONSTRAINT fk_miner_credentials_device_id FOREIGN KEY (device_id) REFERENCES public.device(id) ON DELETE CASCADE;

-- Fk Constraint: pending_enrollment fk_pending_enrollment_fleet_node
ALTER TABLE ONLY public.pending_enrollment
    ADD CONSTRAINT fk_pending_enrollment_fleet_node FOREIGN KEY (fleet_node_id, org_id) REFERENCES public.fleet_node(id, org_id) ON DELETE CASCADE;

-- Fk Constraint: pending_enrollment fk_pending_enrollment_org
ALTER TABLE ONLY public.pending_enrollment
    ADD CONSTRAINT fk_pending_enrollment_org FOREIGN KEY (org_id) REFERENCES public.organization(id) ON DELETE CASCADE;

-- Fk Constraint: pending_enrollment fk_pending_enrollment_user
ALTER TABLE ONLY public.pending_enrollment
    ADD CONSTRAINT fk_pending_enrollment_user FOREIGN KEY (created_by) REFERENCES public."user"(id) ON DELETE CASCADE;

-- Fk Constraint: pool fk_pool_organization
ALTER TABLE ONLY public.pool
    ADD CONSTRAINT fk_pool_organization FOREIGN KEY (org_id) REFERENCES public.organization(id) ON DELETE RESTRICT;

-- Fk Constraint: queue_message fk_queue_message_device
ALTER TABLE ONLY public.queue_message
    ADD CONSTRAINT fk_queue_message_device FOREIGN KEY (device_id) REFERENCES public.device(id);

-- Fk Constraint: rack_slot fk_rack_slot_membership
ALTER TABLE ONLY public.rack_slot
    ADD CONSTRAINT fk_rack_slot_membership FOREIGN KEY (device_set_id, device_id) REFERENCES public.device_set_membership(device_set_id, device_id) ON DELETE CASCADE;

-- Fk Constraint: release_channel_setting fk_release_channel_setting_org
ALTER TABLE ONLY public.release_channel_setting
    ADD CONSTRAINT fk_release_channel_setting_org FOREIGN KEY (organization_id) REFERENCES public.organization(id);

-- Fk Constraint: repair_ticket fk_repair_ticket_assignee
ALTER TABLE ONLY public.repair_ticket
    ADD CONSTRAINT fk_repair_ticket_assignee FOREIGN KEY (assignee_user_id) REFERENCES public."user"(id) ON DELETE RESTRICT;

-- Fk Constraint: repair_ticket fk_repair_ticket_building
ALTER TABLE ONLY public.repair_ticket
    ADD CONSTRAINT fk_repair_ticket_building FOREIGN KEY (building_id, org_id) REFERENCES public.building(id, org_id) ON DELETE RESTRICT;

-- Fk Constraint: repair_ticket_counter fk_repair_ticket_counter_org
ALTER TABLE ONLY public.repair_ticket_counter
    ADD CONSTRAINT fk_repair_ticket_counter_org FOREIGN KEY (org_id) REFERENCES public.organization(id) ON DELETE RESTRICT;

-- Fk Constraint: repair_ticket fk_repair_ticket_org
ALTER TABLE ONLY public.repair_ticket
    ADD CONSTRAINT fk_repair_ticket_org FOREIGN KEY (org_id) REFERENCES public.organization(id) ON DELETE RESTRICT;

-- Fk Constraint: repair_ticket fk_repair_ticket_site
ALTER TABLE ONLY public.repair_ticket
    ADD CONSTRAINT fk_repair_ticket_site FOREIGN KEY (site_id, org_id) REFERENCES public.site(id, org_id) ON DELETE RESTRICT;

-- Fk Constraint: role fk_role_organization
ALTER TABLE ONLY public.role
    ADD CONSTRAINT fk_role_organization FOREIGN KEY (organization_id) REFERENCES public.organization(id) ON DELETE CASCADE;

-- Fk Constraint: schedule fk_schedule_org
ALTER TABLE ONLY public.schedule
    ADD CONSTRAINT fk_schedule_org FOREIGN KEY (org_id) REFERENCES public.organization(id) ON DELETE RESTRICT;

-- Fk Constraint: session fk_session_organization
ALTER TABLE ONLY public.session
    ADD CONSTRAINT fk_session_organization FOREIGN KEY (organization_id) REFERENCES public.organization(id) ON DELETE CASCADE;

-- Fk Constraint: session fk_session_user
ALTER TABLE ONLY public.session
    ADD CONSTRAINT fk_session_user FOREIGN KEY (user_id) REFERENCES public."user"(id) ON DELETE CASCADE;

-- Fk Constraint: site fk_site_organization
ALTER TABLE ONLY public.site
    ADD CONSTRAINT fk_site_organization FOREIGN KEY (org_id) REFERENCES public.organization(id) ON DELETE RESTRICT;

-- Fk Constraint: repair_ticket_comment fk_ticket_comment_org
ALTER TABLE ONLY public.repair_ticket_comment
    ADD CONSTRAINT fk_ticket_comment_org FOREIGN KEY (org_id) REFERENCES public.organization(id) ON DELETE RESTRICT;

-- Fk Constraint: repair_ticket_comment fk_ticket_comment_ticket
ALTER TABLE ONLY public.repair_ticket_comment
    ADD CONSTRAINT fk_ticket_comment_ticket FOREIGN KEY (ticket_id, org_id) REFERENCES public.repair_ticket(id, org_id) ON DELETE CASCADE;

-- Fk Constraint: repair_ticket_comment fk_ticket_comment_user
ALTER TABLE ONLY public.repair_ticket_comment
    ADD CONSTRAINT fk_ticket_comment_user FOREIGN KEY (user_id) REFERENCES public."user"(id) ON DELETE RESTRICT;

-- Fk Constraint: repair_ticket_part fk_ticket_part_inventory
ALTER TABLE ONLY public.repair_ticket_part
    ADD CONSTRAINT fk_ticket_part_inventory FOREIGN KEY (inventory_part_id, org_id) REFERENCES public.inventory_part(id, org_id) ON DELETE RESTRICT;

-- Fk Constraint: repair_ticket_part fk_ticket_part_ticket
ALTER TABLE ONLY public.repair_ticket_part
    ADD CONSTRAINT fk_ticket_part_ticket FOREIGN KEY (ticket_id, org_id) REFERENCES public.repair_ticket(id, org_id) ON DELETE CASCADE;

-- Fk Constraint: user_organization_role fk_user_org_role_role
ALTER TABLE ONLY public.user_organization_role
    ADD CONSTRAINT fk_user_org_role_role FOREIGN KEY (role_id, organization_id) REFERENCES public.role(id, organization_id) ON DELETE RESTRICT;

-- Fk Constraint: user_organization_role fk_user_org_role_site
ALTER TABLE ONLY public.user_organization_role
    ADD CONSTRAINT fk_user_org_role_site FOREIGN KEY (scope_id, organization_id) REFERENCES public.site(id, org_id) ON DELETE CASCADE DEFERRABLE INITIALLY DEFERRED;

-- Fk Constraint: user_organization fk_user_organization_organization
ALTER TABLE ONLY public.user_organization
    ADD CONSTRAINT fk_user_organization_organization FOREIGN KEY (organization_id) REFERENCES public.organization(id) ON DELETE RESTRICT;

-- Fk Constraint: user_organization fk_user_organization_role
ALTER TABLE ONLY public.user_organization
    ADD CONSTRAINT fk_user_organization_role FOREIGN KEY (role_id) REFERENCES public.role(id) ON DELETE RESTRICT;

-- Fk Constraint: user_organization fk_user_organization_user
ALTER TABLE ONLY public.user_organization
    ADD CONSTRAINT fk_user_organization_user FOREIGN KEY (user_id) REFERENCES public."user"(id) ON DELETE RESTRICT;

-- Fk Constraint: release_channel_firmware release_channel_firmware_channel_id_fkey
ALTER TABLE ONLY public.release_channel_firmware
    ADD CONSTRAINT release_channel_firmware_channel_id_fkey FOREIGN KEY (channel_id) REFERENCES public.release_channel(id) ON DELETE CASCADE;

-- Fk Constraint: release_channel release_channel_org_id_fkey
ALTER TABLE ONLY public.release_channel
    ADD CONSTRAINT release_channel_org_id_fkey FOREIGN KEY (org_id) REFERENCES public.organization(id) ON DELETE CASCADE;

-- Fk Constraint: release_channel_target release_channel_target_channel_id_fkey
ALTER TABLE ONLY public.release_channel_target
    ADD CONSTRAINT release_channel_target_channel_id_fkey FOREIGN KEY (channel_id) REFERENCES public.release_channel(id) ON DELETE CASCADE;

-- Fk Constraint: role_permission role_permission_permission_id_fkey
ALTER TABLE ONLY public.role_permission
    ADD CONSTRAINT role_permission_permission_id_fkey FOREIGN KEY (permission_id) REFERENCES public.permission(id) ON DELETE RESTRICT;

-- Fk Constraint: role_permission role_permission_role_id_fkey
ALTER TABLE ONLY public.role_permission
    ADD CONSTRAINT role_permission_role_id_fkey FOREIGN KEY (role_id) REFERENCES public.role(id) ON DELETE CASCADE;

-- Fk Constraint: schedule_target schedule_target_schedule_id_fkey
ALTER TABLE ONLY public.schedule_target
    ADD CONSTRAINT schedule_target_schedule_id_fkey FOREIGN KEY (schedule_id) REFERENCES public.schedule(id) ON DELETE CASCADE;

-- Fk Constraint: user_organization_role user_organization_role_organization_id_fkey
ALTER TABLE ONLY public.user_organization_role
    ADD CONSTRAINT user_organization_role_organization_id_fkey FOREIGN KEY (organization_id) REFERENCES public.organization(id) ON DELETE CASCADE;

-- Fk Constraint: user_organization_role user_organization_role_user_id_fkey
ALTER TABLE ONLY public.user_organization_role
    ADD CONSTRAINT user_organization_role_user_id_fkey FOREIGN KEY (user_id) REFERENCES public."user"(id) ON DELETE CASCADE;

-- Acl: FUNCTION fleet_slow_statements()
REVOKE ALL ON FUNCTION public.fleet_slow_statements() FROM PUBLIC;

-- Hypertables and final storage/retention policies.
SELECT create_hypertable('device_metrics', by_range('time', INTERVAL '1 hour'), create_default_indexes => false);
SELECT create_hypertable('miner_state_snapshots', by_range('time', INTERVAL '6 hours'), create_default_indexes => false);
SELECT create_hypertable('notification_metric_sample', by_range('time', INTERVAL '1 hour'), create_default_indexes => false);
SELECT create_hypertable('fleet_metric_rollup_90s', by_range('bucket', INTERVAL '1 day'), create_default_indexes => false);

ALTER TABLE device_metrics SET (
    timescaledb.compress,
    timescaledb.compress_segmentby = 'device_identifier',
    timescaledb.compress_orderby = 'time DESC'
);
ALTER TABLE miner_state_snapshots SET (
    timescaledb.compress,
    timescaledb.compress_segmentby = 'device_identifier',
    timescaledb.compress_orderby = 'time DESC'
);
ALTER TABLE notification_metric_sample SET (
    timescaledb.compress,
    timescaledb.compress_segmentby = 'metric, organization_id',
    timescaledb.compress_orderby = 'time DESC'
);
SELECT add_compression_policy('device_metrics', INTERVAL '6 hours', schedule_interval => INTERVAL '1 hour');
SELECT add_retention_policy('device_metrics', INTERVAL '10 days', schedule_interval => INTERVAL '1 hour');
SELECT add_compression_policy('miner_state_snapshots', INTERVAL '6 hours', schedule_interval => INTERVAL '1 hour');
SELECT add_retention_policy('miner_state_snapshots', INTERVAL '1 year');
SELECT add_compression_policy('notification_metric_sample', INTERVAL '4 hours');
SELECT add_retention_policy('notification_metric_sample', INTERVAL '7 days');
SELECT add_retention_policy('fleet_metric_rollup_90s', INTERVAL '10 days');

-- Surviving continuous aggregates and their final refresh policies.


CREATE MATERIALIZED VIEW device_metrics_hourly
WITH (timescaledb.continuous) AS
SELECT
    time_bucket('1 hour', time) AS bucket,
    device_identifier,
    AVG(hash_rate_hs) AS avg_hash_rate,
    MAX(hash_rate_hs) AS max_hash_rate,
    MIN(hash_rate_hs) AS min_hash_rate,
    AVG(temp_c) AS avg_temp,
    MAX(temp_c) AS max_temp,
    MIN(temp_c) AS min_temp,
    AVG(fan_rpm) AS avg_fan_rpm,
    AVG(power_w) AS avg_power,
    SUM(power_w) AS total_power,
    AVG(efficiency_jh) AS avg_efficiency,
    COUNT(*) AS data_points
FROM device_metrics
GROUP BY bucket, device_identifier
WITH NO DATA;

SELECT add_continuous_aggregate_policy('device_metrics_hourly',
    start_offset => INTERVAL '1 day',
    end_offset => INTERVAL '1 hour',
    schedule_interval => INTERVAL '30 minutes');

CREATE INDEX idx_device_metrics_hourly_device
    ON device_metrics_hourly(device_identifier, bucket DESC);

CREATE MATERIALIZED VIEW device_metrics_daily
WITH (timescaledb.continuous) AS
SELECT
    time_bucket('1 day', time) AS bucket,
    device_identifier,
    AVG(hash_rate_hs) AS avg_hash_rate,
    MAX(hash_rate_hs) AS max_hash_rate,
    MIN(hash_rate_hs) AS min_hash_rate,
    AVG(temp_c) AS avg_temp,
    MAX(temp_c) AS max_temp,
    MIN(temp_c) AS min_temp,
    AVG(power_w) AS avg_power,
    AVG(efficiency_jh) AS avg_efficiency,
    COUNT(*) AS data_points
FROM device_metrics
GROUP BY bucket, device_identifier
WITH NO DATA;

SELECT add_continuous_aggregate_policy('device_metrics_daily',
    start_offset => INTERVAL '7 days',
    end_offset => INTERVAL '1 day',
    schedule_interval => INTERVAL '6 hours');

CREATE INDEX idx_device_metrics_daily_device
    ON device_metrics_daily(device_identifier, bucket DESC);

SELECT add_retention_policy('device_metrics_hourly', INTERVAL '3 months');
SELECT add_retention_policy('device_metrics_daily', INTERVAL '3 years');

CREATE MATERIALIZED VIEW device_status_hourly
WITH (timescaledb.continuous) AS
SELECT
    time_bucket('1 hour', time) AS bucket,
    device_identifier,
    -- Temperature histogram (10°C buckets for flexibility)
    -- All buckets explicitly check for NOT NULL to ensure consistent handling
    SUM(CASE WHEN temp_c IS NOT NULL AND temp_c < 0 THEN 1 ELSE 0 END)::int AS temp_below_0,
    SUM(CASE WHEN temp_c IS NOT NULL AND temp_c >= 0 AND temp_c < 10 THEN 1 ELSE 0 END)::int AS temp_0_10,
    SUM(CASE WHEN temp_c IS NOT NULL AND temp_c >= 10 AND temp_c < 20 THEN 1 ELSE 0 END)::int AS temp_10_20,
    SUM(CASE WHEN temp_c IS NOT NULL AND temp_c >= 20 AND temp_c < 30 THEN 1 ELSE 0 END)::int AS temp_20_30,
    SUM(CASE WHEN temp_c IS NOT NULL AND temp_c >= 30 AND temp_c < 40 THEN 1 ELSE 0 END)::int AS temp_30_40,
    SUM(CASE WHEN temp_c IS NOT NULL AND temp_c >= 40 AND temp_c < 50 THEN 1 ELSE 0 END)::int AS temp_40_50,
    SUM(CASE WHEN temp_c IS NOT NULL AND temp_c >= 50 AND temp_c < 60 THEN 1 ELSE 0 END)::int AS temp_50_60,
    SUM(CASE WHEN temp_c IS NOT NULL AND temp_c >= 60 AND temp_c < 70 THEN 1 ELSE 0 END)::int AS temp_60_70,
    SUM(CASE WHEN temp_c IS NOT NULL AND temp_c >= 70 AND temp_c < 80 THEN 1 ELSE 0 END)::int AS temp_70_80,
    SUM(CASE WHEN temp_c IS NOT NULL AND temp_c >= 80 AND temp_c < 90 THEN 1 ELSE 0 END)::int AS temp_80_90,
    SUM(CASE WHEN temp_c IS NOT NULL AND temp_c >= 90 AND temp_c < 100 THEN 1 ELSE 0 END)::int AS temp_90_100,
    SUM(CASE WHEN temp_c IS NOT NULL AND temp_c >= 100 THEN 1 ELSE 0 END)::int AS temp_100_plus,
    -- Uptime status counts
    SUM(CASE WHEN health = 'health_healthy_active' THEN 1 ELSE 0 END)::int AS hashing_count,
    SUM(CASE WHEN health IS NULL OR health != 'health_healthy_active' THEN 1 ELSE 0 END)::int AS not_hashing_count,
    COUNT(*)::int AS data_points
FROM device_metrics
GROUP BY bucket, device_identifier
WITH NO DATA;

SELECT add_continuous_aggregate_policy('device_status_hourly',
    start_offset => INTERVAL '1 day',
    end_offset => INTERVAL '1 hour',
    schedule_interval => INTERVAL '30 minutes');

CREATE INDEX idx_device_status_hourly_device ON device_status_hourly(device_identifier, bucket DESC);

CREATE MATERIALIZED VIEW device_status_daily
WITH (timescaledb.continuous) AS
SELECT
    time_bucket('1 day', time) AS bucket,
    device_identifier,
    -- Temperature histogram (same structure as hourly)
    -- All buckets explicitly check for NOT NULL to ensure consistent handling
    SUM(CASE WHEN temp_c IS NOT NULL AND temp_c < 0 THEN 1 ELSE 0 END)::int AS temp_below_0,
    SUM(CASE WHEN temp_c IS NOT NULL AND temp_c >= 0 AND temp_c < 10 THEN 1 ELSE 0 END)::int AS temp_0_10,
    SUM(CASE WHEN temp_c IS NOT NULL AND temp_c >= 10 AND temp_c < 20 THEN 1 ELSE 0 END)::int AS temp_10_20,
    SUM(CASE WHEN temp_c IS NOT NULL AND temp_c >= 20 AND temp_c < 30 THEN 1 ELSE 0 END)::int AS temp_20_30,
    SUM(CASE WHEN temp_c IS NOT NULL AND temp_c >= 30 AND temp_c < 40 THEN 1 ELSE 0 END)::int AS temp_30_40,
    SUM(CASE WHEN temp_c IS NOT NULL AND temp_c >= 40 AND temp_c < 50 THEN 1 ELSE 0 END)::int AS temp_40_50,
    SUM(CASE WHEN temp_c IS NOT NULL AND temp_c >= 50 AND temp_c < 60 THEN 1 ELSE 0 END)::int AS temp_50_60,
    SUM(CASE WHEN temp_c IS NOT NULL AND temp_c >= 60 AND temp_c < 70 THEN 1 ELSE 0 END)::int AS temp_60_70,
    SUM(CASE WHEN temp_c IS NOT NULL AND temp_c >= 70 AND temp_c < 80 THEN 1 ELSE 0 END)::int AS temp_70_80,
    SUM(CASE WHEN temp_c IS NOT NULL AND temp_c >= 80 AND temp_c < 90 THEN 1 ELSE 0 END)::int AS temp_80_90,
    SUM(CASE WHEN temp_c IS NOT NULL AND temp_c >= 90 AND temp_c < 100 THEN 1 ELSE 0 END)::int AS temp_90_100,
    SUM(CASE WHEN temp_c IS NOT NULL AND temp_c >= 100 THEN 1 ELSE 0 END)::int AS temp_100_plus,
    -- Uptime status counts
    SUM(CASE WHEN health = 'health_healthy_active' THEN 1 ELSE 0 END)::int AS hashing_count,
    SUM(CASE WHEN health IS NULL OR health != 'health_healthy_active' THEN 1 ELSE 0 END)::int AS not_hashing_count,
    COUNT(*)::int AS data_points
FROM device_metrics
GROUP BY bucket, device_identifier
WITH NO DATA;

SELECT add_continuous_aggregate_policy('device_status_daily',
    start_offset => INTERVAL '7 days',
    end_offset => INTERVAL '1 day',
    schedule_interval => INTERVAL '6 hours');

CREATE INDEX idx_device_status_daily_device ON device_status_daily(device_identifier, bucket DESC);

CREATE MATERIALIZED VIEW fleet_telemetry_poll_heartbeat
WITH (timescaledb.continuous, timescaledb.materialized_only = true) AS
SELECT
    time_bucket(INTERVAL '1 minute', time) AS bucket,
    organization_id,
    count(*)::bigint AS sample_count
FROM notification_metric_sample
WHERE metric = 'fleet_telemetry_poll_total'
  AND organization_id <> ''
GROUP BY bucket, organization_id
WITH NO DATA;

SELECT add_continuous_aggregate_policy('fleet_telemetry_poll_heartbeat',
    start_offset => INTERVAL '30 minutes',
    end_offset => INTERVAL '1 minute',
    schedule_interval => INTERVAL '30 seconds');

SELECT add_retention_policy('fleet_telemetry_poll_heartbeat', INTERVAL '7 days');

CREATE INDEX idx_fleet_telemetry_poll_heartbeat_org_bucket
    ON fleet_telemetry_poll_heartbeat (organization_id, bucket DESC);

CREATE MATERIALIZED VIEW miner_state_snapshot_device_1m
WITH (timescaledb.continuous, timescaledb.materialized_only = true) AS
SELECT
    time_bucket(INTERVAL '1 minute', time) AS bucket,
    org_id,
    device_identifier,
    last(time, time)::timestamptz AS state_time,
    last(state, time)::smallint AS state
FROM miner_state_snapshots
GROUP BY bucket, org_id, device_identifier
WITH NO DATA;

CREATE INDEX idx_miner_state_snapshot_device_1m_org_bucket
    ON miner_state_snapshot_device_1m(org_id, bucket DESC);
CREATE INDEX idx_miner_state_snapshot_device_1m_org_device_bucket
    ON miner_state_snapshot_device_1m(org_id, device_identifier, bucket DESC);

SELECT add_continuous_aggregate_policy('miner_state_snapshot_device_1m',
    start_offset => INTERVAL '14 days',
    end_offset => INTERVAL '2 minutes',
    schedule_interval => INTERVAL '5 minutes',
    buckets_per_batch => 1440);

SELECT add_retention_policy('miner_state_snapshot_device_1m', INTERVAL '14 days',
    schedule_interval => INTERVAL '1 day');

CREATE MATERIALIZED VIEW miner_state_snapshot_device_hourly
WITH (timescaledb.continuous, timescaledb.materialized_only = true) AS
SELECT
    time_bucket(INTERVAL '1 hour', time) AS bucket,
    org_id,
    device_identifier,
    last(state, time)::smallint AS state
FROM miner_state_snapshots
GROUP BY bucket, org_id, device_identifier
WITH NO DATA;

CREATE INDEX idx_miner_state_snapshot_device_hourly_org_bucket
    ON miner_state_snapshot_device_hourly(org_id, bucket DESC);
CREATE INDEX idx_miner_state_snapshot_device_hourly_org_device_bucket
    ON miner_state_snapshot_device_hourly(org_id, device_identifier, bucket DESC);

SELECT add_continuous_aggregate_policy('miner_state_snapshot_device_hourly',
    start_offset => INTERVAL '3 months',
    end_offset => INTERVAL '1 hour',
    schedule_interval => INTERVAL '30 minutes',
    buckets_per_batch => 168);

SELECT add_retention_policy('miner_state_snapshot_device_hourly', INTERVAL '3 months',
    schedule_interval => INTERVAL '1 day');

CREATE MATERIALIZED VIEW miner_state_snapshot_device_daily
WITH (timescaledb.continuous, timescaledb.materialized_only = true) AS
SELECT
    time_bucket(INTERVAL '1 day', time) AS bucket,
    org_id,
    device_identifier,
    last(state, time)::smallint AS state
FROM miner_state_snapshots
GROUP BY bucket, org_id, device_identifier
WITH NO DATA;

CREATE INDEX idx_miner_state_snapshot_device_daily_org_bucket
    ON miner_state_snapshot_device_daily(org_id, bucket DESC);
CREATE INDEX idx_miner_state_snapshot_device_daily_org_device_bucket
    ON miner_state_snapshot_device_daily(org_id, device_identifier, bucket DESC);

SELECT add_continuous_aggregate_policy('miner_state_snapshot_device_daily',
    start_offset => INTERVAL '12 months',
    end_offset => INTERVAL '1 day',
    schedule_interval => INTERVAL '6 hours',
    buckets_per_batch => 30);

ALTER MATERIALIZED VIEW miner_state_snapshot_device_daily SET (
    timescaledb.compress,
    timescaledb.compress_segmentby = 'org_id, device_identifier',
    timescaledb.compress_orderby = 'bucket DESC'
);

SELECT add_compression_policy('miner_state_snapshot_device_daily', INTERVAL '13 months',
    schedule_interval => INTERVAL '1 day');
SELECT add_retention_policy('miner_state_snapshot_device_daily', INTERVAL '3 years',
    schedule_interval => INTERVAL '1 week');

-- Materialization intervals survive later source-hypertable interval changes.
SELECT set_chunk_time_interval('device_metrics_daily', INTERVAL '10 days');
SELECT set_chunk_time_interval('device_metrics_hourly', INTERVAL '10 days');
SELECT set_chunk_time_interval('device_status_daily', INTERVAL '10 days');
SELECT set_chunk_time_interval('device_status_hourly', INTERVAL '10 days');
SELECT set_chunk_time_interval('fleet_telemetry_poll_heartbeat', INTERVAL '10 days');
SELECT set_chunk_time_interval('miner_state_snapshot_device_1m', INTERVAL '10 days');
SELECT set_chunk_time_interval('miner_state_snapshot_device_daily', INTERVAL '10 days');
SELECT set_chunk_time_interval('miner_state_snapshot_device_hourly', INTERVAL '10 days');

-- Installation-owned Grafana role is optional; preserve the existing grants.
-- HA creates grafana_ha_ro during Patroni bootstrap, before Fleet migrations
-- create these objects. Grant only the two relations used by the provisioned
-- HA readiness rule; standalone installs continue provisioning their broader
-- optional Grafana grants through run-fleet.sh.
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'grafana_ha_ro') THEN
        GRANT SELECT ON notification_metric_sample TO grafana_ha_ro;
        GRANT SELECT ON fleet_active_organization TO grafana_ha_ro;
    END IF;
END
$$;

-- HA creates grafana_ha_ro during Patroni bootstrap, before Fleet migrations
-- run. Grant only the Fleet Node columns required by the availability rule.
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'grafana_ha_ro') THEN
        GRANT SELECT (
            org_id,
            id,
            last_seen_at,
            enrollment_status,
            deleted_at
        ) ON fleet_node TO grafana_ha_ro;
    END IF;
END
$$;


-- Deterministic permission catalog. IDs and sequence advancement match legacy replay.
INSERT INTO permission (id, key, description) VALUES
    (1, 'fleet:read', 'View dashboard, miner list, and telemetry. Required floor for any role with miner actions.'),
    (2, 'miner:read', 'View miner detail, status snapshot, and error history. Required floor for any miner action permission.'),
    (3, 'miner:blink_led', 'Trigger the locator LED on a miner.'),
    (4, 'miner:reboot', 'Reboot a miner.'),
    (5, 'miner:start_mining', 'Start mining on a miner.'),
    (6, 'miner:stop_mining', 'Stop mining on a miner.'),
    (7, 'miner:update_pools', 'Update a miner''s pool configuration.'),
    (8, 'miner:update_worker_names', 'Update worker names on a miner.'),
    (9, 'miner:rename', 'Rename a miner.'),
    (10, 'miner:delete', 'Delete a miner.'),
    (11, 'miner:set_cooling_mode', 'Change a miner''s cooling mode.'),
    (12, 'miner:set_power_target', 'Change a miner''s power target.'),
    (13, 'miner:firmware_update', 'Push a firmware update to a miner.'),
    (14, 'miner:download_logs', 'Download diagnostic logs from a miner.'),
    (15, 'miner:update_password', 'Change the miner''s device-local web UI password.'),
    (16, 'miner:unpair', 'Unpair a miner from the fleet.'),
    (17, 'miner:pair', 'Pair a new miner into the fleet.'),
    (18, 'miner:export_csv', 'Export miner data as CSV.'),
    (19, 'rack:read', 'List racks at a site.'),
    (20, 'rack:manage', 'Create, rename, delete racks and move miners between them.'),
    (21, 'site:read', 'View sites and buildings.'),
    (22, 'site:manage', 'Create, edit, and delete sites and buildings.'),
    (23, 'serverlog:read', 'View server-side logs.'),
    (24, 'curtailment:read', 'View curtailment policies and preview impact.'),
    (25, 'curtailment:manage', 'Create, edit, and delete curtailment policies.'),
    (26, 'fleetnode:read', 'View fleet-node state.'),
    (27, 'fleetnode:manage', 'Perform fleet-node admin operations.'),
    (28, 'apikey:manage', 'List, create, and revoke API keys for the organization.'),
    (29, 'user:read', 'List users in the organization.'),
    (30, 'user:manage', 'Create, reset, and deactivate users in the organization.'),
    (31, 'role:manage', 'Create, edit, and delete custom roles and edit the ADMIN/FIELD_TECH built-ins.'),
    (32, 'curtailment:ingest', 'Accept curtailment dispatch signals from external providers (QSE bridge, aggregator, OpenADR VTN).'),
    (33, 'pool:read', 'View saved mining pool configurations.'),
    (34, 'pool:manage', 'Create, edit, and delete saved mining pool configurations.'),
    (35, 'schedule:read', 'View scheduled miner actions.'),
    (36, 'schedule:manage', 'Create, edit, pause, resume, and delete scheduled miner actions. Requires the underlying miner action permission to schedule that action.'),
    (37, 'activity:read', 'View the organization-wide activity log and export it as CSV.'),
    (40, 'alert:read', 'View alert channels, alert rules, silences, and delivery history.'),
    (41, 'alert:manage', 'Create, edit, test, and delete alert channels; pause and resume alert rules; create and lift silences.'),
    (42, 'instance:update', 'See available server updates, change the release channel, and apply server upgrades.'),
    (43, 'maintenance:read', 'View repair tickets, maintenance history, and parts inventory.'),
    (44, 'maintenance:manage', 'Create, assign, update, and close repair tickets; manage parts inventory.');
SELECT setval('permission_id_seq', 44, true);

-- Preserve the legacy fresh-install tombstone without seeding a live role.
-- Per-organization built-ins are created by the existing authorization service.
INSERT INTO role (id, name, description, deleted_at)
VALUES (1, 'ADMIN', 'Admin role with full permissions except managing SUPER_ADMIN', CURRENT_TIMESTAMP);
SELECT setval('role_id_seq', 1, true);

INSERT INTO curtailment_reconciler_heartbeat (id, last_tick_at, last_tick_uuid)
VALUES (1, CURRENT_TIMESTAMP, '00000000-0000-0000-0000-000000000000');
