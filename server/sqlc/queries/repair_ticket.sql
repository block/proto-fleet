-- name: NextRepairTicketNumber :one
INSERT INTO repair_ticket_counter (org_id, next_number)
VALUES (sqlc.arg('org_id'), 2)
ON CONFLICT (org_id) DO UPDATE
SET next_number = repair_ticket_counter.next_number + 1
RETURNING (next_number - 1)::bigint;

-- name: CreateRepairTicket :one
INSERT INTO repair_ticket (
    org_id, ticket_number, category, urgent, component, diagnosis,
    miner_identifier, alert_id, assignee_user_id, warranty_status,
    site_id, building_id, zone, rack_id, rack_label, group_label,
    notes, daily_impact_usd
) VALUES (
    sqlc.arg('org_id'), sqlc.arg('ticket_number'), sqlc.arg('category'), sqlc.arg('urgent'),
    sqlc.arg('component'), sqlc.narg('diagnosis'), sqlc.narg('miner_identifier'),
    sqlc.narg('alert_id'), sqlc.narg('assignee_user_id'), sqlc.arg('warranty_status'),
    sqlc.narg('site_id'), sqlc.narg('building_id'), sqlc.narg('zone'),
    sqlc.narg('rack_id'), sqlc.narg('rack_label'), sqlc.narg('group_label'),
    sqlc.narg('notes'), sqlc.arg('daily_impact_usd')
)
RETURNING id;

-- name: GetRepairTicket :one
SELECT
    rt.id, rt.org_id, rt.ticket_number, rt.category, rt.status, rt.urgent,
    rt.component, rt.diagnosis, rt.miner_identifier, rt.alert_id,
    rt.assignee_user_id, COALESCE(u.username, '') AS assignee_name,
    rt.warranty_status, rt.resolution, rt.repair_location, rt.notes,
    rt.daily_impact_usd, rt.rma_vendor, rt.rma_tracking, rt.rma_eta,
    rt.site_id, COALESCE(s.name, '') AS site_name,
    rt.building_id, COALESCE(b.name, '') AS building_name,
    rt.zone, rt.rack_id, rt.rack_label, rt.group_label,
    rt.completed_at, rt.created_at, rt.updated_at, rt.deleted_at
FROM repair_ticket rt
LEFT JOIN "user" u ON u.id = rt.assignee_user_id AND (u.deleted_at IS NULL OR rt.status = 5)
LEFT JOIN site s ON s.id = rt.site_id AND s.org_id = rt.org_id AND s.deleted_at IS NULL
LEFT JOIN building b ON b.id = rt.building_id AND b.org_id = rt.org_id AND b.deleted_at IS NULL
WHERE rt.id = sqlc.arg('id') AND rt.org_id = sqlc.arg('org_id') AND rt.deleted_at IS NULL;

-- name: GetRepairTicketForUpdate :one
SELECT
    rt.id, rt.org_id, rt.ticket_number, rt.category, rt.status, rt.urgent,
    rt.component, rt.diagnosis, rt.miner_identifier, rt.alert_id,
    rt.assignee_user_id, COALESCE(u.username, '') AS assignee_name,
    rt.warranty_status, rt.resolution, rt.repair_location, rt.notes,
    rt.daily_impact_usd, rt.rma_vendor, rt.rma_tracking, rt.rma_eta,
    rt.site_id, COALESCE(s.name, '') AS site_name,
    rt.building_id, COALESCE(b.name, '') AS building_name,
    rt.zone, rt.rack_id, rt.rack_label, rt.group_label,
    rt.completed_at, rt.created_at, rt.updated_at, rt.deleted_at
FROM repair_ticket rt
LEFT JOIN "user" u ON u.id = rt.assignee_user_id AND (u.deleted_at IS NULL OR rt.status = 5)
LEFT JOIN site s ON s.id = rt.site_id AND s.org_id = rt.org_id AND s.deleted_at IS NULL
LEFT JOIN building b ON b.id = rt.building_id AND b.org_id = rt.org_id AND b.deleted_at IS NULL
WHERE rt.id = sqlc.arg('id') AND rt.org_id = sqlc.arg('org_id') AND rt.deleted_at IS NULL
FOR UPDATE OF rt;

