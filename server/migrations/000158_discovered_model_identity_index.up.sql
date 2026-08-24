CREATE INDEX CONCURRENTLY idx_discovered_device_model_identity_observed
    ON discovered_device(org_id, model_identity_observed_at, id)
    WHERE deleted_at IS NULL
      AND model_identity_observed_at IS NOT NULL;
