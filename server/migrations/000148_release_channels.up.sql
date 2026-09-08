-- Firmware release channels and the rollouts that enforce them.
--
-- A release channel is a scope (sites, buildings, racks, groups, miners)
-- resolved live against fleet placement, with one optional firmware
-- assignment per (manufacturer, model) pair. The enforcement loop updates any
-- member of an assigned pair not running its assigned artifact; each run for
-- one (channel, manufacturer, model) tuple is a firmware_rollout.
--
-- Conventions (see proto/rollout/v1/rollout.proto):
--   * Pair keys and observed device identities are compared through
--     release_channel_pair_key(), which trims the whitespace Go's
--     strings.TrimSpace trims and folds A-Z.
--   * Firmware is identified by the SHA-256 of its payload
--     (firmware_checksum), never by file id; file ids are resolved at read and
--     dispatch time from the files service.
--   * firmware_rollout.revision starts at 1 and advances exactly once per
--     later transaction that changes the rollout row, its target rows or the
--     deployment provenance that references it, so a logical change is one
--     bump however many statements make it.

-- Trims Unicode White_Space (the set Go's strings.TrimSpace trims) and folds
-- A-Z only (lower() under the "C" collation). NULL folds to ''.
CREATE OR REPLACE FUNCTION release_channel_pair_key(text)
RETURNS text
LANGUAGE sql IMMUTABLE PARALLEL SAFE
AS $$
    SELECT lower(btrim(COALESCE($1, ''),
        E' \t\n\v\f\r\u0085\u00a0\u1680\u2000\u2001\u2002\u2003\u2004\u2005\u2006\u2007\u2008\u2009\u200a\u2028\u2029\u202f\u205f\u3000') COLLATE "C")
$$;

CREATE TABLE release_channel (
    id BIGSERIAL PRIMARY KEY,
    org_id BIGINT NOT NULL REFERENCES organization(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    -- Update behavior, copied onto each rollout when it starts so editing the
    -- channel never changes a run in flight. max_concurrent_offline is the
    -- exception: it is channel-wide and read live.
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

-- Scope selectors. Sites, buildings, racks and groups are referenced by id;
-- miners by device identifier, which survives the miner being deleted and
-- paired again under a new device row.
CREATE TABLE release_channel_target (
    channel_id BIGINT NOT NULL REFERENCES release_channel(id) ON DELETE CASCADE,
    target_type TEXT NOT NULL CHECK (target_type IN ('site', 'building', 'rack', 'group', 'miner')),
    target_id BIGINT NULL,
    device_identifier TEXT NULL,
    CHECK (CASE WHEN target_type = 'miner'
                THEN target_id IS NULL AND device_identifier IS NOT NULL
                ELSE target_id IS NOT NULL AND device_identifier IS NULL END)
);

CREATE UNIQUE INDEX idx_release_channel_target_id
    ON release_channel_target(channel_id, target_type, target_id) WHERE target_type <> 'miner';
CREATE UNIQUE INDEX idx_release_channel_target_miner
    ON release_channel_target(channel_id, device_identifier) WHERE target_type = 'miner';
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

CREATE UNIQUE INDEX idx_release_channel_firmware_pair
    ON release_channel_firmware(channel_id, release_channel_pair_key(manufacturer), release_channel_pair_key(model));

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
    -- Revision rule (header): maintained by the triggers below; revision_txid
    -- is the transaction that created the row or last advanced revision.
    revision BIGINT NOT NULL DEFAULT 1,
    revision_txid BIGINT NOT NULL DEFAULT 0,
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
    ON firmware_rollout(channel_id, release_channel_pair_key(manufacturer), release_channel_pair_key(model))
    WHERE status = 'active';

-- Keyset order of ListFirmwareRollouts.
CREATE INDEX idx_firmware_rollout_org_created ON firmware_rollout(org_id, created_at DESC, id DESC);
CREATE INDEX idx_firmware_rollout_org_updated ON firmware_rollout(org_id, updated_at);

-- The creating transaction owns revision 1, so the initial snapshot and any
-- other statement in it do not bump.
CREATE OR REPLACE FUNCTION firmware_rollout_bump_revision()
RETURNS TRIGGER AS $$
DECLARE
    txid BIGINT := pg_current_xact_id()::text::bigint;
BEGIN
    IF TG_OP = 'INSERT' THEN
        NEW.revision_txid = txid;
    ELSIF OLD.revision_txid <> txid THEN
        NEW.revision = OLD.revision + 1;
        NEW.revision_txid = txid;
        NEW.updated_at = now();
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER firmware_rollout_revision
    BEFORE INSERT OR UPDATE ON firmware_rollout
    FOR EACH ROW
    EXECUTE FUNCTION firmware_rollout_bump_revision();

-- Every miner a rollout targets: snapshotted at start (with its baseline
-- health), late joiners appended by the enforcement loop, leavers marked
-- excluded. Membership is not re-derived from the channel once a rollout has
-- finished, so history stays stable.
CREATE TABLE firmware_rollout_device (
    rollout_id BIGINT NOT NULL REFERENCES firmware_rollout(id) ON DELETE CASCADE,
    device_id BIGINT NOT NULL REFERENCES device(id) ON DELETE CASCADE,
    -- 0-based batch; NULL for the unbatched rest and for late joiners.
    batch_index INT NULL,
    -- Order within the rollout (efficiency-ranked or shuffled at start);
    -- NULL for late joiners, which sort last.
    position INT NULL,
    -- Update commands sent so far; a miner is halted as failed once
    -- attempts are exhausted without it verifying.
    attempts INT NOT NULL DEFAULT 0,
    first_sent_at TIMESTAMPTZ NULL,
    last_sent_at TIMESTAMPTZ NULL,
    -- Set when the miner first meets every convergence criterion (target
    -- version, provenance, online, hashing when required). DONE is this
    -- column, not a live derivation from health, so reaching it is a change to
    -- the rollout under the revision rule and a finished rollout's counts do
    -- not drift with later telemetry.
    verified_at TIMESTAMPTZ NULL,
    -- Set when the miner will not be retried for this version: attempts
    -- exhausted ('failed'), the rollout was canceled ('canceled') or a caller
    -- settled it without updating ('skipped'). A halted miner stays out of
    -- enforcement while this is the most recent rollout of the assignment
    -- generation that holds it (firmware_rollout_suppressed_device);
    -- RetryFailedRolloutDevices clears it.
    halted_at TIMESTAMPTZ NULL,
    halt_reason TEXT NOT NULL DEFAULT '',  -- failed | canceled | skipped
    last_error TEXT NOT NULL DEFAULT '',
    skip_note TEXT NOT NULL DEFAULT '',
    -- Set when the miner left the channel scope while the rollout ran.
    excluded_at TIMESTAMPTZ NULL,
    -- Health when the miner was snapshotted into the rollout; NULL for late
    -- joiners, which are judged on version and being online only.
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

-- Touches every rollout the rows of one statement reference after it and,
-- for updates, before it (provenance moving to a later rollout changes the
-- earlier one too); firmware_rollout_revision turns the touches into one bump
-- per transaction.
CREATE OR REPLACE FUNCTION firmware_rollout_touch_from_rows()
RETURNS TRIGGER AS $$
BEGIN
    IF TG_OP = 'UPDATE' THEN
        UPDATE firmware_rollout
        SET updated_at = now()
        WHERE id IN (SELECT rollout_id FROM changed_rows UNION SELECT rollout_id FROM previous_rows);
    ELSE
        UPDATE firmware_rollout
        SET updated_at = now()
        WHERE id IN (SELECT rollout_id FROM changed_rows);
    END IF;
    RETURN NULL;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER firmware_rollout_device_inserted
    AFTER INSERT ON firmware_rollout_device
    REFERENCING NEW TABLE AS changed_rows
    FOR EACH STATEMENT
    EXECUTE FUNCTION firmware_rollout_touch_from_rows();

CREATE TRIGGER firmware_rollout_device_updated
    AFTER UPDATE ON firmware_rollout_device
    REFERENCING OLD TABLE AS previous_rows NEW TABLE AS changed_rows
    FOR EACH STATEMENT
    EXECUTE FUNCTION firmware_rollout_touch_from_rows();

CREATE TRIGGER device_firmware_deployment_inserted
    AFTER INSERT ON device_firmware_deployment
    REFERENCING NEW TABLE AS changed_rows
    FOR EACH STATEMENT
    EXECUTE FUNCTION firmware_rollout_touch_from_rows();

CREATE TRIGGER device_firmware_deployment_updated
    AFTER UPDATE ON device_firmware_deployment
    REFERENCING OLD TABLE AS previous_rows NEW TABLE AS changed_rows
    FOR EACH STATEMENT
    EXECUTE FUNCTION firmware_rollout_touch_from_rows();

-- Live placement of every non-deleted device: its site, its building (the
-- rack's building, falling back to device.building_id) and its rack. Same
-- rules as fleet_device_placement (000134), restated so that view and this
-- domain can change independently. Groups are read from
-- device_set_membership directly because a device can be in several.
CREATE VIEW release_channel_placement AS
SELECT d.org_id,
       d.id AS device_id,
       d.device_identifier,
       d.site_id,
       COALESCE(dsr.building_id, d.building_id) AS building_id,
       rs.id AS rack_id
FROM device d
LEFT JOIN device_set_membership rm ON rm.org_id = d.org_id AND rm.device_id = d.id AND rm.device_set_type = 'rack'
LEFT JOIN device_set rs ON rs.id = rm.device_set_id AND rs.deleted_at IS NULL
LEFT JOIN device_set_rack dsr ON dsr.device_set_id = rs.id
WHERE d.deleted_at IS NULL;

-- Every (channel, miner) selector hit with the selector's specificity (miner
-- 1, group 2, rack 3, building 4, site 5). One row per hit, so a miner in a
-- rack and a group of the same channel appears twice; consume through
-- release_channel_member.
CREATE VIEW release_channel_match AS
SELECT t.channel_id, c.org_id, p.device_id, 1 AS specificity
FROM release_channel_target t
JOIN release_channel c ON c.id = t.channel_id
JOIN release_channel_placement p ON p.org_id = c.org_id AND p.device_identifier = t.device_identifier
WHERE t.target_type = 'miner'
UNION ALL
SELECT t.channel_id, c.org_id, gm.device_id, 2
FROM release_channel_target t
JOIN release_channel c ON c.id = t.channel_id
JOIN device_set gs ON gs.id = t.target_id AND gs.org_id = c.org_id AND gs.type = 'group' AND gs.deleted_at IS NULL
JOIN device_set_membership gm ON gm.device_set_id = gs.id AND gm.device_set_type = 'group'
JOIN device d ON d.id = gm.device_id AND d.deleted_at IS NULL
WHERE t.target_type = 'group'
UNION ALL
SELECT t.channel_id, c.org_id, p.device_id, 3
FROM release_channel_target t
JOIN release_channel c ON c.id = t.channel_id
JOIN release_channel_placement p ON p.org_id = c.org_id AND p.rack_id = t.target_id
WHERE t.target_type = 'rack'
UNION ALL
SELECT t.channel_id, c.org_id, p.device_id, 4
FROM release_channel_target t
JOIN release_channel c ON c.id = t.channel_id
JOIN release_channel_placement p ON p.org_id = c.org_id AND p.building_id = t.target_id
WHERE t.target_type = 'building'
UNION ALL
SELECT t.channel_id, c.org_id, p.device_id, 5
FROM release_channel_target t
JOIN release_channel c ON c.id = t.channel_id
JOIN release_channel_placement p ON p.org_id = c.org_id AND p.site_id = t.target_id
WHERE t.target_type = 'site';

-- One row per (channel, miner) the channel matches, with the miner's most
-- specific selector in that channel and how it ranks against the miner's other
-- channels: best is the lowest specificity across them, at_level how many
-- channels share this row's specificity, channels how many match at all.
-- Every window is partitioned by (org_id, device_id) so org and device
-- predicates reach release_channel_match.
CREATE VIEW release_channel_resolution AS
SELECT channel_id,
       org_id,
       device_id,
       specificity,
       min(specificity) OVER (PARTITION BY org_id, device_id) AS best,
       count(*) OVER (PARTITION BY org_id, device_id, specificity) AS at_level,
       count(*) OVER (PARTITION BY org_id, device_id) AS channels
FROM (
    SELECT channel_id, org_id, device_id, min(specificity) AS specificity
    FROM release_channel_match
    GROUP BY channel_id, org_id, device_id
) per_channel;

-- The channel each miner belongs to: at most one row per miner. A miner
-- belongs to the unique most specific channel that matches it, flagged
-- conflicted when others match too, and to none when several tie.
CREATE VIEW release_channel_member AS
SELECT channel_id, org_id, device_id, channels > 1 AS conflicted
FROM release_channel_resolution
WHERE specificity = best AND at_level = 1;

-- How every channel matching a multiply-matched miner is resolved: the unique
-- most specific match is the 'winner', less specific matches are 'loser', and
-- channels tying at the most specific level are all 'excluded_tie'.
CREATE VIEW release_channel_conflict AS
SELECT channel_id,
       org_id,
       device_id,
       specificity,
       CASE
           WHEN specificity = best AND at_level = 1 THEN 'winner'
           WHEN specificity = best THEN 'excluded_tie'
           ELSE 'loser'
       END AS resolution
FROM release_channel_resolution
WHERE channels > 1;

-- Miners the enforcement loop leaves alone for a pair: halted (failed,
-- canceled or skipped) in the most recent rollout of the pair's assignment
-- generation that holds them, until RetryFailedRolloutDevices re-queues them.
CREATE VIEW firmware_rollout_suppressed_device AS
SELECT channel_id, manufacturer_key, model_key, assignment_generation, device_id
FROM (
    SELECT r.channel_id,
           release_channel_pair_key(r.manufacturer) AS manufacturer_key,
           release_channel_pair_key(r.model) AS model_key,
           r.assignment_generation,
           rd.device_id,
           rd.halted_at,
           row_number() OVER (
               PARTITION BY r.channel_id, release_channel_pair_key(r.manufacturer), release_channel_pair_key(r.model),
                            r.assignment_generation, rd.device_id
               ORDER BY r.created_at DESC, r.id DESC
           ) AS recency
    FROM firmware_rollout_device rd
    JOIN firmware_rollout r ON r.id = rd.rollout_id
) latest
WHERE recency = 1 AND halted_at IS NOT NULL;
