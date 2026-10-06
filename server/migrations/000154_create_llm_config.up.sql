CREATE TABLE llm_config (
    organization_id         BIGINT PRIMARY KEY REFERENCES organization(id) ON DELETE CASCADE,
    harness                 TEXT NOT NULL CHECK (harness IN ('native', 'goose')),
    provider                TEXT NOT NULL CHECK (provider IN ('openai', 'anthropic', 'ollama', 'custom')),
    api_key_encrypted       TEXT NOT NULL DEFAULT '',
    base_url                TEXT NOT NULL DEFAULT '',
    model                   TEXT NOT NULL,
    temperature             DOUBLE PRECISION NOT NULL DEFAULT 0.2 CHECK (temperature >= 0 AND temperature <= 1),
    goose_base_url          TEXT NOT NULL DEFAULT '',
    goose_secret_encrypted  TEXT NOT NULL DEFAULT '',
    created_at              TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at              TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

COMMENT ON TABLE llm_config IS 'Organization-scoped agent harness and encrypted BYOLLM provider configuration.';
COMMENT ON COLUMN llm_config.api_key_encrypted IS 'AES-GCM ciphertext produced by the fleet service master key; never returned over RPC.';
COMMENT ON COLUMN llm_config.goose_secret_encrypted IS 'Encrypted authentication secret for a future remote Goose ACP adapter.';
