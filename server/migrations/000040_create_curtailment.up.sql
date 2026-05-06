-- Per-org curtailment tunables. Read at handler entry so request normalization
-- (max_duration_seconds=0 -> default, candidate_min_power_w resolution) hits a
-- guaranteed row rather than a sometimes-present optional value. Seed inserts
-- one row per existing org so subsequent reads always succeed.
CREATE TABLE curtailment_org_config (
    org_id                   BIGINT      PRIMARY KEY,
    max_duration_default_sec INT         NOT NULL DEFAULT 14400,
    candidate_min_power_w    INT         NOT NULL DEFAULT 1500,
    post_event_cooldown_sec  INT         NOT NULL DEFAULT 600,
    created_at               TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at               TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT fk_curtailment_org_config_org FOREIGN KEY (org_id)
        REFERENCES organization(id) ON DELETE CASCADE,
    CONSTRAINT ck_curtailment_org_config_max_duration_positive
        CHECK (max_duration_default_sec > 0),
    CONSTRAINT ck_curtailment_org_config_candidate_power_positive
        CHECK (candidate_min_power_w > 0),
    CONSTRAINT ck_curtailment_org_config_cooldown_nonneg
        CHECK (post_event_cooldown_sec >= 0)
);

CREATE TRIGGER update_curtailment_org_config_updated_at
    BEFORE UPDATE ON curtailment_org_config
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

INSERT INTO curtailment_org_config (org_id)
    SELECT id FROM organization
    ON CONFLICT (org_id) DO NOTHING;

