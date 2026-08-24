ALTER TABLE discovered_device
    ADD COLUMN model_identity_observed_at TIMESTAMPTZ NULL;

UPDATE discovered_device
SET model_identity_observed_at = COALESCE(last_seen, updated_at, created_at)
WHERE btrim(COALESCE(manufacturer, '')) <> ''
  AND btrim(COALESCE(model, '')) <> '';

CREATE FUNCTION rollout_model_identity_v1(manufacturer TEXT, model TEXT)
RETURNS TEXT
LANGUAGE sql
IMMUTABLE
PARALLEL SAFE
AS $$
    SELECT CASE
        WHEN btrim(COALESCE(manufacturer, '')) = ''
          OR btrim(COALESCE(model, '')) = ''
            THEN NULL
        ELSE 'v1:'
            || octet_length(lower(btrim(manufacturer)))::text
            || ':' || lower(btrim(manufacturer))
            || ':' || octet_length(lower(btrim(model)))::text
            || ':' || lower(btrim(model))
    END
$$;

CREATE TABLE rollout_lane_model (
    id UUID PRIMARY KEY,
    lane_id UUID NOT NULL,
    org_id BIGINT NOT NULL,
    model_identity_key TEXT NOT NULL,
    normalization_version SMALLINT NOT NULL DEFAULT 1,
    manufacturer TEXT NOT NULL,
    model TEXT NOT NULL,
    current_channel_id BIGINT NOT NULL,
    current_release_set_id BIGINT NOT NULL,
    current_release_target_id BIGINT NOT NULL,
    revision BIGINT NOT NULL DEFAULT 1,
    origin TEXT NOT NULL DEFAULT 'topology',
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT fk_rollout_lane_model_lane
        FOREIGN KEY (lane_id, org_id)
        REFERENCES rollout_lane(id, org_id)
        ON DELETE RESTRICT,
    CONSTRAINT fk_rollout_lane_model_channel
        FOREIGN KEY (lane_id, org_id, current_channel_id)
        REFERENCES rollout_lane_channel(lane_id, org_id, channel_id)
        ON DELETE RESTRICT,
    CONSTRAINT fk_rollout_lane_model_target
        FOREIGN KEY (
            current_release_target_id,
            current_release_set_id,
            org_id
        )
        REFERENCES firmware_release_target(id, release_set_id, org_id)
        ON DELETE RESTRICT,
    CONSTRAINT uq_rollout_lane_model_id_lane_org
        UNIQUE (id, lane_id, org_id),
    CONSTRAINT uq_rollout_lane_model_identity
        UNIQUE (lane_id, model_identity_key),
    CONSTRAINT ck_rollout_lane_model_identity CHECK (
        normalization_version = 1
        AND model_identity_key = rollout_model_identity_v1(manufacturer, model)
    ),
    CONSTRAINT ck_rollout_lane_model_revision CHECK (revision > 0),
    CONSTRAINT ck_rollout_lane_model_origin CHECK (
        origin IN ('legacy_backfill', 'topology')
    )
);

CREATE TRIGGER update_rollout_lane_model_updated_at
    BEFORE UPDATE ON rollout_lane_model
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

CREATE TABLE rollout_lane_model_channel (
    lane_model_id UUID NOT NULL,
    lane_id UUID NOT NULL,
    org_id BIGINT NOT NULL,
    channel_id BIGINT NOT NULL,
    release_set_id BIGINT NOT NULL,
    release_target_id BIGINT NOT NULL,
    position INT NOT NULL,
    origin TEXT NOT NULL DEFAULT 'topology',
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT pk_rollout_lane_model_channel
        PRIMARY KEY (lane_model_id, channel_id),
    CONSTRAINT fk_rollout_lane_model_channel_model
        FOREIGN KEY (lane_model_id, lane_id, org_id)
        REFERENCES rollout_lane_model(id, lane_id, org_id)
        ON DELETE RESTRICT,
    CONSTRAINT fk_rollout_lane_model_channel_registry
        FOREIGN KEY (lane_id, org_id, channel_id)
        REFERENCES rollout_lane_channel(lane_id, org_id, channel_id)
        ON DELETE RESTRICT,
    CONSTRAINT fk_rollout_lane_model_channel_target
        FOREIGN KEY (release_target_id, release_set_id, org_id)
        REFERENCES firmware_release_target(id, release_set_id, org_id)
        ON DELETE RESTRICT,
    CONSTRAINT uq_rollout_lane_model_channel_position
        UNIQUE (lane_model_id, position),
    CONSTRAINT ck_rollout_lane_model_channel_position CHECK (position >= 0),
    CONSTRAINT ck_rollout_lane_model_channel_origin CHECK (
        origin IN ('legacy_backfill', 'topology')
    )
);

