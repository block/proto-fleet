DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM curtailment_response_profile
        WHERE site_id IS NULL
    ) THEN
        RAISE EXCEPTION 'migration 000082 cannot restore site_id NOT NULL while org-wide curtailment response profiles exist';
    END IF;
END $$;

ALTER TABLE curtailment_response_profile
    ALTER COLUMN site_id SET NOT NULL;
