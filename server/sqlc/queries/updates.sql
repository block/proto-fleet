-- name: GetReleaseChannelSetting :one
-- No row means the org has never chosen a channel; the service layer maps
-- sql.ErrNoRows to the 'stable' default rather than seeding a row here.
SELECT * FROM release_channel_setting
WHERE organization_id = sqlc.arg('organization_id');

-- name: UpsertReleaseChannelSetting :one
INSERT INTO release_channel_setting (organization_id, channel)
VALUES (
    sqlc.arg('organization_id'),
    sqlc.arg('channel')
)
ON CONFLICT (organization_id)
DO UPDATE SET channel = EXCLUDED.channel, updated_at = now()
RETURNING *;
