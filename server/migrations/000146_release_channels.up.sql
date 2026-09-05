-- Firmware release channels and the rollouts that enforce them.
--
-- A release channel is a scope (sites, buildings, racks, groups, miners)
-- resolved live against fleet placement, with one optional firmware
-- assignment per hardware model. The enforcement loop updates any member
-- not running its model's assigned version; each run for one
-- (channel, model) pair is a firmware_rollout.

CREATE TABLE release_channel (
    id BIGSERIAL PRIMARY KEY,
    org_id BIGINT NOT NULL REFERENCES organization(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    -- Update behavior. Copied onto each rollout when it starts so editing
    -- the channel never changes a run in flight.
    method TEXT NOT NULL DEFAULT 'all_at_once',              -- all_at_once | batched | pilot_then_continue
    order_by TEXT NOT NULL DEFAULT 'least_efficient_first',  -- least_efficient_first | random
    batch_size INT NOT NULL DEFAULT 0,                       -- batched
    pilot_size INT NOT NULL DEFAULT 0,                       -- pilot_then_continue
    wait_between_batches_seconds INT NOT NULL DEFAULT 0,     -- batched without review
    review_after_each_batch BOOLEAN NOT NULL DEFAULT false,  -- batched
    auto_continue BOOLEAN NOT NULL DEFAULT false,
    stabilization_seconds INT NOT NULL DEFAULT 0,
    max_hashrate_drop_percent DOUBLE PRECISION NULL,         -- NULL: not checked
    max_efficiency_increase_percent DOUBLE PRECISION NULL,
    max_temp_increase_c DOUBLE PRECISION NULL,
    max_new_errors INT NULL,
    max_concurrent_offline INT NOT NULL DEFAULT 0,           -- 0: no limit
    created_by BIGINT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (org_id, name)
);

-- Which miners a channel applies to. A miner is in the channel when any
-- selector matches its current placement (fleet_device_placement).
CREATE TABLE release_channel_target (
    channel_id BIGINT NOT NULL REFERENCES release_channel(id) ON DELETE CASCADE,
    target_type TEXT NOT NULL CHECK (target_type IN ('site', 'building', 'rack', 'group', 'miner')),
    target_id BIGINT NOT NULL,
    PRIMARY KEY (channel_id, target_type, target_id)
);

CREATE INDEX idx_release_channel_target_lookup ON release_channel_target(target_type, target_id);

-- Desired firmware per (channel, model). The version string is compared
-- against what miners report to decide whether enforcement is needed.
CREATE TABLE release_channel_firmware (
    channel_id BIGINT NOT NULL REFERENCES release_channel(id) ON DELETE CASCADE,
    model TEXT NOT NULL,
    firmware_file_id TEXT NOT NULL,
    firmware_version TEXT NOT NULL,
    assigned_by BIGINT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (channel_id, model)
);

-- One firmware change for one model within one channel.
CREATE TABLE firmware_rollout (
    id BIGSERIAL PRIMARY KEY,
    org_id BIGINT NOT NULL REFERENCES organization(id) ON DELETE CASCADE,
    channel_id BIGINT NOT NULL REFERENCES release_channel(id) ON DELETE CASCADE,
    model TEXT NOT NULL,
    firmware_file_id TEXT NOT NULL,
    firmware_version TEXT NOT NULL,
    -- Assignment in place before this rollout; what a rollback restores.
    previous_firmware_file_id TEXT NOT NULL DEFAULT '',
    previous_firmware_version TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'active',   -- active | completed | completed_with_failures | canceled
    cancel_reason TEXT NOT NULL DEFAULT '',  -- superseded | canceled_remaining | rolled_back | cleared
    stage TEXT NOT NULL DEFAULT 'rest',      -- batch | awaiting_review | waiting | rest
    -- Behavior snapshot (see release_channel).
    method TEXT NOT NULL DEFAULT 'all_at_once',
    order_by TEXT NOT NULL DEFAULT 'least_efficient_first',
    batch_size INT NOT NULL DEFAULT 0,
    pilot_size INT NOT NULL DEFAULT 0,
    wait_between_batches_seconds INT NOT NULL DEFAULT 0,
    review_after_each_batch BOOLEAN NOT NULL DEFAULT false,
    auto_continue BOOLEAN NOT NULL DEFAULT false,
    stabilization_seconds INT NOT NULL DEFAULT 0,
    max_hashrate_drop_percent DOUBLE PRECISION NULL,
    max_efficiency_increase_percent DOUBLE PRECISION NULL,
    max_temp_increase_c DOUBLE PRECISION NULL,
    max_new_errors INT NULL,
    max_concurrent_offline INT NOT NULL DEFAULT 0,
    -- Snapshotted batches (0 for all-at-once) and the one in flight.
    batch_count INT NOT NULL DEFAULT 0,
    current_batch INT NOT NULL DEFAULT 0,
    stage_changed_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    paused_at TIMESTAMPTZ NULL,
    created_by BIGINT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    finished_at TIMESTAMPTZ NULL
);

-- At most one active rollout per (channel, model).
CREATE UNIQUE INDEX idx_one_active_rollout_per_channel_model
    ON firmware_rollout(channel_id, model)
    WHERE status = 'active';

CREATE INDEX idx_firmware_rollout_org_created ON firmware_rollout(org_id, created_at DESC);

-- Every miner a rollout targets: snapshotted at start (with its baseline
-- health), late joiners appended by the enforcement loop, leavers marked
-- excluded. This is the rollout's target set; membership is not re-derived
-- from the channel once a rollout has finished, so history stays stable.
CREATE TABLE firmware_rollout_device (
    rollout_id BIGINT NOT NULL REFERENCES firmware_rollout(id) ON DELETE CASCADE,
    device_id BIGINT NOT NULL REFERENCES device(id) ON DELETE CASCADE,
    -- 0-based batch; NULL for the unbatched rest / late joiners.
    batch_index INT NULL,
    -- Order within the rollout (efficiency-ranked or shuffled at start);
    -- NULL for late joiners, which sort last.
    position INT NULL,
    -- Update commands sent so far; a miner is halted as failed once
    -- attempts are exhausted without it verifying.
    attempts INT NOT NULL DEFAULT 0,
    first_sent_at TIMESTAMPTZ NULL,
    last_sent_at TIMESTAMPTZ NULL,
    -- Set when the miner will not be retried for this version: attempts
    -- exhausted ('failed') or the rollout was canceled ('canceled'). Drift
    -- correction skips halted miners; RetryFailedRolloutDevices clears it.
    halted_at TIMESTAMPTZ NULL,
    halt_reason TEXT NOT NULL DEFAULT '',  -- failed | canceled
    last_error TEXT NOT NULL DEFAULT '',
    -- Set when the miner left the channel scope while the rollout ran.
    excluded_at TIMESTAMPTZ NULL,
    -- Health when the miner was snapshotted into the rollout; NULL for late
    -- joiners, which are judged on version + online only.
    baseline_status TEXT NULL,
    baseline_hash_rate_hs DOUBLE PRECISION NULL,
    baseline_power_w DOUBLE PRECISION NULL,
    baseline_efficiency_jh DOUBLE PRECISION NULL,
    baseline_temp_c DOUBLE PRECISION NULL,
    baseline_open_errors INT NULL,
    baseline_at TIMESTAMPTZ NULL,
    added_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (rollout_id, device_id)
);

CREATE INDEX idx_firmware_rollout_device_device ON firmware_rollout_device(device_id);

-- Every (channel, miner) selector hit, with how specific the selector was.
-- One row per hit, so a miner in a rack and a group of the same channel
-- appears twice; consume through release_channel_member or aggregate.
--
-- Placement follows the same rules as fleet_device_placement (000134): a
-- miner's rack is its live 'rack' set membership, its building the rack's
-- building falling back to device.building_id, its site device.site_id, and
-- its groups its live 'group' set memberships. The joins are inlined rather
-- than read through that view so neither view blocks changes to the other.
CREATE VIEW release_channel_match AS
SELECT t.channel_id, c.org_id, d.id AS device_id, 1 AS specificity
FROM release_channel_target t
JOIN release_channel c ON c.id = t.channel_id
JOIN device d ON d.id = t.target_id AND d.org_id = c.org_id AND d.deleted_at IS NULL
WHERE t.target_type = 'miner'
UNION ALL
SELECT t.channel_id, c.org_id, d.id, 2
FROM release_channel_target t
JOIN release_channel c ON c.id = t.channel_id
JOIN device_set gs ON gs.id = t.target_id AND gs.org_id = c.org_id AND gs.type = 'group' AND gs.deleted_at IS NULL
JOIN device_set_membership gm ON gm.device_set_id = gs.id AND gm.device_set_type = 'group'
JOIN device d ON d.id = gm.device_id AND d.org_id = c.org_id AND d.deleted_at IS NULL
WHERE t.target_type = 'group'
UNION ALL
SELECT t.channel_id, c.org_id, d.id, 3
FROM release_channel_target t
JOIN release_channel c ON c.id = t.channel_id
JOIN device_set rs ON rs.id = t.target_id AND rs.org_id = c.org_id AND rs.type = 'rack' AND rs.deleted_at IS NULL
JOIN device_set_membership rm ON rm.device_set_id = rs.id AND rm.device_set_type = 'rack'
JOIN device d ON d.id = rm.device_id AND d.org_id = c.org_id AND d.deleted_at IS NULL
WHERE t.target_type = 'rack'
UNION ALL
SELECT t.channel_id, c.org_id, d.id, 4
FROM release_channel_target t
JOIN release_channel c ON c.id = t.channel_id
JOIN device d ON d.org_id = c.org_id AND d.deleted_at IS NULL
LEFT JOIN device_set_membership rm ON rm.org_id = d.org_id AND rm.device_id = d.id AND rm.device_set_type = 'rack'
LEFT JOIN device_set rs ON rs.id = rm.device_set_id AND rs.deleted_at IS NULL
LEFT JOIN device_set_rack dsr ON dsr.device_set_id = rs.id
WHERE t.target_type = 'building'
  AND COALESCE(dsr.building_id, d.building_id) = t.target_id
UNION ALL
SELECT t.channel_id, c.org_id, d.id, 5
FROM release_channel_target t
JOIN release_channel c ON c.id = t.channel_id
JOIN device d ON d.org_id = c.org_id AND d.deleted_at IS NULL AND d.site_id = t.target_id
WHERE t.target_type = 'site';

-- The channel each miner belongs to: one row per miner. When more than one
-- channel matches (a miner moved after both were saved), the most specific
-- selector wins, then the lowest channel id, and the row is flagged.
CREATE VIEW release_channel_member AS
SELECT DISTINCT ON (m.device_id)
       m.channel_id,
       m.org_id,
       m.device_id,
       -- m has one row per (channel, miner), so this counts channels.
       (count(*) OVER (PARTITION BY m.device_id)) > 1 AS conflicted
FROM (
    SELECT channel_id, org_id, device_id, min(specificity) AS specificity
    FROM release_channel_match
    GROUP BY channel_id, org_id, device_id
) m
ORDER BY m.device_id, m.specificity, m.channel_id;
