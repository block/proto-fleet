-- Imported intent must survive a rollback that cannot reconstruct its provenance.
DO $$
BEGIN
    LOCK TABLE pool_configuration_assignment, pool_configuration_observation IN ACCESS EXCLUSIVE MODE;
    IF EXISTS (SELECT 1 FROM pool_configuration_assignment WHERE operation_id IS NULL) THEN
        RAISE EXCEPTION 'cannot roll back pool adoption while imported assignments lack reviewed operations';
    END IF;
    DROP TABLE pool_configuration_observation;
    ALTER TABLE pool_configuration_assignment DROP CONSTRAINT chk_pool_configuration_assignment_origin;
    ALTER TABLE pool_configuration_assignment DROP COLUMN adopted_at;
    ALTER TABLE pool_configuration_assignment ALTER COLUMN operation_id SET NOT NULL;
END;
$$;
