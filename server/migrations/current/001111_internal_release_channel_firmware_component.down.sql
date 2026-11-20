DROP FUNCTION release_channel_firmware_version(bigint, integer, text, text);
CREATE FUNCTION release_channel_firmware_version(device_id bigint, device_class integer, controller_version text)
RETURNS SETOF text LANGUAGE sql STABLE PARALLEL SAFE ROWS 1 AS $$
    SELECT CASE WHEN device_class = 2 THEN (
        SELECT CASE WHEN i.hashboard_firmware_mixed THEN '' ELSE NULLIF(i.hashboard_firmware_version, '') END
        FROM container_inventory i
        WHERE i.device_id = $1 AND i.observed_at > statement_timestamp() - INTERVAL '2 minutes'
          AND i.observed_at <= statement_timestamp() + INTERVAL '5 seconds'
    ) ELSE COALESCE(controller_version, '') END;
$$;

-- Only the main component existed before; other components' rows have no
-- place in the earlier schema.
DROP VIEW firmware_rollout_suppressed_device;

DELETE FROM device_firmware_deployment WHERE component <> '';
ALTER TABLE device_firmware_deployment
    DROP CONSTRAINT device_firmware_deployment_pkey,
    DROP COLUMN component,
    ADD PRIMARY KEY (device_id);

DROP INDEX idx_firmware_rollout_assignment_history;
DROP INDEX idx_one_active_rollout_per_pair;
DELETE FROM firmware_rollout WHERE component <> '';
ALTER TABLE firmware_rollout DROP COLUMN component;
CREATE UNIQUE INDEX idx_one_active_rollout_per_pair
    ON firmware_rollout(channel_id, release_channel_pair_key(manufacturer), release_channel_pair_key(model))
    WHERE status = 'active';
CREATE INDEX idx_firmware_rollout_assignment_history
    ON firmware_rollout(channel_id, release_channel_pair_key(manufacturer),
        release_channel_pair_key(model), assignment_generation, id DESC);

DROP INDEX idx_release_channel_firmware_pair;
DELETE FROM release_channel_firmware WHERE component <> '';
ALTER TABLE release_channel_firmware
    DROP CONSTRAINT release_channel_firmware_pkey,
    DROP COLUMN component,
    ADD PRIMARY KEY (channel_id, manufacturer, model);
CREATE UNIQUE INDEX idx_release_channel_firmware_pair
    ON release_channel_firmware(channel_id, release_channel_pair_key(manufacturer), release_channel_pair_key(model));

CREATE VIEW firmware_rollout_suppressed_device AS
SELECT channel_id, manufacturer_key, model_key, assignment_generation, device_id
FROM (
    SELECT r.channel_id,
           release_channel_pair_key(r.manufacturer) AS manufacturer_key,
           release_channel_pair_key(r.model) AS model_key,
           r.assignment_generation,
           rd.device_id,
           rd.halted_at,
           row_number() OVER (
               PARTITION BY r.channel_id, release_channel_pair_key(r.manufacturer), release_channel_pair_key(r.model),
                            r.assignment_generation, rd.device_id
               ORDER BY r.id DESC
           ) AS recency
    FROM firmware_rollout_device rd
    JOIN firmware_rollout r ON r.id = rd.rollout_id
) latest
WHERE recency = 1 AND halted_at IS NOT NULL;
