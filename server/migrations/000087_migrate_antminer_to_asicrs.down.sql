-- Revert stock Antminers back to the removed Go antminer driver name for rollback.
UPDATE discovered_device
SET driver_name = 'antminer',
    port = CASE WHEN port = 80 THEN 4028 ELSE port END
WHERE driver_name = 'asicrs'
  AND deleted_at IS NULL
  AND (
      LOWER(COALESCE(manufacturer, '')) = 'bitmain'
      OR LOWER(COALESCE(model, '')) LIKE 'antminer%'
  );
