-- Keep the refusal guard and destructive DDL in one statement so standalone
-- execution cannot continue dropping data after the guard raises an exception.
DO $$
BEGIN
    LOCK TABLE hosted_account_pool, hosted_account_pool_revision, hosted_account_pool_profile IN ACCESS EXCLUSIVE MODE;
    IF EXISTS (SELECT 1 FROM hosted_account_pool)
       OR EXISTS (SELECT 1 FROM hosted_account_pool_revision)
       OR EXISTS (SELECT 1 FROM hosted_account_pool_profile) THEN
        RAISE EXCEPTION 'preserve or remove pool configuration data before rolling back hosted account pools';
    END IF;

    DROP TABLE hosted_account_pool_profile;
    DROP TABLE hosted_account_pool_revision;
    DROP FUNCTION keep_hosted_account_pool_revision_immutable();
    DROP FUNCTION valid_hosted_account_pool_slots(JSONB);
    DROP TABLE hosted_account_pool;
END;
$$;
