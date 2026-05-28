-- Drop curtailment:ingest. Cascades to role_permission via FK. Boot
-- reconciler re-upserts unless the catalog entry is also reverted.
DELETE FROM permission WHERE key = 'curtailment:ingest';
