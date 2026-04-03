CREATE TABLE api_key (
    id              BIGSERIAL PRIMARY KEY,
    key_id          VARCHAR(36) NOT NULL,
    name            VARCHAR(255) NOT NULL,
    prefix          VARCHAR(12) NOT NULL,
    key_hash        TEXT NOT NULL,
    user_id         BIGINT NOT NULL,
    organization_id BIGINT NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    expires_at      TIMESTAMPTZ NULL,
    revoked_at      TIMESTAMPTZ NULL,
    last_used_at    TIMESTAMPTZ NULL,

    CONSTRAINT uq_api_key_key_id UNIQUE (key_id),
    CONSTRAINT uq_api_key_prefix_org UNIQUE (prefix, organization_id),
    CONSTRAINT uq_api_key_key_hash UNIQUE (key_hash),
    CONSTRAINT fk_api_key_user FOREIGN KEY (user_id)
        REFERENCES "user"(id) ON DELETE CASCADE,
    CONSTRAINT fk_api_key_organization FOREIGN KEY (organization_id)
        REFERENCES organization(id) ON DELETE CASCADE
);

CREATE INDEX idx_api_key_user_id ON api_key(user_id);
CREATE INDEX idx_api_key_organization_id ON api_key(organization_id);
