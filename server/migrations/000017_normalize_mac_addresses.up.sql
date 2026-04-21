-- Normalize historical MAC addresses to colon-separated uppercase format (AA:BB:CC:DD:EE:FF).
-- This ensures reconciliation queries can use a simple equality match instead of REGEXP_REPLACE.
UPDATE device
SET mac_address = UPPER(
    SUBSTRING(REGEXP_REPLACE(mac_address, '[^0-9A-Fa-f]', '', 'g') FROM 1 FOR 2) || ':' ||
    SUBSTRING(REGEXP_REPLACE(mac_address, '[^0-9A-Fa-f]', '', 'g') FROM 3 FOR 2) || ':' ||
    SUBSTRING(REGEXP_REPLACE(mac_address, '[^0-9A-Fa-f]', '', 'g') FROM 5 FOR 2) || ':' ||
    SUBSTRING(REGEXP_REPLACE(mac_address, '[^0-9A-Fa-f]', '', 'g') FROM 7 FOR 2) || ':' ||
    SUBSTRING(REGEXP_REPLACE(mac_address, '[^0-9A-Fa-f]', '', 'g') FROM 9 FOR 2) || ':' ||
    SUBSTRING(REGEXP_REPLACE(mac_address, '[^0-9A-Fa-f]', '', 'g') FROM 11 FOR 2)
)
WHERE mac_address IS NOT NULL
  AND mac_address != ''
  AND LENGTH(REGEXP_REPLACE(mac_address, '[^0-9A-Fa-f]', '', 'g')) = 12;

-- Partial index for MAC-based reconciliation lookups.
CREATE INDEX idx_device_org_mac_active ON device (org_id, mac_address) WHERE deleted_at IS NULL;
