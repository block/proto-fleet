-- A firmware rollout targets exactly one miner model. The column is added
-- separately from 000087 because that migration may already be applied on
-- running environments. Existing prototype rows backfill to '' and are not
-- expected to survive; the service requires a non-empty model on create.
ALTER TABLE firmware_rollout ADD COLUMN miner_model TEXT NOT NULL DEFAULT '';
ALTER TABLE firmware_rollout ALTER COLUMN miner_model DROP DEFAULT;
