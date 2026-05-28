-- Drop curtailment:ingest. role_permission.permission_id is ON DELETE
-- RESTRICT (000052_create_permission_tables), so dependent rows must
-- be removed first. Boot reconciler re-upserts unless the catalog
-- entry is also reverted.
DELETE FROM role_permission WHERE permission_id = (SELECT id FROM permission WHERE key = 'curtailment:ingest');
DELETE FROM permission WHERE key = 'curtailment:ingest';
