CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE curtailment_event (
    id                          BIGSERIAL PRIMARY KEY,
    event_uuid                  UUID NOT NULL UNIQUE,
    org_id                      BIGINT NOT NULL,
    state                       TEXT NOT NULL,
    mode                        TEXT NOT NULL,
    strategy                    TEXT NOT NULL,
    level                       TEXT NOT NULL,
    priority                    TEXT NOT NULL,
    loop_type                   TEXT NOT NULL,
    scope_type                  TEXT NOT NULL,
    scope_jsonb                 JSONB NOT NULL,
    mode_params_jsonb           JSONB NOT NULL DEFAULT '{}'::jsonb,
    restore_batch_size          INT NOT NULL,
    restore_batch_interval_sec  INT NOT NULL,
    min_curtailed_duration_sec  INT NOT NULL DEFAULT 0,
    max_duration_seconds        INT,
    include_maintenance         BOOLEAN NOT NULL DEFAULT FALSE,
    force_include_maintenance   BOOLEAN NOT NULL DEFAULT FALSE,
    decision_snapshot_jsonb     JSONB NOT NULL DEFAULT '{}'::jsonb,
    source_actor_type           TEXT NOT NULL,
    source_actor_id             TEXT,
    external_source             TEXT,
    external_reference          TEXT,
    idempotency_key             TEXT,
    supersedes_event_id         BIGINT REFERENCES curtailment_event(id),
    reason                      TEXT NOT NULL,
    scheduled_start_at          TIMESTAMPTZ,
    started_at                  TIMESTAMPTZ,
    ended_at                    TIMESTAMPTZ,
    created_at                  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at                  TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT fk_curtailment_event_org FOREIGN KEY (org_id)
        REFERENCES organization(id) ON DELETE RESTRICT,
    CONSTRAINT chk_curtailment_event_state_nonempty
        CHECK (state <> ''),
    CONSTRAINT chk_curtailment_event_mode_nonempty
        CHECK (mode <> ''),
    CONSTRAINT chk_curtailment_event_strategy_nonempty
        CHECK (strategy <> ''),
    CONSTRAINT chk_curtailment_event_level_nonempty
        CHECK (level <> ''),
    CONSTRAINT chk_curtailment_event_priority_nonempty
        CHECK (priority <> ''),
    CONSTRAINT chk_curtailment_event_loop_type
        CHECK (loop_type IN ('open', 'closed')),
    CONSTRAINT chk_curtailment_event_scope_type
        CHECK (scope_type IN ('whole_org', 'device_sets', 'device_list')),
    CONSTRAINT chk_curtailment_event_restore_batch_size
        CHECK (restore_batch_size > 0),
    CONSTRAINT chk_curtailment_event_restore_batch_interval_sec
        CHECK (restore_batch_interval_sec > 0),
    CONSTRAINT chk_curtailment_event_min_duration
        CHECK (min_curtailed_duration_sec >= 0),
    CONSTRAINT chk_curtailment_event_max_duration
        CHECK (max_duration_seconds IS NULL OR max_duration_seconds > 0),
    CONSTRAINT chk_curtailment_event_external_source_nonempty
        CHECK (external_source IS NULL OR external_source <> ''),
    CONSTRAINT chk_curtailment_event_external_reference_nonempty
        CHECK (external_reference IS NULL OR external_reference <> ''),
    CONSTRAINT chk_curtailment_event_idempotency_key_nonempty
        CHECK (idempotency_key IS NULL OR idempotency_key <> ''),
    CONSTRAINT chk_curtailment_event_reason_nonempty
        CHECK (reason <> ''),
    CONSTRAINT chk_curtailment_event_maintenance_consistency
        CHECK (include_maintenance = force_include_maintenance)
);

CREATE TRIGGER update_curtailment_event_updated_at
    BEFORE UPDATE ON curtailment_event
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

CREATE UNIQUE INDEX idx_curtailment_event_external_ref
    ON curtailment_event (org_id, external_source, external_reference)
    WHERE external_source IS NOT NULL AND external_reference IS NOT NULL;

