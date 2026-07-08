-- Single source of truth for the search document used by ListActivityLogs and
-- CountActivityLogs: maps an activity_log row's event_type (plus scope/metadata
-- context) to the human-readable label the client renders, so searching the
-- visible text matches rows. Keep in sync with the client-side label maps in
-- client/src/protoFleet/features/activity/utils/ (formatLabel.ts,
-- formatActivityDescription.ts).

-- Mirrors the client's countLabel(): "1 rack" / "4 racks".
CREATE FUNCTION activity_count_label(
    item_count BIGINT,
    singular TEXT,
    plural TEXT
) RETURNS TEXT
LANGUAGE SQL
IMMUTABLE
PARALLEL SAFE
AS $$
SELECT item_count || ' ' || CASE WHEN item_count = 1 THEN singular ELSE plural END
$$;

CREATE FUNCTION activity_display_label(
    event_type TEXT,
    scope_type TEXT,
    scope_label TEXT,
    metadata JSONB,
    description TEXT
) RETURNS TEXT
LANGUAGE SQL
IMMUTABLE
PARALLEL SAFE
AS $$
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
        WHEN event_type = 'site.deleted' THEN
            CASE WHEN counts.deleted_building_count IS NULL
                      AND counts.unassigned_rack_count IS NULL
                      AND counts.unassigned_device_count IS NULL
                      AND counts.deleted_response_profile_count IS NULL
                 THEN 'Deleted site'
                 ELSE CONCAT(
                     'Deleted site', COALESCE(' ' || counts.site_id, ''), ': ',
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
                     'Deleted building', COALESCE(' ' || counts.building_id, ''), ': ',
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
$$;
