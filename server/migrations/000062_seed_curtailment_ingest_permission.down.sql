-- Drop the curtailment:ingest permission row. role_permission rows
-- referencing it cascade through the FK on permission_id; SUPER_ADMIN
-- and ADMIN bindings disappear with the row, matching the inverse of
-- the .up step. Boot reconciler re-upserts on the next boot if the
-- catalog still declares the key — this down migration is only useful
-- after also reverting the catalog entry in catalog.go.
DELETE FROM permission WHERE key = 'curtailment:ingest';
