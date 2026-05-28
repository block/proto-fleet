-- Seed curtailment:ingest into the catalog. The boot reconciler's
-- upsertCatalog adds it lazily, but tests bypass the boot path and seed
-- fresh orgs directly. ON CONFLICT keeps both paths idempotent.
--
-- No role_permission backfill for existing-org ADMINs. The intended
-- caller is a provider service account on a custom role; interactive
-- admins do not invoke this RPC. SUPER_ADMIN auto-gains the permission
-- on next boot via ReconcileFull.
INSERT INTO permission (key, description) VALUES
    ('curtailment:ingest', 'Accept curtailment dispatch signals from external providers (QSE bridge, aggregator, OpenADR VTN).')
ON CONFLICT (key) DO NOTHING;