-- name: ListRepairTickets :many
WITH filtered AS (
    SELECT
        rt.id, rt.org_id, rt.ticket_number, rt.category, rt.status, rt.urgent,
        rt.component, rt.diagnosis, rt.miner_identifier, rt.alert_id,
        rt.assignee_user_id, COALESCE(u.username, '') AS assignee_name,
        rt.warranty_status, rt.resolution, rt.repair_location, rt.notes,
        rt.daily_impact_usd, rt.rma_vendor, rt.rma_tracking, rt.rma_eta,
        rt.site_id, COALESCE(s.name, '') AS site_name,
        rt.building_id, COALESCE(b.name, '') AS building_name,
        rt.zone, rt.rack_id, rt.rack_label, rt.group_label,
        rt.completed_at, rt.created_at, rt.updated_at, rt.deleted_at,
        (SELECT COUNT(*)::int FROM repair_ticket_comment c
         WHERE c.ticket_id = rt.id AND c.org_id = rt.org_id AND c.deleted_at IS NULL) AS comment_count,
        (SELECT COUNT(*)::int FROM repair_ticket_part p
         WHERE p.ticket_id = rt.id AND p.org_id = rt.org_id) AS parts_count,
        CASE sqlc.arg('sort_field')::smallint
            WHEN 1 THEN LOWER(rt.component)
            WHEN 2 THEN LOWER(COALESCE(rt.miner_identifier, rt.component))
            WHEN 3 THEN COALESCE(NULLIF(LOWER(CONCAT_WS(' / ', s.name, b.name, rt.zone, rt.rack_label, rt.group_label)), ''), '(unassigned)')
            WHEN 4 THEN LPAD(rt.status::text, 3, '0')
            ELSE TO_CHAR(rt.created_at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.US')
        END AS sort_value
    FROM repair_ticket rt
    LEFT JOIN "user" u ON u.id = rt.assignee_user_id AND (u.deleted_at IS NULL OR rt.status = 5)
    LEFT JOIN site s ON s.id = rt.site_id AND s.org_id = rt.org_id AND s.deleted_at IS NULL
    LEFT JOIN building b ON b.id = rt.building_id AND b.org_id = rt.org_id AND b.deleted_at IS NULL
    WHERE rt.org_id = sqlc.arg('org_id') AND rt.deleted_at IS NULL
      AND (sqlc.narg('filter_statuses')::smallint[] IS NULL OR rt.status = ANY(sqlc.narg('filter_statuses')::smallint[]))
      AND (sqlc.narg('filter_categories')::smallint[] IS NULL OR rt.category = ANY(sqlc.narg('filter_categories')::smallint[]))
      AND (sqlc.narg('filter_site_ids')::bigint[] IS NULL OR rt.site_id = ANY(sqlc.narg('filter_site_ids')::bigint[]))
      AND (sqlc.narg('filter_building_ids')::bigint[] IS NULL OR rt.building_id = ANY(sqlc.narg('filter_building_ids')::bigint[]))
      AND (sqlc.narg('filter_rack_ids')::bigint[] IS NULL OR rt.rack_id = ANY(sqlc.narg('filter_rack_ids')::bigint[]))
      AND (sqlc.narg('filter_group_labels')::text[] IS NULL OR rt.group_label = ANY(sqlc.narg('filter_group_labels')::text[]))
      AND (sqlc.narg('filter_assignee_user_id')::bigint IS NULL OR rt.assignee_user_id = sqlc.narg('filter_assignee_user_id'))
      AND (NOT sqlc.arg('filter_urgent_only')::boolean OR rt.urgent)
      AND (NOT sqlc.arg('filter_exclude_completed')::boolean OR rt.status <> 5)
      AND (NOT sqlc.arg('filter_overdue_only')::boolean OR (rt.status <> 5 AND rt.created_at < NOW() - INTERVAL '72 hours'))
      AND (sqlc.arg('search_query')::text = '' OR
           rt.ticket_number ILIKE '%' || sqlc.arg('search_query') || '%' OR
           rt.component ILIKE '%' || sqlc.arg('search_query') || '%' OR
           COALESCE(rt.miner_identifier, '') ILIKE '%' || sqlc.arg('search_query') || '%' OR
           COALESCE(rt.diagnosis, '') ILIKE '%' || sqlc.arg('search_query') || '%' OR
           COALESCE(s.name, '') ILIKE '%' || sqlc.arg('search_query') || '%' OR
           COALESCE(b.name, '') ILIKE '%' || sqlc.arg('search_query') || '%')
)
SELECT * FROM filtered
WHERE sqlc.narg('cursor_value')::text IS NULL
   OR (sqlc.arg('sort_direction')::smallint = 1 AND
       (sort_value COLLATE "C" > sqlc.narg('cursor_value')::text COLLATE "C" OR
        (sort_value = sqlc.narg('cursor_value') AND id > sqlc.narg('cursor_id')::bigint)))
   OR (sqlc.arg('sort_direction')::smallint = 2 AND
       (sort_value COLLATE "C" < sqlc.narg('cursor_value')::text COLLATE "C" OR
        (sort_value = sqlc.narg('cursor_value') AND id < sqlc.narg('cursor_id')::bigint)))
ORDER BY
    CASE WHEN sqlc.arg('sort_direction')::smallint = 1 THEN sort_value END COLLATE "C" ASC,
    CASE WHEN sqlc.arg('sort_direction')::smallint = 1 THEN id END ASC,
    CASE WHEN sqlc.arg('sort_direction')::smallint = 2 THEN sort_value END COLLATE "C" DESC,
    CASE WHEN sqlc.arg('sort_direction')::smallint = 2 THEN id END DESC
LIMIT sqlc.arg('limit_n');

-- name: CountRepairTickets :one
SELECT COUNT(*)::int
FROM repair_ticket rt
LEFT JOIN site s ON s.id = rt.site_id AND s.org_id = rt.org_id AND s.deleted_at IS NULL
LEFT JOIN building b ON b.id = rt.building_id AND b.org_id = rt.org_id AND b.deleted_at IS NULL
WHERE rt.org_id = sqlc.arg('org_id') AND rt.deleted_at IS NULL
  AND (sqlc.narg('filter_statuses')::smallint[] IS NULL OR rt.status = ANY(sqlc.narg('filter_statuses')::smallint[]))
  AND (sqlc.narg('filter_categories')::smallint[] IS NULL OR rt.category = ANY(sqlc.narg('filter_categories')::smallint[]))
  AND (sqlc.narg('filter_site_ids')::bigint[] IS NULL OR rt.site_id = ANY(sqlc.narg('filter_site_ids')::bigint[]))
  AND (sqlc.narg('filter_building_ids')::bigint[] IS NULL OR rt.building_id = ANY(sqlc.narg('filter_building_ids')::bigint[]))
  AND (sqlc.narg('filter_rack_ids')::bigint[] IS NULL OR rt.rack_id = ANY(sqlc.narg('filter_rack_ids')::bigint[]))
  AND (sqlc.narg('filter_group_labels')::text[] IS NULL OR rt.group_label = ANY(sqlc.narg('filter_group_labels')::text[]))
  AND (sqlc.narg('filter_assignee_user_id')::bigint IS NULL OR rt.assignee_user_id = sqlc.narg('filter_assignee_user_id'))
  AND (NOT sqlc.arg('filter_urgent_only')::boolean OR rt.urgent)
  AND (NOT sqlc.arg('filter_exclude_completed')::boolean OR rt.status <> 5)
  AND (NOT sqlc.arg('filter_overdue_only')::boolean OR (rt.status <> 5 AND rt.created_at < NOW() - INTERVAL '72 hours'))
  AND (sqlc.arg('search_query')::text = '' OR
       rt.ticket_number ILIKE '%' || sqlc.arg('search_query') || '%' OR
       rt.component ILIKE '%' || sqlc.arg('search_query') || '%' OR
       COALESCE(rt.miner_identifier, '') ILIKE '%' || sqlc.arg('search_query') || '%' OR
       COALESCE(rt.diagnosis, '') ILIKE '%' || sqlc.arg('search_query') || '%' OR
       COALESCE(s.name, '') ILIKE '%' || sqlc.arg('search_query') || '%' OR
       COALESCE(b.name, '') ILIKE '%' || sqlc.arg('search_query') || '%');

-- name: UpdateRepairTicket :one
UPDATE repair_ticket SET
    status = COALESCE(sqlc.narg('status'), status),
    urgent = COALESCE(sqlc.narg('urgent'), urgent),
    assignee_user_id = CASE WHEN sqlc.arg('clear_assignee')::boolean THEN NULL ELSE COALESCE(sqlc.narg('assignee_user_id'), assignee_user_id) END,
    component = COALESCE(sqlc.narg('component'), component),
    diagnosis = COALESCE(sqlc.narg('diagnosis'), diagnosis),
    warranty_status = COALESCE(sqlc.narg('warranty_status'), warranty_status),
    resolution = COALESCE(sqlc.narg('resolution'), resolution),
    repair_location = COALESCE(sqlc.narg('repair_location'), repair_location),
    notes = COALESCE(sqlc.narg('notes'), notes),
    rma_vendor = COALESCE(sqlc.narg('rma_vendor'), rma_vendor),
    rma_tracking = COALESCE(sqlc.narg('rma_tracking'), rma_tracking),
    rma_eta = CASE WHEN sqlc.arg('clear_rma_eta')::boolean THEN NULL ELSE COALESCE(sqlc.narg('rma_eta'), rma_eta) END,
    completed_at = CASE WHEN sqlc.narg('status')::smallint = 5 AND status <> 5 THEN NOW() ELSE completed_at END,
    updated_at = NOW()
WHERE id = sqlc.arg('id') AND org_id = sqlc.arg('org_id') AND deleted_at IS NULL
RETURNING id;

-- name: SoftDeleteRepairTicket :execrows
UPDATE repair_ticket SET deleted_at = NOW(), updated_at = NOW()
WHERE id = sqlc.arg('id') AND org_id = sqlc.arg('org_id') AND deleted_at IS NULL;

-- name: LockRepairTicketsByIDs :many
SELECT id FROM repair_ticket
WHERE org_id = sqlc.arg('org_id') AND id = ANY(sqlc.arg('ticket_ids')::bigint[]) AND deleted_at IS NULL
ORDER BY id
FOR UPDATE;

-- name: BulkUpdateTicketStatus :execrows
UPDATE repair_ticket SET status = sqlc.arg('status'), updated_at = NOW()
WHERE org_id = sqlc.arg('org_id') AND id = ANY(sqlc.arg('ticket_ids')::bigint[]) AND deleted_at IS NULL;

-- name: BulkAssignTickets :execrows
UPDATE repair_ticket SET assignee_user_id = sqlc.narg('assignee_user_id'), updated_at = NOW()
WHERE org_id = sqlc.arg('org_id') AND id = ANY(sqlc.arg('ticket_ids')::bigint[]) AND deleted_at IS NULL;

-- name: BulkMarkTicketsUrgent :execrows
UPDATE repair_ticket SET urgent = TRUE, updated_at = NOW()
WHERE org_id = sqlc.arg('org_id') AND id = ANY(sqlc.arg('ticket_ids')::bigint[]) AND deleted_at IS NULL;

-- name: BulkCloseTickets :execrows
UPDATE repair_ticket SET
    status = 5, resolution = sqlc.arg('resolution'), repair_location = sqlc.arg('repair_location'),
    notes = COALESCE(sqlc.narg('notes'), notes), completed_at = COALESCE(completed_at, NOW()), updated_at = NOW()
WHERE org_id = sqlc.arg('org_id') AND id = ANY(sqlc.arg('ticket_ids')::bigint[]) AND deleted_at IS NULL;

-- name: GetFilteredTicketStats :one
SELECT
    COUNT(*) FILTER (WHERE rt.status = 1)::int AS open_count,
    COUNT(*) FILTER (WHERE rt.status = 2)::int AS in_progress_count,
    COUNT(*) FILTER (WHERE rt.status = 3)::int AS on_hold_count,
    COUNT(*) FILTER (WHERE rt.status = 4)::int AS sent_to_vendor_count,
    COUNT(*) FILTER (WHERE rt.status = 5)::int AS completed_count,
    COUNT(*) FILTER (WHERE rt.status <> 5 AND rt.assignee_user_id IS NULL)::int AS unassigned_count,
    COUNT(*) FILTER (WHERE rt.status <> 5 AND rt.urgent)::int AS urgent_count,
    COUNT(*) FILTER (WHERE rt.status <> 5 AND rt.created_at < NOW() - INTERVAL '72 hours')::int AS overdue_count,
    COALESCE(AVG(EXTRACT(EPOCH FROM (NOW() - rt.created_at)) / 3600.0) FILTER (WHERE rt.status <> 5), 0)::float8 AS avg_age_hours
FROM repair_ticket rt
LEFT JOIN site s ON s.id = rt.site_id AND s.org_id = rt.org_id AND s.deleted_at IS NULL
LEFT JOIN building b ON b.id = rt.building_id AND b.org_id = rt.org_id AND b.deleted_at IS NULL
WHERE rt.org_id = sqlc.arg('org_id') AND rt.deleted_at IS NULL
  AND (sqlc.narg('filter_statuses')::smallint[] IS NULL OR rt.status = ANY(sqlc.narg('filter_statuses')::smallint[]))
  AND (sqlc.narg('filter_categories')::smallint[] IS NULL OR rt.category = ANY(sqlc.narg('filter_categories')::smallint[]))
  AND (sqlc.narg('filter_site_ids')::bigint[] IS NULL OR rt.site_id = ANY(sqlc.narg('filter_site_ids')::bigint[]))
  AND (sqlc.narg('filter_building_ids')::bigint[] IS NULL OR rt.building_id = ANY(sqlc.narg('filter_building_ids')::bigint[]))
  AND (sqlc.narg('filter_rack_ids')::bigint[] IS NULL OR rt.rack_id = ANY(sqlc.narg('filter_rack_ids')::bigint[]))
  AND (sqlc.narg('filter_group_labels')::text[] IS NULL OR rt.group_label = ANY(sqlc.narg('filter_group_labels')::text[]))
  AND (sqlc.narg('filter_assignee_user_id')::bigint IS NULL OR rt.assignee_user_id = sqlc.narg('filter_assignee_user_id'))
  AND (NOT sqlc.arg('filter_urgent_only')::boolean OR rt.urgent)
  AND (NOT sqlc.arg('filter_exclude_completed')::boolean OR rt.status <> 5)
  AND (NOT sqlc.arg('filter_overdue_only')::boolean OR (rt.status <> 5 AND rt.created_at < NOW() - INTERVAL '72 hours'))
  AND (sqlc.arg('search_query')::text = '' OR
       rt.ticket_number ILIKE '%' || sqlc.arg('search_query') || '%' OR
       rt.component ILIKE '%' || sqlc.arg('search_query') || '%' OR
       COALESCE(rt.miner_identifier, '') ILIKE '%' || sqlc.arg('search_query') || '%' OR
       COALESCE(rt.diagnosis, '') ILIKE '%' || sqlc.arg('search_query') || '%' OR
       COALESCE(s.name, '') ILIKE '%' || sqlc.arg('search_query') || '%' OR
       COALESCE(b.name, '') ILIKE '%' || sqlc.arg('search_query') || '%');

-- name: ListCompletedTickets :many
WITH completed AS (
    SELECT
        rt.id, rt.org_id, rt.ticket_number, rt.category, rt.status, rt.urgent,
        rt.component, rt.diagnosis, rt.miner_identifier, rt.alert_id,
        rt.assignee_user_id, COALESCE(u.username, '') AS assignee_name,
        rt.warranty_status, rt.resolution, rt.repair_location, rt.notes,
        rt.daily_impact_usd, rt.rma_vendor, rt.rma_tracking, rt.rma_eta,
        rt.site_id, COALESCE(s.name, '') AS site_name,
        rt.building_id, COALESCE(b.name, '') AS building_name,
        rt.zone, rt.rack_id, rt.rack_label, rt.group_label,
        rt.completed_at, rt.created_at, rt.updated_at, rt.deleted_at,
        (SELECT COUNT(*)::int FROM repair_ticket_comment c WHERE c.ticket_id = rt.id AND c.org_id = rt.org_id AND c.deleted_at IS NULL) AS comment_count,
        (SELECT COUNT(*)::int FROM repair_ticket_part p WHERE p.ticket_id = rt.id AND p.org_id = rt.org_id) AS parts_count,
        CASE sqlc.arg('sort_field')::smallint
            WHEN 1 THEN LOWER(rt.component)
            WHEN 2 THEN LOWER(COALESCE(rt.miner_identifier, rt.component))
            WHEN 3 THEN COALESCE(NULLIF(LOWER(CONCAT_WS(' / ', s.name, b.name, rt.zone, rt.rack_label, rt.group_label)), ''), '(unassigned)')
            WHEN 4 THEN LPAD(rt.status::text, 3, '0')
            WHEN 6 THEN TO_CHAR(COALESCE(rt.completed_at, rt.updated_at) AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.US')
            ELSE TO_CHAR(rt.created_at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.US')
        END AS sort_value
    FROM repair_ticket rt
    LEFT JOIN "user" u ON u.id = rt.assignee_user_id
    LEFT JOIN site s ON s.id = rt.site_id AND s.org_id = rt.org_id AND s.deleted_at IS NULL
    LEFT JOIN building b ON b.id = rt.building_id AND b.org_id = rt.org_id AND b.deleted_at IS NULL
    WHERE rt.org_id = sqlc.arg('org_id') AND rt.deleted_at IS NULL AND rt.status = 5
      AND (sqlc.narg('component_filter')::text IS NULL OR rt.component = sqlc.narg('component_filter'))
      AND (sqlc.narg('assignee_filter')::bigint IS NULL OR rt.assignee_user_id = sqlc.narg('assignee_filter'))
)
SELECT * FROM completed
WHERE sqlc.narg('cursor_value')::text IS NULL
   OR (sqlc.arg('sort_direction')::smallint = 1 AND
       (sort_value COLLATE "C" > sqlc.narg('cursor_value')::text COLLATE "C" OR
        (sort_value = sqlc.narg('cursor_value') AND id > sqlc.narg('cursor_id')::bigint)))
   OR (sqlc.arg('sort_direction')::smallint = 2 AND
       (sort_value COLLATE "C" < sqlc.narg('cursor_value')::text COLLATE "C" OR
        (sort_value = sqlc.narg('cursor_value') AND id < sqlc.narg('cursor_id')::bigint)))
ORDER BY
    CASE WHEN sqlc.arg('sort_direction')::smallint = 1 THEN sort_value END COLLATE "C" ASC,
    CASE WHEN sqlc.arg('sort_direction')::smallint = 1 THEN id END ASC,
    CASE WHEN sqlc.arg('sort_direction')::smallint = 2 THEN sort_value END COLLATE "C" DESC,
    CASE WHEN sqlc.arg('sort_direction')::smallint = 2 THEN id END DESC
LIMIT sqlc.arg('limit_n');

-- name: CountCompletedTickets :one
SELECT COUNT(*)::int FROM repair_ticket
WHERE org_id = sqlc.arg('org_id') AND deleted_at IS NULL AND status = 5
  AND (sqlc.narg('component_filter')::text IS NULL OR component = sqlc.narg('component_filter'))
  AND (sqlc.narg('assignee_filter')::bigint IS NULL OR assignee_user_id = sqlc.narg('assignee_filter'));

-- name: ListTicketsByMiner :many
SELECT id FROM repair_ticket
WHERE org_id = sqlc.arg('org_id') AND miner_identifier = sqlc.arg('miner_identifier') AND deleted_at IS NULL
ORDER BY CASE WHEN status = 5 THEN 1 ELSE 0 END, created_at DESC, id DESC;

-- name: ListTicketsByRack :many
SELECT id FROM repair_ticket
WHERE org_id = sqlc.arg('org_id') AND rack_id = sqlc.arg('rack_id') AND status <> 5 AND deleted_at IS NULL
ORDER BY created_at DESC, id DESC;
