-- Pilot-then-rest rollouts: a rollout carries a method and, for the pilot
-- method, a staged lifecycle (pilot -> awaiting_review -> rest) with a
-- snapshotted pilot cohort in firmware_rollout_device.

ALTER TABLE firmware_rollout
    ADD COLUMN method TEXT NOT NULL DEFAULT 'immediate', -- immediate | pilot
    ADD COLUMN stage TEXT NOT NULL DEFAULT 'rest',       -- pilot | awaiting_review | rest
    ADD COLUMN pilot_count INT NOT NULL DEFAULT 0;

-- Cohort rows are now inserted at rollout creation for pilot rollouts,
-- before any update command is sent.
ALTER TABLE firmware_rollout_device
    ADD COLUMN cohort TEXT NOT NULL DEFAULT 'rest',      -- pilot | rest
    ALTER COLUMN update_sent_at DROP NOT NULL,
    ALTER COLUMN update_sent_at DROP DEFAULT;
