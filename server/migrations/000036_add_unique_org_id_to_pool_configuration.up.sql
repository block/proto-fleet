-- TODO Drop on https://linear.app/squareup/issue/DASH-568/refactor-pool-configuration-to-multiple-pools-per-org-approach
ALTER TABLE pool_configuration
    ADD CONSTRAINT uk_pool_configuration_org_id UNIQUE (org_id);
