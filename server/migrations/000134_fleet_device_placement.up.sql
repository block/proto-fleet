-- Owner-privilege view for scoped alert-rule SQL: grafana_ro resolves current placement without SELECT on device (credentials FKs) or device_set tables.
-- One row per device x group membership (rack columns repeat across a device's group rows), so consume as a semijoin — never for counting.
CREATE VIEW fleet_device_placement AS
SELECT
    d.org_id,
    d.device_identifier AS device_id,
    d.site_id,
    -- Rack-derived building wins: device.building_id is only cascade-written today (see 000091).
    COALESCE(dcr.building_id, d.building_id) AS building_id,
    -- rs.id, not rm.device_set_id: null when the rack set is soft-deleted.
    rs.id AS rack_id,
    gm.device_set_id AS group_id
FROM device d
-- Membership org_id is denormalized and always equals the device's org; the redundant predicate lets both joins ride idx_dcm_org_device instead of scanning global memberships (the group side has no device_id-leading index).
LEFT JOIN device_set_membership rm ON rm.org_id = d.org_id AND rm.device_id = d.id AND rm.device_set_type = 'rack'
LEFT JOIN device_set rs ON rs.id = rm.device_set_id AND rs.deleted_at IS NULL
LEFT JOIN device_set_rack dcr ON dcr.device_set_id = rs.id
LEFT JOIN device_set_membership gm ON gm.org_id = d.org_id AND gm.device_id = d.id AND gm.device_set_type = 'group'
    AND EXISTS (SELECT 1 FROM device_set gs WHERE gs.id = gm.device_set_id AND gs.deleted_at IS NULL)
WHERE d.deleted_at IS NULL;
