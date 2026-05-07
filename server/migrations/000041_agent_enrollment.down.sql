-- Drop agent-owned api_keys before restoring user_id NOT NULL; otherwise the
-- ALTER below fails on rows created by ConfirmAgent.
DELETE FROM api_key WHERE user_id IS NULL;

DROP TABLE IF EXISTS agent_session;
DROP TABLE IF EXISTS agent_auth_challenge;
DROP TABLE IF EXISTS pending_enrollment;

DROP INDEX IF EXISTS idx_api_key_agent_id;
DROP INDEX IF EXISTS idx_api_key_user_id;
CREATE INDEX idx_api_key_user_id ON api_key(user_id);

ALTER TABLE api_key
    DROP CONSTRAINT IF EXISTS ck_api_key_subject,
    DROP CONSTRAINT IF EXISTS fk_api_key_agent,
    DROP COLUMN IF EXISTS subject_kind,
    DROP COLUMN IF EXISTS agent_id,
    ALTER COLUMN user_id SET NOT NULL;
