-- Fix device types that were incorrectly stored as 'asic' due to race condition
-- in multi-plugin discovery. The type should match the plugin's miner type.

-- Fix Antminer devices: manufacturer contains 'bitmain' (case-insensitive)
-- or model starts with 'antminer' (case-insensitive)
UPDATE discovered_device
SET type = 'antminer'
WHERE type = 'asic'
  AND (
    LOWER(manufacturer) LIKE '%bitmain%'
    OR LOWER(model) LIKE 'antminer%'
  );

-- Fix Proto devices: manufacturer contains 'proto' (case-insensitive)
UPDATE discovered_device
SET type = 'proto'
WHERE type = 'asic'
  AND LOWER(manufacturer) LIKE '%proto%';