CREATE TABLE rollout_lane_model_binding (
    id UUID PRIMARY KEY,
    lane_id UUID NOT NULL,
    lane_model_id UUID NOT NULL,
    org_id BIGINT NOT NULL,
    device_id BIGINT NOT NULL,
    channel_id BIGINT NOT NULL,
    model_identity_key TEXT NOT NULL,
    model_identity_observed_at TIMESTAMPTZ NULL,
    revision BIGINT NOT NULL DEFAULT 1,
    origin TEXT NOT NULL DEFAULT 'topology',
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    ended_at TIMESTAMPTZ NULL,
    ended_by_operation_id UUID NULL,

    CONSTRAINT fk_rollout_lane_model_binding_model
        FOREIGN KEY (lane_model_id, lane_id, org_id)
        REFERENCES rollout_lane_model(id, lane_id, org_id)
        ON DELETE RESTRICT,
    CONSTRAINT fk_rollout_lane_model_binding_device
        FOREIGN KEY (device_id, org_id)
        REFERENCES device(id, org_id)
        ON DELETE RESTRICT,
    CONSTRAINT fk_rollout_lane_model_binding_registry
        FOREIGN KEY (lane_id, org_id, channel_id)
        REFERENCES rollout_lane_channel(lane_id, org_id, channel_id)
        ON DELETE RESTRICT,
    CONSTRAINT uq_rollout_lane_model_binding_id_org UNIQUE (id, org_id),
    CONSTRAINT ck_rollout_lane_model_binding_revision CHECK (revision > 0),
    CONSTRAINT ck_rollout_lane_model_binding_identity CHECK (
        model_identity_key LIKE 'v1:%'
    ),
    CONSTRAINT ck_rollout_lane_model_binding_origin CHECK (
        origin IN ('legacy_backfill', 'repair', 'topology')
    ),
    CONSTRAINT ck_rollout_lane_model_binding_end CHECK (
        (ended_at IS NULL AND ended_by_operation_id IS NULL)
        OR ended_at IS NOT NULL
    )
);

CREATE UNIQUE INDEX uq_rollout_lane_model_binding_active_device
    ON rollout_lane_model_binding(lane_id, device_id)
    WHERE ended_at IS NULL;

CREATE TABLE firmware_rollout_group (
    id UUID PRIMARY KEY,
    lane_id UUID NOT NULL,
    org_id BIGINT NOT NULL,
    name TEXT NOT NULL,
    idempotency_key VARCHAR(256) NOT NULL,
    create_fingerprint VARCHAR(64) NOT NULL,
    reason VARCHAR(256) NOT NULL,
    created_by_user_id BIGINT NOT NULL,
    actor_type TEXT NOT NULL,
    actor_credential_id TEXT NULL,
    result_revision BIGINT NOT NULL DEFAULT 0,
    terminal_outcome TEXT NULL,
    result_ready BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT fk_firmware_rollout_group_lane
        FOREIGN KEY (lane_id, org_id)
        REFERENCES rollout_lane(id, org_id)
        ON DELETE RESTRICT,
    CONSTRAINT fk_firmware_rollout_group_user
        FOREIGN KEY (created_by_user_id)
        REFERENCES "user"(id)
        ON DELETE RESTRICT,
    CONSTRAINT uq_firmware_rollout_group_id_org UNIQUE (id, org_id),
    CONSTRAINT uq_firmware_rollout_group_id_lane_org
        UNIQUE (id, lane_id, org_id),
    CONSTRAINT uq_firmware_rollout_group_idempotency
        UNIQUE (org_id, idempotency_key),
    CONSTRAINT ck_firmware_rollout_group_name CHECK (btrim(name) <> ''),
    CONSTRAINT ck_firmware_rollout_group_idempotency
        CHECK (btrim(idempotency_key) <> ''),
    CONSTRAINT ck_firmware_rollout_group_fingerprint
        CHECK (create_fingerprint ~ '^[0-9a-f]{64}$'),
    CONSTRAINT ck_firmware_rollout_group_reason CHECK (btrim(reason) <> ''),
    CONSTRAINT ck_firmware_rollout_group_result_revision
        CHECK (result_revision >= 0),
    CONSTRAINT ck_firmware_rollout_group_terminal_outcome CHECK (
        terminal_outcome IS NULL
        OR terminal_outcome IN (
            'successful',
            'reverted',
            'aborted',
            'completed_with_failures',
            'mixed'
        )
    ),
    CONSTRAINT ck_firmware_rollout_group_actor CHECK (
        actor_type = 'user'
            AND (
                actor_credential_id IS NULL
                OR btrim(actor_credential_id) <> ''
            )
        OR actor_type = 'api_key'
            AND actor_credential_id IS NOT NULL
            AND btrim(actor_credential_id) <> ''
        OR actor_type = 'system'
            AND actor_credential_id IS NULL
    )
);

CREATE TRIGGER update_firmware_rollout_group_updated_at
    BEFORE UPDATE ON firmware_rollout_group
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

