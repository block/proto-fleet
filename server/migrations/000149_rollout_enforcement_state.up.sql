-- Assignment lineage survives even when every member already matches and no
-- rollout is created. The next changed assignment captures the current pair;
-- a later reconciliation rollout inherits that saved predecessor.
ALTER TABLE release_channel_firmware
    ADD COLUMN previous_firmware_checksum TEXT NOT NULL DEFAULT '',
    ADD COLUMN previous_firmware_version TEXT NOT NULL DEFAULT '';

WITH latest AS (
    SELECT DISTINCT ON (channel_id, release_channel_pair_key(manufacturer), release_channel_pair_key(model), assignment_generation)
           channel_id, manufacturer, model, assignment_generation,
           previous_firmware_checksum, previous_firmware_version
    FROM firmware_rollout
    ORDER BY channel_id, release_channel_pair_key(manufacturer), release_channel_pair_key(model), assignment_generation, id DESC
)
UPDATE release_channel_firmware assignment
SET previous_firmware_checksum = latest.previous_firmware_checksum,
    previous_firmware_version = latest.previous_firmware_version
FROM latest
WHERE assignment.channel_id = latest.channel_id
  AND release_channel_pair_key(assignment.manufacturer) = release_channel_pair_key(latest.manufacturer)
  AND release_channel_pair_key(assignment.model) = release_channel_pair_key(latest.model)
  AND assignment.assignment_generation = latest.assignment_generation
  AND assignment.firmware_checksum <> '';

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
