-- Keep coverage and deletion fences: returning to independent raw retention is
-- unsafe. Application rollback can leave this additive schema/jobs in place.
DO $$ BEGIN
 RAISE EXCEPTION 'telemetry recovery protects retained evidence; roll back the application or disable maintenance jobs and fix forward';
END $$;
