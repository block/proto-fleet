-- Per-source MQTT publisher config for the curtailment mqtt-ingest
-- subscriber: broker pair, topic, credentials, contracted power, thresholds.
-- Operator-managed; no CRUD RPC yet (seed via migration data or DML).
CREATE TABLE curtailment_mqtt_source_config (
    id                              BIGSERIAL    PRIMARY KEY,
    organization_id                 BIGINT       NOT NULL,
    -- Service-account user the subscriber acts as (curtailment_event has a
    -- NOT NULL user FK; the subscriber has no human session).
    service_user_id                 BIGINT       NOT NULL,
    -- Stable internal label; surfaces in event.external_source.
    source_name                     VARCHAR(64)  NOT NULL,
    topic                           VARCHAR(255) NOT NULL,
    broker_primary_host             VARCHAR(255) NOT NULL,
    broker_secondary_host           VARCHAR(255) NOT NULL,
    -- Broker port; NULL → default applied in code (1883 for MaestroOS TCP).
    broker_port                     INT          NULL,
    -- Explicit transport so plaintext MQTT is deliberate. `tcp` is accepted
    -- only for private/local broker hosts by the subscriber startup guard.
    broker_transport                TEXT         NOT NULL DEFAULT 'tcp',
    mqtt_username                   VARCHAR(255) NOT NULL,
    -- Encrypted via infrastructure/encrypt (base64-wrapped); rotation
    -- is operator-driven.
    mqtt_password_enc               TEXT         NOT NULL,
    -- target_kw dispatched on ON->OFF / WATCHDOG_OFF edges for curtail_mode
    -- 'FIXED_KW'. Required for 'FIXED_KW'; NULL-able (reporting only) for
    -- 'FULL_FLEET'. Upper bound is a fat-finger sanity ceiling (1 GW/source).
    contracted_curtailment_kw       INT          NULL,
    -- Target-set mode; defaults to curtailing every eligible device in scope.
    curtail_mode                    TEXT         NOT NULL DEFAULT 'FULL_FLEET',
    -- Wire format of the MQTT payload; selects the decoder that maps it to the
    -- canonical (target, timestamp). Validated against the in-code decoder
    -- registry at startup, so a new integration needs no migration here.
    payload_format                  TEXT         NOT NULL DEFAULT 'target_timestamp',
    -- Curtailment scope this source targets: 'whole_org' (all org devices),
    -- 'site' (scope_site_id below), or 'device_list'
    -- (scope_device_identifiers below). device_sets is not yet supported by
    -- the curtailment core.
    scope_type                      TEXT         NOT NULL DEFAULT 'whole_org',
    -- Site target for scope_type='site'; NULL for other scopes. The subscriber
    -- still rejects site scope until the curtailment core supports it.
    scope_site_id                   BIGINT       NULL,
    -- Device identifiers for scope_type='device_list'; NULL for 'whole_org'.
    scope_device_identifiers        TEXT[]       NULL,
    -- Seconds of broker silence before the watchdog fires WATCHDOG_OFF.
    -- NULL → default applied in code.
    staleness_threshold_sec         INT          NULL,
    -- Minimum hold time stamped on the curtailment event.
    -- NULL → default applied in code.
    min_curtailed_duration_sec      INT          NULL,
    enabled                         BOOLEAN      NOT NULL DEFAULT TRUE,
    created_at                      TIMESTAMPTZ  NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at                      TIMESTAMPTZ  NOT NULL DEFAULT CURRENT_TIMESTAMP,

    -- RESTRICT (not CASCADE): an org delete must not silently drop a source's
    -- encrypted broker credentials; detach the sources first.
    CONSTRAINT fk_curtailment_mqtt_source_config_org FOREIGN KEY (organization_id)
        REFERENCES organization(id) ON DELETE RESTRICT,
    CONSTRAINT fk_curtailment_mqtt_source_config_service_user FOREIGN KEY (service_user_id)
        REFERENCES "user"(id) ON DELETE RESTRICT,
    CONSTRAINT fk_curtailment_mqtt_source_config_site FOREIGN KEY (scope_site_id, organization_id)
        REFERENCES site(id, org_id) ON DELETE RESTRICT,
    CONSTRAINT uq_curtailment_mqtt_source_config_org_name UNIQUE (organization_id, source_name),
    CONSTRAINT ck_curtailment_mqtt_source_config_source_name_nonempty
        CHECK (btrim(source_name) <> ''),
    CONSTRAINT ck_curtailment_mqtt_source_config_topic_nonempty
        CHECK (btrim(topic) <> ''),
    CONSTRAINT ck_curtailment_mqtt_source_config_primary_host_nonempty
        CHECK (btrim(broker_primary_host) <> ''),
    CONSTRAINT ck_curtailment_mqtt_source_config_secondary_host_nonempty
        CHECK (btrim(broker_secondary_host) <> ''),
    CONSTRAINT ck_curtailment_mqtt_source_config_username_nonempty
        CHECK (btrim(mqtt_username) <> ''),
    CONSTRAINT ck_curtailment_mqtt_source_config_password_nonempty
        CHECK (btrim(mqtt_password_enc) <> ''),
    CONSTRAINT ck_curtailment_mqtt_source_config_port_positive
        CHECK (broker_port IS NULL OR (broker_port > 0 AND broker_port < 65536)),
    CONSTRAINT ck_curtailment_mqtt_source_config_transport
        CHECK (broker_transport IN ('tcp', 'tls')),
    -- kW is null-able (reporting-only for full_fleet); valid range when set.
    CONSTRAINT ck_curtailment_mqtt_source_config_contracted_kw_range
        CHECK (contracted_curtailment_kw IS NULL
            OR (contracted_curtailment_kw > 0 AND contracted_curtailment_kw <= 1000000)),
    CONSTRAINT ck_curtailment_mqtt_source_config_curtail_mode
        CHECK (curtail_mode IN ('FIXED_KW', 'FULL_FLEET')),
    -- FIXED_KW must carry a contracted target; FULL_FLEET may omit it.
    CONSTRAINT ck_curtailment_mqtt_source_config_fixed_kw_requires_target
        CHECK (curtail_mode <> 'FIXED_KW' OR contracted_curtailment_kw IS NOT NULL),
    CONSTRAINT ck_curtailment_mqtt_source_config_staleness_positive
        CHECK (staleness_threshold_sec IS NULL OR staleness_threshold_sec > 0),
    CONSTRAINT ck_curtailment_mqtt_source_config_hold_nonneg
        CHECK (min_curtailed_duration_sec IS NULL OR min_curtailed_duration_sec >= 0),
    CONSTRAINT ck_curtailment_mqtt_source_config_brokers_distinct
        CHECK (btrim(broker_primary_host) <> btrim(broker_secondary_host)),
    -- whole_org carries no scoped fields; site requires exactly a site id;
    -- device_list requires a non-empty device list.
    CONSTRAINT ck_curtailment_mqtt_source_config_scope CHECK (
        (scope_type = 'whole_org' AND scope_site_id IS NULL
            AND scope_device_identifiers IS NULL)
        OR
        (scope_type = 'site' AND scope_site_id IS NOT NULL
            AND scope_device_identifiers IS NULL)
        OR
        (scope_type = 'device_list' AND scope_site_id IS NULL
            AND scope_device_identifiers IS NOT NULL
            AND cardinality(scope_device_identifiers) > 0)
    )
);

