-- name: GetLLMConfig :one
SELECT
    organization_id,
    harness,
    provider,
    api_key_encrypted,
    base_url,
    model,
    temperature,
    goose_base_url,
    goose_secret_encrypted,
    created_at,
    updated_at
FROM llm_config
WHERE organization_id = $1;

-- name: UpsertLLMConfig :one
INSERT INTO llm_config (
    organization_id,
    harness,
    provider,
    api_key_encrypted,
    base_url,
    model,
    temperature,
    goose_base_url,
    goose_secret_encrypted
) VALUES (
    sqlc.arg('organization_id'),
    sqlc.arg('harness'),
    sqlc.arg('provider'),
    sqlc.arg('api_key_encrypted'),
    sqlc.arg('base_url'),
    sqlc.arg('model'),
    sqlc.arg('temperature'),
    sqlc.arg('goose_base_url'),
    sqlc.arg('goose_secret_encrypted')
)
ON CONFLICT (organization_id) DO UPDATE SET
    harness = EXCLUDED.harness,
    provider = EXCLUDED.provider,
    api_key_encrypted = EXCLUDED.api_key_encrypted,
    base_url = EXCLUDED.base_url,
    model = EXCLUDED.model,
    temperature = EXCLUDED.temperature,
    goose_base_url = EXCLUDED.goose_base_url,
    goose_secret_encrypted = EXCLUDED.goose_secret_encrypted,
    updated_at = CURRENT_TIMESTAMP
RETURNING
    organization_id,
    harness,
    provider,
    api_key_encrypted,
    base_url,
    model,
    temperature,
    goose_base_url,
    goose_secret_encrypted,
    created_at,
    updated_at;
