DROP INDEX IF EXISTS idx_miner_channel_membership_miner_channel;
DROP TABLE IF EXISTS miner_channel_membership;

DROP TRIGGER IF EXISTS update_miner_channel_updated_at ON miner_channel;
DROP INDEX IF EXISTS idx_miner_channel_org_state;
DROP INDEX IF EXISTS idx_miner_channel_expiry;
DROP INDEX IF EXISTS idx_miner_channel_owner_active;
DROP INDEX IF EXISTS uq_miner_channel_active_label_per_org;
DROP INDEX IF EXISTS uq_miner_channel_idempotency;
DROP INDEX IF EXISTS uq_miner_channel_one_default_per_org;
DROP TABLE IF EXISTS miner_channel;