CREATE INDEX idx_curtailment_mqtt_source_config_enabled
    ON curtailment_mqtt_source_config (enabled)
    WHERE enabled = TRUE;

CREATE TRIGGER update_curtailment_mqtt_source_config_updated_at
    BEFORE UPDATE ON curtailment_mqtt_source_config
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

-- Per-source subscriber state; rehydrated on fleetd start so edge detection
-- survives restarts. last_received_at powers the watchdog; last_edge_event_uuid
-- lets OFF->ON resolve the event for Service.Stop.
CREATE TABLE curtailment_mqtt_source_state (
    source_config_id        BIGINT       PRIMARY KEY,
    -- Canonical target 'OFF' / 'ON', or NULL when no message received yet.
    last_target             TEXT         NULL,
    -- Publisher-stamped timestamp from the most recent payload.
    last_target_at          TIMESTAMPTZ  NULL,
    -- Target of the payload that set last_target_at; may differ from
    -- last_target after a debounced flip. Persisted so the duplicate guard
    -- survives a restart (a redelivery of a debounced flip stays suppressed).
    last_processed_target   TEXT         NULL,
    -- All targets already processed for last_target_at. The wire timestamp is
    -- seconds-precision, so this suppresses old same-second QoS redeliveries
    -- after a legitimate opposite-target flip has already settled.
    last_processed_targets  TEXT[]       NULL,
    -- Fleet's receive timestamp; staleness compares this against now().
    last_received_at        TIMESTAMPTZ  NULL,
    -- Broker that won precedence on the last message.
    last_received_broker    VARCHAR(255) NULL,
    -- Timestamp of the most recent ON<->OFF flip.
    last_edge_at            TIMESTAMPTZ  NULL,
    -- Curtailment event from the last ON->OFF/WATCHDOG_OFF edge (audit).
    -- OFF->ON resolves this source's event via Service.ListActive matched on
    -- source_actor_id, so cross-source events are never stopped by this source.
    last_edge_event_uuid    UUID         NULL,
    -- Durable in-flight edge. Written before the curtailment service side
    -- effect and cleared only after source-state settlement succeeds, so
    -- restarts can retry/complete the edge instead of trusting stale state.
    pending_direction       TEXT         NULL,
    pending_target          TEXT         NULL,
    pending_target_at       TIMESTAMPTZ  NULL,
    pending_received_at     TIMESTAMPTZ  NULL,
    pending_received_broker VARCHAR(255) NULL,
    pending_prior_edge_at   TIMESTAMPTZ  NULL,
    -- Watchdog no-op marker for empty FULL_FLEET starts. Prevents one
    -- terminal event per tick while still allowing a later window to retry.
    last_empty_full_fleet_watchdog_ref TEXT NULL,
    updated_at              TIMESTAMPTZ  NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT fk_curtailment_mqtt_source_state_config FOREIGN KEY (source_config_id)
        REFERENCES curtailment_mqtt_source_config(id) ON DELETE CASCADE,
    CONSTRAINT ck_curtailment_mqtt_source_state_target_valid
        CHECK (last_target IS NULL OR last_target IN ('OFF', 'ON')),
    CONSTRAINT ck_curtailment_mqtt_source_state_processed_target_valid
        CHECK (last_processed_target IS NULL OR last_processed_target IN ('OFF', 'ON')),
    CONSTRAINT ck_curtailment_mqtt_source_state_processed_targets_valid
        CHECK (last_processed_targets IS NULL
            OR last_processed_targets <@ ARRAY['OFF', 'ON']::TEXT[]),
    CONSTRAINT ck_curtailment_mqtt_source_state_pending_direction_valid
        CHECK (pending_direction IS NULL
            OR pending_direction IN ('on_to_off', 'off_to_on', 'watchdog_off')),
    CONSTRAINT ck_curtailment_mqtt_source_state_pending_target_valid
        CHECK (pending_target IS NULL OR pending_target IN ('OFF', 'ON'))
);

CREATE TRIGGER update_curtailment_mqtt_source_state_updated_at
    BEFORE UPDATE ON curtailment_mqtt_source_state
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();
