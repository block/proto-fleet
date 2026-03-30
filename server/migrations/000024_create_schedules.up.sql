CREATE TABLE schedule (
    id              BIGSERIAL PRIMARY KEY,
    org_id          BIGINT NOT NULL,
    name            VARCHAR(100) NOT NULL,
    action          TEXT NOT NULL,       -- 'set_power_target' | 'reboot'
    action_config   JSONB NOT NULL DEFAULT '{}',
    schedule_type   TEXT NOT NULL,       -- 'one_time' | 'recurring'
    recurrence      JSONB,
    start_date      DATE NOT NULL,
    start_time      TIME NOT NULL,
    end_time        TIME,
    end_date        DATE,
    timezone        TEXT NOT NULL,
    status          TEXT NOT NULL DEFAULT 'active',
    priority        INT NOT NULL,
    created_by      BIGINT NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at      TIMESTAMPTZ,
    last_run_at     TIMESTAMPTZ,
    next_run_at     TIMESTAMPTZ,

    CONSTRAINT fk_schedule_org FOREIGN KEY (org_id)
        REFERENCES organization(id) ON DELETE RESTRICT
);

CREATE INDEX idx_schedule_org_status ON schedule(org_id, status) WHERE deleted_at IS NULL;
CREATE INDEX idx_schedule_next_run ON schedule(next_run_at, status)
    WHERE status = 'active' AND deleted_at IS NULL;
CREATE UNIQUE INDEX uk_schedule_org_priority
    ON schedule(org_id, priority)
    WHERE deleted_at IS NULL;

CREATE TRIGGER update_schedule_updated_at
    BEFORE UPDATE ON schedule
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

CREATE TABLE schedule_target (
    id            BIGSERIAL PRIMARY KEY,
    schedule_id   BIGINT NOT NULL REFERENCES schedule(id) ON DELETE CASCADE,
    target_type   TEXT NOT NULL,     -- 'rack' | 'miner'
    target_id     TEXT NOT NULL,

    CONSTRAINT uk_schedule_target UNIQUE (schedule_id, target_type, target_id)
);

