ALTER TABLE container_inventory ADD COLUMN hashboard_firmware_version text NOT NULL DEFAULT '';

-- Controller software and hashboard firmware have independent versions.
-- Unknown, mixed, and stale board observations must never prove convergence.
CREATE FUNCTION release_channel_firmware_version(device_id bigint, device_class integer, controller_version text)
RETURNS text LANGUAGE sql STABLE AS $$
    SELECT CASE WHEN device_class = 2 THEN COALESCE((
        SELECT i.hashboard_firmware_version FROM container_inventory i
        WHERE i.device_id = $1 AND i.observed_at > now() - INTERVAL '2 minutes'
          AND i.observed_at <= now() + INTERVAL '5 seconds'
    ), '') ELSE COALESCE(controller_version, '') END;
$$;