-- Curtailment event: one row per Start request (or Preview persists nothing,
-- but Start writes here). Internal BIGSERIAL id; UUID event_uuid is the
-- external reference exposed in API responses (matches activity_log convention).
CREATE TABLE curtailment_event (
    id                          BIGSERIAL    PRIMARY KEY,
    event_uuid                  UUID         NOT NULL UNIQUE,
    org_id                      BIGINT       NOT NULL,

    -- CurtailmentEventState text: 'pending' | 'active' | 'restoring' |
    -- 'completed' | 'completed_with_failures' | 'cancelled' | 'failed'.
    state                       TEXT         NOT NULL,
    -- CurtailmentMode / Strategy / Level / Priority text.
    mode                        TEXT         NOT NULL,
    strategy                    TEXT         NOT NULL,
    level                       TEXT         NOT NULL,
    priority                    TEXT         NOT NULL,
    -- 'open' (FIXED_KW v1) | 'closed' (v3 closed-loop).
    loop_type                   TEXT         NOT NULL,

    -- 'whole_org' | 'device_sets' | 'device_list'.
    scope_type                  TEXT         NOT NULL,
    scope_jsonb                 JSONB        NOT NULL,
    mode_params_jsonb           JSONB        NOT NULL DEFAULT '{}'::jsonb,

    -- Restore controls. effective_batch_size is computed at restore start
    -- (max(restore_batch_size, ceil(0.01 * selected_target_count)) clamped
    -- to [10, 100]); BE-2 lands the column, BE-4 populates it.
    restore_batch_size          INT          NOT NULL,
    restore_batch_interval_sec  INT          NOT NULL,
    effective_batch_size        INT          NULL,

    -- Duration controls. allow_unbounded is the admin acknowledgement to
    -- skip max_duration_default_sec normalization (see BE-1.x #173).
    min_curtailed_duration_sec  INT          NOT NULL DEFAULT 0,
    max_duration_seconds        INT          NULL,
    allow_unbounded             BOOLEAN      NOT NULL DEFAULT FALSE,

    -- Maintenance override pair. force_include_maintenance=true requires
    -- include_maintenance=true; equality at the DB level is the
    -- defense-in-depth backstop for the API validator.
    include_maintenance         BOOLEAN      NOT NULL DEFAULT FALSE,
    force_include_maintenance   BOOLEAN      NOT NULL DEFAULT FALSE,

    decision_snapshot_jsonb     JSONB        NOT NULL DEFAULT '{}'::jsonb,

    -- 'user' | 'api_key' | 'webhook' | 'scheduler'.
    source_actor_type           TEXT         NOT NULL,
    source_actor_id             TEXT         NULL,
    -- Handlers MUST normalize empty-string proto3 inputs to NULL before
    -- insert. CHECK constraints below enforce the rule at the DB level.
    external_source             TEXT         NULL,
    external_reference          TEXT         NULL,
    idempotency_key             TEXT         NULL,
    -- Reserved for v3+ merge/replace semantics; unwritten in v1.
    supersedes_event_id         BIGINT       NULL,

    reason                      TEXT         NOT NULL,

    scheduled_start_at          TIMESTAMPTZ  NULL,
    started_at                  TIMESTAMPTZ  NULL,
    ended_at                    TIMESTAMPTZ  NULL,
    created_at                  TIMESTAMPTZ  NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at                  TIMESTAMPTZ  NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT fk_curtailment_event_org FOREIGN KEY (org_id)
        REFERENCES organization(id) ON DELETE RESTRICT,
    CONSTRAINT fk_curtailment_event_supersedes FOREIGN KEY (supersedes_event_id)
        REFERENCES curtailment_event(id) ON DELETE SET NULL,

    CONSTRAINT ck_curtailment_event_external_source_nonempty
        CHECK (external_source IS NULL OR external_source <> ''),
    CONSTRAINT ck_curtailment_event_external_reference_nonempty
        CHECK (external_reference IS NULL OR external_reference <> ''),
    CONSTRAINT ck_curtailment_event_idempotency_key_nonempty
        CHECK (idempotency_key IS NULL OR idempotency_key <> ''),
    CONSTRAINT ck_curtailment_event_maintenance_consistency
        CHECK (include_maintenance = force_include_maintenance),
    CONSTRAINT ck_curtailment_event_reason_nonempty
        CHECK (length(trim(reason)) > 0)
);

CREATE TRIGGER update_curtailment_event_updated_at
    BEFORE UPDATE ON curtailment_event
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

-- Webhook dedupe: both source and reference must be present to dedupe.
CREATE UNIQUE INDEX uq_curtailment_event_external_ref
    ON curtailment_event (org_id, external_source, external_reference)
    WHERE external_source IS NOT NULL AND external_reference IS NOT NULL;

-- Idempotent Start retries collapse to one row per (org, key).
CREATE UNIQUE INDEX uq_curtailment_event_idempotency
    ON curtailment_event (org_id, idempotency_key)
    WHERE idempotency_key IS NOT NULL;

-- Active-event lookup by org. Partial index keeps the working set small.
CREATE INDEX idx_curtailment_event_active
    ON curtailment_event (org_id, state, started_at DESC)
    WHERE state IN ('pending', 'active', 'restoring');

-- Future retention pruning: BulkDelete by (org_id, created_at < cutoff).
CREATE INDEX idx_curtailment_event_org_created
    ON curtailment_event (org_id, created_at);

-- Per-event miner row. Composite primary key (event_id, device_identifier);
-- unbounded by VARCHAR length so future smart-PDU/outlet/rack identifiers
-- (target_type extension) fit without a schema change.
CREATE TABLE curtailment_target (
    curtailment_event_id      BIGINT       NOT NULL,
    device_identifier         VARCHAR      NOT NULL,
    -- Future: 'pdu' | 'pdu_outlet' | 'rack'. v1 only emits 'miner'.
    target_type               TEXT         NOT NULL DEFAULT 'miner',
    -- CurtailmentTargetState text: 'pending' | 'dispatched' | 'confirmed' |
    -- 'drifted' | 'resolved' | 'released' | 'restore_failed'.
    state                     TEXT         NOT NULL,
    -- 'curtailed' (active phase) | 'active' (restoring phase).
    desired_state             TEXT         NOT NULL,
    -- Captured once at selection from the same telemetry sample the
    -- selector ranked against; never overwritten. NULL when telemetry
    -- gap at selection forced the dual-signal fallback.
    baseline_power_w          NUMERIC(12,3) NULL,
    added_at                  TIMESTAMPTZ  NOT NULL DEFAULT CURRENT_TIMESTAMP,
    -- Reserved for v3+ closed-loop release; unwritten in v1.
    released_at               TIMESTAMPTZ  NULL,
    -- Reset when desired_state changes (curtail -> restore handoff).
    last_dispatched_at        TIMESTAMPTZ  NULL,
    last_batch_uuid           VARCHAR(36)  NULL,
    -- Mutable per-tick rolling read; not a stable baseline.
    observed_power_w          NUMERIC(12,3) NULL,
    observed_at               TIMESTAMPTZ  NULL,
    -- First time we verified the current desired_state; reset on phase change.
    confirmed_at              TIMESTAMPTZ  NULL,
    retry_count               INT          NOT NULL DEFAULT 0,
    last_error                TEXT         NULL,
    selector_rationale_jsonb  JSONB        NULL,

    PRIMARY KEY (curtailment_event_id, device_identifier),
    CONSTRAINT fk_curtailment_target_event FOREIGN KEY (curtailment_event_id)
        REFERENCES curtailment_event(id) ON DELETE CASCADE
);

CREATE INDEX idx_curtailment_target_pending_work
    ON curtailment_target (curtailment_event_id, state)
    WHERE state IN ('pending', 'dispatched', 'drifted');

-- Hot path for schedule suppression (EXISTS lookup by device_identifier).
CREATE INDEX idx_curtailment_target_active_by_device
    ON curtailment_target (device_identifier, curtailment_event_id)
    WHERE state NOT IN ('resolved', 'restore_failed', 'released');

-- Singleton liveness row. The reconciler upserts on every successful tick;
-- the monitoring stack alerts on staleness AND non-terminal events existing.
-- CHECK (id = 1) plus PRIMARY KEY guarantee at most one row.
CREATE TABLE curtailment_reconciler_heartbeat (
    id                    SMALLINT     PRIMARY KEY DEFAULT 1,
    last_tick_at          TIMESTAMPTZ  NOT NULL,
    last_tick_uuid        UUID         NOT NULL,
    last_tick_duration_ms INT          NULL,
    active_event_count    INT          NOT NULL DEFAULT 0,

    CONSTRAINT ck_curtailment_reconciler_heartbeat_singleton
        CHECK (id = 1)
);

-- Seed the singleton at migration time so the alert predicate always has a
-- row to read. Sentinel zero-UUID is overwritten on the reconciler's first
-- successful tick.
INSERT INTO curtailment_reconciler_heartbeat (id, last_tick_at, last_tick_uuid)
    VALUES (1, CURRENT_TIMESTAMP, '00000000-0000-0000-0000-000000000000')
    ON CONFLICT (id) DO NOTHING;
