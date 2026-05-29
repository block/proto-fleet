-- Per-source MQTT publisher config consumed by the curtailment mqtt-ingest
-- subscriber. One row per publisher (broker pair + topic + credentials +
-- contracted curtailment power + thresholds). Operator-managed; v2.0 has no
-- CRUD RPC, so initial rows are seeded via migration data or operator DML.
CREATE TABLE curtailment_mqtt_source_config (
    id                              BIGSERIAL    PRIMARY KEY,
    organization_id                 BIGINT       NOT NULL,
    -- Service-account user the subscriber acts as. curtailment_event has a
    -- NOT NULL FK to "user"; the subscriber runs without a human session,
    -- so the operator provisions a service-account user per source.
    service_user_id                 BIGINT       NOT NULL,
    -- Stable internal label; surfaces in event.external_source.
    source_name                     VARCHAR(64)  NOT NULL,
    topic                           VARCHAR(255) NOT NULL,
    broker_primary_host             VARCHAR(255) NOT NULL,
    broker_secondary_host           VARCHAR(255) NOT NULL,
    broker_port                     INT          NOT NULL DEFAULT 1883,
    mqtt_username                   VARCHAR(255) NOT NULL,
    -- Encrypted via infrastructure/encrypt (base64-wrapped); rotation
    -- is operator-driven for v2.0.
    mqtt_password_enc               TEXT         NOT NULL,
    -- target_kw dispatched on ON->OFF / WATCHDOG_OFF edges.
    -- Upper bound is a fat-finger sanity ceiling (1 GW per source).
    contracted_curtailment_kw       INT          NOT NULL,
    -- Watchdog fires WATCHDOG_OFF after this many seconds of broker silence.
    staleness_threshold_sec         INT          NOT NULL DEFAULT 240,
    -- Minimum hold time stamped on the curtailment event.
    min_curtailed_duration_sec      INT          NOT NULL DEFAULT 600,
    enabled                         BOOLEAN      NOT NULL DEFAULT TRUE,
    created_at                      TIMESTAMPTZ  NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at                      TIMESTAMPTZ  NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT fk_curtailment_mqtt_source_config_org FOREIGN KEY (organization_id)
        REFERENCES organization(id) ON DELETE CASCADE,
    CONSTRAINT fk_curtailment_mqtt_source_config_service_user FOREIGN KEY (service_user_id)
        REFERENCES "user"(id) ON DELETE RESTRICT,
    CONSTRAINT uq_curtailment_mqtt_source_config_org_name UNIQUE (organization_id, source_name),
    CONSTRAINT ck_curtailment_mqtt_source_config_port_positive
        CHECK (broker_port > 0 AND broker_port < 65536),
    CONSTRAINT ck_curtailment_mqtt_source_config_contracted_kw_positive
        CHECK (contracted_curtailment_kw > 0),
    CONSTRAINT ck_curtailment_mqtt_source_config_contracted_kw_max
        CHECK (contracted_curtailment_kw <= 1000000),
    CONSTRAINT ck_curtailment_mqtt_source_config_staleness_positive
        CHECK (staleness_threshold_sec > 0),
    CONSTRAINT ck_curtailment_mqtt_source_config_hold_nonneg
        CHECK (min_curtailed_duration_sec >= 0),
    CONSTRAINT ck_curtailment_mqtt_source_config_brokers_distinct
        CHECK (broker_primary_host <> broker_secondary_host)
);

CREATE INDEX idx_curtailment_mqtt_source_config_enabled
    ON curtailment_mqtt_source_config (enabled)
    WHERE enabled = TRUE;

CREATE TRIGGER update_curtailment_mqtt_source_config_updated_at
    BEFORE UPDATE ON curtailment_mqtt_source_config
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

-- Per-source subscriber state. Singleton per source row; rehydrated on
-- fleetd start so edge detection survives process restarts. last_received_at
-- powers the watchdog query; last_edge_event_uuid lets OFF->ON resolve the
-- in-flight curtailment event for Service.Stop.
CREATE TABLE curtailment_mqtt_source_state (
    source_config_id        BIGINT       PRIMARY KEY,
    -- 0, 100, or NULL when no message has been received yet.
    last_target             SMALLINT     NULL,
    -- Publisher-stamped timestamp from the most recent payload.
    last_target_at          TIMESTAMPTZ  NULL,
    -- Fleet's receive timestamp; staleness compares this against now().
    last_received_at        TIMESTAMPTZ  NULL,
    -- Broker that won precedence on the last message.
    last_received_broker    VARCHAR(255) NULL,
    -- Timestamp of the most recent ON<->OFF flip.
    last_edge_at            TIMESTAMPTZ  NULL,
    -- Curtailment event created by the last ON->OFF (or WATCHDOG_OFF) edge,
    -- stored for audit. v2.0 Stop resolution uses Service.GetActive, not
    -- this column; if multi-source-per-org lands the driver should pivot
    -- to read here so cross-source events aren't accidentally stopped.
    last_edge_event_uuid    UUID         NULL,
    updated_at              TIMESTAMPTZ  NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT fk_curtailment_mqtt_source_state_config FOREIGN KEY (source_config_id)
        REFERENCES curtailment_mqtt_source_config(id) ON DELETE CASCADE,
    CONSTRAINT ck_curtailment_mqtt_source_state_target_valid
        CHECK (last_target IS NULL OR last_target IN (0, 100))
);

CREATE TRIGGER update_curtailment_mqtt_source_state_updated_at
    BEFORE UPDATE ON curtailment_mqtt_source_state
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();
