-- name: NextTicketNumber :one
-- Returns the next ticket number for the org. Runs inside a
-- transaction to prevent duplicates under concurrent inserts.
SELECT COALESCE(MAX(id), 0) + 1 AS next_id
FROM repair_ticket
WHERE org_id = sqlc.arg('org_id');

-- name: CreateRepairTicket :one
INSERT INTO repair_ticket (
    org_id, ticket_number, category, status, urgent,
    component, diagnosis, miner_identifier, alert_id,
    assignee_user_id, warranty_status,
    site_id, building_id, zone, rack_id, rack_label, group_label,
    notes, daily_impact_usd
) VALUES (
    sqlc.arg('org_id'),
    sqlc.arg('ticket_number'),
    sqlc.arg('category'),
    1, -- OPEN
    sqlc.arg('urgent'),
    sqlc.arg('component'),
    sqlc.narg('diagnosis'),
    sqlc.narg('miner_identifier'),
    sqlc.narg('alert_id'),
    sqlc.narg('assignee_user_id'),
    sqlc.arg('warranty_status'),
    sqlc.narg('site_id'),
    sqlc.narg('building_id'),
    sqlc.narg('zone'),
    sqlc.narg('rack_id'),
    sqlc.narg('rack_label'),
    sqlc.narg('group_label'),
    sqlc.narg('notes'),
    sqlc.arg('daily_impact_usd')
)
RETURNING *;

-- name: GetRepairTicket :one
SELECT *
FROM repair_ticket
WHERE id = sqlc.arg('id')
  AND org_id = sqlc.arg('org_id')
  AND deleted_at IS NULL;

-- name: ListRepairTickets :many
-- Returns tickets matching the supplied filters. All narg filters are
-- optional; when NULL, that dimension is not filtered. search_query
-- performs case-insensitive prefix/substring matching across key text
-- fields. Cursor pagination via (id) descending.
SELECT
    rt.*,
    COALESCE(cc.comment_count, 0)::int AS comment_count,
    COALESCE(pc.parts_count, 0)::int AS parts_count
FROM repair_ticket rt
LEFT JOIN (
    SELECT ticket_id, COUNT(*)::int AS comment_count
    FROM repair_ticket_comment
    WHERE deleted_at IS NULL
    GROUP BY ticket_id
) cc ON cc.ticket_id = rt.id
LEFT JOIN (
    SELECT ticket_id, COUNT(*)::int AS parts_count
    FROM repair_ticket_part
    GROUP BY ticket_id
) pc ON pc.ticket_id = rt.id
WHERE rt.org_id = sqlc.arg('org_id')
  AND rt.deleted_at IS NULL
  AND (sqlc.narg('filter_statuses')::smallint[] IS NULL
       OR rt.status = ANY(sqlc.narg('filter_statuses')::smallint[]))
  AND (sqlc.narg('filter_categories')::smallint[] IS NULL
       OR rt.category = ANY(sqlc.narg('filter_categories')::smallint[]))
  AND (sqlc.narg('filter_site_ids')::bigint[] IS NULL
       OR rt.site_id = ANY(sqlc.narg('filter_site_ids')::bigint[]))
  AND (sqlc.narg('filter_building_ids')::bigint[] IS NULL
       OR rt.building_id = ANY(sqlc.narg('filter_building_ids')::bigint[]))
  AND (sqlc.narg('filter_rack_ids')::bigint[] IS NULL
       OR rt.rack_id = ANY(sqlc.narg('filter_rack_ids')::bigint[]))
  AND (sqlc.narg('filter_group_labels')::text[] IS NULL
       OR rt.group_label = ANY(sqlc.narg('filter_group_labels')::text[]))
  AND (sqlc.narg('filter_assignee_user_id')::bigint IS NULL
       OR rt.assignee_user_id = sqlc.narg('filter_assignee_user_id')::bigint)
  AND (sqlc.narg('filter_urgent_only')::boolean IS NULL
       OR sqlc.narg('filter_urgent_only')::boolean = false
       OR rt.urgent = true)
  AND (sqlc.narg('exclude_completed')::boolean IS NULL
       OR sqlc.narg('exclude_completed')::boolean = false
       OR rt.status != 5)
  AND (sqlc.narg('search_query')::text IS NULL
       OR rt.ticket_number ILIKE '%' || sqlc.narg('search_query')::text || '%'
       OR rt.component ILIKE '%' || sqlc.narg('search_query')::text || '%'
       OR rt.diagnosis ILIKE '%' || sqlc.narg('search_query')::text || '%'
       OR rt.miner_identifier ILIKE '%' || sqlc.narg('search_query')::text || '%')
  AND (sqlc.narg('cursor_id')::bigint IS NULL
       OR rt.id < sqlc.narg('cursor_id')::bigint)
