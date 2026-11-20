-- requires-shared: 1074

-- Release channels assign firmware per component of a model. The empty
-- component is the model's main firmware, which for a CT1 container is its
-- hashboard firmware; 'psu' is its PSU firmware. Assignments, their
-- generations, rollouts and managed-deployment provenance are kept per
-- component, so two components of one model are assigned, rolled out and
-- verified independently.

ALTER TABLE release_channel_firmware
    ADD COLUMN component TEXT NOT NULL DEFAULT '',
    DROP CONSTRAINT release_channel_firmware_pkey,
    ADD PRIMARY KEY (channel_id, manufacturer, model, component);

DROP INDEX idx_release_channel_firmware_pair;
CREATE UNIQUE INDEX idx_release_channel_firmware_pair
    ON release_channel_firmware(channel_id, release_channel_pair_key(manufacturer), release_channel_pair_key(model), component);

ALTER TABLE firmware_rollout
    ADD COLUMN component TEXT NOT NULL DEFAULT '';

-- At most one active rollout per (channel, manufacturer, model, component).
DROP INDEX idx_one_active_rollout_per_pair;
CREATE UNIQUE INDEX idx_one_active_rollout_per_pair
    ON firmware_rollout(channel_id, release_channel_pair_key(manufacturer), release_channel_pair_key(model), component)
    WHERE status = 'active';

DROP INDEX idx_firmware_rollout_assignment_history;
CREATE INDEX idx_firmware_rollout_assignment_history
    ON firmware_rollout(channel_id, release_channel_pair_key(manufacturer),
        release_channel_pair_key(model), component, assignment_generation, id DESC);

-- One provenance row per device and component. A finished firmware command
-- serializes through, and invalidates, only its own component's row.
ALTER TABLE device_firmware_deployment
    ADD COLUMN component TEXT NOT NULL DEFAULT '',
    DROP CONSTRAINT device_firmware_deployment_pkey,
    ADD PRIMARY KEY (device_id, component);

-- A view keeps its column order, so component is appended.
CREATE OR REPLACE VIEW firmware_rollout_suppressed_device AS
SELECT channel_id, manufacturer_key, model_key, assignment_generation, device_id, component
FROM (
    SELECT r.channel_id,
           release_channel_pair_key(r.manufacturer) AS manufacturer_key,
           release_channel_pair_key(r.model) AS model_key,
           r.assignment_generation,
           rd.device_id,
           r.component,
           rd.halted_at,
           row_number() OVER (
               PARTITION BY r.channel_id, release_channel_pair_key(r.manufacturer), release_channel_pair_key(r.model),
                            r.component, r.assignment_generation, rd.device_id
               ORDER BY r.id DESC
           ) AS recency
    FROM firmware_rollout_device rd
    JOIN firmware_rollout r ON r.id = rd.rollout_id
) latest
WHERE recency = 1 AND halted_at IS NOT NULL;

-- The version a device reports for one component. A container reports its
-- hashboard firmware for the main component and its PSU firmware for 'psu',
-- each from fresh inventory: NULL when unknown, '' when versions differ. Any
-- other device reports its controller version for the main component and
-- nothing for another.
DROP FUNCTION release_channel_firmware_version(bigint, integer, text);
CREATE FUNCTION release_channel_firmware_version(device_id bigint, device_class integer, controller_version text, component text)
RETURNS SETOF text LANGUAGE sql STABLE PARALLEL SAFE ROWS 1 AS $$
    SELECT CASE
        WHEN component = 'psu' THEN (
            SELECT CASE WHEN i.psu_firmware_mixed THEN '' ELSE NULLIF(i.psu_firmware_version, '') END
            FROM container_inventory i
            WHERE device_class = 2 AND i.device_id = $1
              AND i.observed_at > statement_timestamp() - INTERVAL '2 minutes'
              AND i.observed_at <= statement_timestamp() + INTERVAL '5 seconds'
        )
        WHEN component <> '' THEN NULL
        WHEN device_class = 2 THEN (
            SELECT CASE WHEN i.hashboard_firmware_mixed THEN '' ELSE NULLIF(i.hashboard_firmware_version, '') END
            FROM container_inventory i
            WHERE i.device_id = $1 AND i.observed_at > statement_timestamp() - INTERVAL '2 minutes'
              AND i.observed_at <= statement_timestamp() + INTERVAL '5 seconds'
        )
        ELSE COALESCE(controller_version, '')
    END;
$$;
