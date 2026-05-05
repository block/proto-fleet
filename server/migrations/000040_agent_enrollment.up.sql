ALTER TABLE api_key
    ALTER COLUMN user_id DROP NOT NULL,
    ADD COLUMN agent_id BIGINT NULL,
    ADD COLUMN subject_kind VARCHAR(16) NOT NULL DEFAULT 'user',
    ADD CONSTRAINT fk_api_key_agent FOREIGN KEY (agent_id, organization_id)
        REFERENCES agent(id, org_id) ON DELETE CASCADE,
    ADD CONSTRAINT ck_api_key_subject CHECK (
        (subject_kind = 'user'  AND user_id  IS NOT NULL AND agent_id IS NULL) OR
        (subject_kind = 'agent' AND user_id  IS NULL     AND agent_id IS NOT NULL)
    );

DROP INDEX IF EXISTS idx_api_key_user_id;
CREATE INDEX idx_api_key_user_id  ON api_key(user_id)  WHERE user_id  IS NOT NULL;
CREATE INDEX idx_api_key_agent_id ON api_key(agent_id) WHERE agent_id IS NOT NULL;

-- pending_enrollment carries operator-issued bootstrap codes. Plaintext is
-- shown to the operator once at creation time; only the SHA-256 hash is
-- persisted. State machine:
--   PENDING               -> created by operator, agent has not registered yet
--   AWAITING_CONFIRMATION -> agent called Register; agent_id set, fingerprint visible
--   CONFIRMED             -> operator confirmed fingerprint; api_key issued
--   EXPIRED               -> TTL passed without progress (set by sweep)
--   CANCELLED             -> operator cancelled before Register

CREATE TABLE pending_enrollment (
    id           BIGSERIAL PRIMARY KEY,
    code_hash    TEXT NOT NULL,
    org_id       BIGINT NOT NULL,
    created_by   BIGINT NOT NULL,
    agent_id     BIGINT NULL,
    status       VARCHAR(32) NOT NULL DEFAULT 'PENDING',
    expires_at   TIMESTAMPTZ NOT NULL,
    consumed_at  TIMESTAMPTZ NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT uq_pending_enrollment_code_hash UNIQUE (code_hash),
    CONSTRAINT fk_pending_enrollment_org FOREIGN KEY (org_id)
        REFERENCES organization(id) ON DELETE CASCADE,
    CONSTRAINT fk_pending_enrollment_user FOREIGN KEY (created_by)
        REFERENCES "user"(id) ON DELETE CASCADE,
    CONSTRAINT fk_pending_enrollment_agent FOREIGN KEY (agent_id, org_id)
        REFERENCES agent(id, org_id) ON DELETE CASCADE,
    CONSTRAINT ck_pending_enrollment_status CHECK (
        status IN ('PENDING', 'AWAITING_CONFIRMATION', 'CONFIRMED', 'EXPIRED', 'CANCELLED')
    ),
    -- agent_id is bound at Register; pre-Register the row can sit in PENDING,
    -- CANCELLED, or EXPIRED with no agent. Post-Register it can land in any
    -- terminal state (CANCELLED handles operator revoke of an unconfirmed
    -- agent; EXPIRED handles abandoned awaiting-confirmation rows).
    CONSTRAINT ck_pending_enrollment_agent_states CHECK (
        (agent_id IS NULL     AND status IN ('PENDING', 'CANCELLED', 'EXPIRED')) OR
        (agent_id IS NOT NULL AND status IN ('AWAITING_CONFIRMATION', 'CONFIRMED', 'CANCELLED', 'EXPIRED'))
    )
);

CREATE INDEX idx_pending_enrollment_org_status ON pending_enrollment(org_id, status);
CREATE INDEX idx_pending_enrollment_expires_at ON pending_enrollment(expires_at);

-- agent_auth_challenge: short-TTL nonces used during BeginAuthHandshake.
-- Atomic DELETE ... RETURNING on consume gives replay safety without a
-- consumed_at column.

CREATE TABLE agent_auth_challenge (
    challenge   BYTEA PRIMARY KEY,
    agent_id    BIGINT NOT NULL,
    expires_at  TIMESTAMPTZ NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT fk_agent_auth_challenge_agent FOREIGN KEY (agent_id)
        REFERENCES agent(id) ON DELETE CASCADE
);

CREATE INDEX idx_agent_auth_challenge_expires_at ON agent_auth_challenge(expires_at);

-- agent_session: short-lived bearer tokens minted by CompleteAuthHandshake.
-- Stored hashed (SHA-256); plaintext is returned once.

CREATE TABLE agent_session (
    token_hash  TEXT PRIMARY KEY,
    agent_id    BIGINT NOT NULL,
    expires_at  TIMESTAMPTZ NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT fk_agent_session_agent FOREIGN KEY (agent_id)
        REFERENCES agent(id) ON DELETE CASCADE
);

CREATE INDEX idx_agent_session_agent_id ON agent_session(agent_id);
CREATE INDEX idx_agent_session_expires_at ON agent_session(expires_at);