ORDER BY rt.id DESC
LIMIT sqlc.arg('limit_n')::int;

-- name: CountRepairTickets :one
-- Returns the total count matching the same filters (for pagination).
SELECT COUNT(*)::int AS total_count
FROM repair_ticket rt
WHERE rt.org_id = sqlc.arg('org_id')
  AND rt.deleted_at IS NULL
  AND (sqlc.narg('filter_statuses')::smallint[] IS NULL
       OR rt.status = ANY(sqlc.narg('filter_statuses')::smallint[]))
  AND (sqlc.narg('filter_categories')::smallint[] IS NULL
       OR rt.category = ANY(sqlc.narg('filter_categories')::smallint[]))
  AND (sqlc.narg('filter_site_ids')::bigint[] IS NULL
       OR rt.site_id = ANY(sqlc.narg('filter_site_ids')::bigint[]))
  AND (sqlc.narg('filter_building_ids')::bigint[] IS NULL
       OR rt.building_id = ANY(sqlc.narg('filter_building_ids')::bigint[]))
  AND (sqlc.narg('filter_rack_ids')::bigint[] IS NULL
       OR rt.rack_id = ANY(sqlc.narg('filter_rack_ids')::bigint[]))
  AND (sqlc.narg('filter_group_labels')::text[] IS NULL
       OR rt.group_label = ANY(sqlc.narg('filter_group_labels')::text[]))
  AND (sqlc.narg('filter_assignee_user_id')::bigint IS NULL
       OR rt.assignee_user_id = sqlc.narg('filter_assignee_user_id')::bigint)
  AND (sqlc.narg('filter_urgent_only')::boolean IS NULL
       OR sqlc.narg('filter_urgent_only')::boolean = false
       OR rt.urgent = true)
  AND (sqlc.narg('exclude_completed')::boolean IS NULL
       OR sqlc.narg('exclude_completed')::boolean = false
       OR rt.status != 5)
  AND (sqlc.narg('search_query')::text IS NULL
       OR rt.ticket_number ILIKE '%' || sqlc.narg('search_query')::text || '%'
       OR rt.component ILIKE '%' || sqlc.narg('search_query')::text || '%'
       OR rt.diagnosis ILIKE '%' || sqlc.narg('search_query')::text || '%'
       OR rt.miner_identifier ILIKE '%' || sqlc.narg('search_query')::text || '%');

-- name: UpdateRepairTicket :one
UPDATE repair_ticket
SET status           = COALESCE(sqlc.narg('status'), status),
    urgent           = COALESCE(sqlc.narg('urgent'), urgent),
    assignee_user_id = CASE
        WHEN sqlc.arg('clear_assignee')::boolean THEN NULL
        ELSE COALESCE(sqlc.narg('assignee_user_id'), assignee_user_id)
    END,
    component        = COALESCE(sqlc.narg('component'), component),
    diagnosis        = COALESCE(sqlc.narg('diagnosis'), diagnosis),
    warranty_status  = COALESCE(sqlc.narg('warranty_status'), warranty_status),
    resolution       = COALESCE(sqlc.narg('resolution'), resolution),
    repair_location  = COALESCE(sqlc.narg('repair_location'), repair_location),
    notes            = COALESCE(sqlc.narg('notes'), notes),
    rma_vendor       = COALESCE(sqlc.narg('rma_vendor'), rma_vendor),
    rma_tracking     = COALESCE(sqlc.narg('rma_tracking'), rma_tracking),
    rma_eta          = COALESCE(sqlc.narg('rma_eta'), rma_eta),
    completed_at     = CASE
        WHEN sqlc.narg('status')::smallint = 5 THEN CURRENT_TIMESTAMP
        ELSE completed_at
    END,
    updated_at       = CURRENT_TIMESTAMP
WHERE id = sqlc.arg('id')
  AND org_id = sqlc.arg('org_id')
  AND deleted_at IS NULL
RETURNING *;

-- name: SoftDeleteRepairTicket :execrows
UPDATE repair_ticket
SET deleted_at = CURRENT_TIMESTAMP
WHERE id = sqlc.arg('id')
  AND org_id = sqlc.arg('org_id')
  AND deleted_at IS NULL;

-- name: BulkUpdateTicketStatus :execrows
UPDATE repair_ticket
SET status = sqlc.arg('new_status'),
    completed_at = CASE
        WHEN sqlc.arg('new_status')::smallint = 5 THEN CURRENT_TIMESTAMP
        ELSE completed_at
    END,
    updated_at = CURRENT_TIMESTAMP
WHERE id = ANY(sqlc.arg('ticket_ids')::bigint[])
  AND org_id = sqlc.arg('org_id')
  AND deleted_at IS NULL;

-- name: BulkAssignTickets :execrows
UPDATE repair_ticket
SET assignee_user_id = sqlc.narg('assignee_user_id'),
    updated_at = CURRENT_TIMESTAMP