CREATE FUNCTION maintain_firmware_rollout_group_result_revision()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF NEW.terminal_outcome IS DISTINCT FROM OLD.terminal_outcome
       OR NEW.result_ready IS DISTINCT FROM OLD.result_ready THEN
        NEW.result_revision := OLD.result_revision + 1;
    ELSE
        NEW.result_revision := OLD.result_revision;
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER firmware_rollout_group_result_revision
    BEFORE UPDATE ON firmware_rollout_group
    FOR EACH ROW
    EXECUTE FUNCTION maintain_firmware_rollout_group_result_revision();

CREATE TABLE firmware_rollout_group_model (
    group_id UUID NOT NULL,
    lane_id UUID NOT NULL,
    lane_model_id UUID NOT NULL,
    org_id BIGINT NOT NULL,
    model_identity_key TEXT NOT NULL,
    source_channel_id BIGINT NOT NULL,
    source_release_set_id BIGINT NOT NULL,
    source_release_target_id BIGINT NOT NULL,
    target_channel_id BIGINT NOT NULL,
    target_release_set_id BIGINT NOT NULL,
    target_release_target_id BIGINT NOT NULL,
    child_rollout_id UUID NULL,
    snapshot JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT pk_firmware_rollout_group_model
        PRIMARY KEY (group_id, lane_model_id),
    CONSTRAINT fk_firmware_rollout_group_model_group
        FOREIGN KEY (group_id, lane_id, org_id)
        REFERENCES firmware_rollout_group(id, lane_id, org_id)
        ON DELETE RESTRICT,
    CONSTRAINT fk_firmware_rollout_group_model_declaration
        FOREIGN KEY (lane_model_id, lane_id, org_id)
        REFERENCES rollout_lane_model(id, lane_id, org_id)
        ON DELETE RESTRICT,
    CONSTRAINT fk_firmware_rollout_group_model_source_channel
        FOREIGN KEY (lane_id, org_id, source_channel_id)
        REFERENCES rollout_lane_channel(lane_id, org_id, channel_id)
        ON DELETE RESTRICT,
    CONSTRAINT fk_firmware_rollout_group_model_target_channel
        FOREIGN KEY (lane_id, org_id, target_channel_id)
        REFERENCES rollout_lane_channel(lane_id, org_id, channel_id)
        ON DELETE RESTRICT,
    CONSTRAINT fk_firmware_rollout_group_model_source_target
        FOREIGN KEY (
            source_release_target_id,
            source_release_set_id,
            org_id
        )
        REFERENCES firmware_release_target(id, release_set_id, org_id)
        ON DELETE RESTRICT,
    CONSTRAINT fk_firmware_rollout_group_model_target_target
        FOREIGN KEY (
            target_release_target_id,
            target_release_set_id,
            org_id
        )
        REFERENCES firmware_release_target(id, release_set_id, org_id)
        ON DELETE RESTRICT,
    CONSTRAINT fk_firmware_rollout_group_model_child
        FOREIGN KEY (child_rollout_id, org_id)
        REFERENCES firmware_rollout(id, org_id)
        ON DELETE RESTRICT,
    CONSTRAINT uq_firmware_rollout_group_model_child UNIQUE (child_rollout_id),
    CONSTRAINT ck_firmware_rollout_group_model_snapshot
        CHECK (jsonb_typeof(snapshot) = 'object')
);

CREATE TABLE rollout_lane_active_parent (
    lane_id UUID PRIMARY KEY,
    org_id BIGINT NOT NULL,
    group_id UUID NOT NULL,
    claim_idempotency_key VARCHAR(256) NOT NULL,
    claim_fingerprint VARCHAR(64) NOT NULL,
    claimed_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT fk_rollout_lane_active_parent_lane
        FOREIGN KEY (lane_id, org_id)
        REFERENCES rollout_lane(id, org_id)
        ON DELETE RESTRICT,
    CONSTRAINT fk_rollout_lane_active_parent_group
        FOREIGN KEY (group_id, lane_id, org_id)
        REFERENCES firmware_rollout_group(id, lane_id, org_id)
        ON DELETE RESTRICT,
    CONSTRAINT uq_rollout_lane_active_parent_group UNIQUE (group_id),
    CONSTRAINT uq_rollout_lane_active_parent_key
        UNIQUE (org_id, claim_idempotency_key),
    CONSTRAINT ck_rollout_lane_active_parent_key
        CHECK (btrim(claim_idempotency_key) <> ''),
    CONSTRAINT ck_rollout_lane_active_parent_fingerprint
        CHECK (claim_fingerprint ~ '^[0-9a-f]{64}$')
);

