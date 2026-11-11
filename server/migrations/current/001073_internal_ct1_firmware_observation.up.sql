-- requires-shared: 1064

ALTER TABLE container_inventory
    ADD COLUMN hashboard_firmware_version text NOT NULL DEFAULT '',
    ADD COLUMN hashboard_firmware_mixed boolean NOT NULL DEFAULT false;

CREATE FUNCTION release_channel_firmware_version(device_id bigint, device_class integer, controller_version text)
RETURNS SETOF text LANGUAGE sql STABLE PARALLEL SAFE ROWS 1 AS $$
    SELECT CASE WHEN device_class = 2 THEN (
        SELECT CASE WHEN i.hashboard_firmware_mixed THEN '' ELSE NULLIF(i.hashboard_firmware_version, '') END
        FROM container_inventory i
        WHERE i.device_id = $1 AND i.observed_at > statement_timestamp() - INTERVAL '2 minutes'
          AND i.observed_at <= statement_timestamp() + INTERVAL '5 seconds'
    ) ELSE COALESCE(controller_version, '') END;
$$;
