-- Migrate devices discovered by the removed Go antminer plugin to ASIC-RS.
UPDATE discovered_device
SET driver_name = 'asicrs',
    port = CASE WHEN port = 4028 THEN 80 ELSE port END
WHERE driver_name = 'antminer'
  AND deleted_at IS NULL;