CREATE TABLE rollout_lane_topology_cutover (
    org_id BIGINT PRIMARY KEY,
    enabled BOOLEAN NOT NULL DEFAULT FALSE,
    revision BIGINT NOT NULL DEFAULT 1,
    enabled_at TIMESTAMPTZ NULL,
    enabled_by_user_id BIGINT NULL,
    enabled_actor_type TEXT NULL,
    enabled_actor_credential_id TEXT NULL,
    enable_reason TEXT NULL,
    enable_idempotency_key VARCHAR(256) NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT fk_rollout_lane_topology_cutover_org
        FOREIGN KEY (org_id) REFERENCES organization(id) ON DELETE RESTRICT,
    CONSTRAINT fk_rollout_lane_topology_cutover_user
        FOREIGN KEY (enabled_by_user_id) REFERENCES "user"(id) ON DELETE RESTRICT,
    CONSTRAINT ck_rollout_lane_topology_cutover_revision CHECK (revision > 0),
    CONSTRAINT ck_rollout_lane_topology_cutover_state CHECK (
        (
            NOT enabled
            AND enabled_at IS NULL
            AND enabled_by_user_id IS NULL
            AND enabled_actor_type IS NULL
            AND enabled_actor_credential_id IS NULL
            AND enable_reason IS NULL
            AND enable_idempotency_key IS NULL
        )
        OR
        (
            enabled
            AND enabled_at IS NOT NULL
            AND enabled_by_user_id IS NOT NULL
            AND enabled_actor_type IS NOT NULL
            AND enable_reason IS NOT NULL
            AND btrim(enable_reason) <> ''
            AND enable_idempotency_key IS NOT NULL
            AND btrim(enable_idempotency_key) <> ''
        )
    )
);

CREATE TRIGGER update_rollout_lane_topology_cutover_updated_at
    BEFORE UPDATE ON rollout_lane_topology_cutover
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

CREATE TABLE rollout_lane_topology_admin_operation (
    id UUID PRIMARY KEY,
    org_id BIGINT NOT NULL,
    operation TEXT NOT NULL,
    lane_id UUID NULL,
    lane_model_id UUID NULL,
    device_id BIGINT NULL,
    idempotency_key VARCHAR(256) NOT NULL,
    request_fingerprint VARCHAR(64) NOT NULL,
    expected_revision BIGINT NOT NULL,
    resulting_revision BIGINT NOT NULL,
    reason VARCHAR(256) NOT NULL,
    requested JSONB NOT NULL,
    applied JSONB NOT NULL,
    actor_user_id BIGINT NOT NULL,
    actor_type TEXT NOT NULL,
    actor_credential_id TEXT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT fk_rollout_lane_topology_admin_operation_org
        FOREIGN KEY (org_id) REFERENCES organization(id) ON DELETE RESTRICT,
    CONSTRAINT fk_rollout_lane_topology_admin_operation_lane
        FOREIGN KEY (lane_id, org_id)
        REFERENCES rollout_lane(id, org_id)
        ON DELETE RESTRICT,
    CONSTRAINT fk_rollout_lane_topology_admin_operation_model
        FOREIGN KEY (lane_model_id, lane_id, org_id)
        REFERENCES rollout_lane_model(id, lane_id, org_id)
        ON DELETE RESTRICT,
    CONSTRAINT fk_rollout_lane_topology_admin_operation_device
        FOREIGN KEY (device_id, org_id)
        REFERENCES device(id, org_id)
        ON DELETE RESTRICT,
    CONSTRAINT fk_rollout_lane_topology_admin_operation_actor
        FOREIGN KEY (actor_user_id) REFERENCES "user"(id) ON DELETE RESTRICT,
    CONSTRAINT uq_rollout_lane_topology_admin_operation_id_org
        UNIQUE (id, org_id),
    CONSTRAINT uq_rollout_lane_topology_admin_operation_key
        UNIQUE (org_id, idempotency_key),
    CONSTRAINT ck_rollout_lane_topology_admin_operation_kind
        CHECK (operation IN (
            'repair_binding',
            'enable',
            'create_declaration',
            'publish_target',
            'update_membership'
        )),
    CONSTRAINT ck_rollout_lane_topology_admin_operation_key
        CHECK (btrim(idempotency_key) <> ''),
    CONSTRAINT ck_rollout_lane_topology_admin_operation_fingerprint
        CHECK (request_fingerprint ~ '^[0-9a-f]{64}$'),
    CONSTRAINT ck_rollout_lane_topology_admin_operation_revision
        CHECK (
            expected_revision >= 0
            AND resulting_revision > 0
            AND (operation = 'create_declaration') = (expected_revision = 0)
        ),
    CONSTRAINT ck_rollout_lane_topology_admin_operation_reason
        CHECK (btrim(reason) <> ''),
    CONSTRAINT ck_rollout_lane_topology_admin_operation_json
        CHECK (
            jsonb_typeof(requested) = 'object'
            AND jsonb_typeof(applied) = 'object'
        ),
    CONSTRAINT ck_rollout_lane_topology_admin_operation_actor CHECK (
        actor_type = 'user'
            AND (
                actor_credential_id IS NULL
                OR btrim(actor_credential_id) <> ''
            )
        OR actor_type = 'api_key'
            AND actor_credential_id IS NOT NULL
            AND btrim(actor_credential_id) <> ''
        OR actor_type = 'system'
            AND actor_credential_id IS NULL
    )
);

