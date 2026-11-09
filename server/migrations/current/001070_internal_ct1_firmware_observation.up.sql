-- requires-shared: 1064

ALTER TABLE container_inventory
    ADD COLUMN hashboard_firmware_version text NOT NULL DEFAULT '',
    ADD COLUMN hashboard_firmware_mixed boolean NOT NULL DEFAULT false;

-- Controller software and hashboard firmware have independent versions.
-- NULL means no fresh, complete board evidence: it can neither prove
-- convergence nor show drift. '' means fresh evidence of mixed versions.
CREATE FUNCTION release_channel_firmware_version(device_id bigint, device_class integer, controller_version text)
RETURNS text LANGUAGE sql STABLE PARALLEL SAFE AS $$
    SELECT CASE WHEN device_class = 2 THEN (
        SELECT CASE WHEN i.hashboard_firmware_mixed THEN '' ELSE NULLIF(i.hashboard_firmware_version, '') END
        FROM container_inventory i
        WHERE i.device_id = $1 AND i.observed_at > statement_timestamp() - INTERVAL '2 minutes'
          AND i.observed_at <= statement_timestamp() + INTERVAL '5 seconds'
    ) ELSE COALESCE(controller_version, '') END;
$$;
