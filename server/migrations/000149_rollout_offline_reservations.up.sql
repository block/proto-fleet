-- Dispatch reservations outlive a rollout's active phase and retry bookkeeping.
-- Keep observations separately from rollout targets: they are channel safety
-- state, not changes to a completed rollout's history or public revision.
CREATE TABLE firmware_rollout_reservation (
    channel_id BIGINT NOT NULL REFERENCES release_channel(id) ON DELETE CASCADE,
    device_id BIGINT NOT NULL REFERENCES device(id) ON DELETE CASCADE,
    batch_uuid VARCHAR(36) NOT NULL,
    observed_offline BOOLEAN NOT NULL DEFAULT false,
    PRIMARY KEY (channel_id, device_id, batch_uuid)
);

-- Existing dispatches must continue to reserve while their command is live.
-- Previous offline observations cannot be reconstructed, so retain the
-- reservation conservatively until a new offline observation or termination.
INSERT INTO firmware_rollout_reservation (channel_id, device_id, batch_uuid)
SELECT DISTINCT r.channel_id, rd.device_id, rd.last_dispatched_batch_uuid
FROM firmware_rollout_device rd
JOIN firmware_rollout r ON r.id = rd.rollout_id
WHERE rd.last_dispatched_batch_uuid IS NOT NULL;