ALTER TABLE rollout_lane_model_binding
    ADD CONSTRAINT fk_rollout_lane_model_binding_end_operation
    FOREIGN KEY (ended_by_operation_id, org_id)
    REFERENCES rollout_lane_topology_admin_operation(id, org_id)
    ON DELETE RESTRICT
    DEFERRABLE INITIALLY DEFERRED;

CREATE INDEX idx_rollout_lane_model_org_lane
    ON rollout_lane_model(org_id, lane_id, model_identity_key);

CREATE INDEX idx_rollout_lane_model_channel_registry
    ON rollout_lane_model_channel(org_id, lane_id, channel_id);

CREATE INDEX idx_rollout_lane_model_binding_active_model
    ON rollout_lane_model_binding(org_id, lane_model_id, device_id)
    WHERE ended_at IS NULL;

CREATE INDEX idx_rollout_lane_model_binding_device_history
    ON rollout_lane_model_binding(org_id, device_id, created_at DESC, id);

CREATE INDEX idx_firmware_rollout_group_org_created
    ON firmware_rollout_group(org_id, created_at DESC, id DESC);

CREATE INDEX idx_rollout_lane_topology_admin_operation_lane
    ON rollout_lane_topology_admin_operation(
        org_id,
        lane_id,
        created_at DESC,
        id
    );

CREATE FUNCTION reject_rollout_lane_topology_admin_operation_mutation()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION 'rollout lane topology administration history is immutable'
        USING ERRCODE = '55000';
END;
$$;

CREATE TRIGGER rollout_lane_topology_admin_operation_immutable
    BEFORE UPDATE OR DELETE ON rollout_lane_topology_admin_operation
    FOR EACH ROW
    EXECUTE FUNCTION reject_rollout_lane_topology_admin_operation_mutation();

CREATE TRIGGER rollout_lane_topology_admin_operation_truncate_immutable
    BEFORE TRUNCATE ON rollout_lane_topology_admin_operation
    FOR EACH STATEMENT
    EXECUTE FUNCTION reject_rollout_lane_topology_admin_operation_mutation();

CREATE FUNCTION backfill_rollout_lane_model_topology(target_org_id BIGINT)
RETURNS VOID
LANGUAGE plpgsql
AS $$
BEGIN
    INSERT INTO rollout_lane_topology_cutover (org_id)
    SELECT organization.id
    FROM organization
    WHERE target_org_id IS NULL OR organization.id = target_org_id
    ON CONFLICT (org_id) DO NOTHING;

    INSERT INTO rollout_lane_channel (
        lane_id,
        org_id,
        channel_id,
        position
    )
    SELECT lane.id,
           lane.org_id,
           lane.current_channel_id,
           COALESCE((
               SELECT MAX(existing.position) + 1
               FROM rollout_lane_channel existing
               WHERE existing.lane_id = lane.id
           ), 0)
    FROM rollout_lane lane
    WHERE (target_org_id IS NULL OR lane.org_id = target_org_id)
      AND NOT EXISTS (
          SELECT 1
          FROM rollout_lane_channel existing
          WHERE existing.lane_id = lane.id
            AND existing.org_id = lane.org_id
            AND existing.channel_id = lane.current_channel_id
      )
    ORDER BY lane.id
    ON CONFLICT (lane_id, channel_id) DO NOTHING;

    INSERT INTO rollout_lane_model (
        id,
        lane_id,
        org_id,
        model_identity_key,
        manufacturer,
        model,
        current_channel_id,
        current_release_set_id,
        current_release_target_id,
        origin
    )
    SELECT md5(lane.id::text || ':' || target.model_identity_key)::uuid,
           lane.id,
           lane.org_id,
           target.model_identity_key,
           target.manufacturer,
           target.model,
           lane.current_channel_id,
           target.release_set_id,
           target.release_target_id,
           'legacy_backfill'
    FROM rollout_lane lane
    JOIN device_set_channel channel
      ON channel.device_set_id = lane.current_channel_id
     AND channel.org_id = lane.org_id
    JOIN (
        SELECT release_set_id,
               org_id,
               rollout_model_identity_v1(
                   target_manufacturer,
                   target_model
               ) AS model_identity_key,
               lower(btrim(MIN(target_manufacturer))) AS manufacturer,
               lower(btrim(MIN(target_model))) AS model,
               MIN(id) AS release_target_id
        FROM firmware_release_target
        GROUP BY release_set_id,
                 org_id,
                 rollout_model_identity_v1(
                     target_manufacturer,
                     target_model
                 )
    ) target
      ON target.release_set_id = channel.release_set_id
     AND target.org_id = channel.org_id
    WHERE target.model_identity_key IS NOT NULL
      AND (target_org_id IS NULL OR lane.org_id = target_org_id)
    ON CONFLICT (lane_id, model_identity_key) DO NOTHING;

    INSERT INTO rollout_lane_model_channel (
        lane_model_id,
        lane_id,
        org_id,
        channel_id,
        release_set_id,
        release_target_id,
        position,
        origin
    )
    SELECT model.id,
           model.lane_id,
           model.org_id,
           model.current_channel_id,
           model.current_release_set_id,
           model.current_release_target_id,
           0,
           'legacy_backfill'
    FROM rollout_lane_model model
    WHERE model.origin = 'legacy_backfill'
      AND (target_org_id IS NULL OR model.org_id = target_org_id)
    ON CONFLICT (lane_model_id, channel_id) DO NOTHING;

    INSERT INTO rollout_lane_model_binding (
        id,
        lane_id,
        lane_model_id,
        org_id,
        device_id,
        channel_id,
        model_identity_key,
        model_identity_observed_at,
        origin
    )
    SELECT md5(
               model.lane_id::text
               || ':' || device.id::text
               || ':' || model.id::text
           )::uuid,
           model.lane_id,
           model.id,
           model.org_id,
           device.id,
           membership.device_set_id,
           model.model_identity_key,
           discovered.model_identity_observed_at,
           'legacy_backfill'
    FROM rollout_lane_model model
    JOIN device_set_membership membership
      ON membership.device_set_id = model.current_channel_id
     AND membership.org_id = model.org_id
     AND membership.device_set_type = 'channel'
    JOIN device
      ON device.id = membership.device_id
     AND device.org_id = membership.org_id
     AND device.deleted_at IS NULL
    JOIN discovered_device discovered
      ON discovered.id = device.discovered_device_id
     AND discovered.org_id = device.org_id
     AND discovered.deleted_at IS NULL
    WHERE model.model_identity_key = rollout_model_identity_v1(
              discovered.manufacturer,
              discovered.model
          )
      AND (
          SELECT COUNT(*)
          FROM firmware_release_target target
          WHERE target.release_set_id = model.current_release_set_id
            AND target.org_id = model.org_id
            AND rollout_model_identity_v1(
                target.target_manufacturer,
                target.target_model
            ) = model.model_identity_key
      ) = 1
      AND (target_org_id IS NULL OR model.org_id = target_org_id)
    ON CONFLICT (lane_id, device_id) WHERE ended_at IS NULL DO NOTHING;
