-- Channel listing uses immutable IDs so deleting or renaming the cursor's
-- channel does not change where the next page begins.

-- name: ListReleaseChannelsPage :many
SELECT * FROM release_channel
WHERE org_id = sqlc.arg('org_id')
  AND id > sqlc.arg('after_channel_id')
ORDER BY id
LIMIT sqlc.arg('page_limit');

-- name: ListReleaseChannelPageTargets :many
SELECT t.channel_id,
       t.target_type,
       COALESCE(t.target_id, 0)::bigint AS target_id,
       COALESCE(t.device_identifier, '')::text AS device_identifier
FROM release_channel_target t
JOIN release_channel c ON c.id = t.channel_id
WHERE c.org_id = sqlc.arg('org_id')
  AND c.id = ANY(sqlc.arg('channel_ids')::bigint[])
ORDER BY t.channel_id, t.target_type, t.target_id, t.device_identifier;

-- name: ListReleaseChannelPageMemberModels :many
-- Aggregate only the requested channels' resolved members. Preserve observed
-- hardware spelling; assignment matching uses normalized keys in the domain.
SELECT m.channel_id,
       COALESCE(dd.manufacturer, '')::text AS manufacturer,
       COALESCE(dd.model, '')::text AS model,
       COUNT(*)::int AS miner_count
FROM release_channel_member m
JOIN device d ON d.id = m.device_id
JOIN discovered_device dd ON dd.id = d.discovered_device_id
WHERE m.org_id = sqlc.arg('org_id')
  AND m.channel_id = ANY(sqlc.arg('channel_ids')::bigint[])
GROUP BY m.channel_id, COALESCE(dd.manufacturer, ''), COALESCE(dd.model, '')
ORDER BY m.channel_id, manufacturer, model;

-- name: ListReleaseChannelPageFirmware :many
-- Summaries need active assignments only; cleared rows retain generations for
-- later assignments but do not contribute additional model groups.
SELECT f.*
FROM release_channel_firmware f
JOIN release_channel c ON c.id = f.channel_id
WHERE c.org_id = sqlc.arg('org_id')
  AND c.id = ANY(sqlc.arg('channel_ids')::bigint[])
  AND f.firmware_checksum <> ''
ORDER BY f.channel_id, f.manufacturer, f.model;
