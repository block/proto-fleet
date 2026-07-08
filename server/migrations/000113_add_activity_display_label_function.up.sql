-- Single source of truth for the search document used by ListActivityLogs and
-- CountActivityLogs: maps an activity_log row's event_type (plus scope/metadata
-- context) to the human-readable label the client renders, so searching the
-- visible text matches rows. Keep in sync with the client-side label maps in
-- client/src/protoFleet/features/activity/utils/ (formatLabel.ts,
-- formatActivityDescription.ts).
CREATE FUNCTION activity_display_label(
    event_type TEXT,
    scope_type TEXT,
    scope_label TEXT,
    metadata JSONB
) RETURNS TEXT
LANGUAGE SQL
IMMUTABLE
PARALLEL SAFE
AS $$
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
    WHEN event_type = 'assign_devices_to_rack' THEN CONCAT('Assigned miners to rack', COALESCE(': ' || scope_label, ''))
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
    WHEN event_type = 'site.deleted' THEN 'Deleted site'
    WHEN event_type = 'building.created' THEN CONCAT('Created building', COALESCE(': ' || COALESCE(metadata->>'building_name', scope_label), ''))
    WHEN event_type = 'building.updated' THEN CONCAT('Updated building', COALESCE(': ' || COALESCE(metadata->>'building_name', scope_label), ''))
    WHEN event_type = 'building.deleted' THEN 'Deleted building'
    WHEN event_type = 'building.assigned_to_site' THEN 'Assigned building to site'
    WHEN event_type = 'racks.assigned_to_site' THEN 'Assigned racks to site'
    WHEN event_type = 'building.rack_assigned' THEN 'Assigned racks to building'
    WHEN event_type = 'devices.reassigned_to_site' THEN 'Reassigned miners to site'
    WHEN event_type = 'devices.reassigned_to_building' THEN 'Reassigned miners to building'

    WHEN event_type = 'schedule_executed' THEN 'Ran schedule'
    WHEN event_type = 'schedule_window_ended' THEN 'Ended schedule window'
    WHEN event_type = 'schedule_completed' THEN 'Completed schedule'
    WHEN event_type = 'schedule_conflict_skip' THEN 'Skipped schedule conflict'
    WHEN event_type = 'schedule_skipped_due_to_curtailment' THEN 'Skipped schedule during curtailment'
    WHEN event_type = 'curtailment_started' THEN 'Started curtailment'
    WHEN event_type = 'curtailment_admin_terminated' THEN 'Stopped curtailment'
    WHEN event_type = 'curtailment_admin_terminated_replay' THEN 'Curtailment already stopped'
    WHEN event_type = 'curtailment_updated' THEN 'Updated curtailment'
    WHEN event_type = 'curtailment_force_released' THEN 'Released curtailment ownership'
    WHEN event_type = 'command_preflight_blocked' THEN 'Command couldn''t run'
    WHEN event_type = 'command_filter_skip' THEN 'Command ran with skipped miners'
END
$$;