END;
$$;

SELECT backfill_rollout_lane_model_topology(NULL);

CREATE VIEW rollout_lane_topology_anomaly AS
WITH lane_members AS (
    SELECT lane.id AS lane_id,
           lane.org_id,
           lane.current_channel_id,
           membership.device_set_id AS channel_id,
           device.id AS device_id,
           device.device_identifier,
           discovered.manufacturer,
           discovered.model,
           discovered.model_identity_observed_at,
           rollout_model_identity_v1(
               discovered.manufacturer,
               discovered.model
           ) AS model_identity_key,
           channel.release_set_id
    FROM rollout_lane lane
    JOIN rollout_lane_channel registry
      ON registry.lane_id = lane.id
     AND registry.org_id = lane.org_id
    JOIN device_set_membership membership
      ON membership.device_set_id = registry.channel_id
     AND membership.org_id = registry.org_id
     AND membership.device_set_type = 'channel'
    JOIN device
      ON device.id = membership.device_id
     AND device.org_id = membership.org_id
     AND device.deleted_at IS NULL
    LEFT JOIN discovered_device discovered
      ON discovered.id = device.discovered_device_id
     AND discovered.org_id = device.org_id
     AND discovered.deleted_at IS NULL
    JOIN device_set_channel channel
      ON channel.device_set_id = lane.current_channel_id
     AND channel.org_id = lane.org_id
),
member_matches AS (
    SELECT member.*,
           COUNT(target.id) AS target_match_count,
           MIN(declaration.id::text)::uuid AS lane_model_id,
           MIN(declaration.revision) AS lane_model_revision
    FROM lane_members member
    LEFT JOIN firmware_release_target target
      ON target.release_set_id = member.release_set_id
     AND target.org_id = member.org_id
     AND rollout_model_identity_v1(
         target.target_manufacturer,
         target.target_model
     ) = member.model_identity_key
    LEFT JOIN rollout_lane_model declaration
      ON declaration.lane_id = member.lane_id
     AND declaration.org_id = member.org_id
     AND declaration.model_identity_key = member.model_identity_key
    GROUP BY member.lane_id,
             member.org_id,
             member.current_channel_id,
             member.channel_id,
             member.device_id,
             member.device_identifier,
             member.manufacturer,
             member.model,
             member.model_identity_observed_at,
             member.model_identity_key,
             member.release_set_id
),
anomalies AS (
    SELECT md5('null_identity:' || lane_id::text || ':' || device_id::text)::uuid
               AS anomaly_id,
           org_id,
           lane_id,
           device_id,
           device_identifier,
           NULL::uuid AS lane_model_id,
           NULL::bigint AS lane_model_revision,
           'null_identity'::text AS anomaly_type,
           ARRAY['confirm_identity', 'rerun_backfill']::text[]
               AS supported_repair_actions,
           jsonb_build_object(
               'manufacturer', COALESCE(manufacturer, ''),
               'model', COALESCE(model, '')
           ) AS details
    FROM member_matches
    WHERE model_identity_key IS NULL

    UNION ALL

    SELECT md5('ambiguous_target_match:' || lane_id::text || ':' || device_id::text)::uuid,
           org_id,
           lane_id,
           device_id,
           device_identifier,
           lane_model_id,
           lane_model_revision,
           'ambiguous_target_match',
           ARRAY['select_declaration', 'repair_binding', 'rerun_backfill']::text[],
           jsonb_build_object(
               'model_identity_key', model_identity_key,
               'target_match_count', target_match_count
           )
    FROM member_matches
    WHERE model_identity_key IS NOT NULL
      AND target_match_count > 1
      AND NOT EXISTS (
          SELECT 1
          FROM rollout_lane_model_binding binding
          WHERE binding.lane_id = member_matches.lane_id
            AND binding.org_id = member_matches.org_id
            AND binding.device_id = member_matches.device_id
            AND binding.lane_model_id = member_matches.lane_model_id
            AND binding.ended_at IS NULL
      )

    UNION ALL

    SELECT md5('no_target_match:' || lane_id::text || ':' || device_id::text)::uuid,
           org_id,
           lane_id,
           device_id,
           device_identifier,
           lane_model_id,
           lane_model_revision,
           'no_target_match',
           ARRAY['confirm_identity', 'rerun_backfill']::text[],
           jsonb_build_object('model_identity_key', model_identity_key)
    FROM member_matches
    WHERE model_identity_key IS NOT NULL
      AND target_match_count = 0

    UNION ALL

    SELECT md5('physical_mismatch:' || lane_id::text || ':' || device_id::text)::uuid,
           org_id,
           lane_id,
           device_id,
           device_identifier,
           lane_model_id,
           lane_model_revision,
           'physical_mismatch',
           ARRAY['repair_physical_membership', 'rerun_backfill']::text[],
           jsonb_build_object(
               'current_channel_id', current_channel_id,
               'physical_channel_id', channel_id
           )
    FROM member_matches
    WHERE model_identity_key IS NOT NULL
      AND target_match_count = 1
      AND channel_id <> current_channel_id

    UNION ALL

    SELECT md5('missing_binding:' || match.lane_id::text || ':' || match.device_id::text)::uuid,
           match.org_id,
           match.lane_id,
           match.device_id,
           match.device_identifier,
           match.lane_model_id,
           match.lane_model_revision,
           'missing_binding',
           ARRAY['repair_binding', 'rerun_backfill']::text[],
           jsonb_build_object('model_identity_key', match.model_identity_key)
    FROM member_matches match
    WHERE match.model_identity_key IS NOT NULL
      AND match.target_match_count = 1
      AND match.channel_id = match.current_channel_id
      AND NOT EXISTS (
          SELECT 1
          FROM rollout_lane_model_binding binding
          WHERE binding.lane_id = match.lane_id
            AND binding.org_id = match.org_id
            AND binding.device_id = match.device_id
            AND binding.ended_at IS NULL
      )

    UNION ALL

    SELECT md5('physical_mismatch:' || binding.lane_id::text || ':' || binding.device_id::text)::uuid,
           binding.org_id,
           binding.lane_id,
           binding.device_id,
           device.device_identifier,
           binding.lane_model_id,
           declaration.revision,
           'physical_mismatch',
           ARRAY['repair_physical_membership', 'end_stale_binding']::text[],
           jsonb_build_object(
               'binding_channel_id', binding.channel_id,
               'declaration_channel_id', declaration.current_channel_id
           )
    FROM rollout_lane_model_binding binding
    JOIN rollout_lane_model declaration
      ON declaration.id = binding.lane_model_id
     AND declaration.lane_id = binding.lane_id
     AND declaration.org_id = binding.org_id
    JOIN device
      ON device.id = binding.device_id
     AND device.org_id = binding.org_id
    WHERE binding.ended_at IS NULL
      AND (
          binding.channel_id <> declaration.current_channel_id
          OR NOT EXISTS (
              SELECT 1
              FROM device_set_membership membership
              WHERE membership.org_id = binding.org_id
                AND membership.device_id = binding.device_id
                AND membership.device_set_type = 'channel'
                AND membership.device_set_id = binding.channel_id
          )
      )

    UNION ALL

    SELECT md5(
               'duplicate_active_binding:'
               || binding.lane_id::text
               || ':' || binding.device_id::text
           )::uuid,
           binding.org_id,
           binding.lane_id,
           binding.device_id,
           MIN(device.device_identifier),
           NULL::uuid,
           NULL::bigint,
           'duplicate_active_binding',
           ARRAY['end_stale_binding']::text[],
           jsonb_build_object('active_binding_count', COUNT(*))
    FROM rollout_lane_model_binding binding
    JOIN device
      ON device.id = binding.device_id
     AND device.org_id = binding.org_id
    WHERE binding.ended_at IS NULL
    GROUP BY binding.org_id, binding.lane_id, binding.device_id
    HAVING COUNT(*) > 1
)
SELECT DISTINCT ON (org_id, anomaly_id)
       anomaly_id,
       org_id,
       lane_id,
       device_id,
       device_identifier,
       lane_model_id,
       lane_model_revision,
       anomaly_type,
       supported_repair_actions,
       details
