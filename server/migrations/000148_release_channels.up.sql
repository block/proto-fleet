-- Firmware release channels and the rollouts that enforce them.
--
-- A release channel is a scope (sites, buildings, racks, groups, miners)
-- resolved live against fleet placement, with one optional firmware
-- assignment per (manufacturer, model) pair. The enforcement loop updates any
-- member of an assigned pair not running its assigned artifact; each run for
-- one (channel, manufacturer, model) tuple is a firmware_rollout.
--
-- Identity conventions (see proto/rollout/v1/rollout.proto):
--   * Assignment keys are canonical printable-ASCII manufacturer/model strings
--     compared ASCII-case-insensitively: lower(x COLLATE "C") folds A-Z only.
--     Members are matched to a pair by folding their observed identity the
--     same way after trimming.
--   * Firmware is identified by the SHA-256 of its payload
--     (firmware_checksum), never by file id; file ids are resolved at read and
--     dispatch time from the files service.

CREATE TABLE release_channel (
    id BIGSERIAL PRIMARY KEY,
    org_id BIGINT NOT NULL REFERENCES organization(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    -- Update behavior. Copied onto each rollout when it starts so editing
    -- the channel never changes a run in flight, except max_concurrent_offline
    -- which is channel-wide and live.
    method TEXT NOT NULL DEFAULT 'all_at_once',              -- all_at_once | batched | pilot_then_continue | delegated
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
    min_sample_coverage_percent DOUBLE PRECISION NULL,       -- NULL: 100
    max_concurrent_offline INT NOT NULL DEFAULT 0,           -- 0: no limit
    controller_timeout_seconds INT NOT NULL DEFAULT 0,       -- delegated; 0: never
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

-- Desired firmware per (channel, manufacturer, model) pair. The row outlives
-- the assignment: clearing sets firmware_checksum to '' and keeps the
-- generation counter, which increments on every assignment change including
-- the clear. A rollout is current while its generation equals the pair's.
CREATE TABLE release_channel_firmware (
    channel_id BIGINT NOT NULL REFERENCES release_channel(id) ON DELETE CASCADE,
    manufacturer TEXT NOT NULL,
    model TEXT NOT NULL,
    firmware_checksum TEXT NOT NULL DEFAULT '',            -- '' when unassigned
    firmware_version TEXT NOT NULL DEFAULT '',
    -- Target metadata snapshotted from the file when assigned.
    firmware_target_manufacturer TEXT NOT NULL DEFAULT '',
    firmware_target_model TEXT NOT NULL DEFAULT '',
    assignment_generation BIGINT NOT NULL DEFAULT 0,
    assigned_by BIGINT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (channel_id, manufacturer, model)
);

CREATE UNIQUE INDEX idx_release_channel_firmware_folded_key
    ON release_channel_firmware(channel_id, lower(manufacturer COLLATE "C"), lower(model COLLATE "C"));

-- One firmware change for one (manufacturer, model) pair within one channel.
CREATE TABLE firmware_rollout (
    id BIGSERIAL PRIMARY KEY,
    org_id BIGINT NOT NULL REFERENCES organization(id) ON DELETE CASCADE,
    channel_id BIGINT NOT NULL REFERENCES release_channel(id) ON DELETE CASCADE,
    manufacturer TEXT NOT NULL,
    model TEXT NOT NULL,
    firmware_checksum TEXT NOT NULL,
    firmware_version TEXT NOT NULL,
    -- Assignment lineage: what a rollback restores; '' for a first assignment.
    previous_firmware_checksum TEXT NOT NULL DEFAULT '',
    previous_firmware_version TEXT NOT NULL DEFAULT '',
    -- Generation of the pair's assignment this rollout enforces.
    assignment_generation BIGINT NOT NULL,
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
    min_sample_coverage_percent DOUBLE PRECISION NULL,
    max_concurrent_offline INT NOT NULL DEFAULT 0,
    controller_timeout_seconds INT NOT NULL DEFAULT 0,
    -- Snapshotted batches (0 for all-at-once and delegated) and the one in flight.
    batch_count INT NOT NULL DEFAULT 0,
    current_batch INT NOT NULL DEFAULT 0,
    stage_changed_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    paused_at TIMESTAMPTZ NULL,
    -- Revision rule: +1 on every change to this row or to its devices
    -- (trigger below); updated_at records when.
    revision BIGINT NOT NULL DEFAULT 1,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- Actors: user | api_key | system, with id (0 for system) and display name.
    started_by_type TEXT NOT NULL DEFAULT 'system',
    started_by_id BIGINT NOT NULL DEFAULT 0,
    started_by_name TEXT NOT NULL DEFAULT '',
    last_action_by_type TEXT NOT NULL DEFAULT 'system',
    last_action_by_id BIGINT NOT NULL DEFAULT 0,
    last_action_by_name TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    finished_at TIMESTAMPTZ NULL
);

-- At most one active rollout per (channel, manufacturer, model).
CREATE UNIQUE INDEX idx_one_active_rollout_per_pair
    ON firmware_rollout(channel_id, lower(manufacturer COLLATE "C"), lower(model COLLATE "C"))
    WHERE status = 'active';

CREATE INDEX idx_firmware_rollout_org_created ON firmware_rollout(org_id, created_at DESC);
CREATE INDEX idx_firmware_rollout_org_updated ON firmware_rollout(org_id, updated_at);

CREATE OR REPLACE FUNCTION firmware_rollout_bump_revision()
RETURNS TRIGGER AS $$
BEGIN
    NEW.revision = OLD.revision + 1;
    NEW.updated_at = now();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER firmware_rollout_revision
    BEFORE UPDATE ON firmware_rollout
    FOR EACH ROW
    EXECUTE FUNCTION firmware_rollout_bump_revision();

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
    -- exhausted ('failed'), the rollout was canceled ('canceled'), or a caller
    -- settled it without updating ('skipped'). Enforcement suppresses failed
    -- and skipped miners for the rest of the assignment generation;
    -- RetryFailedRolloutDevices clears it.
    halted_at TIMESTAMPTZ NULL,
    halt_reason TEXT NOT NULL DEFAULT '',  -- failed | canceled | skipped
    last_error TEXT NOT NULL DEFAULT '',
    skip_note TEXT NOT NULL DEFAULT '',
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

-- Any change to a rollout's devices is a change to the rollout under the
-- revision rule: one bump per statement per rollout.
CREATE OR REPLACE FUNCTION firmware_rollout_devices_changed()
RETURNS TRIGGER AS $$
BEGIN
    UPDATE firmware_rollout
    SET updated_at = now()
    WHERE id IN (SELECT DISTINCT rollout_id FROM changed_rows);
    RETURN NULL;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER firmware_rollout_device_inserted
    AFTER INSERT ON firmware_rollout_device
    REFERENCING NEW TABLE AS changed_rows
    FOR EACH STATEMENT
    EXECUTE FUNCTION firmware_rollout_devices_changed();

CREATE TRIGGER firmware_rollout_device_updated
    AFTER UPDATE ON firmware_rollout_device
    REFERENCING NEW TABLE AS changed_rows
    FOR EACH STATEMENT
    EXECUTE FUNCTION firmware_rollout_devices_changed();

-- Managed-deployment provenance: the artifact Fleet last deployed to a miner,
-- recorded when a rollout target reports the target version. A member is on
-- target only when its reported version and its provenance both match the
-- assignment.
CREATE TABLE device_firmware_deployment (
    device_id BIGINT PRIMARY KEY REFERENCES device(id) ON DELETE CASCADE,
    firmware_checksum TEXT NOT NULL,
    firmware_version TEXT NOT NULL,
    rollout_id BIGINT NULL REFERENCES firmware_rollout(id) ON DELETE SET NULL,
    deployed_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

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

-- How every channel that matches a miner is resolved. Rows exist only for
-- miners matched by more than one channel: the unique most specific match is
-- the 'winner', less specific matches are 'loser', and when several channels
-- tie at the most specific level every one of them is 'excluded_tie' and the
-- miner belongs to no channel until the conflict is removed.
CREATE VIEW release_channel_conflict AS
WITH per_channel AS (
    SELECT channel_id, org_id, device_id, min(specificity) AS specificity
    FROM release_channel_match
    GROUP BY channel_id, org_id, device_id
), per_device AS (
    SELECT device_id, min(specificity) AS best, count(*) AS channels
    FROM per_channel
    GROUP BY device_id
), at_best AS (
    SELECT pc.device_id, count(*) AS n
    FROM per_channel pc
    JOIN per_device pd ON pd.device_id = pc.device_id AND pc.specificity = pd.best
    GROUP BY pc.device_id
)
SELECT pc.channel_id,
       pc.org_id,
       pc.device_id,
       pc.specificity,
       CASE
           WHEN pc.specificity = pd.best AND ab.n = 1 THEN 'winner'
           WHEN pc.specificity = pd.best THEN 'excluded_tie'
           ELSE 'loser'
       END AS resolution
FROM per_channel pc
JOIN per_device pd ON pd.device_id = pc.device_id
JOIN at_best ab ON ab.device_id = pc.device_id
WHERE pd.channels > 1;

-- The channel each miner belongs to: at most one row per miner. A miner
-- matched by one channel belongs to it; a miner matched by several belongs to
-- the unique most specific one (flagged conflicted) or to none on a tie.
CREATE VIEW release_channel_member AS
WITH per_channel AS (
    SELECT channel_id, org_id, device_id, min(specificity) AS specificity
    FROM release_channel_match
    GROUP BY channel_id, org_id, device_id
), per_device AS (
    SELECT device_id, min(specificity) AS best, count(*) AS channels
    FROM per_channel
    GROUP BY device_id
), winners AS (
    SELECT pc.channel_id,
           pc.org_id,
           pc.device_id,
           pd.channels,
           count(*) OVER (PARTITION BY pc.device_id) AS at_best
    FROM per_channel pc
    JOIN per_device pd ON pd.device_id = pc.device_id AND pc.specificity = pd.best
)
SELECT channel_id, org_id, device_id, channels > 1 AS conflicted
FROM winners
WHERE at_best = 1;
