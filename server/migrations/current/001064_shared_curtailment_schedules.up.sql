-- Automation rules can curtail on a time schedule instead of an MQTT source.
ALTER TABLE curtailment_automation_rule
    DROP CONSTRAINT ck_curtailment_automation_rule_trigger_type,
    ALTER COLUMN mqtt_source_id DROP NOT NULL,
    ADD CONSTRAINT ck_curtailment_automation_rule_trigger_type CHECK (
        (trigger_type = 'MQTT' AND mqtt_source_id IS NOT NULL)
        OR (trigger_type = 'SCHEDULE' AND mqtt_source_id IS NULL)
    );

-- schedule_revision is the schedule revision a queued window boundary was
-- produced from; work from an earlier revision cannot start or restore events.
ALTER TABLE curtailment_automation_execution
    ALTER COLUMN mqtt_source_id DROP NOT NULL,
    ADD COLUMN schedule_revision uuid;

-- schedule_revision is the schedule revision a scheduled event started under.
-- A schedule window runs at most one event per schedule revision, including
-- after that event ends; the index also finds the window's event in any state.
ALTER TABLE curtailment_event ADD COLUMN schedule_revision uuid;
CREATE UNIQUE INDEX idx_curtailment_event_schedule_window
    ON curtailment_event (org_id, external_reference, scheduled_start_at, schedule_revision)
    WHERE schedule_revision IS NOT NULL;

-- Dates and clock times are local to timezone. service_user_id is the user who
-- last saved the schedule; it attributes scheduled events and their commands.
-- revision changes when a save changes the schedule's timing and when a
-- disabled schedule is enabled.
CREATE TABLE curtailment_automation_schedule (
    rule_id bigint PRIMARY KEY,
    schedule_type text NOT NULL,
    timezone text NOT NULL,
    start_date date NOT NULL,
    start_time time NOT NULL,
    end_time time NOT NULL,
    end_date date,
    frequency text,
    days_of_week integer[] NOT NULL DEFAULT '{}',
    day_of_month smallint,
    until_date date,
    service_user_id bigint NOT NULL,
    revision uuid NOT NULL DEFAULT gen_random_uuid(),
    created_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT fk_curtailment_automation_schedule_rule
        FOREIGN KEY (rule_id) REFERENCES curtailment_automation_rule(id) ON DELETE CASCADE,
    CONSTRAINT fk_curtailment_automation_schedule_service_user
        FOREIGN KEY (service_user_id) REFERENCES "user"(id) ON DELETE RESTRICT,
    CONSTRAINT ck_curtailment_automation_schedule_shape CHECK (
        (
            schedule_type = 'ONE_TIME'
            AND end_date IS NOT NULL
            AND frequency IS NULL
            AND cardinality(days_of_week) = 0
            AND day_of_month IS NULL
            AND until_date IS NULL
        )
        OR (
            schedule_type = 'RECURRING'
            AND end_date IS NULL
            AND start_time <> end_time
            AND (
                (frequency = 'DAILY' AND cardinality(days_of_week) = 0 AND day_of_month IS NULL)
                OR (frequency = 'WEEKLY' AND cardinality(days_of_week) BETWEEN 1 AND 7 AND day_of_month IS NULL)
                OR (frequency = 'MONTHLY' AND cardinality(days_of_week) = 0 AND day_of_month BETWEEN 1 AND 31)
            )
            AND (until_date IS NULL OR until_date >= start_date)
        )
    ),
    CONSTRAINT ck_curtailment_automation_schedule_days_of_week CHECK (
        days_of_week <@ ARRAY[0, 1, 2, 3, 4, 5, 6]
    )
);