FROM anomalies
ORDER BY org_id, anomaly_id, anomaly_type;

CREATE FUNCTION validate_rollout_lane_model_binding()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF NEW.ended_at IS NULL AND NOT EXISTS (
        SELECT 1
        FROM rollout_lane_model declaration
        JOIN device_set_membership membership
          ON membership.org_id = declaration.org_id
         AND membership.device_id = NEW.device_id
         AND membership.device_set_type = 'channel'
         AND membership.device_set_id = declaration.current_channel_id
        WHERE declaration.id = NEW.lane_model_id
          AND declaration.lane_id = NEW.lane_id
          AND declaration.org_id = NEW.org_id
          AND declaration.current_channel_id = NEW.channel_id
          AND declaration.model_identity_key = NEW.model_identity_key
    ) THEN
        RAISE EXCEPTION
            'active rollout lane model binding must match physical membership and declaration'
            USING ERRCODE = '23514';
    END IF;
    IF NEW.ended_at IS NOT NULL AND EXISTS (
        SELECT 1
        FROM rollout_lane_topology_cutover cutover
        JOIN device_set_membership membership
          ON membership.org_id = NEW.org_id
         AND membership.device_id = NEW.device_id
         AND membership.device_set_type = 'channel'
         AND membership.device_set_id = NEW.channel_id
        WHERE cutover.org_id = NEW.org_id
          AND cutover.enabled
    ) THEN
        RAISE EXCEPTION
            'ended rollout lane model binding cannot retain physical membership'
            USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;