CREATE UNIQUE INDEX idx_curtailment_event_idempotency
    ON curtailment_event (org_id, idempotency_key)
    WHERE idempotency_key IS NOT NULL;

CREATE INDEX idx_curtailment_event_active
    ON curtailment_event (org_id, state, started_at DESC)
    WHERE state IN ('pending', 'active', 'restoring');

CREATE INDEX idx_curtailment_event_org_created
    ON curtailment_event (org_id, created_at);

CREATE TABLE curtailment_target (
    curtailment_event_id      BIGINT NOT NULL REFERENCES curtailment_event(id) ON DELETE CASCADE,
    device_identifier         VARCHAR NOT NULL,
    target_type               TEXT NOT NULL DEFAULT 'miner',
    state                     TEXT NOT NULL,
    desired_state             TEXT NOT NULL,
    baseline_power_w          NUMERIC(12,3),
    added_at                  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    released_at               TIMESTAMPTZ,
    last_dispatched_at        TIMESTAMPTZ,
    last_batch_uuid           VARCHAR(36),
    observed_power_w          NUMERIC(12,3),
    observed_at               TIMESTAMPTZ,
    confirmed_at              TIMESTAMPTZ,
    retry_count               INT NOT NULL DEFAULT 0,
    last_error                TEXT,
    selector_rationale_jsonb  JSONB,

    PRIMARY KEY (curtailment_event_id, device_identifier),
    CONSTRAINT chk_curtailment_target_device_identifier_nonempty
        CHECK (device_identifier <> ''),
    CONSTRAINT chk_curtailment_target_target_type_nonempty
        CHECK (target_type <> ''),
    CONSTRAINT chk_curtailment_target_state_nonempty
        CHECK (state <> ''),
    CONSTRAINT chk_curtailment_target_desired_state
        CHECK (desired_state IN ('curtailed', 'active')),
    CONSTRAINT chk_curtailment_target_baseline_power
        CHECK (baseline_power_w IS NULL OR baseline_power_w >= 0),
    CONSTRAINT chk_curtailment_target_observed_power
        CHECK (observed_power_w IS NULL OR observed_power_w >= 0),
    CONSTRAINT chk_curtailment_target_retry_count
        CHECK (retry_count >= 0)
);

CREATE INDEX idx_curtailment_target_pending_work
    ON curtailment_target (curtailment_event_id, state)
    WHERE state IN ('pending', 'dispatched', 'drifted');

CREATE INDEX idx_curtailment_target_active_by_device
    ON curtailment_target (device_identifier, curtailment_event_id)
    WHERE state NOT IN ('resolved', 'restore_failed', 'released');

CREATE INDEX idx_curtailment_target_terminal_cooldown_by_device
    ON curtailment_target (device_identifier, state, released_at DESC, confirmed_at DESC, added_at DESC, curtailment_event_id)
    WHERE state IN ('resolved', 'restore_failed');

CREATE TABLE curtailment_reconciler_heartbeat (
    id                     SMALLINT    PRIMARY KEY DEFAULT 1,
    last_tick_at           TIMESTAMPTZ NOT NULL,
    last_tick_uuid         UUID        NOT NULL,
    last_tick_duration_ms  INT,
    active_event_count     INT         NOT NULL DEFAULT 0,

    CONSTRAINT chk_curtailment_reconciler_heartbeat_singleton CHECK (id = 1),
    CONSTRAINT chk_curtailment_reconciler_heartbeat_duration
        CHECK (last_tick_duration_ms IS NULL OR last_tick_duration_ms >= 0),
    CONSTRAINT chk_curtailment_reconciler_heartbeat_active_count
        CHECK (active_event_count >= 0)
);

INSERT INTO curtailment_reconciler_heartbeat (id, last_tick_at, last_tick_uuid)
VALUES (1, NOW(), gen_random_uuid())
ON CONFLICT (id) DO NOTHING;
