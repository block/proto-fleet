-- Index supports zone-based miner filtering.
-- The miner list query joins device_set_membership -> device_set_rack and filters
-- on zone; this index makes the value-side scan fast on large fleets.
-- Org scoping is enforced via the membership join (device_set_membership.org_id),
-- so a single-column index on zone is sufficient.
CREATE INDEX idx_device_set_rack_zone ON device_set_rack(zone);
