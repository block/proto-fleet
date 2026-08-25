-- Rollout lanes: containers of miners with per-model firmware enforcement.

CREATE TABLE rollout_lane (
    id BIGSERIAL PRIMARY KEY,
    org_id BIGINT NOT NULL REFERENCES organization(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (org_id, name)
);

-- Lane membership. The primary key on device_id enforces that a miner
-- belongs to at most one lane; moving a miner is an upsert.
CREATE TABLE rollout_lane_member (
    device_id BIGINT PRIMARY KEY REFERENCES device(id) ON DELETE CASCADE,
    lane_id BIGINT NOT NULL REFERENCES rollout_lane(id) ON DELETE CASCADE,
    added_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_rollout_lane_member_lane ON rollout_lane_member(lane_id);

-- Desired firmware per (lane, model). Firmware file id/version reference an
-- uploaded firmware file; the version string is compared against the version
-- miners report to decide whether enforcement is needed.
CREATE TABLE rollout_lane_firmware (
    lane_id BIGINT NOT NULL REFERENCES rollout_lane(id) ON DELETE CASCADE,
    model TEXT NOT NULL,
    firmware_file_id TEXT NOT NULL,
    firmware_version TEXT NOT NULL,
    assigned_by BIGINT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (lane_id, model)
);

-- One firmware change for one model within one lane.
CREATE TABLE firmware_rollout (
    id BIGSERIAL PRIMARY KEY,
    org_id BIGINT NOT NULL REFERENCES organization(id) ON DELETE CASCADE,
    lane_id BIGINT NOT NULL REFERENCES rollout_lane(id) ON DELETE CASCADE,
    model TEXT NOT NULL,
    firmware_file_id TEXT NOT NULL,
    firmware_version TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'active', -- active | completed | canceled
    created_by BIGINT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    finished_at TIMESTAMPTZ
);

-- At most one active rollout per (lane, model).
CREATE UNIQUE INDEX idx_one_active_rollout_per_lane_model
    ON firmware_rollout(lane_id, model)
    WHERE status = 'active';

-- Devices an active rollout has already sent an update command to, so the
-- enforcement loop does not re-issue commands every tick.
CREATE TABLE firmware_rollout_device (
    rollout_id BIGINT NOT NULL REFERENCES firmware_rollout(id) ON DELETE CASCADE,
    device_id BIGINT NOT NULL REFERENCES device(id) ON DELETE CASCADE,
    update_sent_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (rollout_id, device_id)
);