CREATE CONSTRAINT TRIGGER rollout_lane_model_binding_consistent
    AFTER INSERT OR UPDATE ON rollout_lane_model_binding
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW
    EXECUTE FUNCTION validate_rollout_lane_model_binding();

CREATE FUNCTION validate_enabled_rollout_lane_physical_membership()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    affected_org_id BIGINT;
    affected_device_id BIGINT;
BEGIN
    affected_org_id := COALESCE(NEW.org_id, OLD.org_id);
    affected_device_id := COALESCE(NEW.device_id, OLD.device_id);

    IF EXISTS (
        SELECT 1
        FROM rollout_lane_topology_cutover cutover
        WHERE cutover.org_id = affected_org_id
          AND cutover.enabled
    ) AND EXISTS (
        SELECT 1
        FROM rollout_lane_channel registry
        JOIN device_set_membership membership
          ON membership.device_set_id = registry.channel_id
         AND membership.org_id = registry.org_id
         AND membership.device_set_type = 'channel'
        WHERE registry.org_id = affected_org_id
          AND membership.device_id = affected_device_id
          AND NOT EXISTS (
              SELECT 1
              FROM rollout_lane_model_binding binding
              WHERE binding.lane_id = registry.lane_id
                AND binding.org_id = registry.org_id
                AND binding.device_id = affected_device_id
                AND binding.channel_id = membership.device_set_id
                AND binding.ended_at IS NULL
          )
    ) THEN
        RAISE EXCEPTION
            'rollout lane physical membership requires a matching active model binding'
            USING ERRCODE = '23514';
    END IF;
    IF EXISTS (
        SELECT 1
        FROM rollout_lane_topology_cutover cutover
        JOIN rollout_lane_model_binding binding
          ON binding.org_id = cutover.org_id
         AND binding.device_id = affected_device_id
         AND binding.ended_at IS NULL
        WHERE cutover.org_id = affected_org_id
          AND cutover.enabled
          AND NOT EXISTS (
              SELECT 1
              FROM device_set_membership membership
              WHERE membership.org_id = binding.org_id
                AND membership.device_id = binding.device_id
                AND membership.device_set_type = 'channel'
                AND membership.device_set_id = binding.channel_id
          )
    ) THEN
        RAISE EXCEPTION
            'active rollout lane model binding requires matching physical membership'
            USING ERRCODE = '23514';
    END IF;
    RETURN NULL;
END;
$$;

CREATE CONSTRAINT TRIGGER rollout_lane_physical_membership_consistent
    AFTER INSERT OR UPDATE OR DELETE ON device_set_membership
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW
    EXECUTE FUNCTION validate_enabled_rollout_lane_physical_membership();
