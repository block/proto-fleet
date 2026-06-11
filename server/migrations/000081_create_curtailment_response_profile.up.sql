CREATE TABLE curtailment_response_profile (
    id                          BIGSERIAL    PRIMARY KEY,
    org_id                      BIGINT       NOT NULL,
    profile_name                VARCHAR(64)  NOT NULL,
    site_id                     BIGINT       NOT NULL,

    -- Supported v1 response behavior. SITE_POWER_CAP remains reserved until
    -- the curtailment service implements residual site-cap semantics.
    mode                        TEXT         NOT NULL,
    strategy                    TEXT         NOT NULL DEFAULT 'LEAST_EFFICIENT_FIRST',
    level                       TEXT         NOT NULL DEFAULT 'FULL',
    priority                    TEXT         NOT NULL DEFAULT 'NORMAL',
    target_kw                   NUMERIC(12,3) NULL,
    tolerance_kw                NUMERIC(12,3) NULL,

    -- curtail_batch_size NULL means curtail all selected miners in scope.
    curtail_batch_size          INT          NULL,
    curtail_batch_interval_sec  INT          NOT NULL DEFAULT 0,
    restore_batch_size          INT          NOT NULL DEFAULT 50,
    restore_batch_interval_sec  INT          NOT NULL DEFAULT 5,
    include_maintenance         BOOLEAN      NOT NULL DEFAULT FALSE,
    force_include_maintenance   BOOLEAN      NOT NULL DEFAULT FALSE,

    created_at                  TIMESTAMPTZ  NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at                  TIMESTAMPTZ  NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT fk_curtailment_response_profile_org FOREIGN KEY (org_id)
        REFERENCES organization(id) ON DELETE RESTRICT,
    CONSTRAINT fk_curtailment_response_profile_site FOREIGN KEY (site_id, org_id)
        REFERENCES site(id, org_id) ON DELETE RESTRICT,
    CONSTRAINT uq_curtailment_response_profile_org_name UNIQUE (org_id, profile_name),

    CONSTRAINT ck_curtailment_response_profile_name_nonempty
        CHECK (btrim(profile_name) <> ''),
    CONSTRAINT ck_curtailment_response_profile_mode
        CHECK (mode IN ('FIXED_KW', 'FULL_FLEET')),
    CONSTRAINT ck_curtailment_response_profile_strategy
        CHECK (strategy IN ('LEAST_EFFICIENT_FIRST')),
    CONSTRAINT ck_curtailment_response_profile_level
        CHECK (level IN ('FULL')),
    CONSTRAINT ck_curtailment_response_profile_priority
        CHECK (priority IN ('NORMAL', 'EMERGENCY')),
    CONSTRAINT ck_curtailment_response_profile_target_positive
        CHECK (target_kw IS NULL OR target_kw > 0),
    CONSTRAINT ck_curtailment_response_profile_tolerance_nonnegative
        CHECK (tolerance_kw IS NULL OR tolerance_kw >= 0),
    CONSTRAINT ck_curtailment_response_profile_tolerance_less_than_target
        CHECK (tolerance_kw IS NULL OR (target_kw IS NOT NULL AND tolerance_kw < target_kw)),
    CONSTRAINT ck_curtailment_response_profile_mode_params
        CHECK (
            (mode = 'FIXED_KW' AND target_kw IS NOT NULL)
            OR
            (mode = 'FULL_FLEET' AND target_kw IS NULL AND tolerance_kw IS NULL)
        ),
    CONSTRAINT ck_curtailment_response_profile_curtail_batch_size
        CHECK (curtail_batch_size IS NULL OR (curtail_batch_size > 0 AND curtail_batch_size <= 10000)),
    CONSTRAINT ck_curtailment_response_profile_curtail_batch_interval
        CHECK (curtail_batch_interval_sec >= 0 AND curtail_batch_interval_sec <= 3600),
    CONSTRAINT ck_curtailment_response_profile_restore_batch_size
        CHECK (restore_batch_size > 0 AND restore_batch_size <= 10000),
    CONSTRAINT ck_curtailment_response_profile_restore_batch_interval
        CHECK (restore_batch_interval_sec >= 0 AND restore_batch_interval_sec <= 3600),
    CONSTRAINT ck_curtailment_response_profile_maintenance_consistency
        CHECK (include_maintenance = force_include_maintenance)
);

CREATE INDEX idx_curtailment_response_profile_org_site
    ON curtailment_response_profile (org_id, site_id);

CREATE TRIGGER update_curtailment_response_profile_updated_at
    BEFORE UPDATE ON curtailment_response_profile
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();
