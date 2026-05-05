DROP TABLE IF EXISTS agent_device;

DROP TRIGGER IF EXISTS update_agent_updated_at ON agent;

DROP INDEX IF EXISTS idx_agent_org_id;

DROP TABLE IF EXISTS agent;

ALTER TABLE device
    DROP CONSTRAINT IF EXISTS uq_device_id_org_id;
