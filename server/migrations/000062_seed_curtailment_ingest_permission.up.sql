-- Seed curtailment:ingest into the catalog. The boot reconciler's
-- upsertCatalog adds it lazily, but tests bypass the boot path and seed
-- fresh orgs directly. ON CONFLICT keeps both paths idempotent.
INSERT INTO permission (key, description) VALUES
    ('curtailment:ingest', 'Accept curtailment dispatch signals from external providers (QSE bridge, aggregator, OpenADR VTN).')
ON CONFLICT (key) DO NOTHING;
