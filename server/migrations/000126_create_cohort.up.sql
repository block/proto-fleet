CREATE TABLE cohort (
    id                    BIGSERIAL    PRIMARY KEY,
    org_id                BIGINT       NOT NULL,
    label                 TEXT         NOT NULL,
    is_default            BOOLEAN      NOT NULL DEFAULT FALSE,
    owner_user_id         BIGINT       NULL,
    owner_username        TEXT         NULL,
    expires_at            TIMESTAMPTZ  NULL,
    desired_config_jsonb  JSONB        NULL,
    state                 TEXT         NOT NULL DEFAULT 'active',
    purpose               TEXT         NOT NULL,
    source_actor_type     TEXT         NOT NULL,
    source_actor_id       TEXT         NULL,
    idempotency_key       TEXT         NULL,
    created_at            TIMESTAMPTZ  NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at            TIMESTAMPTZ  NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT fk_cohort_org FOREIGN KEY (org_id)
        REFERENCES organization(id) ON DELETE RESTRICT,
    CONSTRAINT fk_cohort_owner FOREIGN KEY (owner_user_id)
        REFERENCES "user"(id),
    CONSTRAINT ck_cohort_state CHECK (state IN ('active', 'released')),
    CONSTRAINT ck_cohort_label_nonempty CHECK (length(trim(label)) > 0),
    CONSTRAINT ck_cohort_purpose_nonempty CHECK (length(trim(purpose)) > 0),
    CONSTRAINT ck_cohort_source_actor_type
        CHECK (source_actor_type IN ('user', 'api_key', 'scheduler', 'cohort')),
    CONSTRAINT ck_cohort_source_actor_id_nonempty
        CHECK (source_actor_id IS NULL OR source_actor_id <> ''),
    CONSTRAINT ck_cohort_idempotency_key_nonempty
        CHECK (idempotency_key IS NULL OR idempotency_key <> '')
);

CREATE UNIQUE INDEX uq_cohort_one_default_per_org
    ON cohort (org_id)
    WHERE is_default;

CREATE UNIQUE INDEX uq_cohort_idempotency
    ON cohort (org_id, idempotency_key)
    WHERE idempotency_key IS NOT NULL;

CREATE UNIQUE INDEX uq_cohort_active_label_per_org
    ON cohort (org_id, lower(trim(label)))
    WHERE state = 'active' AND is_default = FALSE;

CREATE INDEX idx_cohort_owner_active
    ON cohort (org_id, owner_user_id)
    WHERE state = 'active';

CREATE INDEX idx_cohort_expiry
    ON cohort (expires_at)
    WHERE state = 'active' AND expires_at IS NOT NULL;

CREATE INDEX idx_cohort_org_state
    ON cohort (org_id, state, updated_at DESC);

CREATE TRIGGER update_cohort_updated_at
    BEFORE UPDATE ON cohort
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

CREATE TABLE cohort_membership (
    cohort_id         BIGINT      NOT NULL,
    org_id            BIGINT      NOT NULL,
    device_identifier VARCHAR     NOT NULL,
    added_at          TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT fk_cohort_membership_cohort FOREIGN KEY (cohort_id)
        REFERENCES cohort(id) ON DELETE CASCADE,
    CONSTRAINT uq_cohort_membership_one_per_device UNIQUE (org_id, device_identifier)
);

CREATE INDEX idx_cohort_membership_cohort
    ON cohort_membership (cohort_id);

INSERT INTO cohort (
    org_id,
    label,
    is_default,
    state,
    purpose,
    source_actor_type
)
SELECT
    id,
    'Default',
    TRUE,
    'active',
    'Default cohort',
    'scheduler'
FROM organization
WHERE deleted_at IS NULL
ON CONFLICT DO NOTHING;