WHERE id = ANY(sqlc.arg('ticket_ids')::bigint[])
  AND org_id = sqlc.arg('org_id')
  AND deleted_at IS NULL;

-- name: BulkMarkUrgent :execrows
UPDATE repair_ticket
SET urgent = true,
    updated_at = CURRENT_TIMESTAMP
WHERE id = ANY(sqlc.arg('ticket_ids')::bigint[])
  AND org_id = sqlc.arg('org_id')
  AND deleted_at IS NULL;

-- name: BulkCloseTickets :execrows
UPDATE repair_ticket
SET status = 5,
    resolution = sqlc.arg('resolution'),
    repair_location = sqlc.arg('repair_location'),
    notes = sqlc.narg('notes'),
    completed_at = CURRENT_TIMESTAMP,
    updated_at = CURRENT_TIMESTAMP
WHERE id = ANY(sqlc.arg('ticket_ids')::bigint[])
  AND org_id = sqlc.arg('org_id')
  AND deleted_at IS NULL;

-- name: CountTicketsByStatus :many
-- Returns per-status counts for the queue stats row and kanban headers.
SELECT status, COUNT(*)::int AS count
FROM repair_ticket
WHERE org_id = sqlc.arg('org_id')
  AND deleted_at IS NULL
GROUP BY status;

-- name: CountUnassignedTickets :one
SELECT COUNT(*)::int AS count
FROM repair_ticket
WHERE org_id = sqlc.arg('org_id')
  AND deleted_at IS NULL
  AND assignee_user_id IS NULL
  AND status != 5;

-- name: CountUrgentTickets :one
SELECT COUNT(*)::int AS count
FROM repair_ticket
WHERE org_id = sqlc.arg('org_id')
  AND deleted_at IS NULL
  AND urgent = true
  AND status != 5;

-- name: CountOverdueTickets :one
-- Overdue = non-completed, older than 72 hours.
SELECT COUNT(*)::int AS count
FROM repair_ticket
WHERE org_id = sqlc.arg('org_id')
  AND deleted_at IS NULL
  AND status != 5
  AND created_at < CURRENT_TIMESTAMP - INTERVAL '72 hours';

-- name: AvgTicketAgeHours :one
-- Average age in hours for non-completed tickets.
SELECT COALESCE(
    AVG(EXTRACT(EPOCH FROM (CURRENT_TIMESTAMP - created_at)) / 3600),
    0
)::double precision AS avg_hours
FROM repair_ticket
WHERE org_id = sqlc.arg('org_id')
  AND deleted_at IS NULL
  AND status != 5;

-- name: ListCompletedTickets :many
-- History tab: completed tickets with optional component and assignee filters.
SELECT
    rt.*,
    COALESCE(cc.comment_count, 0)::int AS comment_count,
    COALESCE(pc.parts_count, 0)::int AS parts_count
FROM repair_ticket rt
LEFT JOIN (
    SELECT ticket_id, COUNT(*)::int AS comment_count
    FROM repair_ticket_comment
    WHERE deleted_at IS NULL
    GROUP BY ticket_id
) cc ON cc.ticket_id = rt.id
LEFT JOIN (
    SELECT ticket_id, COUNT(*)::int AS parts_count
    FROM repair_ticket_part
    GROUP BY ticket_id
) pc ON pc.ticket_id = rt.id
WHERE rt.org_id = sqlc.arg('org_id')
  AND rt.deleted_at IS NULL
  AND rt.status = 5
  AND (sqlc.narg('filter_component')::text IS NULL
       OR rt.component = sqlc.narg('filter_component')::text)
  AND (sqlc.narg('filter_assignee_user_id')::bigint IS NULL
       OR rt.assignee_user_id = sqlc.narg('filter_assignee_user_id')::bigint)
  AND (sqlc.narg('cursor_id')::bigint IS NULL
       OR rt.id < sqlc.narg('cursor_id')::bigint)
ORDER BY rt.completed_at DESC NULLS LAST, rt.id DESC
LIMIT sqlc.arg('limit_n')::int;

-- name: ListTicketsByMiner :many
-- Miner detail section: tickets for a specific miner.
SELECT *
FROM repair_ticket
WHERE org_id = sqlc.arg('org_id')
  AND deleted_at IS NULL
  AND miner_identifier = sqlc.arg('miner_identifier')
ORDER BY
    CASE WHEN status != 5 THEN 0 ELSE 1 END,
    created_at DESC;

-- name: ListTicketsByRack :many
-- Rack detail section: non-completed tickets for miners in a rack.
SELECT *
FROM repair_ticket
WHERE org_id = sqlc.arg('org_id')
  AND deleted_at IS NULL
  AND rack_id = sqlc.arg('rack_id')
  AND status != 5
ORDER BY created_at DESC;
