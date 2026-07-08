-- name: InsertActivityLog :exec
-- The unique partial index on (batch_id, event_type) for '*.completed' event
-- types lets the Go layer detect idempotent re-inserts via pq unique_violation.
--
-- Single statement so the activity row and its site membership (#538) commit
-- atomically. member_site_ids carries the distinct touched sites for a
-- multi_site event (empty otherwise); member_unassigned adds the NULL-site
-- membership row when the multi-site set also touched site-less devices. Both
-- data-modifying CTEs run to completion regardless of the outer SELECT, and
-- an empty member array unnests to zero rows — so the single-site / org-level
-- path inserts only the activity_log row.
WITH inserted AS (
    INSERT INTO activity_log (
        event_id,
        event_category, event_type, description,
        result, error_message,
        scope_type, scope_label, scope_count,
        actor_type, user_id, username,
        organization_id, metadata, batch_id,
        site_id, multi_site
    ) VALUES (
        sqlc.arg('event_id'),
        sqlc.arg('event_category'), sqlc.arg('event_type'), sqlc.arg('description'),
        sqlc.arg('result'), sqlc.arg('error_message'),
        sqlc.arg('scope_type'), sqlc.arg('scope_label'), sqlc.arg('scope_count'),
        sqlc.arg('actor_type'), sqlc.arg('user_id'), sqlc.arg('username'),
        sqlc.arg('organization_id'), sqlc.arg('metadata'), sqlc.arg('batch_id'),
        sqlc.arg('site_id'), sqlc.arg('multi_site')
    )
    RETURNING id, organization_id
),
member_sites AS (
    INSERT INTO activity_log_site (activity_log_id, org_id, site_id)
    SELECT inserted.id, inserted.organization_id, member.site_id
    FROM inserted
    CROSS JOIN unnest(sqlc.arg('member_site_ids')::bigint[]) AS member(site_id)
    WHERE sqlc.arg('multi_site')::boolean
    RETURNING activity_log_id
)
INSERT INTO activity_log_site (activity_log_id, org_id, site_id)
SELECT inserted.id, inserted.organization_id, NULL::bigint
FROM inserted
WHERE sqlc.arg('multi_site')::boolean AND sqlc.arg('member_unassigned')::boolean;

-- name: ListActivityLogs :many
-- Array filter contract: the Go store layer must pass nil (not empty slice)
-- for the narg text[] filters below. An empty non-nil array
-- (pq.Array([]string{})) produces '{}' which matches nothing via ANY, leading
-- to zero results.
--
-- The site filter (site_ids / include_unassigned / org_level_categories) is an
-- arg, not a narg: the all-sites case is detected via cardinality() = 0, so the
-- Go layer must pass an empty (non-nil) bigint[] when no site filter is active,
-- matching the ListBuildings / ListRacks / ListMiners contract.
SELECT
    a.id, a.event_id, a.event_category, a.event_type, a.description,
    a.result, a.error_message,
    a.scope_type, a.scope_label, a.scope_count,
    a.actor_type, a.user_id, a.username,
    a.created_at, a.metadata, a.batch_id
FROM activity_log a
WHERE a.organization_id = sqlc.arg('org_id')
    AND (sqlc.narg('categories')::text[] IS NULL OR a.event_category = ANY(sqlc.narg('categories')::text[]))
    AND (sqlc.narg('event_types')::text[] IS NULL OR a.event_type = ANY(sqlc.narg('event_types')::text[]))
    AND (sqlc.narg('user_ids')::text[] IS NULL OR a.user_id = ANY(sqlc.narg('user_ids')::text[]))
    AND (sqlc.narg('scope_types')::text[] IS NULL OR a.scope_type = ANY(sqlc.narg('scope_types')::text[]))
    AND (
        sqlc.narg('search_pattern')::text IS NULL
        OR CONCAT_WS(' ', a.description,
            CASE
                WHEN a.event_type = 'login' THEN 'Logged in'
                WHEN a.event_type = 'login_failed' THEN 'Couldn''t log in'
                WHEN a.event_type = 'logout' THEN 'Logged out'
                WHEN a.event_type = 'create_admin_user' THEN 'Created admin account'
                WHEN a.event_type = 'create_user' THEN CONCAT('Created user', COALESCE(': ' || COALESCE(a.metadata->>'target_username', a.scope_label), ''))
                WHEN a.event_type = 'update_username' THEN 'Updated username'
                WHEN a.event_type = 'step_up_auth_failed' THEN 'Couldn''t verify authentication'
                WHEN a.event_type = 'update_password' THEN 'Updated password'
                WHEN a.event_type = 'reset_password' THEN CONCAT('Reset password', COALESCE(' for ' || COALESCE(a.metadata->>'target_username', a.scope_label), ''))
                WHEN a.event_type = 'deactivate_user' THEN CONCAT('Deactivated user', COALESCE(': ' || COALESCE(a.metadata->>'target_username', a.scope_label), ''))
                WHEN a.event_type = 'update_user_role' THEN COALESCE('Updated role for ' || COALESCE(a.metadata->>'target_username', a.scope_label), 'Updated user role')
                WHEN a.event_type = 'create_api_key' THEN 'Created API key'
                WHEN a.event_type = 'revoke_api_key' THEN 'Revoked API key'

                WHEN a.event_type = 'start_mining.completed' THEN 'Started mining'
                WHEN a.event_type = 'stop_mining.completed' THEN 'Stopped mining'
                WHEN a.event_type = 'reboot.completed' THEN 'Rebooted miners'
                WHEN a.event_type = 'blink_led.completed' THEN 'Blinked LEDs'
                WHEN a.event_type = 'download_logs.completed' THEN 'Downloaded logs'
                WHEN a.event_type = 'set_power_target.completed' THEN 'Updated power target'
                WHEN a.event_type = 'set_cooling_mode.completed' THEN 'Updated cooling mode'
                WHEN a.event_type = 'update_mining_pools.completed' THEN 'Updated mining pools'
                WHEN a.event_type = 'update_miner_password.completed' THEN 'Updated miner password'
                WHEN a.event_type = 'firmware_update.completed' THEN 'Updated firmware'
                WHEN a.event_type = 'unpair.completed' THEN 'Unpaired miners'
                WHEN a.event_type = 'curtail.completed' THEN 'Started curtailment'
                WHEN a.event_type = 'uncurtail.completed' THEN 'Ended curtailment'

                WHEN a.event_type = 'start_mining' THEN 'Starting mining'
                WHEN a.event_type = 'stop_mining' THEN 'Stopping mining'
                WHEN a.event_type = 'reboot' THEN 'Rebooting miners'
                WHEN a.event_type = 'blink_led' THEN 'Blinking LEDs'
                WHEN a.event_type = 'download_logs' THEN 'Downloading logs'
                WHEN a.event_type = 'set_power_target' THEN 'Updating power target'
                WHEN a.event_type = 'set_cooling_mode' THEN 'Updating cooling mode'
                WHEN a.event_type = 'update_mining_pools' THEN 'Updating mining pools'
                WHEN a.event_type = 'update_miner_password' THEN 'Updating miner password'
                WHEN a.event_type = 'firmware_update' THEN 'Updating firmware'
                WHEN a.event_type = 'unpair' THEN 'Unpairing miners'
                WHEN a.event_type = 'curtail' THEN 'Starting curtailment'
                WHEN a.event_type = 'uncurtail' THEN 'Ending curtailment'

                WHEN a.event_type = 'create_collection' THEN CONCAT('Created ', COALESCE(a.scope_type, 'collection'), COALESCE(': ' || a.scope_label, ''))
                WHEN a.event_type = 'update_collection' THEN CONCAT('Updated ', COALESCE(a.scope_type, 'collection'), COALESCE(': ' || a.scope_label, ''))
                WHEN a.event_type = 'delete_collection' THEN CONCAT('Deleted ', COALESCE(a.scope_type, 'collection'), COALESCE(': ' || a.scope_label, ''))
                WHEN a.event_type = 'add_devices' THEN CONCAT('Added miners to group', COALESCE(': ' || a.scope_label, ''))
                WHEN a.event_type = 'remove_devices' THEN CONCAT('Removed miners from group', COALESCE(': ' || a.scope_label, ''))
                WHEN a.event_type = 'assign_devices_to_rack' THEN CONCAT('Assigned miners to rack', COALESCE(': ' || a.scope_label, ''))
                WHEN a.event_type IN ('set_rack_slot', 'clear_rack_slot') THEN CONCAT('Updated rack position', COALESCE(': ' || a.scope_label, ''))
                WHEN a.event_type = 'save_rack' THEN CONCAT('Saved rack', COALESCE(': ' || a.scope_label, ''))
                WHEN a.event_type = 'unpair_miners' THEN 'Unpaired miners'
                WHEN a.event_type = 'rename_miners' THEN 'Renamed miners'

                WHEN a.event_type = 'create_pool' THEN CONCAT('Created pool', COALESCE(': ' || COALESCE(a.metadata->>'pool_name', a.scope_label), ''))
                WHEN a.event_type = 'update_pool' THEN CONCAT('Updated pool', COALESCE(': ' || COALESCE(a.metadata->>'pool_name', a.scope_label), ''))
                WHEN a.event_type = 'delete_pool' THEN CONCAT('Deleted pool', COALESCE(': ' || COALESCE(a.metadata->>'pool_name', a.scope_label), ''))
                WHEN a.event_type = 'create_role' THEN CONCAT('Created role', COALESCE(': ' || COALESCE(a.metadata->>'role_name', a.scope_label), ''))
                WHEN a.event_type = 'update_role' THEN CONCAT('Updated role', COALESCE(': ' || COALESCE(a.metadata->>'role_name', a.scope_label), ''))
                WHEN a.event_type = 'delete_role' THEN CONCAT('Deleted role', COALESCE(': ' || COALESCE(a.metadata->>'role_name', a.scope_label), ''))
                WHEN a.event_type = 'site.created' THEN CONCAT('Created site', COALESCE(': ' || COALESCE(a.metadata->>'site_name', a.scope_label), ''))
                WHEN a.event_type = 'site.updated' THEN CONCAT('Updated site', COALESCE(': ' || COALESCE(a.metadata->>'site_name', a.scope_label), ''))
                WHEN a.event_type = 'site.deleted' THEN 'Deleted site'
                WHEN a.event_type = 'building.created' THEN CONCAT('Created building', COALESCE(': ' || COALESCE(a.metadata->>'building_name', a.scope_label), ''))
                WHEN a.event_type = 'building.updated' THEN CONCAT('Updated building', COALESCE(': ' || COALESCE(a.metadata->>'building_name', a.scope_label), ''))
                WHEN a.event_type = 'building.deleted' THEN 'Deleted building'
                WHEN a.event_type = 'building.assigned_to_site' THEN 'Assigned building to site'
                WHEN a.event_type = 'racks.assigned_to_site' THEN 'Assigned racks to site'
                WHEN a.event_type = 'building.rack_assigned' THEN 'Assigned racks to building'
                WHEN a.event_type = 'devices.reassigned_to_site' THEN 'Reassigned miners to site'
                WHEN a.event_type = 'devices.reassigned_to_building' THEN 'Reassigned miners to building'

                WHEN a.event_type = 'schedule_executed' THEN 'Ran schedule'
                WHEN a.event_type = 'schedule_window_ended' THEN 'Ended schedule window'
                WHEN a.event_type = 'schedule_completed' THEN 'Completed schedule'
                WHEN a.event_type = 'schedule_conflict_skip' THEN 'Skipped schedule conflict'
                WHEN a.event_type = 'schedule_skipped_due_to_curtailment' THEN 'Skipped schedule during curtailment'
                WHEN a.event_type = 'curtailment_started' THEN 'Started curtailment'
                WHEN a.event_type = 'curtailment_admin_terminated' THEN 'Stopped curtailment'
                WHEN a.event_type = 'curtailment_admin_terminated_replay' THEN 'Curtailment already stopped'
                WHEN a.event_type = 'curtailment_updated' THEN 'Updated curtailment'
                WHEN a.event_type = 'curtailment_force_released' THEN 'Released curtailment ownership'
                WHEN a.event_type = 'command_preflight_blocked' THEN 'Command couldn''t run'
                WHEN a.event_type = 'command_filter_skip' THEN 'Command ran with skipped miners'
            END
        ) ILIKE sqlc.narg('search_pattern') ESCAPE '\'
    )
    AND (sqlc.narg('start_time')::timestamptz IS NULL OR a.created_at >= sqlc.narg('start_time'))
    AND (sqlc.narg('end_time')::timestamptz IS NULL OR a.created_at <= sqlc.narg('end_time'))
    AND (sqlc.narg('cursor_time')::timestamptz IS NULL OR (a.created_at, a.id) < (sqlc.narg('cursor_time')::timestamptz, sqlc.narg('cursor_id')::bigint))
    AND (
        -- all-sites: no site filter active
        (cardinality(sqlc.arg('site_ids')::bigint[]) = 0
         AND sqlc.arg('include_unassigned')::boolean = false)

        -- direct (non-batch) events. Site scope has two representations (#538):
        -- the scalar a.site_id is the single-site fast path; multi_site events
        -- carry their full touched-site set in activity_log_site (the two are
        -- mutually exclusive — multi_site rows have site_id NULL). So a
        -- cross-site event surfaces under EACH of its sites via the membership
        -- EXISTS. The unassigned bucket takes a single-slot site-less event
        -- (site_id NULL, not multi_site, non-org-level) OR a multi-site event
        -- that also touched site-less devices (its NULL-site membership row).
        OR (a.batch_id IS NULL AND (
                a.site_id = ANY(sqlc.arg('site_ids')::bigint[])
             OR (a.multi_site AND EXISTS (
                    SELECT 1 FROM activity_log_site als
                    WHERE als.activity_log_id = a.id
                      AND als.site_id = ANY(sqlc.arg('site_ids')::bigint[])
                ))
             OR (sqlc.arg('include_unassigned')::boolean
                 AND a.site_id IS NULL
                 AND NOT a.multi_site
                 AND a.event_category <> ALL(sqlc.arg('org_level_categories')::text[]))
             OR (sqlc.arg('include_unassigned')::boolean
                 AND a.multi_site
                 AND EXISTS (
                    SELECT 1 FROM activity_log_site als
                    WHERE als.activity_log_id = a.id
                      AND als.site_id IS NULL
                ))
        ))

        -- command-batch events: derive touched sites from command_on_device_log
        OR (a.batch_id IS NOT NULL AND EXISTS (
                SELECT 1
                FROM command_on_device_log codl
                JOIN command_batch_log cbl ON cbl.id = codl.command_batch_log_id
                WHERE cbl.uuid = a.batch_id
                  AND (
                        codl.site_id = ANY(sqlc.arg('site_ids')::bigint[])
                     OR (sqlc.arg('include_unassigned')::boolean AND codl.site_id IS NULL)
                  )
        ))
    )
ORDER BY a.created_at DESC, a.id DESC
LIMIT sqlc.arg('page_size');

-- name: CountActivityLogs :one
-- Site filter must stay byte-for-byte identical to ListActivityLogs so the
-- pagination total never disagrees with the rendered feed (or the CSV export,
-- which reuses ListActivityLogs).
SELECT COUNT(*)
FROM activity_log a
WHERE a.organization_id = sqlc.arg('org_id')
    AND (sqlc.narg('categories')::text[] IS NULL OR a.event_category = ANY(sqlc.narg('categories')::text[]))
    AND (sqlc.narg('event_types')::text[] IS NULL OR a.event_type = ANY(sqlc.narg('event_types')::text[]))
    AND (sqlc.narg('user_ids')::text[] IS NULL OR a.user_id = ANY(sqlc.narg('user_ids')::text[]))
    AND (sqlc.narg('scope_types')::text[] IS NULL OR a.scope_type = ANY(sqlc.narg('scope_types')::text[]))
    AND (
        sqlc.narg('search_pattern')::text IS NULL
        OR CONCAT_WS(' ', a.description,
            CASE
                WHEN a.event_type = 'login' THEN 'Logged in'
                WHEN a.event_type = 'login_failed' THEN 'Couldn''t log in'
                WHEN a.event_type = 'logout' THEN 'Logged out'
                WHEN a.event_type = 'create_admin_user' THEN 'Created admin account'
                WHEN a.event_type = 'create_user' THEN CONCAT('Created user', COALESCE(': ' || COALESCE(a.metadata->>'target_username', a.scope_label), ''))
                WHEN a.event_type = 'update_username' THEN 'Updated username'
                WHEN a.event_type = 'step_up_auth_failed' THEN 'Couldn''t verify authentication'
                WHEN a.event_type = 'update_password' THEN 'Updated password'
                WHEN a.event_type = 'reset_password' THEN CONCAT('Reset password', COALESCE(' for ' || COALESCE(a.metadata->>'target_username', a.scope_label), ''))
                WHEN a.event_type = 'deactivate_user' THEN CONCAT('Deactivated user', COALESCE(': ' || COALESCE(a.metadata->>'target_username', a.scope_label), ''))
                WHEN a.event_type = 'update_user_role' THEN COALESCE('Updated role for ' || COALESCE(a.metadata->>'target_username', a.scope_label), 'Updated user role')
                WHEN a.event_type = 'create_api_key' THEN 'Created API key'
                WHEN a.event_type = 'revoke_api_key' THEN 'Revoked API key'

                WHEN a.event_type = 'start_mining.completed' THEN 'Started mining'
                WHEN a.event_type = 'stop_mining.completed' THEN 'Stopped mining'
                WHEN a.event_type = 'reboot.completed' THEN 'Rebooted miners'
                WHEN a.event_type = 'blink_led.completed' THEN 'Blinked LEDs'
                WHEN a.event_type = 'download_logs.completed' THEN 'Downloaded logs'
                WHEN a.event_type = 'set_power_target.completed' THEN 'Updated power target'
                WHEN a.event_type = 'set_cooling_mode.completed' THEN 'Updated cooling mode'
                WHEN a.event_type = 'update_mining_pools.completed' THEN 'Updated mining pools'
                WHEN a.event_type = 'update_miner_password.completed' THEN 'Updated miner password'
                WHEN a.event_type = 'firmware_update.completed' THEN 'Updated firmware'
                WHEN a.event_type = 'unpair.completed' THEN 'Unpaired miners'
                WHEN a.event_type = 'curtail.completed' THEN 'Started curtailment'
                WHEN a.event_type = 'uncurtail.completed' THEN 'Ended curtailment'

                WHEN a.event_type = 'start_mining' THEN 'Starting mining'
                WHEN a.event_type = 'stop_mining' THEN 'Stopping mining'
                WHEN a.event_type = 'reboot' THEN 'Rebooting miners'
                WHEN a.event_type = 'blink_led' THEN 'Blinking LEDs'
                WHEN a.event_type = 'download_logs' THEN 'Downloading logs'
                WHEN a.event_type = 'set_power_target' THEN 'Updating power target'
                WHEN a.event_type = 'set_cooling_mode' THEN 'Updating cooling mode'
                WHEN a.event_type = 'update_mining_pools' THEN 'Updating mining pools'
                WHEN a.event_type = 'update_miner_password' THEN 'Updating miner password'
                WHEN a.event_type = 'firmware_update' THEN 'Updating firmware'
                WHEN a.event_type = 'unpair' THEN 'Unpairing miners'
                WHEN a.event_type = 'curtail' THEN 'Starting curtailment'
                WHEN a.event_type = 'uncurtail' THEN 'Ending curtailment'

                WHEN a.event_type = 'create_collection' THEN CONCAT('Created ', COALESCE(a.scope_type, 'collection'), COALESCE(': ' || a.scope_label, ''))
                WHEN a.event_type = 'update_collection' THEN CONCAT('Updated ', COALESCE(a.scope_type, 'collection'), COALESCE(': ' || a.scope_label, ''))
                WHEN a.event_type = 'delete_collection' THEN CONCAT('Deleted ', COALESCE(a.scope_type, 'collection'), COALESCE(': ' || a.scope_label, ''))
                WHEN a.event_type = 'add_devices' THEN CONCAT('Added miners to group', COALESCE(': ' || a.scope_label, ''))
                WHEN a.event_type = 'remove_devices' THEN CONCAT('Removed miners from group', COALESCE(': ' || a.scope_label, ''))
                WHEN a.event_type = 'assign_devices_to_rack' THEN CONCAT('Assigned miners to rack', COALESCE(': ' || a.scope_label, ''))
                WHEN a.event_type IN ('set_rack_slot', 'clear_rack_slot') THEN CONCAT('Updated rack position', COALESCE(': ' || a.scope_label, ''))
                WHEN a.event_type = 'save_rack' THEN CONCAT('Saved rack', COALESCE(': ' || a.scope_label, ''))
                WHEN a.event_type = 'unpair_miners' THEN 'Unpaired miners'
                WHEN a.event_type = 'rename_miners' THEN 'Renamed miners'

                WHEN a.event_type = 'create_pool' THEN CONCAT('Created pool', COALESCE(': ' || COALESCE(a.metadata->>'pool_name', a.scope_label), ''))
                WHEN a.event_type = 'update_pool' THEN CONCAT('Updated pool', COALESCE(': ' || COALESCE(a.metadata->>'pool_name', a.scope_label), ''))
                WHEN a.event_type = 'delete_pool' THEN CONCAT('Deleted pool', COALESCE(': ' || COALESCE(a.metadata->>'pool_name', a.scope_label), ''))
                WHEN a.event_type = 'create_role' THEN CONCAT('Created role', COALESCE(': ' || COALESCE(a.metadata->>'role_name', a.scope_label), ''))
                WHEN a.event_type = 'update_role' THEN CONCAT('Updated role', COALESCE(': ' || COALESCE(a.metadata->>'role_name', a.scope_label), ''))
                WHEN a.event_type = 'delete_role' THEN CONCAT('Deleted role', COALESCE(': ' || COALESCE(a.metadata->>'role_name', a.scope_label), ''))
                WHEN a.event_type = 'site.created' THEN CONCAT('Created site', COALESCE(': ' || COALESCE(a.metadata->>'site_name', a.scope_label), ''))
                WHEN a.event_type = 'site.updated' THEN CONCAT('Updated site', COALESCE(': ' || COALESCE(a.metadata->>'site_name', a.scope_label), ''))
                WHEN a.event_type = 'site.deleted' THEN 'Deleted site'
                WHEN a.event_type = 'building.created' THEN CONCAT('Created building', COALESCE(': ' || COALESCE(a.metadata->>'building_name', a.scope_label), ''))
                WHEN a.event_type = 'building.updated' THEN CONCAT('Updated building', COALESCE(': ' || COALESCE(a.metadata->>'building_name', a.scope_label), ''))
                WHEN a.event_type = 'building.deleted' THEN 'Deleted building'
                WHEN a.event_type = 'building.assigned_to_site' THEN 'Assigned building to site'
                WHEN a.event_type = 'racks.assigned_to_site' THEN 'Assigned racks to site'
                WHEN a.event_type = 'building.rack_assigned' THEN 'Assigned racks to building'
                WHEN a.event_type = 'devices.reassigned_to_site' THEN 'Reassigned miners to site'
                WHEN a.event_type = 'devices.reassigned_to_building' THEN 'Reassigned miners to building'

                WHEN a.event_type = 'schedule_executed' THEN 'Ran schedule'
                WHEN a.event_type = 'schedule_window_ended' THEN 'Ended schedule window'
                WHEN a.event_type = 'schedule_completed' THEN 'Completed schedule'
                WHEN a.event_type = 'schedule_conflict_skip' THEN 'Skipped schedule conflict'
                WHEN a.event_type = 'schedule_skipped_due_to_curtailment' THEN 'Skipped schedule during curtailment'
                WHEN a.event_type = 'curtailment_started' THEN 'Started curtailment'
                WHEN a.event_type = 'curtailment_admin_terminated' THEN 'Stopped curtailment'
                WHEN a.event_type = 'curtailment_admin_terminated_replay' THEN 'Curtailment already stopped'
                WHEN a.event_type = 'curtailment_updated' THEN 'Updated curtailment'
                WHEN a.event_type = 'curtailment_force_released' THEN 'Released curtailment ownership'
                WHEN a.event_type = 'command_preflight_blocked' THEN 'Command couldn''t run'
                WHEN a.event_type = 'command_filter_skip' THEN 'Command ran with skipped miners'
            END
        ) ILIKE sqlc.narg('search_pattern') ESCAPE '\'
    )
    AND (sqlc.narg('start_time')::timestamptz IS NULL OR a.created_at >= sqlc.narg('start_time'))
    AND (sqlc.narg('end_time')::timestamptz IS NULL OR a.created_at <= sqlc.narg('end_time'))
    AND (
        (cardinality(sqlc.arg('site_ids')::bigint[]) = 0
         AND sqlc.arg('include_unassigned')::boolean = false)

        OR (a.batch_id IS NULL AND (
                a.site_id = ANY(sqlc.arg('site_ids')::bigint[])
             OR (a.multi_site AND EXISTS (
                    SELECT 1 FROM activity_log_site als
                    WHERE als.activity_log_id = a.id
                      AND als.site_id = ANY(sqlc.arg('site_ids')::bigint[])
                ))
             OR (sqlc.arg('include_unassigned')::boolean
                 AND a.site_id IS NULL
                 AND NOT a.multi_site
                 AND a.event_category <> ALL(sqlc.arg('org_level_categories')::text[]))
             OR (sqlc.arg('include_unassigned')::boolean
                 AND a.multi_site
                 AND EXISTS (
                    SELECT 1 FROM activity_log_site als
                    WHERE als.activity_log_id = a.id
                      AND als.site_id IS NULL
                ))
        ))

        OR (a.batch_id IS NOT NULL AND EXISTS (
                SELECT 1
                FROM command_on_device_log codl
                JOIN command_batch_log cbl ON cbl.id = codl.command_batch_log_id
                WHERE cbl.uuid = a.batch_id
                  AND (
                        codl.site_id = ANY(sqlc.arg('site_ids')::bigint[])
                     OR (sqlc.arg('include_unassigned')::boolean AND codl.site_id IS NULL)
                  )
        ))
    );

-- name: GetDistinctActivityUsers :many
SELECT * FROM (
    SELECT DISTINCT ON (user_id) user_id, username
    FROM activity_log
    WHERE organization_id = sqlc.arg('org_id') AND user_id IS NOT NULL
    ORDER BY user_id, (username IS NULL) ASC, created_at DESC
) AS latest_users
ORDER BY username;

-- name: GetDistinctEventTypes :many
SELECT DISTINCT event_type, event_category
FROM activity_log
WHERE organization_id = sqlc.arg('org_id')
ORDER BY event_category, event_type;

-- name: GetDistinctScopeTypes :many
SELECT DISTINCT scope_type
FROM activity_log
WHERE organization_id = sqlc.arg('org_id') AND scope_type IS NOT NULL
ORDER BY scope_type;
