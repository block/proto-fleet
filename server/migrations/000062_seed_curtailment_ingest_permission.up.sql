-- Seed the curtailment:ingest permission row. The catalog reconciler at
-- boot would otherwise upsert this lazily, but tests bypass the boot
-- reconciler and seed fresh orgs directly via SeedOrgBuiltins. Without
-- the row present at migration time, assignSeedPermissions for
-- SUPER_ADMIN (whose SeedPermissions = AllPermissions()) fails with
-- "seed permissions [curtailment:ingest] not in catalog".
--
-- ON CONFLICT keeps this idempotent against the boot reconciler's
-- upsertCatalog path so the order is not load-bearing.
INSERT INTO permission (key, description) VALUES
    ('curtailment:ingest', 'Accept curtailment dispatch signals from external providers (QSE bridge, aggregator, OpenADR VTN).')
ON CONFLICT (key) DO NOTHING;
