-- Keep coverage and deletion fences: returning to independent raw retention is
-- unsafe. Older binaries reject this checkpoint; preserve the database and fix
-- forward with a release that includes this migration.
DO $$ BEGIN
 RAISE EXCEPTION 'telemetry recovery protects retained evidence; downgrade is unsupported: preserve the database and fix forward with a release that includes this migration';
END $$;
