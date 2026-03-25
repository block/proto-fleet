CREATE TABLE activity_log (
    id              BIGSERIAL PRIMARY KEY,
    event_id        UUID NOT NULL UNIQUE,
    event_category  TEXT NOT NULL,
    event_type      TEXT NOT NULL,
    description     TEXT NOT NULL,
    result          TEXT NOT NULL DEFAULT 'success',
    error_message   TEXT,
    scope_type      TEXT,
    scope_label     TEXT,
    scope_count     INT,
    actor_type      TEXT NOT NULL DEFAULT 'user',
    user_id         TEXT,
    username        TEXT,
    organization_id BIGINT,
    metadata        JSONB,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT fk_activity_log_organization FOREIGN KEY (organization_id)
        REFERENCES organization(id) ON DELETE RESTRICT
);

CREATE INDEX idx_activity_log_org_created ON activity_log(organization_id, created_at DESC, id DESC);
