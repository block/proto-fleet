-- Repository consolidation after shared baseline 1000.
-- The migration runner applies the numbered steps at their original checkpoints.
-- Keep the step markers and SQL bytes intact.

-- fleet-migration: 001001_internal_baseline.up.sql
-- requires-shared: 1000
-- Fresh internal installations only. Existing databases use the checked offline
-- reconciliation procedure; this file must never be replayed against them.
-- Final private schema through legacy internal 158. Obsolete snapshots are not
-- created; the reconciliation procedure preserves any existing snapshot data.

ALTER TABLE discovered_device
ADD COLUMN device_class INTEGER NOT NULL DEFAULT 0 CHECK (device_class IN (0, 1, 2));

-- Plant history is retained independently of latest inventory observations.
CREATE TABLE container_plant_metrics (
 device_id BIGINT NOT NULL REFERENCES device(id) ON DELETE CASCADE,
 time TIMESTAMPTZ NOT NULL,
 metrics JSONB NOT NULL CHECK (jsonb_typeof(metrics) = 'object'),
 PRIMARY KEY (device_id, time)
);
SELECT create_hypertable('container_plant_metrics', 'time');
SELECT add_retention_policy('container_plant_metrics', INTERVAL '30 days');

-- The controller remains the only paired/polled device. Child IDs are Fleet
-- identities, independent of firmware selectors and reported serial numbers.
CREATE TABLE container_inventory (
    device_id BIGINT PRIMARY KEY REFERENCES device(id),
    observed_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE container_tank (
    id BIGSERIAL PRIMARY KEY,
    device_id BIGINT NOT NULL REFERENCES container_inventory(device_id),
    position INT NOT NULL CHECK (position > 0),
    present BOOLEAN NOT NULL DEFAULT TRUE,
    first_seen_at TIMESTAMPTZ NOT NULL,
    last_seen_at TIMESTAMPTZ NOT NULL,
    UNIQUE (device_id, position)
);

CREATE TABLE container_module (
    id BIGSERIAL PRIMARY KEY,
    tank_id BIGINT NOT NULL REFERENCES container_tank(id),
    position INT NOT NULL CHECK (position > 0),
    reported_id TEXT NOT NULL DEFAULT '',
    first_seen_at TIMESTAMPTZ NOT NULL,
    last_seen_at TIMESTAMPTZ NOT NULL,
    retired_at TIMESTAMPTZ,
    state TEXT NOT NULL DEFAULT 'unknown',
    state_observed_at TIMESTAMPTZ,
    CHECK (state IN ('unknown', 'hashing', 'attention', 'offline', 'sleeping', 'isolated'))
);
CREATE UNIQUE INDEX container_module_current_position ON container_module(tank_id, position) WHERE retired_at IS NULL;

CREATE TABLE container_module_metrics (
    module_id BIGINT NOT NULL REFERENCES container_module(id),
    time TIMESTAMPTZ NOT NULL,
    collected_at TIMESTAMPTZ NOT NULL,
    hashrate_hs DOUBLE PRECISION,
    power_w DOUBLE PRECISION,
    temperature_c DOUBLE PRECISION,
    PRIMARY KEY (module_id, time, collected_at)
);
SELECT create_hypertable('container_module_metrics', 'time');
SELECT add_retention_policy('container_module_metrics', INTERVAL '30 days');

-- Latest values survive history retention and avoid a history scan per list row.
CREATE TABLE container_module_latest_metrics (
    module_id BIGINT PRIMARY KEY REFERENCES container_module(id),
    time TIMESTAMPTZ NOT NULL,
    collected_at TIMESTAMPTZ NOT NULL,
    hashrate_hs DOUBLE PRECISION,
    power_w DOUBLE PRECISION,
    temperature_c DOUBLE PRECISION
);

-- Shared read projection, not a shared physical-entity lifecycle. In particular,
-- modules are never inserted into device and are never separately polled.
CREATE VIEW fleet_mining_asset AS
SELECT 5::int AS kind, d.id, d.org_id, d.site_id,
       COALESCE(NULLIF(d.custom_name, ''), concat_ws(' ', dd.manufacturer, dd.model))::text AS name,
       d.device_identifier::text AS device_identifier, 0::int AS position, COALESCE(d.serial_number, '')::text AS reported_id,
       1::int AS facility_kind, COALESCE(d.building_id, rack.building_id) AS facility_id,
       3::int AS group_kind, rack.device_set_id AS group_id,
       CASE WHEN ds.status = 'OFFLINE' OR (ds.status IS NULL AND dp.pairing_status IN ('PAIRED','DEFAULT_PASSWORD')) OR (fn.last_seen_at < now() - INTERVAL '2 minutes' AND fn.enrollment_status = 'CONFIRMED' AND fn.deleted_at IS NULL) THEN 'offline'
            WHEN ds.status IN ('MAINTENANCE', 'INACTIVE') AND dp.pairing_status <> 'AUTHENTICATION_NEEDED' THEN 'sleeping'
            WHEN ds.status IN ('ERROR', 'NEEDS_MINING_POOL', 'UPDATING', 'REBOOT_REQUIRED') OR dp.pairing_status='AUTHENTICATION_NEEDED' OR EXISTS (SELECT 1 FROM errors e WHERE e.device_id=d.id AND e.org_id=d.org_id AND e.closed_at IS NULL AND e.severity IN (1,2,3,4)) THEN 'attention'
            WHEN ds.status = 'ACTIVE' THEN 'hashing' ELSE 'unknown' END::text AS state,
       dm.time AS observed_at, dm.hash_rate_hs AS hashrate_hs, dm.power_w, dm.temp_c AS temperature_c, (dm.efficiency_jh * 1e12)::double precision AS miner_efficiency_jth
FROM device d
JOIN discovered_device dd ON dd.id = d.discovered_device_id AND dd.org_id = d.org_id
JOIN device_pairing dp ON dp.device_id = d.id
LEFT JOIN device_status ds ON ds.device_id = d.id
LEFT JOIN fleet_node_device fnd ON fnd.device_id = d.id AND fnd.org_id = d.org_id
LEFT JOIN fleet_node fn ON fn.id = fnd.fleet_node_id
LEFT JOIN LATERAL (
  SELECT r.device_set_id, r.building_id
  FROM device_set_membership ms JOIN device_set s ON s.id = ms.device_set_id AND s.deleted_at IS NULL
  JOIN device_set_rack r ON r.device_set_id = s.id
  WHERE ms.device_id = d.id AND ms.device_set_type = 'rack' AND ms.org_id = d.org_id
) rack ON TRUE
LEFT JOIN LATERAL (
  SELECT time, hash_rate_hs, power_w, temp_c, efficiency_jh FROM device_metrics
  WHERE device_identifier = d.device_identifier AND time > now() - INTERVAL '10 minutes'
    AND time <= now() + INTERVAL '5 seconds'
    AND ds.status IS DISTINCT FROM 'OFFLINE'
    AND NOT COALESCE(fn.last_seen_at < now() - INTERVAL '2 minutes' AND fn.enrollment_status = 'CONFIRMED' AND fn.deleted_at IS NULL, FALSE)
  ORDER BY time DESC LIMIT 1
) dm ON TRUE
WHERE d.deleted_at IS NULL AND dd.deleted_at IS NULL AND dd.is_active AND dd.device_class <> 2
 AND dp.pairing_status IN ('PAIRED','AUTHENTICATION_NEEDED','DEFAULT_PASSWORD')
UNION ALL
SELECT 6::int, m.id, d.org_id, d.site_id,
       (COALESCE(NULLIF(d.custom_name, ''), 'Container ' || COALESCE(NULLIF(d.serial_number, ''), d.device_identifier)) || ' T' || t.position || ' M' || m.position)::text,
       d.device_identifier::text, m.position, m.reported_id,
       2::int, d.id, 4::int, t.id,
       CASE WHEN m.state_observed_at > now() - INTERVAL '2 minutes' AND m.state_observed_at <= now() + INTERVAL '5 seconds'
              AND m.last_seen_at > now() - INTERVAL '2 minutes' THEN m.state ELSE 'unknown' END::text,
       mm.time, mm.hashrate_hs, mm.power_w, mm.temperature_c, NULL::double precision
FROM container_module m JOIN container_tank t ON t.id = m.tank_id
JOIN device d ON d.id = t.device_id
JOIN discovered_device dd ON dd.id = d.discovered_device_id AND dd.org_id = d.org_id
LEFT JOIN container_module_latest_metrics mm ON mm.module_id = m.id
  AND mm.time > now() - INTERVAL '2 minutes' AND mm.time <= now() + INTERVAL '5 seconds'
  AND m.last_seen_at > now() - INTERVAL '2 minutes' AND m.state <> 'offline'
WHERE m.retired_at IS NULL AND t.present AND d.deleted_at IS NULL AND dd.deleted_at IS NULL;

-- Latest observations only; equipment belongs to its paired controller.
CREATE TABLE container_fan_observation (
 device_id bigint NOT NULL REFERENCES device(id) ON DELETE CASCADE,
 component_id text NOT NULL,
 observed_at timestamptz NOT NULL,
 speed_percent double precision CHECK (speed_percent BETWEEN 0 AND 100),
 power_w double precision CHECK (power_w >= 0 AND power_w < 'Infinity'::double precision),
 direction text,
 alarm_active boolean,
 PRIMARY KEY (device_id, component_id)
);
CREATE TABLE container_controls_observation (
 device_id bigint PRIMARY KEY REFERENCES device(id) ON DELETE CASCADE,
 observed_at timestamptz NOT NULL,
 supported boolean,
 main_circuit_breaker_supported boolean,
 main_circuit_breaker_closed boolean,
 alarm_reset_supported boolean,
 alarm_mute_supported boolean,
 dry_cooler_fan_reverse_supported boolean
);
CREATE TABLE container_control_observation (
 device_id bigint NOT NULL REFERENCES container_controls_observation(device_id) ON DELETE CASCADE,
 name text NOT NULL,
 enabled boolean,
 PRIMARY KEY (device_id, name)
);
CREATE TABLE container_setpoint_observation (
 device_id bigint NOT NULL REFERENCES container_controls_observation(device_id) ON DELETE CASCADE,
 name text NOT NULL,
 value double precision,
 unit text NOT NULL,
 PRIMARY KEY (device_id, name)
);

-- fleet-migration: 001002_internal_container_module_issues.up.sql
-- requires-shared: 1000
-- Current evidence only. Retain the source watermark when evidence disappears.
ALTER TABLE container_module
    ADD COLUMN issues jsonb,
    ADD COLUMN issues_observed_at timestamptz,
    ADD COLUMN issues_source_times jsonb NOT NULL DEFAULT '{}'::jsonb;

-- fleet-migration: 001003_internal_hosted_accounts.up.sql
-- requires-shared: 1000
-- Account membership is separate from organization RBAC. This migration does
-- not assign organization roles or customer memberships to existing users.
-- Take the DDL fence before the ADMIN snapshot, including in-flight organization
-- creation and blocking new inserts until the migration transaction commits.
LOCK TABLE organization IN SHARE ROW EXCLUSIVE MODE;

INSERT INTO permission (key, description) VALUES
    ('account:manage', 'Create customer accounts and control delegation beneath the host account. Does not grant private customer views or pool overrides.')
ON CONFLICT (key) DO UPDATE SET description = EXCLUDED.description;

-- ADMIN is seeded once; catalog growth only reaches newly created roles.
-- Match the new-role seed without changing custom or deleted roles.
INSERT INTO role_permission (role_id, permission_id)
SELECT r.id, p.id
FROM role r CROSS JOIN permission p
WHERE r.builtin_key = 'ADMIN'
  AND r.deleted_at IS NULL
  AND p.key = 'account:manage'
ON CONFLICT (role_id, permission_id) DO NOTHING;

CREATE TABLE hosted_account (
    id BIGSERIAL PRIMARY KEY,
    account_id VARCHAR(36) NOT NULL UNIQUE,
    organization_id BIGINT NOT NULL REFERENCES organization(id) ON DELETE CASCADE,
    parent_id BIGINT,
    depth INTEGER NOT NULL CHECK (depth BETWEEN 0 AND 2),
    name VARCHAR(255) NOT NULL CHECK (length(btrim(name)) > 0),
    capabilities BIGINT NOT NULL CHECK (capabilities BETWEEN 1 AND 31),
    suspended BOOLEAN NOT NULL DEFAULT FALSE,
    revision BIGINT NOT NULL DEFAULT 1 CHECK (revision > 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (organization_id, id),
    FOREIGN KEY (organization_id, parent_id)
        REFERENCES hosted_account(organization_id, id) ON DELETE CASCADE,
    CHECK ((depth = 0 AND parent_id IS NULL) OR (depth > 0 AND parent_id IS NOT NULL))
);

CREATE UNIQUE INDEX hosted_account_one_root ON hosted_account(organization_id) WHERE depth = 0;
CREATE INDEX hosted_account_parent ON hosted_account(organization_id, parent_id);

CREATE FUNCTION check_hosted_account_hierarchy() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'UPDATE' AND
        (NEW.organization_id, NEW.parent_id, NEW.depth) IS DISTINCT FROM
        (OLD.organization_id, OLD.parent_id, OLD.depth) THEN
        RAISE EXCEPTION 'account organization and parent cannot change' USING ERRCODE = '23514';
    END IF;
    IF NEW.depth > 0 AND NOT EXISTS (
        SELECT 1 FROM hosted_account parent
        WHERE parent.organization_id = NEW.organization_id
          AND parent.id = NEW.parent_id AND parent.depth = NEW.depth - 1
    ) THEN
        RAISE EXCEPTION 'invalid account hierarchy' USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER hosted_account_hierarchy
    BEFORE INSERT OR UPDATE ON hosted_account
    FOR EACH ROW EXECUTE FUNCTION check_hosted_account_hierarchy();

CREATE TABLE hosted_account_membership (
    organization_id BIGINT NOT NULL,
    account_id BIGINT NOT NULL,
    user_id BIGINT NOT NULL,
    role VARCHAR(32) NOT NULL CHECK (role IN ('viewer', 'pool_operator', 'account_administrator')),
    revision BIGINT NOT NULL DEFAULT 1 CHECK (revision > 0),
    revoked_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (organization_id, account_id, user_id),
    FOREIGN KEY (organization_id, account_id)
        REFERENCES hosted_account(organization_id, id) ON DELETE CASCADE,
    FOREIGN KEY (user_id, organization_id)
        REFERENCES user_organization(user_id, organization_id) ON DELETE CASCADE
);

CREATE INDEX hosted_account_membership_user
    ON hosted_account_membership(organization_id, user_id) WHERE revoked_at IS NULL;

INSERT INTO hosted_account (account_id, organization_id, depth, name, capabilities)
SELECT gen_random_uuid()::text, id, 0,
    COALESCE(NULLIF(btrim(name), ''), 'Host ' || id::text), 31 FROM organization;

-- Organizations created after the migration also receive exactly one root.
CREATE FUNCTION create_hosted_account_root() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    INSERT INTO hosted_account (account_id, organization_id, depth, name, capabilities)
    VALUES (gen_random_uuid()::text, NEW.id, 0,
        COALESCE(NULLIF(btrim(NEW.name), ''), 'Host ' || NEW.id::text), 31);
    RETURN NEW;
END;
$$;

CREATE TRIGGER organization_hosted_account_root
    AFTER INSERT ON organization
    FOR EACH ROW EXECUTE FUNCTION create_hosted_account_root();

-- fleet-migration: 001004_internal_hashboards.up.sql
-- requires-shared: 1000
-- Hardware survives module replacement and movement between slots/controllers.
CREATE TABLE hashboard (
    id BIGSERIAL PRIMARY KEY,
    org_id BIGINT NOT NULL REFERENCES organization(id),
    first_seen_at TIMESTAMPTZ NOT NULL,
    last_seen_at TIMESTAMPTZ NOT NULL
);
CREATE UNIQUE INDEX hashboard_org_id_id ON hashboard(org_id, id);

CREATE TABLE hashboard_identity (
    org_id BIGINT NOT NULL,
    kind TEXT NOT NULL CHECK (kind IN ('mac', 'serial')),
    value TEXT NOT NULL CHECK (value <> ''),
    hashboard_id BIGINT NOT NULL,
    PRIMARY KEY (org_id, kind, value),
    FOREIGN KEY (org_id, hashboard_id) REFERENCES hashboard(org_id, id)
);
CREATE INDEX hashboard_identity_board ON hashboard_identity(hashboard_id);

CREATE TABLE hashboard_installation (
    id BIGSERIAL PRIMARY KEY,
    hashboard_id BIGINT NOT NULL REFERENCES hashboard(id),
    module_id BIGINT NOT NULL REFERENCES container_module(id),
    position INT NOT NULL CHECK (position BETWEEN 1 AND 3),
    -- Historical access follows the site when installed, not the board's next site.
    site_id BIGINT,
    installed_at TIMESTAMPTZ NOT NULL,
    last_seen_at TIMESTAMPTZ NOT NULL,
    removed_at TIMESTAMPTZ,
    metadata JSONB NOT NULL DEFAULT '{}',
    CHECK (removed_at IS NULL OR removed_at >= installed_at)
);
CREATE UNIQUE INDEX hashboard_current_board ON hashboard_installation(hashboard_id) WHERE removed_at IS NULL;
CREATE UNIQUE INDEX hashboard_current_slot ON hashboard_installation(module_id, position) WHERE removed_at IS NULL;
CREATE INDEX hashboard_installation_history ON hashboard_installation(hashboard_id, installed_at);

CREATE TABLE hashboard_slot (
    module_id BIGINT NOT NULL REFERENCES container_module(id),
    hashboard_id BIGINT REFERENCES hashboard(id),
    position INT NOT NULL CHECK (position BETWEEN 1 AND 3),
    unavailable_reason TEXT NOT NULL DEFAULT '',
    occupancy TEXT NOT NULL CHECK (occupancy IN ('occupied', 'empty', 'unknown')),
    observed_at TIMESTAMPTZ NOT NULL,
    health_observed_at TIMESTAMPTZ,
    state TEXT NOT NULL DEFAULT 'unknown',
    state_observed_at TIMESTAMPTZ,
    mining_state TEXT NOT NULL DEFAULT '',
    isolated BOOLEAN,
    PRIMARY KEY (module_id, position)
);

CREATE TABLE hashboard_metrics (
    hashboard_id BIGINT NOT NULL REFERENCES hashboard(id),
    installation_id BIGINT NOT NULL REFERENCES hashboard_installation(id),
    time TIMESTAMPTZ NOT NULL,
    collected_at TIMESTAMPTZ NOT NULL,
    state TEXT NOT NULL,
    state_observed_at TIMESTAMPTZ,
    mining_state TEXT NOT NULL,
    isolated BOOLEAN,
    hashrate_hs DOUBLE PRECISION,
    temperature_c DOUBLE PRECISION,
    hashrail_ok BOOLEAN,
    refclks_ok BOOLEAN,
    -- All MDK health fields, in firmware units. Shared PSU evidence is not board power.
    health JSONB,
    PRIMARY KEY (hashboard_id, time, collected_at)
);
SELECT create_hypertable('hashboard_metrics', 'time', chunk_time_interval => INTERVAL '1 day');
SELECT add_retention_policy('hashboard_metrics', INTERVAL '30 days');

CREATE TABLE hashboard_latest_metrics (
    hashboard_id BIGINT PRIMARY KEY REFERENCES hashboard(id),
    installation_id BIGINT NOT NULL REFERENCES hashboard_installation(id),
    time TIMESTAMPTZ NOT NULL,
    collected_at TIMESTAMPTZ NOT NULL,
    state TEXT NOT NULL,
    state_observed_at TIMESTAMPTZ,
    mining_state TEXT NOT NULL,
    isolated BOOLEAN,
    hashrate_hs DOUBLE PRECISION,
    temperature_c DOUBLE PRECISION,
    hashrail_ok BOOLEAN,
    refclks_ok BOOLEAN,
    health JSONB
);

-- fleet-migration: 001005_internal_hosted_account_administration.up.sql
-- requires-shared: 1000
-- Customer identity affiliation grants no organization role or permissions.
ALTER TABLE user_organization ALTER COLUMN role_id DROP NOT NULL;
ALTER TABLE hosted_account ADD COLUMN sites_restricted BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE hosted_account ADD COLUMN site_ceiling_restricted BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE hosted_account ADD COLUMN authority_revision BIGINT NOT NULL DEFAULT 1 CHECK (authority_revision > 0);

CREATE TABLE hosted_account_site (
    organization_id BIGINT NOT NULL,
    account_id BIGINT NOT NULL,
    site_id BIGINT NOT NULL,
    parent_controlled BOOLEAN NOT NULL DEFAULT FALSE,
    PRIMARY KEY (organization_id, account_id, parent_controlled, site_id),
    FOREIGN KEY (organization_id, account_id) REFERENCES hosted_account(organization_id, id) ON DELETE CASCADE,
    FOREIGN KEY (site_id, organization_id) REFERENCES site(id, org_id) ON DELETE CASCADE
);

CREATE TABLE hosted_account_ancestor_grant (
    organization_id BIGINT NOT NULL,
    account_id BIGINT NOT NULL,
    ancestor_id BIGINT NOT NULL,
    capabilities BIGINT NOT NULL CHECK (capabilities BETWEEN 1 AND 31),
    revision BIGINT NOT NULL DEFAULT 1 CHECK (revision > 0),
    revoked_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (organization_id, account_id, ancestor_id),
    FOREIGN KEY (organization_id, account_id) REFERENCES hosted_account(organization_id, id) ON DELETE CASCADE,
    FOREIGN KEY (organization_id, ancestor_id) REFERENCES hosted_account(organization_id, id) ON DELETE CASCADE,
    CHECK (account_id <> ancestor_id)
);

CREATE FUNCTION check_hosted_account_ancestor_grant() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM hosted_account target
        JOIN hosted_account ancestor ON ancestor.organization_id = target.organization_id
            AND ancestor.id = NEW.ancestor_id
        WHERE target.organization_id = NEW.organization_id AND target.id = NEW.account_id
            AND (ancestor.depth = 0 OR target.parent_id = ancestor.id)
            AND ancestor.depth < target.depth
    ) THEN
        RAISE EXCEPTION 'grant requires a strict same-organization ancestor' USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER hosted_account_ancestor_grant_hierarchy BEFORE INSERT OR UPDATE
    ON hosted_account_ancestor_grant FOR EACH ROW EXECUTE FUNCTION check_hosted_account_ancestor_grant();

CREATE TABLE hosted_account_invitation (
    invitation_id VARCHAR(36) PRIMARY KEY,
    organization_id BIGINT NOT NULL,
    account_id BIGINT NOT NULL,
    inviter_id BIGINT NOT NULL,
    authority_fingerprint VARCHAR(64) NOT NULL,
    token_hash VARCHAR(64) NOT NULL UNIQUE,
    role VARCHAR(32) NOT NULL CHECK (role IN ('viewer', 'pool_operator', 'account_administrator')),
    expires_at TIMESTAMPTZ NOT NULL,
    revoked_at TIMESTAMPTZ,
    accepted_at TIMESTAMPTZ,
    accepted_user_id BIGINT REFERENCES "user"(id) ON DELETE RESTRICT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (organization_id, account_id) REFERENCES hosted_account(organization_id, id) ON DELETE CASCADE,
    FOREIGN KEY (inviter_id, organization_id) REFERENCES user_organization(user_id, organization_id) ON DELETE CASCADE,
    CHECK (expires_at > created_at),
    CHECK ((accepted_at IS NULL) = (accepted_user_id IS NULL))
);
CREATE INDEX hosted_account_invitation_account ON hosted_account_invitation(organization_id, account_id, created_at DESC, invitation_id DESC);

-- fleet-migration: 001006_internal_hosted_account_keys.up.sql
-- requires-shared: 1000

-- Account keys remain a distinct credential kind even if their account is removed.
ALTER TABLE api_key DROP CONSTRAINT ck_api_key_subject;
ALTER TABLE api_key ADD CONSTRAINT ck_api_key_subject CHECK (
    (subject_kind IN ('user', 'account') AND user_id IS NOT NULL AND fleet_node_id IS NULL) OR
    (subject_kind = 'fleet_node' AND user_id IS NULL AND fleet_node_id IS NOT NULL)
);
ALTER TABLE api_key ADD CONSTRAINT uq_api_key_org_id UNIQUE (organization_id, id);
CREATE TABLE hosted_account_key (
    api_key_id BIGINT PRIMARY KEY,
    organization_id BIGINT NOT NULL,
    account_id BIGINT NOT NULL,
    capabilities BIGINT NOT NULL CHECK (capabilities BETWEEN 1 AND 7 AND (capabilities & 1) = 1),
    include_future_descendants BOOLEAN NOT NULL DEFAULT FALSE,
    CONSTRAINT uq_hosted_account_key_org_api_key UNIQUE (organization_id, api_key_id),
    FOREIGN KEY (organization_id, api_key_id) REFERENCES api_key(organization_id, id) ON DELETE CASCADE,
    FOREIGN KEY (organization_id, account_id) REFERENCES hosted_account(organization_id, id) ON DELETE CASCADE
);
CREATE INDEX idx_hosted_account_key_account ON hosted_account_key (organization_id, account_id);
CREATE TABLE hosted_account_key_descendant (
    organization_id BIGINT NOT NULL,
    api_key_id BIGINT NOT NULL,
    account_id BIGINT NOT NULL,
    PRIMARY KEY (organization_id, api_key_id, account_id),
    FOREIGN KEY (organization_id, api_key_id) REFERENCES hosted_account_key(organization_id, api_key_id) ON DELETE CASCADE,
    FOREIGN KEY (organization_id, account_id) REFERENCES hosted_account(organization_id, id) ON DELETE CASCADE
);
CREATE INDEX idx_hosted_account_key_descendant_account ON hosted_account_key_descendant (organization_id, account_id);
CREATE FUNCTION check_hosted_account_key() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM api_key k JOIN hosted_account a ON a.organization_id=k.organization_id
        JOIN user_organization uo ON uo.user_id=k.user_id AND uo.organization_id=k.organization_id
        WHERE k.id=NEW.api_key_id AND k.organization_id=NEW.organization_id AND k.subject_kind='account'
        AND a.id=NEW.account_id AND a.depth>0
    ) THEN
        RAISE EXCEPTION 'account key requires same-organization account credential and customer account' USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER hosted_account_key_scope BEFORE INSERT OR UPDATE ON hosted_account_key
FOR EACH ROW EXECUTE FUNCTION check_hosted_account_key();
CREATE FUNCTION check_hosted_account_key_descendant() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM hosted_account_key k JOIN hosted_account a ON a.id=k.account_id AND a.organization_id=k.organization_id
        JOIN hosted_account child ON child.parent_id=a.id AND child.organization_id=a.organization_id
        WHERE k.api_key_id=NEW.api_key_id AND k.organization_id=NEW.organization_id AND child.id=NEW.account_id
    ) THEN
        RAISE EXCEPTION 'key scope requires a strict same-organization descendant' USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER hosted_account_key_descendant_scope BEFORE INSERT OR UPDATE ON hosted_account_key_descendant
FOR EACH ROW EXECUTE FUNCTION check_hosted_account_key_descendant();

-- fleet-migration: 001007_internal_hosted_account_pools.up.sql
-- requires-shared: 1000

-- Customer pool destinations are usable immediately, without host allowlists.
CREATE TABLE hosted_account_pool (
    pool_id VARCHAR(36) PRIMARY KEY,
    organization_id BIGINT NOT NULL,
    account_id BIGINT NOT NULL,
    archived BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT uq_hosted_account_pool_org_account_pool UNIQUE (organization_id, account_id, pool_id),
    FOREIGN KEY (organization_id, account_id)
        REFERENCES hosted_account(organization_id, id) ON DELETE CASCADE
);

-- Each slot has only an encrypted password; even an empty password is encrypted.
CREATE FUNCTION valid_hosted_account_pool_slots(slots JSONB) RETURNS BOOLEAN
LANGUAGE plpgsql IMMUTABLE AS $$
DECLARE slot JSONB;
BEGIN
    IF jsonb_typeof(slots) IS DISTINCT FROM 'array' THEN RETURN FALSE; END IF;
    IF jsonb_array_length(slots) NOT BETWEEN 1 AND 3 THEN RETURN FALSE; END IF;
    FOR slot IN SELECT value FROM jsonb_array_elements(slots) LOOP
        IF jsonb_typeof(slot) IS DISTINCT FROM 'object'
           OR NOT slot ?& ARRAY['url','username','worker_template','password_ciphertext','has_password']
           OR (SELECT count(*) FROM jsonb_object_keys(slot)) <> 5
           OR jsonb_typeof(slot->'url') IS DISTINCT FROM 'string'
           OR jsonb_typeof(slot->'username') IS DISTINCT FROM 'string'
           OR jsonb_typeof(slot->'worker_template') IS DISTINCT FROM 'string'
           OR jsonb_typeof(slot->'password_ciphertext') IS DISTINCT FROM 'string'
           OR length(slot->>'password_ciphertext') = 0
           OR jsonb_typeof(slot->'has_password') IS DISTINCT FROM 'boolean'
        THEN RETURN FALSE; END IF;
    END LOOP;
    RETURN TRUE;
END;
$$;

CREATE TABLE hosted_account_pool_revision (
    revision_id VARCHAR(36) PRIMARY KEY,
    organization_id BIGINT NOT NULL,
    account_id BIGINT NOT NULL,
    pool_id VARCHAR(36) NOT NULL,
    number BIGINT NOT NULL CHECK (number > 0),
    name VARCHAR(255) NOT NULL CHECK (length(btrim(name)) > 0),
    slots JSONB NOT NULL CHECK (valid_hosted_account_pool_slots(slots)),
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT uq_hosted_account_pool_revision_pool_number UNIQUE (pool_id, number),
    CONSTRAINT uq_hosted_account_pool_revision_org_account_revision UNIQUE (organization_id, account_id, revision_id),
    FOREIGN KEY (organization_id, account_id, pool_id)
        REFERENCES hosted_account_pool(organization_id, account_id, pool_id) ON DELETE CASCADE
);

CREATE FUNCTION keep_hosted_account_pool_revision_immutable() RETURNS TRIGGER
LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'DELETE' AND NOT EXISTS (
        SELECT 1 FROM hosted_account_pool WHERE pool_id=OLD.pool_id
    ) THEN RETURN OLD; END IF;
    RAISE EXCEPTION 'pool revisions are immutable' USING ERRCODE = '23514';
END;
$$;

CREATE TRIGGER hosted_account_pool_revision_immutable
    BEFORE UPDATE OR DELETE ON hosted_account_pool_revision
    FOR EACH ROW EXECUTE FUNCTION keep_hosted_account_pool_revision_immutable();

CREATE TABLE hosted_account_pool_profile (
    organization_id BIGINT NOT NULL,
    account_id BIGINT NOT NULL,
    revision_id VARCHAR(36),
    revision BIGINT NOT NULL DEFAULT 1 CHECK (revision > 0),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (organization_id, account_id),
    FOREIGN KEY (organization_id, account_id)
        REFERENCES hosted_account(organization_id, id) ON DELETE CASCADE,
    FOREIGN KEY (organization_id, account_id, revision_id)
        REFERENCES hosted_account_pool_revision(organization_id, account_id, revision_id) ON DELETE CASCADE
);

-- fleet-migration: 001008_internal_hosted_device_write_fences.up.sql
-- requires-shared: 1000

-- Reuse the composite device identity constraint established by migration 39.
CREATE TABLE device_write_fence (
 device_id BIGINT PRIMARY KEY,
 organization_id BIGINT NOT NULL,
 epoch BIGINT NOT NULL DEFAULT 0 CHECK (epoch >= 0),
 state TEXT NOT NULL DEFAULT 'host' CHECK (state IN ('host','reserved','active','quarantined')),
 quarantine_origin TEXT CHECK (quarantine_origin IN ('reserved','active')),
 updated_at TIMESTAMPTZ NOT NULL DEFAULT statement_timestamp(),
 FOREIGN KEY (device_id, organization_id) REFERENCES device(id,org_id),
 CONSTRAINT uq_device_write_fence_device_org UNIQUE (device_id, organization_id)
);

ALTER TABLE queue_message ADD COLUMN write_epoch BIGINT NOT NULL DEFAULT 0 CHECK (write_epoch >= 0);

-- Attempt lifetime is independent of queue status and has no expiry. Reaping a
-- message cannot establish that a previously dispatched firmware call drained.
CREATE TABLE device_write_attempt (
 attempt_id UUID PRIMARY KEY,
 device_id BIGINT NOT NULL,
 organization_id BIGINT NOT NULL,
 epoch BIGINT NOT NULL CHECK (epoch >= 0),
 queue_message_id BIGINT NOT NULL REFERENCES queue_message(id),
 owner_instance_id UUID NOT NULL,
 operation_kind TEXT NOT NULL DEFAULT 'mutation' CHECK (operation_kind IN ('mutation','observation')),
 identity_snapshot JSONB NOT NULL,
 status TEXT NOT NULL DEFAULT 'running' CHECK (status IN ('running','complete','unknown','resolved')),
 created_at TIMESTAMPTZ NOT NULL DEFAULT statement_timestamp(),
 finished_at TIMESTAMPTZ,
 FOREIGN KEY (device_id, organization_id) REFERENCES device_write_fence(device_id,organization_id),
 CHECK ((status='running') = (finished_at IS NULL))
);
CREATE INDEX idx_device_write_attempt_outstanding ON device_write_attempt(device_id) WHERE status NOT IN ('complete','resolved');
CREATE INDEX idx_device_write_attempt_queue ON device_write_attempt(queue_message_id);

-- Recovery preserves the original attempt and distinguishes operator resolution
-- from successful firmware completion. Database credentials authorize this
-- offline operational boundary; evidence is explicitly operator-attested.
CREATE TABLE device_write_recovery (
 recovery_id UUID PRIMARY KEY,
 attempt_id UUID NOT NULL REFERENCES device_write_attempt(attempt_id),
 CONSTRAINT uq_device_write_recovery_attempt UNIQUE (attempt_id),
 fence_epoch BIGINT NOT NULL CHECK (fence_epoch >= 0),
 prior_status TEXT NOT NULL CHECK (prior_status IN ('running','unknown')),
 operator_name TEXT NOT NULL CHECK (length(operator_name) > 0),
 database_actor TEXT NOT NULL DEFAULT session_user,
 evidence JSONB NOT NULL,
 evidence_sha256 TEXT NOT NULL CHECK (length(evidence_sha256) = 64),
 created_at TIMESTAMPTZ NOT NULL DEFAULT statement_timestamp()
);

-- fleet-migration: 001009_internal_queue_pending_order_index.up.sql
-- requires-shared: 1000

-- The current runner commits each private migration with its checkpoint.
-- Build this index in that transaction so a failed upgrade rolls back fully.
CREATE INDEX idx_queue_message_pending_created_id ON queue_message(created_at, id) WHERE status = 'PENDING';

-- fleet-migration: 001010_internal_device_rpc_endpoint.up.sql
-- requires-shared: 1000
ALTER TABLE discovered_device
  ADD COLUMN rpc_port integer NOT NULL DEFAULT 0 CHECK (rpc_port BETWEEN 0 AND 65535),
  ADD COLUMN rpc_tls boolean NOT NULL DEFAULT FALSE;

-- fleet-migration: 001011_internal_direct_device_write_attempts.up.sql
-- requires-shared: 1000

-- Refuse unsupported intermediate hosted runtimes before advancing beyond
-- checkpoint 1010 or changing queue-claim nullability. Keep unexpected attempt evidence.
LOCK TABLE device_write_attempt IN ACCESS EXCLUSIVE MODE;
DO $$ BEGIN
 IF EXISTS (SELECT 1 FROM device_write_attempt) THEN
  RAISE EXCEPTION 'device write history exists; intermediate hosted schema upgrades are unsupported';
 END IF;
END $$;

-- Direct SDK and proxy attempts have no queue claim.
ALTER TABLE device_write_attempt ALTER COLUMN queue_message_id DROP NOT NULL;

-- fleet-migration: 001012_internal_retired_device_write_permits.up.sql
-- requires-shared: 1000

-- Reported serial/MAC identities are trusted only within their organization.
-- Endpoints additionally belong to the authenticated Fleet Node routing scope.
CREATE FUNCTION device_write_identity_keys_values(org_id bigint, serial text, mac text, routing_scope text, ip text, port text) RETURNS text[]
LANGUAGE sql IMMUTABLE AS $$
 SELECT ARRAY_REMOVE(ARRAY[
  CASE WHEN NULLIF(btrim(serial),'') IS NOT NULL THEN 'org:' || org_id || ':serial:' || lower(btrim(serial)) END,
  CASE WHEN length(regexp_replace(lower(mac),'[^0-9a-f]','','g'))=12 THEN 'org:' || org_id || ':mac:' || regexp_replace(lower(mac),'[^0-9a-f]','','g') END,
  CASE WHEN NULLIF(routing_scope,'') IS NOT NULL AND NULLIF(ip,'') IS NOT NULL AND NULLIF(port,'') IS NOT NULL THEN 'org:' || org_id || ':endpoint:' || routing_scope || ':' || lower(ip) || ':' || port END
 ],NULL)
$$;
CREATE FUNCTION device_write_identity_keys(target_id bigint) RETURNS text[]
LANGUAGE sql STABLE AS $$
 SELECT device_write_identity_keys_values(d.org_id,d.serial_number,d.mac_address,COALESCE(fnd.fleet_node_id::text,'local'),dd.ip_address,dd.port)
 FROM device d JOIN discovered_device dd ON dd.id=d.discovered_device_id
 LEFT JOIN fleet_node_device fnd ON fnd.device_id=d.id AND fnd.org_id=d.org_id
 WHERE d.id=target_id
$$;

-- Hosted ships as one feature. Earlier schema checkpoints are migration steps,
-- not supported runtimes that can create attempts before this final schema.
LOCK TABLE device_write_attempt IN ACCESS EXCLUSIVE MODE;
DO $$ BEGIN
 IF EXISTS (SELECT 1 FROM device_write_attempt) THEN
  RAISE EXCEPTION 'device write history exists; intermediate hosted schema upgrades are unsupported';
 END IF;
END $$;
ALTER TABLE device_write_attempt ADD COLUMN identity_keys text[] NOT NULL DEFAULT '{}';
CREATE INDEX idx_device_write_attempt_outstanding_identity_keys
 ON device_write_attempt USING gin(identity_keys) WHERE status NOT IN ('complete','resolved');
CREATE TABLE device_deletion_unpair_permit (
 device_id bigint PRIMARY KEY,
 organization_id bigint NOT NULL,
 epoch bigint NOT NULL CHECK(epoch>0),
 binding_hash text NOT NULL,
 identity_keys text[] NOT NULL,
 attempt_id uuid REFERENCES device_write_attempt(attempt_id),
 created_at timestamptz NOT NULL DEFAULT statement_timestamp(),
 CONSTRAINT uq_device_deletion_unpair_permit_attempt_id UNIQUE (attempt_id),
 FOREIGN KEY(device_id,organization_id) REFERENCES device_write_fence(device_id,organization_id)
);

-- fleet-migration: 001013_internal_fleet_node_heartbeat.up.sql
-- requires-shared: 1000

-- Heartbeats must not update the node row held by physical-dispatch leases.
CREATE TABLE fleet_node_heartbeat (
 fleet_node_id bigint CONSTRAINT pk_fleet_node_heartbeat PRIMARY KEY,
 org_id bigint NOT NULL,
 last_seen_at timestamptz NOT NULL,
 CONSTRAINT fk_fleet_node_heartbeat_node FOREIGN KEY (fleet_node_id,org_id)
  REFERENCES fleet_node(id,org_id) ON DELETE CASCADE
);
INSERT INTO fleet_node_heartbeat(fleet_node_id,org_id,last_seen_at)
 SELECT id,org_id,last_seen_at FROM fleet_node WHERE last_seen_at IS NOT NULL;

-- Keep the deployed baseline column as a migration snapshot. Active readers
-- and writers use this view/table; rollback copies current liveness back.
CREATE VIEW fleet_node_live AS
 SELECT fn.id,fn.org_id,fn.name,fn.identity_pubkey,fn.encryption_pubkey,
  fn.enrollment_status,h.last_seen_at,fn.created_at,fn.updated_at,fn.deleted_at
 FROM fleet_node fn LEFT JOIN fleet_node_heartbeat h
  ON h.fleet_node_id=fn.id AND h.org_id=fn.org_id;
-- The asset projection must use the same authoritative liveness as node readers.
CREATE OR REPLACE VIEW fleet_mining_asset AS
SELECT 5::int AS kind, d.id, d.org_id, d.site_id,
       COALESCE(NULLIF(d.custom_name, ''), concat_ws(' ', dd.manufacturer, dd.model))::text AS name,
       d.device_identifier::text AS device_identifier, 0::int AS position, COALESCE(d.serial_number, '')::text AS reported_id,
       1::int AS facility_kind, COALESCE(d.building_id, rack.building_id) AS facility_id,
       3::int AS group_kind, rack.device_set_id AS group_id,
       CASE WHEN ds.status = 'OFFLINE' OR (ds.status IS NULL AND dp.pairing_status IN ('PAIRED','DEFAULT_PASSWORD')) OR (fn.last_seen_at < now() - INTERVAL '2 minutes' AND fn.enrollment_status = 'CONFIRMED' AND fn.deleted_at IS NULL) THEN 'offline'
            WHEN ds.status IN ('MAINTENANCE', 'INACTIVE') AND dp.pairing_status <> 'AUTHENTICATION_NEEDED' THEN 'sleeping'
            WHEN ds.status IN ('ERROR', 'NEEDS_MINING_POOL', 'UPDATING', 'REBOOT_REQUIRED') OR dp.pairing_status='AUTHENTICATION_NEEDED' OR EXISTS (SELECT 1 FROM errors e WHERE e.device_id=d.id AND e.org_id=d.org_id AND e.closed_at IS NULL AND e.severity IN (1,2,3,4)) THEN 'attention'
            WHEN ds.status = 'ACTIVE' THEN 'hashing' ELSE 'unknown' END::text AS state,
       dm.time AS observed_at, dm.hash_rate_hs AS hashrate_hs, dm.power_w, dm.temp_c AS temperature_c, (dm.efficiency_jh * 1e12)::double precision AS miner_efficiency_jth
FROM device d
JOIN discovered_device dd ON dd.id = d.discovered_device_id AND dd.org_id = d.org_id
JOIN device_pairing dp ON dp.device_id = d.id
LEFT JOIN device_status ds ON ds.device_id = d.id
LEFT JOIN fleet_node_device fnd ON fnd.device_id = d.id AND fnd.org_id = d.org_id
LEFT JOIN fleet_node_live fn ON fn.id = fnd.fleet_node_id AND fn.org_id = fnd.org_id
LEFT JOIN LATERAL (
  SELECT r.device_set_id, r.building_id
  FROM device_set_membership ms JOIN device_set s ON s.id = ms.device_set_id AND s.deleted_at IS NULL
  JOIN device_set_rack r ON r.device_set_id = s.id
  WHERE ms.device_id = d.id AND ms.device_set_type = 'rack' AND ms.org_id = d.org_id
) rack ON TRUE
LEFT JOIN LATERAL (
  SELECT time, hash_rate_hs, power_w, temp_c, efficiency_jh FROM device_metrics
  WHERE device_identifier = d.device_identifier AND time > now() - INTERVAL '10 minutes'
    AND time <= now() + INTERVAL '5 seconds'
    AND ds.status IS DISTINCT FROM 'OFFLINE'
    AND NOT COALESCE(fn.last_seen_at < now() - INTERVAL '2 minutes' AND fn.enrollment_status = 'CONFIRMED' AND fn.deleted_at IS NULL, FALSE)
  ORDER BY time DESC LIMIT 1
) dm ON TRUE
WHERE d.deleted_at IS NULL AND dd.deleted_at IS NULL AND dd.is_active AND dd.device_class <> 2
 AND dp.pairing_status IN ('PAIRED','AUTHENTICATION_NEEDED','DEFAULT_PASSWORD')
UNION ALL
SELECT 6::int, m.id, d.org_id, d.site_id,
       (COALESCE(NULLIF(d.custom_name, ''), 'Container ' || COALESCE(NULLIF(d.serial_number, ''), d.device_identifier)) || ' T' || t.position || ' M' || m.position)::text,
       d.device_identifier::text, m.position, m.reported_id,
       2::int, d.id, 4::int, t.id,
       CASE WHEN m.state_observed_at > now() - INTERVAL '2 minutes' AND m.state_observed_at <= now() + INTERVAL '5 seconds'
              AND m.last_seen_at > now() - INTERVAL '2 minutes' THEN m.state ELSE 'unknown' END::text,
       mm.time, mm.hashrate_hs, mm.power_w, mm.temperature_c, NULL::double precision
FROM container_module m JOIN container_tank t ON t.id = m.tank_id
JOIN device d ON d.id = t.device_id
JOIN discovered_device dd ON dd.id = d.discovered_device_id AND dd.org_id = d.org_id
LEFT JOIN container_module_latest_metrics mm ON mm.module_id = m.id
  AND mm.time > now() - INTERVAL '2 minutes' AND mm.time <= now() + INTERVAL '5 seconds'
  AND m.last_seen_at > now() - INTERVAL '2 minutes' AND m.state <> 'offline'
WHERE m.retired_at IS NULL AND t.present AND d.deleted_at IS NULL AND dd.deleted_at IS NULL;

DO $$ BEGIN
 IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname='grafana_ha_ro') THEN
  GRANT SELECT (org_id,id,last_seen_at,enrollment_status,deleted_at)
   ON fleet_node_live TO grafana_ha_ro;
 END IF;
END $$;

-- fleet-migration: 001014_internal_discovery_identity.up.sql
-- requires-shared: 1000

-- Keep the identity reported by discovery through database reloads before
-- credential-bearing pairing. Existing rows have no retained discovery evidence;
-- do not infer it from a previously paired device at the same endpoint.
ALTER TABLE discovered_device
    ADD COLUMN serial_number TEXT NOT NULL DEFAULT '',
    ADD COLUMN mac_address TEXT NOT NULL DEFAULT '';

-- fleet-migration: 001015_internal_ct1_poll_rollups.up.sql
-- requires-shared: 1000
ALTER TABLE hashboard_metrics ADD COLUMN power_w double precision, ADD COLUMN efficiency_jh double precision;
ALTER TABLE hashboard_latest_metrics ADD COLUMN power_w double precision, ADD COLUMN efficiency_jh double precision;
ALTER TABLE container_module_metrics ADD COLUMN efficiency_jh double precision, ADD COLUMN coverage jsonb;
ALTER TABLE container_module_latest_metrics ADD COLUMN efficiency_jh double precision, ADD COLUMN coverage jsonb,
 ADD COLUMN source_times jsonb NOT NULL DEFAULT '{}';

CREATE TABLE container_topology_version (
 id bigserial PRIMARY KEY,
 device_id bigint NOT NULL REFERENCES device(id),
 fingerprint text NOT NULL,
 topology jsonb NOT NULL,
 CONSTRAINT uq_container_topology_version_device_fingerprint UNIQUE(device_id, fingerprint)
);
CREATE TABLE container_topology_observation (
 id bigserial PRIMARY KEY,
 device_id bigint NOT NULL REFERENCES device(id),
 version_id bigint NOT NULL REFERENCES container_topology_version(id),
 collected_at timestamptz NOT NULL,
 complete boolean NOT NULL,
 CONSTRAINT uq_container_topology_observation_device_time UNIQUE(device_id, collected_at)
);
CREATE INDEX idx_container_topology_observation_version ON container_topology_observation(version_id);

CREATE TABLE container_tank_metrics (
 tank_id bigint NOT NULL REFERENCES container_tank(id),
 time timestamptz NOT NULL,
 topology_observation_id bigint NOT NULL REFERENCES container_topology_observation(id),
 site_id bigint REFERENCES site(id),
 hashrate_hs double precision,
 power_w double precision,
 efficiency_jh double precision,
 temperature_c double precision,
 min_temperature_c double precision,
 max_temperature_c double precision,
 coverage jsonb NOT NULL,
 CONSTRAINT uq_container_tank_metrics_tank_time UNIQUE(tank_id, time)
);
SELECT create_hypertable('container_tank_metrics','time',chunk_time_interval => INTERVAL '1 day');
SELECT add_retention_policy('container_tank_metrics', INTERVAL '30 days');
CREATE TABLE container_tank_latest_metrics (
 tank_id bigint PRIMARY KEY REFERENCES container_tank(id),
 time timestamptz NOT NULL,
 topology_observation_id bigint NOT NULL REFERENCES container_topology_observation(id),
 hashrate_hs double precision,
 power_w double precision,
 efficiency_jh double precision,
 temperature_c double precision,
 min_temperature_c double precision,
 max_temperature_c double precision,
 coverage jsonb NOT NULL,
 source_times jsonb NOT NULL DEFAULT '{}'
);
ALTER TABLE device_metrics ADD COLUMN metric_semantics smallint NOT NULL DEFAULT 1,
 ADD COLUMN coverage jsonb,
 -- Compressed device_metrics cannot acquire a new FK. The telemetry transaction
 -- writes this reference after creating the observation; topology is retained.
 ADD COLUMN topology_observation_id bigint;
CREATE TABLE container_latest_metrics (
 device_id bigint PRIMARY KEY REFERENCES device(id),
 time timestamptz NOT NULL,
 topology_observation_id bigint NOT NULL REFERENCES container_topology_observation(id),
 hashrate_hs double precision,
 power_w double precision,
 efficiency_jh double precision,
 temperature_c double precision,
 min_temperature_c double precision,
 max_temperature_c double precision,
 coverage jsonb NOT NULL,
 source_times jsonb NOT NULL DEFAULT '{}'
);

CREATE VIEW container_equipment_current_metrics AS
WITH latest AS (
 SELECT 4 AS kind,tank_id AS id,time,hashrate_hs,power_w,efficiency_jh,temperature_c,min_temperature_c,max_temperature_c,coverage FROM container_tank_latest_metrics
 UNION ALL
 SELECT 2,device_id,time,hashrate_hs,power_w,efficiency_jh,temperature_c,min_temperature_c,max_temperature_c,coverage FROM container_latest_metrics
 UNION ALL
 SELECT 6,module_id,collected_at,hashrate_hs,power_w,efficiency_jh,temperature_c,temperature_c,temperature_c,coverage FROM container_module_latest_metrics
), fresh AS (
 SELECT *, time > now()-INTERVAL '2 minutes' AND time <= now()+INTERVAL '5 seconds'
  AND (CASE WHEN coverage IS NULL THEN time ELSE (coverage#>>'{hashrate,oldest_at}')::timestamptz END) > now()-INTERVAL '2 minutes' AS hash_fresh,
 time > now()-INTERVAL '2 minutes' AND time <= now()+INTERVAL '5 seconds'
  AND (CASE WHEN coverage IS NULL THEN time ELSE (coverage#>>'{power,oldest_at}')::timestamptz END) > now()-INTERVAL '2 minutes' AS power_fresh,
 time > now()-INTERVAL '2 minutes' AND time <= now()+INTERVAL '5 seconds'
  AND (CASE WHEN coverage IS NULL THEN time ELSE (coverage#>>'{temperature,oldest_at}')::timestamptz END) > now()-INTERVAL '2 minutes' AS temp_fresh
 FROM latest
)
SELECT kind,id,time,
 (CASE WHEN hash_fresh THEN hashrate_hs END)::double precision AS hashrate_hs,
 (CASE WHEN power_fresh THEN power_w END)::double precision AS power_w,
 (CASE WHEN hash_fresh AND power_fresh THEN efficiency_jh END)::double precision AS efficiency_jh,
 (CASE WHEN temp_fresh THEN temperature_c END)::double precision AS temperature_c,
 (CASE WHEN temp_fresh THEN min_temperature_c END)::double precision AS min_temperature_c,
 (CASE WHEN temp_fresh THEN max_temperature_c END)::double precision AS max_temperature_c,
 coverage FROM fresh;

CREATE OR REPLACE VIEW fleet_mining_asset AS
SELECT 5::int AS kind, d.id, d.org_id, d.site_id,
       COALESCE(NULLIF(d.custom_name, ''), concat_ws(' ', dd.manufacturer, dd.model))::text AS name,
       d.device_identifier::text AS device_identifier, 0::int AS position, COALESCE(d.serial_number, '')::text AS reported_id,
       1::int AS facility_kind, COALESCE(d.building_id, rack.building_id) AS facility_id,
       3::int AS group_kind, rack.device_set_id AS group_id,
       CASE WHEN ds.status = 'OFFLINE' OR (ds.status IS NULL AND dp.pairing_status IN ('PAIRED','DEFAULT_PASSWORD')) OR (fn.last_seen_at < now() - INTERVAL '2 minutes' AND fn.enrollment_status = 'CONFIRMED' AND fn.deleted_at IS NULL) THEN 'offline'
            WHEN ds.status IN ('MAINTENANCE', 'INACTIVE') AND dp.pairing_status <> 'AUTHENTICATION_NEEDED' THEN 'sleeping'
            WHEN ds.status IN ('ERROR', 'NEEDS_MINING_POOL', 'UPDATING', 'REBOOT_REQUIRED') OR dp.pairing_status='AUTHENTICATION_NEEDED' OR EXISTS (SELECT 1 FROM errors e WHERE e.device_id=d.id AND e.org_id=d.org_id AND e.closed_at IS NULL AND e.severity IN (1,2,3,4)) THEN 'attention'
            WHEN ds.status = 'ACTIVE' THEN 'hashing' ELSE 'unknown' END::text AS state,
       dm.time AS observed_at, dm.hash_rate_hs AS hashrate_hs, dm.power_w, dm.temp_c AS temperature_c, (dm.efficiency_jh * 1e12)::double precision AS miner_efficiency_jth
FROM device d
JOIN discovered_device dd ON dd.id = d.discovered_device_id AND dd.org_id = d.org_id
JOIN device_pairing dp ON dp.device_id = d.id
LEFT JOIN device_status ds ON ds.device_id = d.id
LEFT JOIN fleet_node_device fnd ON fnd.device_id = d.id AND fnd.org_id = d.org_id
LEFT JOIN fleet_node fn ON fn.id = fnd.fleet_node_id
LEFT JOIN LATERAL (
  SELECT r.device_set_id, r.building_id
  FROM device_set_membership ms JOIN device_set s ON s.id = ms.device_set_id AND s.deleted_at IS NULL
  JOIN device_set_rack r ON r.device_set_id = s.id
  WHERE ms.device_id = d.id AND ms.device_set_type = 'rack' AND ms.org_id = d.org_id
) rack ON TRUE
LEFT JOIN LATERAL (
  SELECT time, hash_rate_hs, power_w, temp_c, efficiency_jh FROM device_metrics
  WHERE device_identifier = d.device_identifier AND time > now() - INTERVAL '10 minutes'
    AND time <= now() + INTERVAL '5 seconds'
    AND ds.status IS DISTINCT FROM 'OFFLINE'
    AND NOT COALESCE(fn.last_seen_at < now() - INTERVAL '2 minutes' AND fn.enrollment_status = 'CONFIRMED' AND fn.deleted_at IS NULL, FALSE)
  ORDER BY time DESC LIMIT 1
) dm ON TRUE
WHERE d.deleted_at IS NULL AND dd.deleted_at IS NULL AND dd.is_active AND dd.device_class <> 2
 AND dp.pairing_status IN ('PAIRED','AUTHENTICATION_NEEDED','DEFAULT_PASSWORD')
UNION ALL
SELECT 6::int, m.id, d.org_id, d.site_id,
       (COALESCE(NULLIF(d.custom_name, ''), 'Container ' || COALESCE(NULLIF(d.serial_number, ''), d.device_identifier)) || ' T' || t.position || ' M' || m.position)::text,
       d.device_identifier::text, m.position, m.reported_id,
       2::int, d.id, 4::int, t.id,
       CASE WHEN m.state_observed_at > now() - INTERVAL '2 minutes' AND m.state_observed_at <= now() + INTERVAL '5 seconds'
              AND m.last_seen_at > now() - INTERVAL '2 minutes' THEN m.state ELSE 'unknown' END::text,
       mm.time, mm.hashrate_hs, mm.power_w, mm.temperature_c, (mm.efficiency_jh * 1e12)::double precision
FROM container_module m JOIN container_tank t ON t.id = m.tank_id
JOIN device d ON d.id = t.device_id
JOIN discovered_device dd ON dd.id = d.discovered_device_id AND dd.org_id = d.org_id
LEFT JOIN container_equipment_current_metrics mm ON mm.kind=6 AND mm.id = m.id
  AND mm.time > now() - INTERVAL '2 minutes' AND mm.time <= now() + INTERVAL '5 seconds'
  AND m.last_seen_at > now() - INTERVAL '2 minutes' AND m.state <> 'offline'
WHERE m.retired_at IS NULL AND t.present AND d.deleted_at IS NULL AND dd.deleted_at IS NULL;

-- fleet-migration: 001016_internal_ct1_metric_summaries.up.sql
-- requires-shared: 1000
-- Preserve raw evidence until the backfill job has materialized the existing history.
SELECT remove_retention_policy('device_metrics', if_exists => true);
SELECT remove_retention_policy('container_tank_metrics', if_exists => true);
SELECT remove_retention_policy('container_module_metrics', if_exists => true);

-- Nullable provenance supports existing compressed raw chunks. Topology versions
-- are immutable and retained indefinitely; writers stamp these in the poll transaction.
ALTER TABLE device_metrics ADD COLUMN topology_version_id bigint;
ALTER TABLE container_tank_metrics ADD COLUMN topology_version_id bigint;
ALTER TABLE container_module_metrics ADD COLUMN topology_version_id bigint, ADD COLUMN site_id bigint;
-- Bound the device backfill to its six-hour compression window.
-- A join predicate alone can decompress all historical chunks, even when their
-- observation IDs are NULL, exceeding TimescaleDB's transaction limit (#221).
-- Older rows retain nullable provenance and their existing raw evidence.
UPDATE device_metrics m SET topology_version_id=o.version_id FROM container_topology_observation o
WHERE o.id=m.topology_observation_id AND m.time > now()-INTERVAL '6 hours';
UPDATE container_tank_metrics m SET topology_version_id=o.version_id FROM container_topology_observation o WHERE o.id=m.topology_observation_id;
UPDATE container_module_metrics mm SET topology_version_id=o.version_id,
 site_id=CASE WHEN (v.topology#>>'{site,Valid}')::boolean THEN (v.topology#>>'{site,Int64}')::bigint END
FROM container_module m JOIN container_tank t ON t.id=m.tank_id
JOIN container_topology_observation o ON o.device_id=t.device_id
JOIN container_topology_version v ON v.id=o.version_id
WHERE m.id=mm.module_id AND o.collected_at=mm.collected_at;

-- Separate topology versions and sites even when a move falls inside a bucket.
CREATE MATERIALIZED VIEW container_metrics_hourly
WITH (timescaledb.continuous, timescaledb.materialized_only=false) AS
SELECT time_bucket(INTERVAL '1 hour',time) AS bucket,device_identifier,site_id,topology_version_id,
 sum(temp_c) FILTER (WHERE temp_c > '-Infinity'::float8 AND temp_c < 'Infinity'::float8) AS temperature_sum,
 count(temp_c) FILTER (WHERE temp_c > '-Infinity'::float8 AND temp_c < 'Infinity'::float8) AS temperature_count,
 min(temp_c) FILTER (WHERE temp_c > '-Infinity'::float8 AND temp_c < 'Infinity'::float8) AS temperature_min,
 max(temp_c) FILTER (WHERE temp_c > '-Infinity'::float8 AND temp_c < 'Infinity'::float8) AS temperature_max,
 min(COALESCE((coverage#>>'{temperature,reporting}')::int,0)) AS temperature_reporting,
 max(COALESCE((coverage#>>'{temperature,expected}')::int,0)) AS temperature_expected,
 bool_and(COALESCE((coverage#>>'{temperature,topology_complete}')::boolean,false) AND COALESCE((coverage#>>'{temperature,reporting}')::int,0)=COALESCE((coverage#>>'{temperature,expected}')::int,0) AND COALESCE((coverage#>>'{temperature,expected}')::int,0)>0 AND COALESCE(temp_c > '-Infinity'::float8 AND temp_c < 'Infinity'::float8,false)) AS temperature_complete,
 sum(hash_rate_hs) FILTER (WHERE hash_rate_hs > '-Infinity'::float8 AND hash_rate_hs < 'Infinity'::float8) AS hashrate_sum,
 count(hash_rate_hs) FILTER (WHERE hash_rate_hs > '-Infinity'::float8 AND hash_rate_hs < 'Infinity'::float8) AS hashrate_count,
 min(hash_rate_hs) FILTER (WHERE hash_rate_hs > '-Infinity'::float8 AND hash_rate_hs < 'Infinity'::float8) AS hashrate_min,
 max(hash_rate_hs) FILTER (WHERE hash_rate_hs > '-Infinity'::float8 AND hash_rate_hs < 'Infinity'::float8) AS hashrate_max,
 min(COALESCE((coverage#>>'{hashrate,reporting}')::int,0)) AS hashrate_reporting,
 max(COALESCE((coverage#>>'{hashrate,expected}')::int,0)) AS hashrate_expected,
 bool_and(COALESCE((coverage#>>'{hashrate,topology_complete}')::boolean,false) AND COALESCE((coverage#>>'{hashrate,reporting}')::int,0)=COALESCE((coverage#>>'{hashrate,expected}')::int,0) AND COALESCE((coverage#>>'{hashrate,expected}')::int,0)>0 AND COALESCE(hash_rate_hs > '-Infinity'::float8 AND hash_rate_hs < 'Infinity'::float8,false)) AS hashrate_complete,
 sum(power_w) FILTER (WHERE power_w > '-Infinity'::float8 AND power_w < 'Infinity'::float8) AS power_sum,
 count(power_w) FILTER (WHERE power_w > '-Infinity'::float8 AND power_w < 'Infinity'::float8) AS power_count,
 min(power_w) FILTER (WHERE power_w > '-Infinity'::float8 AND power_w < 'Infinity'::float8) AS power_min,
 max(power_w) FILTER (WHERE power_w > '-Infinity'::float8 AND power_w < 'Infinity'::float8) AS power_max,
 min(COALESCE((coverage#>>'{power,reporting}')::int,0)) AS power_reporting,
 max(COALESCE((coverage#>>'{power,expected}')::int,0)) AS power_expected,
 bool_and(COALESCE((coverage#>>'{power,topology_complete}')::boolean,false) AND COALESCE((coverage#>>'{power,reporting}')::int,0)=COALESCE((coverage#>>'{power,expected}')::int,0) AND COALESCE((coverage#>>'{power,expected}')::int,0)>0 AND COALESCE(power_w > '-Infinity'::float8 AND power_w < 'Infinity'::float8,false)) AS power_complete,
 sum(efficiency_jh) FILTER (WHERE efficiency_jh > '-Infinity'::float8 AND efficiency_jh < 'Infinity'::float8) AS efficiency_sum,
 count(efficiency_jh) FILTER (WHERE efficiency_jh > '-Infinity'::float8 AND efficiency_jh < 'Infinity'::float8) AS efficiency_count,
 min(efficiency_jh) FILTER (WHERE efficiency_jh > '-Infinity'::float8 AND efficiency_jh < 'Infinity'::float8) AS efficiency_min,
 max(efficiency_jh) FILTER (WHERE efficiency_jh > '-Infinity'::float8 AND efficiency_jh < 'Infinity'::float8) AS efficiency_max,
 min(COALESCE((coverage#>>'{hashrate,reporting}')::int,0)) AS efficiency_reporting,
 max(COALESCE((coverage#>>'{hashrate,expected}')::int,0)) AS efficiency_expected,
 bool_and(COALESCE((coverage#>>'{hashrate,topology_complete}')::boolean,false) AND COALESCE((coverage#>>'{hashrate,reporting}')::int,0)=COALESCE((coverage#>>'{hashrate,expected}')::int,0) AND COALESCE((coverage#>>'{hashrate,expected}')::int,0)>0 AND COALESCE(efficiency_jh > '-Infinity'::float8 AND efficiency_jh < 'Infinity'::float8,false)) AS efficiency_complete
FROM device_metrics WHERE metric_semantics=2 AND topology_version_id IS NOT NULL
GROUP BY bucket,device_identifier,site_id,topology_version_id WITH NO DATA;
CREATE INDEX idx_container_metrics_hourly_equipment_bucket ON container_metrics_hourly(device_identifier,bucket DESC);

-- Separate topology versions and sites even when a move falls inside a bucket.
CREATE MATERIALIZED VIEW container_metrics_daily
WITH (timescaledb.continuous, timescaledb.materialized_only=false) AS
SELECT time_bucket(INTERVAL '1 day',time) AS bucket,device_identifier,site_id,topology_version_id,
 sum(temp_c) FILTER (WHERE temp_c > '-Infinity'::float8 AND temp_c < 'Infinity'::float8) AS temperature_sum,
 count(temp_c) FILTER (WHERE temp_c > '-Infinity'::float8 AND temp_c < 'Infinity'::float8) AS temperature_count,
 min(temp_c) FILTER (WHERE temp_c > '-Infinity'::float8 AND temp_c < 'Infinity'::float8) AS temperature_min,
 max(temp_c) FILTER (WHERE temp_c > '-Infinity'::float8 AND temp_c < 'Infinity'::float8) AS temperature_max,
 min(COALESCE((coverage#>>'{temperature,reporting}')::int,0)) AS temperature_reporting,
 max(COALESCE((coverage#>>'{temperature,expected}')::int,0)) AS temperature_expected,
 bool_and(COALESCE((coverage#>>'{temperature,topology_complete}')::boolean,false) AND COALESCE((coverage#>>'{temperature,reporting}')::int,0)=COALESCE((coverage#>>'{temperature,expected}')::int,0) AND COALESCE((coverage#>>'{temperature,expected}')::int,0)>0 AND COALESCE(temp_c > '-Infinity'::float8 AND temp_c < 'Infinity'::float8,false)) AS temperature_complete,
 sum(hash_rate_hs) FILTER (WHERE hash_rate_hs > '-Infinity'::float8 AND hash_rate_hs < 'Infinity'::float8) AS hashrate_sum,
 count(hash_rate_hs) FILTER (WHERE hash_rate_hs > '-Infinity'::float8 AND hash_rate_hs < 'Infinity'::float8) AS hashrate_count,
 min(hash_rate_hs) FILTER (WHERE hash_rate_hs > '-Infinity'::float8 AND hash_rate_hs < 'Infinity'::float8) AS hashrate_min,
 max(hash_rate_hs) FILTER (WHERE hash_rate_hs > '-Infinity'::float8 AND hash_rate_hs < 'Infinity'::float8) AS hashrate_max,
 min(COALESCE((coverage#>>'{hashrate,reporting}')::int,0)) AS hashrate_reporting,
 max(COALESCE((coverage#>>'{hashrate,expected}')::int,0)) AS hashrate_expected,
 bool_and(COALESCE((coverage#>>'{hashrate,topology_complete}')::boolean,false) AND COALESCE((coverage#>>'{hashrate,reporting}')::int,0)=COALESCE((coverage#>>'{hashrate,expected}')::int,0) AND COALESCE((coverage#>>'{hashrate,expected}')::int,0)>0 AND COALESCE(hash_rate_hs > '-Infinity'::float8 AND hash_rate_hs < 'Infinity'::float8,false)) AS hashrate_complete,
 sum(power_w) FILTER (WHERE power_w > '-Infinity'::float8 AND power_w < 'Infinity'::float8) AS power_sum,
 count(power_w) FILTER (WHERE power_w > '-Infinity'::float8 AND power_w < 'Infinity'::float8) AS power_count,
 min(power_w) FILTER (WHERE power_w > '-Infinity'::float8 AND power_w < 'Infinity'::float8) AS power_min,
 max(power_w) FILTER (WHERE power_w > '-Infinity'::float8 AND power_w < 'Infinity'::float8) AS power_max,
 min(COALESCE((coverage#>>'{power,reporting}')::int,0)) AS power_reporting,
 max(COALESCE((coverage#>>'{power,expected}')::int,0)) AS power_expected,
 bool_and(COALESCE((coverage#>>'{power,topology_complete}')::boolean,false) AND COALESCE((coverage#>>'{power,reporting}')::int,0)=COALESCE((coverage#>>'{power,expected}')::int,0) AND COALESCE((coverage#>>'{power,expected}')::int,0)>0 AND COALESCE(power_w > '-Infinity'::float8 AND power_w < 'Infinity'::float8,false)) AS power_complete,
 sum(efficiency_jh) FILTER (WHERE efficiency_jh > '-Infinity'::float8 AND efficiency_jh < 'Infinity'::float8) AS efficiency_sum,
 count(efficiency_jh) FILTER (WHERE efficiency_jh > '-Infinity'::float8 AND efficiency_jh < 'Infinity'::float8) AS efficiency_count,
 min(efficiency_jh) FILTER (WHERE efficiency_jh > '-Infinity'::float8 AND efficiency_jh < 'Infinity'::float8) AS efficiency_min,
 max(efficiency_jh) FILTER (WHERE efficiency_jh > '-Infinity'::float8 AND efficiency_jh < 'Infinity'::float8) AS efficiency_max,
 min(COALESCE((coverage#>>'{hashrate,reporting}')::int,0)) AS efficiency_reporting,
 max(COALESCE((coverage#>>'{hashrate,expected}')::int,0)) AS efficiency_expected,
 bool_and(COALESCE((coverage#>>'{hashrate,topology_complete}')::boolean,false) AND COALESCE((coverage#>>'{hashrate,reporting}')::int,0)=COALESCE((coverage#>>'{hashrate,expected}')::int,0) AND COALESCE((coverage#>>'{hashrate,expected}')::int,0)>0 AND COALESCE(efficiency_jh > '-Infinity'::float8 AND efficiency_jh < 'Infinity'::float8,false)) AS efficiency_complete
FROM device_metrics WHERE metric_semantics=2 AND topology_version_id IS NOT NULL
GROUP BY bucket,device_identifier,site_id,topology_version_id WITH NO DATA;
CREATE INDEX idx_container_metrics_daily_equipment_bucket ON container_metrics_daily(device_identifier,bucket DESC);

-- Separate topology versions and sites even when a move falls inside a bucket.
CREATE MATERIALIZED VIEW container_tank_metrics_hourly
WITH (timescaledb.continuous, timescaledb.materialized_only=false) AS
SELECT time_bucket(INTERVAL '1 hour',time) AS bucket,tank_id,site_id,topology_version_id,
 sum(temperature_c) FILTER (WHERE temperature_c > '-Infinity'::float8 AND temperature_c < 'Infinity'::float8) AS temperature_sum,
 count(temperature_c) FILTER (WHERE temperature_c > '-Infinity'::float8 AND temperature_c < 'Infinity'::float8) AS temperature_count,
 min(min_temperature_c) FILTER (WHERE min_temperature_c > '-Infinity'::float8 AND min_temperature_c < 'Infinity'::float8) AS temperature_min,
 max(max_temperature_c) FILTER (WHERE max_temperature_c > '-Infinity'::float8 AND max_temperature_c < 'Infinity'::float8) AS temperature_max,
 min(COALESCE((coverage#>>'{temperature,reporting}')::int,0)) AS temperature_reporting,
 max(COALESCE((coverage#>>'{temperature,expected}')::int,0)) AS temperature_expected,
 bool_and(COALESCE((coverage#>>'{temperature,topology_complete}')::boolean,false) AND COALESCE((coverage#>>'{temperature,reporting}')::int,0)=COALESCE((coverage#>>'{temperature,expected}')::int,0) AND COALESCE((coverage#>>'{temperature,expected}')::int,0)>0 AND COALESCE(temperature_c > '-Infinity'::float8 AND temperature_c < 'Infinity'::float8,false)) AS temperature_complete,
 sum(hashrate_hs) FILTER (WHERE hashrate_hs > '-Infinity'::float8 AND hashrate_hs < 'Infinity'::float8) AS hashrate_sum,
 count(hashrate_hs) FILTER (WHERE hashrate_hs > '-Infinity'::float8 AND hashrate_hs < 'Infinity'::float8) AS hashrate_count,
 min(hashrate_hs) FILTER (WHERE hashrate_hs > '-Infinity'::float8 AND hashrate_hs < 'Infinity'::float8) AS hashrate_min,
 max(hashrate_hs) FILTER (WHERE hashrate_hs > '-Infinity'::float8 AND hashrate_hs < 'Infinity'::float8) AS hashrate_max,
 min(COALESCE((coverage#>>'{hashrate,reporting}')::int,0)) AS hashrate_reporting,
 max(COALESCE((coverage#>>'{hashrate,expected}')::int,0)) AS hashrate_expected,
 bool_and(COALESCE((coverage#>>'{hashrate,topology_complete}')::boolean,false) AND COALESCE((coverage#>>'{hashrate,reporting}')::int,0)=COALESCE((coverage#>>'{hashrate,expected}')::int,0) AND COALESCE((coverage#>>'{hashrate,expected}')::int,0)>0 AND COALESCE(hashrate_hs > '-Infinity'::float8 AND hashrate_hs < 'Infinity'::float8,false)) AS hashrate_complete,
 sum(power_w) FILTER (WHERE power_w > '-Infinity'::float8 AND power_w < 'Infinity'::float8) AS power_sum,
 count(power_w) FILTER (WHERE power_w > '-Infinity'::float8 AND power_w < 'Infinity'::float8) AS power_count,
 min(power_w) FILTER (WHERE power_w > '-Infinity'::float8 AND power_w < 'Infinity'::float8) AS power_min,
 max(power_w) FILTER (WHERE power_w > '-Infinity'::float8 AND power_w < 'Infinity'::float8) AS power_max,
 min(COALESCE((coverage#>>'{power,reporting}')::int,0)) AS power_reporting,
 max(COALESCE((coverage#>>'{power,expected}')::int,0)) AS power_expected,
 bool_and(COALESCE((coverage#>>'{power,topology_complete}')::boolean,false) AND COALESCE((coverage#>>'{power,reporting}')::int,0)=COALESCE((coverage#>>'{power,expected}')::int,0) AND COALESCE((coverage#>>'{power,expected}')::int,0)>0 AND COALESCE(power_w > '-Infinity'::float8 AND power_w < 'Infinity'::float8,false)) AS power_complete,
 sum(efficiency_jh) FILTER (WHERE efficiency_jh > '-Infinity'::float8 AND efficiency_jh < 'Infinity'::float8) AS efficiency_sum,
 count(efficiency_jh) FILTER (WHERE efficiency_jh > '-Infinity'::float8 AND efficiency_jh < 'Infinity'::float8) AS efficiency_count,
 min(efficiency_jh) FILTER (WHERE efficiency_jh > '-Infinity'::float8 AND efficiency_jh < 'Infinity'::float8) AS efficiency_min,
 max(efficiency_jh) FILTER (WHERE efficiency_jh > '-Infinity'::float8 AND efficiency_jh < 'Infinity'::float8) AS efficiency_max,
 min(COALESCE((coverage#>>'{hashrate,reporting}')::int,0)) AS efficiency_reporting,
 max(COALESCE((coverage#>>'{hashrate,expected}')::int,0)) AS efficiency_expected,
 bool_and(COALESCE((coverage#>>'{hashrate,topology_complete}')::boolean,false) AND COALESCE((coverage#>>'{hashrate,reporting}')::int,0)=COALESCE((coverage#>>'{hashrate,expected}')::int,0) AND COALESCE((coverage#>>'{hashrate,expected}')::int,0)>0 AND COALESCE(efficiency_jh > '-Infinity'::float8 AND efficiency_jh < 'Infinity'::float8,false)) AS efficiency_complete
FROM container_tank_metrics WHERE coverage IS NOT NULL AND topology_version_id IS NOT NULL
GROUP BY bucket,tank_id,site_id,topology_version_id WITH NO DATA;
CREATE INDEX idx_container_tank_metrics_hourly_equipment_bucket ON container_tank_metrics_hourly(tank_id,bucket DESC);

-- Separate topology versions and sites even when a move falls inside a bucket.
CREATE MATERIALIZED VIEW container_tank_metrics_daily
WITH (timescaledb.continuous, timescaledb.materialized_only=false) AS
SELECT time_bucket(INTERVAL '1 day',time) AS bucket,tank_id,site_id,topology_version_id,
 sum(temperature_c) FILTER (WHERE temperature_c > '-Infinity'::float8 AND temperature_c < 'Infinity'::float8) AS temperature_sum,
 count(temperature_c) FILTER (WHERE temperature_c > '-Infinity'::float8 AND temperature_c < 'Infinity'::float8) AS temperature_count,
 min(min_temperature_c) FILTER (WHERE min_temperature_c > '-Infinity'::float8 AND min_temperature_c < 'Infinity'::float8) AS temperature_min,
 max(max_temperature_c) FILTER (WHERE max_temperature_c > '-Infinity'::float8 AND max_temperature_c < 'Infinity'::float8) AS temperature_max,
 min(COALESCE((coverage#>>'{temperature,reporting}')::int,0)) AS temperature_reporting,
 max(COALESCE((coverage#>>'{temperature,expected}')::int,0)) AS temperature_expected,
 bool_and(COALESCE((coverage#>>'{temperature,topology_complete}')::boolean,false) AND COALESCE((coverage#>>'{temperature,reporting}')::int,0)=COALESCE((coverage#>>'{temperature,expected}')::int,0) AND COALESCE((coverage#>>'{temperature,expected}')::int,0)>0 AND COALESCE(temperature_c > '-Infinity'::float8 AND temperature_c < 'Infinity'::float8,false)) AS temperature_complete,
 sum(hashrate_hs) FILTER (WHERE hashrate_hs > '-Infinity'::float8 AND hashrate_hs < 'Infinity'::float8) AS hashrate_sum,
 count(hashrate_hs) FILTER (WHERE hashrate_hs > '-Infinity'::float8 AND hashrate_hs < 'Infinity'::float8) AS hashrate_count,
 min(hashrate_hs) FILTER (WHERE hashrate_hs > '-Infinity'::float8 AND hashrate_hs < 'Infinity'::float8) AS hashrate_min,
 max(hashrate_hs) FILTER (WHERE hashrate_hs > '-Infinity'::float8 AND hashrate_hs < 'Infinity'::float8) AS hashrate_max,
 min(COALESCE((coverage#>>'{hashrate,reporting}')::int,0)) AS hashrate_reporting,
 max(COALESCE((coverage#>>'{hashrate,expected}')::int,0)) AS hashrate_expected,
 bool_and(COALESCE((coverage#>>'{hashrate,topology_complete}')::boolean,false) AND COALESCE((coverage#>>'{hashrate,reporting}')::int,0)=COALESCE((coverage#>>'{hashrate,expected}')::int,0) AND COALESCE((coverage#>>'{hashrate,expected}')::int,0)>0 AND COALESCE(hashrate_hs > '-Infinity'::float8 AND hashrate_hs < 'Infinity'::float8,false)) AS hashrate_complete,
 sum(power_w) FILTER (WHERE power_w > '-Infinity'::float8 AND power_w < 'Infinity'::float8) AS power_sum,
 count(power_w) FILTER (WHERE power_w > '-Infinity'::float8 AND power_w < 'Infinity'::float8) AS power_count,
 min(power_w) FILTER (WHERE power_w > '-Infinity'::float8 AND power_w < 'Infinity'::float8) AS power_min,
 max(power_w) FILTER (WHERE power_w > '-Infinity'::float8 AND power_w < 'Infinity'::float8) AS power_max,
 min(COALESCE((coverage#>>'{power,reporting}')::int,0)) AS power_reporting,
 max(COALESCE((coverage#>>'{power,expected}')::int,0)) AS power_expected,
 bool_and(COALESCE((coverage#>>'{power,topology_complete}')::boolean,false) AND COALESCE((coverage#>>'{power,reporting}')::int,0)=COALESCE((coverage#>>'{power,expected}')::int,0) AND COALESCE((coverage#>>'{power,expected}')::int,0)>0 AND COALESCE(power_w > '-Infinity'::float8 AND power_w < 'Infinity'::float8,false)) AS power_complete,
 sum(efficiency_jh) FILTER (WHERE efficiency_jh > '-Infinity'::float8 AND efficiency_jh < 'Infinity'::float8) AS efficiency_sum,
 count(efficiency_jh) FILTER (WHERE efficiency_jh > '-Infinity'::float8 AND efficiency_jh < 'Infinity'::float8) AS efficiency_count,
 min(efficiency_jh) FILTER (WHERE efficiency_jh > '-Infinity'::float8 AND efficiency_jh < 'Infinity'::float8) AS efficiency_min,
 max(efficiency_jh) FILTER (WHERE efficiency_jh > '-Infinity'::float8 AND efficiency_jh < 'Infinity'::float8) AS efficiency_max,
 min(COALESCE((coverage#>>'{hashrate,reporting}')::int,0)) AS efficiency_reporting,
 max(COALESCE((coverage#>>'{hashrate,expected}')::int,0)) AS efficiency_expected,
 bool_and(COALESCE((coverage#>>'{hashrate,topology_complete}')::boolean,false) AND COALESCE((coverage#>>'{hashrate,reporting}')::int,0)=COALESCE((coverage#>>'{hashrate,expected}')::int,0) AND COALESCE((coverage#>>'{hashrate,expected}')::int,0)>0 AND COALESCE(efficiency_jh > '-Infinity'::float8 AND efficiency_jh < 'Infinity'::float8,false)) AS efficiency_complete
FROM container_tank_metrics WHERE coverage IS NOT NULL AND topology_version_id IS NOT NULL
GROUP BY bucket,tank_id,site_id,topology_version_id WITH NO DATA;
CREATE INDEX idx_container_tank_metrics_daily_equipment_bucket ON container_tank_metrics_daily(tank_id,bucket DESC);

-- Separate topology versions and sites even when a move falls inside a bucket.
CREATE MATERIALIZED VIEW container_module_metrics_hourly
WITH (timescaledb.continuous, timescaledb.materialized_only=false) AS
SELECT time_bucket(INTERVAL '1 hour',time) AS bucket,module_id,site_id,topology_version_id,
 sum(temperature_c) FILTER (WHERE temperature_c > '-Infinity'::float8 AND temperature_c < 'Infinity'::float8) AS temperature_sum,
 count(temperature_c) FILTER (WHERE temperature_c > '-Infinity'::float8 AND temperature_c < 'Infinity'::float8) AS temperature_count,
 min(temperature_c) FILTER (WHERE temperature_c > '-Infinity'::float8 AND temperature_c < 'Infinity'::float8) AS temperature_min,
 max(temperature_c) FILTER (WHERE temperature_c > '-Infinity'::float8 AND temperature_c < 'Infinity'::float8) AS temperature_max,
 min(COALESCE((coverage#>>'{temperature,reporting}')::int,0)) AS temperature_reporting,
 max(COALESCE((coverage#>>'{temperature,expected}')::int,0)) AS temperature_expected,
 bool_and(COALESCE((coverage#>>'{temperature,topology_complete}')::boolean,false) AND COALESCE((coverage#>>'{temperature,reporting}')::int,0)=COALESCE((coverage#>>'{temperature,expected}')::int,0) AND COALESCE((coverage#>>'{temperature,expected}')::int,0)>0 AND COALESCE(temperature_c > '-Infinity'::float8 AND temperature_c < 'Infinity'::float8,false)) AS temperature_complete,
 sum(hashrate_hs) FILTER (WHERE hashrate_hs > '-Infinity'::float8 AND hashrate_hs < 'Infinity'::float8) AS hashrate_sum,
 count(hashrate_hs) FILTER (WHERE hashrate_hs > '-Infinity'::float8 AND hashrate_hs < 'Infinity'::float8) AS hashrate_count,
 min(hashrate_hs) FILTER (WHERE hashrate_hs > '-Infinity'::float8 AND hashrate_hs < 'Infinity'::float8) AS hashrate_min,
 max(hashrate_hs) FILTER (WHERE hashrate_hs > '-Infinity'::float8 AND hashrate_hs < 'Infinity'::float8) AS hashrate_max,
 min(COALESCE((coverage#>>'{hashrate,reporting}')::int,0)) AS hashrate_reporting,
 max(COALESCE((coverage#>>'{hashrate,expected}')::int,0)) AS hashrate_expected,
 bool_and(COALESCE((coverage#>>'{hashrate,topology_complete}')::boolean,false) AND COALESCE((coverage#>>'{hashrate,reporting}')::int,0)=COALESCE((coverage#>>'{hashrate,expected}')::int,0) AND COALESCE((coverage#>>'{hashrate,expected}')::int,0)>0 AND COALESCE(hashrate_hs > '-Infinity'::float8 AND hashrate_hs < 'Infinity'::float8,false)) AS hashrate_complete,
 sum(power_w) FILTER (WHERE power_w > '-Infinity'::float8 AND power_w < 'Infinity'::float8) AS power_sum,
 count(power_w) FILTER (WHERE power_w > '-Infinity'::float8 AND power_w < 'Infinity'::float8) AS power_count,
 min(power_w) FILTER (WHERE power_w > '-Infinity'::float8 AND power_w < 'Infinity'::float8) AS power_min,
 max(power_w) FILTER (WHERE power_w > '-Infinity'::float8 AND power_w < 'Infinity'::float8) AS power_max,
 min(COALESCE((coverage#>>'{power,reporting}')::int,0)) AS power_reporting,
 max(COALESCE((coverage#>>'{power,expected}')::int,0)) AS power_expected,
 bool_and(COALESCE((coverage#>>'{power,topology_complete}')::boolean,false) AND COALESCE((coverage#>>'{power,reporting}')::int,0)=COALESCE((coverage#>>'{power,expected}')::int,0) AND COALESCE((coverage#>>'{power,expected}')::int,0)>0 AND COALESCE(power_w > '-Infinity'::float8 AND power_w < 'Infinity'::float8,false)) AS power_complete,
 sum(efficiency_jh) FILTER (WHERE efficiency_jh > '-Infinity'::float8 AND efficiency_jh < 'Infinity'::float8) AS efficiency_sum,
 count(efficiency_jh) FILTER (WHERE efficiency_jh > '-Infinity'::float8 AND efficiency_jh < 'Infinity'::float8) AS efficiency_count,
 min(efficiency_jh) FILTER (WHERE efficiency_jh > '-Infinity'::float8 AND efficiency_jh < 'Infinity'::float8) AS efficiency_min,
 max(efficiency_jh) FILTER (WHERE efficiency_jh > '-Infinity'::float8 AND efficiency_jh < 'Infinity'::float8) AS efficiency_max,
 min(COALESCE((coverage#>>'{hashrate,reporting}')::int,0)) AS efficiency_reporting,
 max(COALESCE((coverage#>>'{hashrate,expected}')::int,0)) AS efficiency_expected,
 bool_and(COALESCE((coverage#>>'{hashrate,topology_complete}')::boolean,false) AND COALESCE((coverage#>>'{hashrate,reporting}')::int,0)=COALESCE((coverage#>>'{hashrate,expected}')::int,0) AND COALESCE((coverage#>>'{hashrate,expected}')::int,0)>0 AND COALESCE(efficiency_jh > '-Infinity'::float8 AND efficiency_jh < 'Infinity'::float8,false)) AS efficiency_complete
FROM container_module_metrics WHERE coverage IS NOT NULL AND topology_version_id IS NOT NULL
GROUP BY bucket,module_id,site_id,topology_version_id WITH NO DATA;
CREATE INDEX idx_container_module_metrics_hourly_equipment_bucket ON container_module_metrics_hourly(module_id,bucket DESC);

-- Separate topology versions and sites even when a move falls inside a bucket.
CREATE MATERIALIZED VIEW container_module_metrics_daily
WITH (timescaledb.continuous, timescaledb.materialized_only=false) AS
SELECT time_bucket(INTERVAL '1 day',time) AS bucket,module_id,site_id,topology_version_id,
 sum(temperature_c) FILTER (WHERE temperature_c > '-Infinity'::float8 AND temperature_c < 'Infinity'::float8) AS temperature_sum,
 count(temperature_c) FILTER (WHERE temperature_c > '-Infinity'::float8 AND temperature_c < 'Infinity'::float8) AS temperature_count,
 min(temperature_c) FILTER (WHERE temperature_c > '-Infinity'::float8 AND temperature_c < 'Infinity'::float8) AS temperature_min,
 max(temperature_c) FILTER (WHERE temperature_c > '-Infinity'::float8 AND temperature_c < 'Infinity'::float8) AS temperature_max,
 min(COALESCE((coverage#>>'{temperature,reporting}')::int,0)) AS temperature_reporting,
 max(COALESCE((coverage#>>'{temperature,expected}')::int,0)) AS temperature_expected,
 bool_and(COALESCE((coverage#>>'{temperature,topology_complete}')::boolean,false) AND COALESCE((coverage#>>'{temperature,reporting}')::int,0)=COALESCE((coverage#>>'{temperature,expected}')::int,0) AND COALESCE((coverage#>>'{temperature,expected}')::int,0)>0 AND COALESCE(temperature_c > '-Infinity'::float8 AND temperature_c < 'Infinity'::float8,false)) AS temperature_complete,
 sum(hashrate_hs) FILTER (WHERE hashrate_hs > '-Infinity'::float8 AND hashrate_hs < 'Infinity'::float8) AS hashrate_sum,
 count(hashrate_hs) FILTER (WHERE hashrate_hs > '-Infinity'::float8 AND hashrate_hs < 'Infinity'::float8) AS hashrate_count,
 min(hashrate_hs) FILTER (WHERE hashrate_hs > '-Infinity'::float8 AND hashrate_hs < 'Infinity'::float8) AS hashrate_min,
 max(hashrate_hs) FILTER (WHERE hashrate_hs > '-Infinity'::float8 AND hashrate_hs < 'Infinity'::float8) AS hashrate_max,
 min(COALESCE((coverage#>>'{hashrate,reporting}')::int,0)) AS hashrate_reporting,
 max(COALESCE((coverage#>>'{hashrate,expected}')::int,0)) AS hashrate_expected,
 bool_and(COALESCE((coverage#>>'{hashrate,topology_complete}')::boolean,false) AND COALESCE((coverage#>>'{hashrate,reporting}')::int,0)=COALESCE((coverage#>>'{hashrate,expected}')::int,0) AND COALESCE((coverage#>>'{hashrate,expected}')::int,0)>0 AND COALESCE(hashrate_hs > '-Infinity'::float8 AND hashrate_hs < 'Infinity'::float8,false)) AS hashrate_complete,
 sum(power_w) FILTER (WHERE power_w > '-Infinity'::float8 AND power_w < 'Infinity'::float8) AS power_sum,
 count(power_w) FILTER (WHERE power_w > '-Infinity'::float8 AND power_w < 'Infinity'::float8) AS power_count,
 min(power_w) FILTER (WHERE power_w > '-Infinity'::float8 AND power_w < 'Infinity'::float8) AS power_min,
 max(power_w) FILTER (WHERE power_w > '-Infinity'::float8 AND power_w < 'Infinity'::float8) AS power_max,
 min(COALESCE((coverage#>>'{power,reporting}')::int,0)) AS power_reporting,
 max(COALESCE((coverage#>>'{power,expected}')::int,0)) AS power_expected,
 bool_and(COALESCE((coverage#>>'{power,topology_complete}')::boolean,false) AND COALESCE((coverage#>>'{power,reporting}')::int,0)=COALESCE((coverage#>>'{power,expected}')::int,0) AND COALESCE((coverage#>>'{power,expected}')::int,0)>0 AND COALESCE(power_w > '-Infinity'::float8 AND power_w < 'Infinity'::float8,false)) AS power_complete,
 sum(efficiency_jh) FILTER (WHERE efficiency_jh > '-Infinity'::float8 AND efficiency_jh < 'Infinity'::float8) AS efficiency_sum,
 count(efficiency_jh) FILTER (WHERE efficiency_jh > '-Infinity'::float8 AND efficiency_jh < 'Infinity'::float8) AS efficiency_count,
 min(efficiency_jh) FILTER (WHERE efficiency_jh > '-Infinity'::float8 AND efficiency_jh < 'Infinity'::float8) AS efficiency_min,
 max(efficiency_jh) FILTER (WHERE efficiency_jh > '-Infinity'::float8 AND efficiency_jh < 'Infinity'::float8) AS efficiency_max,
 min(COALESCE((coverage#>>'{hashrate,reporting}')::int,0)) AS efficiency_reporting,
 max(COALESCE((coverage#>>'{hashrate,expected}')::int,0)) AS efficiency_expected,
 bool_and(COALESCE((coverage#>>'{hashrate,topology_complete}')::boolean,false) AND COALESCE((coverage#>>'{hashrate,reporting}')::int,0)=COALESCE((coverage#>>'{hashrate,expected}')::int,0) AND COALESCE((coverage#>>'{hashrate,expected}')::int,0)>0 AND COALESCE(efficiency_jh > '-Infinity'::float8 AND efficiency_jh < 'Infinity'::float8,false)) AS efficiency_complete
FROM container_module_metrics WHERE coverage IS NOT NULL AND topology_version_id IS NOT NULL
GROUP BY bucket,module_id,site_id,topology_version_id WITH NO DATA;
CREATE INDEX idx_container_module_metrics_daily_equipment_bucket ON container_module_metrics_daily(module_id,bucket DESC);

-- One typed read surface; resolution filters prune unused UNION branches.
CREATE VIEW container_equipment_history AS
SELECT 2::int AS equipment_kind,device_identifier::text AS container_identifier,NULL::bigint AS tank_id,NULL::bigint AS module_id,site_id,topology_version_id,time AS bucket,time AS collected_at,0::int AS resolution_seconds,1::int AS measurement_type,
 CASE WHEN temp_c > '-Infinity'::float8 AND temp_c < 'Infinity'::float8 THEN temp_c END::float8 AS total,CASE WHEN temp_c > '-Infinity'::float8 AND temp_c < 'Infinity'::float8 THEN 1 ELSE 0 END::bigint AS samples,
 CASE WHEN temp_c > '-Infinity'::float8 AND temp_c < 'Infinity'::float8 THEN temp_c END::float8 AS minimum,CASE WHEN temp_c > '-Infinity'::float8 AND temp_c < 'Infinity'::float8 THEN temp_c END::float8 AS maximum,
 COALESCE((coverage#>>'{temperature,reporting}')::int,0) AS reporting_count,COALESCE((coverage#>>'{temperature,expected}')::int,0) AS expected_count,(COALESCE((coverage#>>'{temperature,topology_complete}')::boolean,false) AND COALESCE((coverage#>>'{temperature,reporting}')::int,0)=COALESCE((coverage#>>'{temperature,expected}')::int,0) AND COALESCE((coverage#>>'{temperature,expected}')::int,0)>0 AND COALESCE(temp_c > '-Infinity'::float8 AND temp_c < 'Infinity'::float8,false)) AS coverage_complete
FROM device_metrics WHERE metric_semantics=2
UNION ALL
SELECT 2::int AS equipment_kind,device_identifier::text AS container_identifier,NULL::bigint AS tank_id,NULL::bigint AS module_id,site_id,topology_version_id,time AS bucket,time AS collected_at,0::int AS resolution_seconds,2::int AS measurement_type,
 CASE WHEN hash_rate_hs > '-Infinity'::float8 AND hash_rate_hs < 'Infinity'::float8 THEN hash_rate_hs END::float8 AS total,CASE WHEN hash_rate_hs > '-Infinity'::float8 AND hash_rate_hs < 'Infinity'::float8 THEN 1 ELSE 0 END::bigint AS samples,
 CASE WHEN hash_rate_hs > '-Infinity'::float8 AND hash_rate_hs < 'Infinity'::float8 THEN hash_rate_hs END::float8 AS minimum,CASE WHEN hash_rate_hs > '-Infinity'::float8 AND hash_rate_hs < 'Infinity'::float8 THEN hash_rate_hs END::float8 AS maximum,
 COALESCE((coverage#>>'{hashrate,reporting}')::int,0) AS reporting_count,COALESCE((coverage#>>'{hashrate,expected}')::int,0) AS expected_count,(COALESCE((coverage#>>'{hashrate,topology_complete}')::boolean,false) AND COALESCE((coverage#>>'{hashrate,reporting}')::int,0)=COALESCE((coverage#>>'{hashrate,expected}')::int,0) AND COALESCE((coverage#>>'{hashrate,expected}')::int,0)>0 AND COALESCE(hash_rate_hs > '-Infinity'::float8 AND hash_rate_hs < 'Infinity'::float8,false)) AS coverage_complete
FROM device_metrics WHERE metric_semantics=2
UNION ALL
SELECT 2::int AS equipment_kind,device_identifier::text AS container_identifier,NULL::bigint AS tank_id,NULL::bigint AS module_id,site_id,topology_version_id,time AS bucket,time AS collected_at,0::int AS resolution_seconds,3::int AS measurement_type,
 CASE WHEN power_w > '-Infinity'::float8 AND power_w < 'Infinity'::float8 THEN power_w END::float8 AS total,CASE WHEN power_w > '-Infinity'::float8 AND power_w < 'Infinity'::float8 THEN 1 ELSE 0 END::bigint AS samples,
 CASE WHEN power_w > '-Infinity'::float8 AND power_w < 'Infinity'::float8 THEN power_w END::float8 AS minimum,CASE WHEN power_w > '-Infinity'::float8 AND power_w < 'Infinity'::float8 THEN power_w END::float8 AS maximum,
 COALESCE((coverage#>>'{power,reporting}')::int,0) AS reporting_count,COALESCE((coverage#>>'{power,expected}')::int,0) AS expected_count,(COALESCE((coverage#>>'{power,topology_complete}')::boolean,false) AND COALESCE((coverage#>>'{power,reporting}')::int,0)=COALESCE((coverage#>>'{power,expected}')::int,0) AND COALESCE((coverage#>>'{power,expected}')::int,0)>0 AND COALESCE(power_w > '-Infinity'::float8 AND power_w < 'Infinity'::float8,false)) AS coverage_complete
FROM device_metrics WHERE metric_semantics=2
UNION ALL
SELECT 2::int AS equipment_kind,device_identifier::text AS container_identifier,NULL::bigint AS tank_id,NULL::bigint AS module_id,site_id,topology_version_id,time AS bucket,time AS collected_at,0::int AS resolution_seconds,4::int AS measurement_type,
 CASE WHEN efficiency_jh > '-Infinity'::float8 AND efficiency_jh < 'Infinity'::float8 THEN efficiency_jh END::float8 AS total,CASE WHEN efficiency_jh > '-Infinity'::float8 AND efficiency_jh < 'Infinity'::float8 THEN 1 ELSE 0 END::bigint AS samples,
 CASE WHEN efficiency_jh > '-Infinity'::float8 AND efficiency_jh < 'Infinity'::float8 THEN efficiency_jh END::float8 AS minimum,CASE WHEN efficiency_jh > '-Infinity'::float8 AND efficiency_jh < 'Infinity'::float8 THEN efficiency_jh END::float8 AS maximum,
 COALESCE((coverage#>>'{hashrate,reporting}')::int,0) AS reporting_count,COALESCE((coverage#>>'{hashrate,expected}')::int,0) AS expected_count,(COALESCE((coverage#>>'{hashrate,topology_complete}')::boolean,false) AND COALESCE((coverage#>>'{hashrate,reporting}')::int,0)=COALESCE((coverage#>>'{hashrate,expected}')::int,0) AND COALESCE((coverage#>>'{hashrate,expected}')::int,0)>0 AND COALESCE(efficiency_jh > '-Infinity'::float8 AND efficiency_jh < 'Infinity'::float8,false)) AS coverage_complete
FROM device_metrics WHERE metric_semantics=2
UNION ALL
SELECT 2::int,device_identifier::text AS container_identifier,NULL::bigint AS tank_id,NULL::bigint AS module_id,site_id,topology_version_id,bucket,bucket,3600::int,1::int,
 temperature_sum::float8,temperature_count::bigint,temperature_min::float8,temperature_max::float8,temperature_reporting::int,temperature_expected::int,temperature_complete::boolean
FROM container_metrics_hourly
UNION ALL
SELECT 2::int,device_identifier::text AS container_identifier,NULL::bigint AS tank_id,NULL::bigint AS module_id,site_id,topology_version_id,bucket,bucket,3600::int,2::int,
 hashrate_sum::float8,hashrate_count::bigint,hashrate_min::float8,hashrate_max::float8,hashrate_reporting::int,hashrate_expected::int,hashrate_complete::boolean
FROM container_metrics_hourly
UNION ALL
SELECT 2::int,device_identifier::text AS container_identifier,NULL::bigint AS tank_id,NULL::bigint AS module_id,site_id,topology_version_id,bucket,bucket,3600::int,3::int,
 power_sum::float8,power_count::bigint,power_min::float8,power_max::float8,power_reporting::int,power_expected::int,power_complete::boolean
FROM container_metrics_hourly
UNION ALL
SELECT 2::int,device_identifier::text AS container_identifier,NULL::bigint AS tank_id,NULL::bigint AS module_id,site_id,topology_version_id,bucket,bucket,3600::int,4::int,
 efficiency_sum::float8,efficiency_count::bigint,efficiency_min::float8,efficiency_max::float8,efficiency_reporting::int,efficiency_expected::int,efficiency_complete::boolean
FROM container_metrics_hourly
UNION ALL
SELECT 2::int,device_identifier::text AS container_identifier,NULL::bigint AS tank_id,NULL::bigint AS module_id,site_id,topology_version_id,bucket,bucket,86400::int,1::int,
 temperature_sum::float8,temperature_count::bigint,temperature_min::float8,temperature_max::float8,temperature_reporting::int,temperature_expected::int,temperature_complete::boolean
FROM container_metrics_daily
UNION ALL
SELECT 2::int,device_identifier::text AS container_identifier,NULL::bigint AS tank_id,NULL::bigint AS module_id,site_id,topology_version_id,bucket,bucket,86400::int,2::int,
 hashrate_sum::float8,hashrate_count::bigint,hashrate_min::float8,hashrate_max::float8,hashrate_reporting::int,hashrate_expected::int,hashrate_complete::boolean
FROM container_metrics_daily
UNION ALL
SELECT 2::int,device_identifier::text AS container_identifier,NULL::bigint AS tank_id,NULL::bigint AS module_id,site_id,topology_version_id,bucket,bucket,86400::int,3::int,
 power_sum::float8,power_count::bigint,power_min::float8,power_max::float8,power_reporting::int,power_expected::int,power_complete::boolean
FROM container_metrics_daily
UNION ALL
SELECT 2::int,device_identifier::text AS container_identifier,NULL::bigint AS tank_id,NULL::bigint AS module_id,site_id,topology_version_id,bucket,bucket,86400::int,4::int,
 efficiency_sum::float8,efficiency_count::bigint,efficiency_min::float8,efficiency_max::float8,efficiency_reporting::int,efficiency_expected::int,efficiency_complete::boolean
FROM container_metrics_daily
UNION ALL
SELECT 4::int AS equipment_kind,NULL::text AS container_identifier,tank_id AS tank_id,NULL::bigint AS module_id,site_id,topology_version_id,time AS bucket,time AS collected_at,0::int AS resolution_seconds,1::int AS measurement_type,
 CASE WHEN temperature_c > '-Infinity'::float8 AND temperature_c < 'Infinity'::float8 THEN temperature_c END::float8 AS total,CASE WHEN temperature_c > '-Infinity'::float8 AND temperature_c < 'Infinity'::float8 THEN 1 ELSE 0 END::bigint AS samples,
 CASE WHEN min_temperature_c > '-Infinity'::float8 AND min_temperature_c < 'Infinity'::float8 THEN min_temperature_c END::float8 AS minimum,CASE WHEN max_temperature_c > '-Infinity'::float8 AND max_temperature_c < 'Infinity'::float8 THEN max_temperature_c END::float8 AS maximum,
 COALESCE((coverage#>>'{temperature,reporting}')::int,0) AS reporting_count,COALESCE((coverage#>>'{temperature,expected}')::int,0) AS expected_count,(COALESCE((coverage#>>'{temperature,topology_complete}')::boolean,false) AND COALESCE((coverage#>>'{temperature,reporting}')::int,0)=COALESCE((coverage#>>'{temperature,expected}')::int,0) AND COALESCE((coverage#>>'{temperature,expected}')::int,0)>0 AND COALESCE(temperature_c > '-Infinity'::float8 AND temperature_c < 'Infinity'::float8,false)) AS coverage_complete
FROM container_tank_metrics
UNION ALL
SELECT 4::int AS equipment_kind,NULL::text AS container_identifier,tank_id AS tank_id,NULL::bigint AS module_id,site_id,topology_version_id,time AS bucket,time AS collected_at,0::int AS resolution_seconds,2::int AS measurement_type,
 CASE WHEN hashrate_hs > '-Infinity'::float8 AND hashrate_hs < 'Infinity'::float8 THEN hashrate_hs END::float8 AS total,CASE WHEN hashrate_hs > '-Infinity'::float8 AND hashrate_hs < 'Infinity'::float8 THEN 1 ELSE 0 END::bigint AS samples,
 CASE WHEN hashrate_hs > '-Infinity'::float8 AND hashrate_hs < 'Infinity'::float8 THEN hashrate_hs END::float8 AS minimum,CASE WHEN hashrate_hs > '-Infinity'::float8 AND hashrate_hs < 'Infinity'::float8 THEN hashrate_hs END::float8 AS maximum,
 COALESCE((coverage#>>'{hashrate,reporting}')::int,0) AS reporting_count,COALESCE((coverage#>>'{hashrate,expected}')::int,0) AS expected_count,(COALESCE((coverage#>>'{hashrate,topology_complete}')::boolean,false) AND COALESCE((coverage#>>'{hashrate,reporting}')::int,0)=COALESCE((coverage#>>'{hashrate,expected}')::int,0) AND COALESCE((coverage#>>'{hashrate,expected}')::int,0)>0 AND COALESCE(hashrate_hs > '-Infinity'::float8 AND hashrate_hs < 'Infinity'::float8,false)) AS coverage_complete
FROM container_tank_metrics
UNION ALL
SELECT 4::int AS equipment_kind,NULL::text AS container_identifier,tank_id AS tank_id,NULL::bigint AS module_id,site_id,topology_version_id,time AS bucket,time AS collected_at,0::int AS resolution_seconds,3::int AS measurement_type,
 CASE WHEN power_w > '-Infinity'::float8 AND power_w < 'Infinity'::float8 THEN power_w END::float8 AS total,CASE WHEN power_w > '-Infinity'::float8 AND power_w < 'Infinity'::float8 THEN 1 ELSE 0 END::bigint AS samples,
 CASE WHEN power_w > '-Infinity'::float8 AND power_w < 'Infinity'::float8 THEN power_w END::float8 AS minimum,CASE WHEN power_w > '-Infinity'::float8 AND power_w < 'Infinity'::float8 THEN power_w END::float8 AS maximum,
 COALESCE((coverage#>>'{power,reporting}')::int,0) AS reporting_count,COALESCE((coverage#>>'{power,expected}')::int,0) AS expected_count,(COALESCE((coverage#>>'{power,topology_complete}')::boolean,false) AND COALESCE((coverage#>>'{power,reporting}')::int,0)=COALESCE((coverage#>>'{power,expected}')::int,0) AND COALESCE((coverage#>>'{power,expected}')::int,0)>0 AND COALESCE(power_w > '-Infinity'::float8 AND power_w < 'Infinity'::float8,false)) AS coverage_complete
FROM container_tank_metrics
UNION ALL
SELECT 4::int AS equipment_kind,NULL::text AS container_identifier,tank_id AS tank_id,NULL::bigint AS module_id,site_id,topology_version_id,time AS bucket,time AS collected_at,0::int AS resolution_seconds,4::int AS measurement_type,
 CASE WHEN efficiency_jh > '-Infinity'::float8 AND efficiency_jh < 'Infinity'::float8 THEN efficiency_jh END::float8 AS total,CASE WHEN efficiency_jh > '-Infinity'::float8 AND efficiency_jh < 'Infinity'::float8 THEN 1 ELSE 0 END::bigint AS samples,
 CASE WHEN efficiency_jh > '-Infinity'::float8 AND efficiency_jh < 'Infinity'::float8 THEN efficiency_jh END::float8 AS minimum,CASE WHEN efficiency_jh > '-Infinity'::float8 AND efficiency_jh < 'Infinity'::float8 THEN efficiency_jh END::float8 AS maximum,
 COALESCE((coverage#>>'{hashrate,reporting}')::int,0) AS reporting_count,COALESCE((coverage#>>'{hashrate,expected}')::int,0) AS expected_count,(COALESCE((coverage#>>'{hashrate,topology_complete}')::boolean,false) AND COALESCE((coverage#>>'{hashrate,reporting}')::int,0)=COALESCE((coverage#>>'{hashrate,expected}')::int,0) AND COALESCE((coverage#>>'{hashrate,expected}')::int,0)>0 AND COALESCE(efficiency_jh > '-Infinity'::float8 AND efficiency_jh < 'Infinity'::float8,false)) AS coverage_complete
FROM container_tank_metrics
UNION ALL
SELECT 4::int,NULL::text AS container_identifier,tank_id AS tank_id,NULL::bigint AS module_id,site_id,topology_version_id,bucket,bucket,3600::int,1::int,
 temperature_sum::float8,temperature_count::bigint,temperature_min::float8,temperature_max::float8,temperature_reporting::int,temperature_expected::int,temperature_complete::boolean
FROM container_tank_metrics_hourly
UNION ALL
SELECT 4::int,NULL::text AS container_identifier,tank_id AS tank_id,NULL::bigint AS module_id,site_id,topology_version_id,bucket,bucket,3600::int,2::int,
 hashrate_sum::float8,hashrate_count::bigint,hashrate_min::float8,hashrate_max::float8,hashrate_reporting::int,hashrate_expected::int,hashrate_complete::boolean
FROM container_tank_metrics_hourly
UNION ALL
SELECT 4::int,NULL::text AS container_identifier,tank_id AS tank_id,NULL::bigint AS module_id,site_id,topology_version_id,bucket,bucket,3600::int,3::int,
 power_sum::float8,power_count::bigint,power_min::float8,power_max::float8,power_reporting::int,power_expected::int,power_complete::boolean
FROM container_tank_metrics_hourly
UNION ALL
SELECT 4::int,NULL::text AS container_identifier,tank_id AS tank_id,NULL::bigint AS module_id,site_id,topology_version_id,bucket,bucket,3600::int,4::int,
 efficiency_sum::float8,efficiency_count::bigint,efficiency_min::float8,efficiency_max::float8,efficiency_reporting::int,efficiency_expected::int,efficiency_complete::boolean
FROM container_tank_metrics_hourly
UNION ALL
SELECT 4::int,NULL::text AS container_identifier,tank_id AS tank_id,NULL::bigint AS module_id,site_id,topology_version_id,bucket,bucket,86400::int,1::int,
 temperature_sum::float8,temperature_count::bigint,temperature_min::float8,temperature_max::float8,temperature_reporting::int,temperature_expected::int,temperature_complete::boolean
FROM container_tank_metrics_daily
UNION ALL
SELECT 4::int,NULL::text AS container_identifier,tank_id AS tank_id,NULL::bigint AS module_id,site_id,topology_version_id,bucket,bucket,86400::int,2::int,
 hashrate_sum::float8,hashrate_count::bigint,hashrate_min::float8,hashrate_max::float8,hashrate_reporting::int,hashrate_expected::int,hashrate_complete::boolean
FROM container_tank_metrics_daily
UNION ALL
SELECT 4::int,NULL::text AS container_identifier,tank_id AS tank_id,NULL::bigint AS module_id,site_id,topology_version_id,bucket,bucket,86400::int,3::int,
 power_sum::float8,power_count::bigint,power_min::float8,power_max::float8,power_reporting::int,power_expected::int,power_complete::boolean
FROM container_tank_metrics_daily
UNION ALL
SELECT 4::int,NULL::text AS container_identifier,tank_id AS tank_id,NULL::bigint AS module_id,site_id,topology_version_id,bucket,bucket,86400::int,4::int,
 efficiency_sum::float8,efficiency_count::bigint,efficiency_min::float8,efficiency_max::float8,efficiency_reporting::int,efficiency_expected::int,efficiency_complete::boolean
FROM container_tank_metrics_daily
UNION ALL
SELECT 6::int AS equipment_kind,NULL::text AS container_identifier,NULL::bigint AS tank_id,module_id AS module_id,site_id,topology_version_id,time AS bucket,collected_at AS collected_at,0::int AS resolution_seconds,1::int AS measurement_type,
 CASE WHEN temperature_c > '-Infinity'::float8 AND temperature_c < 'Infinity'::float8 THEN temperature_c END::float8 AS total,CASE WHEN temperature_c > '-Infinity'::float8 AND temperature_c < 'Infinity'::float8 THEN 1 ELSE 0 END::bigint AS samples,
 CASE WHEN temperature_c > '-Infinity'::float8 AND temperature_c < 'Infinity'::float8 THEN temperature_c END::float8 AS minimum,CASE WHEN temperature_c > '-Infinity'::float8 AND temperature_c < 'Infinity'::float8 THEN temperature_c END::float8 AS maximum,
 COALESCE((coverage#>>'{temperature,reporting}')::int,0) AS reporting_count,COALESCE((coverage#>>'{temperature,expected}')::int,0) AS expected_count,(COALESCE((coverage#>>'{temperature,topology_complete}')::boolean,false) AND COALESCE((coverage#>>'{temperature,reporting}')::int,0)=COALESCE((coverage#>>'{temperature,expected}')::int,0) AND COALESCE((coverage#>>'{temperature,expected}')::int,0)>0 AND COALESCE(temperature_c > '-Infinity'::float8 AND temperature_c < 'Infinity'::float8,false)) AS coverage_complete
FROM container_module_metrics
UNION ALL
SELECT 6::int AS equipment_kind,NULL::text AS container_identifier,NULL::bigint AS tank_id,module_id AS module_id,site_id,topology_version_id,time AS bucket,collected_at AS collected_at,0::int AS resolution_seconds,2::int AS measurement_type,
 CASE WHEN hashrate_hs > '-Infinity'::float8 AND hashrate_hs < 'Infinity'::float8 THEN hashrate_hs END::float8 AS total,CASE WHEN hashrate_hs > '-Infinity'::float8 AND hashrate_hs < 'Infinity'::float8 THEN 1 ELSE 0 END::bigint AS samples,
 CASE WHEN hashrate_hs > '-Infinity'::float8 AND hashrate_hs < 'Infinity'::float8 THEN hashrate_hs END::float8 AS minimum,CASE WHEN hashrate_hs > '-Infinity'::float8 AND hashrate_hs < 'Infinity'::float8 THEN hashrate_hs END::float8 AS maximum,
 COALESCE((coverage#>>'{hashrate,reporting}')::int,0) AS reporting_count,COALESCE((coverage#>>'{hashrate,expected}')::int,0) AS expected_count,(COALESCE((coverage#>>'{hashrate,topology_complete}')::boolean,false) AND COALESCE((coverage#>>'{hashrate,reporting}')::int,0)=COALESCE((coverage#>>'{hashrate,expected}')::int,0) AND COALESCE((coverage#>>'{hashrate,expected}')::int,0)>0 AND COALESCE(hashrate_hs > '-Infinity'::float8 AND hashrate_hs < 'Infinity'::float8,false)) AS coverage_complete
FROM container_module_metrics
UNION ALL
SELECT 6::int AS equipment_kind,NULL::text AS container_identifier,NULL::bigint AS tank_id,module_id AS module_id,site_id,topology_version_id,time AS bucket,collected_at AS collected_at,0::int AS resolution_seconds,3::int AS measurement_type,
 CASE WHEN power_w > '-Infinity'::float8 AND power_w < 'Infinity'::float8 THEN power_w END::float8 AS total,CASE WHEN power_w > '-Infinity'::float8 AND power_w < 'Infinity'::float8 THEN 1 ELSE 0 END::bigint AS samples,
 CASE WHEN power_w > '-Infinity'::float8 AND power_w < 'Infinity'::float8 THEN power_w END::float8 AS minimum,CASE WHEN power_w > '-Infinity'::float8 AND power_w < 'Infinity'::float8 THEN power_w END::float8 AS maximum,
 COALESCE((coverage#>>'{power,reporting}')::int,0) AS reporting_count,COALESCE((coverage#>>'{power,expected}')::int,0) AS expected_count,(COALESCE((coverage#>>'{power,topology_complete}')::boolean,false) AND COALESCE((coverage#>>'{power,reporting}')::int,0)=COALESCE((coverage#>>'{power,expected}')::int,0) AND COALESCE((coverage#>>'{power,expected}')::int,0)>0 AND COALESCE(power_w > '-Infinity'::float8 AND power_w < 'Infinity'::float8,false)) AS coverage_complete
FROM container_module_metrics
UNION ALL
SELECT 6::int AS equipment_kind,NULL::text AS container_identifier,NULL::bigint AS tank_id,module_id AS module_id,site_id,topology_version_id,time AS bucket,collected_at AS collected_at,0::int AS resolution_seconds,4::int AS measurement_type,
 CASE WHEN efficiency_jh > '-Infinity'::float8 AND efficiency_jh < 'Infinity'::float8 THEN efficiency_jh END::float8 AS total,CASE WHEN efficiency_jh > '-Infinity'::float8 AND efficiency_jh < 'Infinity'::float8 THEN 1 ELSE 0 END::bigint AS samples,
 CASE WHEN efficiency_jh > '-Infinity'::float8 AND efficiency_jh < 'Infinity'::float8 THEN efficiency_jh END::float8 AS minimum,CASE WHEN efficiency_jh > '-Infinity'::float8 AND efficiency_jh < 'Infinity'::float8 THEN efficiency_jh END::float8 AS maximum,
 COALESCE((coverage#>>'{hashrate,reporting}')::int,0) AS reporting_count,COALESCE((coverage#>>'{hashrate,expected}')::int,0) AS expected_count,(COALESCE((coverage#>>'{hashrate,topology_complete}')::boolean,false) AND COALESCE((coverage#>>'{hashrate,reporting}')::int,0)=COALESCE((coverage#>>'{hashrate,expected}')::int,0) AND COALESCE((coverage#>>'{hashrate,expected}')::int,0)>0 AND COALESCE(efficiency_jh > '-Infinity'::float8 AND efficiency_jh < 'Infinity'::float8,false)) AS coverage_complete
FROM container_module_metrics
UNION ALL
SELECT 6::int,NULL::text AS container_identifier,NULL::bigint AS tank_id,module_id AS module_id,site_id,topology_version_id,bucket,bucket,3600::int,1::int,
 temperature_sum::float8,temperature_count::bigint,temperature_min::float8,temperature_max::float8,temperature_reporting::int,temperature_expected::int,temperature_complete::boolean
FROM container_module_metrics_hourly
UNION ALL
SELECT 6::int,NULL::text AS container_identifier,NULL::bigint AS tank_id,module_id AS module_id,site_id,topology_version_id,bucket,bucket,3600::int,2::int,
 hashrate_sum::float8,hashrate_count::bigint,hashrate_min::float8,hashrate_max::float8,hashrate_reporting::int,hashrate_expected::int,hashrate_complete::boolean
FROM container_module_metrics_hourly
UNION ALL
SELECT 6::int,NULL::text AS container_identifier,NULL::bigint AS tank_id,module_id AS module_id,site_id,topology_version_id,bucket,bucket,3600::int,3::int,
 power_sum::float8,power_count::bigint,power_min::float8,power_max::float8,power_reporting::int,power_expected::int,power_complete::boolean
FROM container_module_metrics_hourly
UNION ALL
SELECT 6::int,NULL::text AS container_identifier,NULL::bigint AS tank_id,module_id AS module_id,site_id,topology_version_id,bucket,bucket,3600::int,4::int,
 efficiency_sum::float8,efficiency_count::bigint,efficiency_min::float8,efficiency_max::float8,efficiency_reporting::int,efficiency_expected::int,efficiency_complete::boolean
FROM container_module_metrics_hourly
UNION ALL
SELECT 6::int,NULL::text AS container_identifier,NULL::bigint AS tank_id,module_id AS module_id,site_id,topology_version_id,bucket,bucket,86400::int,1::int,
 temperature_sum::float8,temperature_count::bigint,temperature_min::float8,temperature_max::float8,temperature_reporting::int,temperature_expected::int,temperature_complete::boolean
FROM container_module_metrics_daily
UNION ALL
SELECT 6::int,NULL::text AS container_identifier,NULL::bigint AS tank_id,module_id AS module_id,site_id,topology_version_id,bucket,bucket,86400::int,2::int,
 hashrate_sum::float8,hashrate_count::bigint,hashrate_min::float8,hashrate_max::float8,hashrate_reporting::int,hashrate_expected::int,hashrate_complete::boolean
FROM container_module_metrics_daily
UNION ALL
SELECT 6::int,NULL::text AS container_identifier,NULL::bigint AS tank_id,module_id AS module_id,site_id,topology_version_id,bucket,bucket,86400::int,3::int,
 power_sum::float8,power_count::bigint,power_min::float8,power_max::float8,power_reporting::int,power_expected::int,power_complete::boolean
FROM container_module_metrics_daily
UNION ALL
SELECT 6::int,NULL::text AS container_identifier,NULL::bigint AS tank_id,module_id AS module_id,site_id,topology_version_id,bucket,bucket,86400::int,4::int,
 efficiency_sum::float8,efficiency_count::bigint,efficiency_min::float8,efficiency_max::float8,efficiency_reporting::int,efficiency_expected::int,efficiency_complete::boolean
FROM container_module_metrics_daily;

-- Match miner routing, also honoring the available tier for old narrow windows.
CREATE FUNCTION container_history_resolution(start_time timestamptz,end_time timestamptz)
RETURNS integer LANGUAGE sql STABLE AS $$
 SELECT CASE WHEN end_time-start_time <= INTERVAL '1 day' AND start_time >= now()-INTERVAL '10 days' THEN 0
 WHEN end_time-start_time <= INTERVAL '10 days' AND start_time >= now()-INTERVAL '3 months' THEN 3600
 ELSE 86400 END;
$$;

-- Timescale refresh cannot run inside a migration transaction. A scheduled
-- procedure backfills first, then enables policies and removes its own job.
-- Failed backfills retry with raw retention disabled, preserving the evidence.
CREATE PROCEDURE initialize_container_metric_summaries(job_id integer,config jsonb)
LANGUAGE plpgsql AS $$
BEGIN

 CALL refresh_continuous_aggregate('container_metrics_hourly',NULL,now());
 CALL refresh_continuous_aggregate('container_metrics_daily',NULL,now());
 CALL refresh_continuous_aggregate('container_tank_metrics_hourly',NULL,now());
 CALL refresh_continuous_aggregate('container_tank_metrics_daily',NULL,now());
 CALL refresh_continuous_aggregate('container_module_metrics_hourly',NULL,now());
 CALL refresh_continuous_aggregate('container_module_metrics_daily',NULL,now());
 PERFORM add_continuous_aggregate_policy('container_metrics_hourly',start_offset=>INTERVAL '1 day',end_offset=>INTERVAL '1 hour',schedule_interval=>INTERVAL '30 minutes',if_not_exists=>true);
 PERFORM add_retention_policy('container_metrics_hourly',INTERVAL '3 months',if_not_exists=>true);
 PERFORM add_continuous_aggregate_policy('container_metrics_daily',start_offset=>INTERVAL '7 days',end_offset=>INTERVAL '1 day',schedule_interval=>INTERVAL '6 hours',if_not_exists=>true);
 PERFORM add_retention_policy('container_metrics_daily',INTERVAL '3 years',if_not_exists=>true);
 PERFORM add_continuous_aggregate_policy('container_tank_metrics_hourly',start_offset=>INTERVAL '1 day',end_offset=>INTERVAL '1 hour',schedule_interval=>INTERVAL '30 minutes',if_not_exists=>true);
 PERFORM add_retention_policy('container_tank_metrics_hourly',INTERVAL '3 months',if_not_exists=>true);
 PERFORM add_continuous_aggregate_policy('container_tank_metrics_daily',start_offset=>INTERVAL '7 days',end_offset=>INTERVAL '1 day',schedule_interval=>INTERVAL '6 hours',if_not_exists=>true);
 PERFORM add_retention_policy('container_tank_metrics_daily',INTERVAL '3 years',if_not_exists=>true);
 PERFORM add_continuous_aggregate_policy('container_module_metrics_hourly',start_offset=>INTERVAL '1 day',end_offset=>INTERVAL '1 hour',schedule_interval=>INTERVAL '30 minutes',if_not_exists=>true);
 PERFORM add_retention_policy('container_module_metrics_hourly',INTERVAL '3 months',if_not_exists=>true);
 PERFORM add_continuous_aggregate_policy('container_module_metrics_daily',start_offset=>INTERVAL '7 days',end_offset=>INTERVAL '1 day',schedule_interval=>INTERVAL '6 hours',if_not_exists=>true);
 PERFORM add_retention_policy('container_module_metrics_daily',INTERVAL '3 years',if_not_exists=>true);
 PERFORM add_retention_policy('device_metrics',INTERVAL '10 days',if_not_exists=>true);
 PERFORM add_retention_policy('container_tank_metrics',INTERVAL '10 days',if_not_exists=>true);
 PERFORM add_retention_policy('container_module_metrics',INTERVAL '10 days',if_not_exists=>true);
 PERFORM delete_job(job_id);
END;
$$;
SELECT add_job('initialize_container_metric_summaries',INTERVAL '1 hour',initial_start=>now()+INTERVAL '1 minute');

-- fleet-migration: 001018_internal_hosted_curtailment_control.up.sql
-- requires-shared: 1000

-- Keep fleet curtailment authority distinct from pool/configuration ownership.
ALTER TABLE device_write_attempt ADD COLUMN curtailment_command TEXT
 CHECK (curtailment_command IN ('Curtail','Uncurtail','ApplyCurtailmentConfig'));

-- fleet-migration: 001019_internal_container_module_identity_confirmation.up.sql
-- requires-shared: 1000

-- Historical reported_id identifies the hardware whose history belongs to this
-- row; only a fresh identity-bearing observation confirms it for commands.
ALTER TABLE container_module ADD COLUMN identity_confirmed boolean NOT NULL DEFAULT false;

-- fleet-migration: 001021_internal_hashboard_metric_summaries.up.sql
-- requires-shared: 1000
-- Keep raw evidence until the scheduled bootstrap has materialized both tiers.
SELECT remove_retention_policy('hashboard_metrics', if_exists => true);
ALTER TABLE hashboard_metrics ADD COLUMN topology_version_id bigint REFERENCES container_topology_version(id);

-- Match the collection, not the health source timestamp or current placement.
-- Legacy rows without an observation keep NULL provenance and still summarize.
UPDATE hashboard_metrics b SET topology_version_id=o.version_id
FROM hashboard_installation i
JOIN container_module m ON m.id=i.module_id
JOIN container_tank t ON t.id=m.tank_id
JOIN container_topology_observation o ON o.device_id=t.device_id
WHERE b.installation_id=i.id AND b.collected_at=o.collected_at;

-- Source-time buckets preserve board history semantics. Collection bounds retain
-- the poll timing separately; parent rollups use collection-time buckets.
CREATE MATERIALIZED VIEW hashboard_metrics_hourly
WITH (timescaledb.continuous, timescaledb.materialized_only=false) AS
SELECT time_bucket(INTERVAL '1 hour',time) AS bucket,hashboard_id,installation_id,topology_version_id,
 min(collected_at) AS first_collected_at,max(collected_at) AS last_collected_at,count(*) AS observations,
 sum(hashrate_hs) FILTER (WHERE hashrate_hs > '-Infinity'::float8 AND hashrate_hs < 'Infinity'::float8) AS hashrate_sum,
 count(hashrate_hs) FILTER (WHERE hashrate_hs > '-Infinity'::float8 AND hashrate_hs < 'Infinity'::float8) AS hashrate_count,
 min(hashrate_hs) FILTER (WHERE hashrate_hs > '-Infinity'::float8 AND hashrate_hs < 'Infinity'::float8) AS hashrate_min,
 max(hashrate_hs) FILTER (WHERE hashrate_hs > '-Infinity'::float8 AND hashrate_hs < 'Infinity'::float8) AS hashrate_max,
 sum(temperature_c) FILTER (WHERE temperature_c > '-Infinity'::float8 AND temperature_c < 'Infinity'::float8) AS temperature_sum,
 count(temperature_c) FILTER (WHERE temperature_c > '-Infinity'::float8 AND temperature_c < 'Infinity'::float8) AS temperature_count,
 min(temperature_c) FILTER (WHERE temperature_c > '-Infinity'::float8 AND temperature_c < 'Infinity'::float8) AS temperature_min,
 max(temperature_c) FILTER (WHERE temperature_c > '-Infinity'::float8 AND temperature_c < 'Infinity'::float8) AS temperature_max,
 sum(power_w) FILTER (WHERE power_w > '-Infinity'::float8 AND power_w < 'Infinity'::float8) AS power_sum,
 count(power_w) FILTER (WHERE power_w > '-Infinity'::float8 AND power_w < 'Infinity'::float8) AS power_count,
 min(power_w) FILTER (WHERE power_w > '-Infinity'::float8 AND power_w < 'Infinity'::float8) AS power_min,
 max(power_w) FILTER (WHERE power_w > '-Infinity'::float8 AND power_w < 'Infinity'::float8) AS power_max,
 sum(efficiency_jh) FILTER (WHERE efficiency_jh > '-Infinity'::float8 AND efficiency_jh < 'Infinity'::float8) AS efficiency_sum,
 count(efficiency_jh) FILTER (WHERE efficiency_jh > '-Infinity'::float8 AND efficiency_jh < 'Infinity'::float8) AS efficiency_count,
 min(efficiency_jh) FILTER (WHERE efficiency_jh > '-Infinity'::float8 AND efficiency_jh < 'Infinity'::float8) AS efficiency_min,
 max(efficiency_jh) FILTER (WHERE efficiency_jh > '-Infinity'::float8 AND efficiency_jh < 'Infinity'::float8) AS efficiency_max
FROM hashboard_metrics
GROUP BY bucket,hashboard_id,installation_id,topology_version_id WITH NO DATA;
CREATE INDEX idx_hashboard_metrics_hourly_board_bucket ON hashboard_metrics_hourly(hashboard_id,bucket DESC);

-- Source-time buckets preserve board history semantics. Collection bounds retain
-- the poll timing separately; parent rollups use collection-time buckets.
CREATE MATERIALIZED VIEW hashboard_metrics_daily
WITH (timescaledb.continuous, timescaledb.materialized_only=false) AS
SELECT time_bucket(INTERVAL '1 day',time) AS bucket,hashboard_id,installation_id,topology_version_id,
 min(collected_at) AS first_collected_at,max(collected_at) AS last_collected_at,count(*) AS observations,
 sum(hashrate_hs) FILTER (WHERE hashrate_hs > '-Infinity'::float8 AND hashrate_hs < 'Infinity'::float8) AS hashrate_sum,
 count(hashrate_hs) FILTER (WHERE hashrate_hs > '-Infinity'::float8 AND hashrate_hs < 'Infinity'::float8) AS hashrate_count,
 min(hashrate_hs) FILTER (WHERE hashrate_hs > '-Infinity'::float8 AND hashrate_hs < 'Infinity'::float8) AS hashrate_min,
 max(hashrate_hs) FILTER (WHERE hashrate_hs > '-Infinity'::float8 AND hashrate_hs < 'Infinity'::float8) AS hashrate_max,
 sum(temperature_c) FILTER (WHERE temperature_c > '-Infinity'::float8 AND temperature_c < 'Infinity'::float8) AS temperature_sum,
 count(temperature_c) FILTER (WHERE temperature_c > '-Infinity'::float8 AND temperature_c < 'Infinity'::float8) AS temperature_count,
 min(temperature_c) FILTER (WHERE temperature_c > '-Infinity'::float8 AND temperature_c < 'Infinity'::float8) AS temperature_min,
 max(temperature_c) FILTER (WHERE temperature_c > '-Infinity'::float8 AND temperature_c < 'Infinity'::float8) AS temperature_max,
 sum(power_w) FILTER (WHERE power_w > '-Infinity'::float8 AND power_w < 'Infinity'::float8) AS power_sum,
 count(power_w) FILTER (WHERE power_w > '-Infinity'::float8 AND power_w < 'Infinity'::float8) AS power_count,
 min(power_w) FILTER (WHERE power_w > '-Infinity'::float8 AND power_w < 'Infinity'::float8) AS power_min,
 max(power_w) FILTER (WHERE power_w > '-Infinity'::float8 AND power_w < 'Infinity'::float8) AS power_max,
 sum(efficiency_jh) FILTER (WHERE efficiency_jh > '-Infinity'::float8 AND efficiency_jh < 'Infinity'::float8) AS efficiency_sum,
 count(efficiency_jh) FILTER (WHERE efficiency_jh > '-Infinity'::float8 AND efficiency_jh < 'Infinity'::float8) AS efficiency_count,
 min(efficiency_jh) FILTER (WHERE efficiency_jh > '-Infinity'::float8 AND efficiency_jh < 'Infinity'::float8) AS efficiency_min,
 max(efficiency_jh) FILTER (WHERE efficiency_jh > '-Infinity'::float8 AND efficiency_jh < 'Infinity'::float8) AS efficiency_max
FROM hashboard_metrics
GROUP BY bucket,hashboard_id,installation_id,topology_version_id WITH NO DATA;
CREATE INDEX idx_hashboard_metrics_daily_board_bucket ON hashboard_metrics_daily(hashboard_id,bucket DESC);

-- A typed union keeps sqlc and history consumers independent of aggregate
-- inference. Unknown legacy topology remains unknown rather than being dropped.
CREATE VIEW hashboard_metric_summaries AS
SELECT bucket::timestamptz,hashboard_id,installation_id,topology_version_id,first_collected_at::timestamptz,last_collected_at::timestamptz,observations::bigint,3600::int AS resolution_seconds,hashrate_sum::float8,hashrate_count::bigint,hashrate_min::float8,hashrate_max::float8,temperature_sum::float8,temperature_count::bigint,temperature_min::float8,temperature_max::float8,power_sum::float8,power_count::bigint,power_min::float8,power_max::float8,efficiency_sum::float8,efficiency_count::bigint,efficiency_min::float8,efficiency_max::float8 FROM hashboard_metrics_hourly
UNION ALL
SELECT bucket::timestamptz,hashboard_id,installation_id,topology_version_id,first_collected_at::timestamptz,last_collected_at::timestamptz,observations::bigint,86400::int AS resolution_seconds,hashrate_sum::float8,hashrate_count::bigint,hashrate_min::float8,hashrate_max::float8,temperature_sum::float8,temperature_count::bigint,temperature_min::float8,temperature_max::float8,power_sum::float8,power_count::bigint,power_min::float8,power_max::float8,efficiency_sum::float8,efficiency_count::bigint,efficiency_min::float8,efficiency_max::float8 FROM hashboard_metrics_daily;

CREATE PROCEDURE initialize_hashboard_metric_summaries(job_id integer,config jsonb)
LANGUAGE plpgsql AS $$
BEGIN
 CALL refresh_continuous_aggregate('hashboard_metrics_hourly',NULL,now());
 CALL refresh_continuous_aggregate('hashboard_metrics_daily',NULL,now());
 PERFORM add_continuous_aggregate_policy('hashboard_metrics_hourly',start_offset=>INTERVAL '1 day',end_offset=>INTERVAL '1 hour',schedule_interval=>INTERVAL '30 minutes',if_not_exists=>true);
 PERFORM add_retention_policy('hashboard_metrics_hourly',INTERVAL '3 months',if_not_exists=>true);
 PERFORM add_continuous_aggregate_policy('hashboard_metrics_daily',start_offset=>INTERVAL '7 days',end_offset=>INTERVAL '1 day',schedule_interval=>INTERVAL '6 hours',if_not_exists=>true);
 PERFORM add_retention_policy('hashboard_metrics_daily',INTERVAL '3 years',if_not_exists=>true);
 PERFORM add_retention_policy('hashboard_metrics',INTERVAL '10 days',if_not_exists=>true);
 PERFORM delete_job(job_id);
END;
$$;
SELECT add_job('initialize_hashboard_metric_summaries',INTERVAL '1 hour',initial_start=>now()+INTERVAL '1 minute');

-- fleet-migration: 001022_internal_topology_compaction.up.sql
-- requires-shared: 1000
ALTER TABLE container_topology_observation ADD COLUMN last_confirmed_at timestamptz,
 ADD CONSTRAINT ck_container_topology_observation_confirmation CHECK (last_confirmed_at>=collected_at);

-- Index FK checks on retained raw and latest-value references.
CREATE INDEX idx_device_metrics_topology_observation ON device_metrics(topology_observation_id) WHERE topology_observation_id IS NOT NULL;
CREATE INDEX idx_container_tank_metrics_topology_observation ON container_tank_metrics(topology_observation_id);
CREATE INDEX idx_container_tank_latest_metrics_topology_observation ON container_tank_latest_metrics(topology_observation_id);
CREATE INDEX idx_container_latest_metrics_topology_observation ON container_latest_metrics(topology_observation_id);

CREATE TABLE container_topology_compaction_state (
 device_id bigint PRIMARY KEY REFERENCES device(id),
 scanned_through timestamptz,
 anchor_id bigint REFERENCES container_topology_observation(id),
 next_run_at timestamptz NOT NULL DEFAULT now(),
 last_run_at timestamptz,
 last_duration_ms double precision,
 examined bigint NOT NULL DEFAULT 0,
 deleted bigint NOT NULL DEFAULT 0,
 failures bigint NOT NULL DEFAULT 0,
 last_error text
);
CREATE INDEX idx_container_topology_compaction_state_due ON container_topology_compaction_state(next_run_at,device_id);
CREATE TABLE container_topology_compaction_pending (
 observation_id bigint PRIMARY KEY REFERENCES container_topology_observation(id) ON DELETE CASCADE,
 device_id bigint NOT NULL REFERENCES device(id),
 next_check_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX idx_container_topology_compaction_pending_due ON container_topology_compaction_pending(device_id,next_check_at,observation_id);
INSERT INTO container_topology_compaction_state(device_id) SELECT DISTINCT device_id FROM container_topology_observation;

-- Register once, including newly discovered controllers. No per-poll progress
-- update: collection never waits for a cleanup cursor on an existing device.
CREATE FUNCTION register_container_topology_compaction() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 INSERT INTO container_topology_compaction_state(device_id) VALUES (NEW.device_id) ON CONFLICT DO NOTHING;
 RETURN NEW;
END;
$$;
CREATE TRIGGER register_container_topology_compaction AFTER INSERT ON container_topology_observation
FOR EACH ROW EXECUTE FUNCTION register_container_topology_compaction();

CREATE FUNCTION container_topology_references(ids bigint[])
RETURNS TABLE(observation_id bigint,reference_kind text) LANGUAGE sql STABLE AS $$
 -- Ingestion stamps device_metrics only at the observation's collection time
 -- (StampContainerSample). Constrain time and device for chunk/segment pruning.
 WITH observations AS MATERIALIZED (
  SELECT o.id,o.collected_at,d.device_identifier FROM container_topology_observation o
  JOIN device d ON d.id=o.device_id WHERE o.id=ANY(ids)
 )
 SELECT DISTINCT m.topology_observation_id,'raw'::text FROM observations o
 JOIN LATERAL (SELECT topology_observation_id FROM device_metrics m
  WHERE m.time=o.collected_at AND m.device_identifier=o.device_identifier
   AND m.topology_observation_id=o.id LIMIT 1) m ON true
 UNION
 SELECT DISTINCT topology_observation_id,'raw'::text FROM container_tank_metrics WHERE topology_observation_id=ANY(ids)
 UNION
 SELECT DISTINCT topology_observation_id,'latest'::text FROM container_tank_latest_metrics WHERE topology_observation_id=ANY(ids)
 UNION
 SELECT DISTINCT topology_observation_id,'latest'::text FROM container_latest_metrics WHERE topology_observation_id=ANY(ids);
$$;

-- Read-only bounded classifier, shared by the candidate report and cleanup.
-- Policy v1 is fixed at 120 seconds: default scheduler eligibility is 15 seconds
-- plus its five-second tick and collection duration. Several missed polls are
-- tolerated, but longer outages remain explicit. This is not metric freshness.
-- Changing this policy cannot reconstruct timestamps already compacted away.
CREATE FUNCTION container_topology_candidates(p_device bigint,p_cutoff timestamptz,p_limit integer)
RETURNS TABLE(observation_id bigint,anchor_id bigint,collected_at timestamptz,confirmed_at timestamptz,is_anchor boolean)
LANGUAGE plpgsql STABLE AS $$
DECLARE
 progress container_topology_compaction_state%ROWTYPE;
 anchor container_topology_observation%ROWTYPE;
 observation container_topology_observation%ROWTYPE;
BEGIN
 IF p_limit IS NULL OR p_cutoff IS NULL OR p_device IS NULL OR p_limit<1 OR p_limit>10000 OR p_cutoff>now()-INTERVAL '10 days' THEN
  RAISE EXCEPTION 'cleanup requires a limit of 1..10000 and a cutoff at least ten days old';
 END IF;
 SELECT * INTO progress FROM container_topology_compaction_state WHERE device_id=p_device;
 SELECT * INTO anchor FROM container_topology_observation WHERE id=progress.anchor_id;
 FOR observation IN
  SELECT o.* FROM container_topology_observation o WHERE o.device_id=p_device
   AND o.collected_at>COALESCE(progress.scanned_through,'-infinity'::timestamptz)
   AND o.collected_at<p_cutoff ORDER BY o.collected_at LIMIT p_limit
 LOOP
  is_anchor := anchor.id IS NULL OR observation.version_id<>anchor.version_id
   OR observation.complete<>anchor.complete
   OR observation.collected_at>COALESCE(anchor.last_confirmed_at,anchor.collected_at)+INTERVAL '120 seconds';
  IF is_anchor THEN
   anchor := observation;
  ELSE
   anchor.last_confirmed_at := GREATEST(COALESCE(anchor.last_confirmed_at,anchor.collected_at),COALESCE(observation.last_confirmed_at,observation.collected_at));
  END IF;
  observation_id := observation.id;
  anchor_id := anchor.id;
  collected_at := observation.collected_at;
  confirmed_at := COALESCE(anchor.last_confirmed_at,anchor.collected_at);
  RETURN NEXT;
 END LOOP;
END;
$$;

CREATE FUNCTION compact_container_topology(p_device bigint,p_cutoff timestamptz,p_limit integer)
RETURNS jsonb LANGUAGE plpgsql AS $$
DECLARE
 candidate record;
 pending record;
 final_scanned_through timestamptz;
 final_anchor_id bigint;
 examined_count integer := 0;
 deleted_count integer := 0;
 pinned_count integer := 0;
 started timestamptz := clock_timestamp();
BEGIN
 IF p_limit IS NULL OR p_cutoff IS NULL OR p_device IS NULL OR p_limit<1 OR p_limit>10000 OR p_cutoff>now()-INTERVAL '10 days' THEN
  RAISE EXCEPTION 'cleanup requires a device, a limit of 1..10000 and a cutoff at least ten days old';
 END IF;
 -- Match ingestion's organization lock before touching cleanup progress. Never
 -- queue behind live ingestion. The scheduler retries this device later.
 PERFORM o.id FROM organization o JOIN device d ON d.org_id=o.id WHERE d.id=p_device FOR NO KEY UPDATE OF o NOWAIT;
 SELECT s.scanned_through,s.anchor_id INTO final_scanned_through,final_anchor_id
 FROM container_topology_compaction_state s WHERE s.device_id=p_device FOR UPDATE NOWAIT;
 IF NOT FOUND THEN RETURN jsonb_build_object('examined',0,'deleted',0,'pinned',0); END IF;
 -- Retry pinned receipts independently of forward progress; bounded work even
 -- when a latest-value reference pins one forever. The represented spans already
 -- include these observations, so deleting them does not alter any anchor.
 FOR pending IN
  WITH due AS MATERIALIZED (
   SELECT p.observation_id FROM container_topology_compaction_pending p
   WHERE p.device_id=p_device AND p.next_check_at<=now() ORDER BY p.next_check_at,p.observation_id LIMIT p_limit
  ), refs AS MATERIALIZED (
   SELECT * FROM container_topology_references(ARRAY(SELECT d.observation_id FROM due d))
  )
  SELECT d.observation_id,EXISTS(SELECT 1 FROM refs r WHERE r.observation_id=d.observation_id) AS pinned FROM due d
 LOOP
  IF pending.pinned THEN
   UPDATE container_topology_compaction_pending SET next_check_at=now()+INTERVAL '1 hour' WHERE observation_id=pending.observation_id;
   pinned_count := pinned_count+1;
  ELSE
   DELETE FROM container_topology_observation WHERE id=pending.observation_id;
   deleted_count := deleted_count+1;
  END IF;
 END LOOP;
 FOR candidate IN
  WITH candidates AS MATERIALIZED (SELECT * FROM container_topology_candidates(p_device,p_cutoff,p_limit)),
  refs AS MATERIALIZED (SELECT * FROM container_topology_references(ARRAY(SELECT c.observation_id FROM candidates c WHERE NOT c.is_anchor)))
  SELECT c.*,EXISTS(SELECT 1 FROM refs r WHERE r.observation_id=c.observation_id) AS pinned FROM candidates c ORDER BY c.collected_at
 LOOP
  examined_count := examined_count+1;
  IF NOT candidate.is_anchor THEN
   UPDATE container_topology_observation SET last_confirmed_at=candidate.confirmed_at WHERE id=candidate.anchor_id;
   IF candidate.pinned THEN
    INSERT INTO container_topology_compaction_pending(observation_id,device_id,next_check_at)
     VALUES(candidate.observation_id,p_device,now()+INTERVAL '1 hour') ON CONFLICT DO NOTHING;
    pinned_count := pinned_count+1;
   ELSE
    DELETE FROM container_topology_observation WHERE id=candidate.observation_id;
    deleted_count := deleted_count+1;
   END IF;
  END IF;
  final_scanned_through := candidate.collected_at;
  final_anchor_id := candidate.anchor_id;
 END LOOP;
 -- A retry backlog also needs another bounded turn, even after forward scanning
 -- catches up. Pins deferred into the future must not cause immediate spinning.
 UPDATE container_topology_compaction_state SET scanned_through=final_scanned_through,
  anchor_id=final_anchor_id,next_run_at=now()+CASE WHEN examined_count=p_limit OR EXISTS (
  SELECT 1 FROM container_topology_compaction_pending p WHERE p.device_id=p_device AND p.next_check_at<=now()
 ) THEN INTERVAL '0' ELSE INTERVAL '1 hour' END,
  last_run_at=now(),last_duration_ms=extract(epoch FROM clock_timestamp()-started)*1000,
  examined=examined+examined_count,deleted=deleted+deleted_count,last_error=NULL WHERE device_id=p_device;
 RETURN jsonb_build_object('examined',examined_count,'deleted',deleted_count,'pinned',pinned_count);
END;
$$;

-- One device per job transaction bounds organization-lock duration. Fair due-time
-- ordering prevents a busy controller or an indefinitely pinned receipt starving
-- other controllers. Timescale's max_runtime bounds the whole transaction.
CREATE PROCEDURE compact_container_topology_job(job_id integer,config jsonb)
LANGUAGE plpgsql AS $$
DECLARE
 target bigint;
 cutoff timestamptz := now()-INTERVAL '10 days';
 batch_size integer := COALESCE((config->>'batch_size')::integer,500);
BEGIN
 IF NOT COALESCE((config->>'enabled')::boolean,false) THEN RETURN; END IF;
 SELECT device_id INTO target FROM container_topology_compaction_state WHERE next_run_at<=now()
  ORDER BY next_run_at,device_id LIMIT 1 FOR UPDATE SKIP LOCKED;
 IF target IS NULL THEN RETURN; END IF;
 -- Persist only a scheduling lease before beginning destructive work. A worker
 -- timeout or termination rolls back the entire batch but not this backoff,
 -- so a slow device cannot monopolize subsequent runs. CALL outside a transaction.
 UPDATE container_topology_compaction_state SET next_run_at=now()+INTERVAL '1 minute' WHERE device_id=target;
 COMMIT;
 BEGIN
  PERFORM compact_container_topology(target,cutoff,batch_size);
 EXCEPTION WHEN lock_not_available THEN
  -- Live ingestion owns the lock: this is expected backpressure, not a failure.
  UPDATE container_topology_compaction_state SET next_run_at=now()+INTERVAL '1 minute' WHERE device_id=target;
 WHEN OTHERS THEN
  -- The failed subtransaction releases all its locks and mutations.
  UPDATE container_topology_compaction_state SET next_run_at=now()+INTERVAL '1 minute',failures=failures+1,last_error=SQLERRM WHERE device_id=target;
 END;
END;
$$;
SELECT add_job('compact_container_topology_job',INTERVAL '10 seconds',config=>' {"enabled":false,"batch_size":500}'::jsonb);
SELECT alter_job(job_id,max_runtime=>INTERVAL '2 seconds',retry_period=>INTERVAL '1 minute')
FROM timescaledb_information.jobs WHERE proc_name='compact_container_topology_job';

-- fleet-migration: 001023_internal_hashboard_observation.up.sql
-- requires-shared: 1000
-- Controller/site attribution and source ordering survive loss of physical placement.
CREATE TABLE hashboard_observation (
    hashboard_id BIGINT PRIMARY KEY REFERENCES hashboard(id),
    device_id BIGINT NOT NULL REFERENCES device(id),
    site_id BIGINT,
    last_seen_at TIMESTAMPTZ NOT NULL,
    source_at TIMESTAMPTZ,
    -- NULL allows asynchronous readings on initial discovery only.
    attribution_since TIMESTAMPTZ,
    released_at TIMESTAMPTZ
);

-- Seed existing hardware from its latest installation without scanning metric history.
INSERT INTO hashboard_observation(hashboard_id, device_id, site_id, last_seen_at, source_at, attribution_since, released_at)
SELECT i.hashboard_id, t.device_id, i.site_id, GREATEST(i.last_seen_at, i.removed_at), h.time,
    CASE WHEN EXISTS (SELECT 1 FROM hashboard_installation prior
        WHERE prior.hashboard_id=i.hashboard_id AND (prior.installed_at, prior.id)<(i.installed_at, i.id))
        THEN i.installed_at END,
    i.removed_at
FROM (SELECT DISTINCT ON (hashboard_id) * FROM hashboard_installation
      ORDER BY hashboard_id, installed_at DESC, id DESC) i
JOIN container_module m ON m.id=i.module_id
JOIN container_tank t ON t.id=m.tank_id
LEFT JOIN hashboard_latest_metrics h ON h.hashboard_id=i.hashboard_id;

-- fleet-migration: 001024_internal_device_reboot_required_after.up.sql
-- requires-shared: 1000
ALTER TABLE device_status ADD COLUMN reboot_required_after TIMESTAMPTZ;

COMMENT ON COLUMN device_status.reboot_required_after IS
    'Earliest time a boot must follow to satisfy the pending firmware installation; NULL falls back to status_timestamp.';

-- fleet-migration: 001025_internal_container_site_scope.up.sql
-- requires-shared: 1000
ALTER TABLE container_inventory ADD COLUMN org_id BIGINT;
UPDATE container_inventory ci SET org_id=d.org_id FROM device d WHERE d.id=ci.device_id;
ALTER TABLE container_inventory ALTER COLUMN org_id SET NOT NULL;
ALTER TABLE container_inventory ADD CONSTRAINT fk_container_inventory_device_org
    FOREIGN KEY (device_id, org_id) REFERENCES device(id, org_id);

-- All CT1 children inherit their current placement from this root. Site is
-- deliberately not copied into inventory; historical observations keep theirs.
CREATE VIEW container_scope AS
SELECT d.id, d.org_id, d.site_id, d.device_identifier, d.discovered_device_id, d.deleted_at
FROM container_inventory ci
JOIN device d ON d.id=ci.device_id AND d.org_id=ci.org_id;

-- Refuse legacy invalid placement rather than silently changing site/layout.
DO $$
DECLARE
    offenders TEXT;
BEGIN
    SELECT string_agg(format('device %s (%s), org %s, building %s, racks [%s]',
        d.id, d.device_identifier, d.org_id, COALESCE(d.building_id::text, 'none'),
        COALESCE((SELECT string_agg(m.device_set_id::text, ', ' ORDER BY m.device_set_id)
          FROM device_set_membership m WHERE m.device_id=d.id AND m.device_set_type='rack'), '')),
        E'\n' ORDER BY d.id)
    INTO offenders
    FROM device d JOIN discovered_device dd ON dd.id=d.discovered_device_id
    WHERE dd.device_class=2 AND d.deleted_at IS NULL AND dd.deleted_at IS NULL
      AND (d.building_id IS NOT NULL OR EXISTS
        (SELECT 1 FROM device_set_membership m WHERE m.device_id=d.id AND m.device_set_type='rack'));
    IF offenders IS NOT NULL THEN
        RAISE EXCEPTION 'CT1 containers must be removed from buildings and racks before this migration:%', E'\n' || offenders
          USING ERRCODE='23514',
            HINT='See docs/development/ct1-site-placement.md (CSV and migration) for preflight and placement remediation; preserve device.site_id.';
    END IF;
END $$;

CREATE FUNCTION enforce_container_device_placement() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.building_id IS NOT NULL OR EXISTS
       (SELECT 1 FROM device_set_membership WHERE device_id=NEW.id AND device_set_type='rack') THEN
        -- Discovery/classification writes already hold the discovery row before
        -- taking a device lock. Never wait in the inverse order: the placement
        -- transaction retries instead, with a fresh view of the committed class.
        PERFORM 1 FROM discovered_device WHERE id=NEW.discovered_device_id FOR SHARE NOWAIT;
        IF EXISTS (SELECT 1 FROM discovered_device WHERE id=NEW.discovered_device_id AND device_class=2) THEN
            RAISE EXCEPTION 'containers cannot be placed in a building or rack' USING ERRCODE='23514';
        END IF;
    END IF;
    RETURN NEW;
EXCEPTION WHEN lock_not_available THEN
    RAISE EXCEPTION 'device classification changed during placement' USING ERRCODE='40001',
      HINT='Retry the complete placement transaction.';
END $$;
CREATE TRIGGER container_device_placement BEFORE INSERT OR UPDATE OF building_id, discovered_device_id ON device
FOR EACH ROW EXECUTE FUNCTION enforce_container_device_placement();

CREATE FUNCTION enforce_container_rack_placement() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.device_set_type='rack' THEN
        -- Membership foreign keys hold KEY SHARE on the device. NO KEY UPDATE
        -- serializes placement without upgrading those FK locks to FOR UPDATE.
        PERFORM 1 FROM device WHERE id=NEW.device_id FOR NO KEY UPDATE;
        PERFORM 1 FROM discovered_device dd JOIN device d ON d.discovered_device_id=dd.id
          WHERE d.id=NEW.device_id FOR SHARE OF dd NOWAIT;
        IF EXISTS (SELECT 1 FROM device d JOIN discovered_device dd ON dd.id=d.discovered_device_id
                   WHERE d.id=NEW.device_id AND dd.device_class=2) THEN
            RAISE EXCEPTION 'containers cannot be placed in a rack' USING ERRCODE='23514';
        END IF;
    END IF;
    RETURN NEW;
EXCEPTION WHEN lock_not_available THEN
    RAISE EXCEPTION 'device classification changed during placement' USING ERRCODE='40001',
      HINT='Retry the complete placement transaction.';
END $$;
CREATE TRIGGER container_rack_placement BEFORE INSERT OR UPDATE ON device_set_membership
FOR EACH ROW EXECUTE FUNCTION enforce_container_rack_placement();

CREATE FUNCTION enforce_container_class_placement() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.device_class=2 AND OLD.device_class IS DISTINCT FROM NEW.device_class THEN
        PERFORM 1 FROM device WHERE discovered_device_id=NEW.id ORDER BY id FOR NO KEY UPDATE;
        IF EXISTS (SELECT 1 FROM device d WHERE d.discovered_device_id=NEW.id AND
                   (d.building_id IS NOT NULL OR EXISTS (SELECT 1 FROM device_set_membership m
                     WHERE m.device_id=d.id AND m.device_set_type='rack'))) THEN
            RAISE EXCEPTION 'containers cannot be placed in a building or rack' USING ERRCODE='23514';
        END IF;
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER container_class_placement BEFORE UPDATE OF device_class ON discovered_device
FOR EACH ROW EXECUTE FUNCTION enforce_container_class_placement();

-- fleet-migration: 001026_internal_fleet_node_details.up.sql
-- requires-shared: 1000

ALTER TABLE fleet_node_heartbeat
    ADD COLUMN ip_addresses text[] NOT NULL DEFAULT '{}',
    ADD COLUMN version text NOT NULL DEFAULT '';

ALTER TABLE pending_enrollment
    ADD COLUMN confirmed_by bigint CONSTRAINT fk_pending_enrollment_confirmed_by REFERENCES "user"(id);

CREATE INDEX idx_pending_enrollment_confirmed_node
    ON pending_enrollment (fleet_node_id, org_id, consumed_at DESC, id DESC)
    WHERE status = 'CONFIRMED';

-- fleet-migration: 001027_internal_container_telemetry_compression.up.sql
-- requires-shared: 1000
-- Match device_metrics: equipment segments, newest samples first, and hourly
-- compression of chunks whose entire time range is at least six hours old.
-- Only new chunks adopt the one-hour interval. Existing day/week chunks remain
-- eligible once their full range ages out; do not rewrite history in migration.

-- UPDATE ... FROM a recordset can join after Timescale has decompressed every
-- board in a chunk. Group inside the database to keep one client round trip,
-- with explicit segment and exact source-time parameters on each UPDATE.
CREATE FUNCTION stamp_hashboard_topology(p_version bigint,p_collected_at timestamptz,p_samples jsonb)
RETURNS void LANGUAGE plpgsql SET plan_cache_mode=force_custom_plan AS $$
DECLARE
 sample record;
BEGIN
 FOR sample IN
  SELECT x.time,array_agg(DISTINCT x.hashboard_id) AS board_ids
  FROM jsonb_to_recordset(p_samples) x(hashboard_id bigint,time timestamptz)
  GROUP BY x.time ORDER BY x.time
 LOOP
  -- Timescale's DML decompression cannot prune a generic-plan ANY(array)
  -- parameter. Plan with this group's actual board IDs and timestamp even if
  -- the outer prepared SELECT uses a generic plan. The setting is call-local.
  UPDATE hashboard_metrics SET topology_version_id=p_version
  WHERE hashboard_id=ANY(sample.board_ids) AND time=sample.time
   AND collected_at=p_collected_at;
 END LOOP;
END;
$$;

CREATE OR REPLACE FUNCTION container_topology_references(ids bigint[])
RETURNS TABLE(observation_id bigint,reference_kind text) LANGUAGE sql STABLE AS $$
 -- Ingestion stamps device_metrics only at the observation's collection time
 -- (StampContainerSample). Constrain time and device for chunk/segment pruning.
 WITH observations AS MATERIALIZED (
  SELECT o.id,o.device_id,o.collected_at,d.device_identifier FROM container_topology_observation o
  JOIN device d ON d.id=o.device_id WHERE o.id=ANY(ids)
 )
 SELECT DISTINCT m.topology_observation_id,'raw'::text FROM observations o
 JOIN LATERAL (SELECT topology_observation_id FROM device_metrics m
  WHERE m.time=o.collected_at AND m.device_identifier=o.device_identifier
   AND m.topology_observation_id=o.id LIMIT 1) m ON true
 UNION
 -- Tank rollups use the same collection time as their observation. Include
 -- historical tanks too: retired equipment can still have retained telemetry.
 SELECT DISTINCT m.topology_observation_id,'raw'::text FROM observations o
 JOIN container_tank t ON t.device_id=o.device_id
 JOIN LATERAL (SELECT topology_observation_id FROM container_tank_metrics m
  WHERE m.time=o.collected_at AND m.tank_id=t.id
   AND m.topology_observation_id=o.id LIMIT 1) m ON true
 UNION
 SELECT DISTINCT topology_observation_id,'latest'::text FROM container_tank_latest_metrics WHERE topology_observation_id=ANY(ids)
 UNION
 SELECT DISTINCT topology_observation_id,'latest'::text FROM container_latest_metrics WHERE topology_observation_id=ANY(ids);
$$;

SELECT set_chunk_time_interval('container_plant_metrics', INTERVAL '1 hour');
ALTER TABLE container_plant_metrics SET (
 timescaledb.compress,
 timescaledb.compress_segmentby = 'device_id',
 timescaledb.compress_orderby = 'time DESC'
);
SELECT add_compression_policy('container_plant_metrics', INTERVAL '6 hours', schedule_interval => INTERVAL '1 hour');

-- RI checks run independently of container_topology_references(). Include the
-- collection time in the FK so deleting an expired receipt can prune retained
-- chunks instead of probing all compressed tank history for its observation ID.
-- Add constraints before enabling compression; existing rows must satisfy the
-- same collection-time invariant already used by ingestion and compaction.
ALTER TABLE container_topology_observation
 ADD CONSTRAINT uq_container_topology_observation_id_collected_at UNIQUE(id,collected_at);
ALTER TABLE container_tank_metrics
 DROP CONSTRAINT container_tank_metrics_topology_observation_id_fkey,
 ADD CONSTRAINT fk_container_tank_metrics_observation_time
 FOREIGN KEY(topology_observation_id,time) REFERENCES container_topology_observation(id,collected_at);

SELECT set_chunk_time_interval('container_tank_metrics', INTERVAL '1 hour');
ALTER TABLE container_tank_metrics SET (
 timescaledb.compress,
 timescaledb.compress_segmentby = 'tank_id',
 timescaledb.compress_orderby = 'time DESC'
);
SELECT add_compression_policy('container_tank_metrics', INTERVAL '6 hours', schedule_interval => INTERVAL '1 hour');

SELECT set_chunk_time_interval('container_module_metrics', INTERVAL '1 hour');
ALTER TABLE container_module_metrics SET (
 timescaledb.compress,
 timescaledb.compress_segmentby = 'module_id',
 timescaledb.compress_orderby = 'time DESC'
);
SELECT add_compression_policy('container_module_metrics', INTERVAL '6 hours', schedule_interval => INTERVAL '1 hour');

SELECT set_chunk_time_interval('hashboard_metrics', INTERVAL '1 hour');
ALTER TABLE hashboard_metrics SET (
 timescaledb.compress,
 timescaledb.compress_segmentby = 'hashboard_id',
 timescaledb.compress_orderby = 'time DESC'
);
SELECT add_compression_policy('hashboard_metrics', INTERVAL '6 hours', schedule_interval => INTERVAL '1 hour');

-- Chunk-level FK trigger overhead still grows with retained chunk count even
-- with time pruning. Bound each of the new-candidate and pending-retry queues
-- to 50 per scheduled transaction so both can progress under max_runtime=2s.
-- Keep the existing enabled flag, schedule, lease, and manual function limits.
CREATE OR REPLACE PROCEDURE compact_container_topology_job(job_id integer,config jsonb)
LANGUAGE plpgsql AS $$
DECLARE
 target bigint;
 cutoff timestamptz := now()-INTERVAL '10 days';
 batch_size integer := LEAST(COALESCE((config->>'batch_size')::integer,50),50);
BEGIN
 IF NOT COALESCE((config->>'enabled')::boolean,false) THEN RETURN; END IF;
 SELECT device_id INTO target FROM container_topology_compaction_state WHERE next_run_at<=now()
  ORDER BY next_run_at,device_id LIMIT 1 FOR UPDATE SKIP LOCKED;
 IF target IS NULL THEN RETURN; END IF;
 -- Persist only a scheduling lease before beginning destructive work. A worker
 -- timeout or termination rolls back the entire batch but not this backoff,
 -- so a slow device cannot monopolize subsequent runs. CALL outside a transaction.
 UPDATE container_topology_compaction_state SET next_run_at=now()+INTERVAL '1 minute' WHERE device_id=target;
 COMMIT;
 BEGIN
  PERFORM compact_container_topology(target,cutoff,batch_size);
 EXCEPTION WHEN lock_not_available THEN
  -- Live ingestion owns the lock: this is expected backpressure, not a failure.
  UPDATE container_topology_compaction_state SET next_run_at=now()+INTERVAL '1 minute' WHERE device_id=target;
 WHEN OTHERS THEN
  -- The failed subtransaction releases all its locks and mutations.
  UPDATE container_topology_compaction_state SET next_run_at=now()+INTERVAL '1 minute',failures=failures+1,last_error=SQLERRM WHERE device_id=target;
 END;
END;
$$;

-- fleet-migration: 001028_internal_hosted_account_assets.up.sql
-- requires-shared: 1000
-- Reporting-only ownership; recalled rows remain to prevent ABA updates.
CREATE TABLE hosted_account_asset (
 organization_id BIGINT NOT NULL REFERENCES organization(id),
 kind INTEGER NOT NULL CHECK (kind IN (5,6)),
 asset_id BIGINT NOT NULL CHECK (asset_id > 0),
 device_id BIGINT NOT NULL,
 miner_id BIGINT REFERENCES device(id),
 module_id BIGINT REFERENCES container_module(id),
 client_id BIGINT,
 subclient_id BIGINT,
 revision BIGINT NOT NULL DEFAULT 1 CHECK (revision > 0),
 updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
 PRIMARY KEY (organization_id,kind,asset_id),
 FOREIGN KEY (device_id,organization_id) REFERENCES device(id,org_id),
 FOREIGN KEY (organization_id,client_id) REFERENCES hosted_account(organization_id,id),
 FOREIGN KEY (organization_id,subclient_id) REFERENCES hosted_account(organization_id,id),
 CHECK ((kind=5 AND miner_id IS NOT NULL AND miner_id=asset_id AND module_id IS NULL AND device_id=asset_id) OR
        (kind=6 AND module_id IS NOT NULL AND module_id=asset_id AND miner_id IS NULL)),
 CHECK (subclient_id IS NULL OR client_id IS NOT NULL)
);
CREATE INDEX idx_hosted_account_asset_client ON hosted_account_asset(organization_id,client_id);
CREATE INDEX idx_hosted_account_asset_subclient ON hosted_account_asset(organization_id,subclient_id);
CREATE FUNCTION check_hosted_account_asset() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.kind=6 AND NOT EXISTS (SELECT 1 FROM container_module m JOIN container_tank t ON t.id=m.tank_id WHERE m.id=NEW.module_id AND t.device_id=NEW.device_id) THEN
  RAISE EXCEPTION 'module controller mismatch' USING ERRCODE='23514';
 END IF;
 IF NEW.client_id IS NOT NULL AND NOT EXISTS (SELECT 1 FROM hosted_account a WHERE a.id=NEW.client_id AND a.organization_id=NEW.organization_id AND a.depth=1) THEN
  RAISE EXCEPTION 'allocation requires customer' USING ERRCODE='23514';
 END IF;
 IF NEW.subclient_id IS NOT NULL AND NOT EXISTS (SELECT 1 FROM hosted_account a WHERE a.id=NEW.subclient_id AND a.organization_id=NEW.organization_id AND a.parent_id=NEW.client_id AND a.depth=2) THEN
  RAISE EXCEPTION 'allocation requires direct subclient' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END;
$$;
CREATE TRIGGER hosted_account_asset_chain BEFORE INSERT OR UPDATE ON hosted_account_asset FOR EACH ROW EXECUTE FUNCTION check_hosted_account_asset();

-- fleet-migration: 001029_internal_bound_topology_reference_checks.up.sql
-- requires-shared: 1000
-- Check a controller's tank IDs together. A lateral lookup per tank repeats
-- Timescale chunk pruning for every receipt/tank pair, exhausting the cleanup
-- job's two-second deadline as retained hourly chunks accumulate. One lookup
-- per receipt preserves historical-tank references and the existing batch cap.
CREATE OR REPLACE FUNCTION container_topology_references(ids bigint[])
RETURNS TABLE(observation_id bigint,reference_kind text) LANGUAGE sql STABLE AS $$
 WITH observations AS MATERIALIZED (
  SELECT o.id,o.device_id,o.collected_at,d.device_identifier FROM container_topology_observation o
  JOIN device d ON d.id=o.device_id WHERE o.id=ANY(ids)
 )
 SELECT DISTINCT m.topology_observation_id,'raw'::text FROM observations o
 JOIN LATERAL (SELECT topology_observation_id FROM device_metrics m
  WHERE m.time=o.collected_at AND m.device_identifier=o.device_identifier
   AND m.topology_observation_id=o.id LIMIT 1) m ON true
 UNION
 SELECT DISTINCT m.topology_observation_id,'raw'::text FROM observations o
 JOIN LATERAL (SELECT topology_observation_id FROM container_tank_metrics m
  WHERE m.time=o.collected_at AND m.topology_observation_id=o.id
   AND m.tank_id=ANY(ARRAY(SELECT t.id FROM container_tank t WHERE t.device_id=o.device_id))
  LIMIT 1) m ON true
 UNION
 SELECT DISTINCT topology_observation_id,'latest'::text FROM container_tank_latest_metrics WHERE topology_observation_id=ANY(ids)
 UNION
 SELECT DISTINCT topology_observation_id,'latest'::text FROM container_latest_metrics WHERE topology_observation_id=ANY(ids);
$$;

-- fleet-migration: 001030_internal_telemetry_summary_recovery.up.sql
-- requires-shared: 1000
-- Replace independent refresh/expiration with restartable coverage and a shared
-- raw-source fence. Stop writers and database maintenance during this upgrade.
SELECT delete_job(job_id) FROM timescaledb_information.jobs
WHERE proc_name IN ('initialize_container_metric_summaries','initialize_hashboard_metric_summaries');
DROP PROCEDURE initialize_container_metric_summaries(integer,jsonb);
DROP PROCEDURE initialize_hashboard_metric_summaries(integer,jsonb);

SELECT remove_continuous_aggregate_policy(aggregate_name::regclass,if_exists=>true)
FROM unnest(ARRAY[
 'device_metrics_hourly','device_metrics_daily','device_status_hourly','device_status_daily',
 'container_metrics_hourly','container_metrics_daily','container_tank_metrics_hourly','container_tank_metrics_daily',
 'container_module_metrics_hourly','container_module_metrics_daily','hashboard_metrics_hourly','hashboard_metrics_daily'
]) aggregate_name;

CREATE TABLE telemetry_summary_source (
 source_name text PRIMARY KEY,
 raw_floor timestamptz,
 dropped_through timestamptz,
 raw_retention interval NOT NULL DEFAULT INTERVAL '240 hours',
 job_id integer CONSTRAINT uq_telemetry_summary_source_job UNIQUE,
 next_retention_at timestamptz NOT NULL DEFAULT now(),
 last_attempt_at timestamptz,
 last_retention_at timestamptz,
 retention_error text,
 retention_deferrals bigint NOT NULL DEFAULT 0
);
INSERT INTO telemetry_summary_source(source_name)
VALUES ('device_metrics'),('container_tank_metrics'),('container_module_metrics'),('hashboard_metrics');

-- An old chunk can have been recreated by a late observation after expiration.
-- Capture the old policy BEFORE removing it, and trust only its retained window,
-- capped by the repository's shortest prior raw target. Missing policy/history
-- (including an unfinished bootstrap) cannot prove older raw complete either.
-- Empty sources still start at this UTC day so their first live day is covered.
-- The extension catalog is unavailable to sqlc's offline schema parser.
DO $$ BEGIN
 UPDATE telemetry_summary_source s SET raw_floor=f.initial_floor,dropped_through=f.initial_floor
 FROM (
  SELECT s.source_name,CASE WHEN min(c.range_start) IS NULL THEN time_bucket(INTERVAL '24 hours',now())
   ELSE GREATEST(min(c.range_start),now()-LEAST(s.raw_retention,
    COALESCE(min(CASE WHEN j.job_id IS NOT NULL THEN
     COALESCE((j.config->>'drop_after')::interval,INTERVAL '0') END),s.raw_retention))) END AS initial_floor
  FROM telemetry_summary_source s LEFT JOIN timescaledb_information.chunks c
   ON c.hypertable_schema='public' AND c.hypertable_name=s.source_name
  LEFT JOIN timescaledb_information.jobs j ON j.hypertable_schema='public'
   AND j.hypertable_name=s.source_name AND j.proc_name='policy_retention'
  GROUP BY s.source_name,s.raw_retention
 ) f WHERE s.source_name=f.source_name;
END $$;
SELECT remove_retention_policy(source_name::regclass,if_exists=>true)
FROM telemetry_summary_source;

-- Writers hold a shared row lock through their transaction. Before certifying
-- a range, maintenance publishes its write fence and commits, waiting for all
-- earlier writers. Keep this row separate from retention/job metadata: holding
-- it while acquiring raw-table DDL locks would invert the writer's lock order.
CREATE TABLE telemetry_summary_write_gate (
 source_name text PRIMARY KEY REFERENCES telemetry_summary_source(source_name),
 frozen_through timestamptz
);
INSERT INTO telemetry_summary_write_gate(source_name) SELECT source_name FROM telemetry_summary_source;

CREATE FUNCTION telemetry_bucket_ceiling(width interval,at_time timestamptz) RETURNS timestamptz
LANGUAGE sql IMMUTABLE STRICT AS $$
 SELECT boundary+CASE WHEN boundary<at_time THEN width ELSE INTERVAL '0' END
 FROM (SELECT time_bucket(width,at_time) AS boundary) bucket;
$$;

CREATE TABLE telemetry_summary_progress (
 aggregate_name text PRIMARY KEY,
 source_name text NOT NULL REFERENCES telemetry_summary_source(source_name),
 bucket_width interval NOT NULL,
 lateness interval NOT NULL,
 end_offset interval NOT NULL,
 coverage_start timestamptz,
 completed_through timestamptz,
 last_success_at timestamptz,
 attempts bigint NOT NULL DEFAULT 0,
 recovered_batches bigint NOT NULL DEFAULT 0,
 CONSTRAINT ck_telemetry_summary_progress_coverage CHECK (completed_through>=coverage_start)
);
INSERT INTO telemetry_summary_progress(aggregate_name,source_name,bucket_width,lateness,end_offset)
SELECT name||'_'||tier,source,width,late,delay FROM (VALUES
 ('device_metrics','device_metrics'),('device_status','device_metrics'),
 ('container_metrics','device_metrics'),('container_tank_metrics','container_tank_metrics'),
 ('container_module_metrics','container_module_metrics'),('hashboard_metrics','hashboard_metrics')
) equipment(name,source) CROSS JOIN (VALUES
 ('hourly',INTERVAL '1 hour',INTERVAL '24 hours',INTERVAL '1 hour'),
 ('daily',INTERVAL '24 hours',INTERVAL '168 hours',INTERVAL '24 hours')
) tiers(tier,width,late,delay);

-- Each source has one canonical background job. Work is independently due so
-- a busy or failing tier cannot monopolize its siblings or recent refreshes.
CREATE TABLE telemetry_summary_work (
 aggregate_name text NOT NULL REFERENCES telemetry_summary_progress(aggregate_name),
 work_kind text NOT NULL CONSTRAINT ck_telemetry_summary_work_kind CHECK (work_kind IN ('catchup','recent')),
 next_run_at timestamptz NOT NULL DEFAULT now(),
 batch_buckets integer NOT NULL CONSTRAINT ck_telemetry_summary_work_batch CHECK (batch_buckets>0),
 replay_until timestamptz,
 pending boolean NOT NULL DEFAULT false,
 failures bigint NOT NULL DEFAULT 0,
 minimum_batch_failures integer NOT NULL DEFAULT 0,
 successful_batches integer NOT NULL DEFAULT 0,
 blocked boolean NOT NULL DEFAULT false,
 last_start timestamptz,
 last_end timestamptz,
 last_error text,
 last_success_at timestamptz,
 CONSTRAINT pk_telemetry_summary_work PRIMARY KEY (aggregate_name,work_kind)
);
INSERT INTO telemetry_summary_work(aggregate_name,work_kind,batch_buckets,next_run_at)
SELECT aggregate_name,kind,CASE WHEN bucket_width=INTERVAL '1 hour' THEN 24 ELSE 1 END,
 now()-CASE WHEN kind='recent' THEN INTERVAL '1 second' ELSE INTERVAL '0' END
FROM telemetry_summary_progress CROSS JOIN (VALUES ('catchup'),('recent')) kinds(kind);

CREATE FUNCTION retain_telemetry_raw(source_key text) RETURNS void
LANGUAGE plpgsql SET lock_timeout='5s' AS $$
DECLARE
 expires_before timestamptz;
 safe_end timestamptz;
 safe_start timestamptz;
 initialized boolean;
BEGIN
 SELECT now()-s.raw_retention,bool_and(p.completed_through IS NOT NULL),min(p.completed_through),max(p.coverage_start)
 INTO STRICT expires_before,initialized,safe_end,safe_start
 FROM telemetry_summary_source s JOIN telemetry_summary_progress p USING (source_name)
 WHERE s.source_name=source_key GROUP BY s.raw_retention;
 IF NOT COALESCE(initialized,false) OR safe_end<expires_before THEN
  UPDATE telemetry_summary_source SET retention_deferrals=retention_deferrals+1 WHERE source_name=source_key;
 END IF;
 IF NOT COALESCE(initialized,false) THEN RETURN; END IF;
 safe_end := LEAST(safe_end,expires_before);
 IF safe_end<=safe_start THEN RETURN; END IF;
 SELECT max(range_end) INTO safe_end FROM (
  SELECT range_end FROM timescaledb_information.chunks
  WHERE hypertable_schema='public' AND hypertable_name=source_key
   AND range_start>=safe_start AND range_end<=safe_end ORDER BY range_end LIMIT 32
 ) due;
 IF safe_end IS NULL THEN RETURN; END IF;
 PERFORM drop_chunks(source_key::regclass,older_than=>safe_end,newer_than=>safe_start);
 UPDATE telemetry_summary_source SET dropped_through=GREATEST(dropped_through,safe_end),last_retention_at=clock_timestamp()
 WHERE source_name=source_key;
END;
$$;
REVOKE ALL ON FUNCTION retain_telemetry_raw(text) FROM PUBLIC;

-- Internal worker step; invoke the canonical job through run_job, not this
-- procedure. Refresh owns transaction boundaries, so exception subtransactions
-- cannot wrap it. A committed pending marker makes failed attempts restartable.
CREATE PROCEDURE telemetry_summary_step(source_key text)
LANGUAGE plpgsql AS $$
DECLARE
 source telemetry_summary_source%ROWTYPE;
 progress telemetry_summary_progress%ROWTYPE;
 work telemetry_summary_work%ROWTYPE;
 start_at timestamptz;
 end_at timestamptz;
 final_target timestamptz;
 recent_target timestamptz;
 cadence interval;
 finalizing boolean;
 refresh_started timestamptz;
 fast_batch boolean;
BEGIN
 SELECT * INTO STRICT source FROM telemetry_summary_source WHERE source_name=source_key;
 UPDATE telemetry_summary_source SET last_attempt_at=clock_timestamp() WHERE source_name=source_key;
 -- A previous worker exited without acknowledging a refresh. Shrink only at
 -- bucket boundaries. At minimum size, stop after three failures and alert.
 UPDATE telemetry_summary_work w SET pending=false,failures=failures+1,
  minimum_batch_failures=minimum_batch_failures+CASE WHEN batch_buckets=1 THEN 1 ELSE 0 END,
  blocked=minimum_batch_failures+CASE WHEN batch_buckets=1 THEN 1 ELSE 0 END>=3,
  batch_buckets=GREATEST(1,batch_buckets/2),successful_batches=0,
  next_run_at=GREATEST(w.next_run_at,clock_timestamp()+INTERVAL '5 minutes'),
  last_error='refresh interrupted before acknowledgement; inspect Timescale job_errors'
 FROM telemetry_summary_progress p WHERE p.aggregate_name=w.aggregate_name AND p.source_name=source_key AND w.pending;

 -- Expiration gets a turn even when refresh work is continuously due.
 IF source.next_retention_at<=now() THEN
  UPDATE telemetry_summary_source SET next_retention_at=clock_timestamp()+INTERVAL '1 hour' WHERE source_name=source_key;
  BEGIN
   PERFORM retain_telemetry_raw(source_key);
   UPDATE telemetry_summary_source SET retention_error=NULL WHERE source_name=source_key;
  EXCEPTION WHEN OTHERS THEN
   UPDATE telemetry_summary_source SET retention_error=SQLERRM,next_retention_at=clock_timestamp()+INTERVAL '5 minutes'
    WHERE source_name=source_key;
  END;
 END IF;
 UPDATE telemetry_summary_progress p SET coverage_start=f.initial_floor,completed_through=f.initial_floor
 FROM (SELECT aggregate_name,telemetry_bucket_ceiling(bucket_width,source.raw_floor) AS initial_floor
  FROM telemetry_summary_progress WHERE source_name=source_key AND coverage_start IS NULL) f
 WHERE p.aggregate_name=f.aggregate_name;
 SELECT w.* INTO work FROM telemetry_summary_work w JOIN telemetry_summary_progress p USING (aggregate_name)
 WHERE p.source_name=source_key AND NOT w.blocked AND w.next_run_at<=now()
 ORDER BY w.next_run_at,w.aggregate_name,w.work_kind LIMIT 1;
 IF NOT FOUND THEN RETURN; END IF;
 SELECT * INTO STRICT progress FROM telemetry_summary_progress WHERE aggregate_name=work.aggregate_name;
 final_target := time_bucket(progress.bucket_width,now()-progress.lateness);
 recent_target := time_bucket(progress.bucket_width,now()-progress.end_offset);
 finalizing := work.work_kind='catchup';
 cadence := CASE WHEN progress.bucket_width=INTERVAL '1 hour' THEN INTERVAL '30 minutes' ELSE INTERVAL '6 hours' END;
 IF finalizing THEN
  start_at := progress.completed_through;
  end_at := LEAST(start_at+progress.bucket_width*work.batch_buckets,final_target);
 ELSE
  -- Replay newest buckets first even during a multi-week cold catch-up.
  -- Coordinated imports can request newer complete buckets through this
  -- cursor, without certifying coverage or advancing the write gate.
  end_at := LEAST(time_bucket(progress.bucket_width,now()),COALESCE(work.replay_until,recent_target));
  start_at := GREATEST(progress.coverage_start,final_target,end_at-progress.bucket_width*work.batch_buckets);
 END IF;
 IF start_at<source.dropped_through THEN
  UPDATE telemetry_summary_work SET blocked=true,pending=false,failures=failures+1,successful_batches=0,
   last_error=format('refresh would cross expired raw data at %s',source.dropped_through)
  WHERE aggregate_name=work.aggregate_name AND work_kind=work.work_kind;
  RETURN;
 END IF;
 IF start_at>=end_at THEN
  UPDATE telemetry_summary_work SET next_run_at=CASE WHEN finalizing THEN
   final_target+progress.bucket_width+progress.lateness ELSE clock_timestamp()+cadence END,replay_until=NULL
  WHERE aggregate_name=work.aggregate_name AND work_kind=work.work_kind;
  RETURN;
 END IF;
 -- Persist retry/backoff before Timescale commits internally. Other tasks keep
 -- their place ahead of this attempt, including after cancellation or restart.
 UPDATE telemetry_summary_work SET pending=true,last_start=start_at,last_end=end_at,
  next_run_at=clock_timestamp()+INTERVAL '5 minutes'
 WHERE aggregate_name=work.aggregate_name AND work_kind=work.work_kind;
 UPDATE telemetry_summary_progress SET attempts=attempts+1 WHERE aggregate_name=work.aggregate_name;
 COMMIT;
 IF finalizing THEN
  -- Retention's raw-table locks and retry bookkeeping committed above.
  -- An in-flight writer holds SHARE here; publishing waits for its commit.
  UPDATE telemetry_summary_write_gate SET frozen_through=GREATEST(frozen_through,end_at)
   WHERE source_name=source_key;
  COMMIT;
 END IF;
 refresh_started := clock_timestamp();
 CALL refresh_continuous_aggregate(progress.aggregate_name::regclass,start_at,end_at,force=>finalizing);
 fast_batch := clock_timestamp()-refresh_started<INTERVAL '1 minute';
 UPDATE telemetry_summary_progress SET
  completed_through=CASE WHEN finalizing THEN end_at ELSE completed_through END,
  last_success_at=clock_timestamp(),recovered_batches=recovered_batches+CASE WHEN finalizing THEN 1 ELSE 0 END
 WHERE aggregate_name=work.aggregate_name;
 -- Three fast successes restore capacity gradually, without growing a batch
 -- that is already taking a minute or more. Never exceed one UTC day.
 UPDATE telemetry_summary_work SET pending=false,last_error=NULL,minimum_batch_failures=0,last_success_at=clock_timestamp(),
  batch_buckets=CASE WHEN fast_batch AND work.successful_batches>=2 THEN
   LEAST(work.batch_buckets*2,CASE WHEN progress.bucket_width=INTERVAL '1 hour' THEN 24 ELSE 1 END) ELSE batch_buckets END,
  successful_batches=CASE WHEN fast_batch AND work.successful_batches<2 THEN successful_batches+1 ELSE 0 END,
  replay_until=CASE WHEN NOT finalizing AND start_at>GREATEST(progress.coverage_start,final_target) THEN start_at END,
  next_run_at=CASE WHEN finalizing AND end_at<final_target THEN clock_timestamp()
   WHEN finalizing THEN final_target+progress.bucket_width+progress.lateness
   WHEN start_at>GREATEST(progress.coverage_start,final_target) THEN clock_timestamp()
   ELSE clock_timestamp()+cadence END
 WHERE aggregate_name=work.aggregate_name AND work_kind=work.work_kind;
 IF finalizing AND end_at>=final_target THEN
  UPDATE telemetry_summary_source SET next_retention_at=LEAST(next_retention_at,clock_timestamp()) WHERE source_name=source_key;
 END IF;
END;
$$;
REVOKE ALL ON PROCEDURE telemetry_summary_step(text) FROM PUBLIC;

CREATE PROCEDURE maintain_telemetry_source(job_id integer,config jsonb)
LANGUAGE plpgsql AS $$
DECLARE
 source telemetry_summary_source%ROWTYPE;
 due timestamptz;
 enabled boolean;
BEGIN
 SELECT * INTO STRICT source FROM telemetry_summary_source WHERE source_name=config->>'source';
 IF job_id IS DISTINCT FROM source.job_id THEN
  RAISE EXCEPTION 'use canonical telemetry job % for %',source.job_id,source.source_name;
 END IF;
 -- Timescale serializes scheduled executions of one job. Foreground run_job
 -- bypasses that scheduler: request the canonical worker and return, so a pooled
 -- or hidden-role client can neither overlap it nor strand a session claim.
 IF NOT EXISTS (SELECT 1 FROM pg_stat_activity a JOIN timescaledb_information.jobs j
  ON j.job_id=source.job_id WHERE a.pid=pg_backend_pid() AND a.backend_type=j.application_name) THEN
  SELECT scheduled INTO STRICT enabled FROM timescaledb_information.jobs j WHERE j.job_id=source.job_id;
  IF NOT enabled THEN
   RAISE EXCEPTION 'telemetry maintenance is paused; explicitly enable job % before requesting it',job_id;
  END IF;
  PERFORM alter_job(job_id,next_start=>clock_timestamp());
  RETURN;
 END IF;
 -- Shared session lock survives refresh commits. The dedicated worker exits
 -- on failure, releasing it. A seeder holds the exclusive counterpart on its
 -- dedicated connection; an idle ordinary run_job caller holds no session lock.
 IF NOT pg_try_advisory_lock_shared(1030,source.source_name::regclass::oid::integer) THEN
  PERFORM alter_job(job_id,next_start=>clock_timestamp()+INTERVAL '30 seconds');
  RETURN;
 END IF;
 CALL telemetry_summary_step(source.source_name);
 -- Publish retention and refresh acknowledgements before an importer can
 -- acquire the exclusive lock and reopen retained history.
 COMMIT;
 PERFORM pg_advisory_unlock_shared(1030,source.source_name::regclass::oid::integer);
 SELECT LEAST(s.next_retention_at,min(w.next_run_at) FILTER (WHERE NOT w.blocked)) INTO due
 FROM telemetry_summary_source s JOIN telemetry_summary_progress p USING (source_name)
 JOIN telemetry_summary_work w USING (aggregate_name) WHERE s.source_name=source.source_name GROUP BY s.next_retention_at;
 -- Explicit next_start survives mark_end; idle jobs sleep until useful work.
 PERFORM alter_job(job_id,next_start=>GREATEST(clock_timestamp()+INTERVAL '10 seconds',due));
END;
$$;

UPDATE telemetry_summary_source s SET job_id=add_job('maintain_telemetry_source',INTERVAL '30 minutes',
 config=>jsonb_build_object('source',s.source_name),fixed_schedule=>false,
 initial_start=>now()+INTERVAL '1 minute'+n.ordinal*INTERVAL '45 seconds')
FROM (SELECT source_name,row_number() OVER (ORDER BY source_name)-1 AS ordinal FROM telemetry_summary_source) n
WHERE s.source_name=n.source_name;
SELECT alter_job(job_id,max_runtime=>INTERVAL '30 minutes',retry_period=>INTERVAL '10 seconds')
FROM telemetry_summary_source;

-- One lookup/lock per statement, with only a cached comparison per row. The
-- shared advisory transaction lock coordinates development imports; the shared
-- gate row lock coordinates normal writers with a finalizing maintenance batch.
CREATE FUNCTION lock_telemetry_summary_writes() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE floor_at timestamptz;
BEGIN
 PERFORM pg_advisory_xact_lock_shared(1030,TG_RELID::integer);
 SELECT frozen_through INTO STRICT floor_at FROM telemetry_summary_write_gate
 WHERE source_name=TG_ARGV[0] FOR SHARE;
 PERFORM set_config('protofleet.'||TG_ARGV[0]||'_write_floor',COALESCE(floor_at::text,'-infinity'),true);
 RETURN NULL;
END;
$$;
CREATE FUNCTION guard_telemetry_summary_time() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE floor_at timestamptz := current_setting('protofleet.'||TG_ARGV[0]||'_write_floor')::timestamptz;
BEGIN
 -- Cached/asynchronous readings and ON CONFLICT duplicates must not abort a
 -- healthy container poll. Updates also cannot remove a certified old row.
 IF NEW.time<floor_at OR (TG_OP='UPDATE' AND OLD.time<floor_at) THEN RETURN NULL; END IF;
 RETURN NEW;
END;
$$;
DO $$ DECLARE source text; BEGIN
 FOR source IN SELECT source_name FROM telemetry_summary_source LOOP
  EXECUTE format('CREATE TRIGGER lock_telemetry_summary_writes BEFORE INSERT OR UPDATE ON %I FOR EACH STATEMENT EXECUTE FUNCTION lock_telemetry_summary_writes(%L)',source,source);
  EXECUTE format('CREATE TRIGGER guard_telemetry_summary_time BEFORE INSERT OR UPDATE ON %I FOR EACH ROW EXECUTE FUNCTION guard_telemetry_summary_time(%L)',source,source);
 END LOOP;
END $$;

-- Development seeding uses an exclusive session maintenance lock. Reopen only
-- complete retained UTC days, invalidating coverage BEFORE modifying any raw.
-- Failure or disconnect leaves recovery able to rebuild the unfinished range.
CREATE FUNCTION prepare_telemetry_seed(requested_start timestamptz,requested_end timestamptz) RETURNS timestamptz
LANGUAGE plpgsql AS $$
DECLARE
 source telemetry_summary_source%ROWTYPE;
 safe_start timestamptz;
 aggregate_name text;
 populated boolean;
 empty_history boolean := true;
BEGIN
 IF requested_start IS NULL OR requested_end IS NULL OR requested_start>=requested_end THEN
  RAISE EXCEPTION 'telemetry seeding requires a nonempty affected range';
 END IF;
 IF NOT EXISTS (SELECT 1 FROM pg_locks WHERE locktype='advisory' AND pid=pg_backend_pid()
  AND classid=1030 AND objid='device_metrics'::regclass::oid AND objsubid=2 AND mode='ExclusiveLock' AND granted) THEN
  RAISE EXCEPTION 'telemetry seeding requires the exclusive maintenance lock';
 END IF;
 SELECT * INTO STRICT source FROM telemetry_summary_source WHERE source_name='device_metrics';
 IF source.last_retention_at IS NULL AND NOT EXISTS (SELECT 1 FROM device_metrics LIMIT 1) THEN
  FOR aggregate_name IN SELECT p.aggregate_name FROM telemetry_summary_progress p WHERE p.source_name=source.source_name LOOP
   EXECUTE format('SELECT EXISTS (SELECT 1 FROM %I LIMIT 1)',aggregate_name) INTO populated;
   empty_history := empty_history AND NOT populated;
  END LOOP;
 ELSE empty_history := false;
 END IF;
 IF empty_history THEN
  -- Proven empty sources can start their first history before migration time.
  safe_start := LEAST(source.raw_floor,time_bucket(INTERVAL '24 hours',requested_start));
  UPDATE telemetry_summary_source SET raw_floor=safe_start,dropped_through=safe_start WHERE source_name=source.source_name;
  UPDATE telemetry_summary_progress SET coverage_start=safe_start,completed_through=safe_start WHERE source_name=source.source_name;
 ELSE
  SELECT GREATEST(source.raw_floor,source.dropped_through,max(coverage_start)) INTO safe_start
  FROM telemetry_summary_progress WHERE source_name=source.source_name;
  safe_start := telemetry_bucket_ceiling(INTERVAL '24 hours',safe_start);
 END IF;
 safe_start := GREATEST(safe_start,time_bucket(INTERVAL '24 hours',requested_start));
 UPDATE telemetry_summary_progress SET
  coverage_start=COALESCE(coverage_start,telemetry_bucket_ceiling(bucket_width,source.raw_floor)),
  completed_through=LEAST(COALESCE(completed_through,telemetry_bucket_ceiling(bucket_width,source.raw_floor)),safe_start)
 WHERE source_name=source.source_name;
 UPDATE telemetry_summary_write_gate SET frozen_through=LEAST(frozen_through,safe_start) WHERE source_name=source.source_name;
 -- A retry may have already replaced raw, hiding the end of its deleted tail.
 -- Preserve any unfinished recent repair until those old buckets are rebuilt.
 UPDATE telemetry_summary_work w SET next_run_at=now(),
  replay_until=CASE WHEN w.work_kind='recent' THEN
   GREATEST(w.replay_until,LEAST(time_bucket(p.bucket_width,now()),telemetry_bucket_ceiling(p.bucket_width,requested_end))) END,
  pending=false,blocked=false,
  minimum_batch_failures=0,successful_batches=0,last_error=NULL
 FROM telemetry_summary_progress p WHERE p.aggregate_name=w.aggregate_name AND p.source_name=source.source_name;
 RETURN safe_start;
END;
$$;
REVOKE ALL ON FUNCTION prepare_telemetry_seed(timestamptz,timestamptz) FROM PUBLIC;

-- Summary expiration remains independent: these targets intentionally discard
-- old summary tiers, and never authorize discarding unfinished raw evidence.
SELECT add_retention_policy(aggregate_name::regclass,CASE WHEN bucket_width=INTERVAL '1 hour' THEN INTERVAL '3 months' ELSE INTERVAL '3 years' END,if_not_exists=>true)
FROM telemetry_summary_progress WHERE aggregate_name NOT IN ('device_status_hourly','device_status_daily');

-- Operator views depend on extension-owned catalogs, which sqlc's offline
-- schema parser does not have. They are not application query inputs.
DO $health$ BEGIN
EXECUTE $view$CREATE VIEW telemetry_summary_health AS
SELECT p.*,w.work_kind,w.next_run_at,w.batch_buckets,w.successful_batches,w.pending,w.failures,w.blocked,w.last_error,
 w.last_success_at AS work_last_success_at,w.last_start,w.last_end,
 CASE WHEN p.completed_through IS NOT NULL THEN
 GREATEST(INTERVAL '0',time_bucket(p.bucket_width,now()-p.lateness)-p.completed_through) END AS catchup_lag,
 s.job_id,js.last_run_status,js.total_failures,js.last_run_started_at,js.next_start
FROM telemetry_summary_progress p JOIN telemetry_summary_source s USING (source_name)
JOIN telemetry_summary_work w USING (aggregate_name)
LEFT JOIN timescaledb_information.job_stats js ON js.job_id=s.job_id;$view$;

EXECUTE $view$CREATE VIEW telemetry_retention_health AS
SELECT s.*,g.frozen_through,p.coverage_start,p.completed_through,
 (SELECT count(*) FROM timescaledb_information.chunks c WHERE c.hypertable_schema='public'
  AND c.hypertable_name=s.source_name AND c.range_start>=p.coverage_start
  AND c.range_end<=LEAST(p.completed_through,now()-s.raw_retention)) AS eligible_raw_chunks,
 (SELECT count(*) FROM timescaledb_information.chunks c WHERE c.hypertable_schema='public'
  AND c.hypertable_name=s.source_name AND c.range_start>=p.coverage_start
  AND c.range_end<now()-s.raw_retention AND c.range_end>p.completed_through) AS awaiting_summary_chunks,
 (SELECT count(*) FROM timescaledb_information.chunks c WHERE c.hypertable_schema='public'
  AND c.hypertable_name=s.source_name AND c.range_start<p.coverage_start
  AND c.range_end<now()-s.raw_retention) AS unverified_raw_chunks
FROM telemetry_summary_source s JOIN telemetry_summary_write_gate g USING (source_name)
CROSS JOIN LATERAL (SELECT max(coverage_start) AS coverage_start,min(completed_through) AS completed_through
 FROM telemetry_summary_progress p WHERE p.source_name=s.source_name) p;$view$;
END $health$;

-- fleet-migration: 001031_internal_hosted_allocation_history.up.sql
-- requires-shared: 1000
-- Establish ownership prospectively. Existing ownership proves only the period
-- beginning at this migration; it must never be projected into older samples.
-- Fence assignments and placement changes before the launch boundary and seed.
LOCK TABLE device,hosted_account_asset IN SHARE ROW EXCLUSIVE MODE;
CREATE TABLE hosted_account_history_epoch (
 singleton BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK (singleton),
 started_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp()
);
INSERT INTO hosted_account_history_epoch(singleton) VALUES(TRUE);
CREATE TABLE hosted_account_asset_interval (
 id BIGSERIAL PRIMARY KEY,
 organization_id BIGINT NOT NULL REFERENCES organization(id),
 kind INTEGER NOT NULL CHECK (kind IN (5,6)),
 asset_id BIGINT NOT NULL,
 device_id BIGINT NOT NULL,
 site_id BIGINT,
 device_identifier TEXT NOT NULL,
 client_id BIGINT NOT NULL,
 subclient_id BIGINT,
 started_at TIMESTAMPTZ NOT NULL,
 ended_at TIMESTAMPTZ,
 FOREIGN KEY (device_id,organization_id) REFERENCES device(id,org_id),
 FOREIGN KEY (organization_id,client_id) REFERENCES hosted_account(organization_id,id),
 FOREIGN KEY (organization_id,subclient_id) REFERENCES hosted_account(organization_id,id),
 CHECK (ended_at IS NULL OR ended_at >= started_at)
);
CREATE UNIQUE INDEX idx_hosted_account_asset_interval_open ON hosted_account_asset_interval(organization_id,kind,asset_id) WHERE ended_at IS NULL;
CREATE INDEX idx_hosted_account_asset_interval_asset ON hosted_account_asset_interval(organization_id,kind,asset_id,started_at);
CREATE INDEX idx_hosted_account_asset_interval_device ON hosted_account_asset_interval(device_id) WHERE ended_at IS NULL;
CREATE INDEX idx_hosted_account_asset_interval_client ON hosted_account_asset_interval(organization_id,client_id,started_at);
CREATE INDEX idx_hosted_account_asset_interval_subclient ON hosted_account_asset_interval(organization_id,subclient_id,started_at);
INSERT INTO hosted_account_asset_interval(organization_id,kind,asset_id,device_id,client_id,subclient_id,started_at,site_id,device_identifier)
SELECT a.organization_id,a.kind,a.asset_id,a.device_id,a.client_id,a.subclient_id,e.started_at,d.site_id,d.device_identifier
FROM hosted_account_asset a JOIN device d ON d.id=a.device_id CROSS JOIN hosted_account_history_epoch e WHERE client_id IS NOT NULL AND d.deleted_at IS NULL;
CREATE FUNCTION record_hosted_account_asset_interval() RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE boundary TIMESTAMPTZ := clock_timestamp();
BEGIN
 IF TG_OP='UPDATE' AND NEW.client_id IS NOT DISTINCT FROM OLD.client_id AND NEW.subclient_id IS NOT DISTINCT FROM OLD.subclient_id THEN RETURN NEW; END IF;
 UPDATE hosted_account_asset_interval SET ended_at=boundary
 WHERE organization_id=NEW.organization_id AND kind=NEW.kind AND asset_id=NEW.asset_id AND ended_at IS NULL;
 IF NEW.client_id IS NOT NULL THEN
  INSERT INTO hosted_account_asset_interval(organization_id,kind,asset_id,device_id,client_id,subclient_id,started_at,site_id,device_identifier)
  SELECT NEW.organization_id,NEW.kind,NEW.asset_id,NEW.device_id,NEW.client_id,NEW.subclient_id,boundary,d.site_id,d.device_identifier FROM device d WHERE d.id=NEW.device_id AND d.deleted_at IS NULL;
 END IF;
 RETURN NEW;
END;
$$;
CREATE TRIGGER hosted_account_asset_history AFTER INSERT OR UPDATE ON hosted_account_asset FOR EACH ROW EXECUTE FUNCTION record_hosted_account_asset_interval();

CREATE FUNCTION record_hosted_account_asset_placement() RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE boundary TIMESTAMPTZ := clock_timestamp();
BEGIN
 IF NEW.deleted_at IS NOT NULL THEN
  UPDATE hosted_account_asset_interval SET ended_at=boundary WHERE device_id=NEW.id AND ended_at IS NULL;
  RETURN NEW;
 END IF;
 IF NEW.site_id IS NOT DISTINCT FROM OLD.site_id AND NEW.device_identifier=OLD.device_identifier THEN RETURN NEW; END IF;
 WITH closed AS (
  UPDATE hosted_account_asset_interval SET ended_at=boundary WHERE device_id=NEW.id AND ended_at IS NULL RETURNING *
 )
 INSERT INTO hosted_account_asset_interval(organization_id,kind,asset_id,device_id,client_id,subclient_id,started_at,site_id,device_identifier)
 SELECT organization_id,kind,asset_id,device_id,client_id,subclient_id,boundary,NEW.site_id,NEW.device_identifier FROM closed;
 RETURN NEW;
END;
$$;
CREATE TRIGGER hosted_account_asset_placement AFTER UPDATE OF site_id,device_identifier,deleted_at ON device FOR EACH ROW EXECUTE FUNCTION record_hosted_account_asset_placement();

-- A current allocation does not authorize cached observations from a previous
-- owner or placement. Every contributor must fit one established interval.
CREATE FUNCTION hosted_account_source_authorized(
 organization BIGINT, asset_kind INTEGER, asset BIGINT, client BIGINT, subclient BIGINT,
 sample_site BIGINT, identifier TEXT, oldest TIMESTAMPTZ, newest TIMESTAMPTZ
) RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
 SELECT COALESCE(oldest IS NOT NULL AND newest IS NOT NULL AND newest>=oldest AND EXISTS (
  SELECT 1 FROM hosted_account_asset_interval i
  WHERE i.organization_id=organization AND i.kind=asset_kind AND i.asset_id=asset
   AND i.client_id=client AND COALESCE(i.subclient_id,0)=COALESCE(subclient,0)
   AND COALESCE(i.site_id,0)=COALESCE(sample_site,0) AND i.device_identifier=identifier
   AND oldest>=i.started_at AND (i.ended_at IS NULL OR newest<i.ended_at)
 ),false);
$$;

-- fleet-migration: 001032_internal_hosted_history_collection_index.up.sql
-- requires-shared: 1000
-- Module source time can be pinned by a stale PSU independently of fresh board
-- readings. Hosted history bounds collections before authorizing each source.
CREATE INDEX idx_container_module_metrics_module_collected_at
 ON container_module_metrics(module_id,collected_at DESC);

-- fleet-migration: 001033_internal_hosted_history_compression.up.sql
-- requires-shared: 1000
-- Keep collection bounds usable on compressed module history without assuming
-- that the partition timestamp and independent contributor windows agree.
ALTER TABLE container_module_metrics SET (
 timescaledb.compress_orderby = 'collected_at DESC,time DESC'
);
-- Changing the hypertable setting affects new chunks. Apply it to retained
-- compressed chunks too. Rehearse recompression/storage in the upgrade window.
DO $$
DECLARE chunk regclass;
BEGIN
 FOR chunk IN
  SELECT format('%I.%I',chunk_schema,chunk_name)::regclass
  FROM timescaledb_information.chunks
  WHERE hypertable_schema='public' AND hypertable_name='container_module_metrics' AND is_compressed
 LOOP
  PERFORM compress_chunk(chunk,if_not_compressed=>true,recompress=>true);
 END LOOP;
END;
$$;

-- fleet-migration: 001034_internal_invitation_rate_limit.up.sql
-- requires-shared: 1000
CREATE TABLE hosted_invitation_rate_limit (
    client_hash TEXT PRIMARY KEY,
    window_started_at TIMESTAMPTZ NOT NULL,
    attempts INTEGER NOT NULL CHECK (attempts > 0)
);

CREATE INDEX idx_hosted_invitation_rate_limit_window_started_at
    ON hosted_invitation_rate_limit (window_started_at);

-- fleet-migration: 001035_internal_topology_cleanup_throughput.up.sql
-- requires-shared: 1000
-- Keep one worker and bounded transactions. At 50 new receipts per run, the
-- former ten-second schedule cannot keep up with 100 controllers polling every
-- fifteen seconds. A one-second interval leaves capacity to drain a backlog
-- without increasing the batch cap or contending with parallel cleanup workers.
-- Drifting scheduling leaves a full interval after success and avoids snapping
-- failed runs to the next one-second slot. Timescale still caps ordinary failure
-- backoff at five intervals; the committed per-controller lease remains separate.
-- Preserve the rollout flag, scheduling state, runtime limit and retry setting.
SELECT alter_job(job_id, schedule_interval => INTERVAL '1 second', fixed_schedule => false)
FROM timescaledb_information.jobs
WHERE proc_schema = 'public' AND proc_name = 'compact_container_topology_job';

-- fleet-migration: 001036_internal_team_invitation.up.sql
-- requires-shared: 1000
-- Host team invitations grant an organization role on acceptance.
--
-- role.permission_revision counts every write to a role's permission rows, so
-- it only grows, even when an edit restores an earlier or empty permission set.
-- Startup reconciliation inserts with ON CONFLICT DO NOTHING, which writes no
-- row for permissions a role already has and leaves the revision unchanged.
ALTER TABLE role ADD COLUMN permission_revision BIGINT NOT NULL DEFAULT 1
    CHECK (permission_revision > 0);

CREATE FUNCTION bump_role_permission_revision() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP <> 'INSERT' THEN
        UPDATE role SET permission_revision = permission_revision + 1 WHERE id = OLD.role_id;
    END IF;
    IF TG_OP <> 'DELETE' THEN
        UPDATE role SET permission_revision = permission_revision + 1 WHERE id = NEW.role_id;
    END IF;
    RETURN NULL;
END;
$$;

CREATE TRIGGER role_permission_revision AFTER INSERT OR UPDATE OR DELETE ON role_permission
    FOR EACH ROW EXECUTE FUNCTION bump_role_permission_revision();

-- The authority fingerprint witnesses the invited role's permission revision
-- and the inviter's live role assignments with each assigned role's revision.
-- Any change to either invalidates an invitation permanently: role changes
-- replace assignment rows, and revisions never repeat.
CREATE FUNCTION team_invitation_authority_fingerprint(
    target_role_id BIGINT, inviter_user_id BIGINT, target_organization_id BIGINT
) RETURNS text LANGUAGE sql STABLE AS $$
    SELECT encode(sha256(convert_to(
        'role:' || target_role_id::text || ':' || COALESCE((
            SELECT r.permission_revision::text FROM role r WHERE r.id = target_role_id
        ), '')
        || '|inviter:' || COALESCE((
            SELECT string_agg(
                ur.id::text || ':' || ur.role_id::text || ':' || ur.scope_type || ':' || COALESCE(ur.scope_id::text, '') || ':'
                    || ir.permission_revision::text,
                ';' ORDER BY ur.id)
            FROM user_organization_role ur
            JOIN role ir ON ir.id = ur.role_id
            WHERE ur.user_id = inviter_user_id AND ur.organization_id = target_organization_id
                AND ur.deleted_at IS NULL
        ), ''),
        'UTF8')), 'hex')
$$;

CREATE TABLE organization_invitation (
    invitation_id VARCHAR(36) PRIMARY KEY,
    organization_id BIGINT NOT NULL,
    inviter_id BIGINT NOT NULL,
    role_id BIGINT NOT NULL,
    authority_fingerprint VARCHAR(64) NOT NULL,
    token_hash VARCHAR(64) NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    revoked_at TIMESTAMPTZ,
    accepted_at TIMESTAMPTZ,
    accepted_user_id BIGINT REFERENCES "user"(id) ON DELETE RESTRICT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT uq_organization_invitation_token_hash UNIQUE (token_hash),
    CONSTRAINT fk_organization_invitation_inviter FOREIGN KEY (inviter_id, organization_id)
        REFERENCES user_organization(user_id, organization_id) ON DELETE CASCADE,
    CONSTRAINT fk_organization_invitation_role FOREIGN KEY (role_id, organization_id)
        REFERENCES role(id, organization_id) ON DELETE CASCADE,
    CHECK (expires_at > created_at),
    CHECK ((accepted_at IS NULL) = (accepted_user_id IS NULL))
);

CREATE INDEX idx_organization_invitation_org_created
    ON organization_invitation (organization_id, created_at DESC, invitation_id DESC)
    WHERE accepted_at IS NULL;

-- fleet-migration: 001037_internal_diagnostic_observations.up.sql
-- requires-shared: 1000
-- Numeric intervals cannot represent state transitions or diagnostic evidence.
-- Keep poll observations at their actual collection and source timestamps.
CREATE TABLE hashboard_diagnostic_observation (
 hashboard_id bigint NOT NULL REFERENCES hashboard(id),
 installation_id bigint NOT NULL REFERENCES hashboard_installation(id),
 collected_at timestamptz NOT NULL,
 observed_at timestamptz,
 state text NOT NULL,
 state_observed_at timestamptz,
 mining_state text NOT NULL,
 isolated boolean,
 hashrail_ok boolean,
 refclks_ok boolean,
 health jsonb,
 issues jsonb NOT NULL DEFAULT '[]'::jsonb,
 CONSTRAINT pk_hashboard_diagnostic_observation PRIMARY KEY(hashboard_id,collected_at)
);
SELECT create_hypertable('hashboard_diagnostic_observation','collected_at',chunk_time_interval=>INTERVAL '1 day');
SELECT add_retention_policy('hashboard_diagnostic_observation',INTERVAL '30 days');

CREATE TABLE container_module_diagnostic_observation (
 module_id bigint NOT NULL REFERENCES container_module(id),
 collected_at timestamptz NOT NULL,
 site_id bigint,
 attribution_since timestamptz,
 state text NOT NULL,
 state_observed_at timestamptz,
 issues jsonb,
 CONSTRAINT pk_container_module_diagnostic_observation PRIMARY KEY(module_id,collected_at)
);
SELECT create_hypertable('container_module_diagnostic_observation','collected_at',chunk_time_interval=>INTERVAL '1 day');
SELECT add_retention_policy('container_module_diagnostic_observation',INTERVAL '30 days');

-- Keep attribution watermarks beyond raw-observation retention.
ALTER TABLE container_module ADD COLUMN observation_site_id bigint;
ALTER TABLE container_module ADD COLUMN attribution_since timestamptz;
UPDATE container_module m SET observation_site_id=d.site_id
FROM container_tank t JOIN device d ON d.id=t.device_id WHERE t.id=m.tank_id;

-- Retire CT1-only current-state labels. Attention/isolation do not establish a
-- canonical failure/operating state; the next poll supplies that evidence.
ALTER TABLE container_module DROP CONSTRAINT container_module_state_check;
UPDATE container_module SET issues=NULL,issues_observed_at=NULL,issues_source_times='{}'::jsonb;
UPDATE container_module SET state=CASE state WHEN 'hashing' THEN 'active' WHEN 'sleeping' THEN 'inactive' WHEN 'offline' THEN 'offline' ELSE 'unknown' END;
ALTER TABLE container_module ADD CONSTRAINT ck_container_module_state CHECK(state IN ('unknown','active','offline','inactive','maintenance','error','needs_mining_pool','updating','reboot_required'));
UPDATE hashboard_slot SET state=CASE state WHEN 'hashing' THEN 'active' WHEN 'sleeping' THEN 'inactive' WHEN 'offline' THEN 'offline' ELSE 'unknown' END;
UPDATE hashboard_latest_metrics SET state=CASE state WHEN 'hashing' THEN 'active' WHEN 'sleeping' THEN 'inactive' WHEN 'offline' THEN 'offline' ELSE 'unknown' END;

CREATE OR REPLACE VIEW fleet_mining_asset AS
SELECT 5::int AS kind, d.id, d.org_id, d.site_id,
       COALESCE(NULLIF(d.custom_name, ''), concat_ws(' ', dd.manufacturer, dd.model))::text AS name,
       d.device_identifier::text AS device_identifier, 0::int AS position, COALESCE(d.serial_number, '')::text AS reported_id,
       1::int AS facility_kind, COALESCE(d.building_id, rack.building_id) AS facility_id,
       3::int AS group_kind, rack.device_set_id AS group_id,
       CASE WHEN ds.status = 'OFFLINE' OR (ds.status IS NULL AND dp.pairing_status IN ('PAIRED','DEFAULT_PASSWORD')) OR (fn.last_seen_at < now() - INTERVAL '2 minutes' AND fn.enrollment_status = 'CONFIRMED' AND fn.deleted_at IS NULL) THEN 'offline'
            WHEN ds.status IN ('MAINTENANCE','INACTIVE') AND dp.pairing_status <> 'AUTHENTICATION_NEEDED' THEN lower(ds.status::text)
            WHEN dp.pairing_status='AUTHENTICATION_NEEDED' OR EXISTS (SELECT 1 FROM errors e WHERE e.device_id=d.id AND e.org_id=d.org_id AND e.closed_at IS NULL AND e.severity IN (1,2,3,4)) THEN 'error'
            ELSE COALESCE(lower(ds.status::text),'unknown') END::text AS state,
       dm.time AS observed_at, dm.hash_rate_hs AS hashrate_hs, dm.power_w, dm.temp_c AS temperature_c, (dm.efficiency_jh * 1e12)::double precision AS miner_efficiency_jth
FROM device d
JOIN discovered_device dd ON dd.id = d.discovered_device_id AND dd.org_id = d.org_id
JOIN device_pairing dp ON dp.device_id = d.id
LEFT JOIN device_status ds ON ds.device_id = d.id
LEFT JOIN fleet_node_device fnd ON fnd.device_id = d.id AND fnd.org_id = d.org_id
LEFT JOIN fleet_node fn ON fn.id = fnd.fleet_node_id
LEFT JOIN LATERAL (
  SELECT r.device_set_id, r.building_id
  FROM device_set_membership ms JOIN device_set s ON s.id = ms.device_set_id AND s.deleted_at IS NULL
  JOIN device_set_rack r ON r.device_set_id = s.id
  WHERE ms.device_id = d.id AND ms.device_set_type = 'rack' AND ms.org_id = d.org_id
) rack ON TRUE
LEFT JOIN LATERAL (
  SELECT time, hash_rate_hs, power_w, temp_c, efficiency_jh FROM device_metrics
  WHERE device_identifier = d.device_identifier AND time > now() - INTERVAL '10 minutes'
    AND time <= now() + INTERVAL '5 seconds'
    AND site_id IS NOT DISTINCT FROM d.site_id
    AND ds.status IS DISTINCT FROM 'OFFLINE'
    AND NOT COALESCE(fn.last_seen_at < now() - INTERVAL '2 minutes' AND fn.enrollment_status = 'CONFIRMED' AND fn.deleted_at IS NULL, FALSE)
  ORDER BY time DESC LIMIT 1
) dm ON TRUE
WHERE d.deleted_at IS NULL AND dd.deleted_at IS NULL AND dd.is_active AND dd.device_class <> 2
 AND dp.pairing_status IN ('PAIRED','AUTHENTICATION_NEEDED','DEFAULT_PASSWORD')
UNION ALL
SELECT 6::int, m.id, d.org_id, d.site_id,
       (COALESCE(NULLIF(d.custom_name, ''), 'Container ' || COALESCE(NULLIF(d.serial_number, ''), d.device_identifier)) || ' T' || t.position || ' M' || m.position)::text,
       d.device_identifier::text, m.position, m.reported_id,
       2::int, d.id, 4::int, t.id,
       CASE WHEN m.state_observed_at > now() - INTERVAL '2 minutes' AND m.state_observed_at <= now() + INTERVAL '5 seconds'
              AND m.last_seen_at > now() - INTERVAL '2 minutes' AND m.observation_site_id IS NOT DISTINCT FROM d.site_id THEN m.state ELSE 'unknown' END::text,
       mm.time, mm.hashrate_hs, mm.power_w, mm.temperature_c, (mm.efficiency_jh * 1e12)::double precision
FROM container_module m JOIN container_tank t ON t.id = m.tank_id
JOIN device d ON d.id = t.device_id
JOIN discovered_device dd ON dd.id = d.discovered_device_id AND dd.org_id = d.org_id
LEFT JOIN container_equipment_current_metrics mm ON mm.kind=6 AND mm.id = m.id
  AND mm.time > now() - INTERVAL '2 minutes' AND mm.time <= now() + INTERVAL '5 seconds'
  AND m.last_seen_at > now() - INTERVAL '2 minutes' AND m.state <> 'offline'
WHERE m.retired_at IS NULL AND t.present AND d.deleted_at IS NULL AND dd.deleted_at IS NULL;

-- fleet-migration: 001038_internal_module_psu_identity.up.sql
-- requires-shared: 1000
-- reported_module_id is the map's PSU-derived hashboard.moduleId, not
-- hashboard.psuIdentifier. module_id references Fleet's own numeric module ID.
-- Each organization resolves its own identity namespace. Ambiguous legacy claims remain
-- explicit and unresolved; no historical module records or samples are merged.
CREATE TABLE container_module_identity (
 org_id BIGINT NOT NULL REFERENCES organization(id),
 reported_module_id TEXT NOT NULL CHECK (reported_module_id <> '' AND reported_module_id=btrim(reported_module_id)),
 module_id BIGINT REFERENCES container_module(id),
 legacy_module_ids BIGINT[] NOT NULL DEFAULT '{}',
 PRIMARY KEY (org_id,reported_module_id),
 CONSTRAINT uq_container_module_identity_module UNIQUE(module_id)
);
INSERT INTO container_module_identity(org_id,reported_module_id,module_id,legacy_module_ids)
SELECT d.org_id,btrim(m.reported_id),CASE WHEN count(*)=1 AND octet_length(btrim(m.reported_id))<=256 THEN min(m.id) END,array_agg(m.id ORDER BY m.id)
FROM container_module m JOIN container_tank t ON t.id=m.tank_id JOIN device d ON d.id=t.device_id
WHERE btrim(m.reported_id)<>'' GROUP BY d.org_id,btrim(m.reported_id);
UPDATE container_module SET identity_confirmed=FALSE;

CREATE FUNCTION check_container_module_identity_org() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.module_id IS NOT NULL AND NOT EXISTS (
  SELECT 1 FROM container_module m JOIN container_tank t ON t.id=m.tank_id
  JOIN device d ON d.id=t.device_id WHERE m.id=NEW.module_id AND d.org_id=NEW.org_id
 ) THEN
  RAISE EXCEPTION 'module identity organization mismatch' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END;
$$;
CREATE TRIGGER container_module_identity_org BEFORE INSERT OR UPDATE ON container_module_identity
FOR EACH ROW EXECUTE FUNCTION check_container_module_identity_org();
CREATE FUNCTION check_container_module_placement_org() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
 IF EXISTS (
  SELECT 1 FROM container_module_identity i JOIN container_tank t ON t.id=NEW.tank_id
  JOIN device d ON d.id=t.device_id WHERE i.module_id=NEW.id AND i.org_id<>d.org_id
 ) THEN
  RAISE EXCEPTION 'module placement organization mismatch' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END;
$$;
CREATE TRIGGER container_module_placement_org BEFORE UPDATE OF tank_id ON container_module
FOR EACH ROW EXECUTE FUNCTION check_container_module_placement_org();

-- Placement evidence is separate from PSU identity. Seed current placement only
-- at this upgrade boundary; previous topology records keep their original data.
CREATE TABLE container_module_placement (
 id BIGSERIAL PRIMARY KEY,
 module_id BIGINT NOT NULL REFERENCES container_module(id),
 tank_id BIGINT NOT NULL REFERENCES container_tank(id),
 position INTEGER NOT NULL CHECK (position>0),
 site_id BIGINT,
 started_at TIMESTAMPTZ NOT NULL,
 ended_at TIMESTAMPTZ,
 CHECK (ended_at IS NULL OR ended_at>=started_at)
);
CREATE UNIQUE INDEX idx_container_module_placement_open ON container_module_placement(module_id) WHERE ended_at IS NULL;
CREATE INDEX idx_container_module_placement_history ON container_module_placement(module_id,started_at);
INSERT INTO container_module_placement(module_id,tank_id,position,site_id,started_at)
SELECT m.id,m.tank_id,m.position,d.site_id,CURRENT_TIMESTAMP
FROM container_module m JOIN container_tank t ON t.id=m.tank_id JOIN device d ON d.id=t.device_id
WHERE m.retired_at IS NULL;
UPDATE container_inventory SET observed_at=GREATEST(observed_at,CURRENT_TIMESTAMP);

-- Installations retain the physical placement witnessed then, even when the PSU
-- subsequently moves to another controller or tank with the same board members.
ALTER TABLE hashboard_installation ADD COLUMN device_id BIGINT REFERENCES device(id),
 ADD COLUMN tank_id BIGINT REFERENCES container_tank(id), ADD COLUMN module_position INTEGER;
UPDATE hashboard_installation i SET device_id=t.device_id,tank_id=m.tank_id,module_position=m.position
FROM container_module m JOIN container_tank t ON t.id=m.tank_id WHERE m.id=i.module_id;
ALTER TABLE hashboard_installation ALTER COLUMN device_id SET NOT NULL,
 ALTER COLUMN tank_id SET NOT NULL, ALTER COLUMN module_position SET NOT NULL;
CREATE FUNCTION capture_hashboard_installation_placement() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
 SELECT t.device_id,m.tank_id,m.position INTO NEW.device_id,NEW.tank_id,NEW.module_position
 FROM container_module m JOIN container_tank t ON t.id=m.tank_id WHERE m.id=NEW.module_id;
 RETURN NEW;
END;
$$;
CREATE TRIGGER hashboard_installation_placement BEFORE INSERT ON hashboard_installation
FOR EACH ROW EXECUTE FUNCTION capture_hashboard_installation_placement();

CREATE FUNCTION record_container_module_placement() RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE boundary TIMESTAMPTZ := COALESCE(NEW.retired_at,NEW.last_seen_at);
BEGIN
 UPDATE container_module_placement SET ended_at=GREATEST(started_at,boundary)
 WHERE module_id=NEW.id AND ended_at IS NULL
 AND (NEW.retired_at IS NOT NULL OR tank_id<>NEW.tank_id OR position<>NEW.position
   OR site_id IS DISTINCT FROM (SELECT d.site_id FROM container_tank t JOIN device d ON d.id=t.device_id WHERE t.id=NEW.tank_id));
 IF FOUND THEN
  -- A topology-only poll cannot carry old placement readings to a new scope.
  -- Keep source watermarks so a later cached poll cannot revive them.
  UPDATE container_module_latest_metrics SET hashrate_hs=NULL,power_w=NULL,
   temperature_c=NULL,efficiency_jh=NULL,coverage=NULL WHERE module_id=NEW.id;
 END IF;
 IF NEW.retired_at IS NULL THEN
  INSERT INTO container_module_placement(module_id,tank_id,position,site_id,started_at)
  SELECT NEW.id,NEW.tank_id,NEW.position,d.site_id,boundary
  FROM container_tank t JOIN device d ON d.id=t.device_id WHERE t.id=NEW.tank_id
  ON CONFLICT (module_id) WHERE ended_at IS NULL DO NOTHING;
 END IF;
 RETURN NEW;
END;
$$;
CREATE TRIGGER container_module_placement AFTER INSERT OR UPDATE OF tank_id,position,retired_at,last_seen_at ON container_module
FOR EACH ROW EXECUTE FUNCTION record_container_module_placement();

-- Site assignment changes read authority immediately, before another poll can
-- establish placement there. Withdraw readings without fabricating observations.
CREATE FUNCTION withdraw_container_module_site_readings() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
 -- A response captured before this assignment cannot establish new-site evidence.
 UPDATE container_inventory SET observed_at=GREATEST(observed_at,CURRENT_TIMESTAMP)
 WHERE device_id=NEW.id;
 UPDATE container_module m SET identity_confirmed=FALSE,state='unknown',
  state_observed_at=NULL,issues=NULL
 FROM container_tank t WHERE t.id=m.tank_id AND t.device_id=NEW.id;
 UPDATE container_module_latest_metrics mm SET hashrate_hs=NULL,power_w=NULL,
  temperature_c=NULL,efficiency_jh=NULL,coverage=NULL
 FROM container_module m JOIN container_tank t ON t.id=m.tank_id
 WHERE mm.module_id=m.id AND t.device_id=NEW.id;
 RETURN NEW;
END;
$$;
CREATE TRIGGER container_module_site_readings AFTER UPDATE OF site_id ON device
FOR EACH ROW WHEN (OLD.site_id IS DISTINCT FROM NEW.site_id)
EXECUTE FUNCTION withdraw_container_module_site_readings();

-- fleet-migration: 001039_internal_mining_groups.up.sql
-- requires-shared: 1000
LOCK TABLE organization IN SHARE ROW EXCLUSIVE MODE;
INSERT INTO permission(key,description) VALUES ('miner:activate_mining_groups','Enable per-hashboard pool configuration on a container.') ON CONFLICT(key) DO NOTHING;
INSERT INTO role_permission(role_id,permission_id)
SELECT r.id,p.id FROM role r CROSS JOIN permission p
WHERE r.builtin_key='ADMIN' AND r.deleted_at IS NULL AND p.key='miner:activate_mining_groups'
ON CONFLICT(role_id,permission_id) DO NOTHING;

-- One journal per controller; desired assignments and controller observations
-- are deliberately separate. Every outbound request is committed before I/O.
CREATE TABLE container_mining_group_state (
 device_id BIGINT PRIMARY KEY REFERENCES device(id),
 organization_id BIGINT NOT NULL REFERENCES organization(id),
 observed JSONB NOT NULL DEFAULT '{}',
 observed_at TIMESTAMPTZ,
 bindings JSONB NOT NULL DEFAULT '{}',
 operation_id UUID,
 attempt_id UUID REFERENCES device_write_attempt(attempt_id),
 actor_id BIGINT REFERENCES "user"(id),
 operation_kind TEXT NOT NULL DEFAULT '' CHECK(operation_kind IN ('','activate','pools')),
 status TEXT NOT NULL DEFAULT 'idle' CHECK(status IN ('idle','pending','applying','blocked','complete','failed')),
 message TEXT NOT NULL DEFAULT '',
 step JSONB,
 step_key UUID,
 controller_operation_id TEXT NOT NULL DEFAULT '',
 write_epoch BIGINT NOT NULL DEFAULT 0,
 revision BIGINT NOT NULL DEFAULT 1,
 updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
 retry_count INTEGER NOT NULL DEFAULT 0
   CONSTRAINT chk_container_mining_group_state_retry_count CHECK (retry_count BETWEEN 0 AND 7),
 next_reconcile_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
 activation_resume_mining BOOLEAN,
 activation_stop_requested BOOLEAN NOT NULL DEFAULT FALSE,
 transfer_failure_count INTEGER NOT NULL DEFAULT 0
   CONSTRAINT chk_container_mining_group_state_transfer_failure_count CHECK (transfer_failure_count BETWEEN 0 AND 3),
 FOREIGN KEY(device_id,organization_id) REFERENCES device(id,org_id)
);
CREATE TABLE hashboard_pool_intent (
 organization_id BIGINT NOT NULL,
 hashboard_id BIGINT NOT NULL,
 device_id BIGINT NOT NULL REFERENCES container_mining_group_state(device_id),
 installation_id BIGINT NOT NULL REFERENCES hashboard_installation(id),
 mac_address TEXT NOT NULL,
 -- Account is the pool's owner. Host-owned revisions have depth zero; customer
 -- dispatch remains disabled until authoritative board allocations exist.
 account_id BIGINT NOT NULL,
 pool_revision_id VARCHAR(36) NOT NULL,
 operation_id UUID NOT NULL,
 updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
 PRIMARY KEY(organization_id,hashboard_id),
 FOREIGN KEY(organization_id,hashboard_id) REFERENCES hashboard(org_id,id),
 FOREIGN KEY(organization_id,account_id,pool_revision_id) REFERENCES hosted_account_pool_revision(organization_id,account_id,revision_id)
);
CREATE INDEX idx_hashboard_pool_intent_device ON hashboard_pool_intent(device_id);
CREATE INDEX idx_container_mining_group_state_due
 ON container_mining_group_state(next_reconcile_at, device_id)
 WHERE status IN ('pending','applying','blocked');

-- Retain every request identity, including after a newer request completes.
CREATE TABLE mining_group_request (
 organization_id BIGINT NOT NULL REFERENCES organization(id),
 request_id UUID NOT NULL,
 device_id BIGINT NOT NULL REFERENCES device(id),
 fingerprint TEXT NOT NULL,
 created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
 PRIMARY KEY(organization_id,request_id)
);

-- fleet-migration: 001040_internal_fleet_asset_node_liveness.up.sql
-- requires-shared: 1000
CREATE OR REPLACE VIEW fleet_mining_asset AS
SELECT 5::int AS kind, d.id, d.org_id, d.site_id,
       COALESCE(NULLIF(d.custom_name, ''), concat_ws(' ', dd.manufacturer, dd.model))::text AS name,
       d.device_identifier::text AS device_identifier, 0::int AS position, COALESCE(d.serial_number, '')::text AS reported_id,
       1::int AS facility_kind, COALESCE(d.building_id, rack.building_id) AS facility_id,
       3::int AS group_kind, rack.device_set_id AS group_id,
       CASE WHEN ds.status = 'OFFLINE' OR (ds.status IS NULL AND dp.pairing_status IN ('PAIRED','DEFAULT_PASSWORD')) OR (fn.last_seen_at < now() - INTERVAL '2 minutes' AND fn.enrollment_status = 'CONFIRMED' AND fn.deleted_at IS NULL) THEN 'offline'
            WHEN ds.status IN ('MAINTENANCE','INACTIVE') AND dp.pairing_status <> 'AUTHENTICATION_NEEDED' THEN lower(ds.status::text)
            WHEN dp.pairing_status='AUTHENTICATION_NEEDED' OR EXISTS (SELECT 1 FROM errors e WHERE e.device_id=d.id AND e.org_id=d.org_id AND e.closed_at IS NULL AND e.severity IN (1,2,3,4)) THEN 'error'
            ELSE COALESCE(lower(ds.status::text),'unknown') END::text AS state,
       dm.time AS observed_at, dm.hash_rate_hs AS hashrate_hs, dm.power_w, dm.temp_c AS temperature_c, (dm.efficiency_jh * 1e12)::double precision AS miner_efficiency_jth
FROM device d
JOIN discovered_device dd ON dd.id = d.discovered_device_id AND dd.org_id = d.org_id
JOIN device_pairing dp ON dp.device_id = d.id
LEFT JOIN device_status ds ON ds.device_id = d.id
LEFT JOIN fleet_node_device fnd ON fnd.device_id = d.id AND fnd.org_id = d.org_id
LEFT JOIN fleet_node_live fn ON fn.id = fnd.fleet_node_id AND fn.org_id = fnd.org_id
LEFT JOIN LATERAL (
  SELECT r.device_set_id, r.building_id
  FROM device_set_membership ms JOIN device_set s ON s.id = ms.device_set_id AND s.deleted_at IS NULL
  JOIN device_set_rack r ON r.device_set_id = s.id
  WHERE ms.device_id = d.id AND ms.device_set_type = 'rack' AND ms.org_id = d.org_id
) rack ON TRUE
LEFT JOIN LATERAL (
  SELECT time, hash_rate_hs, power_w, temp_c, efficiency_jh FROM device_metrics
  WHERE device_identifier = d.device_identifier AND time > now() - INTERVAL '10 minutes'
    AND time <= now() + INTERVAL '5 seconds'
    AND site_id IS NOT DISTINCT FROM d.site_id
    AND ds.status IS DISTINCT FROM 'OFFLINE'
    AND NOT COALESCE(fn.last_seen_at < now() - INTERVAL '2 minutes' AND fn.enrollment_status = 'CONFIRMED' AND fn.deleted_at IS NULL, FALSE)
  ORDER BY time DESC LIMIT 1
) dm ON TRUE
WHERE d.deleted_at IS NULL AND dd.deleted_at IS NULL AND dd.is_active AND dd.device_class <> 2
 AND dp.pairing_status IN ('PAIRED','AUTHENTICATION_NEEDED','DEFAULT_PASSWORD')
UNION ALL
SELECT 6::int, m.id, d.org_id, d.site_id,
       (COALESCE(NULLIF(d.custom_name, ''), 'Container ' || COALESCE(NULLIF(d.serial_number, ''), d.device_identifier)) || ' T' || t.position || ' M' || m.position)::text,
       d.device_identifier::text, m.position, m.reported_id,
       2::int, d.id, 4::int, t.id,
       CASE WHEN m.state_observed_at > now() - INTERVAL '2 minutes' AND m.state_observed_at <= now() + INTERVAL '5 seconds'
              AND m.last_seen_at > now() - INTERVAL '2 minutes' AND m.observation_site_id IS NOT DISTINCT FROM d.site_id THEN m.state ELSE 'unknown' END::text,
       mm.time, mm.hashrate_hs, mm.power_w, mm.temperature_c, (mm.efficiency_jh * 1e12)::double precision
FROM container_module m JOIN container_tank t ON t.id = m.tank_id
JOIN device d ON d.id = t.device_id
JOIN discovered_device dd ON dd.id = d.discovered_device_id AND dd.org_id = d.org_id
LEFT JOIN container_equipment_current_metrics mm ON mm.kind=6 AND mm.id = m.id
  AND mm.time > now() - INTERVAL '2 minutes' AND mm.time <= now() + INTERVAL '5 seconds'
  AND m.last_seen_at > now() - INTERVAL '2 minutes' AND m.state <> 'offline'
WHERE m.retired_at IS NULL AND t.present AND d.deleted_at IS NULL AND dd.deleted_at IS NULL;

-- fleet-migration: 001041_internal_device_endpoints.up.sql
-- requires-shared: 1000
ALTER TABLE discovered_device
    ADD COLUMN endpoint_key TEXT NOT NULL DEFAULT '',
    ADD COLUMN endpoints JSONB NOT NULL DEFAULT '{}';
CREATE INDEX idx_discovered_device_endpoint_key ON discovered_device (org_id, endpoint_key)
    WHERE deleted_at IS NULL AND endpoint_key <> '';
CREATE TABLE container_ha_observation (
    device_id BIGINT PRIMARY KEY REFERENCES device(id) ON DELETE CASCADE,
    observed_at TIMESTAMPTZ NOT NULL,
    degraded BOOLEAN NOT NULL
);

-- fleet-migration: 001042_internal_fleet_node_standby_ha.up.sql
-- requires-shared: 1000
ALTER TABLE pending_enrollment ADD COLUMN standby_for_node_id bigint;
ALTER TABLE pending_enrollment ADD CONSTRAINT fk_pending_enrollment_standby_for
    FOREIGN KEY (standby_for_node_id, org_id) REFERENCES fleet_node(id, org_id) ON DELETE CASCADE;

CREATE TABLE fleet_node_ha_pair (
    id bigserial PRIMARY KEY,
    org_id bigint NOT NULL,
    active_node_id bigint NOT NULL,
    standby_node_id bigint NOT NULL,
    standby_ready boolean NOT NULL DEFAULT false,
    pending_credential_key bytea,
    status_reason text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    failed_over_at timestamptz,
    CONSTRAINT uq_fleet_node_ha_pair_active UNIQUE (active_node_id),
    CONSTRAINT uq_fleet_node_ha_pair_standby UNIQUE (standby_node_id),
    CONSTRAINT ck_fleet_node_ha_pair_distinct CHECK (active_node_id <> standby_node_id),
    CONSTRAINT fk_fleet_node_ha_pair_active FOREIGN KEY (active_node_id, org_id)
        REFERENCES fleet_node(id, org_id) ON DELETE CASCADE,
    CONSTRAINT fk_fleet_node_ha_pair_standby FOREIGN KEY (standby_node_id, org_id)
        REFERENCES fleet_node(id, org_id) ON DELETE CASCADE
);

-- fleet-migration: 001043_internal_curtailment_query_helpers.up.sql
-- requires-shared: 1000

-- Keep state outside the view: the restore guard explicitly locks st.
CREATE VIEW curtailment_automation_rule_binding WITH (security_invoker = true) AS
SELECT r.*, rule_revision.response_profile_revision,
       src.source_name AS mqtt_source_name,
       profile.profile_name AS response_profile_name,
       profile.site_id AS response_profile_site_id,
       profile.scope_json AS response_profile_scope_json
FROM curtailment_automation_rule r
JOIN curtailment_automation_rule_profile_revision rule_revision
    ON rule_revision.automation_rule_id = r.id
JOIN curtailment_mqtt_source_config src
    ON src.id = r.mqtt_source_id
    AND src.organization_id = r.org_id
JOIN curtailment_response_profile profile
    ON profile.id = r.response_profile_id
    AND profile.org_id = r.org_id;

-- STABLE keeps the live-event test on the calling statement's snapshot.
CREATE FUNCTION curtailment_automation_rule_has_live_event(p_rule_id bigint, p_org_id bigint)
RETURNS boolean LANGUAGE sql STABLE SECURITY INVOKER AS $$
    SELECT EXISTS (
        SELECT 1 FROM curtailment_event e
        WHERE e.state IN ('pending', 'active', 'restoring')
          AND (
              EXISTS (
                  SELECT 1 FROM curtailment_automation_rule_state st
                  WHERE st.rule_id = p_rule_id AND st.active_event_uuid = e.event_uuid
              )
              OR (e.org_id = p_org_id
                  AND e.external_source = 'curtailment_automation'
                  AND e.external_reference = p_rule_id::text)
          )
    );
$$;

-- Each caller consumes the returned generation in the same statement. The
-- upsert keeps the existing organization lock order and full/targeted semantics.
CREATE FUNCTION request_curtailment_rig_config_reconciliation(p_org_id bigint, p_requested_by bigint, p_full boolean)
RETURNS bigint
LANGUAGE sql VOLATILE SECURITY INVOKER AS $$
    INSERT INTO curtailment_rig_config_reconciliation AS reconciliation (
        organization_id, requested_by, full_reconcile_generation
    ) VALUES (p_org_id, p_requested_by, CASE WHEN p_full THEN 1 ELSE 0 END)
    ON CONFLICT (organization_id) DO UPDATE
    SET requested_by = EXCLUDED.requested_by,
        desired_generation = reconciliation.desired_generation + 1,
        full_reconcile_generation = CASE WHEN p_full
            THEN reconciliation.desired_generation + 1
            ELSE reconciliation.full_reconcile_generation END,
        retry_at = CURRENT_TIMESTAMP,
        last_error = NULL
    RETURNING reconciliation.desired_generation;
$$;

-- fleet-migration: 001044_internal_hosted_history_summaries.up.sql
-- requires-shared: 1000
-- Stop application and scheduled writers during this upgrade. Existing generic
-- summaries cannot prove allocation/source-time authorization; never seed this
-- history from them or project current allocations into older measurements.
LOCK TABLE device_metrics,miner_state_snapshots,container_module_metrics,
 container_module_diagnostic_observation,hosted_account_asset_interval IN SHARE ROW EXCLUSIVE MODE;

CREATE TABLE hosted_account_history_summary_epoch (
 singleton boolean CONSTRAINT pk_hosted_account_history_summary_epoch PRIMARY KEY DEFAULT true
  CONSTRAINT ck_hosted_account_history_summary_epoch_singleton CHECK(singleton),
 started_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
INSERT INTO hosted_account_history_summary_epoch(singleton) VALUES(true);

CREATE FUNCTION hosted_history_metric_code(metric_name text) RETURNS smallint
LANGUAGE sql IMMUTABLE PARALLEL SAFE AS $$
 SELECT CASE metric_name WHEN 'hashrate' THEN 1 WHEN 'power' THEN 2
  WHEN 'temperature' THEN 3 WHEN 'state' THEN 4 END::smallint
$$;

-- Retain one independently authorized source per metric and raw observation.
-- The origin key includes the complete raw primary key, making retries and
-- corrections replacements rather than additional weight in an average.
CREATE TABLE hosted_account_history_sample (
 time timestamptz NOT NULL,
 interval_id bigint NOT NULL REFERENCES hosted_account_asset_interval(id),
 metric smallint NOT NULL CONSTRAINT ck_hosted_account_history_sample_metric CHECK(metric BETWEEN 1 AND 4),
 value double precision,
 complete boolean,
 source_oldest_at timestamptz NOT NULL,
 source_newest_at timestamptz NOT NULL,
 source_id smallint NOT NULL,
 origin_key text NOT NULL,
 origin_time timestamptz NOT NULL,
 origin_collected_at timestamptz NOT NULL,
 CONSTRAINT pk_hosted_account_history_sample PRIMARY KEY(time,source_id,origin_key,origin_time,origin_collected_at,metric,interval_id),
 CONSTRAINT ck_hosted_account_history_sample_source CHECK(source_newest_at>=source_oldest_at),
 CONSTRAINT ck_hosted_account_history_sample_value CHECK(value>'-Infinity'::float8 AND value<'Infinity'::float8)
);
SELECT create_hypertable('hosted_account_history_sample','time',chunk_time_interval=>INTERVAL '1 hour');
CREATE INDEX idx_hosted_account_history_sample_interval_time ON hosted_account_history_sample(interval_id,time DESC);
CREATE INDEX idx_hosted_account_asset_interval_capture_miner ON hosted_account_asset_interval(device_identifier,site_id,started_at) WHERE kind=5;
CREATE INDEX idx_hosted_account_asset_interval_capture_module ON hosted_account_asset_interval(asset_id,site_id,started_at) WHERE kind=6;
ALTER TABLE hosted_account_history_sample SET(timescaledb.compress,
 timescaledb.compress_segmentby='interval_id,metric',timescaledb.compress_orderby='time DESC');
SELECT add_compression_policy('hosted_account_history_sample',INTERVAL '6 hours',schedule_interval=>INTERVAL '1 hour');

-- One row per allocation/source period, with independent evidence per metric.
CREATE MATERIALIZED VIEW hosted_account_history_hourly
WITH(timescaledb.continuous,timescaledb.materialized_only=false) AS
SELECT time_bucket(INTERVAL '1 hour',time) AS bucket,interval_id,
 sum(value) FILTER(WHERE metric=hosted_history_metric_code('hashrate')) AS hashrate_sum,
 count(value) FILTER(WHERE metric=hosted_history_metric_code('hashrate')) AS hashrate_count,
 count(value) FILTER(WHERE metric=hosted_history_metric_code('hashrate') AND value>0) AS hashrate_positive_count,
 bool_and(complete) FILTER(WHERE metric=hosted_history_metric_code('hashrate')) AS hashrate_complete,
 min(source_oldest_at) FILTER(WHERE metric=hosted_history_metric_code('hashrate')) AS hashrate_source_oldest_at,
 max(source_newest_at) FILTER(WHERE metric=hosted_history_metric_code('hashrate')) AS hashrate_source_newest_at,
 sum(value) FILTER(WHERE metric=hosted_history_metric_code('power')) AS power_sum,
 count(value) FILTER(WHERE metric=hosted_history_metric_code('power')) AS power_count,
 bool_and(complete) FILTER(WHERE metric=hosted_history_metric_code('power')) AS power_complete,
 min(source_oldest_at) FILTER(WHERE metric=hosted_history_metric_code('power')) AS power_source_oldest_at,
 max(source_newest_at) FILTER(WHERE metric=hosted_history_metric_code('power')) AS power_source_newest_at,
 sum(value) FILTER(WHERE metric=hosted_history_metric_code('temperature')) AS temperature_sum,
 count(value) FILTER(WHERE metric=hosted_history_metric_code('temperature')) AS temperature_count,
 last(value,time) FILTER(WHERE metric=hosted_history_metric_code('temperature') AND value IS NOT NULL) AS temperature_latest_value,
 max(time) FILTER(WHERE metric=hosted_history_metric_code('temperature') AND value IS NOT NULL) AS temperature_latest_at,
 min(source_oldest_at) FILTER(WHERE metric=hosted_history_metric_code('temperature')) AS temperature_source_oldest_at,
 max(source_newest_at) FILTER(WHERE metric=hosted_history_metric_code('temperature')) AS temperature_source_newest_at,
 count(value) FILTER(WHERE metric=hosted_history_metric_code('state')) AS state_count,
 last(value,time) FILTER(WHERE metric=hosted_history_metric_code('state') AND value IS NOT NULL) AS state_latest_value,
 max(time) FILTER(WHERE metric=hosted_history_metric_code('state') AND value IS NOT NULL) AS state_latest_at,
 min(source_oldest_at) FILTER(WHERE metric=hosted_history_metric_code('state')) AS state_source_oldest_at,
 max(source_newest_at) FILTER(WHERE metric=hosted_history_metric_code('state')) AS state_source_newest_at
FROM hosted_account_history_sample GROUP BY bucket,interval_id WITH NO DATA;
CREATE INDEX idx_hosted_account_history_hourly_interval_bucket ON hosted_account_history_hourly(interval_id,bucket DESC);

-- Roll up hourly rows without collapsing metric-specific source windows.
CREATE MATERIALIZED VIEW hosted_account_history_daily
WITH(timescaledb.continuous,timescaledb.materialized_only=false) AS
SELECT time_bucket(INTERVAL '24 hours',bucket) AS bucket,interval_id,
 sum(hashrate_sum) AS hashrate_sum,
 sum(hashrate_count)::bigint AS hashrate_count,
 sum(hashrate_positive_count)::bigint AS hashrate_positive_count,
 bool_and(hashrate_complete) AS hashrate_complete,
 min(hashrate_source_oldest_at) AS hashrate_source_oldest_at,
 max(hashrate_source_newest_at) AS hashrate_source_newest_at,
 sum(power_sum) AS power_sum,
 sum(power_count)::bigint AS power_count,
 bool_and(power_complete) AS power_complete,
 min(power_source_oldest_at) AS power_source_oldest_at,
 max(power_source_newest_at) AS power_source_newest_at,
 sum(temperature_sum) AS temperature_sum,
 sum(temperature_count)::bigint AS temperature_count,
 last(temperature_latest_value,temperature_latest_at) FILTER(WHERE temperature_latest_at IS NOT NULL) AS temperature_latest_value,
 max(temperature_latest_at) AS temperature_latest_at,
 min(temperature_source_oldest_at) AS temperature_source_oldest_at,
 max(temperature_source_newest_at) AS temperature_source_newest_at,
 sum(state_count)::bigint AS state_count,
 last(state_latest_value,state_latest_at) FILTER(WHERE state_latest_at IS NOT NULL) AS state_latest_value,
 max(state_latest_at) AS state_latest_at,
 min(state_source_oldest_at) AS state_source_oldest_at,
 max(state_source_newest_at) AS state_source_newest_at
FROM hosted_account_history_hourly GROUP BY 1,interval_id WITH NO DATA;
CREATE INDEX idx_hosted_account_history_daily_interval_bucket ON hosted_account_history_daily(interval_id,bucket DESC);

-- The caller scopes intervals to the requested account. Share physical-site
-- authority before expanding observations; source-time checks remain per metric.
CREATE FUNCTION hosted_history_authorized_intervals(requested jsonb,site_filter bigint,
 policy_restricted boolean,policy_sites bigint[],host_authority jsonb)
RETURNS TABLE(interval_id bigint,kind integer,asset_id bigint,device_identifier text,
 site_id bigint,started_at timestamptz,ended_at timestamptz)
LANGUAGE sql STABLE PARALLEL SAFE AS $$
 SELECT i.* FROM jsonb_to_recordset(requested) AS i(interval_id bigint,kind integer,
  asset_id bigint,device_identifier text,site_id bigint,started_at timestamptz,ended_at timestamptz)
 WHERE (site_filter=0 OR i.site_id=site_filter)
  AND (NOT policy_restricted OR i.site_id=ANY(policy_sites))
  AND NOT EXISTS (
   SELECT 1 FROM jsonb_to_recordset(host_authority) AS a(org_wide boolean,sites bigint[])
   WHERE NOT (CASE WHEN a.org_wide THEN i.site_id=0 OR NOT(i.site_id=ANY(a.sites)) ELSE i.site_id=ANY(a.sites) END)
  )
$$;

-- Keep Timescale's catalog behind a typed read interface. Uninitialized
-- watermarks are -infinity: charge the full available request in that case.
CREATE FUNCTION hosted_account_history_watermarks(available_from timestamptz)
RETURNS TABLE(hourly timestamptz,daily timestamptz)
LANGUAGE sql STABLE AS $$
 SELECT GREATEST(available_from,COALESCE(
  _timescaledb_functions.to_timestamp(_timescaledb_functions.cagg_watermark(h.mat_hypertable_id)),available_from)),
 GREATEST(available_from,COALESCE(
  _timescaledb_functions.to_timestamp(_timescaledb_functions.cagg_watermark(d.mat_hypertable_id)),available_from))
 FROM _timescaledb_catalog.continuous_agg h
 CROSS JOIN _timescaledb_catalog.continuous_agg d
 WHERE h.user_view_schema='public' AND h.user_view_name='hosted_account_history_hourly'
  AND d.user_view_schema='public' AND d.user_view_name='hosted_account_history_daily'
$$;

-- Standard Timescale policies maintain recent summaries. No historical bootstrap
-- or catch-up state is needed for this prospective stream.
DO $$ DECLARE
 raw_retention CONSTANT interval := INTERVAL '10 days';
 refresh_window CONSTANT interval := raw_retention-INTERVAL '1 day';
 hourly_refresh_cadence CONSTANT interval := INTERVAL '30 minutes';
 daily_refresh_cadence CONSTANT interval := INTERVAL '6 hours';
 hourly_retention CONSTANT interval := INTERVAL '3 months';
 daily_retention CONSTANT interval := INTERVAL '3 years';
BEGIN
 PERFORM add_continuous_aggregate_policy('hosted_account_history_hourly',
  start_offset=>refresh_window,end_offset=>INTERVAL '1 hour',schedule_interval=>hourly_refresh_cadence);
 PERFORM add_continuous_aggregate_policy('hosted_account_history_daily',
  start_offset=>refresh_window,end_offset=>INTERVAL '1 day',schedule_interval=>daily_refresh_cadence);
 PERFORM add_retention_policy('hosted_account_history_sample',raw_retention);
 PERFORM add_retention_policy('hosted_account_history_hourly',hourly_retention);
 PERFORM add_retention_policy('hosted_account_history_daily',daily_retention);
END $$;

-- Raw and retained module charts must classify a state identically.
CREATE FUNCTION hosted_module_uptime_state(state text) RETURNS smallint
LANGUAGE sql IMMUTABLE PARALLEL SAFE AS $$
 SELECT CASE WHEN state='active' THEN 3
  WHEN state IN ('error','needs_mining_pool','updating','reboot_required') THEN 2
  WHEN state='offline' THEN 0
  WHEN state IN ('inactive','maintenance') THEN 1
  ELSE 4 END::smallint
$$;

-- Only malformed coverage conversions become unavailable evidence. Errors from
-- capture writes still propagate; this does not hide database/schema failures.
CREATE FUNCTION hosted_history_metric_coverage(coverage jsonb)
RETURNS TABLE(oldest_at timestamptz,newest_at timestamptz,complete boolean)
LANGUAGE sql STABLE AS $$
 WITH validity AS (
  SELECT COALESCE(pg_input_is_valid(coverage->>'oldest_at','timestamptz'),true)
   AND COALESCE(pg_input_is_valid(coverage->>'newest_at','timestamptz'),true)
   AND COALESCE(pg_input_is_valid(coverage->>'topology_complete','boolean'),true)
   AND COALESCE(pg_input_is_valid(coverage->>'expected','integer'),true)
   AND COALESCE(pg_input_is_valid(coverage->>'reporting','integer'),true) AS valid
 )
 SELECT CASE WHEN valid THEN (coverage->>'oldest_at')::timestamptz END,
  CASE WHEN valid THEN (coverage->>'newest_at')::timestamptz END,
  CASE WHEN valid THEN COALESCE((coverage->>'topology_complete')::boolean
   AND (coverage->>'expected')::integer>0
   AND (coverage->>'reporting')::integer=(coverage->>'expected')::integer,false) ELSE false END
 FROM validity
$$;

CREATE FUNCTION record_hosted_account_history_sample() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
 miner_metric_source CONSTANT smallint := 1;
 miner_state_source CONSTANT smallint := 2;
 module_metric_source CONSTANT smallint := 3;
 module_state_source CONSTANT smallint := 4;
 hashrate_metric CONSTANT smallint := hosted_history_metric_code('hashrate');
 power_metric CONSTANT smallint := hosted_history_metric_code('power');
 temperature_metric CONSTANT smallint := hosted_history_metric_code('temperature');
 state_metric CONSTANT smallint := hosted_history_metric_code('state');
 miner_asset_kind CONSTANT integer := 5;
 module_asset_kind CONSTANT integer := 6;
 source_freshness CONSTANT interval := INTERVAL '2 minutes';
 source_clock_skew CONSTANT interval := INTERVAL '5 seconds';
 row_data jsonb;
 previous_data jsonb;
 source_number smallint;
 asset_kind integer;
 asset bigint;
 identifier text;
 site bigint;
 organization bigint;
 origin_at timestamptz;
 collected_at timestamptz;
 original_key text;
 previous_times timestamptz[];
 epoch timestamptz;
BEGIN
 -- Hypertable row triggers execute on physical chunks, whose TG_TABLE_NAME is
 -- not the logical source name. Keep source identity in the trigger argument.
 source_number := CASE TG_ARGV[0]
  WHEN 'miner_metrics' THEN miner_metric_source WHEN 'miner_state' THEN miner_state_source
  WHEN 'module_metrics' THEN module_metric_source WHEN 'module_state' THEN module_state_source END;
 -- Most estate telemetry need not enter hosted history. Skip unallocated
 -- observations before normalizing their individual metrics.
 IF TG_OP='INSERT' THEN
  IF source_number IN (miner_metric_source,miner_state_source) THEN
   -- Literal kinds let generic plans prove the capture partial-index predicates.
   IF NOT EXISTS(SELECT 1 FROM hosted_account_asset_interval i WHERE i.kind=5
    AND i.device_identifier=NEW.device_identifier AND COALESCE(i.site_id,0)=COALESCE(NEW.site_id,0)
    AND i.started_at<=NEW.time AND (i.ended_at IS NULL OR NEW.time<i.ended_at)) THEN RETURN NEW; END IF;
  ELSE
   IF NOT EXISTS(SELECT 1 FROM hosted_account_asset_interval i WHERE i.kind=6
    AND i.asset_id=NEW.module_id AND COALESCE(i.site_id,0)=COALESCE(NEW.site_id,0)
    AND i.started_at<=NEW.collected_at+source_clock_skew
    AND (i.ended_at IS NULL OR NEW.collected_at<i.ended_at+source_freshness)) THEN RETURN NEW; END IF;
  END IF;
 END IF;
 IF TG_OP IN ('UPDATE','DELETE') THEN
  previous_data := to_jsonb(OLD);
  original_key := COALESCE(previous_data->>'device_identifier',previous_data->>'module_id');
  origin_at := COALESCE((previous_data->>'time')::timestamptz,(previous_data->>'collected_at')::timestamptz);
  collected_at := COALESCE((previous_data->>'collected_at')::timestamptz,origin_at);
  previous_times := ARRAY[origin_at,collected_at,(previous_data->>'state_observed_at')::timestamptz,
   (SELECT newest_at FROM hosted_history_metric_coverage(previous_data#>'{coverage,hashrate}')),
   (SELECT newest_at FROM hosted_history_metric_coverage(previous_data#>'{coverage,power}')),
   (SELECT newest_at FROM hosted_history_metric_coverage(previous_data#>'{coverage,temperature}'))];
  DELETE FROM hosted_account_history_sample
  WHERE time=ANY(previous_times) AND source_id=source_number
   AND origin_key=original_key AND origin_time=origin_at AND origin_collected_at=collected_at;
 END IF;
 IF TG_OP='DELETE' THEN RETURN OLD; END IF;
 row_data := to_jsonb(NEW);
 asset_kind := CASE WHEN source_number IN (miner_metric_source,miner_state_source) THEN miner_asset_kind ELSE module_asset_kind END;
 asset := (row_data->>'module_id')::bigint;
 identifier := row_data->>'device_identifier';
 site := COALESCE((row_data->>'site_id')::bigint,0);
 organization := (row_data->>'org_id')::bigint;
 original_key := COALESCE(identifier,asset::text);
 origin_at := COALESCE((row_data->>'time')::timestamptz,(row_data->>'collected_at')::timestamptz);
 collected_at := COALESCE((row_data->>'collected_at')::timestamptz,origin_at);
 SELECT started_at INTO STRICT epoch FROM hosted_account_history_summary_epoch WHERE singleton;

 INSERT INTO hosted_account_history_sample(time,interval_id,metric,value,complete,source_oldest_at,source_newest_at,
  source_id,origin_key,origin_time,origin_collected_at)
 SELECT samples.newest_at,i.id,samples.metric,
  CASE WHEN samples.metric IN (hashrate_metric,power_metric) THEN CASE WHEN samples.value>=0 AND samples.value<'Infinity'::float8 THEN samples.value END
   ELSE CASE WHEN samples.value>'-Infinity'::float8 AND samples.value<'Infinity'::float8 THEN samples.value END END,
  samples.complete,samples.oldest_at,samples.newest_at,source_number,original_key,origin_at,collected_at
 FROM (
  -- Miner samples have one observation timestamp; paired validity is shared
  -- by hash and power so invalid/missing inputs cannot manufacture efficiency.
  SELECT metric,value,origin_at AS oldest_at,origin_at AS newest_at,
   CASE WHEN metric IN (hashrate_metric,power_metric) THEN COALESCE(
    (row_data->>'hash_rate_hs')::float8>=0 AND (row_data->>'hash_rate_hs')::float8<'Infinity'::float8
    AND (row_data->>'power_w')::float8>=0 AND (row_data->>'power_w')::float8<'Infinity'::float8,false) END AS complete
  FROM (VALUES(hashrate_metric,(row_data->>'hash_rate_hs')::float8),
   (power_metric,(row_data->>'power_w')::float8),(temperature_metric,(row_data->>'temp_c')::float8)) miner(metric,value)
  WHERE source_number=miner_metric_source
  UNION ALL
  SELECT state_metric,CASE WHEN source_number=miner_state_source THEN (row_data->>'state')::float8 END,
   origin_at,origin_at,NULL::boolean WHERE source_number=miner_state_source
  UNION ALL
  -- Preserve each module contributor window, including missing-source markers
  -- for efficiency coverage. A different metric's timestamp grants no access.
  SELECT metric,CASE WHEN has_source THEN value END,
   CASE WHEN has_source THEN oldest_at ELSE collected_at END,
   CASE WHEN has_source THEN newest_at ELSE collected_at END,
   CASE WHEN metric IN (hashrate_metric,power_metric) THEN has_source AND source.complete
    AND COALESCE(value>=0 AND value<'Infinity'::float8,false) END
  FROM (VALUES
   (hashrate_metric,'hashrate',(row_data->>'hashrate_hs')::float8),
   (power_metric,'power',(row_data->>'power_w')::float8),
   (temperature_metric,'temperature',(row_data->>'temperature_c')::float8)
  ) module(metric,name,value)
  CROSS JOIN LATERAL hosted_history_metric_coverage(row_data->'coverage'->name) source
  CROSS JOIN LATERAL(SELECT COALESCE(oldest_at>'0001-01-01T00:00:00Z'::timestamptz
   AND isfinite(oldest_at) AND isfinite(newest_at) AND newest_at>=oldest_at,false) AS has_source) validity
  WHERE source_number=module_metric_source AND (has_source OR metric IN (hashrate_metric,power_metric))
  UNION ALL
  SELECT state_metric,hosted_module_uptime_state(row_data->>'state')::float8,
   (row_data->>'state_observed_at')::timestamptz,(row_data->>'state_observed_at')::timestamptz,NULL::boolean
  WHERE source_number=module_state_source
   AND (row_data->>'state_observed_at')::timestamptz>collected_at-source_freshness
   AND (row_data->>'state_observed_at')::timestamptz<=collected_at+source_clock_skew
   AND ((row_data->>'attribution_since') IS NULL
    OR (row_data->>'state_observed_at')::timestamptz>=(row_data->>'attribution_since')::timestamptz)
 ) samples
 JOIN hosted_account_asset_interval i ON i.kind=asset_kind
  AND (CASE WHEN asset_kind=miner_asset_kind THEN i.device_identifier=identifier ELSE i.asset_id=asset END)
  AND COALESCE(i.site_id,0)=site AND (organization IS NULL OR i.organization_id=organization)
  AND samples.oldest_at>=i.started_at AND (i.ended_at IS NULL OR samples.newest_at<i.ended_at)
  AND (asset_kind=miner_asset_kind OR collected_at>=i.started_at-source_clock_skew
   AND (i.ended_at IS NULL OR collected_at<i.ended_at+source_freshness))
 WHERE samples.oldest_at>=epoch
 ON CONFLICT(time,source_id,origin_key,origin_time,origin_collected_at,metric,interval_id) DO UPDATE
 SET value=EXCLUDED.value,complete=EXCLUDED.complete,source_oldest_at=EXCLUDED.source_oldest_at,source_newest_at=EXCLUDED.source_newest_at;
 RETURN NEW;
END;
$$;
CREATE TRIGGER record_hosted_account_history AFTER INSERT OR UPDATE OR DELETE ON device_metrics
 FOR EACH ROW EXECUTE FUNCTION record_hosted_account_history_sample('miner_metrics');
CREATE TRIGGER record_hosted_account_history AFTER INSERT OR UPDATE OR DELETE ON miner_state_snapshots
 FOR EACH ROW EXECUTE FUNCTION record_hosted_account_history_sample('miner_state');
CREATE TRIGGER record_hosted_account_history AFTER INSERT OR UPDATE OR DELETE ON container_module_metrics
 FOR EACH ROW EXECUTE FUNCTION record_hosted_account_history_sample('module_metrics');
CREATE TRIGGER record_hosted_account_history AFTER INSERT OR UPDATE OR DELETE ON container_module_diagnostic_observation
 FOR EACH ROW EXECUTE FUNCTION record_hosted_account_history_sample('module_state');

-- fleet-migration: 001045_shared_pool_configurations.up.sql
-- Configuration ownership is immutable. NULL account_id denotes the operator
-- organization; private migrations enforce hosted account references.
CREATE TABLE pool_configuration (
    configuration_id VARCHAR(36) PRIMARY KEY,
    organization_id BIGINT NOT NULL REFERENCES organization(id),
    account_id BIGINT,
    name VARCHAR(255) NOT NULL CHECK (length(btrim(name)) > 0),
    archived BOOLEAN NOT NULL DEFAULT FALSE,
    version BIGINT NOT NULL DEFAULT 1 CHECK (version > 0),
    assignment_version BIGINT NOT NULL DEFAULT 0 CHECK (assignment_version >= 0),
    current_revision_id VARCHAR(36),
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT uq_pool_configuration_org_configuration UNIQUE (organization_id, configuration_id)
);
CREATE INDEX idx_pool_configuration_scope
    ON pool_configuration(organization_id, account_id, configuration_id);

CREATE FUNCTION valid_pool_configuration_slots(slots JSONB) RETURNS BOOLEAN
LANGUAGE plpgsql IMMUTABLE AS $$
DECLARE slot JSONB;
BEGIN
    IF jsonb_typeof(slots) IS DISTINCT FROM 'array' THEN RETURN FALSE; END IF;
    IF jsonb_array_length(slots) NOT BETWEEN 1 AND 3 THEN RETURN FALSE; END IF;
    FOR slot IN SELECT value FROM jsonb_array_elements(slots) LOOP
        IF jsonb_typeof(slot) IS DISTINCT FROM 'object'
           OR NOT slot ?& ARRAY['id','url','username','worker_template','password_ciphertext','has_password']
           OR (SELECT count(*) FROM jsonb_object_keys(slot)) <> 6
           OR jsonb_typeof(slot->'id') IS DISTINCT FROM 'string'
           OR length(slot->>'id') = 0
           OR jsonb_typeof(slot->'url') IS DISTINCT FROM 'string'
           OR jsonb_typeof(slot->'username') IS DISTINCT FROM 'string'
           OR jsonb_typeof(slot->'worker_template') IS DISTINCT FROM 'string'
           OR jsonb_typeof(slot->'password_ciphertext') IS DISTINCT FROM 'string'
           OR length(slot->>'password_ciphertext') = 0
           OR jsonb_typeof(slot->'has_password') IS DISTINCT FROM 'boolean'
        THEN RETURN FALSE; END IF;
    END LOOP;
    RETURN (SELECT count(DISTINCT value->>'id') FROM jsonb_array_elements(slots)) = jsonb_array_length(slots);
END;
$$;

CREATE TABLE pool_configuration_revision (
    revision_id VARCHAR(36) PRIMARY KEY,
    organization_id BIGINT NOT NULL,
    configuration_id VARCHAR(36) NOT NULL,
    number BIGINT NOT NULL CHECK (number > 0),
    slots JSONB NOT NULL CHECK (valid_pool_configuration_slots(slots)),
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT uq_pool_configuration_revision_configuration_number UNIQUE (configuration_id, number),
    CONSTRAINT uq_pool_configuration_revision_org_configuration_revision UNIQUE (organization_id, configuration_id, revision_id),
    CONSTRAINT fk_pool_configuration_revision_configuration FOREIGN KEY (organization_id, configuration_id)
        REFERENCES pool_configuration(organization_id, configuration_id)
);
ALTER TABLE pool_configuration ADD CONSTRAINT fk_pool_configuration_current_revision
    FOREIGN KEY (organization_id, configuration_id, current_revision_id)
    REFERENCES pool_configuration_revision(organization_id, configuration_id, revision_id);

CREATE FUNCTION keep_pool_configuration_revision_immutable() RETURNS TRIGGER
LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'pool configuration revisions are immutable' USING ERRCODE = '23514';
END;
$$;
CREATE TRIGGER pool_configuration_revision_immutable
    BEFORE UPDATE OR DELETE ON pool_configuration_revision
    FOR EACH ROW EXECUTE FUNCTION keep_pool_configuration_revision_immutable();

CREATE FUNCTION keep_pool_configuration_owner_immutable() RETURNS TRIGGER
LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.organization_id IS DISTINCT FROM OLD.organization_id
       OR NEW.account_id IS DISTINCT FROM OLD.account_id THEN
        RAISE EXCEPTION 'pool configuration ownership is immutable' USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER pool_configuration_owner_immutable
    BEFORE UPDATE ON pool_configuration
    FOR EACH ROW EXECUTE FUNCTION keep_pool_configuration_owner_immutable();

-- Source identities make consolidation repeatable without storing a digest of
-- credentials. Old IDs remain resolvable after redundant library rows retire.
CREATE TABLE pool_configuration_import (
    organization_id BIGINT NOT NULL,
    source_kind TEXT NOT NULL CHECK (source_kind IN ('operator_pool', 'hosted_revision', 'device_readback')),
    source_id TEXT NOT NULL CHECK (source_id <> ''),
    source_version TEXT NOT NULL CHECK (source_version <> ''),
    configuration_id VARCHAR(36) NOT NULL,
    revision_id VARCHAR(36) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (organization_id, source_kind, source_id, source_version),
    CONSTRAINT fk_pool_configuration_import_revision FOREIGN KEY (organization_id, configuration_id, revision_id)
        REFERENCES pool_configuration_revision(organization_id, configuration_id, revision_id)
);

-- Assignments are ongoing intent. Operations retain the exact revision approved
-- for each update, even after the equipment switches to another configuration.
CREATE TABLE pool_configuration_operation (
    operation_id VARCHAR(36) PRIMARY KEY,
    organization_id BIGINT NOT NULL,
    configuration_id VARCHAR(36) NOT NULL,
    revision_id VARCHAR(36) NOT NULL,
    actor_id BIGINT NOT NULL CHECK (actor_id > 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT uq_pool_configuration_operation_org_operation UNIQUE (organization_id, operation_id),
    CONSTRAINT uq_pool_configuration_operation_revision_operation UNIQUE (organization_id, configuration_id, revision_id, operation_id),
    CONSTRAINT fk_pool_configuration_operation_revision FOREIGN KEY (organization_id, configuration_id, revision_id)
        REFERENCES pool_configuration_revision(organization_id, configuration_id, revision_id)
);
CREATE TRIGGER pool_configuration_operation_immutable
    BEFORE UPDATE OR DELETE ON pool_configuration_operation
    FOR EACH ROW EXECUTE FUNCTION keep_pool_configuration_revision_immutable();

CREATE TABLE pool_configuration_assignment (
    organization_id BIGINT NOT NULL,
    target_kind TEXT NOT NULL CHECK (target_kind IN ('device','hashboard_installation')),
    target_id BIGINT NOT NULL CHECK (target_id > 0),
    device_id BIGINT NOT NULL,
    configuration_id VARCHAR(36) NOT NULL,
    revision_id VARCHAR(36) NOT NULL,
    version BIGINT NOT NULL CHECK (version > 0),
    operation_id VARCHAR(36) NOT NULL,
    retired_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (organization_id, target_kind, target_id),
    CHECK (target_kind <> 'device' OR target_id = device_id),
    CONSTRAINT fk_pool_configuration_assignment_device FOREIGN KEY (device_id, organization_id) REFERENCES device(id, org_id),
    CONSTRAINT fk_pool_configuration_assignment_revision FOREIGN KEY (organization_id, configuration_id, revision_id)
        REFERENCES pool_configuration_revision(organization_id, configuration_id, revision_id),
    CONSTRAINT fk_pool_configuration_assignment_operation FOREIGN KEY (organization_id, configuration_id, revision_id, operation_id)
        REFERENCES pool_configuration_operation(organization_id, configuration_id, revision_id, operation_id)
);
CREATE INDEX idx_pool_configuration_assignment_configuration
    ON pool_configuration_assignment(organization_id, configuration_id, target_kind, target_id) WHERE retired_at IS NULL;
CREATE INDEX idx_pool_configuration_assignment_device
    ON pool_configuration_assignment(organization_id, device_id) WHERE retired_at IS NULL;

CREATE FUNCTION advance_pool_configuration_assignments() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'UPDATE' AND (NEW.organization_id, NEW.target_kind, NEW.target_id, NEW.device_id)
       IS DISTINCT FROM (OLD.organization_id, OLD.target_kind, OLD.target_id, OLD.device_id) THEN
        RAISE EXCEPTION 'pool assignment identity is immutable' USING ERRCODE = '23514';
    END IF;
    -- Retain tombstones so an old review cannot mistake a retired/reassigned
    -- target for equipment that has never had an assignment.
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'retire pool assignments instead of deleting them' USING ERRCODE = '23514';
    END IF;
    IF TG_OP = 'UPDATE' AND NEW.version <> OLD.version + 1 THEN
        RAISE EXCEPTION 'pool assignment version must advance' USING ERRCODE = '23514';
    END IF;
    IF NEW.retired_at IS NULL AND EXISTS (
        SELECT 1 FROM pool_configuration_assignment a
        WHERE a.organization_id = NEW.organization_id AND a.device_id = NEW.device_id
          AND a.target_kind <> NEW.target_kind AND a.retired_at IS NULL
    ) THEN
        RAISE EXCEPTION 'retire whole-device intent before assigning hashboards or vice versa' USING ERRCODE = '23514';
    END IF;
    UPDATE pool_configuration SET assignment_version = assignment_version + 1
    WHERE organization_id = NEW.organization_id AND configuration_id = NEW.configuration_id;
    IF TG_OP = 'UPDATE' AND OLD.configuration_id <> NEW.configuration_id THEN
        UPDATE pool_configuration SET assignment_version = assignment_version + 1
        WHERE organization_id = OLD.organization_id AND configuration_id = OLD.configuration_id;
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER pool_configuration_assignment_version
    BEFORE INSERT OR UPDATE OR DELETE ON pool_configuration_assignment
    FOR EACH ROW EXECUTE FUNCTION advance_pool_configuration_assignments();

CREATE TABLE pool_configuration_operation_target (
    organization_id BIGINT NOT NULL,
    operation_id VARCHAR(36) NOT NULL,
    target_kind TEXT NOT NULL,
    target_id BIGINT NOT NULL,
    assignment_version BIGINT NOT NULL CHECK (assignment_version > 0),
    state TEXT NOT NULL DEFAULT 'pending' CHECK (state IN ('pending','applying','applied_unverified','verified','failed','different','superseded')),
    attempts INT NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    result_code TEXT NOT NULL DEFAULT '',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (operation_id, target_kind, target_id),
    CONSTRAINT fk_pool_configuration_operation_target_operation FOREIGN KEY (organization_id, operation_id)
        REFERENCES pool_configuration_operation(organization_id, operation_id),
    CONSTRAINT fk_pool_configuration_operation_target_assignment FOREIGN KEY (organization_id, target_kind, target_id)
        REFERENCES pool_configuration_assignment(organization_id, target_kind, target_id)
);
CREATE INDEX idx_pool_configuration_operation_target_pending
    ON pool_configuration_operation_target(updated_at, operation_id) WHERE state = 'pending';

-- Reviews freeze encrypted settings and the exact affected assignment set.
-- Confirmation only adds its durable result; retries reuse that result.
CREATE TABLE pool_configuration_review (
    review_id VARCHAR(36) PRIMARY KEY,
    kind TEXT NOT NULL CHECK (kind IN ('edit','assign')),
    organization_id BIGINT NOT NULL,
    configuration_id VARCHAR(36) NOT NULL,
    actor_id BIGINT NOT NULL CHECK (actor_id > 0),
    configuration_version BIGINT NOT NULL CHECK (configuration_version > 0),
    assignment_version BIGINT NOT NULL CHECK (assignment_version >= 0),
    name VARCHAR(255) NOT NULL,
    slots JSONB NOT NULL CHECK (valid_pool_configuration_slots(slots)),
    settings_changed BOOLEAN NOT NULL,
    targets JSONB NOT NULL CHECK (jsonb_typeof(targets) = 'array'),
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    expires_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp() + INTERVAL '15 minutes',
    saved_version BIGINT,
    saved_revision_id VARCHAR(36),
    operation_id VARCHAR(36),
    CHECK ((saved_version IS NULL) = (saved_revision_id IS NULL)),
    CHECK (operation_id IS NULL OR saved_version IS NOT NULL),
    CONSTRAINT fk_pool_configuration_review_configuration FOREIGN KEY (organization_id, configuration_id)
        REFERENCES pool_configuration(organization_id, configuration_id),
    CONSTRAINT fk_pool_configuration_review_saved_revision FOREIGN KEY (organization_id, configuration_id, saved_revision_id)
        REFERENCES pool_configuration_revision(organization_id, configuration_id, revision_id),
    CONSTRAINT fk_pool_configuration_review_operation FOREIGN KEY (organization_id, configuration_id, saved_revision_id, operation_id)
        REFERENCES pool_configuration_operation(organization_id, configuration_id, revision_id, operation_id)
);

CREATE FUNCTION keep_pool_configuration_review_immutable() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    IF (to_jsonb(NEW) - ARRAY['saved_version','saved_revision_id','operation_id'])
        IS DISTINCT FROM (to_jsonb(OLD) - ARRAY['saved_version','saved_revision_id','operation_id'])
        OR OLD.saved_version IS NOT NULL THEN
        RAISE EXCEPTION 'pool configuration reviews are immutable' USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER pool_configuration_review_immutable BEFORE UPDATE ON pool_configuration_review
    FOR EACH ROW EXECUTE FUNCTION keep_pool_configuration_review_immutable();

-- fleet-migration: 001046_internal_pool_configuration_constraints.up.sql
-- requires-shared: 1045
ALTER TABLE pool_configuration ADD CONSTRAINT fk_pool_configuration_hosted_owner
    FOREIGN KEY (organization_id, account_id)
    REFERENCES hosted_account(organization_id, id);

-- Private topology adds installation validation to the shared assignment store.
CREATE FUNCTION validate_pool_assignment_installation() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.target_kind = 'hashboard_installation' AND NEW.retired_at IS NULL AND NOT EXISTS (
        SELECT 1 FROM hashboard_installation i
        JOIN hashboard h ON h.id = i.hashboard_id
        JOIN container_module m ON m.id = i.module_id
        JOIN container_tank t ON t.id = m.tank_id
        WHERE i.id = NEW.target_id AND h.org_id = NEW.organization_id
          AND t.device_id = NEW.device_id AND i.device_id = NEW.device_id AND i.removed_at IS NULL
          AND m.retired_at IS NULL AND t.present
    ) THEN
        RAISE EXCEPTION 'pool assignment requires the current hashboard installation' USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER pool_configuration_assignment_installation
    BEFORE INSERT OR UPDATE ON pool_configuration_assignment
    FOR EACH ROW EXECUTE FUNCTION validate_pool_assignment_installation();

-- fleet-migration: 001047_internal_pool_operation_bindings.up.sql
-- requires-shared: 1045
-- The asynchronous executor must use the physical identity, ownership and worker
-- inputs that were reviewed, rather than reconstructing intent from new values.
-- Empty bindings belong to imported/internal records and cannot authorize I/O.
ALTER TABLE pool_configuration_operation_target ADD COLUMN reviewed_binding TEXT NOT NULL DEFAULT '';

CREATE FUNCTION keep_pool_operation_target_identity() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    IF (NEW.organization_id,NEW.operation_id,NEW.target_kind,NEW.target_id,NEW.assignment_version,NEW.reviewed_binding)
       IS DISTINCT FROM
       (OLD.organization_id,OLD.operation_id,OLD.target_kind,OLD.target_id,OLD.assignment_version,OLD.reviewed_binding) THEN
        RAISE EXCEPTION 'pool operation target identity is immutable' USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER pool_operation_target_identity BEFORE UPDATE ON pool_configuration_operation_target
    FOR EACH ROW EXECUTE FUNCTION keep_pool_operation_target_identity();

-- Bind each claimed whole-device update to its separately durable physical
-- admission. Recovery can then distinguish settled work from unresolved I/O.
ALTER TABLE pool_configuration_operation_target ADD COLUMN physical_attempt_id UUID
    CONSTRAINT fk_pool_configuration_operation_target_attempt REFERENCES device_write_attempt(attempt_id);
CREATE UNIQUE INDEX idx_pool_configuration_operation_target_attempt
    ON pool_configuration_operation_target(physical_attempt_id) WHERE physical_attempt_id IS NOT NULL;

-- Keep periodic recovery proportional to unfinished work, not retained history.
CREATE INDEX idx_pool_configuration_operation_target_recovery
    ON pool_configuration_operation_target(physical_attempt_id)
    WHERE state = 'applying' OR (state = 'failed' AND result_code = 'write_uncertain');
-- Reallocation and supersession find unfinished history for one assignment.
CREATE INDEX idx_pool_configuration_operation_target_open
    ON pool_configuration_operation_target(organization_id, target_kind, target_id)
    WHERE state IN ('pending','applying','failed','different','applied_unverified');

CREATE FUNCTION validate_pool_update_attempt() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'UPDATE' AND OLD.physical_attempt_id IS NOT NULL AND
        NEW.physical_attempt_id IS DISTINCT FROM OLD.physical_attempt_id THEN
        RAISE EXCEPTION 'pool update attempt is immutable' USING ERRCODE = '23514';
    END IF;
    IF NEW.physical_attempt_id IS NOT NULL AND (TG_OP = 'INSERT' OR OLD.physical_attempt_id IS NULL) AND NOT EXISTS (
        SELECT 1 FROM device_write_attempt w
        JOIN pool_configuration_assignment a ON a.organization_id = NEW.organization_id
            AND a.target_kind = NEW.target_kind AND a.target_id = NEW.target_id
        WHERE w.attempt_id = NEW.physical_attempt_id AND w.organization_id = a.organization_id
            AND w.device_id = a.device_id AND w.operation_kind = 'mutation' AND w.status = 'running'
            AND a.operation_id = NEW.operation_id AND a.version = NEW.assignment_version AND a.retired_at IS NULL
            AND NEW.state = 'applying'
    ) THEN
        RAISE EXCEPTION 'pool update requires its current physical admission' USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER pool_update_attempt BEFORE INSERT OR UPDATE ON pool_configuration_operation_target
    FOR EACH ROW EXECUTE FUNCTION validate_pool_update_attempt();

-- fleet-migration: 001048_internal_client_ticketing.up.sql
-- requires-shared: 1000
ALTER TABLE repair_ticket_part DROP CONSTRAINT uq_ticket_inventory_part;
CREATE UNIQUE INDEX idx_repair_ticket_part_active_inventory_part ON repair_ticket_part(ticket_id,inventory_part_id) WHERE consumed_at IS NULL;

-- Existing accounts require explicit opt-in; accounts created after this migration
-- retain the default-enabled onboarding workflow.
ALTER TABLE hosted_account ADD COLUMN ticketing_enabled BOOLEAN NOT NULL DEFAULT FALSE;
-- The host root is an ancestor gate, not a client account administrators can enable.
UPDATE hosted_account SET ticketing_enabled=TRUE WHERE depth=0;
ALTER TABLE hosted_account ALTER COLUMN ticketing_enabled SET DEFAULT TRUE;
ALTER TABLE container_module ADD COLUMN model TEXT NOT NULL DEFAULT '';

ALTER TABLE repair_ticket DROP CONSTRAINT ck_repair_ticket_category;
ALTER TABLE repair_ticket ADD CONSTRAINT ck_repair_ticket_category CHECK (category BETWEEN 1 AND 5);
ALTER TABLE repair_ticket ADD COLUMN description TEXT NOT NULL DEFAULT '';
ALTER TABLE repair_ticket ADD COLUMN created_by_account_id BIGINT;
ALTER TABLE repair_ticket ADD COLUMN shared_with_clients BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE repair_ticket ADD CONSTRAINT fk_repair_ticket_creator_account FOREIGN KEY (org_id,created_by_account_id) REFERENCES hosted_account(organization_id,id);

CREATE TABLE repair_ticket_audience (
 org_id BIGINT NOT NULL,
 ticket_id BIGINT NOT NULL,
 account_id BIGINT NOT NULL,
 PRIMARY KEY (org_id,ticket_id,account_id),
 FOREIGN KEY (ticket_id,org_id) REFERENCES repair_ticket(id,org_id) ON DELETE CASCADE,
 FOREIGN KEY (org_id,account_id) REFERENCES hosted_account(organization_id,id)
);
CREATE INDEX idx_repair_ticket_audience_account ON repair_ticket_audience(org_id,account_id,ticket_id);

-- Each row is the physical identity and installation at opening, not a live
-- join through the equipment's next owner or the next board occupying a slot.
CREATE TABLE repair_ticket_target (
 org_id BIGINT NOT NULL,
 ticket_id BIGINT NOT NULL,
 kind SMALLINT NOT NULL CHECK (kind IN (1,3,4)),
 asset_id BIGINT NOT NULL CHECK (asset_id>0),
 miner_id BIGINT REFERENCES device(id),
 miner_identifier TEXT,
 building_id BIGINT,
 rack_id BIGINT,
 module_id BIGINT REFERENCES container_module(id),
 hashboard_id BIGINT REFERENCES hashboard(id),
 installation_id BIGINT REFERENCES hashboard_installation(id),
 container_id BIGINT REFERENCES device(id),
 site_id BIGINT,
 label TEXT NOT NULL,
 model TEXT NOT NULL DEFAULT '',
 position INTEGER,
 client_id BIGINT,
 subclient_id BIGINT,
 diagnosis TEXT NOT NULL DEFAULT '',
 warranty_evidence TEXT NOT NULL DEFAULT '',
 repair_outcome TEXT NOT NULL DEFAULT '',
 rma_outcome TEXT NOT NULL DEFAULT '',
 replacement_module_id BIGINT REFERENCES container_module(id),
 replacement_hashboard_id BIGINT REFERENCES hashboard(id),
 PRIMARY KEY(org_id,ticket_id,kind,asset_id),
 FOREIGN KEY(ticket_id,org_id) REFERENCES repair_ticket(id,org_id) ON DELETE CASCADE,
 FOREIGN KEY(org_id,client_id) REFERENCES hosted_account(organization_id,id),
 FOREIGN KEY(org_id,subclient_id) REFERENCES hosted_account(organization_id,id),
 CHECK ((kind=1 AND miner_id IS NOT NULL AND miner_id=asset_id AND module_id IS NULL AND hashboard_id IS NULL) OR
        (kind=3 AND module_id IS NOT NULL AND module_id=asset_id AND miner_id IS NULL AND hashboard_id IS NULL) OR
        (kind=4 AND hashboard_id IS NOT NULL AND hashboard_id=asset_id AND module_id IS NOT NULL AND installation_id IS NOT NULL AND miner_id IS NULL))
);

ALTER TABLE repair_ticket_comment ADD COLUMN internal BOOLEAN NOT NULL DEFAULT TRUE;
CREATE INDEX idx_repair_ticket_comment_client_page ON repair_ticket_comment(org_id,ticket_id,created_at DESC,id DESC) WHERE deleted_at IS NULL AND NOT internal;
ALTER TABLE repair_ticket_comment ADD COLUMN account_id BIGINT;
ALTER TABLE repair_ticket_comment ADD CONSTRAINT fk_ticket_comment_audience FOREIGN KEY(org_id,ticket_id,account_id) REFERENCES repair_ticket_audience(org_id,ticket_id,account_id);
ALTER TABLE repair_ticket_comment ADD CONSTRAINT ck_ticket_comment_internal_scope CHECK (NOT internal OR account_id IS NULL);

CREATE VIEW repair_ticket_asset_option AS
SELECT 1::smallint AS kind,d.id,d.org_id,COALESCE(d.site_id,dsr.site_id) AS site_id,d.id AS parent_asset_id,5::int AS allocation_kind,
 NULL::bigint AS module_id,NULL::bigint AS installation_id,NULL::bigint AS container_id,NULL::int AS position,
 COALESCE(NULLIF(d.custom_name,''),d.device_identifier)::text AS label,COALESCE(dd.model,'')::text AS model,
 d.device_identifier::text AS miner_identifier,COALESCE(d.building_id,dsr.building_id) AS building_id,rack.id AS rack_id
FROM device d JOIN discovered_device dd ON dd.id=d.discovered_device_id AND dd.org_id=d.org_id
LEFT JOIN device_set_membership rm ON rm.device_id=d.id AND rm.org_id=d.org_id AND rm.device_set_type='rack'
LEFT JOIN device_set rack ON rack.id=rm.device_set_id AND rack.org_id=d.org_id AND rack.deleted_at IS NULL
LEFT JOIN device_set_rack dsr ON dsr.device_set_id=rack.id AND dsr.org_id=d.org_id
WHERE d.deleted_at IS NULL AND dd.deleted_at IS NULL AND dd.device_class<>2
UNION ALL
SELECT 3::smallint,m.id,d.org_id,d.site_id,m.id,6,m.id,NULL::bigint,d.id,m.position,
 ('Module '||m.id||CASE WHEN m.reported_id<>'' THEN ' / '||m.reported_id ELSE '' END)::text,m.model,''::text,NULL::bigint,NULL::bigint
FROM container_module m JOIN container_tank t ON t.id=m.tank_id JOIN device d ON d.id=t.device_id
WHERE m.retired_at IS NULL AND d.deleted_at IS NULL
UNION ALL
SELECT 4::smallint,b.id,b.org_id,d.site_id,m.id,6,m.id,i.id,d.id,i.position,
 ('Hashboard '||b.id||' / Module '||m.id||' / slot '||i.position)::text,m.model,''::text,NULL::bigint,NULL::bigint
FROM hashboard b JOIN hashboard_installation i ON i.hashboard_id=b.id AND i.removed_at IS NULL
JOIN container_module m ON m.id=i.module_id JOIN container_tank t ON t.id=m.tank_id JOIN device d ON d.id=t.device_id AND d.org_id=b.org_id
WHERE m.retired_at IS NULL AND d.deleted_at IS NULL;

ALTER TABLE repair_ticket ADD COLUMN resolution_note TEXT NOT NULL DEFAULT '';
ALTER TABLE repair_ticket ADD COLUMN resolution_note_account_id BIGINT;
ALTER TABLE repair_ticket ADD CONSTRAINT fk_ticket_resolution_note_account FOREIGN KEY(org_id,resolution_note_account_id) REFERENCES hosted_account(organization_id,id);

-- fleet-migration: 001049_internal_hashboard_allocations.up.sql
-- requires-shared: 1000
-- Capture retained board history before writers resume after this upgrade.
LOCK TABLE hashboard_metrics,hashboard_diagnostic_observation IN SHARE ROW EXCLUSIVE MODE;
-- Keep recorded module history, but allocate stable board identities from now on.
LOCK TABLE device, hosted_account_asset, container_tank, container_module, hashboard_slot, hashboard_installation,
 hashboard_observation, container_inventory IN SHARE ROW EXCLUSIVE MODE;

CREATE VIEW hosted_module_membership AS
SELECT m.id AS module_id, t.device_id, d.org_id,
 GREATEST(max(i.installed_at),m.attribution_since,
  (SELECT max(prior.removed_at) FROM hashboard_installation prior WHERE prior.module_id=m.id)) AS membership_since,
 array_agg(s.hashboard_id ORDER BY s.hashboard_id) FILTER (WHERE s.occupancy='occupied') AS board_ids
FROM container_module m
JOIN container_tank t ON t.id=m.tank_id AND t.present
JOIN device d ON d.id=t.device_id AND d.deleted_at IS NULL
JOIN container_inventory inventory ON inventory.device_id=d.id
JOIN hashboard_slot s ON s.module_id=m.id
LEFT JOIN hashboard_installation i ON i.hashboard_id=s.hashboard_id
 AND i.module_id=m.id AND i.position=s.position AND i.removed_at IS NULL
LEFT JOIN hashboard_observation o ON o.hashboard_id=s.hashboard_id
WHERE m.retired_at IS NULL AND m.last_seen_at=inventory.observed_at
GROUP BY m.id,t.device_id,d.org_id,inventory.observed_at,d.site_id
HAVING count(*)=3 AND count(*) FILTER (WHERE s.occupancy='occupied')>0
 AND count(DISTINCT s.hashboard_id)=count(*) FILTER (WHERE s.occupancy='occupied')
 AND bool_and(COALESCE(s.observed_at=inventory.observed_at AND s.unavailable_reason=''
  AND ((s.occupancy='empty' AND s.hashboard_id IS NULL)
   OR (s.occupancy='occupied' AND s.hashboard_id IS NOT NULL AND i.id IS NOT NULL
    AND i.device_id=t.device_id AND i.site_id IS NOT DISTINCT FROM d.site_id
    AND o.device_id=t.device_id AND o.site_id IS NOT DISTINCT FROM d.site_id
    AND o.released_at IS NULL)),false));

DO $$ BEGIN
 IF EXISTS (SELECT 1 FROM hosted_account_asset a LEFT JOIN hosted_module_membership m ON m.module_id=a.asset_id
  WHERE a.kind=6 AND a.client_id IS NOT NULL AND m.module_id IS NULL) THEN
  RAISE EXCEPTION 'cannot migrate allocated modules with unresolved board membership'
   USING HINT='Restore a complete, confirmed inventory for allocated modules, or recall their allocations before retrying. No ownership has been changed.';
 END IF;
END $$;

ALTER TABLE hosted_account_asset ADD COLUMN hashboard_id bigint REFERENCES hashboard(id);
ALTER TABLE hosted_account_asset DROP CONSTRAINT hosted_account_asset_kind_check;
ALTER TABLE hosted_account_asset DROP CONSTRAINT hosted_account_asset_check;
ALTER TABLE hosted_account_asset ADD CONSTRAINT ck_hosted_account_asset_identity CHECK (
 (kind=5 AND miner_id=asset_id AND miner_id IS NOT NULL AND module_id IS NULL AND hashboard_id IS NULL AND device_id=asset_id) OR
 (kind=6 AND module_id=asset_id AND module_id IS NOT NULL AND miner_id IS NULL AND hashboard_id IS NULL) OR
 (kind=7 AND hashboard_id=asset_id AND hashboard_id IS NOT NULL AND miner_id IS NULL AND module_id IS NULL));
ALTER TABLE hosted_account_asset_interval DROP CONSTRAINT hosted_account_asset_interval_kind_check;
ALTER TABLE hosted_account_asset_interval ADD CONSTRAINT ck_hosted_account_asset_interval_kind CHECK (kind IN (5,6,7));

CREATE OR REPLACE FUNCTION check_hosted_account_asset() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.kind=7 AND NOT EXISTS (SELECT 1 FROM hashboard b WHERE b.id=NEW.hashboard_id AND b.org_id=NEW.organization_id) THEN
  RAISE EXCEPTION 'hashboard organization mismatch' USING ERRCODE='23514';
 END IF;
 IF NEW.client_id IS NOT NULL AND NOT EXISTS (SELECT 1 FROM hosted_account a WHERE a.id=NEW.client_id AND a.organization_id=NEW.organization_id AND a.depth=1) THEN
  RAISE EXCEPTION 'allocation requires customer' USING ERRCODE='23514';
 END IF;
 IF NEW.subclient_id IS NOT NULL AND NOT EXISTS (SELECT 1 FROM hosted_account a WHERE a.id=NEW.subclient_id AND a.organization_id=NEW.organization_id AND a.parent_id=NEW.client_id AND a.depth=2) THEN
  RAISE EXCEPTION 'allocation requires direct subclient' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;

-- One timestamp closes the legacy period and starts the board periods. Existing
-- module history is retained as module history, never backfilled onto boards.
CREATE TEMP TABLE allocation_transition ON COMMIT DROP AS SELECT clock_timestamp() AS at;
ALTER TABLE hosted_account_asset DISABLE TRIGGER hosted_account_asset_history;
INSERT INTO hosted_account_asset(organization_id,kind,asset_id,device_id,hashboard_id,client_id,subclient_id,revision)
SELECT a.organization_id,7,b.id,m.device_id,b.id,a.client_id,a.subclient_id,a.revision
FROM hosted_account_asset a JOIN hosted_module_membership m ON m.module_id=a.asset_id
CROSS JOIN LATERAL unnest(m.board_ids) b(id) WHERE a.kind=6 AND a.client_id IS NOT NULL;
UPDATE hosted_account_asset_interval SET ended_at=(SELECT at FROM allocation_transition) WHERE kind=6 AND ended_at IS NULL;
INSERT INTO hosted_account_asset_interval(organization_id,kind,asset_id,device_id,site_id,device_identifier,client_id,subclient_id,started_at)
SELECT a.organization_id,7,a.asset_id,a.device_id,d.site_id,d.device_identifier,a.client_id,a.subclient_id,tr.at
FROM hosted_account_asset a JOIN device d ON d.id=a.device_id CROSS JOIN allocation_transition tr WHERE a.kind=7;
DELETE FROM hosted_account_asset WHERE kind=6;
ALTER TABLE hosted_account_asset ENABLE TRIGGER hosted_account_asset_history;
ALTER TABLE hosted_account_asset ADD CONSTRAINT ck_hosted_account_asset_kind CHECK (kind IN (5,7));

CREATE OR REPLACE FUNCTION record_hosted_account_asset_interval() RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE boundary timestamptz := COALESCE(NULLIF(current_setting('fleet.allocation_boundary',true),'' )::timestamptz,clock_timestamp());
BEGIN
 IF TG_OP='UPDATE' AND NEW.client_id IS NOT DISTINCT FROM OLD.client_id AND NEW.subclient_id IS NOT DISTINCT FROM OLD.subclient_id THEN
  IF NEW.device_id=OLD.device_id THEN RETURN NEW; END IF;
  IF NEW.kind=7 THEN
   -- Placement follows the accepted inventory time. A delayed claim must not
   -- extend ownership backwards or overlap a period closed by controller deletion.
   SELECT GREATEST(o.last_seen_at,(SELECT COALESCE(i.ended_at,i.started_at) FROM hosted_account_asset_interval i
    WHERE i.organization_id=NEW.organization_id AND i.kind=NEW.kind AND i.asset_id=NEW.asset_id ORDER BY i.id DESC LIMIT 1))
   INTO boundary FROM hashboard_observation o WHERE o.hashboard_id=NEW.hashboard_id AND o.device_id=NEW.device_id;
  END IF;
 END IF;
 UPDATE hosted_account_asset_interval SET ended_at=boundary
 WHERE organization_id=NEW.organization_id AND kind=NEW.kind AND asset_id=NEW.asset_id AND ended_at IS NULL;
 IF NEW.client_id IS NOT NULL THEN
  INSERT INTO hosted_account_asset_interval(organization_id,kind,asset_id,device_id,client_id,subclient_id,started_at,site_id,device_identifier)
  SELECT NEW.organization_id,NEW.kind,NEW.asset_id,NEW.device_id,NEW.client_id,NEW.subclient_id,boundary,d.site_id,d.device_identifier FROM device d WHERE d.id=NEW.device_id AND d.deleted_at IS NULL;
 END IF;
 RETURN NEW;
END $$;

-- Moving hardware changes placement, not ownership. Released or absent boards
-- retain their allocation and last known controller until a new claim arrives.
CREATE FUNCTION move_hosted_hashboard_allocation() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
 UPDATE hosted_account_asset SET device_id=NEW.device_id,revision=revision+1,updated_at=clock_timestamp()
 WHERE kind=7 AND asset_id=NEW.hashboard_id AND device_id<>NEW.device_id;
 RETURN NEW;
END $$;
CREATE TRIGGER hosted_hashboard_placement AFTER INSERT OR UPDATE OF device_id ON hashboard_observation
 FOR EACH ROW EXECUTE FUNCTION move_hosted_hashboard_allocation();

-- Module entries are projections, never independently writable allocations.
-- -1 marks mixed ownership and prevents a group from being assigned as unowned.
CREATE VIEW hosted_account_asset_projection AS
SELECT organization_id,kind,asset_id,device_id,client_id,subclient_id,revision,
 '[]'::jsonb AS boards FROM hosted_account_asset
UNION ALL
SELECT m.org_id,6,m.module_id,m.device_id,
 CASE WHEN min(COALESCE(a.client_id,0))=max(COALESCE(a.client_id,0)) THEN NULLIF(min(COALESCE(a.client_id,0)),0) ELSE -1 END,
 CASE WHEN min(COALESCE(a.subclient_id,0))=max(COALESCE(a.subclient_id,0)) THEN NULLIF(min(COALESCE(a.subclient_id,0)),0) ELSE -1 END,
 COALESCE(max(a.revision),0)::bigint,
 jsonb_agg(jsonb_build_object('kind',7,'id',b.id,'expected_revision',COALESCE(a.revision,0)) ORDER BY b.id)
FROM hosted_module_membership m CROSS JOIN LATERAL unnest(m.board_ids) b(id)
LEFT JOIN hosted_account_asset a ON a.organization_id=m.org_id AND a.kind=7 AND a.asset_id=b.id
GROUP BY m.org_id,m.module_id,m.device_id;

CREATE VIEW hosted_mining_asset AS
SELECT f.* FROM fleet_mining_asset f WHERE f.kind=5 OR EXISTS (SELECT 1 FROM hosted_module_membership m WHERE m.module_id=f.id)
UNION ALL
SELECT 7::int,b.id,b.org_id,d.site_id,
 ('Hashboard '||COALESCE((SELECT value FROM hashboard_identity i WHERE i.hashboard_id=b.id ORDER BY (kind='serial') DESC,value LIMIT 1),b.id::text))::text,
 d.device_identifier::text,0::int,''::text,2::int,d.id,6::int,i.module_id,
 CASE WHEN slot.observed_at>now()-INTERVAL '2 minutes' AND slot.observed_at<=now()+INTERVAL '5 seconds'
   AND slot.state_observed_at>now()-INTERVAL '2 minutes' AND slot.state_observed_at<=now()+INTERVAL '5 seconds'
   AND (o.attribution_since IS NULL OR slot.state_observed_at>=o.attribution_since)
   AND (slot.state_observed_at>=i.installed_at OR NOT EXISTS (
    SELECT 1 FROM hashboard_installation prior WHERE prior.hashboard_id=b.id AND (prior.installed_at,prior.id)<(i.installed_at,i.id)))
   THEN slot.state ELSE 'unknown' END::text,
 h.time,h.hashrate_hs,h.power_w,h.temperature_c,(h.efficiency_jh*1e12)::float8
FROM hashboard b JOIN hashboard_observation o ON o.hashboard_id=b.id
JOIN device d ON d.id=o.device_id AND d.org_id=b.org_id AND d.deleted_at IS NULL
JOIN discovered_device dd ON dd.id=d.discovered_device_id AND dd.deleted_at IS NULL
LEFT JOIN hashboard_installation i ON i.hashboard_id=b.id AND i.removed_at IS NULL
 AND i.device_id=d.id AND i.site_id IS NOT DISTINCT FROM d.site_id
LEFT JOIN hashboard_slot slot ON slot.module_id=i.module_id AND slot.position=i.position
 AND slot.hashboard_id=b.id AND slot.occupancy='occupied' AND slot.unavailable_reason=''
LEFT JOIN hashboard_latest_metrics h ON h.hashboard_id=b.id AND h.installation_id=i.id
 AND h.time>now()-INTERVAL '2 minutes' AND h.time<=now()+INTERVAL '5 seconds'
 AND i.site_id IS NOT DISTINCT FROM d.site_id
 AND (o.attribution_since IS NULL OR h.time>=o.attribution_since)
WHERE o.released_at IS NULL AND o.site_id IS NOT DISTINCT FROM d.site_id;

CREATE OR REPLACE FUNCTION hosted_account_source_authorized(
 organization bigint, asset_kind integer, asset bigint, client bigint, subclient bigint,
 sample_site bigint, identifier text, oldest timestamptz, newest timestamptz
) RETURNS boolean LANGUAGE SQL STABLE AS $$
 SELECT COALESCE(oldest IS NOT NULL AND newest IS NOT NULL AND newest>=oldest AND (
  EXISTS (SELECT 1 FROM hosted_account_asset_interval i
   WHERE i.organization_id=organization AND i.kind=asset_kind AND i.asset_id=asset
    AND i.client_id=client AND (subclient=-1 OR COALESCE(i.subclient_id,0)=COALESCE(subclient,0))
    AND i.site_id IS NOT DISTINCT FROM sample_site AND i.device_identifier=identifier
    AND oldest>=i.started_at AND (i.ended_at IS NULL OR newest<i.ended_at))
  OR (asset_kind=6 AND EXISTS (
   SELECT 1 FROM hosted_module_membership m WHERE m.module_id=asset AND m.org_id=organization AND oldest>=m.membership_since
    AND NOT EXISTS (
     SELECT 1 FROM unnest(m.board_ids) b(id)
     WHERE NOT EXISTS (SELECT 1 FROM hashboard_installation p JOIN hosted_account_asset_interval i ON i.kind=7 AND i.asset_id=p.hashboard_id
      WHERE p.hashboard_id=b.id AND p.module_id=m.module_id AND p.removed_at IS NULL AND p.installed_at<=oldest
       AND i.organization_id=organization AND i.client_id=client
       AND (subclient=-1 OR COALESCE(i.subclient_id,0)=COALESCE(subclient,0))
       AND i.site_id IS NOT DISTINCT FROM sample_site AND i.device_identifier=identifier
       AND oldest>=i.started_at AND (i.ended_at IS NULL OR newest<i.ended_at))
    )))
 ),false);
$$;

-- Retained history uses the same installation and ownership evidence as raw
-- reads. Capture only new observations; never infer past board ownership.
CREATE INDEX idx_hosted_account_asset_interval_capture_hashboard
 ON hosted_account_asset_interval(asset_id,device_id,site_id,started_at) WHERE kind=7;
CREATE FUNCTION record_hosted_hashboard_history_sample() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
 source_number smallint := CASE TG_ARGV[0] WHEN 'metrics' THEN 5 ELSE 6 END;
 row_data jsonb;
 origin_at timestamptz;
 collected timestamptz;
 observed timestamptz;
 epoch timestamptz;
BEGIN
 IF TG_OP IN ('UPDATE','DELETE') THEN
  row_data := to_jsonb(OLD);
  origin_at := COALESCE((row_data->>'time')::timestamptz,OLD.collected_at);
  DELETE FROM hosted_account_history_sample
  WHERE time=ANY(ARRAY[origin_at,(row_data->>'state_observed_at')::timestamptz])
   AND source_id=source_number AND origin_key=OLD.hashboard_id::text
   AND origin_time=origin_at AND origin_collected_at=OLD.collected_at;
 END IF;
 IF TG_OP='DELETE' THEN RETURN OLD; END IF;
 row_data := to_jsonb(NEW);
 collected := NEW.collected_at;
 origin_at := COALESCE((row_data->>'time')::timestamptz,collected);
 observed := CASE WHEN source_number=5 THEN origin_at ELSE NEW.state_observed_at END;
 SELECT started_at INTO STRICT epoch FROM hosted_account_history_summary_epoch WHERE singleton;
 IF observed IS NULL OR observed<epoch THEN RETURN NEW; END IF;
 IF source_number=6 AND (observed<=collected-INTERVAL '2 minutes' OR observed>collected+INTERVAL '5 seconds') THEN
  RETURN NEW;
 END IF;
 INSERT INTO hosted_account_history_sample(time,interval_id,metric,value,complete,source_oldest_at,source_newest_at,
  source_id,origin_key,origin_time,origin_collected_at)
 SELECT observed,i.id,s.metric,
  CASE WHEN s.metric IN (1,2) THEN CASE WHEN s.value>=0 AND s.value<'Infinity'::float8 THEN s.value END
   ELSE CASE WHEN s.value>'-Infinity'::float8 AND s.value<'Infinity'::float8 THEN s.value END END,
  CASE WHEN s.metric IN (1,2) THEN COALESCE(
   (row_data->>'hashrate_hs')::float8>=0 AND (row_data->>'hashrate_hs')::float8<'Infinity'::float8
   AND (row_data->>'power_w')::float8>=0 AND (row_data->>'power_w')::float8<'Infinity'::float8,false) END,
  observed,observed,source_number,NEW.hashboard_id::text,origin_at,collected
 FROM hashboard_installation p
 JOIN hosted_account_asset_interval i ON i.kind=7 AND i.asset_id=p.hashboard_id
  AND i.device_id=p.device_id AND i.site_id IS NOT DISTINCT FROM p.site_id
  AND observed>=i.started_at AND (i.ended_at IS NULL OR observed<i.ended_at)
 CROSS JOIN LATERAL (
  SELECT metric::smallint,value FROM (VALUES
   (1,(row_data->>'hashrate_hs')::float8),(2,(row_data->>'power_w')::float8),
   (3,(row_data->>'temperature_c')::float8)
  ) metrics(metric,value) WHERE source_number=5
  UNION ALL SELECT 4::smallint,hosted_module_uptime_state(NEW.state)::float8 WHERE source_number=6
 ) s
 WHERE p.id=NEW.installation_id AND p.hashboard_id=NEW.hashboard_id
  AND (source_number=5 OR observed>=p.installed_at OR NOT EXISTS (
   SELECT 1 FROM hashboard_installation prior WHERE prior.hashboard_id=p.hashboard_id
    AND (prior.installed_at,prior.id)<(p.installed_at,p.id)))
 ON CONFLICT(time,source_id,origin_key,origin_time,origin_collected_at,metric,interval_id) DO UPDATE
 SET value=EXCLUDED.value,complete=EXCLUDED.complete,
  source_oldest_at=EXCLUDED.source_oldest_at,source_newest_at=EXCLUDED.source_newest_at;
 RETURN NEW;
END;
$$;
CREATE TRIGGER record_hosted_hashboard_history AFTER INSERT OR UPDATE OR DELETE ON hashboard_metrics
 FOR EACH ROW EXECUTE FUNCTION record_hosted_hashboard_history_sample('metrics');
CREATE TRIGGER record_hosted_hashboard_history AFTER INSERT OR UPDATE OR DELETE ON hashboard_diagnostic_observation
 FOR EACH ROW EXECUTE FUNCTION record_hosted_hashboard_history_sample('state');

-- fleet-migration: 001050_internal_automatic_hashboard_allocation.up.sql
-- requires-shared: 1000
CREATE INDEX idx_hashboard_installation_slot_history
 ON hashboard_installation(module_id,position,installed_at DESC,id DESC);

-- Called after the complete controller map has been reconciled. The shared
-- organization lock serializes this with manual allocation and other polls.
CREATE FUNCTION reconcile_hosted_hashboard_replacements(target_org bigint, controller bigint, observed timestamptz)
RETURNS bigint LANGUAGE plpgsql AS $$
DECLARE
 replacement record;
 previous_boundary text := COALESCE(current_setting('fleet.allocation_boundary',true),'');
 transferred bigint := 0;
BEGIN
 PERFORM id FROM organization WHERE id=target_org AND deleted_at IS NULL FOR NO KEY UPDATE;
 IF NOT FOUND THEN RETURN 0; END IF;

 FOR replacement IN
  SELECT a.asset_id AS old_board, current.hashboard_id AS new_board, current.module_id, current.position,
   a.client_id,a.subclient_id,root.account_id AS root_uuid,client.account_id AS client_uuid,
   COALESCE(child.account_id,client.account_id) AS owner_uuid
  FROM hashboard_installation current
  JOIN container_module module ON module.id=current.module_id AND module.retired_at IS NULL
   AND module.identity_confirmed AND module.last_seen_at=observed
  JOIN container_tank tank ON tank.id=module.tank_id AND tank.device_id=controller AND tank.present
  JOIN container_inventory inventory ON inventory.device_id=controller AND inventory.observed_at=observed
  JOIN device device ON device.id=controller AND device.org_id=target_org AND device.deleted_at IS NULL
   AND device.site_id IS NOT DISTINCT FROM current.site_id
  JOIN container_module_identity identity ON identity.module_id=module.id
   AND identity.org_id=target_org AND identity.reported_module_id=module.reported_id
  JOIN hashboard_slot slot ON slot.module_id=current.module_id AND slot.position=current.position
   AND slot.hashboard_id=current.hashboard_id AND slot.occupancy='occupied'
   AND slot.unavailable_reason='' AND slot.observed_at=observed
  JOIN hashboard_observation incoming ON incoming.hashboard_id=current.hashboard_id
   AND incoming.device_id=controller AND incoming.site_id IS NOT DISTINCT FROM current.site_id
   AND incoming.released_at IS NULL AND incoming.last_seen_at=observed
  CROSS JOIN LATERAL (
   SELECT p.* FROM hashboard_installation p
   WHERE p.module_id=current.module_id AND p.position=current.position
    AND (p.installed_at,p.id)<(current.installed_at,current.id)
   ORDER BY p.installed_at DESC,p.id DESC LIMIT 1
  ) prior
  JOIN hosted_account_asset a ON a.organization_id=target_org AND a.kind=7
   AND a.asset_id=prior.hashboard_id AND a.client_id IS NOT NULL AND a.device_id=prior.device_id
  JOIN hosted_account_asset_interval period ON period.organization_id=a.organization_id AND period.kind=7
   AND period.asset_id=a.asset_id AND period.ended_at IS NULL AND period.client_id=a.client_id
   AND period.subclient_id IS NOT DISTINCT FROM a.subclient_id AND period.started_at<=prior.removed_at
  JOIN hashboard_observation outgoing ON outgoing.hashboard_id=prior.hashboard_id
   AND outgoing.released_at IS NOT NULL
  JOIN hosted_account client ON client.id=a.client_id
  JOIN hosted_account root ON root.organization_id=target_org AND root.depth=0
  LEFT JOIN hosted_account child ON child.id=a.subclient_id
  LEFT JOIN hosted_account_asset destination ON destination.organization_id=target_org AND destination.kind=7
   AND destination.asset_id=current.hashboard_id
  WHERE current.device_id=controller AND current.removed_at IS NULL AND current.last_seen_at=observed
   AND prior.hashboard_id<>current.hashboard_id AND prior.removed_at<=current.installed_at
   -- Never take an allocated board, or override a manual decision made after
   -- the incoming board was installed. Recalled spare boards remain usable.
   AND destination.client_id IS NULL
   AND (destination.asset_id IS NULL OR destination.updated_at<=current.installed_at)
   -- The outgoing board must have disappeared from its last known installation,
   -- not moved to another slot or appeared elsewhere in this same controller map.
   AND NOT EXISTS (SELECT 1 FROM hashboard_installation later WHERE later.hashboard_id=prior.hashboard_id
    AND (later.removed_at IS NULL OR (later.installed_at,later.id)>(prior.installed_at,prior.id)))
   AND NOT EXISTS (
    SELECT 1 FROM hosted_account policy
    WHERE policy.organization_id=target_org AND policy.id IN (root.id,a.client_id,a.subclient_id)
     AND (policy.suspended
      OR (policy.sites_restricted AND NOT EXISTS (SELECT 1 FROM hosted_account_site allowed
       WHERE allowed.account_id=policy.id AND NOT allowed.parent_controlled AND allowed.site_id=current.site_id))
      OR (policy.site_ceiling_restricted AND NOT EXISTS (SELECT 1 FROM hosted_account_site allowed
       WHERE allowed.account_id=policy.id AND allowed.parent_controlled AND allowed.site_id=current.site_id)))
   )
 LOOP
  -- Both ownership periods meet at the accepted observation. A delayed
  -- confirmation starts ownership now; it never backfills earlier telemetry.
  PERFORM set_config('fleet.allocation_boundary',observed::text,true);
  UPDATE hosted_account_asset SET client_id=NULL,subclient_id=NULL,revision=revision+1,updated_at=clock_timestamp()
   WHERE organization_id=target_org AND kind=7 AND asset_id=replacement.old_board;
  INSERT INTO hosted_account_asset(organization_id,kind,asset_id,hashboard_id,device_id,client_id,subclient_id)
   VALUES(target_org,7,replacement.new_board,replacement.new_board,controller,replacement.client_id,replacement.subclient_id)
   ON CONFLICT(organization_id,kind,asset_id) DO UPDATE SET device_id=EXCLUDED.device_id,
    client_id=EXCLUDED.client_id,subclient_id=EXCLUDED.subclient_id,revision=hosted_account_asset.revision+1,updated_at=clock_timestamp();
  INSERT INTO activity_log(event_id,event_category,event_type,description,result,actor_type,organization_id,scope_type,scope_count,metadata)
   VALUES(gen_random_uuid(),'fleet_management','account_hashboard_replaced','Automatically replaced allocated hashboard','success','system',target_org,'account',1,
    jsonb_build_object('account_id',replacement.root_uuid,'child_account_id',replacement.client_uuid,
     'allocated_account_id',replacement.owner_uuid,'asset_count',1,'automatic',true,
     'old_hashboard_id',replacement.old_board,'replacement_hashboard_id',replacement.new_board,
     'module_id',replacement.module_id,'position',replacement.position,'observed_at',observed));
  transferred := transferred+1;
 END LOOP;
 PERFORM set_config('fleet.allocation_boundary',previous_boundary,true);
 RETURN transferred;
END;
$$;

-- fleet-migration: 001051_internal_hashboard_allocation_order.up.sql
-- requires-shared: 1000
-- Allocation writes and inventory take the organization row lock first. Fence
-- those writers before adding ordering markers to their shared tables.
LOCK TABLE organization IN EXCLUSIVE MODE;

CREATE SEQUENCE hosted_hashboard_allocation_order;
ALTER TABLE hosted_account_asset ADD COLUMN allocation_decision_order bigint NOT NULL DEFAULT 0;
ALTER TABLE hashboard_installation ADD COLUMN installed_order bigint, ADD COLUMN removed_order bigint;
ALTER TABLE container_inventory ADD COLUMN hashboard_inventory_order bigint,
 ADD COLUMN hashboard_inventory_complete boolean NOT NULL DEFAULT false;

-- Accepted inventory may be slightly ahead of the server clock. A controller
-- move or deletion must close all of its placements at one valid boundary.
CREATE OR REPLACE FUNCTION record_hosted_account_asset_placement() RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE boundary TIMESTAMPTZ;
BEGIN
 IF NEW.deleted_at IS NULL AND NEW.site_id IS NOT DISTINCT FROM OLD.site_id AND NEW.device_identifier=OLD.device_identifier THEN RETURN NEW; END IF;
 SELECT GREATEST(clock_timestamp(),max(started_at)) INTO boundary
 FROM hosted_account_asset_interval WHERE device_id=NEW.id AND ended_at IS NULL;
 IF NEW.deleted_at IS NOT NULL THEN
  UPDATE hosted_account_asset_interval SET ended_at=boundary WHERE device_id=NEW.id AND ended_at IS NULL;
  RETURN NEW;
 END IF;
 WITH closed AS (
  UPDATE hosted_account_asset_interval SET ended_at=boundary WHERE device_id=NEW.id AND ended_at IS NULL RETURNING *
 )
 INSERT INTO hosted_account_asset_interval(organization_id,kind,asset_id,device_id,client_id,subclient_id,started_at,site_id,device_identifier)
 SELECT organization_id,kind,asset_id,device_id,client_id,subclient_id,boundary,NEW.site_id,NEW.device_identifier FROM closed;
 RETURN NEW;
END;
$$;

-- Ownership writes include explicit decisions to keep the same owner. Physical
-- placement updates do not target these columns and must not advance this marker.
CREATE FUNCTION record_hosted_hashboard_allocation_order() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 NEW.allocation_decision_order := nextval('hosted_hashboard_allocation_order');
 RETURN NEW;
END;
$$;
CREATE TRIGGER hosted_hashboard_allocation_order BEFORE INSERT OR UPDATE OF client_id,subclient_id ON hosted_account_asset
 FOR EACH ROW WHEN (NEW.kind=7) EXECUTE FUNCTION record_hosted_hashboard_allocation_order();

-- These markers order accepted database operations, independent of device or
-- server clocks. Existing installations keep NULL markers: past ordering cannot
-- be inferred safely. Pending pre-upgrade replacements use the manual fallback.
CREATE FUNCTION record_hashboard_installation_order() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='INSERT' THEN
  NEW.installed_order := nextval('hosted_hashboard_allocation_order');
  IF NEW.removed_at IS NOT NULL THEN
   NEW.removed_order := nextval('hosted_hashboard_allocation_order');
  END IF;
 ELSIF OLD.removed_at IS NULL AND NEW.removed_at IS NOT NULL THEN
  NEW.removed_order := nextval('hosted_hashboard_allocation_order');
 END IF;
 RETURN NEW;
END;
$$;
CREATE TRIGGER hashboard_installation_order BEFORE INSERT OR UPDATE OF removed_at ON hashboard_installation
 FOR EACH ROW EXECUTE FUNCTION record_hashboard_installation_order();

CREATE OR REPLACE FUNCTION reconcile_hosted_hashboard_replacements(target_org bigint, controller bigint, observed timestamptz)
RETURNS bigint LANGUAGE plpgsql AS $$
DECLARE
 replacement record;
 previous_boundary text := COALESCE(current_setting('fleet.allocation_boundary',true),'');
 transferred bigint := 0;
 peer_inventory_order bigint;
 peer_observed_at timestamptz;
BEGIN
 PERFORM id FROM organization WHERE id=target_org AND deleted_at IS NULL FOR NO KEY UPDATE;
 IF NOT FOUND THEN RETURN 0; END IF;

 -- Absence on this controller cannot distinguish a replacement from a move
 -- whose destination has not reported yet. Require every other registered
 -- controller to accept a complete map after the candidate was installed.
 -- Read this once per poll, not once per replacement board.
 SELECT min(CASE WHEN inventory.hashboard_inventory_complete
   THEN COALESCE(inventory.hashboard_inventory_order,0) ELSE 0 END),min(inventory.observed_at)
 INTO peer_inventory_order,peer_observed_at
 FROM device peer JOIN discovered_device discovered ON discovered.id=peer.discovered_device_id
 LEFT JOIN container_inventory inventory ON inventory.device_id=peer.id
 WHERE peer.org_id=target_org AND peer.id<>controller AND peer.deleted_at IS NULL
  AND discovered.deleted_at IS NULL AND discovered.device_class=2;

 FOR replacement IN
  SELECT a.asset_id AS old_board, current.hashboard_id AS new_board, current.module_id, current.position,
   a.client_id,a.subclient_id,root.account_id AS root_uuid,client.account_id AS client_uuid,
   COALESCE(child.account_id,client.account_id) AS owner_uuid
  FROM hashboard_installation current
  JOIN container_module module ON module.id=current.module_id AND module.retired_at IS NULL
   AND module.identity_confirmed AND module.last_seen_at=observed
  JOIN container_tank tank ON tank.id=module.tank_id AND tank.device_id=controller AND tank.present
  JOIN container_inventory inventory ON inventory.device_id=controller AND inventory.observed_at=observed
   AND inventory.hashboard_inventory_complete
  JOIN device device ON device.id=controller AND device.org_id=target_org AND device.deleted_at IS NULL
   AND device.site_id IS NOT DISTINCT FROM current.site_id
  JOIN container_module_identity identity ON identity.module_id=module.id
   AND identity.org_id=target_org AND identity.reported_module_id=module.reported_id
  JOIN hashboard_slot slot ON slot.module_id=current.module_id AND slot.position=current.position
   AND slot.hashboard_id=current.hashboard_id AND slot.occupancy='occupied'
   AND slot.unavailable_reason='' AND slot.observed_at=observed
  JOIN hashboard_observation incoming ON incoming.hashboard_id=current.hashboard_id
   AND incoming.device_id=controller AND incoming.site_id IS NOT DISTINCT FROM current.site_id
   AND incoming.released_at IS NULL AND incoming.last_seen_at=observed
  CROSS JOIN LATERAL (
   SELECT p.* FROM hashboard_installation p
   WHERE p.module_id=current.module_id AND p.position=current.position
    AND (p.installed_at,p.id)<(current.installed_at,current.id)
   ORDER BY p.installed_at DESC,p.id DESC LIMIT 1
  ) prior
  JOIN hosted_account_asset a ON a.organization_id=target_org AND a.kind=7
   AND a.asset_id=prior.hashboard_id AND a.client_id IS NOT NULL AND a.device_id=prior.device_id
  JOIN hosted_account_asset_interval period ON period.organization_id=a.organization_id AND period.kind=7
   AND period.asset_id=a.asset_id AND period.ended_at IS NULL AND period.client_id=a.client_id
   AND period.subclient_id IS NOT DISTINCT FROM a.subclient_id AND period.started_at<=observed
   AND a.allocation_decision_order<=prior.removed_order
  JOIN hashboard_observation outgoing ON outgoing.hashboard_id=prior.hashboard_id
   AND outgoing.released_at IS NOT NULL
  JOIN hosted_account client ON client.id=a.client_id
  JOIN hosted_account root ON root.organization_id=target_org AND root.depth=0
  LEFT JOIN hosted_account child ON child.id=a.subclient_id
  LEFT JOIN hosted_account_asset destination ON destination.organization_id=target_org AND destination.kind=7
   AND destination.asset_id=current.hashboard_id
  WHERE current.device_id=controller AND current.removed_at IS NULL AND current.last_seen_at=observed
   AND prior.hashboard_id<>current.hashboard_id AND prior.removed_at<=current.installed_at
   AND (peer_inventory_order IS NULL OR (peer_inventory_order>GREATEST(current.installed_order,prior.removed_order)
    AND peer_observed_at>=current.installed_at))
   -- Never take an allocated board, or override a manual decision made after
   -- the incoming board was installed. Recalled spare boards remain usable.
   AND destination.client_id IS NULL
   AND COALESCE(destination.allocation_decision_order,0)<=current.installed_order
   -- A late report can precede the spare's previous ownership period. Wait for
   -- a later accepted poll instead of backdating or overlapping that history.
   AND NOT EXISTS (
    SELECT 1 FROM hosted_account_asset_interval history
    WHERE history.organization_id=target_org AND history.kind=7 AND history.asset_id=current.hashboard_id
     AND COALESCE(history.ended_at,history.started_at)>observed
   )
   -- The outgoing board must have disappeared from its last known installation,
   -- not moved to another slot or appeared elsewhere in this same controller map.
   AND NOT EXISTS (SELECT 1 FROM hashboard_installation later WHERE later.hashboard_id=prior.hashboard_id
    AND (later.removed_at IS NULL OR (later.installed_at,later.id)>(prior.installed_at,prior.id)))
   AND NOT EXISTS (
    SELECT 1 FROM hosted_account policy
    WHERE policy.organization_id=target_org AND policy.id IN (root.id,a.client_id,a.subclient_id)
     AND (policy.suspended
      OR (policy.sites_restricted AND NOT EXISTS (SELECT 1 FROM hosted_account_site allowed
       WHERE allowed.account_id=policy.id AND NOT allowed.parent_controlled AND allowed.site_id=current.site_id))
      OR (policy.site_ceiling_restricted AND NOT EXISTS (SELECT 1 FROM hosted_account_site allowed
       WHERE allowed.account_id=policy.id AND allowed.parent_controlled AND allowed.site_id=current.site_id)))
   )
 LOOP
  -- Both ownership periods meet at the accepted observation. A delayed
  -- confirmation starts ownership now; it never backfills earlier telemetry.
  PERFORM set_config('fleet.allocation_boundary',observed::text,true);
  UPDATE hosted_account_asset SET client_id=NULL,subclient_id=NULL,revision=revision+1,updated_at=clock_timestamp()
   WHERE organization_id=target_org AND kind=7 AND asset_id=replacement.old_board;
  INSERT INTO hosted_account_asset(organization_id,kind,asset_id,hashboard_id,device_id,client_id,subclient_id)
   VALUES(target_org,7,replacement.new_board,replacement.new_board,controller,replacement.client_id,replacement.subclient_id)
   ON CONFLICT(organization_id,kind,asset_id) DO UPDATE SET device_id=EXCLUDED.device_id,
    client_id=EXCLUDED.client_id,subclient_id=EXCLUDED.subclient_id,revision=hosted_account_asset.revision+1,updated_at=clock_timestamp();
  INSERT INTO activity_log(event_id,event_category,event_type,description,result,actor_type,organization_id,scope_type,scope_count,metadata)
   VALUES(gen_random_uuid(),'fleet_management','account_hashboard_replaced','Automatically replaced allocated hashboard','success','system',target_org,'account',1,
    jsonb_build_object('account_id',replacement.root_uuid,'child_account_id',replacement.client_uuid,
     'allocated_account_id',replacement.owner_uuid,'asset_count',1,'automatic',true,
     'old_hashboard_id',replacement.old_board,'replacement_hashboard_id',replacement.new_board,
     'module_id',replacement.module_id,'position',replacement.position,'observed_at',observed));
  transferred := transferred+1;
 END LOOP;
 PERFORM set_config('fleet.allocation_boundary',previous_boundary,true);
 RETURN transferred;
END;
$$;

-- fleet-migration: 001052_internal_hashboard_allocation_consumers.up.sql
-- requires-shared: 1000
CREATE OR REPLACE VIEW repair_ticket_asset_option AS
SELECT 1::smallint AS kind,d.id,d.org_id,COALESCE(d.site_id,dsr.site_id) AS site_id,d.id AS parent_asset_id,5::int AS allocation_kind,
 NULL::bigint AS module_id,NULL::bigint AS installation_id,NULL::bigint AS container_id,NULL::int AS position,
 COALESCE(NULLIF(d.custom_name,''),d.device_identifier)::text AS label,COALESCE(dd.model,'')::text AS model,
 d.device_identifier::text AS miner_identifier,COALESCE(d.building_id,dsr.building_id) AS building_id,rack.id AS rack_id
FROM device d JOIN discovered_device dd ON dd.id=d.discovered_device_id AND dd.org_id=d.org_id
LEFT JOIN device_set_membership rm ON rm.device_id=d.id AND rm.org_id=d.org_id AND rm.device_set_type='rack'
LEFT JOIN device_set rack ON rack.id=rm.device_set_id AND rack.org_id=d.org_id AND rack.deleted_at IS NULL
LEFT JOIN device_set_rack dsr ON dsr.device_set_id=rack.id AND dsr.org_id=d.org_id
WHERE d.deleted_at IS NULL AND dd.deleted_at IS NULL AND dd.device_class<>2
UNION ALL
SELECT 3::smallint,m.id,d.org_id,d.site_id,m.id,6,m.id,NULL::bigint,d.id,m.position,
 ('Module '||m.id||CASE WHEN m.reported_id<>'' THEN ' / '||m.reported_id ELSE '' END)::text,m.model,''::text,NULL::bigint,NULL::bigint
FROM container_module m JOIN container_tank t ON t.id=m.tank_id JOIN device d ON d.id=t.device_id
WHERE m.retired_at IS NULL AND d.deleted_at IS NULL
UNION ALL
SELECT 4::smallint,b.id,b.org_id,d.site_id,b.id,7,m.id,i.id,d.id,i.position,
 ('Hashboard '||b.id||' / Module '||m.id||' / slot '||i.position)::text,m.model,''::text,NULL::bigint,NULL::bigint
FROM hashboard b JOIN hashboard_installation i ON i.hashboard_id=b.id AND i.removed_at IS NULL
JOIN container_module m ON m.id=i.module_id JOIN container_tank t ON t.id=m.tank_id JOIN device d ON d.id=t.device_id AND d.org_id=b.org_id
WHERE m.retired_at IS NULL AND d.deleted_at IS NULL;


-- Every ownership path, including automatic replacement, retires prior intent.
CREATE FUNCTION retire_reallocated_pool_assignments(organization bigint, controller bigint, asset_kind integer, asset bigint)
RETURNS void LANGUAGE SQL AS $$
 WITH retired AS (
  UPDATE pool_configuration_assignment a SET retired_at=CURRENT_TIMESTAMP,
   version=a.version+1,updated_at=CURRENT_TIMESTAMP
  WHERE a.organization_id=organization AND a.retired_at IS NULL
   AND ((a.target_kind='device' AND a.device_id=controller) OR
    (a.target_kind='hashboard_installation' AND EXISTS (
     SELECT 1 FROM hashboard_installation i WHERE i.id=a.target_id
      AND ((asset_kind=6 AND i.module_id=asset) OR (asset_kind=7 AND i.hashboard_id=asset)))))
  RETURNING a.organization_id,a.target_kind,a.target_id
 )
 UPDATE pool_configuration_operation_target t SET state='superseded',updated_at=CURRENT_TIMESTAMP
 FROM retired a WHERE t.organization_id=a.organization_id AND t.target_kind=a.target_kind AND t.target_id=a.target_id
  AND t.state IN ('pending','applying','failed','different','applied_unverified');
$$;
CREATE FUNCTION retire_hosted_allocation_pool_intent() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='UPDATE' THEN
  IF NEW.client_id IS NOT DISTINCT FROM OLD.client_id AND NEW.subclient_id IS NOT DISTINCT FROM OLD.subclient_id
   AND NEW.device_id=OLD.device_id THEN RETURN NEW; END IF;
  IF NEW.device_id<>OLD.device_id THEN
   PERFORM retire_reallocated_pool_assignments(OLD.organization_id,OLD.device_id,OLD.kind,OLD.asset_id);
  END IF;
 ELSIF NEW.client_id IS NULL THEN RETURN NEW;
 END IF;
 PERFORM retire_reallocated_pool_assignments(NEW.organization_id,NEW.device_id,NEW.kind,NEW.asset_id);
 RETURN NEW;
END $$;
CREATE TRIGGER hosted_allocation_pool_intent AFTER INSERT OR UPDATE ON hosted_account_asset
 FOR EACH ROW EXECUTE FUNCTION retire_hosted_allocation_pool_intent();

-- fleet-migration: 001053_shared_pool_equipment_state.up.sql
-- Importing existing intent is not a new hardware command or a verification
-- receipt. Keep its provenance without fabricating an actor or queued operation.
ALTER TABLE pool_configuration_assignment ALTER COLUMN operation_id DROP NOT NULL;
ALTER TABLE pool_configuration_assignment ADD COLUMN adopted_at TIMESTAMPTZ;
ALTER TABLE pool_configuration_assignment ADD CONSTRAINT chk_pool_configuration_assignment_origin
    CHECK (operation_id IS NOT NULL OR adopted_at IS NOT NULL);
-- Readback evidence is separate from the immutable application history.
-- A new assignment version cannot inherit a previous assignment's verification.
CREATE TABLE pool_configuration_observation (
    organization_id BIGINT NOT NULL,
    target_kind TEXT NOT NULL,
    target_id BIGINT NOT NULL,
    assignment_version BIGINT NOT NULL CHECK (assignment_version > 0),
    state TEXT NOT NULL CHECK (state IN ('verified','different','applied_unverified')),
    result_code TEXT NOT NULL DEFAULT '',
    observed_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    last_verified_at TIMESTAMPTZ,
    PRIMARY KEY (organization_id, target_kind, target_id),
    CONSTRAINT fk_pool_configuration_observation_assignment FOREIGN KEY (organization_id,target_kind,target_id)
        REFERENCES pool_configuration_assignment(organization_id,target_kind,target_id) ON DELETE CASCADE
);

-- fleet-migration: 001054_internal_pool_equipment_integration.up.sql
-- requires-shared: 1053

-- Maintenance adoption cannot attach a previous owner's private configuration
-- to newly allocated hardware, even if an import source was collected earlier.
CREATE FUNCTION validate_pool_assignment_adoption() RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE
    configuration_owner BIGINT;
    equipment_owner BIGINT;
BEGIN
    IF NEW.operation_id IS NOT NULL THEN RETURN NEW; END IF;
    SELECT COALESCE(account_id,0) INTO configuration_owner FROM pool_configuration
        WHERE organization_id=NEW.organization_id AND configuration_id=NEW.configuration_id;
    IF NEW.target_kind='hashboard_installation' THEN
        SELECT COALESCE(a.subclient_id,a.client_id,0) INTO equipment_owner
        FROM hashboard_installation i
        LEFT JOIN hosted_account_asset a ON a.organization_id=NEW.organization_id AND a.kind=7 AND a.asset_id=i.hashboard_id
        WHERE i.id=NEW.target_id AND i.device_id=NEW.device_id AND i.removed_at IS NULL;
    ELSE
        SELECT COALESCE(a.subclient_id,a.client_id,0) INTO equipment_owner
        FROM device d JOIN discovered_device dd ON dd.id=d.discovered_device_id AND dd.org_id=d.org_id
        LEFT JOIN hosted_account_asset a ON a.organization_id=d.org_id AND a.kind=5 AND a.asset_id=d.id
        WHERE d.id=NEW.target_id AND d.org_id=NEW.organization_id AND dd.device_class=1 AND d.deleted_at IS NULL;
        IF equipment_owner IS NULL THEN
            -- Whole-container ownership is derived from every current board,
            -- using the same complete membership as the import collector.
            -- Missing membership must not count as operator-owned hardware.
            SELECT min(COALESCE(a.subclient_id,a.client_id,0)) INTO equipment_owner
            FROM device d JOIN discovered_device dd ON dd.id=d.discovered_device_id AND dd.org_id=d.org_id
            JOIN container_tank t ON t.device_id=d.id AND t.present
            JOIN container_module m ON m.tank_id=t.id AND m.retired_at IS NULL
            LEFT JOIN hosted_module_membership membership ON membership.module_id=m.id AND membership.org_id=d.org_id
            LEFT JOIN LATERAL unnest(membership.board_ids) b(id) ON true
            LEFT JOIN hosted_account_asset a ON a.organization_id=d.org_id AND a.kind=7 AND a.asset_id=b.id
            WHERE d.id=NEW.target_id AND d.org_id=NEW.organization_id AND dd.device_class=2
              AND d.deleted_at IS NULL AND dd.deleted_at IS NULL
            HAVING count(*)>0 AND bool_and(membership.module_id IS NOT NULL)
              AND count(DISTINCT COALESCE(a.subclient_id,a.client_id,0))=1;
        END IF;
    END IF;
    IF equipment_owner IS NULL OR configuration_owner IS DISTINCT FROM equipment_owner THEN
        RAISE EXCEPTION 'pool adoption requires the current equipment owner' USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER pool_assignment_adoption BEFORE INSERT ON pool_configuration_assignment
    FOR EACH ROW EXECUTE FUNCTION validate_pool_assignment_adoption();

-- An immutable, rendered execution snapshot, not another user-facing library.
CREATE TABLE pool_configuration_group_snapshot (
 snapshot_id VARCHAR(36) PRIMARY KEY,
 organization_id BIGINT NOT NULL,
 device_id BIGINT NOT NULL,
 configuration_id VARCHAR(36) NOT NULL,
 revision_id VARCHAR(36) NOT NULL,
 slots JSONB NOT NULL CHECK (valid_pool_configuration_slots(slots)),
 CONSTRAINT fk_pool_configuration_group_snapshot_device FOREIGN KEY(device_id,organization_id) REFERENCES device(id,org_id),
 CONSTRAINT fk_pool_configuration_group_snapshot_revision FOREIGN KEY(organization_id,configuration_id,revision_id)
  REFERENCES pool_configuration_revision(organization_id,configuration_id,revision_id),
 CONSTRAINT uq_pool_configuration_group_snapshot_org_snapshot UNIQUE(organization_id,snapshot_id)
);
CREATE TRIGGER pool_configuration_group_snapshot_immutable BEFORE UPDATE OR DELETE ON pool_configuration_group_snapshot
 FOR EACH ROW EXECUTE FUNCTION keep_pool_configuration_revision_immutable();
ALTER TABLE pool_configuration_operation_target ADD COLUMN group_snapshot_id VARCHAR(36),
 ADD CONSTRAINT fk_pool_configuration_operation_target_group_snapshot FOREIGN KEY(organization_id,group_snapshot_id)
 REFERENCES pool_configuration_group_snapshot(organization_id,snapshot_id);
-- One controller workflow applies the entire selected set of board targets.
DROP INDEX idx_pool_configuration_operation_target_attempt;
CREATE UNIQUE INDEX idx_pool_configuration_operation_target_device_attempt
 ON pool_configuration_operation_target(physical_attempt_id) WHERE physical_attempt_id IS NOT NULL AND target_kind='device';
CREATE INDEX idx_pool_configuration_operation_target_group_attempt
 ON pool_configuration_operation_target(physical_attempt_id) WHERE target_kind='hashboard_installation';

-- Retain the admitted operation so pool updates can distinguish unrelated work.
-- Unclassified direct and workflow attempts remain conservative conflicts.
ALTER TABLE device_write_attempt ADD COLUMN command_type TEXT NOT NULL DEFAULT '';

-- Queue references capture the original command; do not infer legacy direct
-- operations from today's device state or an error message.
UPDATE device_write_attempt attempt SET command_type=message.command_type
FROM queue_message message WHERE message.id=attempt.queue_message_id;

-- fleet-migration: 001055_internal_hosted_reporting_evidence.up.sql
-- requires-shared: 1000
-- Stop application and scheduled writers for this upgrade. Drain source writes
-- before installing capture and its capture-time boundary. Do not backfill.
LOCK TABLE device_metrics,miner_state_snapshots,container_module_metrics,
 container_module_diagnostic_observation,hashboard_metrics,hashboard_diagnostic_observation,
 hosted_account_history_sample
 IN SHARE ROW EXCLUSIVE MODE;

-- Topology stamping does not change the admitted measurement or its identity.
-- Keep real value, source-window and installation corrections on the existing
-- normalization path, without retaining delete/reinsert noise on every poll.
DROP TRIGGER record_hosted_hashboard_history ON hashboard_metrics;
CREATE TRIGGER record_hosted_hashboard_history
 -- Keep this list aligned with record_hosted_hashboard_history_sample in migration 1049.
 AFTER INSERT OR UPDATE OF hashboard_id,installation_id,time,collected_at,state,state_observed_at,
  hashrate_hs,power_w,temperature_c OR DELETE ON hashboard_metrics
 FOR EACH ROW EXECUTE FUNCTION record_hosted_hashboard_history_sample('metrics');

CREATE TABLE hosted_account_reporting_evidence_epoch (
 singleton boolean CONSTRAINT pk_hosted_reporting_evidence_epoch PRIMARY KEY DEFAULT true
  CONSTRAINT ck_hosted_reporting_evidence_epoch_singleton CHECK(singleton),
 started_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
INSERT INTO hosted_account_reporting_evidence_epoch(singleton) VALUES(true);

CREATE TABLE hosted_account_reporting_allocation_snapshot (
 interval_id bigint CONSTRAINT pk_hosted_reporting_allocation_snapshot PRIMARY KEY,
 organization_id bigint NOT NULL,
 client_id bigint NOT NULL,
 subclient_id bigint,
 site_id bigint,
 kind integer NOT NULL,
 asset_id bigint NOT NULL,
 device_id bigint NOT NULL,
 device_identifier text NOT NULL,
 started_at timestamptz NOT NULL,
 captured_at timestamptz NOT NULL DEFAULT clock_timestamp()
);

-- An ordinary table is independent of chart chunks and has no automatic expiry.
CREATE TABLE hosted_account_reporting_evidence (
 event_id bigserial CONSTRAINT pk_hosted_reporting_evidence PRIMARY KEY,
 epoch_started_at timestamptz NOT NULL,
 captured_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 transaction_id bigint NOT NULL DEFAULT txid_current(),
 operation text NOT NULL CONSTRAINT ck_hosted_reporting_evidence_operation CHECK(operation IN ('INSERT','UPDATE','DELETE')),
 image text NOT NULL CONSTRAINT ck_hosted_reporting_evidence_image CHECK(image IN ('before','after')),
 time timestamptz NOT NULL,
 interval_id bigint NOT NULL REFERENCES hosted_account_reporting_allocation_snapshot(interval_id),
 metric smallint NOT NULL,
 value double precision,
 complete boolean,
 source_oldest_at timestamptz NOT NULL,
 source_newest_at timestamptz NOT NULL,
 source_id smallint NOT NULL,
 origin_key text NOT NULL,
 origin_time timestamptz NOT NULL,
 origin_collected_at timestamptz NOT NULL
);
CREATE INDEX idx_hosted_account_reporting_evidence_stream_event
 ON hosted_account_reporting_evidence(interval_id,source_id,metric,event_id DESC);
CREATE INDEX idx_hosted_account_reporting_evidence_stream_observation
 ON hosted_account_reporting_evidence(interval_id,source_id,metric,event_id DESC)
 WHERE operation IN ('INSERT','UPDATE') AND image='after';

CREATE FUNCTION reject_hosted_reporting_evidence_mutation() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 RAISE EXCEPTION 'hosted reporting evidence is append-only';
END;
$$;
CREATE TRIGGER hosted_reporting_evidence_immutable
 BEFORE UPDATE OR DELETE OR TRUNCATE ON hosted_account_reporting_evidence
 FOR EACH STATEMENT EXECUTE FUNCTION reject_hosted_reporting_evidence_mutation();
CREATE TRIGGER hosted_reporting_allocation_snapshot_immutable
 BEFORE UPDATE OR DELETE OR TRUNCATE ON hosted_account_reporting_allocation_snapshot
 FOR EACH STATEMENT EXECUTE FUNCTION reject_hosted_reporting_evidence_mutation();
CREATE TRIGGER hosted_reporting_evidence_epoch_immutable
 BEFORE UPDATE OR DELETE OR TRUNCATE ON hosted_account_reporting_evidence_epoch
 FOR EACH STATEMENT EXECUTE FUNCTION reject_hosted_reporting_evidence_mutation();

CREATE FUNCTION record_hosted_reporting_evidence() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
 sample hosted_account_history_sample;
 sample_image text;
 images text[] := CASE TG_OP WHEN 'UPDATE' THEN ARRAY['before','after']
  WHEN 'DELETE' THEN ARRAY['before'] ELSE ARRAY['after'] END;
BEGIN
 FOREACH sample_image IN ARRAY images LOOP
  IF sample_image='before' THEN sample := OLD; ELSE sample := NEW; END IF;
  INSERT INTO hosted_account_reporting_allocation_snapshot(interval_id,organization_id,client_id,subclient_id,
   site_id,kind,asset_id,device_id,device_identifier,started_at)
  SELECT id,organization_id,client_id,subclient_id,site_id,kind,asset_id,device_id,device_identifier,started_at
  FROM hosted_account_asset_interval WHERE id=sample.interval_id
  ON CONFLICT(interval_id) DO NOTHING;
  INSERT INTO hosted_account_reporting_evidence(epoch_started_at,operation,image,time,interval_id,metric,value,complete,
   source_oldest_at,source_newest_at,source_id,origin_key,origin_time,origin_collected_at)
  SELECT started_at,TG_OP,sample_image,sample.time,sample.interval_id,sample.metric,sample.value,sample.complete,
   sample.source_oldest_at,sample.source_newest_at,sample.source_id,sample.origin_key,sample.origin_time,sample.origin_collected_at
  FROM hosted_account_reporting_evidence_epoch WHERE singleton;
  IF NOT FOUND THEN RAISE EXCEPTION 'hosted reporting evidence epoch is missing'; END IF;
 END LOOP;
 RETURN NULL;
END;
$$;
-- DELETE means normalized-sample deletion. Corrections commonly delete and
-- insert in one transaction; neither deletion nor chunk expiry proves retraction.
CREATE TRIGGER record_hosted_reporting_evidence
 AFTER INSERT OR UPDATE OR DELETE ON hosted_account_history_sample
 FOR EACH ROW EXECUTE FUNCTION record_hosted_reporting_evidence();

CREATE VIEW hosted_account_reporting_capture_status AS
SELECT i.id AS interval_id,i.organization_id,i.client_id,i.subclient_id,i.site_id,i.kind,
 i.asset_id,i.device_id,i.device_identifier,i.started_at AS allocation_started_at,
 epoch.started_at AS hold_started_at,stream.source_id,stream.metric,
 latest.event_id AS last_event_id,latest.operation AS last_event_operation,
 latest.captured_at AS last_event_captured_at,
 observation.event_id AS last_observation_event_id,
 observation.captured_at AS last_observation_captured_at,
 observation.value AS last_observation_value,observation.complete AS last_observation_complete,
 observation.origin_key AS last_observation_origin_key,
 observation.origin_time AS last_observation_origin_time,
 observation.origin_collected_at AS last_observation_collected_at,
 observation.source_oldest_at AS last_observation_source_oldest_at,
 observation.source_newest_at AS last_observation_source_newest_at
FROM hosted_account_asset_interval i
CROSS JOIN hosted_account_reporting_evidence_epoch epoch
JOIN (
 SELECT 5 AS kind,1::smallint AS source_id,hosted_history_metric_code('hashrate') AS metric
 UNION ALL SELECT 5,1::smallint,hosted_history_metric_code('power')
 UNION ALL SELECT 5,1::smallint,hosted_history_metric_code('temperature')
 UNION ALL SELECT 5,2::smallint,hosted_history_metric_code('state')
 UNION ALL SELECT 6,3::smallint,hosted_history_metric_code('hashrate')
 UNION ALL SELECT 6,3::smallint,hosted_history_metric_code('power')
 UNION ALL SELECT 6,3::smallint,hosted_history_metric_code('temperature')
 UNION ALL SELECT 6,4::smallint,hosted_history_metric_code('state')
 UNION ALL SELECT 7,5::smallint,hosted_history_metric_code('hashrate')
 UNION ALL SELECT 7,5::smallint,hosted_history_metric_code('power')
 UNION ALL SELECT 7,5::smallint,hosted_history_metric_code('temperature')
 UNION ALL SELECT 7,6::smallint,hosted_history_metric_code('state')
) stream ON stream.kind=i.kind
LEFT JOIN LATERAL (
 SELECT event_id,operation,captured_at FROM hosted_account_reporting_evidence e
 WHERE e.interval_id=i.id AND e.source_id=stream.source_id AND e.metric=stream.metric
 ORDER BY event_id DESC LIMIT 1
) latest ON true
LEFT JOIN LATERAL (
 SELECT event_id,captured_at,value,complete,origin_key,origin_time,origin_collected_at,source_oldest_at,source_newest_at
 FROM hosted_account_reporting_evidence e WHERE e.interval_id=i.id
  AND e.source_id=stream.source_id AND e.metric=stream.metric
  AND operation IN ('INSERT','UPDATE') AND image='after'
 ORDER BY event_id DESC LIMIT 1
) observation ON true
WHERE i.ended_at IS NULL;

-- fleet-migration: 001056_internal_pool_configuration_cutover.up.sql
-- requires-shared: 1053

-- Effective board intent survives operation outcomes. Pending work remains in
-- operation targets; only established effects or explicit adoption enter here.
ALTER TABLE pool_configuration_group_snapshot ADD CONSTRAINT uq_pool_group_snapshot_device
 UNIQUE(organization_id,device_id,snapshot_id);
CREATE TABLE pool_configuration_board_intent (
 organization_id BIGINT NOT NULL,
 installation_id BIGINT NOT NULL REFERENCES hashboard_installation(id),
 device_id BIGINT NOT NULL,
 hashboard_id BIGINT NOT NULL REFERENCES hashboard(id),
 mac_address TEXT NOT NULL CHECK(mac_address<>''),
 group_snapshot_id VARCHAR(36) NOT NULL,
 operation_id VARCHAR(36),
 physical_attempt_id UUID REFERENCES device_write_attempt(attempt_id),
 adopted_at TIMESTAMPTZ,
 updated_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(organization_id,installation_id),
 CONSTRAINT fk_pool_board_intent_snapshot FOREIGN KEY(organization_id,device_id,group_snapshot_id)
  REFERENCES pool_configuration_group_snapshot(organization_id,device_id,snapshot_id),
 CONSTRAINT fk_pool_board_intent_operation FOREIGN KEY(organization_id,operation_id)
  REFERENCES pool_configuration_operation(organization_id,operation_id),
 CONSTRAINT chk_pool_board_intent_origin CHECK(operation_id IS NOT NULL OR adopted_at IS NOT NULL)
);
CREATE INDEX idx_pool_board_intent_device ON pool_configuration_board_intent(organization_id,device_id);
-- Only terminal verified outcomes establish pre-upgrade effects. Failed and
-- unfinished operations require the maintenance inventory/recovery procedure.
INSERT INTO pool_configuration_board_intent(organization_id,installation_id,device_id,hashboard_id,mac_address,group_snapshot_id,operation_id,physical_attempt_id)
SELECT DISTINCT ON(a.organization_id,a.target_id)
 a.organization_id,a.target_id,a.device_id,i.hashboard_id,i.metadata->>'macAddress',t.group_snapshot_id,t.operation_id,t.physical_attempt_id
FROM pool_configuration_assignment a JOIN pool_configuration_operation_target t
 ON t.organization_id=a.organization_id AND t.target_kind=a.target_kind AND t.target_id=a.target_id
JOIN hashboard_installation i ON i.id=a.target_id
JOIN pool_configuration_group_snapshot snapshot ON snapshot.organization_id=t.organization_id AND snapshot.snapshot_id=t.group_snapshot_id
JOIN pool_configuration previous ON previous.organization_id=snapshot.organization_id AND previous.configuration_id=snapshot.configuration_id
JOIN pool_configuration current_config ON current_config.organization_id=a.organization_id AND current_config.configuration_id=a.configuration_id
WHERE a.target_kind='hashboard_installation' AND a.retired_at IS NULL
 AND t.state='verified' AND t.group_snapshot_id IS NOT NULL AND COALESCE(i.metadata->>'macAddress','')<>''
 AND previous.account_id IS NOT DISTINCT FROM current_config.account_id
 -- A retirement advances the assignment without creating an operation target.
 -- Do not resurrect effects across that gap, even if the same owner returns.
 AND (SELECT count(DISTINCT history.assignment_version) FROM pool_configuration_operation_target history
  WHERE history.organization_id=a.organization_id AND history.target_kind=a.target_kind AND history.target_id=a.target_id
   AND history.assignment_version>t.assignment_version AND history.assignment_version<=a.version)=a.version-t.assignment_version
ORDER BY a.organization_id,a.target_id,t.updated_at DESC,t.operation_id;


-- This marker is written only by the maintenance completeness gate. It is not
-- a rollout flag: returning to legacy writers requires restoring a backup.
CREATE TABLE pool_configuration_cutover (
 organization_id BIGINT PRIMARY KEY REFERENCES organization(id),
 frozen_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 completed_at TIMESTAMPTZ,
 report JSONB NOT NULL DEFAULT '{}'
);
CREATE FUNCTION reject_legacy_pool_writer() RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE org BIGINT;
BEGIN
 org := COALESCE((to_jsonb(NEW)->>'organization_id')::bigint,(to_jsonb(NEW)->>'org_id')::bigint,
                 (to_jsonb(OLD)->>'organization_id')::bigint,(to_jsonb(OLD)->>'org_id')::bigint);
 -- A dedicated barrier avoids blocking ordinary organization foreign-key checks.
 -- Serialize with freeze, including writers that began before it.
 PERFORM pg_advisory_xact_lock_shared(hashtextextended('pool-cutover:'||org::text,0));
 IF EXISTS(SELECT 1 FROM pool_configuration_cutover WHERE organization_id=org) THEN
  RAISE EXCEPTION 'legacy pool writes are disabled after configuration cutover' USING ERRCODE='23514';
 END IF;
 IF TG_OP='DELETE' THEN RETURN OLD; END IF;
 RETURN NEW;
END;
$$;
CREATE TRIGGER legacy_pool_cutover BEFORE INSERT OR UPDATE OR DELETE ON pool
 FOR EACH ROW EXECUTE FUNCTION reject_legacy_pool_writer();
CREATE TRIGGER legacy_hosted_pool_cutover BEFORE INSERT OR UPDATE OR DELETE ON hosted_account_pool
 FOR EACH ROW EXECUTE FUNCTION reject_legacy_pool_writer();
CREATE TRIGGER legacy_pool_revision_cutover BEFORE INSERT OR UPDATE OR DELETE ON hosted_account_pool_revision
 FOR EACH ROW EXECUTE FUNCTION reject_legacy_pool_writer();
CREATE TRIGGER legacy_pool_profile_cutover BEFORE INSERT OR UPDATE OR DELETE ON hosted_account_pool_profile
 FOR EACH ROW EXECUTE FUNCTION reject_legacy_pool_writer();
CREATE TRIGGER legacy_board_intent_cutover BEFORE INSERT OR UPDATE OR DELETE ON hashboard_pool_intent
 FOR EACH ROW EXECUTE FUNCTION reject_legacy_pool_writer();

-- Legacy pool commands carry raw payloads and can bypass the old library.
-- Canonical pool applications use the fenced worker directly, never this queue.
CREATE FUNCTION reject_legacy_pool_command() RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE org BIGINT;
BEGIN
 IF NEW.command_type<>'UpdateMiningPools' OR NEW.status NOT IN ('PENDING','PROCESSING') THEN RETURN NEW; END IF;
 SELECT org_id INTO org FROM device WHERE id=NEW.device_id;
 PERFORM pg_advisory_xact_lock_shared(hashtextextended('pool-cutover:'||org::text,0));
 IF EXISTS(SELECT 1 FROM pool_configuration_cutover WHERE organization_id=org) THEN
  RAISE EXCEPTION 'legacy pool commands are disabled; use reviewed pool configurations' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END;
$$;
CREATE TRIGGER legacy_pool_command_cutover BEFORE INSERT OR UPDATE OF command_type,status ON queue_message
 FOR EACH ROW EXECUTE FUNCTION reject_legacy_pool_command();

-- fleet-migration: 001057_internal_device_write_outcomes.up.sql
-- requires-shared: 1000

ALTER TABLE device_write_attempt
 ADD COLUMN write_outcome JSONB NOT NULL DEFAULT '{}'::jsonb
 CHECK (jsonb_typeof(write_outcome) = 'object');

-- fleet-migration: 001058_internal_bound_topology_reference_planning.up.sql
-- requires-shared: 1000
-- Prune unrelated chunks while planning to leave time for deletions and commits
-- within the cleanup job's runtime budget. Each call needs its own bounds, including when
-- invoked from a prepared statement or a cached cleanup procedure.
CREATE OR REPLACE FUNCTION container_topology_references(ids bigint[])
RETURNS TABLE(observation_id bigint,reference_kind text)
LANGUAGE plpgsql STABLE SET plan_cache_mode=force_custom_plan AS $$
DECLARE
 first_collected_at timestamptz;
 last_collected_at timestamptz;
BEGIN
 SELECT min(o.collected_at),max(o.collected_at) INTO first_collected_at,last_collected_at
 FROM container_topology_observation o WHERE o.id=ANY(ids);
 IF first_collected_at IS NULL THEN RETURN; END IF;
 RETURN QUERY

 WITH observations AS MATERIALIZED (
  SELECT o.id,o.device_id,o.collected_at,d.device_identifier FROM container_topology_observation o
  JOIN device d ON d.id=o.device_id WHERE o.id=ANY(ids)
 )
 SELECT DISTINCT m.topology_observation_id,'raw'::text FROM observations o
 JOIN LATERAL (SELECT topology_observation_id FROM device_metrics m
  WHERE m.time=o.collected_at AND m.time>=first_collected_at AND m.time<=last_collected_at AND m.device_identifier=o.device_identifier
   AND m.topology_observation_id=o.id LIMIT 1) m ON true
 UNION
 SELECT DISTINCT m.topology_observation_id,'raw'::text FROM observations o
 JOIN LATERAL (SELECT topology_observation_id FROM container_tank_metrics m
  WHERE m.time=o.collected_at AND m.time>=first_collected_at AND m.time<=last_collected_at AND m.topology_observation_id=o.id
   AND m.tank_id=ANY(ARRAY(SELECT t.id FROM container_tank t WHERE t.device_id=o.device_id))
  LIMIT 1) m ON true
 UNION
 SELECT DISTINCT topology_observation_id,'latest'::text FROM container_tank_latest_metrics WHERE topology_observation_id=ANY(ids)
 UNION
 SELECT DISTINCT topology_observation_id,'latest'::text FROM container_latest_metrics WHERE topology_observation_id=ANY(ids);

END;
$$;

-- fleet-migration: 001059_internal_telemetry_device_site_lookup_index.up.sql
-- requires-shared: 1000

-- Match telemetry's site lookup, including soft-deleted device history.
-- Index-only scans need the raw deleted_at column, not just the expression key.
CREATE INDEX idx_device_telemetry_site_lookup
    ON device (device_identifier, (deleted_at IS NULL) DESC, updated_at DESC, id DESC)
    INCLUDE (site_id, deleted_at);

-- fleet-migration: 001060_shared_curtailment_wave4_runtime.up.sql
-- Persist dispatch revocation and isolate automation execution per rule.
ALTER TABLE curtailment_event ADD COLUMN dispatch_rejection_reason text;
-- Retry history alone does not prove a Curtail was sent: admission skips and
-- unenqueued devices also consume retries. Only possible sends owe Uncurtail.
ALTER TABLE curtailment_target ADD COLUMN curtail_possibly_sent boolean NOT NULL DEFAULT false;
-- Before this column, retry history was the only evidence that a Curtail may
-- have reached a miner, so live events' targets with retries keep owing one.
UPDATE curtailment_target AS target
SET curtail_possibly_sent = TRUE
FROM curtailment_event AS event
WHERE event.id = target.curtailment_event_id
  AND event.state IN ('pending', 'active', 'restoring')
  AND (target.retry_count > 0 OR target.curtail_retry_count > 0);

CREATE TABLE curtailment_automation_execution_grant (
 rule_id bigint PRIMARY KEY REFERENCES curtailment_automation_rule(id) ON DELETE CASCADE,
 org_id bigint NOT NULL REFERENCES organization(id),
 response_profile_id bigint NOT NULL,
 response_profile_revision uuid NOT NULL,
 authorization_envelope_json jsonb NOT NULL,
 admin_controls_granted boolean NOT NULL DEFAULT false,
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE curtailment_automation_execution (
 rule_id bigint PRIMARY KEY REFERENCES curtailment_automation_rule(id) ON DELETE CASCADE,
 mqtt_source_id bigint NOT NULL REFERENCES curtailment_mqtt_source_config(id) ON DELETE CASCADE,
 generation bigint NOT NULL DEFAULT 1,
 direction text NOT NULL,
 target text NOT NULL CHECK (target IN ('ON','OFF')),
 target_at timestamptz,
 received_at timestamptz NOT NULL,
 received_broker text NOT NULL DEFAULT '',
 prior_edge_at timestamptz,
 status text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','retry','completed')),
 attempts integer NOT NULL DEFAULT 0,
 retry_at timestamptz NOT NULL DEFAULT now(),
 lease_expires_at timestamptz,
 last_error text
);
CREATE INDEX idx_curtailment_automation_execution_due ON curtailment_automation_execution(retry_at,rule_id) WHERE status IN ('pending','retry');
ALTER TABLE curtailment_rig_config_reconciliation ADD COLUMN delivery_attempts integer NOT NULL DEFAULT 0;
CREATE TABLE curtailment_rig_config_delivery_attempt (
 organization_id bigint NOT NULL REFERENCES organization(id) ON DELETE CASCADE,
 device_id bigint NOT NULL REFERENCES device(id) ON DELETE CASCADE,
 attempts integer NOT NULL DEFAULT 0,
 PRIMARY KEY (organization_id,device_id)
);

-- fleet-migration: 001061_internal_curtailment_wave4_runtime.up.sql
-- requires-shared: 1060
CREATE OR REPLACE FUNCTION request_curtailment_rig_config_reconciliation(p_org_id bigint, p_requested_by bigint, p_full boolean)
RETURNS bigint LANGUAGE plpgsql VOLATILE SECURITY INVOKER AS $$
DECLARE result bigint;
BEGIN
 INSERT INTO curtailment_rig_config_reconciliation AS reconciliation (organization_id, requested_by, full_reconcile_generation)
 VALUES (p_org_id, p_requested_by, CASE WHEN p_full THEN 1 ELSE 0 END)
 ON CONFLICT (organization_id) DO UPDATE SET requested_by = EXCLUDED.requested_by,
 desired_generation = reconciliation.desired_generation + 1,
 full_reconcile_generation = CASE WHEN p_full THEN reconciliation.desired_generation + 1 ELSE reconciliation.full_reconcile_generation END,
 retry_at = CURRENT_TIMESTAMP, last_error = NULL, delivery_attempts = 0
 RETURNING reconciliation.desired_generation INTO result;
 IF p_full THEN
  DELETE FROM curtailment_rig_config_delivery_attempt WHERE organization_id = p_org_id;
 END IF;
 RETURN result;
END;
$$;

CREATE FUNCTION request_curtailment_rig_config_on_rule_change() RETURNS trigger
LANGUAGE plpgsql VOLATILE SECURITY INVOKER AS $$
DECLARE affected_org bigint; affected_source bigint; actor bigint;
BEGIN
 IF TG_OP = 'DELETE' THEN affected_org := OLD.org_id; affected_source := OLD.mqtt_source_id;
 ELSE affected_org := NEW.org_id; affected_source := NEW.mqtt_source_id; END IF;
 SELECT service_user_id INTO actor FROM curtailment_mqtt_source_config WHERE id = affected_source AND organization_id = affected_org;
 IF actor IS NOT NULL THEN PERFORM request_curtailment_rig_config_reconciliation(affected_org, actor, TRUE); END IF;
 RETURN NULL;
END;
$$;
CREATE TRIGGER request_curtailment_rig_config_on_rule_change AFTER INSERT OR UPDATE OF enabled, mqtt_source_id, response_profile_id OR DELETE ON curtailment_automation_rule
FOR EACH ROW EXECUTE FUNCTION request_curtailment_rig_config_on_rule_change();

-- Rig fallback providers follow rule scope, which depends on device placement
-- and rack/group membership. Targets are filtered for eligibility when read.
CREATE FUNCTION request_curtailment_rig_config_for_devices(p_org_id bigint, p_device_ids bigint[])
RETURNS void LANGUAGE plpgsql VOLATILE SECURITY INVOKER AS $$
DECLARE actor bigint; generation bigint;
BEGIN
 IF COALESCE(cardinality(p_device_ids), 0) > 0 THEN
  SELECT src.service_user_id INTO actor
  FROM curtailment_mqtt_source_config src
  WHERE src.organization_id = p_org_id AND src.enabled AND src.service_user_id IS NOT NULL
    AND EXISTS (SELECT 1 FROM curtailment_automation_rule rule WHERE rule.org_id = p_org_id AND rule.mqtt_source_id = src.id AND rule.enabled)
  ORDER BY src.id LIMIT 1;
 END IF;
 -- Without an enabled rule no rig has a provider; rule changes already
 -- requested the full reconciliation that cleared them.
 IF actor IS NULL THEN RETURN; END IF;
 generation := request_curtailment_rig_config_reconciliation(p_org_id, actor, FALSE);
 DELETE FROM curtailment_rig_config_delivery_attempt WHERE organization_id = p_org_id AND device_id = ANY(p_device_ids);
 INSERT INTO curtailment_rig_config_target (organization_id, device_id, requested_generation)
 SELECT p_org_id, d.id, generation FROM device d WHERE d.org_id = p_org_id AND d.id = ANY(p_device_ids) ORDER BY d.id
 ON CONFLICT (organization_id, device_id) DO UPDATE SET requested_generation = EXCLUDED.requested_generation;
END;
$$;

CREATE FUNCTION request_curtailment_rig_config_on_membership_change() RETURNS trigger
LANGUAGE plpgsql VOLATILE SECURITY INVOKER AS $$
DECLARE affected record;
BEGIN
 IF TG_OP = 'INSERT' THEN
  FOR affected IN SELECT org_id, array_agg(DISTINCT device_id) AS device_ids FROM new_rows GROUP BY org_id LOOP
   PERFORM request_curtailment_rig_config_for_devices(affected.org_id, affected.device_ids);
  END LOOP;
 ELSIF TG_OP = 'DELETE' THEN
  FOR affected IN SELECT org_id, array_agg(DISTINCT device_id) AS device_ids FROM old_rows GROUP BY org_id LOOP
   PERFORM request_curtailment_rig_config_for_devices(affected.org_id, affected.device_ids);
  END LOOP;
 ELSE
  FOR affected IN
   SELECT org_id, array_agg(DISTINCT device_id) AS device_ids
   FROM (SELECT org_id, device_id FROM old_rows UNION SELECT org_id, device_id FROM new_rows) changed
   GROUP BY org_id
  LOOP
   PERFORM request_curtailment_rig_config_for_devices(affected.org_id, affected.device_ids);
  END LOOP;
 END IF;
 RETURN NULL;
END;
$$;
CREATE TRIGGER request_curtailment_rig_config_on_membership_insert AFTER INSERT ON device_set_membership
REFERENCING NEW TABLE AS new_rows FOR EACH STATEMENT EXECUTE FUNCTION request_curtailment_rig_config_on_membership_change();
CREATE TRIGGER request_curtailment_rig_config_on_membership_update AFTER UPDATE ON device_set_membership
REFERENCING OLD TABLE AS old_rows NEW TABLE AS new_rows FOR EACH STATEMENT EXECUTE FUNCTION request_curtailment_rig_config_on_membership_change();
CREATE TRIGGER request_curtailment_rig_config_on_membership_delete AFTER DELETE ON device_set_membership
REFERENCING OLD TABLE AS old_rows FOR EACH STATEMENT EXECUTE FUNCTION request_curtailment_rig_config_on_membership_change();

-- Bulk site/building reassignments update many devices in one statement, so
-- device placement requests delivery once per organization per statement.
-- Transition tables cannot be combined with UPDATE OF, so the join filters
-- unrelated device updates.
CREATE FUNCTION request_curtailment_rig_config_on_device_placement_change() RETURNS trigger
LANGUAGE plpgsql VOLATILE SECURITY INVOKER AS $$
DECLARE affected record;
BEGIN
 FOR affected IN
  SELECT new_rows.org_id, array_agg(DISTINCT new_rows.id) AS device_ids
  FROM new_rows JOIN old_rows ON old_rows.id = new_rows.id
  WHERE old_rows.site_id IS DISTINCT FROM new_rows.site_id
     OR old_rows.building_id IS DISTINCT FROM new_rows.building_id
  GROUP BY new_rows.org_id
 LOOP
  PERFORM request_curtailment_rig_config_for_devices(affected.org_id, affected.device_ids);
 END LOOP;
 RETURN NULL;
END;
$$;
CREATE TRIGGER request_curtailment_rig_config_on_device_placement AFTER UPDATE ON device
REFERENCING OLD TABLE AS old_rows NEW TABLE AS new_rows FOR EACH STATEMENT
EXECUTE FUNCTION request_curtailment_rig_config_on_device_placement_change();

CREATE FUNCTION request_curtailment_rig_config_on_placement_change() RETURNS trigger
LANGUAGE plpgsql VOLATILE SECURITY INVOKER AS $$
BEGIN
 IF TG_TABLE_NAME = 'device_set_rack' THEN
  PERFORM request_curtailment_rig_config_for_devices(OLD.org_id, ARRAY(
   SELECT membership.device_id FROM device_set_membership membership
   WHERE membership.org_id = OLD.org_id AND membership.device_set_id = OLD.device_set_id));
 ELSIF TG_TABLE_NAME = 'device_set' THEN
  PERFORM request_curtailment_rig_config_for_devices(OLD.org_id, ARRAY(
   SELECT membership.device_id FROM device_set_membership membership
   WHERE membership.org_id = OLD.org_id AND membership.device_set_id = OLD.id));
 ELSIF TG_TABLE_NAME = 'building' THEN
  PERFORM request_curtailment_rig_config_for_devices(OLD.org_id, ARRAY(
   SELECT d.id FROM device d WHERE d.org_id = OLD.org_id AND d.building_id = OLD.id
   UNION
   SELECT membership.device_id FROM device_set_membership membership
   JOIN device_set_rack placement ON placement.device_set_id = membership.device_set_id AND placement.org_id = membership.org_id
   WHERE membership.org_id = OLD.org_id AND placement.building_id = OLD.id));
 END IF;
 RETURN NULL;
END;
$$;
CREATE TRIGGER request_curtailment_rig_config_on_rack_placement AFTER UPDATE OF building_id ON device_set_rack
FOR EACH ROW WHEN (OLD.building_id IS DISTINCT FROM NEW.building_id)
EXECUTE FUNCTION request_curtailment_rig_config_on_placement_change();
CREATE TRIGGER request_curtailment_rig_config_on_rack_placement_delete AFTER DELETE ON device_set_rack
FOR EACH ROW EXECUTE FUNCTION request_curtailment_rig_config_on_placement_change();
CREATE TRIGGER request_curtailment_rig_config_on_device_set_delete AFTER UPDATE OF deleted_at ON device_set
FOR EACH ROW WHEN (OLD.deleted_at IS DISTINCT FROM NEW.deleted_at)
EXECUTE FUNCTION request_curtailment_rig_config_on_placement_change();
CREATE TRIGGER request_curtailment_rig_config_on_building_delete AFTER UPDATE OF deleted_at ON building
FOR EACH ROW WHEN (OLD.deleted_at IS DISTINCT FROM NEW.deleted_at)
EXECUTE FUNCTION request_curtailment_rig_config_on_placement_change();

CREATE FUNCTION guard_curtailment_mqtt_source_disable() RETURNS trigger
LANGUAGE plpgsql VOLATILE SECURITY INVOKER AS $$
BEGIN
 IF OLD.enabled AND NOT NEW.enabled AND EXISTS (
  SELECT 1 FROM curtailment_automation_rule rule
  WHERE rule.org_id = OLD.organization_id AND rule.mqtt_source_id = OLD.id
  AND curtailment_automation_rule_has_live_event(rule.id, rule.org_id)
 ) THEN
  RAISE EXCEPTION 'source has active curtailment events' USING ERRCODE = '23514', CONSTRAINT = 'ck_curtailment_mqtt_source_no_active_disable';
 END IF;
 RETURN NEW;
END;
$$;
CREATE TRIGGER guard_curtailment_mqtt_source_disable BEFORE UPDATE OF enabled ON curtailment_mqtt_source_config
FOR EACH ROW EXECUTE FUNCTION guard_curtailment_mqtt_source_disable();

-- Rigs keep the organization-wide fallback config delivered before this schema
-- until delivery is requested, and existing rules need a rebind before they
-- grant providers again. Rebuild every organization that may hold one.
SELECT request_curtailment_rig_config_reconciliation(candidate.organization_id, candidate.requested_by, TRUE)
FROM (
 SELECT reconciliation.organization_id, reconciliation.requested_by
 FROM curtailment_rig_config_reconciliation reconciliation
 UNION ALL
 SELECT source.organization_id, source.service_user_id
 FROM (
  SELECT DISTINCT ON (src.organization_id) src.organization_id, src.service_user_id
  FROM curtailment_mqtt_source_config src
  WHERE src.service_user_id IS NOT NULL
    AND NOT EXISTS (SELECT 1 FROM curtailment_rig_config_reconciliation existing WHERE existing.organization_id = src.organization_id)
  ORDER BY src.organization_id, src.id
 ) source
) candidate;

-- fleet-migration: 001062_shared_pool_review_authorization.up.sql
-- Approval is limited to immutable reviews, the initiating session, and the
-- verified credential version. Review expiry and live authority still apply.
CREATE TABLE pool_configuration_review_authorization (
    review_id VARCHAR(36) NOT NULL REFERENCES pool_configuration_review(review_id) ON DELETE CASCADE,
    session_hash TEXT NOT NULL,
    credential_fingerprint TEXT NOT NULL,
    authorized_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (review_id, session_hash)
);

-- fleet-migration: 001063_internal_pool_review_authorization_checkpoint.up.sql
-- requires-shared: 1062
SELECT 1;

-- fleet-migration: 001064_shared_curtailment_schedules.up.sql
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

-- fleet-migration: 001065_internal_curtailment_schedules.up.sql
-- requires-shared: 1064

-- Schedule rules have no MQTT source; keep them visible to every rule read.
DROP VIEW curtailment_automation_rule_binding;
CREATE VIEW curtailment_automation_rule_binding WITH (security_invoker = true) AS
SELECT r.*, rule_revision.response_profile_revision,
       COALESCE(src.source_name, '') AS mqtt_source_name,
       profile.profile_name AS response_profile_name,
       profile.site_id AS response_profile_site_id,
       profile.scope_json AS response_profile_scope_json
FROM curtailment_automation_rule r
JOIN curtailment_automation_rule_profile_revision rule_revision
    ON rule_revision.automation_rule_id = r.id
LEFT JOIN curtailment_mqtt_source_config src
    ON src.id = r.mqtt_source_id
    AND src.organization_id = r.org_id
JOIN curtailment_response_profile profile
    ON profile.id = r.response_profile_id
    AND profile.org_id = r.org_id;

-- fleet-migration: 001066_internal_errors_open_dedup_unique.up.sql
-- requires-shared: 1064

-- Hold the write fence through cleanup and index creation in the checkpoint transaction.
LOCK TABLE errors IN SHARE ROW EXCLUSIVE MODE;

-- Legacy device clocks may have left open occurrences ahead of server time.
UPDATE errors
SET last_seen_at = CURRENT_TIMESTAMP
WHERE closed_at IS NULL
  AND last_seen_at > CURRENT_TIMESTAMP;

-- Keep the oldest open occurrence per dedup key; the unique index cannot be built over duplicates.
UPDATE errors
SET closed_at = CURRENT_TIMESTAMP
WHERE id IN (
    SELECT id
    FROM (
        SELECT
            id,
            row_number() OVER (
                PARTITION BY org_id, device_id, miner_error, component_id, component_type
                ORDER BY id
            ) AS occurrence
        FROM errors
        WHERE closed_at IS NULL
    ) open_errors
    WHERE occurrence > 1
);

CREATE UNIQUE INDEX idx_errors_open_dedup
    ON errors (org_id, device_id, miner_error, component_id, component_type) NULLS NOT DISTINCT
    WHERE closed_at IS NULL;

-- fleet-migration: 001067_shared_pool_removal.up.sql
-- Removal uses the same review, operation and target lifecycle as assignment.
ALTER TABLE pool_configuration_review DROP CONSTRAINT pool_configuration_review_kind_check;
ALTER TABLE pool_configuration_review ADD CONSTRAINT pool_configuration_review_kind_check
    CHECK (kind IN ('edit','assign','remove'));
ALTER TABLE pool_configuration_operation ADD COLUMN kind TEXT NOT NULL DEFAULT 'apply'
    CHECK (kind IN ('apply','clear','unassign'));
ALTER TABLE pool_configuration_operation_target DROP CONSTRAINT pool_configuration_operation_target_state_check;
ALTER TABLE pool_configuration_operation_target ADD CONSTRAINT pool_configuration_operation_target_state_check
    CHECK (state IN ('pending','applying','applied_unverified','verified','failed','different','superseded','completed','uncertain'));

-- fleet-migration: 001068_internal_pool_removal_bindings.up.sql
-- requires-shared: 1067
-- Clearing retains the current assignment until empty readback succeeds.
CREATE OR REPLACE FUNCTION validate_pool_update_attempt() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'UPDATE' AND OLD.physical_attempt_id IS NOT NULL AND
        NEW.physical_attempt_id IS DISTINCT FROM OLD.physical_attempt_id THEN
        RAISE EXCEPTION 'pool update attempt is immutable' USING ERRCODE = '23514';
    END IF;
    IF NEW.physical_attempt_id IS NOT NULL AND (TG_OP = 'INSERT' OR OLD.physical_attempt_id IS NULL) AND NOT EXISTS (
        SELECT 1 FROM device_write_attempt w
        JOIN pool_configuration_assignment a ON a.organization_id = NEW.organization_id
            AND a.target_kind = NEW.target_kind AND a.target_id = NEW.target_id
        WHERE w.attempt_id = NEW.physical_attempt_id AND w.organization_id = a.organization_id
            AND w.device_id = a.device_id AND w.operation_kind = 'mutation' AND w.status = 'running'
            AND (a.operation_id = NEW.operation_id OR EXISTS(SELECT 1 FROM pool_configuration_operation o WHERE o.operation_id=NEW.operation_id AND o.organization_id=NEW.organization_id AND o.kind IN ('clear','unassign') AND o.configuration_id=a.configuration_id AND o.revision_id=a.revision_id)) AND a.version = NEW.assignment_version AND a.retired_at IS NULL
            AND NEW.state = 'applying'
    ) THEN
        RAISE EXCEPTION 'pool update requires its current physical admission' USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;

ALTER TABLE device_write_recovery DROP CONSTRAINT device_write_recovery_prior_status_check;
ALTER TABLE device_write_recovery ADD CONSTRAINT device_write_recovery_prior_status_check
    CHECK (prior_status IN ('running','unknown','complete'));

-- fleet-migration: 001069_internal_topology_cleanup_lock_handoff.up.sql
-- requires-shared: 1000

-- Keep the scheduled budget, but release the organization row lock between
-- small transactions. Ingestion gets a handoff point after each microbatch
-- instead of waiting behind the full cleanup run. The next cleanup microbatch
-- defers through its existing NOWAIT guard when ingestion owns the lock.
ALTER TABLE container_topology_compaction_state
 ADD COLUMN deferrals bigint NOT NULL DEFAULT 0,
 ADD COLUMN last_deferred_at timestamptz;

CREATE OR REPLACE PROCEDURE compact_container_topology_job(job_id integer,config jsonb)
LANGUAGE plpgsql AS $$
DECLARE
 target bigint;
 cutoff timestamptz := now()-INTERVAL '10 days';
 batch_size integer := GREATEST(1,LEAST(COALESCE((config->>'batch_size')::integer,50),50));
 configured_microbatch_size integer := GREATEST(1,LEAST(COALESCE((config->>'microbatch_size')::integer,3),batch_size));
 remaining_budget integer;
 microbatch_size integer;
 microbatch_result jsonb;
 made_progress boolean := false;
 end_run boolean;
BEGIN
 IF NOT COALESCE((config->>'enabled')::boolean,false) THEN RETURN; END IF;
 SELECT device_id INTO target FROM container_topology_compaction_state WHERE next_run_at<=now()
  ORDER BY next_run_at,device_id LIMIT 1 FOR UPDATE SKIP LOCKED;
 IF target IS NULL THEN RETURN; END IF;
 -- Persist only a scheduling lease before beginning destructive work. A worker
 -- timeout or termination leaves this backoff and all completed microbatches
 -- committed, so a slow device cannot monopolize subsequent runs.
 UPDATE container_topology_compaction_state SET next_run_at=now()+INTERVAL '1 minute' WHERE device_id=target;
 COMMIT;
 remaining_budget := batch_size;
 LOOP
  microbatch_size := LEAST(remaining_budget,configured_microbatch_size);
  end_run := false;
  BEGIN
   SELECT compact_container_topology(target,cutoff,microbatch_size) INTO microbatch_result;
   made_progress := made_progress
    OR COALESCE((microbatch_result->>'examined')::integer,0)>0
    OR COALESCE((microbatch_result->>'deleted')::integer,0)>0
    OR COALESCE((microbatch_result->>'pinned')::integer,0)>0;
  EXCEPTION WHEN lock_not_available THEN
   -- Live ingestion owns the lock: this is expected backpressure, not a failure.
   -- A device that already made progress stays due behind older work. A device
   -- that could not start keeps the longer backoff and cannot spin each second.
   UPDATE container_topology_compaction_state SET
    next_run_at=CASE WHEN made_progress THEN now() ELSE now()+INTERVAL '1 minute' END,
    deferrals=deferrals+1,last_deferred_at=now() WHERE device_id=target;
   end_run := true;
  WHEN OTHERS THEN
   -- The failed subtransaction releases all its locks and mutations.
   UPDATE container_topology_compaction_state SET next_run_at=now()+INTERVAL '1 minute',
    failures=failures+1,last_error=SQLERRM WHERE device_id=target;
   end_run := true;
  END;
  IF NOT end_run THEN
   remaining_budget := remaining_budget-microbatch_size;
   IF remaining_budget<=0 THEN
    end_run := true;
   ELSE
    -- Renew the cancellation-safe lease only if compact_container_topology left
    -- more due work. FOUND avoids a separate state query on every microbatch.
    UPDATE container_topology_compaction_state SET next_run_at=now()+INTERVAL '1 minute'
     WHERE device_id=target AND next_run_at<=now();
    IF NOT FOUND THEN
     end_run := true;
    END IF;
   END IF;
  END IF;
  -- This is the lock handoff. It commits progress and the renewed lease before
  -- releasing the organization row for ingestion.
  COMMIT;
  IF end_run THEN RETURN; END IF;
 END LOOP;
END;
$$;

-- fleet-migration: 001070_internal_activation_pool_handoff.up.sql
-- requires-shared: 1067
-- Keep the original source and physical selection after retries and later requests.
CREATE TABLE mining_group_pool_handoff (
 organization_id BIGINT NOT NULL REFERENCES organization(id),
 operation_id UUID NOT NULL,
 device_id BIGINT NOT NULL,
 captured JSONB NOT NULL,
 completed_at TIMESTAMPTZ,
 PRIMARY KEY (organization_id, operation_id),
 FOREIGN KEY (device_id, organization_id) REFERENCES device(id, org_id),
 FOREIGN KEY (organization_id, operation_id) REFERENCES mining_group_request(organization_id, request_id)
);

-- fleet-migration: 001071_internal_hosted_reporting_inputs.up.sql
-- requires-shared: 1000
CREATE TABLE hosted_reporting_authority (
 organization_id bigint NOT NULL,
 account_id bigint NOT NULL,
 user_id bigint NOT NULL,
 can_create boolean NOT NULL,
 all_sites boolean NOT NULL DEFAULT false,
 site_ids bigint[] NOT NULL DEFAULT '{}',
 revision bigint NOT NULL DEFAULT 1 CONSTRAINT ck_hosted_reporting_authority_revision CHECK(revision>0),
 updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 CONSTRAINT pk_hosted_reporting_authority PRIMARY KEY(organization_id,account_id,user_id),
 FOREIGN KEY(organization_id,account_id) REFERENCES hosted_account(organization_id,id),
 FOREIGN KEY(user_id,organization_id) REFERENCES user_organization(user_id,organization_id)
);
-- One editable input set per account. The revision prevents lost updates and
-- stale draft publication; it is not an input-history ledger.
CREATE TABLE hosted_reporting_inputs (
 organization_id bigint NOT NULL,
 account_id bigint NOT NULL,
 revision bigint NOT NULL CONSTRAINT ck_hosted_reporting_inputs_revision CHECK(revision>0),
 updated_by bigint NOT NULL REFERENCES "user"(id),
 source_ref text NOT NULL CONSTRAINT ck_hosted_reporting_inputs_source CHECK(length(btrim(source_ref)) BETWEEN 1 AND 2000),
 configuration jsonb NOT NULL CONSTRAINT ck_hosted_reporting_inputs_configuration CHECK(jsonb_typeof(configuration)='object'),
 site_ids bigint[] NOT NULL,
 updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 CONSTRAINT pk_hosted_reporting_inputs PRIMARY KEY(organization_id,account_id),
 FOREIGN KEY(organization_id,account_id) REFERENCES hosted_account(organization_id,id)
);

-- Facts are current evidence, not a content-history ledger. Short row IDs keep
-- secondary indexes safe when both the fact ID and a reference are long UTF-8.
CREATE TABLE hosted_reporting_fact (
 organization_id bigint NOT NULL,
 account_id bigint NOT NULL,
 kind text COLLATE "C" NOT NULL CONSTRAINT ck_hosted_reporting_fact_kind CHECK(kind IN ('order','coverage','module_coverage','meter','fee','incident','hashrate','pool_observation','obligation','external_result')),
 fact_id text COLLATE "C" NOT NULL CONSTRAINT ck_hosted_reporting_fact_id CHECK(octet_length(fact_id) BETWEEN 1 AND 2000 AND length(btrim(fact_id))>0),
 row_id bigint GENERATED ALWAYS AS IDENTITY,
 start_at timestamptz NOT NULL,
 end_at timestamptz,
 start_seconds bigint NOT NULL,
 start_nanos integer NOT NULL,
 received_at timestamptz,
 site_id bigint,
 container_id bigint,
 asset_kind integer,
 asset_id bigint,
 order_id text COLLATE "C",
 meter_id text COLLATE "C",
 allocation_interval_id bigint,
 episode_id text COLLATE "C",
 board_coverage_ids text[] NOT NULL DEFAULT '{}',
 payload jsonb NOT NULL CONSTRAINT ck_hosted_reporting_fact_payload CHECK(jsonb_typeof(payload)='object'),
 payload_bytes bigint GENERATED ALWAYS AS (octet_length(payload::text)) STORED,
 CONSTRAINT uq_hosted_reporting_fact_row_id UNIQUE(row_id),
 CONSTRAINT pk_hosted_reporting_fact PRIMARY KEY(organization_id,account_id,kind,fact_id),
 FOREIGN KEY(organization_id,account_id) REFERENCES hosted_reporting_inputs(organization_id,account_id)
);
CREATE INDEX idx_hosted_reporting_fact_window ON hosted_reporting_fact(organization_id,account_id,kind,start_at,row_id);
CREATE INDEX idx_hosted_reporting_fact_asset ON hosted_reporting_fact(organization_id,account_id,asset_kind,asset_id,start_seconds,start_nanos,row_id);
CREATE INDEX idx_hosted_reporting_fact_order ON hosted_reporting_fact(organization_id,account_id,order_id,row_id);
CREATE INDEX idx_hosted_reporting_fact_meter ON hosted_reporting_fact(organization_id,account_id,meter_id,start_seconds,start_nanos,row_id);
CREATE INDEX idx_hosted_reporting_fact_allocated_meter ON hosted_reporting_fact(organization_id,account_id,row_id) WHERE kind='meter' AND payload->>'allocationFraction'<>'';
CREATE INDEX idx_hosted_reporting_fact_allocation ON hosted_reporting_fact(organization_id,account_id,allocation_interval_id,row_id);
CREATE INDEX idx_hosted_reporting_fact_episode ON hosted_reporting_fact(organization_id,account_id,episode_id,row_id);
CREATE INDEX idx_hosted_reporting_fact_container ON hosted_reporting_fact(organization_id,account_id,container_id,row_id);
CREATE INDEX idx_hosted_reporting_fact_boards ON hosted_reporting_fact USING gin(board_coverage_ids);
CREATE INDEX idx_hosted_reporting_fact_rows ON hosted_reporting_fact(organization_id,account_id,row_id);
CREATE INDEX idx_hosted_reporting_fact_witnesses ON hosted_reporting_fact(organization_id,account_id,row_id) WHERE site_id IS NOT NULL;
CREATE TABLE hosted_reporting_fact_site (
 organization_id bigint NOT NULL,
 account_id bigint NOT NULL,
 site_id bigint NOT NULL,
 fact_count bigint NOT NULL CONSTRAINT ck_hosted_reporting_fact_site_count CHECK(fact_count>=0),
 CONSTRAINT pk_hosted_reporting_fact_site PRIMARY KEY(organization_id,account_id,site_id),
 FOREIGN KEY(organization_id,account_id) REFERENCES hosted_reporting_inputs(organization_id,account_id)
);

-- fleet-migration: 001072_internal_topology_cleanup_runtime_guard.up.sql
-- requires-shared: 1000

-- Stay inside the scheduler's two-second deadline even when several otherwise
-- successful microbatches have variable planning, FK-check, or commit costs.
-- Lock waits use a smaller bound so they become ordinary deferrals instead of
-- consuming the whole worker deadline.
ALTER TABLE container_topology_compaction_state
 ADD COLUMN runtime_yields bigint NOT NULL DEFAULT 0,
 ADD COLUMN last_runtime_yield_at timestamptz;

CREATE OR REPLACE PROCEDURE compact_container_topology_job(job_id integer,config jsonb)
LANGUAGE plpgsql AS $$
DECLARE
 target bigint;
 cutoff timestamptz := now()-INTERVAL '10 days';
 batch_size integer := GREATEST(1,LEAST(COALESCE((config->>'batch_size')::integer,50),50));
 microbatch_limit integer := GREATEST(1,LEAST(COALESCE((config->>'microbatch_size')::integer,3),batch_size));
 run_budget_ms integer := GREATEST(100,LEAST(COALESCE((config->>'run_budget_ms')::integer,1000),1000));
 lock_timeout_ms integer := GREATEST(1,LEAST(COALESCE((config->>'lock_timeout_ms')::integer,250),250));
 run_started_at timestamptz := clock_timestamp();
 remaining_rows integer;
 microbatch_size integer;
 microbatch_result jsonb;
 made_progress boolean := false;
 end_run boolean;
BEGIN
 IF NOT COALESCE((config->>'enabled')::boolean,false) THEN RETURN; END IF;
 SELECT device_id INTO target FROM container_topology_compaction_state WHERE next_run_at<=now()
  ORDER BY next_run_at,device_id LIMIT 1 FOR UPDATE SKIP LOCKED;
 IF target IS NULL THEN RETURN; END IF;
 -- Persist only a scheduling lease before beginning destructive work. A worker
 -- timeout or termination leaves this backoff and all completed microbatches
 -- committed, so a slow device cannot monopolize subsequent runs.
 UPDATE container_topology_compaction_state SET next_run_at=now()+INTERVAL '1 minute' WHERE device_id=target;
 COMMIT;
 remaining_rows := batch_size;
 LOOP
  -- Commit latency counts against the worker deadline. Recheck after the
  -- initial lease commit and every handoff commit before starting more work.
  IF extract(epoch FROM clock_timestamp()-run_started_at)*1000>=run_budget_ms THEN
   UPDATE container_topology_compaction_state SET next_run_at=now(),
    runtime_yields=runtime_yields+1,last_runtime_yield_at=clock_timestamp()
    WHERE device_id=target;
   COMMIT;
   RETURN;
  END IF;
  microbatch_size := LEAST(remaining_rows,microbatch_limit);
  end_run := false;
  -- SET LOCAL resets at every handoff commit. Reapply it before each bounded
  -- microbatch, including FK checks that can wait behind an in-flight writer.
  PERFORM set_config('lock_timeout',lock_timeout_ms::text||'ms',true);
  BEGIN
   SELECT compact_container_topology(target,cutoff,microbatch_size) INTO microbatch_result;
   made_progress := made_progress
    OR COALESCE((microbatch_result->>'examined')::integer,0)>0
    OR COALESCE((microbatch_result->>'deleted')::integer,0)>0
    OR COALESCE((microbatch_result->>'pinned')::integer,0)>0;
  EXCEPTION WHEN lock_not_available THEN
   -- Both the nonblocking organization guard and bounded FK lock waits use
   -- SQLSTATE 55P03. They are expected backpressure, not cleanup failures.
   UPDATE container_topology_compaction_state SET
    next_run_at=CASE WHEN made_progress THEN now() ELSE now()+INTERVAL '1 minute' END,
    deferrals=deferrals+1,last_deferred_at=now() WHERE device_id=target;
   end_run := true;
  WHEN OTHERS THEN
   -- The failed subtransaction releases all its locks and mutations.
   UPDATE container_topology_compaction_state SET next_run_at=now()+INTERVAL '1 minute',
    failures=failures+1,last_error=SQLERRM WHERE device_id=target;
   end_run := true;
  END;
  IF NOT end_run THEN
   remaining_rows := remaining_rows-microbatch_size;
   IF remaining_rows<=0 THEN
    end_run := true;
   ELSE
    -- Renew the cancellation-safe lease only if compact_container_topology left
    -- more due work. Yielded work stays due behind older controllers.
    IF extract(epoch FROM clock_timestamp()-run_started_at)*1000>=run_budget_ms THEN
     UPDATE container_topology_compaction_state SET next_run_at=now(),
      runtime_yields=runtime_yields+1,last_runtime_yield_at=clock_timestamp()
      WHERE device_id=target AND next_run_at<=now();
     end_run := true;
    ELSE
     UPDATE container_topology_compaction_state SET next_run_at=now()+INTERVAL '1 minute'
      WHERE device_id=target AND next_run_at<=now();
     IF NOT FOUND THEN
      end_run := true;
     END IF;
    END IF;
   END IF;
  END IF;
  -- This is the lock handoff. It commits progress and the renewed lease before
  -- releasing the organization row for ingestion.
  COMMIT;
  IF end_run THEN RETURN; END IF;
 END LOOP;
END;
$$;

-- fleet-migration: 001073_internal_ct1_firmware_observation.up.sql
-- requires-shared: 1064

ALTER TABLE container_inventory
    ADD COLUMN hashboard_firmware_version text NOT NULL DEFAULT '',
    ADD COLUMN hashboard_firmware_mixed boolean NOT NULL DEFAULT false;

CREATE FUNCTION release_channel_firmware_version(device_id bigint, device_class integer, controller_version text)
RETURNS SETOF text LANGUAGE sql STABLE PARALLEL SAFE ROWS 1 AS $$
    SELECT CASE WHEN device_class = 2 THEN (
        SELECT CASE WHEN i.hashboard_firmware_mixed THEN '' ELSE NULLIF(i.hashboard_firmware_version, '') END
        FROM container_inventory i
        WHERE i.device_id = $1 AND i.observed_at > statement_timestamp() - INTERVAL '2 minutes'
          AND i.observed_at <= statement_timestamp() + INTERVAL '5 seconds'
    ) ELSE COALESCE(controller_version, '') END;
$$;

-- fleet-migration: 001074_shared_frigate_updates.up.sql
-- Fleet-triggered Frigate software updates for CT1 containers.
-- controller:software_update is not granted to the ADMIN built-in; SUPER_ADMIN
-- receives it from startup reconciliation, and operators grant it through custom roles.
INSERT INTO permission (key, description) VALUES
    ('controller:software_update', 'Upload Frigate software packages and update CT1 controller software. Updates stop mining controllers while they install.')
ON CONFLICT (key) DO NOTHING;

-- Uploaded Frigate Debian packages. The signed release statement and signature
-- are kept verbatim so every controller can re-verify them. The package bytes
-- live on disk under the Fleet host's package store.
CREATE TABLE frigate_update_package (
    id UUID PRIMARY KEY,
    org_id BIGINT NOT NULL REFERENCES organization (id),
    filename TEXT NOT NULL,
    version TEXT NOT NULL,
    architecture TEXT NOT NULL,
    sha256 TEXT NOT NULL CHECK (sha256 ~ '^[0-9a-f]{64}$'),
    size_bytes BIGINT NOT NULL CHECK (size_bytes > 0),
    repository TEXT NOT NULL,
    release_key_id TEXT NOT NULL,
    min_installed_version TEXT NOT NULL DEFAULT '',
    issued_at TIMESTAMPTZ NOT NULL,
    statement BYTEA NOT NULL,
    signature BYTEA NOT NULL,
    uploaded_by BIGINT REFERENCES "user" (id),
    uploaded_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_at TIMESTAMPTZ
);

CREATE UNIQUE INDEX idx_frigate_update_package_org_sha256
    ON frigate_update_package (org_id, sha256) WHERE deleted_at IS NULL;

-- One update of one container. The job id is the staged job id on every
-- controller and the Frigate rollout id.
CREATE TABLE frigate_update_job (
    id UUID PRIMARY KEY,
    org_id BIGINT NOT NULL REFERENCES organization (id),
    device_id BIGINT NOT NULL REFERENCES device (id),
    package_id UUID NOT NULL REFERENCES frigate_update_package (id),
    state TEXT NOT NULL CHECK (state IN (
        'staging', 'staged', 'starting', 'running',
        'succeeded', 'failed', 'cancelled', 'verified', 'abandoned')),
    stage_only BOOLEAN NOT NULL DEFAULT FALSE,
    rollout_phase TEXT NOT NULL DEFAULT '',
    from_version TEXT NOT NULL DEFAULT '',
    target_version TEXT NOT NULL,
    expected_instance_ids TEXT[] NOT NULL CHECK (cardinality(expected_instance_ids) > 0),
    driver_instance_id TEXT NOT NULL DEFAULT '',
    error_code TEXT NOT NULL DEFAULT '',
    error_message TEXT NOT NULL DEFAULT '',
    next_step TEXT NOT NULL DEFAULT '',
    retryable BOOLEAN NOT NULL DEFAULT FALSE,
    cancellable BOOLEAN NOT NULL DEFAULT TRUE,
    disruption_started BOOLEAN NOT NULL DEFAULT FALSE,
    served_by_maintenance BOOLEAN NOT NULL DEFAULT FALSE,
    -- The operator's idempotency key for StartUpdate.
    request_key TEXT NOT NULL,
    -- Set before the first Start is sent. A Start whose outcome is unknown is
    -- reconciled through Status, then repeated with the same derived key.
    start_attempted BOOLEAN NOT NULL DEFAULT FALSE,
    -- When the first Start was attempted. The start deadline counts from here.
    start_attempted_at TIMESTAMPTZ,
    -- A runner that is about to send Start holds this lease while the call can
    -- still be in flight. An operator cancel that would complete locally waits
    -- for it to expire, so a cancelled job is never started afterwards.
    start_lease_until TIMESTAMPTZ,
    -- An operator cancel that arrived while the start lease was live. The runner
    -- takes no new start lease once this is set; after the lease has expired it
    -- reads Frigate's status and either completes the cancel locally (Frigate has
    -- no record of the job) or sends Frigate a Cancel. cancel_request_key is the
    -- operator's idempotency key, which frigate_update_action also records.
    cancel_requested_at TIMESTAMPTZ,
    cancel_request_key TEXT NOT NULL DEFAULT '',
    cancel_request_actor TEXT NOT NULL DEFAULT '',
    -- Number of operator retry/cancel actions; it numbers the derived Frigate
    -- idempotency keys. Each action is recorded in frigate_update_action.
    action_counter INTEGER NOT NULL DEFAULT 0 CHECK (action_counter >= 0),
    -- Staged copies on the controllers still need a best-effort abort.
    cleanup_pending BOOLEAN NOT NULL DEFAULT FALSE,
    -- Last failed abort attempt. The sweep tries the least recently attempted
    -- job first so one unreachable container cannot starve the others.
    cleanup_attempted_at TIMESTAMPTZ,
    actor_user_id BIGINT NOT NULL REFERENCES "user" (id),
    actor_external_id TEXT NOT NULL,
    actor_username TEXT NOT NULL,
    rollout_updated_at TIMESTAMPTZ,
    last_polled_at TIMESTAMPTZ,
    last_contact_at TIMESTAMPTZ,
    unreachable_since TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    completed_at TIMESTAMPTZ,
    CONSTRAINT uq_frigate_update_job_org_request_key UNIQUE (org_id, request_key)
);

-- A container has at most one blocking job: one in progress, or one that
-- failed after stopping controllers and can still be retried.
CREATE UNIQUE INDEX idx_frigate_update_job_device_blocking
    ON frigate_update_job (device_id)
    WHERE state IN ('staging', 'staged', 'starting', 'running')
       OR (state = 'failed' AND retryable);

CREATE INDEX idx_frigate_update_job_device_created
    ON frigate_update_job (device_id, created_at DESC);

CREATE INDEX idx_frigate_update_job_package
    ON frigate_update_job (package_id);

CREATE TABLE frigate_update_instance (
    job_id UUID NOT NULL REFERENCES frigate_update_job (id) ON DELETE CASCADE,
    instance_id TEXT NOT NULL,
    stage_state TEXT NOT NULL DEFAULT 'pending' CHECK (stage_state IN ('pending', 'staging', 'staged', 'failed')),
    manifest_sha256 TEXT NOT NULL DEFAULT '',
    staged_bytes BIGINT NOT NULL DEFAULT 0 CHECK (staged_bytes >= 0),
    size_bytes BIGINT NOT NULL CHECK (size_bytes > 0),
    agent_phase TEXT NOT NULL DEFAULT '',
    last_contact TIMESTAMPTZ,
    contact_stale BOOLEAN NOT NULL DEFAULT FALSE,
    running_version TEXT NOT NULL DEFAULT '',
    migration_executed BOOLEAN NOT NULL DEFAULT FALSE,
    error_code TEXT NOT NULL DEFAULT '',
    error_message TEXT NOT NULL DEFAULT '',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (job_id, instance_id)
);

-- Every operator retry/cancel action, keyed by the operator's idempotency key,
-- so any repeated key replays its original outcome, not only the latest one.
-- The row is written before Frigate is called. counter numbers the derived
-- Frigate key; local is set once the action finished without Frigate.
CREATE TABLE frigate_update_action (
    job_id UUID NOT NULL REFERENCES frigate_update_job (id) ON DELETE CASCADE,
    kind TEXT NOT NULL CHECK (kind IN ('retry', 'cancel')),
    request_key TEXT NOT NULL,
    counter INTEGER NOT NULL CHECK (counter >= 0),
    local BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (job_id, kind, request_key)
);

-- fleet-migration: 001075_internal_frigate_updates_checkpoint.up.sql
-- requires-shared: 1074
SELECT 1;

-- fleet-migration: 001076_internal_telemetry_freshness.up.sql
-- requires-shared: 1000

CREATE TABLE telemetry_freshness (
    device_id bigint CONSTRAINT pk_telemetry_freshness PRIMARY KEY REFERENCES device(id) ON DELETE CASCADE,
    observed_at timestamptz,
    source_node_id bigint NOT NULL DEFAULT 0 CHECK (source_node_id >= 0),
    ingested_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    observation_known boolean NOT NULL,
    CONSTRAINT ck_telemetry_freshness_observation CHECK (NOT observation_known OR observed_at IS NOT NULL)
);
CREATE TABLE telemetry_freshness_exclusion (
    device_id bigint CONSTRAINT pk_telemetry_freshness_exclusion PRIMARY KEY REFERENCES device(id) ON DELETE CASCADE,
    reason text NOT NULL CHECK (length(trim(reason)) BETWEEN 1 AND 256),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    expires_at timestamptz NOT NULL,
    CONSTRAINT ck_telemetry_freshness_exclusion_expiry CHECK (expires_at > created_at)
);
-- One compact row per inventory target, never a scan of raw measurement history.
-- Paused hashing and unreachable nodes remain expected. Maintenance is explicit
-- and expires; pairing/assignment starts a five-minute admission grace period.
CREATE VIEW telemetry_freshness_summary AS
WITH inventory AS (
 SELECT d.org_id, COALESCE(d.site_id,0)::bigint AS site_id,
        COALESCE(fnd.fleet_node_id,0)::bigint AS source_node_id, dd.device_class,
        GREATEST(COALESCE(dp.paired_at,dp.created_at,d.created_at,'epoch'::timestamptz),COALESCE(fnd.assigned_at,'epoch'::timestamptz)) AS expected_since,
        tf.observed_at, tf.observation_known, tf.source_node_id AS observed_source_node_id,
        (ex.expires_at > CURRENT_TIMESTAMP) IS TRUE AS excluded
 FROM device d
 JOIN device_pairing dp ON dp.device_id=d.id
 JOIN discovered_device dd ON dd.id=d.discovered_device_id
 LEFT JOIN fleet_node_device fnd ON fnd.device_id=d.id AND fnd.org_id=d.org_id
 LEFT JOIN telemetry_freshness tf ON tf.device_id=d.id
 LEFT JOIN telemetry_freshness_exclusion ex ON ex.device_id=d.id
 WHERE d.deleted_at IS NULL AND dp.pairing_status IN ('PAIRED','DEFAULT_PASSWORD','AUTHENTICATION_NEEDED')
), classified AS (
 SELECT *, expected_since <= CURRENT_TIMESTAMP - INTERVAL '5 minutes' AND NOT excluded AS expected,
        observation_known IS TRUE AND observed_source_node_id=source_node_id AND observed_at >= expected_since AND observed_at <= CURRENT_TIMESTAMP AS known
 FROM inventory
)
SELECT org_id,site_id,source_node_id,device_class,
 count(*) FILTER (WHERE expected)::bigint AS expected_count,
 count(*) FILTER (WHERE expected AND known AND observed_at >= CURRENT_TIMESTAMP - INTERVAL '2 minutes')::bigint AS fresh_count,
 count(*) FILTER (WHERE expected AND known AND observed_at < CURRENT_TIMESTAMP - INTERVAL '2 minutes')::bigint AS stale_count,
 count(*) FILTER (WHERE expected AND NOT known)::bigint AS unknown_count,
 count(*) FILTER (WHERE excluded)::bigint AS excluded_count,
 count(*) FILTER (WHERE NOT excluded AND NOT expected)::bigint AS grace_count,
 COALESCE(max(EXTRACT(EPOCH FROM CURRENT_TIMESTAMP-observed_at)) FILTER (WHERE expected AND known),0)::double precision AS oldest_age_seconds
FROM classified GROUP BY org_id,site_id,source_node_id,device_class;

-- fleet-migration: 001077_internal_hourly_reporting_evidence.up.sql
-- requires-shared: 1000
-- Hourly hosted reporting evidence: one append-only row per allocated interval
-- and closed hour, with corrections appended as revisions. Hours are UTC
-- time_bucket hours, matching hosted_account_history_hourly.

-- Stop application and scheduled writers for this upgrade. Drain source writes
-- in the 1055 order before installing correction markers on the sample table.
LOCK TABLE device_metrics,miner_state_snapshots,container_module_metrics,
 container_module_diagnostic_observation,hashboard_metrics,hashboard_diagnostic_observation,
 hosted_account_history_sample
 IN SHARE ROW EXCLUSIVE MODE;

-- Retire the raw per-poll hold from 1055 and delete its events; the feature is
-- not live. Its epoch marked the raw-hold start, so it goes too. The allocation
-- snapshot, reject_hosted_reporting_evidence_mutation() and the narrowed
-- record_hosted_hashboard_history trigger stay: hourly evidence reuses them.
-- Operators paused 1055 capture by dropping its trigger, so it may be gone.
DROP VIEW hosted_account_reporting_capture_status;
DROP TRIGGER IF EXISTS record_hosted_reporting_evidence ON hosted_account_history_sample;
DROP FUNCTION record_hosted_reporting_evidence();
DROP TABLE hosted_account_reporting_evidence;
DROP TABLE hosted_account_reporting_evidence_epoch;

-- Closer state. closing_through and closed_through are exclusive hour-end
-- watermarks: hours with bucket < closing_through are closing (writers enqueue
-- markers for them), and hours with bucket < closed_through have evidence rows
-- or a covering gap row. Hours before hold_started_at are outside the hold.
CREATE TABLE hosted_account_reporting_hour_control (
 singleton boolean CONSTRAINT pk_hosted_reporting_hour_control PRIMARY KEY DEFAULT true
  CONSTRAINT ck_hosted_reporting_hour_control_singleton CHECK(singleton),
 hold_started_at timestamptz NOT NULL,
 closing_through timestamptz NOT NULL,
 closed_through timestamptz NOT NULL,
 last_success_at timestamptz,
 CONSTRAINT ck_hosted_reporting_hour_control_aligned CHECK(time_bucket(INTERVAL '1 hour',hold_started_at)=hold_started_at
  AND time_bucket(INTERVAL '1 hour',closing_through)=closing_through
  AND time_bucket(INTERVAL '1 hour',closed_through)=closed_through),
 CONSTRAINT ck_hosted_reporting_hour_control_order CHECK(hold_started_at<=closed_through AND closed_through<=closing_through)
);
-- The hold starts at the first full hour after this migration; nothing is backfilled.
INSERT INTO hosted_account_reporting_hour_control(singleton,hold_started_at,closing_through,closed_through)
SELECT true,start,start,start FROM (SELECT time_bucket(INTERVAL '1 hour',clock_timestamp())+INTERVAL '1 hour' AS start) hold;

-- Per-metric aggregates mirror hosted_account_history_hourly. Report counts
-- include samples without a value, so "no report" (0) differs from "reported
-- a missing value" (report count > 0, count 0, NULL sum). The capture window
-- is the interval's overlap with the hour at capture time, NULL without overlap.
CREATE TABLE hosted_account_reporting_hour (
 interval_id bigint NOT NULL CONSTRAINT fk_hosted_reporting_hour_snapshot
  REFERENCES hosted_account_reporting_allocation_snapshot(interval_id),
 bucket timestamptz NOT NULL CONSTRAINT ck_hosted_reporting_hour_bucket CHECK(time_bucket(INTERVAL '1 hour',bucket)=bucket),
 revision integer NOT NULL CONSTRAINT ck_hosted_reporting_hour_revision CHECK(revision>=1),
 captured_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 capture_reason text NOT NULL CONSTRAINT ck_hosted_reporting_hour_reason CHECK(capture_reason IN ('closed','unattributable')),
 no_source_data boolean NOT NULL,
 window_start timestamptz,
 window_end timestamptz,
 hashrate_report_count integer NOT NULL,
 hashrate_count integer NOT NULL,
 hashrate_positive_count integer NOT NULL,
 hashrate_sum double precision,
 hashrate_complete boolean,
 hashrate_source_oldest_at timestamptz,
 hashrate_source_newest_at timestamptz,
 power_report_count integer NOT NULL,
 power_count integer NOT NULL,
 power_sum double precision,
 power_complete boolean,
 power_source_oldest_at timestamptz,
 power_source_newest_at timestamptz,
 temperature_report_count integer NOT NULL,
 temperature_count integer NOT NULL,
 temperature_sum double precision,
 temperature_latest_value double precision,
 temperature_latest_at timestamptz,
 temperature_source_oldest_at timestamptz,
 temperature_source_newest_at timestamptz,
 state_report_count integer NOT NULL,
 state_count integer NOT NULL,
 state_latest_value double precision,
 state_latest_at timestamptz,
 state_source_oldest_at timestamptz,
 state_source_newest_at timestamptz,
 CONSTRAINT pk_hosted_reporting_hour PRIMARY KEY(interval_id,bucket,revision),
 CONSTRAINT ck_hosted_reporting_hour_window CHECK(CASE WHEN window_start IS NULL OR window_end IS NULL
  THEN window_start IS NULL AND window_end IS NULL
  ELSE window_start>=bucket AND window_start<window_end AND window_end-bucket<=INTERVAL '1 hour' END)
);

-- Hours the closer will never write, covering bucket >= from_bucket and < to_bucket.
CREATE TABLE hosted_account_reporting_hour_gap (
 gap_id bigint GENERATED ALWAYS AS IDENTITY CONSTRAINT pk_hosted_reporting_hour_gap PRIMARY KEY,
 from_bucket timestamptz NOT NULL,
 to_bucket timestamptz NOT NULL,
 reason text NOT NULL CONSTRAINT ck_hosted_reporting_hour_gap_reason CHECK(reason<>''),
 recorded_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 CONSTRAINT ck_hosted_reporting_hour_gap_bounds CHECK(from_bucket<to_bucket
  AND time_bucket(INTERVAL '1 hour',from_bucket)=from_bucket AND time_bucket(INTERVAL '1 hour',to_bucket)=to_bucket)
);

CREATE TRIGGER hosted_reporting_hour_immutable
 BEFORE UPDATE OR DELETE OR TRUNCATE ON hosted_account_reporting_hour
 FOR EACH STATEMENT EXECUTE FUNCTION reject_hosted_reporting_evidence_mutation();
CREATE TRIGGER hosted_reporting_hour_gap_immutable
 BEFORE UPDATE OR DELETE OR TRUNCATE ON hosted_account_reporting_hour_gap
 FOR EACH STATEMENT EXECUTE FUNCTION reject_hosted_reporting_evidence_mutation();

-- Dirty interval-hours awaiting recomputation. Markers are deliberately not
-- deduplicated: every enqueue inserts its own row, and a claim deletes only the
-- rows it can see, so a writer still in flight keeps its marker for the next run.
CREATE TABLE hosted_account_reporting_hour_marker (
 marker_id bigint GENERATED ALWAYS AS IDENTITY CONSTRAINT pk_hosted_reporting_hour_marker PRIMARY KEY,
 interval_id bigint NOT NULL,
 bucket timestamptz NOT NULL CONSTRAINT ck_hosted_reporting_hour_marker_bucket CHECK(time_bucket(INTERVAL '1 hour',bucket)=bucket),
 enqueued_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
-- Lookup path for the revision claim, which deletes every marker of each claimed interval-hour.
CREATE INDEX idx_hosted_account_reporting_hour_marker_interval_bucket
 ON hosted_account_reporting_hour_marker(interval_id,bucket);

-- Shared per-(interval, hour) aggregate over normalized samples with time in
-- [from_bucket, to_bucket); pass hour-aligned bounds. A NULL interval_ids
-- summarizes every interval. Only interval-hours with samples are returned.
-- Expressions mirror hosted_account_history_hourly (which cannot call this
-- function); sums are ordered and latest values break time ties on
-- (origin_collected_at, origin_key, origin_time, source_id), so identical
-- samples always give identical rows.
CREATE FUNCTION hosted_reporting_hour_summary(from_bucket timestamptz,to_bucket timestamptz,interval_ids bigint[] DEFAULT NULL)
RETURNS TABLE(interval_id bigint,bucket timestamptz,
 hashrate_report_count integer,hashrate_count integer,hashrate_positive_count integer,hashrate_sum double precision,
 hashrate_complete boolean,hashrate_source_oldest_at timestamptz,hashrate_source_newest_at timestamptz,
 power_report_count integer,power_count integer,power_sum double precision,
 power_complete boolean,power_source_oldest_at timestamptz,power_source_newest_at timestamptz,
 temperature_report_count integer,temperature_count integer,temperature_sum double precision,
 temperature_latest_value double precision,temperature_latest_at timestamptz,
 temperature_source_oldest_at timestamptz,temperature_source_newest_at timestamptz,
 state_report_count integer,state_count integer,state_latest_value double precision,state_latest_at timestamptz,
 state_source_oldest_at timestamptz,state_source_newest_at timestamptz)
LANGUAGE sql STABLE AS $$
 SELECT s.interval_id,time_bucket(INTERVAL '1 hour',s.time),
  (count(*) FILTER(WHERE s.metric=hosted_history_metric_code('hashrate')))::integer,
  (count(s.value) FILTER(WHERE s.metric=hosted_history_metric_code('hashrate')))::integer,
  (count(s.value) FILTER(WHERE s.metric=hosted_history_metric_code('hashrate') AND s.value>0))::integer,
  sum(s.value ORDER BY s.time,s.source_id,s.origin_key COLLATE "C",s.origin_time,s.origin_collected_at)
   FILTER(WHERE s.metric=hosted_history_metric_code('hashrate')),
  bool_and(s.complete) FILTER(WHERE s.metric=hosted_history_metric_code('hashrate')),
  min(s.source_oldest_at) FILTER(WHERE s.metric=hosted_history_metric_code('hashrate')),
  max(s.source_newest_at) FILTER(WHERE s.metric=hosted_history_metric_code('hashrate')),
  (count(*) FILTER(WHERE s.metric=hosted_history_metric_code('power')))::integer,
  (count(s.value) FILTER(WHERE s.metric=hosted_history_metric_code('power')))::integer,
  sum(s.value ORDER BY s.time,s.source_id,s.origin_key COLLATE "C",s.origin_time,s.origin_collected_at)
   FILTER(WHERE s.metric=hosted_history_metric_code('power')),
  bool_and(s.complete) FILTER(WHERE s.metric=hosted_history_metric_code('power')),
  min(s.source_oldest_at) FILTER(WHERE s.metric=hosted_history_metric_code('power')),
  max(s.source_newest_at) FILTER(WHERE s.metric=hosted_history_metric_code('power')),
  (count(*) FILTER(WHERE s.metric=hosted_history_metric_code('temperature')))::integer,
  (count(s.value) FILTER(WHERE s.metric=hosted_history_metric_code('temperature')))::integer,
  sum(s.value ORDER BY s.time,s.source_id,s.origin_key COLLATE "C",s.origin_time,s.origin_collected_at)
   FILTER(WHERE s.metric=hosted_history_metric_code('temperature')),
  (array_agg(s.value ORDER BY s.time DESC,s.origin_collected_at DESC,s.origin_key COLLATE "C" DESC,s.origin_time DESC,s.source_id DESC)
   FILTER(WHERE s.metric=hosted_history_metric_code('temperature') AND s.value IS NOT NULL))[1],
  max(s.time) FILTER(WHERE s.metric=hosted_history_metric_code('temperature') AND s.value IS NOT NULL),
  min(s.source_oldest_at) FILTER(WHERE s.metric=hosted_history_metric_code('temperature')),
  max(s.source_newest_at) FILTER(WHERE s.metric=hosted_history_metric_code('temperature')),
  (count(*) FILTER(WHERE s.metric=hosted_history_metric_code('state')))::integer,
  (count(s.value) FILTER(WHERE s.metric=hosted_history_metric_code('state')))::integer,
  (array_agg(s.value ORDER BY s.time DESC,s.origin_collected_at DESC,s.origin_key COLLATE "C" DESC,s.origin_time DESC,s.source_id DESC)
   FILTER(WHERE s.metric=hosted_history_metric_code('state') AND s.value IS NOT NULL))[1],
  max(s.time) FILTER(WHERE s.metric=hosted_history_metric_code('state') AND s.value IS NOT NULL),
  min(s.source_oldest_at) FILTER(WHERE s.metric=hosted_history_metric_code('state')),
  max(s.source_newest_at) FILTER(WHERE s.metric=hosted_history_metric_code('state'))
 -- Mutually exclusive branches, so listed interval_ids are an index condition
 -- and a NULL list stays one time-range scan.
 FROM (SELECT * FROM hosted_account_history_sample WHERE interval_ids IS NULL AND time>=from_bucket AND time<to_bucket
  UNION ALL
  SELECT * FROM hosted_account_history_sample WHERE interval_id=ANY(interval_ids) AND time>=from_bucket AND time<to_bucket) s
 GROUP BY s.interval_id,time_bucket(INTERVAL '1 hour',s.time)
$$;

-- First hour still inside the correction window at as_of. Raw samples are kept
-- 10 days; nine days keeps every recomputed hour fully retained and matches the
-- hourly aggregate refresh window. Marker triggers always use the 9-day limit,
-- so a job may shorten its window but never extend it.
CREATE FUNCTION hosted_reporting_hour_window_floor(as_of timestamptz,correction_window interval DEFAULT INTERVAL '9 days')
RETURNS timestamptz LANGUAGE sql STABLE AS $$
 SELECT time_bucket(INTERVAL '1 hour',as_of-LEAST(correction_window,INTERVAL '9 days'))
$$;

-- Expected (kind, source, metric) streams match the 1044/1049 writers: miner
-- metrics/state 1/2, module metrics/state 3/4, hashboard metrics/state 5/6.
CREATE FUNCTION hosted_reporting_expected_streams()
RETURNS TABLE(kind integer,source_id smallint,metric smallint)
LANGUAGE sql IMMUTABLE PARALLEL SAFE AS $$
 SELECT 5,1::smallint,hosted_history_metric_code('hashrate')
 UNION ALL SELECT 5,1::smallint,hosted_history_metric_code('power')
 UNION ALL SELECT 5,1::smallint,hosted_history_metric_code('temperature')
 UNION ALL SELECT 5,2::smallint,hosted_history_metric_code('state')
 UNION ALL SELECT 6,3::smallint,hosted_history_metric_code('hashrate')
 UNION ALL SELECT 6,3::smallint,hosted_history_metric_code('power')
 UNION ALL SELECT 6,3::smallint,hosted_history_metric_code('temperature')
 UNION ALL SELECT 6,4::smallint,hosted_history_metric_code('state')
 UNION ALL SELECT 7,5::smallint,hosted_history_metric_code('hashrate')
 UNION ALL SELECT 7,5::smallint,hosted_history_metric_code('power')
 UNION ALL SELECT 7,5::smallint,hosted_history_metric_code('temperature')
 UNION ALL SELECT 7,6::smallint,hosted_history_metric_code('state')
$$;

-- Candidate evidence for every (interval, hour) in [from_bucket, to_bucket),
-- without revision metadata. A NULL interval_ids gives the closer's driver set:
-- intervals overlapping the hour plus intervals with samples in it. Listed
-- interval_ids are recomputed for every hour, overlapping or not. Silent hours
-- get zero counts and NULL values. no_source_data is true when none of the
-- interval kind's sources delivered any sample at its site, within its
-- organization, in the hour. Intervals without a site form one group per
-- organization.
CREATE FUNCTION hosted_reporting_hour_rows(from_bucket timestamptz,to_bucket timestamptz,interval_ids bigint[] DEFAULT NULL)
RETURNS TABLE(interval_id bigint,bucket timestamptz,no_source_data boolean,window_start timestamptz,window_end timestamptz,
 hashrate_report_count integer,hashrate_count integer,hashrate_positive_count integer,hashrate_sum double precision,
 hashrate_complete boolean,hashrate_source_oldest_at timestamptz,hashrate_source_newest_at timestamptz,
 power_report_count integer,power_count integer,power_sum double precision,
 power_complete boolean,power_source_oldest_at timestamptz,power_source_newest_at timestamptz,
 temperature_report_count integer,temperature_count integer,temperature_sum double precision,
 temperature_latest_value double precision,temperature_latest_at timestamptz,
 temperature_source_oldest_at timestamptz,temperature_source_newest_at timestamptz,
 state_report_count integer,state_count integer,state_latest_value double precision,state_latest_at timestamptz,
 state_source_oldest_at timestamptz,state_source_newest_at timestamptz)
LANGUAGE sql STABLE AS $$
 WITH summary AS MATERIALIZED (SELECT * FROM hosted_reporting_hour_summary(from_bucket,to_bucket,interval_ids)),
 hours AS (SELECT h AS bucket FROM generate_series(from_bucket,to_bucket-INTERVAL '1 hour',INTERVAL '1 hour') h),
 driver AS (
  SELECT i.id AS interval_id,h.bucket FROM hours h JOIN hosted_account_asset_interval i
   ON i.started_at<h.bucket+INTERVAL '1 hour' AND (i.ended_at IS NULL OR i.ended_at>GREATEST(h.bucket,i.started_at))
  WHERE interval_ids IS NULL
  UNION
  SELECT i.id,h.bucket FROM hours h JOIN hosted_account_asset_interval i ON i.id=ANY(interval_ids)
  WHERE interval_ids IS NOT NULL
  UNION
  SELECT s.interval_id,s.bucket FROM summary s
 ),
 framed AS (
  SELECT d.interval_id,d.bucket,i.organization_id AS org,i.kind,COALESCE(i.site_id,0) AS site,
   GREATEST(i.started_at,d.bucket) AS frame_start,
   LEAST(COALESCE(i.ended_at,'infinity'::timestamptz),d.bucket+INTERVAL '1 hour') AS frame_end
  FROM driver d JOIN hosted_account_asset_interval i ON i.id=d.interval_id
 ),
 coverage AS (
  SELECT g.bucket,g.org,g.site,g.kind,EXISTS(SELECT 1 FROM hosted_account_history_sample cs
   JOIN hosted_account_asset_interval ci ON ci.id=cs.interval_id
   WHERE cs.time>=g.bucket AND cs.time<g.bucket+INTERVAL '1 hour'
    AND cs.source_id=ANY(ARRAY(SELECT e.source_id FROM hosted_reporting_expected_streams() e WHERE e.kind=g.kind))
    AND ci.organization_id=g.org AND COALESCE(ci.site_id,0)=g.site) AS reported
  FROM (SELECT DISTINCT f.bucket,f.org,f.site,f.kind FROM framed f) g
 )
 SELECT f.interval_id,f.bucket,NOT c.reported,
  CASE WHEN f.frame_start<f.frame_end THEN f.frame_start END,
  CASE WHEN f.frame_start<f.frame_end THEN f.frame_end END,
  COALESCE(s.hashrate_report_count,0),COALESCE(s.hashrate_count,0),COALESCE(s.hashrate_positive_count,0),s.hashrate_sum,
  s.hashrate_complete,s.hashrate_source_oldest_at,s.hashrate_source_newest_at,
  COALESCE(s.power_report_count,0),COALESCE(s.power_count,0),s.power_sum,
  s.power_complete,s.power_source_oldest_at,s.power_source_newest_at,
  COALESCE(s.temperature_report_count,0),COALESCE(s.temperature_count,0),s.temperature_sum,
  s.temperature_latest_value,s.temperature_latest_at,s.temperature_source_oldest_at,s.temperature_source_newest_at,
  COALESCE(s.state_report_count,0),COALESCE(s.state_count,0),s.state_latest_value,s.state_latest_at,
  s.state_source_oldest_at,s.state_source_newest_at
 FROM framed f JOIN coverage c ON c.bucket=f.bucket AND c.org=f.org AND c.site=f.site AND c.kind=f.kind
 LEFT JOIN summary s ON s.interval_id=f.interval_id AND s.bucket=f.bucket
$$;

-- Correction markers. Writers only enqueue; the job owns every evidence write.
-- A marker is needed for hours that are closing or closed (bucket <
-- closing_through), inside the hold and inside the correction window. Every
-- enqueue inserts its own marker: there is no shared row for writers to queue on.
CREATE FUNCTION enqueue_hosted_reporting_hour_sample() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
 closing timestamptz;
 lower_at timestamptz;
 new_hour timestamptz;
 old_hour timestamptz;
BEGIN
 -- This runs after the row write took the sample-table lock, so it sees any
 -- closing_through that a finished phase 1 committed.
 SELECT c.closing_through,GREATEST(c.hold_started_at,hosted_reporting_hour_window_floor(now()))
 INTO closing,lower_at FROM hosted_account_reporting_hour_control c WHERE c.singleton;
 IF TG_OP<>'DELETE' THEN
  new_hour := time_bucket(INTERVAL '1 hour',NEW.time);
  IF new_hour<closing AND new_hour>=lower_at THEN
   INSERT INTO hosted_account_reporting_hour_marker(interval_id,bucket) VALUES(NEW.interval_id,new_hour);
  END IF;
 END IF;
 IF TG_OP<>'INSERT' THEN
  old_hour := time_bucket(INTERVAL '1 hour',OLD.time);
  IF old_hour<closing AND old_hour>=lower_at
   AND (TG_OP='DELETE' OR old_hour IS DISTINCT FROM new_hour OR OLD.interval_id IS DISTINCT FROM NEW.interval_id) THEN
   INSERT INTO hosted_account_reporting_hour_marker(interval_id,bucket) VALUES(OLD.interval_id,old_hour);
  END IF;
 END IF;
 RETURN NULL;
END;
$$;

-- Interval bound changes alter the capture window of the hours between the old
-- and new bound. Open ends are clamped to closing_through and starts to the hold
-- and correction window, so no series is ever unbounded.
CREATE FUNCTION enqueue_hosted_reporting_hour_interval() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
 closing timestamptz;
 lower_at timestamptz;
BEGIN
 -- This runs after the row write took the interval-table lock, so it sees any
 -- closing_through that a finished phase 1 committed.
 SELECT c.closing_through,GREATEST(c.hold_started_at,hosted_reporting_hour_window_floor(now()))
 INTO closing,lower_at FROM hosted_account_reporting_hour_control c WHERE c.singleton;
 IF lower_at>=closing THEN RETURN NULL; END IF;
 INSERT INTO hosted_account_reporting_hour_marker(interval_id,bucket)
 SELECT DISTINCT NEW.id,h FROM (VALUES
  (TG_OP='INSERT',NEW.started_at,COALESCE(NEW.ended_at,'infinity'::timestamptz)),
  (TG_OP='UPDATE',OLD.started_at,NEW.started_at),
  (TG_OP='UPDATE',COALESCE(OLD.ended_at,'infinity'::timestamptz),COALESCE(NEW.ended_at,'infinity'::timestamptz))
 ) bound(applies,a,z)
 CROSS JOIN LATERAL generate_series(time_bucket(INTERVAL '1 hour',GREATEST(LEAST(bound.a,bound.z),lower_at)),
  LEAST(GREATEST(bound.a,bound.z),closing)-INTERVAL '1 microsecond',INTERVAL '1 hour') h
 WHERE bound.applies AND bound.a IS DISTINCT FROM bound.z;
 RETURN NULL;
END;
$$;

-- Postgres rejects OLD/NEW in the WHEN clause of a trigger that also fires on
-- INSERT or DELETE, so unchanged upserts are filtered by a separate trigger.
CREATE TRIGGER enqueue_hosted_reporting_hour_sample
 AFTER INSERT OR DELETE ON hosted_account_history_sample
 FOR EACH ROW EXECUTE FUNCTION enqueue_hosted_reporting_hour_sample();
CREATE TRIGGER enqueue_hosted_reporting_hour_sample_update
 AFTER UPDATE ON hosted_account_history_sample
 FOR EACH ROW WHEN (OLD.* IS DISTINCT FROM NEW.*) EXECUTE FUNCTION enqueue_hosted_reporting_hour_sample();
CREATE TRIGGER enqueue_hosted_reporting_hour_interval
 AFTER INSERT OR UPDATE OF started_at,ended_at ON hosted_account_asset_interval
 FOR EACH ROW EXECUTE FUNCTION enqueue_hosted_reporting_hour_interval();

-- Phase 1: publish the closing boundary. The sample- and interval-table locks
-- wait out every in-flight sample and allocation writer, so writers that commit
-- later see the new boundary and leave markers. Both enqueue triggers rely on
-- this fence. The sample table goes first: telemetry batches, the only writers
-- of both tables, usually write samples before an interval, and phase 1 then
-- holds nothing while it waits out telemetry. Hours that left the correction
-- window before they could be closed get a gap row instead of evidence. Returns
-- closing_through.
CREATE FUNCTION hosted_reporting_hour_begin_close(as_of timestamptz,grace interval DEFAULT INTERVAL '30 minutes',
 correction_window interval DEFAULT INTERVAL '9 days',max_hours integer DEFAULT 24,lock_timeout_ms integer DEFAULT 1000)
RETURNS timestamptz LANGUAGE plpgsql AS $$
DECLARE
 control hosted_account_reporting_hour_control;
 effective_at timestamptz := LEAST(as_of,clock_timestamp());
 expired_before timestamptz;
 target timestamptz;
 lock_started timestamptz;
BEGIN
 IF grace IS NULL OR grace<INTERVAL '0' THEN RAISE EXCEPTION 'hourly evidence grace must not be negative, got %',grace; END IF;
 IF correction_window IS NULL OR correction_window<=INTERVAL '0' OR correction_window>INTERVAL '9 days' THEN
  RAISE EXCEPTION 'hourly evidence correction window must be positive and at most 9 days, got %',correction_window;
 END IF;
 IF max_hours IS NULL OR max_hours<1 THEN RAISE EXCEPTION 'hourly evidence max_hours must be positive, got %',max_hours; END IF;
 IF lock_timeout_ms IS NULL OR lock_timeout_ms<1 THEN RAISE EXCEPTION 'hourly evidence lock_timeout_ms must be positive, got %',lock_timeout_ms; END IF;
 SELECT * INTO STRICT control FROM hosted_account_reporting_hour_control WHERE singleton FOR UPDATE;
 expired_before := hosted_reporting_hour_window_floor(effective_at,correction_window);
 IF control.closed_through<expired_before THEN
  INSERT INTO hosted_account_reporting_hour_gap(from_bucket,to_bucket,reason)
  VALUES(control.closed_through,expired_before,'closer fell behind past the correction window');
  UPDATE hosted_account_reporting_hour_control
  SET closed_through=expired_before,closing_through=GREATEST(closing_through,expired_before)
  WHERE singleton RETURNING * INTO STRICT control;
 END IF;
 target := LEAST(time_bucket(INTERVAL '1 hour',effective_at-grace),control.closed_through+max_hours*INTERVAL '1 hour');
 IF target<=control.closing_through THEN RETURN control.closing_through; END IF;
 -- lock_timeout applies to each lock wait, so the second lock gets what is
 -- left of the budget: writers queue behind phase 1 for lock_timeout_ms at most.
 lock_started := clock_timestamp();
 PERFORM set_config('lock_timeout',lock_timeout_ms::text||'ms',true);
 LOCK TABLE hosted_account_history_sample IN SHARE ROW EXCLUSIVE MODE;
 PERFORM set_config('lock_timeout',GREATEST(1,lock_timeout_ms
  -ceil(extract(epoch FROM clock_timestamp()-lock_started)*1000)::integer)::text||'ms',true);
 LOCK TABLE hosted_account_asset_interval IN SHARE ROW EXCLUSIVE MODE;
 UPDATE hosted_account_reporting_hour_control SET closing_through=target WHERE singleton;
 RETURN target;
END;
$$;

-- Phase 2: write revision 1 for every interval-hour in [closed_through,
-- closing_through). The marker claim is its own statement: every marker it
-- deletes belongs to a committed write that the later summary statement sees.
-- Markers of writers still in flight survive for the revision step. Returns
-- the number of evidence rows written.
CREATE FUNCTION hosted_reporting_hour_finish_close(as_of timestamptz,correction_window interval DEFAULT INTERVAL '9 days')
RETURNS integer LANGUAGE plpgsql AS $$
DECLARE
 control hosted_account_reporting_hour_control;
 written integer := 0;
BEGIN
 IF correction_window IS NULL OR correction_window<=INTERVAL '0' OR correction_window>INTERVAL '9 days' THEN
  RAISE EXCEPTION 'hourly evidence correction window must be positive and at most 9 days, got %',correction_window;
 END IF;
 SELECT * INTO STRICT control FROM hosted_account_reporting_hour_control WHERE singleton FOR UPDATE;
 IF control.closed_through<control.closing_through THEN
  IF control.closed_through<hosted_reporting_hour_window_floor(LEAST(as_of,clock_timestamp()),correction_window) THEN
   RAISE EXCEPTION 'hour % has left the correction window; run hosted_reporting_hour_begin_close to record the gap',control.closed_through;
  END IF;
  DELETE FROM hosted_account_reporting_hour_marker m
  WHERE m.bucket>=control.closed_through AND m.bucket<control.closing_through;
  WITH candidate AS MATERIALIZED (
   SELECT * FROM hosted_reporting_hour_rows(control.closed_through,control.closing_through)
  ), snapshot AS (
   INSERT INTO hosted_account_reporting_allocation_snapshot(interval_id,organization_id,client_id,subclient_id,
    site_id,kind,asset_id,device_id,device_identifier,started_at)
   SELECT i.id,i.organization_id,i.client_id,i.subclient_id,i.site_id,i.kind,i.asset_id,i.device_id,i.device_identifier,i.started_at
   FROM hosted_account_asset_interval i WHERE i.id IN (SELECT c.interval_id FROM candidate c)
   ON CONFLICT(interval_id) DO NOTHING
  )
  INSERT INTO hosted_account_reporting_hour(interval_id,bucket,revision,capture_reason,no_source_data,window_start,window_end,
   hashrate_report_count,hashrate_count,hashrate_positive_count,hashrate_sum,hashrate_complete,hashrate_source_oldest_at,hashrate_source_newest_at,
   power_report_count,power_count,power_sum,power_complete,power_source_oldest_at,power_source_newest_at,
   temperature_report_count,temperature_count,temperature_sum,temperature_latest_value,temperature_latest_at,
   temperature_source_oldest_at,temperature_source_newest_at,
   state_report_count,state_count,state_latest_value,state_latest_at,state_source_oldest_at,state_source_newest_at)
  SELECT c.interval_id,c.bucket,1,'closed',c.no_source_data,c.window_start,c.window_end,
   c.hashrate_report_count,c.hashrate_count,c.hashrate_positive_count,c.hashrate_sum,c.hashrate_complete,c.hashrate_source_oldest_at,c.hashrate_source_newest_at,
   c.power_report_count,c.power_count,c.power_sum,c.power_complete,c.power_source_oldest_at,c.power_source_newest_at,
   c.temperature_report_count,c.temperature_count,c.temperature_sum,c.temperature_latest_value,c.temperature_latest_at,
   c.temperature_source_oldest_at,c.temperature_source_newest_at,
   c.state_report_count,c.state_count,c.state_latest_value,c.state_latest_at,c.state_source_oldest_at,c.state_source_newest_at
  FROM candidate c;
  GET DIAGNOSTICS written=ROW_COUNT;
 END IF;
 UPDATE hosted_account_reporting_hour_control SET closed_through=closing_through,last_success_at=clock_timestamp() WHERE singleton;
 RETURN written;
END;
$$;

-- Revision step: recompute up to batch_size marked closed interval-hours and append
-- revision N+1 only where the full row differs from the latest revision. An
-- interval-hour without a prior revision (a backdated or late-created interval)
-- gets revision 1 marked unattributable, because samples are attributed only at
-- ingest. When a recompute flips an hour's site source coverage, the other
-- intervals of that organization, kind and site are recomputed too, so their no_source_data
-- flag follows. Markers older than the window, before the hold or under a gap
-- are discarded. Returns the number of revisions appended.
CREATE FUNCTION hosted_reporting_hour_revise(as_of timestamptz,correction_window interval DEFAULT INTERVAL '9 days',
 batch_size integer DEFAULT 10000)
RETURNS integer LANGUAGE plpgsql AS $$
DECLARE
 control hosted_account_reporting_hour_control;
 expired_before timestamptz;
 claimed_ids bigint[];
 claimed_buckets timestamptz[];
 written integer;
BEGIN
 IF correction_window IS NULL OR correction_window<=INTERVAL '0' OR correction_window>INTERVAL '9 days' THEN
  RAISE EXCEPTION 'hourly evidence correction window must be positive and at most 9 days, got %',correction_window;
 END IF;
 IF batch_size IS NULL OR batch_size<1 THEN RAISE EXCEPTION 'hourly evidence revision batch must be positive, got %',batch_size; END IF;
 SELECT * INTO STRICT control FROM hosted_account_reporting_hour_control WHERE singleton FOR UPDATE;
 expired_before := GREATEST(control.hold_started_at,
  hosted_reporting_hour_window_floor(LEAST(as_of,clock_timestamp()),correction_window));
 -- Standalone claim; the recompute below runs with a later snapshot. The batch
 -- counts interval-hours, oldest marker first, and takes every visible marker
 -- of each: one corrected hour can carry thousands of duplicate markers.
 WITH batch AS (
  SELECT q.interval_id,q.bucket FROM hosted_account_reporting_hour_marker q
  WHERE q.bucket<control.closed_through
  GROUP BY q.interval_id,q.bucket ORDER BY min(q.marker_id) LIMIT batch_size
 ), claimed AS (
  DELETE FROM hosted_account_reporting_hour_marker m USING batch b
  WHERE m.interval_id=b.interval_id AND m.bucket=b.bucket
  RETURNING m.interval_id,m.bucket
 )
 SELECT array_agg(c.interval_id),array_agg(c.bucket) INTO claimed_ids,claimed_buckets
 FROM (SELECT DISTINCT claimed.interval_id,claimed.bucket FROM claimed) c
 WHERE c.bucket>=expired_before
  AND NOT EXISTS(SELECT 1 FROM hosted_account_reporting_hour_gap g WHERE c.bucket>=g.from_bucket AND c.bucket<g.to_bucket);
 IF claimed_ids IS NULL THEN RETURN 0; END IF;
 WITH claimed AS (
  SELECT p.interval_id,p.bucket FROM unnest(claimed_ids,claimed_buckets) p(interval_id,bucket)
 ), direct AS MATERIALIZED (
  SELECT r.* FROM (SELECT claimed.bucket,array_agg(claimed.interval_id) AS ids FROM claimed GROUP BY claimed.bucket) b
  CROSS JOIN LATERAL hosted_reporting_hour_rows(b.bucket,b.bucket+INTERVAL '1 hour',b.ids) r
 ), coverage AS (
  SELECT DISTINCT d.bucket,i.organization_id AS org,i.kind,COALESCE(i.site_id,0) AS site,d.no_source_data
  FROM direct d JOIN hosted_account_asset_interval i ON i.id=d.interval_id
 ), stale AS (
  SELECT g.bucket,array_agg(i.id) AS ids FROM coverage g
  JOIN hosted_account_asset_interval i ON i.organization_id=g.org AND i.kind=g.kind AND COALESCE(i.site_id,0)=g.site
  CROSS JOIN LATERAL (SELECT h.no_source_data FROM hosted_account_reporting_hour h
   WHERE h.interval_id=i.id AND h.bucket=g.bucket ORDER BY h.revision DESC LIMIT 1) l
  WHERE l.no_source_data IS DISTINCT FROM g.no_source_data
   AND NOT EXISTS(SELECT 1 FROM claimed c WHERE c.interval_id=i.id AND c.bucket=g.bucket)
  GROUP BY g.bucket
 ), candidate AS (
  SELECT * FROM direct
  UNION ALL
  SELECT r.* FROM stale s CROSS JOIN LATERAL hosted_reporting_hour_rows(s.bucket,s.bucket+INTERVAL '1 hour',s.ids) r
 ), changed AS MATERIALIZED (
  SELECT c.*,COALESCE(l.revision,0)+1 AS revision,COALESCE(l.capture_reason,'unattributable') AS capture_reason
  FROM candidate c LEFT JOIN LATERAL (SELECT h.* FROM hosted_account_reporting_hour h
   WHERE h.interval_id=c.interval_id AND h.bucket=c.bucket ORDER BY h.revision DESC LIMIT 1) l ON true
  WHERE CASE WHEN l.revision IS NULL
   THEN c.window_start IS NOT NULL OR c.hashrate_report_count+c.power_report_count+c.temperature_report_count+c.state_report_count>0
   ELSE ROW(c.no_source_data,c.window_start,c.window_end,
     c.hashrate_report_count,c.hashrate_count,c.hashrate_positive_count,c.hashrate_sum,c.hashrate_complete,c.hashrate_source_oldest_at,c.hashrate_source_newest_at,
     c.power_report_count,c.power_count,c.power_sum,c.power_complete,c.power_source_oldest_at,c.power_source_newest_at,
     c.temperature_report_count,c.temperature_count,c.temperature_sum,c.temperature_latest_value,c.temperature_latest_at,
     c.temperature_source_oldest_at,c.temperature_source_newest_at,
     c.state_report_count,c.state_count,c.state_latest_value,c.state_latest_at,c.state_source_oldest_at,c.state_source_newest_at)
    IS DISTINCT FROM ROW(l.no_source_data,l.window_start,l.window_end,
     l.hashrate_report_count,l.hashrate_count,l.hashrate_positive_count,l.hashrate_sum,l.hashrate_complete,l.hashrate_source_oldest_at,l.hashrate_source_newest_at,
     l.power_report_count,l.power_count,l.power_sum,l.power_complete,l.power_source_oldest_at,l.power_source_newest_at,
     l.temperature_report_count,l.temperature_count,l.temperature_sum,l.temperature_latest_value,l.temperature_latest_at,
     l.temperature_source_oldest_at,l.temperature_source_newest_at,
     l.state_report_count,l.state_count,l.state_latest_value,l.state_latest_at,l.state_source_oldest_at,l.state_source_newest_at)
   END
 ), snapshot AS (
  INSERT INTO hosted_account_reporting_allocation_snapshot(interval_id,organization_id,client_id,subclient_id,
   site_id,kind,asset_id,device_id,device_identifier,started_at)
  SELECT i.id,i.organization_id,i.client_id,i.subclient_id,i.site_id,i.kind,i.asset_id,i.device_id,i.device_identifier,i.started_at
  FROM hosted_account_asset_interval i WHERE i.id IN (SELECT c.interval_id FROM changed c)
  ON CONFLICT(interval_id) DO NOTHING
 )
 INSERT INTO hosted_account_reporting_hour(interval_id,bucket,revision,capture_reason,no_source_data,window_start,window_end,
  hashrate_report_count,hashrate_count,hashrate_positive_count,hashrate_sum,hashrate_complete,hashrate_source_oldest_at,hashrate_source_newest_at,
  power_report_count,power_count,power_sum,power_complete,power_source_oldest_at,power_source_newest_at,
  temperature_report_count,temperature_count,temperature_sum,temperature_latest_value,temperature_latest_at,
  temperature_source_oldest_at,temperature_source_newest_at,
  state_report_count,state_count,state_latest_value,state_latest_at,state_source_oldest_at,state_source_newest_at)
 SELECT c.interval_id,c.bucket,c.revision,c.capture_reason,c.no_source_data,c.window_start,c.window_end,
  c.hashrate_report_count,c.hashrate_count,c.hashrate_positive_count,c.hashrate_sum,c.hashrate_complete,c.hashrate_source_oldest_at,c.hashrate_source_newest_at,
  c.power_report_count,c.power_count,c.power_sum,c.power_complete,c.power_source_oldest_at,c.power_source_newest_at,
  c.temperature_report_count,c.temperature_count,c.temperature_sum,c.temperature_latest_value,c.temperature_latest_at,
  c.temperature_source_oldest_at,c.temperature_source_newest_at,
  c.state_report_count,c.state_count,c.state_latest_value,c.state_latest_at,c.state_source_oldest_at,c.state_source_newest_at
 FROM changed c;
 GET DIAGNOSTICS written=ROW_COUNT;
 RETURN written;
END;
$$;

-- Scheduled wrapper. Each phase commits on its own, so phase 1's table locks
-- are released before any aggregation, and a failed phase 2 leaves
-- closed_through behind closing_through for the next run to resume. There is no
-- exception handler: failures reach timescaledb_information.job_errors.
CREATE PROCEDURE hosted_reporting_hour_job(job_id integer,config jsonb)
LANGUAGE plpgsql AS $$
DECLARE
 as_of timestamptz := clock_timestamp();
 grace interval := COALESCE((config->>'grace')::interval,INTERVAL '30 minutes');
 correction_window interval := COALESCE((config->>'correction_window')::interval,INTERVAL '9 days');
 max_hours integer := COALESCE((config->>'max_hours')::integer,24);
 lock_timeout_ms integer := COALESCE((config->>'lock_timeout_ms')::integer,1000);
 revision_batch integer := COALESCE((config->>'revision_batch')::integer,10000);
BEGIN
 IF NOT COALESCE((config->>'enabled')::boolean,false) THEN RETURN; END IF;
 PERFORM hosted_reporting_hour_begin_close(as_of,grace,correction_window,max_hours,lock_timeout_ms);
 COMMIT;
 PERFORM hosted_reporting_hour_finish_close(as_of,correction_window);
 COMMIT;
 PERFORM hosted_reporting_hour_revise(as_of,correction_window,revision_batch);
 COMMIT;
END;
$$;
SELECT add_job('hosted_reporting_hour_job',INTERVAL '15 minutes',
 config=>'{"enabled":true,"grace":"30 minutes","correction_window":"9 days","max_hours":24,"lock_timeout_ms":1000,"revision_batch":10000}'::jsonb,
 fixed_schedule=>false);
SELECT alter_job(job_id,max_runtime=>INTERVAL '10 minutes',retry_period=>INTERVAL '5 minutes')
FROM timescaledb_information.jobs WHERE proc_name='hosted_reporting_hour_job';

-- Capture status: one row per expected (interval, source, metric) stream of
-- open intervals and intervals that ended inside the correction window.
-- closing_through and closed_through are exclusive hour ends. Observation
-- fields come from the latest revision of the most recent hour inside the
-- correction window whose latest revision has a value for the stream's metric,
-- so revising an older hour never moves stream freshness and the lookup does
-- not grow with retained evidence. missing_hours counts hours in
-- [max(hold_started_at, window floor), closed_through) that overlap the
-- interval and have neither a revision nor a covering gap. Gap hours and the
-- latest gap cover the whole interval regardless of age and are not missing.
CREATE VIEW hosted_account_reporting_capture_status AS
SELECT i.id AS interval_id,i.organization_id,i.client_id,i.subclient_id,i.site_id,i.kind,
 i.asset_id,i.device_id,i.device_identifier,i.started_at AS allocation_started_at,i.ended_at AS allocation_ended_at,
 control.hold_started_at,control.closing_through,control.closed_through,control.last_success_at AS closer_last_success_at,
 extract(epoch FROM now()-control.closed_through)::double precision AS closer_lag_seconds,
 stream.source_id,stream.metric,
 (observation.bucket IS NOT NULL)::boolean AS has_observation,
 observation.bucket AS last_observation_bucket,observation.revision AS last_observation_revision,
 observation.captured_at AS last_observation_captured_at,
 observation.source_newest_at AS last_observation_source_newest_at,
 closed.bucket AS last_closed_bucket,closed.revision AS last_closed_revision,
 closed.no_source_data AS last_closed_no_source_data,
 missing.hours AS missing_hours,gap.hours AS gap_hours,
 latest_gap.from_bucket AS last_gap_from_bucket,latest_gap.to_bucket AS last_gap_to_bucket,
 latest_gap.reason AS last_gap_reason,latest_gap.recorded_at AS last_gap_recorded_at
FROM hosted_account_asset_interval i
CROSS JOIN hosted_account_reporting_hour_control control
-- Hours the interval overlaps: [first_hour, end_hour), matching the closer's driver set.
CROSS JOIN LATERAL (
 SELECT time_bucket(INTERVAL '1 hour',i.started_at) AS first_hour,
  CASE WHEN i.ended_at IS NULL THEN 'infinity'::timestamptz
   WHEN i.ended_at<=i.started_at THEN time_bucket(INTERVAL '1 hour',i.started_at)
   ELSE time_bucket(INTERVAL '1 hour',i.ended_at-INTERVAL '1 microsecond')+INTERVAL '1 hour' END AS end_hour
) span
-- Hours checked for missing evidence: [from_hour, to_hour).
CROSS JOIN LATERAL (
 SELECT GREATEST(control.hold_started_at,hosted_reporting_hour_window_floor(now()),span.first_hour) AS from_hour,
  LEAST(control.closed_through,span.end_hour) AS to_hour
) recent
LEFT JOIN LATERAL (
 SELECT h.bucket,h.revision,h.no_source_data FROM hosted_account_reporting_hour h
 WHERE h.interval_id=i.id ORDER BY h.bucket DESC,h.revision DESC LIMIT 1
) closed ON true
-- EXCEPT, not an anti-join: the planner has picked a nested loop for the
-- anti-join, comparing every window hour with every hour row of the interval.
-- The bucket range keeps the hour-row scan inside the window, so its cost
-- does not grow with the interval's retained evidence.
CROSS JOIN LATERAL (
 SELECT count(*)::integer AS hours FROM (
  SELECT m.bucket FROM generate_series(recent.from_hour,recent.to_hour-INTERVAL '1 hour',INTERVAL '1 hour') m(bucket)
  EXCEPT SELECT h.bucket FROM hosted_account_reporting_hour h
  WHERE h.interval_id=i.id AND h.bucket>=recent.from_hour AND h.bucket<recent.to_hour
 ) m
 WHERE NOT EXISTS(SELECT 1 FROM hosted_account_reporting_hour_gap g WHERE m.bucket>=g.from_bucket AND m.bucket<g.to_bucket)
) missing
CROSS JOIN LATERAL (
 SELECT COALESCE(sum(extract(epoch FROM LEAST(g.to_bucket,span.end_hour)-GREATEST(g.from_bucket,span.first_hour))/3600),0)::integer AS hours
 FROM hosted_account_reporting_hour_gap g WHERE g.from_bucket<span.end_hour AND g.to_bucket>span.first_hour
) gap
LEFT JOIN LATERAL (
 SELECT g.from_bucket,g.to_bucket,g.reason,g.recorded_at FROM hosted_account_reporting_hour_gap g
 WHERE g.from_bucket<span.end_hour AND g.to_bucket>span.first_hour
 ORDER BY g.to_bucket DESC,g.gap_id DESC LIMIT 1
) latest_gap ON true
-- Per-stream lookups follow the per-interval ones, which run once per interval.
-- sqlc cannot see the function's result columns: name them, and the no-op
-- casts give it their types.
JOIN (SELECT e.kind,e.source_id::smallint AS source_id,e.metric::smallint AS metric
 FROM hosted_reporting_expected_streams() e(kind,source_id,metric)) stream ON stream.kind=i.kind
LEFT JOIN LATERAL (
 SELECT h.bucket,h.revision,h.captured_at,
  CASE stream.metric WHEN hosted_history_metric_code('hashrate') THEN h.hashrate_source_newest_at
   WHEN hosted_history_metric_code('power') THEN h.power_source_newest_at
   WHEN hosted_history_metric_code('temperature') THEN h.temperature_source_newest_at
   ELSE h.state_source_newest_at END AS source_newest_at
 FROM hosted_account_reporting_hour h
 WHERE h.interval_id=i.id AND h.bucket>=hosted_reporting_hour_window_floor(now())
  AND CASE stream.metric WHEN hosted_history_metric_code('hashrate') THEN h.hashrate_count
   WHEN hosted_history_metric_code('power') THEN h.power_count
   WHEN hosted_history_metric_code('temperature') THEN h.temperature_count
   ELSE h.state_count END>0
  AND NOT EXISTS(SELECT 1 FROM hosted_account_reporting_hour n
   WHERE n.interval_id=h.interval_id AND n.bucket=h.bucket AND n.revision>h.revision)
 ORDER BY h.bucket DESC LIMIT 1
) observation ON true
WHERE i.ended_at IS NULL OR i.ended_at>hosted_reporting_hour_window_floor(now());

-- fleet-migration: 001078_internal_default_pool_lifecycle.up.sql
-- requires-shared: 1067
-- One private checkpoint installs defaults and their final lifecycle policy.
-- Ownership/inventory writers serialize on the organization row. Pairing
-- writers must also wait until marker backfill and trigger installation finish.
LOCK TABLE organization IN EXCLUSIVE MODE;
LOCK TABLE device_pairing IN SHARE ROW EXCLUSIVE MODE;

ALTER TABLE pool_configuration ADD COLUMN is_default BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE pool_configuration ADD CONSTRAINT ck_pool_configuration_active_default CHECK (NOT (archived AND is_default));

-- Establish a deterministic default without changing existing equipment.
WITH ranked AS (
 SELECT configuration_id, row_number() OVER (
  PARTITION BY organization_id, account_id ORDER BY created_at, configuration_id
 ) AS position FROM pool_configuration WHERE NOT archived
)
UPDATE pool_configuration c SET is_default=TRUE FROM ranked r
WHERE c.configuration_id=r.configuration_id AND r.position=1;

CREATE UNIQUE INDEX idx_pool_configuration_owner_default
 ON pool_configuration(organization_id, COALESCE(account_id, 0)) WHERE is_default;

-- Capture destination policy durably; device I/O happens only after commit.
CREATE TABLE pool_lifecycle_transition (
 transition_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
 organization_id BIGINT NOT NULL REFERENCES organization(id),
 device_id BIGINT NOT NULL,
 asset_kind INTEGER NOT NULL CHECK (asset_kind IN (0,5,7)),
 asset_id BIGINT NOT NULL,
 owner_account_id BIGINT NOT NULL DEFAULT 0,
 allocation_revision BIGINT NOT NULL DEFAULT 0,
 pairing_defaults JSONB NOT NULL DEFAULT '{}',
 reason TEXT NOT NULL CHECK (reason IN ('pairing','ownership')),
 pairing_cycle_id UUID,
 CONSTRAINT ck_pool_lifecycle_pairing_cycle CHECK ((reason='pairing')=(pairing_cycle_id IS NOT NULL)),
 configuration_id VARCHAR(36), revision_id VARCHAR(36),
 target_kind TEXT NOT NULL DEFAULT '', target_id BIGINT NOT NULL DEFAULT 0,
 reviewed_binding TEXT NOT NULL DEFAULT '',
 state TEXT NOT NULL DEFAULT 'pending' CHECK (state IN ('pending','ready','applying','verified','completed','failed','uncertain','superseded')),
 result_code TEXT NOT NULL DEFAULT '',
 operation_id VARCHAR(36), physical_attempt_id UUID REFERENCES device_write_attempt(attempt_id),
 created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 updated_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 CONSTRAINT fk_pool_lifecycle_device FOREIGN KEY (device_id,organization_id) REFERENCES device(id,org_id),
 CONSTRAINT fk_pool_lifecycle_revision FOREIGN KEY (organization_id,configuration_id,revision_id)
  REFERENCES pool_configuration_revision(organization_id,configuration_id,revision_id),
 CONSTRAINT ck_pool_lifecycle_configuration CHECK ((configuration_id IS NULL)=(revision_id IS NULL))
);
-- Polling includes recovery work; reconciliation also visits failed intent.
-- Keep settled history out of these scans and ownership's per-asset lookup.
CREATE INDEX idx_pool_lifecycle_transition_open_asset
 ON pool_lifecycle_transition(state,organization_id,asset_kind,asset_id)
 WHERE state IN ('pending','ready','applying','uncertain','failed');
-- Physical-attempt status is rechecked by the poll; only locally eligible
-- transitions belong here. Terminal failures must not grow the polling scan.
CREATE INDEX idx_pool_lifecycle_transition_poll
 ON pool_lifecycle_transition(device_id,next_attempt_at,created_at)
 WHERE state IN ('pending','ready')
  OR (state IN ('applying','uncertain') AND configuration_id IS NULL AND target_kind='device')
  OR (state='failed' AND result_code='clear_not_dispatched' AND reason='ownership' AND configuration_id IS NULL AND target_kind='device');
CREATE INDEX idx_pool_lifecycle_transition_device ON pool_lifecycle_transition(organization_id,device_id);
CREATE INDEX idx_pool_lifecycle_transition_physical_attempt ON pool_lifecycle_transition(physical_attempt_id)
 WHERE physical_attempt_id IS NOT NULL;
CREATE INDEX idx_pool_lifecycle_transition_removed_board ON pool_lifecycle_transition(organization_id,asset_id)
 WHERE asset_kind=7 AND state='superseded' AND result_code='board_removed';


CREATE FUNCTION validate_pool_lifecycle_transition() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='UPDATE' THEN
  IF ROW(NEW.organization_id,NEW.device_id,NEW.asset_kind,NEW.asset_id,NEW.owner_account_id,NEW.allocation_revision,NEW.reason,NEW.pairing_cycle_id,NEW.configuration_id,NEW.revision_id,NEW.pairing_defaults,NEW.created_at)
   IS DISTINCT FROM ROW(OLD.organization_id,OLD.device_id,OLD.asset_kind,OLD.asset_id,OLD.owner_account_id,OLD.allocation_revision,OLD.reason,OLD.pairing_cycle_id,OLD.configuration_id,OLD.revision_id,OLD.pairing_defaults,OLD.created_at) THEN
   RAISE EXCEPTION 'pool lifecycle policy is immutable' USING ERRCODE='23514';
  END IF;
  IF OLD.state<>'pending' AND ROW(NEW.target_kind,NEW.target_id,NEW.reviewed_binding) IS DISTINCT FROM ROW(OLD.target_kind,OLD.target_id,OLD.reviewed_binding) THEN
   RAISE EXCEPTION 'prepared pool lifecycle binding is immutable' USING ERRCODE='23514';
  END IF;
  IF OLD.physical_attempt_id IS NOT NULL AND NEW.physical_attempt_id IS DISTINCT FROM OLD.physical_attempt_id AND NOT (
   NEW.physical_attempt_id IS NULL AND OLD.state='failed' AND NEW.state='ready' AND EXISTS(
    SELECT 1 FROM device_write_attempt w WHERE w.attempt_id=OLD.physical_attempt_id AND w.status='complete' AND w.write_outcome='{}'::jsonb
   )) THEN
   RAISE EXCEPTION 'pool lifecycle attempt is immutable after dispatch' USING ERRCODE='23514';
  END IF;
 END IF;
 IF NEW.physical_attempt_id IS NOT NULL AND (TG_OP='INSERT' OR OLD.physical_attempt_id IS NULL) AND NOT EXISTS(
  SELECT 1 FROM device_write_attempt w WHERE w.attempt_id=NEW.physical_attempt_id AND w.organization_id=NEW.organization_id
   AND w.device_id=NEW.device_id AND w.operation_kind='mutation' AND w.status='running'
   AND NEW.state='applying' AND NEW.configuration_id IS NULL AND NEW.reason='ownership'
 ) THEN
  RAISE EXCEPTION 'pool lifecycle clearing requires its current physical admission' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER pool_lifecycle_transition_binding BEFORE INSERT OR UPDATE ON pool_lifecycle_transition
 FOR EACH ROW EXECUTE FUNCTION validate_pool_lifecycle_transition();

CREATE FUNCTION enqueue_ownership_pool_transition() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE owner bigint;
BEGIN
 IF TG_OP='UPDATE' AND NEW.client_id IS NOT DISTINCT FROM OLD.client_id
  AND NEW.subclient_id IS NOT DISTINCT FROM OLD.subclient_id AND NEW.device_id=OLD.device_id THEN RETURN NEW; END IF;
 IF TG_OP='INSERT' AND NEW.client_id IS NULL THEN RETURN NEW; END IF;
 owner := COALESCE(NEW.subclient_id,NEW.client_id,0);
 UPDATE pool_lifecycle_transition SET state='superseded',updated_at=clock_timestamp()
 WHERE organization_id=NEW.organization_id AND ((asset_kind=NEW.kind AND asset_id=NEW.asset_id) OR (asset_kind=0 AND NEW.kind=5 AND device_id=NEW.device_id))
  AND state IN ('pending','ready','failed');
 INSERT INTO pool_lifecycle_transition(organization_id,device_id,asset_kind,asset_id,owner_account_id,allocation_revision,reason,configuration_id,revision_id)
 SELECT NEW.organization_id,NEW.device_id,NEW.kind,NEW.asset_id,owner,NEW.revision,'ownership',c.configuration_id,c.current_revision_id
 FROM (SELECT 1) seed LEFT JOIN pool_configuration c ON c.organization_id=NEW.organization_id
  AND COALESCE(c.account_id,0)=owner AND c.is_default AND NOT c.archived;
 RETURN NEW;
END $$;
CREATE TRIGGER hosted_allocation_pool_transition AFTER INSERT OR UPDATE ON hosted_account_asset
 FOR EACH ROW EXECUTE FUNCTION enqueue_ownership_pool_transition();

-- Presence means this pairing cycle has already succeeded (or was retained
-- during upgrade). Keep this policy marker private to Fleet's lifecycle logic.
CREATE TABLE pool_pairing_cycle (
 device_id BIGINT PRIMARY KEY REFERENCES device_pairing(device_id) ON DELETE CASCADE,
 cycle_id UUID NOT NULL DEFAULT gen_random_uuid()
);

-- Historical paired_at also records unsuccessful attempts. For existing
-- non-unpaired devices, preserve settings instead of guessing whether the next
-- authentication is initial pairing or repair. Explicit unpair resets the marker.
INSERT INTO pool_pairing_cycle(device_id)
SELECT device_id FROM device_pairing WHERE pairing_status<>'UNPAIRED';

CREATE FUNCTION enqueue_pairing_pool_transition() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE organization bigint; class integer; cycle uuid;
BEGIN
 -- Authentication/endpoint recovery is still the same pairing cycle. Only
 -- an explicit unpair resets it, including local unpairs without unpaired_at.
 IF NEW.pairing_status='UNPAIRED' THEN
  DELETE FROM pool_pairing_cycle WHERE device_id=NEW.device_id;
  -- Cancel unclaimed intent, including expanded CT1 children. Accepted work
  -- checks the cycle at dispatch; its physical evidence and recovery stay intact.
  UPDATE pool_lifecycle_transition SET state='superseded',result_code='pairing_cycle_ended',updated_at=clock_timestamp()
  WHERE organization_id=(SELECT org_id FROM device WHERE id=NEW.device_id) AND device_id=NEW.device_id
   AND reason='pairing' AND state IN ('pending','ready')
   AND operation_id IS NULL AND physical_attempt_id IS NULL;
  RETURN NEW;
 END IF;
 IF NEW.pairing_status NOT IN ('PAIRED','DEFAULT_PASSWORD') THEN RETURN NEW; END IF;
 SELECT d.org_id,dd.device_class INTO organization,class FROM device d JOIN discovered_device dd ON dd.id=d.discovered_device_id WHERE d.id=NEW.device_id AND d.deleted_at IS NULL;
 IF NOT FOUND THEN RETURN NEW; END IF;
 INSERT INTO pool_pairing_cycle(device_id) VALUES(NEW.device_id) ON CONFLICT DO NOTHING RETURNING cycle_id INTO cycle;
 IF NOT FOUND THEN RETURN NEW; END IF;
 IF class=2 THEN
  INSERT INTO pool_lifecycle_transition(organization_id,device_id,asset_kind,asset_id,reason,pairing_cycle_id,pairing_defaults)
  SELECT organization,NEW.device_id,0,NEW.device_id,'pairing',cycle,COALESCE(jsonb_object_agg(COALESCE(c.account_id,0)::text,jsonb_build_object('configuration_id',c.configuration_id,'revision_id',c.current_revision_id)),'{}')
  FROM pool_configuration c WHERE c.organization_id=organization AND c.is_default AND NOT c.archived
   AND (c.account_id IS NULL OR EXISTS(SELECT 1 FROM hosted_account_asset a WHERE a.organization_id=organization AND a.device_id=NEW.device_id AND COALESCE(a.subclient_id,a.client_id)=c.account_id));
 ELSE
  INSERT INTO pool_lifecycle_transition(organization_id,device_id,asset_kind,asset_id,owner_account_id,allocation_revision,reason,pairing_cycle_id,configuration_id,revision_id)
  SELECT organization,NEW.device_id,5,NEW.device_id,COALESCE(a.subclient_id,a.client_id,0),COALESCE(a.revision,0),'pairing',cycle,c.configuration_id,c.current_revision_id
  FROM (SELECT 1) seed LEFT JOIN hosted_account_asset a ON a.organization_id=organization AND a.kind=5 AND a.asset_id=NEW.device_id
  LEFT JOIN pool_configuration c ON c.organization_id=organization AND COALESCE(c.account_id,0)=COALESCE(a.subclient_id,a.client_id,0) AND c.is_default AND NOT c.archived;
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER device_pairing_pool_transition AFTER INSERT OR UPDATE ON device_pairing
 FOR EACH ROW EXECUTE FUNCTION enqueue_pairing_pool_transition();

ALTER TABLE pool_configuration_operation ADD COLUMN lifecycle_id UUID REFERENCES pool_lifecycle_transition(transition_id);
ALTER TABLE pool_configuration_operation DROP CONSTRAINT pool_configuration_operation_actor_id_check;
ALTER TABLE pool_configuration_operation ADD CONSTRAINT ck_pool_configuration_operation_actor
 CHECK (actor_id>0 OR (actor_id=0 AND lifecycle_id IS NOT NULL));

CREATE VIEW pool_lifecycle_removal AS
SELECT l.transition_id AS removal_id,l.organization_id,NULLIF(l.owner_account_id,0)::bigint AS account_id,0::bigint AS actor_id,
 l.target_kind,l.target_id,l.device_id,''::varchar(36) AS configuration_id,''::varchar(36) AS revision_id,
 l.allocation_revision AS assignment_version,l.reviewed_binding,'different'::text AS verification_state,true AS clear_supported,
 false AS keep_device_settings,CASE WHEN l.state='ready' THEN 'pending' ELSE l.state END::text AS state,l.result_code,
 l.physical_attempt_id,l.created_at,l.updated_at,COALESCE(w.status,'')::text AS attempt_status
FROM pool_lifecycle_transition l LEFT JOIN device_write_attempt w ON w.attempt_id=l.physical_attempt_id
WHERE l.configuration_id IS NULL AND l.reason='ownership' AND l.target_id>0;

-- Replacement recalls the outgoing board after closing its installation.
-- Retain that policy as history without trying to configure absent hardware.
CREATE FUNCTION retire_absent_board_pool_transition() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.asset_kind=7 AND NEW.state='pending' AND NEW.operation_id IS NULL AND NEW.physical_attempt_id IS NULL
  AND NOT EXISTS(SELECT 1 FROM hashboard_installation i WHERE i.hashboard_id=NEW.asset_id AND i.device_id=NEW.device_id AND i.removed_at IS NULL)
  AND EXISTS(SELECT 1 FROM hashboard_installation i WHERE i.hashboard_id=NEW.asset_id AND i.device_id=NEW.device_id AND i.removed_at IS NOT NULL) THEN
  NEW.state := 'superseded';
  NEW.result_code := 'board_removed';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER pool_lifecycle_absent_board BEFORE INSERT ON pool_lifecycle_transition
 FOR EACH ROW EXECUTE FUNCTION retire_absent_board_pool_transition();

CREATE FUNCTION retire_removed_board_pool_transitions() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 -- Accepted removal, not stale telemetry, ends this physical installation.
 -- Claimed operations and physical attempts retain their recovery machinery.
 UPDATE pool_lifecycle_transition SET state='superseded',result_code='board_removed',updated_at=clock_timestamp()
 WHERE device_id=NEW.device_id AND asset_kind=7 AND asset_id=NEW.hashboard_id
  AND state IN ('pending','ready') AND operation_id IS NULL AND physical_attempt_id IS NULL;
 RETURN NEW;
END $$;
CREATE TRIGGER hashboard_removed_pool_transitions AFTER UPDATE OF removed_at ON hashboard_installation
 FOR EACH ROW WHEN (OLD.removed_at IS NULL AND NEW.removed_at IS NOT NULL)
 EXECUTE FUNCTION retire_removed_board_pool_transitions();

CREATE FUNCTION resume_reinstalled_board_pool_transition() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE organization bigint;
BEGIN
 SELECT org_id INTO organization FROM device WHERE id=NEW.device_id AND deleted_at IS NULL;
 IF NOT FOUND THEN RETURN NEW; END IF;
 -- Consume retired, undispatched intent once. A returning board gets a new
 -- policy for its current owner; an archived or changed default is not revived.
 UPDATE pool_lifecycle_transition SET result_code='board_reinstalled',updated_at=clock_timestamp()
 WHERE organization_id=organization AND asset_kind=7 AND asset_id=NEW.hashboard_id
  AND state='superseded' AND result_code='board_removed';
 IF NOT FOUND THEN RETURN NEW; END IF;
 INSERT INTO pool_lifecycle_transition(organization_id,device_id,asset_kind,asset_id,owner_account_id,allocation_revision,reason,configuration_id,revision_id)
 SELECT organization,NEW.device_id,7,NEW.hashboard_id,COALESCE(a.subclient_id,a.client_id,0),COALESCE(a.revision,0),'ownership',c.configuration_id,c.current_revision_id
 FROM (SELECT 1) seed LEFT JOIN hosted_account_asset a ON a.organization_id=organization AND a.kind=7 AND a.asset_id=NEW.hashboard_id
 LEFT JOIN pool_configuration c ON c.organization_id=organization
  AND COALESCE(c.account_id,0)=COALESCE(a.subclient_id,a.client_id,0) AND c.is_default AND NOT c.archived
 -- A controller move may already have captured the same current ownership
 -- policy when its observation updated placement, before installation insert.
 WHERE NOT EXISTS(SELECT 1 FROM pool_lifecycle_transition l
  WHERE l.organization_id=organization AND l.device_id=NEW.device_id AND l.asset_kind=7 AND l.asset_id=NEW.hashboard_id
   AND l.owner_account_id=COALESCE(a.subclient_id,a.client_id,0) AND l.allocation_revision=COALESCE(a.revision,0)
   AND l.state IN ('pending','ready','applying','uncertain'));
 RETURN NEW;
END $$;
CREATE TRIGGER hashboard_reinstalled_pool_transition AFTER INSERT ON hashboard_installation
 FOR EACH ROW WHEN (NEW.removed_at IS NULL) EXECUTE FUNCTION resume_reinstalled_board_pool_transition();

-- fleet-migration: 001079_internal_hosted_reports.up.sql
-- requires-shared: 1000
CREATE TABLE hosted_report (
 id bigserial CONSTRAINT pk_hosted_report PRIMARY KEY,
 report_id text NOT NULL CONSTRAINT uq_hosted_report_report_id UNIQUE,
 organization_id bigint NOT NULL,
 account_id bigint NOT NULL,
 series_id text NOT NULL,
 number bigint NOT NULL CONSTRAINT ck_hosted_report_number CHECK(number>0),
 correction_of bigint,
 correction_reason text NOT NULL DEFAULT '',
 kind text NOT NULL CONSTRAINT ck_hosted_report_kind CHECK(kind IN ('monthly','quarterly','annual','ad_hoc')),
 period_start timestamptz NOT NULL,
 period_end timestamptz NOT NULL,
 as_of timestamptz NOT NULL,
 timezone text NOT NULL,
 period jsonb NOT NULL,
 input_revision bigint NOT NULL CONSTRAINT ck_hosted_report_input_revision CHECK(input_revision>0),
 result jsonb NOT NULL,
 site_ids bigint[] NOT NULL,
 created_by bigint NOT NULL REFERENCES "user"(id),
 owner_id bigint NOT NULL REFERENCES "user"(id),
 status text NOT NULL DEFAULT 'draft' CONSTRAINT ck_hosted_report_status CHECK(status IN ('draft','issued')),
 revision bigint NOT NULL DEFAULT 1 CONSTRAINT ck_hosted_report_revision CHECK(revision>0),
 issued_at timestamptz,
 issued_by bigint REFERENCES "user"(id),
 incomplete_exception text NOT NULL DEFAULT '',
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 -- Monitoring evidence is not copied. A report's evidence is, per allocated
 -- interval and hour, the highest hosted_account_reporting_hour revision with
 -- captured_at<=evidence_cutoff, plus the gaps with recorded_at<=evidence_cutoff.
 evidence_cutoff timestamptz,
 CONSTRAINT uq_hosted_report_scope_id UNIQUE(organization_id,account_id,id),
 CONSTRAINT uq_hosted_report_series_number UNIQUE(organization_id,account_id,series_id,number),
 FOREIGN KEY(organization_id,account_id) REFERENCES hosted_account(organization_id,id),
 FOREIGN KEY(organization_id,account_id,correction_of) REFERENCES hosted_report(organization_id,account_id,id),
 CONSTRAINT ck_hosted_report_period CHECK(period_start<period_end AND as_of>=period_end),
 CONSTRAINT ck_hosted_report_correction CHECK((correction_of IS NULL AND number=1 AND correction_reason='') OR (correction_of IS NOT NULL AND number>1 AND length(btrim(correction_reason)) BETWEEN 1 AND 2000)),
 CONSTRAINT ck_hosted_report_issue CHECK((status='issued' AND issued_at IS NOT NULL AND issued_by IS NOT NULL AND evidence_cutoff IS NOT NULL) OR (status<>'issued' AND issued_at IS NULL AND issued_by IS NULL))
);
CREATE INDEX idx_hosted_report_queue ON hosted_report(organization_id,account_id,status,period_start DESC,id DESC);
CREATE FUNCTION guard_hosted_report() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='TRUNCATE' THEN RAISE EXCEPTION 'report history cannot be truncated'; END IF;
 IF OLD.status='issued' THEN RAISE EXCEPTION 'issued reports are immutable'; END IF;
 IF TG_OP='DELETE' THEN RETURN OLD; END IF;
 IF NEW.report_id<>OLD.report_id OR NEW.organization_id<>OLD.organization_id OR NEW.account_id<>OLD.account_id OR NEW.series_id<>OLD.series_id OR NEW.number<>OLD.number OR NEW.correction_of IS DISTINCT FROM OLD.correction_of THEN RAISE EXCEPTION 'report identity is immutable'; END IF;
 RETURN NEW;
END;
$$;
CREATE TRIGGER hosted_report_guard BEFORE UPDATE OR DELETE ON hosted_report FOR EACH ROW EXECUTE FUNCTION guard_hosted_report();
CREATE TRIGGER hosted_report_truncate_guard BEFORE TRUNCATE ON hosted_report FOR EACH STATEMENT EXECUTE FUNCTION guard_hosted_report();
CREATE TABLE hosted_report_delivery (
 id bigserial CONSTRAINT pk_hosted_report_delivery PRIMARY KEY,
 report_internal_id bigint NOT NULL REFERENCES hosted_report(id),
 request_key text NOT NULL,
 channel text NOT NULL,
 status text NOT NULL CONSTRAINT ck_hosted_report_delivery_status CHECK(status IN ('attempted','failed','confirmed')),
 sent_at timestamptz,
 received_at timestamptz,
 source_ref text NOT NULL,
 evidence text NOT NULL DEFAULT '',
 failure_category text NOT NULL DEFAULT '' CONSTRAINT ck_hosted_report_delivery_failure CHECK(failure_category IN ('','unavailable','rejected','unconfirmed','other')),
 actor_id bigint NOT NULL REFERENCES "user"(id),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 CONSTRAINT uq_hosted_report_delivery_request UNIQUE(report_internal_id,request_key),
 CONSTRAINT ck_hosted_report_delivery_confirmation CHECK(status<>'confirmed' OR (length(btrim(channel))>0 AND length(btrim(source_ref))>0 AND length(btrim(evidence))>0 AND sent_at IS NOT NULL)),
 CONSTRAINT ck_hosted_report_delivery_failed CHECK(status<>'failed' OR failure_category<>'')
);
CREATE UNIQUE INDEX idx_hosted_report_delivery_confirmation ON hosted_report_delivery(report_internal_id) WHERE status='confirmed';
CREATE TABLE hosted_report_request (
 id bigserial CONSTRAINT pk_hosted_report_request PRIMARY KEY,
 request_id text NOT NULL CONSTRAINT uq_hosted_report_request_request_id UNIQUE,
 organization_id bigint NOT NULL,
 account_id bigint NOT NULL,
 received_at timestamptz NOT NULL,
 source_ref text NOT NULL,
 requester text NOT NULL,
 scope text NOT NULL,
 owner_id bigint NOT NULL REFERENCES "user"(id),
 target_at timestamptz NOT NULL,
 limitations text NOT NULL,
 communication_ref text NOT NULL,
 delivery_ref text NOT NULL,
 actor_id bigint NOT NULL REFERENCES "user"(id),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 FOREIGN KEY(organization_id,account_id) REFERENCES hosted_account(organization_id,id)
);
CREATE INDEX idx_hosted_report_request_history ON hosted_report_request(organization_id,account_id,received_at DESC,id DESC);
CREATE FUNCTION reject_hosted_report_append_mutation() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN RAISE EXCEPTION 'report delivery and request history is append-only'; END;
$$;
CREATE TRIGGER hosted_report_delivery_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON hosted_report_delivery FOR EACH STATEMENT EXECUTE FUNCTION reject_hosted_report_append_mutation();
CREATE TRIGGER hosted_report_request_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON hosted_report_request FOR EACH STATEMENT EXECUTE FUNCTION reject_hosted_report_append_mutation();

-- One capture time per transaction for hourly evidence and gaps: the database
-- clock at the transaction's first evidence row. The writers
-- (hosted_reporting_hour_begin_close, _finish_close and _revise) take the
-- control row FOR UPDATE before their first insert and hold it until commit, so
-- every row of a writer step carries one time taken while that step held the
-- row, and any cutoff admits the step whole or not at all.
CREATE FUNCTION hosted_reporting_evidence_step_at() RETURNS timestamptz LANGUAGE plpgsql AS $$
DECLARE micros text := current_setting('hosted_reporting.evidence_step_at',true);
BEGIN
 IF micros IS NULL OR micros='' THEN
  micros := (extract(epoch FROM clock_timestamp())*1000000)::bigint::text;
  PERFORM set_config('hosted_reporting.evidence_step_at',micros,true);
 END IF;
 RETURN timestamptz 'epoch'+micros::bigint*INTERVAL '1 microsecond';
END;
$$;
ALTER TABLE hosted_account_reporting_hour ALTER COLUMN captured_at SET DEFAULT hosted_reporting_evidence_step_at();
ALTER TABLE hosted_account_reporting_hour_gap ALTER COLUMN recorded_at SET DEFAULT hosted_reporting_evidence_step_at();

-- The evidence cutoff of one report preparation, at most as_of. Writers of
-- hourly evidence and gaps hold the control row FOR UPDATE while they insert.
-- Once this share lock is granted, every row stamped at or before the returned
-- time is committed, and the wait below makes every later writer stamp a later
-- time. An as_of earlier than that time cannot split a committed step, because
-- the step's rows share one capture time. Call it as its own statement, outside
-- the preparation transaction, so the lock is released at once. It assumes the
-- database clock never steps backwards.
CREATE FUNCTION hosted_report_evidence_cutoff(as_of timestamptz) RETURNS timestamptz LANGUAGE plpgsql AS $$
DECLARE cutoff timestamptz;
BEGIN
 PERFORM 1 FROM hosted_account_reporting_hour_control WHERE singleton FOR SHARE;
 cutoff := clock_timestamp();
 WHILE clock_timestamp()<=cutoff LOOP NULL; END LOOP;
 RETURN LEAST(cutoff,as_of);
END;
$$;

-- fleet-migration: 001080_internal_hosted_report_artifacts.up.sql
-- requires-shared: 1000
-- Artifact chunks keep publication and downloads bounded in memory. Digests and
-- byte counts describe the exact frozen bytes, independently of future renderers.
CREATE TABLE hosted_report_artifact (
 id bigserial CONSTRAINT pk_hosted_report_artifact PRIMARY KEY,
 report_internal_id bigint NOT NULL REFERENCES hosted_report(id),
 name text NOT NULL CONSTRAINT ck_hosted_report_artifact_name CHECK(name IN ('report.html','evidence.zip','source-inputs.json')),
 media_type text NOT NULL,
 sha256 text NOT NULL DEFAULT '' CONSTRAINT ck_hosted_report_artifact_digest CHECK(sha256='' OR sha256 ~ '^[0-9a-f]{64}$'),
 byte_count bigint NOT NULL DEFAULT 0 CONSTRAINT ck_hosted_report_artifact_bytes CHECK(byte_count>=0),
 chunk_count bigint NOT NULL DEFAULT 0 CONSTRAINT ck_hosted_report_artifact_chunks CHECK(chunk_count>=0),
 CONSTRAINT uq_hosted_report_artifact_file UNIQUE(report_internal_id,name)
);
CREATE TABLE hosted_report_artifact_chunk (
 artifact_id bigint NOT NULL REFERENCES hosted_report_artifact(id),
 sequence bigint NOT NULL CONSTRAINT ck_hosted_report_artifact_sequence CHECK(sequence>=0),
 data bytea NOT NULL CONSTRAINT ck_hosted_report_artifact_chunk_size CHECK(octet_length(data) BETWEEN 1 AND 65536),
 CONSTRAINT pk_hosted_report_artifact_chunk PRIMARY KEY(artifact_id,sequence)
);
CREATE FUNCTION guard_hosted_report_artifact() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE report_key bigint;artifact_key bigint;
BEGIN
 IF TG_OP='TRUNCATE' THEN RAISE EXCEPTION 'report artifacts cannot be truncated'; END IF;
 IF TG_TABLE_NAME='hosted_report_artifact' THEN
  IF TG_OP='DELETE' THEN report_key:=OLD.report_internal_id; ELSE report_key:=NEW.report_internal_id; END IF;
  IF TG_OP='UPDATE' AND (OLD.report_internal_id<>NEW.report_internal_id OR OLD.name<>NEW.name OR OLD.media_type<>NEW.media_type OR OLD.sha256<>'') THEN RAISE EXCEPTION 'frozen artifact identity is immutable'; END IF;
 ELSE
  IF TG_OP<>'INSERT' THEN RAISE EXCEPTION 'artifact chunks are append-only'; END IF;
  artifact_key:=NEW.artifact_id;
  SELECT report_internal_id INTO report_key FROM hosted_report_artifact WHERE id=artifact_key AND sha256='';
 END IF;
 PERFORM id FROM hosted_report WHERE id=report_key AND status='draft' FOR UPDATE;
 IF NOT FOUND THEN RAISE EXCEPTION 'artifact writes require a draft report'; END IF;
 IF TG_OP='DELETE' THEN RETURN OLD; END IF;
 RETURN NEW;
END;
$$;
CREATE TRIGGER hosted_report_artifact_guard BEFORE INSERT OR UPDATE OR DELETE ON hosted_report_artifact FOR EACH ROW EXECUTE FUNCTION guard_hosted_report_artifact();
CREATE TRIGGER hosted_report_artifact_truncate_guard BEFORE TRUNCATE ON hosted_report_artifact FOR EACH STATEMENT EXECUTE FUNCTION guard_hosted_report_artifact();
CREATE TRIGGER hosted_report_artifact_chunk_guard BEFORE INSERT OR UPDATE OR DELETE ON hosted_report_artifact_chunk FOR EACH ROW EXECUTE FUNCTION guard_hosted_report_artifact();
CREATE TRIGGER hosted_report_artifact_chunk_truncate_guard BEFORE TRUNCATE ON hosted_report_artifact_chunk FOR EACH STATEMENT EXECUTE FUNCTION guard_hosted_report_artifact();

-- fleet-migration: 001081_internal_hashboard_worker_names.up.sql
-- requires-shared: 1074
-- A NULL name uses the plugin default; an empty name explicitly omits the suffix.
ALTER TABLE hashboard
    ADD COLUMN worker_name TEXT,
    ADD COLUMN worker_name_mode TEXT NOT NULL DEFAULT 'none'
        CONSTRAINT hashboard_worker_name_mode_check CHECK (worker_name_mode IN ('none', 'mac_address', 'custom'));

-- Activation can inherit a controller's input while the physical board retains
-- a different saved choice. Pool edits preserve this input until an explicit
-- worker-name confirmation replaces it.
ALTER TABLE pool_configuration_assignment ADD COLUMN inherited_worker_name TEXT;

-- Before this migration, {miner_name} used the controller's trimmed saved name,
-- MAC, or identifier, including for individually assigned hashboards. Preserve
-- that input for existing active assignments; new/unassigned boards stay blank.
-- The explicit trim set matches Go strings.TrimSpace, including Unicode spaces.
-- Inherited controller MACs are custom names, not each board's MAC strategy.
UPDATE hashboard h
SET worker_name = COALESCE(NULLIF(btrim(d.worker_name, U&'\0009\000A\000B\000C\000D\0020\0085\00A0\1680\2000\2001\2002\2003\2004\2005\2006\2007\2008\2009\200A\2028\2029\202F\205F\3000'), ''), NULLIF(btrim(d.mac_address, U&'\0009\000A\000B\000C\000D\0020\0085\00A0\1680\2000\2001\2002\2003\2004\2005\2006\2007\2008\2009\200A\2028\2029\202F\205F\3000'), ''), d.device_identifier),
    worker_name_mode = 'custom'
FROM hashboard_installation i
JOIN container_module m ON m.id = i.module_id AND m.retired_at IS NULL
JOIN container_tank tank ON tank.id = m.tank_id AND tank.present AND tank.device_id = i.device_id
JOIN device d ON d.id = i.device_id AND d.deleted_at IS NULL
JOIN discovered_device dd ON dd.id = d.discovered_device_id AND dd.org_id = d.org_id
WHERE h.id = i.hashboard_id AND h.org_id = d.org_id AND h.worker_name IS NULL
    AND i.removed_at IS NULL AND i.site_id IS NOT DISTINCT FROM d.site_id
    AND dd.device_class = 2 AND dd.deleted_at IS NULL
    AND EXISTS (
        SELECT 1 FROM pool_configuration_assignment a
        JOIN pool_configuration_revision r ON r.organization_id = a.organization_id
            AND r.configuration_id = a.configuration_id AND r.revision_id = a.revision_id
        WHERE a.organization_id = d.org_id AND a.device_id = d.id AND a.retired_at IS NULL
            AND ((a.target_kind = 'hashboard_installation' AND a.target_id = i.id)
                OR (a.target_kind = 'device' AND a.target_id = d.id))
            AND EXISTS (
                SELECT 1 FROM jsonb_array_elements(r.slots) slot
                WHERE strpos(slot->>'worker_template', '{miner_name}') > 0
            )
    );

-- Existing miner and whole-container assignments need an explicit name before
-- empty metadata gains the new no-suffix meaning. Keep the old spelling/case exactly;
-- normalizing a MAC here would change an already configured pool username.
UPDATE device d
SET worker_name = COALESCE(NULLIF(btrim(d.worker_name, U&'\0009\000A\000B\000C\000D\0020\0085\00A0\1680\2000\2001\2002\2003\2004\2005\2006\2007\2008\2009\200A\2028\2029\202F\205F\3000'), ''), NULLIF(btrim(d.mac_address, U&'\0009\000A\000B\000C\000D\0020\0085\00A0\1680\2000\2001\2002\2003\2004\2005\2006\2007\2008\2009\200A\2028\2029\202F\205F\3000'), ''), d.device_identifier)
FROM discovered_device dd
WHERE dd.id = d.discovered_device_id AND dd.org_id = d.org_id
    AND dd.device_class IN (1, 2) AND dd.deleted_at IS NULL AND d.deleted_at IS NULL
    AND EXISTS (
        SELECT 1 FROM pool_configuration_assignment a
        JOIN pool_configuration_revision r ON r.organization_id = a.organization_id
            AND r.configuration_id = a.configuration_id AND r.revision_id = a.revision_id
        WHERE a.organization_id = d.org_id AND a.device_id = d.id
            AND a.target_kind = 'device' AND a.target_id = d.id AND a.retired_at IS NULL
            AND EXISTS (
                SELECT 1 FROM jsonb_array_elements(r.slots) slot
                WHERE strpos(slot->>'worker_template', '{miner_name}') > 0
            )
    );

-- fleet-migration: 001082_internal_ct1_mining_group_guards.up.sql
-- requires-shared: 1000
CREATE FUNCTION ct1_hashboard_allocation_ready(target_org bigint, controller bigint)
RETURNS boolean LANGUAGE sql VOLATILE AS $$
 SELECT EXISTS (
  SELECT 1 FROM container_mining_group_state s
  JOIN device_write_fence f ON f.device_id=s.device_id AND f.organization_id=s.organization_id
  WHERE s.organization_id=target_org AND s.device_id=controller
   AND f.state='host' AND s.status NOT IN ('pending','applying','blocked')
   AND s.observed_at>=f.updated_at
   AND s.observed_at>clock_timestamp()-interval '2 minutes' AND s.observed_at<=clock_timestamp()+interval '5 seconds'
   AND COALESCE((s.observed->>'revision')::bigint,0)>0
   AND s.observed @> '{"supported":true,"enabled":true,"suspended":false,"pending":false}'::jsonb
   AND NOT EXISTS (SELECT 1 FROM device_write_attempt a WHERE a.device_id=controller
    AND a.organization_id=target_org AND a.operation_kind='mutation' AND a.status IN ('running','unknown'))
 );
$$;

ALTER TABLE container_mining_group_state DROP CONSTRAINT container_mining_group_state_operation_kind_check;
ALTER TABLE container_mining_group_state ADD CONSTRAINT chk_container_mining_group_state_operation_kind
 CHECK (operation_kind IN ('','activate','deactivate','pools'));

ALTER TABLE pool_lifecycle_transition DROP CONSTRAINT pool_lifecycle_transition_state_check;
ALTER TABLE pool_lifecycle_transition ADD CONSTRAINT chk_pool_lifecycle_transition_state
 CHECK (state IN ('pending','deferred','ready','applying','verified','completed','failed','uncertain','superseded'));

-- Extend the parent's open-work index for deferred trigger and reconciliation
-- lookups. Retained terminal history stays outside the indexed working set.
DROP INDEX idx_pool_lifecycle_transition_open_asset;
CREATE INDEX idx_pool_lifecycle_transition_open_asset
 ON pool_lifecycle_transition(state,organization_id,asset_kind,asset_id)
 WHERE state IN ('pending','deferred','ready','applying','uncertain','failed');

-- Allocation guards also inspect uncertain work. Keep those rows in the
-- assignment-scoped working set instead of scanning retained target history.
DROP INDEX idx_pool_configuration_operation_target_open;
CREATE INDEX idx_pool_configuration_operation_target_open
 ON pool_configuration_operation_target(organization_id,target_kind,target_id)
 WHERE state IN ('pending','applying','uncertain','failed','different','applied_unverified');

-- All workflow guards and the lifecycle worker use the same safe retry rule.
CREATE FUNCTION pool_lifecycle_clear_retryable(l pool_lifecycle_transition,w device_write_attempt)
RETURNS boolean LANGUAGE sql IMMUTABLE AS $$
 SELECT l.state='failed' AND l.result_code='clear_not_dispatched' AND l.reason='ownership'
  AND l.configuration_id IS NULL AND l.target_kind='device' AND w.status='complete'
  AND w.operation_kind='mutation' AND w.command_type='UpdateMiningPools' AND w.write_outcome='{}'::jsonb;
$$;

CREATE INDEX idx_hosted_account_asset_allocated_hashboard_device
 ON hosted_account_asset(organization_id,device_id)
 WHERE kind=7 AND client_id IS NOT NULL AND device_id IS NOT NULL;

LOCK TABLE organization IN SHARE ROW EXCLUSIVE MODE;
INSERT INTO permission(key,description) VALUES ('miner:deactivate_mining_groups','Disable per-hashboard pool configuration on an unallocated container.') ON CONFLICT(key) DO NOTHING;
INSERT INTO role_permission(role_id,permission_id)
SELECT r.id,p.id FROM role r CROSS JOIN permission p
WHERE r.builtin_key='ADMIN' AND r.deleted_at IS NULL AND p.key='miner:deactivate_mining_groups'
ON CONFLICT(role_id,permission_id) DO NOTHING;

-- Retain automatic policy without rejecting its inventory or pairing change.
-- Deferred work waits for the admitted deactivation without blocking it.
CREATE FUNCTION guard_pool_lifecycle_mining_group_deactivation() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 -- Rigs cannot run mining-group deactivation. Keep their ownership and pairing
 -- writes independent of physical-attempt finalization on the device fence.
 IF NEW.state<>'pending' OR NOT EXISTS (
  SELECT 1 FROM device d JOIN discovered_device dd ON dd.id=d.discovered_device_id
  WHERE d.id=NEW.device_id AND d.org_id=NEW.organization_id AND dd.device_class=2
 ) THEN
  RETURN NEW;
 END IF;
 PERFORM 1 FROM device_write_fence WHERE device_id=NEW.device_id AND organization_id=NEW.organization_id FOR UPDATE;
 IF NEW.state='pending' AND EXISTS (SELECT 1 FROM container_mining_group_state WHERE device_id=NEW.device_id
  AND organization_id=NEW.organization_id AND operation_kind='deactivate' AND status IN ('pending','applying','blocked')) THEN
  NEW.state := 'deferred';
  NEW.result_code := 'mining_groups_deactivation';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER pool_lifecycle_mining_group_deactivation BEFORE INSERT ON pool_lifecycle_transition
 FOR EACH ROW EXECUTE FUNCTION guard_pool_lifecycle_mining_group_deactivation();

-- Deferred policies have no physical admission. Retire and deduplicate them
-- with pending policy when ownership, pairing or physical installation changes.
CREATE OR REPLACE FUNCTION enqueue_ownership_pool_transition() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE owner bigint;
BEGIN
 IF TG_OP='UPDATE' AND NEW.client_id IS NOT DISTINCT FROM OLD.client_id
  AND NEW.subclient_id IS NOT DISTINCT FROM OLD.subclient_id AND NEW.device_id=OLD.device_id THEN RETURN NEW; END IF;
 IF TG_OP='INSERT' AND NEW.client_id IS NULL THEN RETURN NEW; END IF;
 owner := COALESCE(NEW.subclient_id,NEW.client_id,0);
 UPDATE pool_lifecycle_transition SET state='superseded',updated_at=clock_timestamp()
 WHERE organization_id=NEW.organization_id AND ((asset_kind=NEW.kind AND asset_id=NEW.asset_id) OR (asset_kind=0 AND NEW.kind=5 AND device_id=NEW.device_id))
  AND state IN ('pending','deferred','ready','failed');
 INSERT INTO pool_lifecycle_transition(organization_id,device_id,asset_kind,asset_id,owner_account_id,allocation_revision,reason,configuration_id,revision_id)
 SELECT NEW.organization_id,NEW.device_id,NEW.kind,NEW.asset_id,owner,NEW.revision,'ownership',c.configuration_id,c.current_revision_id
 FROM (SELECT 1) seed LEFT JOIN pool_configuration c ON c.organization_id=NEW.organization_id
  AND COALESCE(c.account_id,0)=owner AND c.is_default AND NOT c.archived;
 RETURN NEW;
END $$;

CREATE OR REPLACE FUNCTION enqueue_pairing_pool_transition() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE organization bigint; class integer; cycle uuid;
BEGIN
 -- Authentication/endpoint recovery is still the same pairing cycle. Only
 -- an explicit unpair resets it, including local unpairs without unpaired_at.
 IF NEW.pairing_status='UNPAIRED' THEN
  DELETE FROM pool_pairing_cycle WHERE device_id=NEW.device_id;
  -- Cancel unclaimed intent, including expanded CT1 children. Accepted work
  -- checks the cycle at dispatch; its physical evidence and recovery stay intact.
  UPDATE pool_lifecycle_transition SET state='superseded',result_code='pairing_cycle_ended',updated_at=clock_timestamp()
  WHERE organization_id=(SELECT org_id FROM device WHERE id=NEW.device_id) AND device_id=NEW.device_id
   AND reason='pairing' AND state IN ('pending','deferred','ready')
   AND operation_id IS NULL AND physical_attempt_id IS NULL;
  RETURN NEW;
 END IF;
 IF NEW.pairing_status NOT IN ('PAIRED','DEFAULT_PASSWORD') THEN RETURN NEW; END IF;
 SELECT d.org_id,dd.device_class INTO organization,class FROM device d JOIN discovered_device dd ON dd.id=d.discovered_device_id WHERE d.id=NEW.device_id AND d.deleted_at IS NULL;
 IF NOT FOUND THEN RETURN NEW; END IF;
 INSERT INTO pool_pairing_cycle(device_id) VALUES(NEW.device_id) ON CONFLICT DO NOTHING RETURNING cycle_id INTO cycle;
 IF NOT FOUND THEN RETURN NEW; END IF;
 IF class=2 THEN
  INSERT INTO pool_lifecycle_transition(organization_id,device_id,asset_kind,asset_id,reason,pairing_cycle_id,pairing_defaults)
  SELECT organization,NEW.device_id,0,NEW.device_id,'pairing',cycle,COALESCE(jsonb_object_agg(COALESCE(c.account_id,0)::text,jsonb_build_object('configuration_id',c.configuration_id,'revision_id',c.current_revision_id)),'{}')
  FROM pool_configuration c WHERE c.organization_id=organization AND c.is_default AND NOT c.archived
   AND (c.account_id IS NULL OR EXISTS(SELECT 1 FROM hosted_account_asset a WHERE a.organization_id=organization AND a.device_id=NEW.device_id AND COALESCE(a.subclient_id,a.client_id)=c.account_id));
 ELSE
  INSERT INTO pool_lifecycle_transition(organization_id,device_id,asset_kind,asset_id,owner_account_id,allocation_revision,reason,pairing_cycle_id,configuration_id,revision_id)
  SELECT organization,NEW.device_id,5,NEW.device_id,COALESCE(a.subclient_id,a.client_id,0),COALESCE(a.revision,0),'pairing',cycle,c.configuration_id,c.current_revision_id
  FROM (SELECT 1) seed LEFT JOIN hosted_account_asset a ON a.organization_id=organization AND a.kind=5 AND a.asset_id=NEW.device_id
  LEFT JOIN pool_configuration c ON c.organization_id=organization AND COALESCE(c.account_id,0)=COALESCE(a.subclient_id,a.client_id,0) AND c.is_default AND NOT c.archived;
 END IF;
 RETURN NEW;
END $$;

CREATE OR REPLACE FUNCTION retire_absent_board_pool_transition() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.asset_kind=7 AND NEW.state IN ('pending','deferred') AND NEW.operation_id IS NULL AND NEW.physical_attempt_id IS NULL
  AND NOT EXISTS(SELECT 1 FROM hashboard_installation i WHERE i.hashboard_id=NEW.asset_id AND i.device_id=NEW.device_id AND i.removed_at IS NULL)
  AND EXISTS(SELECT 1 FROM hashboard_installation i WHERE i.hashboard_id=NEW.asset_id AND i.device_id=NEW.device_id AND i.removed_at IS NOT NULL) THEN
  NEW.state := 'superseded';
  NEW.result_code := 'board_removed';
 END IF;
 RETURN NEW;
END $$;

CREATE OR REPLACE FUNCTION retire_removed_board_pool_transitions() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 -- Accepted removal, not stale telemetry, ends this physical installation.
 -- Claimed operations and physical attempts retain their recovery machinery.
 UPDATE pool_lifecycle_transition SET state='superseded',result_code='board_removed',updated_at=clock_timestamp()
 WHERE device_id=NEW.device_id AND asset_kind=7 AND asset_id=NEW.hashboard_id
  AND state IN ('pending','deferred','ready') AND operation_id IS NULL AND physical_attempt_id IS NULL;
 RETURN NEW;
END $$;

CREATE OR REPLACE FUNCTION resume_reinstalled_board_pool_transition() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE organization bigint;
BEGIN
 SELECT org_id INTO organization FROM device WHERE id=NEW.device_id AND deleted_at IS NULL;
 IF NOT FOUND THEN RETURN NEW; END IF;
 -- Consume retired, undispatched intent once. A returning board gets a new
 -- policy for its current owner; an archived or changed default is not revived.
 UPDATE pool_lifecycle_transition SET result_code='board_reinstalled',updated_at=clock_timestamp()
 WHERE organization_id=organization AND asset_kind=7 AND asset_id=NEW.hashboard_id
  AND state='superseded' AND result_code='board_removed';
 IF NOT FOUND THEN RETURN NEW; END IF;
 INSERT INTO pool_lifecycle_transition(organization_id,device_id,asset_kind,asset_id,owner_account_id,allocation_revision,reason,configuration_id,revision_id)
 SELECT organization,NEW.device_id,7,NEW.hashboard_id,COALESCE(a.subclient_id,a.client_id,0),COALESCE(a.revision,0),'ownership',c.configuration_id,c.current_revision_id
 FROM (SELECT 1) seed LEFT JOIN hosted_account_asset a ON a.organization_id=organization AND a.kind=7 AND a.asset_id=NEW.hashboard_id
 LEFT JOIN pool_configuration c ON c.organization_id=organization
  AND COALESCE(c.account_id,0)=COALESCE(a.subclient_id,a.client_id,0) AND c.is_default AND NOT c.archived
 -- A controller move may already have captured the same current ownership
 -- policy when its observation updated placement, before installation insert.
 WHERE NOT EXISTS(SELECT 1 FROM pool_lifecycle_transition l
  WHERE l.organization_id=organization AND l.device_id=NEW.device_id AND l.asset_kind=7 AND l.asset_id=NEW.hashboard_id
   AND l.owner_account_id=COALESCE(a.subclient_id,a.client_id,0) AND l.allocation_revision=COALESCE(a.revision,0)
   AND l.state IN ('pending','deferred','ready','applying','uncertain'));
 RETURN NEW;
END $$;

-- fleet-migration: 001083_internal_mining_group_default_return.up.sql
-- requires-shared: 1000
ALTER TABLE container_mining_group_state ADD COLUMN default_return JSONB;
ALTER TABLE container_mining_group_state DROP CONSTRAINT chk_container_mining_group_state_operation_kind;
ALTER TABLE container_mining_group_state ADD CONSTRAINT chk_container_mining_group_state_operation_kind
 CHECK (operation_kind IN ('','activate','deactivate','pools','return_default'));

-- fleet-migration: 001084_internal_mining_group_step_dispatch.up.sql
-- requires-shared: 1000
CREATE TABLE mining_group_step_dispatch (
 step_key UUID PRIMARY KEY,
 attempt_id UUID NOT NULL REFERENCES device_write_attempt(attempt_id),
 dispatch_started BOOLEAN NOT NULL DEFAULT FALSE,
 dispatch_token UUID NOT NULL DEFAULT gen_random_uuid()
);
CREATE INDEX idx_mining_group_step_dispatch_attempt ON mining_group_step_dispatch(attempt_id);
-- Existing saved requests have unknown delivery. Never backfill them as unsent.

-- fleet-migration: 001085_internal_container_power_feed_permission.up.sql
-- requires-shared: 1074
-- CT1 module power-feed control (turn on, turn off, cycle, clear override).
-- Startup reconciliation grants it to SUPER_ADMIN and seeds it for new ADMIN
-- roles; existing ADMIN roles receive it here. FIELD_TECH and custom roles
-- receive it only when granted explicitly.
INSERT INTO permission (key, description) VALUES
    ('container:power_feed', 'Turn a CT1 module''s power feed on or off, cycle it, and clear its operator override. Cutting a feed removes power from the module''s boards without stopping mining first.')
ON CONFLICT (key) DO NOTHING;
INSERT INTO role_permission (role_id, permission_id)
SELECT r.id, p.id FROM role r CROSS JOIN permission p
WHERE r.builtin_key = 'ADMIN' AND r.deleted_at IS NULL AND p.key = 'container:power_feed'
ON CONFLICT (role_id, permission_id) DO NOTHING;

-- fleet-migration: 001086_internal_hosted_report_queue.up.sql
-- requires-shared: 1000
CREATE TABLE hosted_report_queue (
 id bigserial CONSTRAINT pk_hosted_report_queue PRIMARY KEY,
 queue_id text NOT NULL CONSTRAINT uq_hosted_report_queue_queue_id UNIQUE,
 organization_id bigint NOT NULL,
 account_id bigint NOT NULL,
 work_key text NOT NULL,
 owner_id bigint NOT NULL REFERENCES "user"(id),
 site_ids bigint[] NOT NULL,
 projection jsonb NOT NULL,
 fingerprint text NOT NULL,
 generation bigint NOT NULL DEFAULT 1 CONSTRAINT ck_hosted_report_queue_generation CHECK(generation>0),
 notification_status text NOT NULL DEFAULT 'pending' CONSTRAINT ck_hosted_report_queue_notification CHECK(notification_status IN ('pending','sent','failed','acknowledged','quiet')),
 attempts bigint NOT NULL DEFAULT 0,
 retry_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 claim_token text,
 lease_until timestamptz,
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 CONSTRAINT uq_hosted_report_queue_work UNIQUE(organization_id,account_id,work_key),
 FOREIGN KEY(organization_id,account_id) REFERENCES hosted_account(organization_id,id)
);
CREATE INDEX idx_hosted_report_queue_outbox ON hosted_report_queue(retry_at,id) WHERE notification_status IN ('pending','failed');
CREATE TABLE hosted_report_queue_event (
 id bigserial CONSTRAINT pk_hosted_report_queue_event PRIMARY KEY,
 queue_internal_id bigint NOT NULL REFERENCES hosted_report_queue(id),
 generation bigint NOT NULL,
 kind text NOT NULL CONSTRAINT ck_hosted_report_queue_event_kind CHECK(kind IN ('changed','attempted','sent','failed','acknowledged','retry')),
 actor_id bigint REFERENCES "user"(id),
 reason text NOT NULL DEFAULT '',
 created_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE INDEX idx_hosted_report_queue_event_item ON hosted_report_queue_event(queue_internal_id,id);
CREATE FUNCTION reject_hosted_report_queue_event_mutation() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN RAISE EXCEPTION 'report queue event history is append-only'; END;
$$;
CREATE TRIGGER hosted_report_queue_event_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON hosted_report_queue_event FOR EACH STATEMENT EXECUTE FUNCTION reject_hosted_report_queue_event_mutation();

-- fleet-migration: 001087_internal_hosted_report_request_fulfillment.up.sql
-- requires-shared: 1000
CREATE TABLE hosted_report_request_fulfillment (
 id bigserial CONSTRAINT pk_hosted_report_request_fulfillment PRIMARY KEY,
 request_internal_id bigint NOT NULL REFERENCES hosted_report_request(id),
 entry_key text NOT NULL,
 delivered_at timestamptz NOT NULL,
 delivery_ref text NOT NULL,
 communication_ref text NOT NULL,
 evidence text NOT NULL,
 actor_id bigint NOT NULL REFERENCES "user"(id),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 CONSTRAINT uq_hosted_report_request_fulfillment_request_entry UNIQUE(request_internal_id,entry_key)
);
CREATE TRIGGER hosted_report_request_fulfillment_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON hosted_report_request_fulfillment FOR EACH STATEMENT EXECUTE FUNCTION reject_hosted_report_append_mutation();
CREATE INDEX idx_hosted_report_issued_request
 ON hosted_report(organization_id,account_id,(period->>'requestRef'))
 WHERE kind='ad_hoc' AND status='issued';

-- fleet-migration: 001088_internal_hosted_account_analyses.up.sql
-- requires-shared: 1000
CREATE TABLE hosted_account_analysis (
 id bigserial CONSTRAINT pk_hosted_account_analysis PRIMARY KEY,
 analysis_id text NOT NULL CONSTRAINT uq_hosted_account_analysis_analysis_id UNIQUE,
 organization_id bigint NOT NULL,
 account_id bigint NOT NULL,
 kind text NOT NULL CONSTRAINT ck_hosted_account_analysis_kind CHECK(kind IN ('miner_performance','energy_usage','issues_repairs')),
 snapshot jsonb NOT NULL,
 site_ids bigint[] NOT NULL,
 ticket_ids bigint[] NOT NULL,
 created_by bigint NOT NULL REFERENCES "user"(id),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 csv_data bytea NOT NULL,
 sha256 text NOT NULL,
 CONSTRAINT fk_hosted_account_analysis_account FOREIGN KEY(organization_id,account_id) REFERENCES hosted_account(organization_id,id),
 CONSTRAINT ck_hosted_account_analysis_size CHECK(octet_length(csv_data)<=16777216 AND octet_length(snapshot::text)<=16777216)
);
CREATE INDEX idx_hosted_account_analysis_library ON hosted_account_analysis(organization_id,account_id,created_at DESC,id DESC);
CREATE FUNCTION reject_hosted_account_analysis_mutation() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN RAISE EXCEPTION 'saved analyses are immutable'; END;
$$;
CREATE TRIGGER hosted_account_analysis_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON hosted_account_analysis FOR EACH STATEMENT EXECUTE FUNCTION reject_hosted_account_analysis_mutation();

-- fleet-migration: 001089_internal_hosted_reporting_delivery.up.sql
-- requires-shared: 1000
ALTER TABLE hosted_report_queue ADD COLUMN delivered_channel_ids bigint[] NOT NULL DEFAULT '{}';
ALTER TABLE hosted_report_queue DROP CONSTRAINT ck_hosted_report_queue_notification;
ALTER TABLE hosted_report_queue ADD CONSTRAINT ck_hosted_report_queue_notification CHECK(notification_status IN ('pending','sent','failed','acknowledged','quiet','unrouted','exhausted','blocked'));
ALTER TABLE hosted_report_queue_event DROP CONSTRAINT ck_hosted_report_queue_event_kind;
ALTER TABLE hosted_report_queue_event ADD CONSTRAINT ck_hosted_report_queue_event_kind CHECK(kind IN ('changed','attempted','sent','failed','acknowledged','retry','unrouted','exhausted','blocked'));

ALTER TABLE hosted_account_analysis ADD COLUMN request_key text NOT NULL DEFAULT gen_random_uuid()::text;
ALTER TABLE hosted_account_analysis ADD COLUMN request_digest text NOT NULL DEFAULT '';
CREATE UNIQUE INDEX uq_hosted_account_analysis_request ON hosted_account_analysis(organization_id,account_id,request_key);

CREATE INDEX idx_hosted_report_queue_active_page ON hosted_report_queue(organization_id,id DESC) WHERE notification_status<>'quiet';

-- fleet-migration: 001090_internal_account_analysis_deletion.up.sql
-- requires-shared: 1000
CREATE TABLE hosted_account_analysis_deletion (
 analysis_id text CONSTRAINT pk_hosted_account_analysis_deletion PRIMARY KEY,
 organization_id bigint NOT NULL,
 account_id bigint NOT NULL,
 created_by bigint NOT NULL REFERENCES "user"(id),
 deleted_by bigint NOT NULL REFERENCES "user"(id),
 deleted_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 request_key text NOT NULL,
 request_digest text NOT NULL,
 CONSTRAINT fk_hosted_account_analysis_deletion_account FOREIGN KEY(organization_id,account_id) REFERENCES hosted_account(organization_id,id),
 CONSTRAINT uq_hosted_account_analysis_deletion_request UNIQUE(organization_id,account_id,request_key)
);

CREATE FUNCTION guard_hosted_account_analysis_deletion_receipt() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP <> 'INSERT' THEN
  RAISE EXCEPTION 'analysis deletion receipts are immutable';
 END IF;
 IF NOT EXISTS (
  SELECT 1 FROM hosted_account_analysis a WHERE a.analysis_id=NEW.analysis_id
   AND a.organization_id=NEW.organization_id AND a.account_id=NEW.account_id
   AND a.created_by=NEW.created_by AND a.request_key=NEW.request_key AND a.request_digest=NEW.request_digest
 ) THEN
  RAISE EXCEPTION 'analysis deletion receipt must match a saved analysis';
 END IF;
 RETURN NEW;
END;
$$;
CREATE TRIGGER hosted_account_analysis_deletion_receipt_insert BEFORE INSERT ON hosted_account_analysis_deletion
 FOR EACH ROW EXECUTE FUNCTION guard_hosted_account_analysis_deletion_receipt();
CREATE TRIGGER hosted_account_analysis_deletion_receipt_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON hosted_account_analysis_deletion
 FOR EACH STATEMENT EXECUTE FUNCTION guard_hosted_account_analysis_deletion_receipt();

CREATE FUNCTION guard_hosted_account_analysis_deletion() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP = 'INSERT' THEN
  IF EXISTS (
   SELECT 1 FROM hosted_account_analysis_deletion d WHERE d.analysis_id=NEW.analysis_id
    OR (d.organization_id=NEW.organization_id AND d.account_id=NEW.account_id AND d.request_key=NEW.request_key)
  ) THEN
   RAISE EXCEPTION 'a permanently deleted analysis cannot be recreated';
  END IF;
  RETURN NEW;
 END IF;
 IF NOT EXISTS (
  SELECT 1 FROM hosted_account_analysis_deletion d WHERE d.analysis_id=OLD.analysis_id
   AND d.organization_id=OLD.organization_id AND d.account_id=OLD.account_id
   AND d.created_by=OLD.created_by AND d.request_key=OLD.request_key AND d.request_digest=OLD.request_digest
 ) THEN
  RAISE EXCEPTION 'analysis deletion requires a retained matching receipt';
 END IF;
 RETURN OLD;
END;
$$;
DROP TRIGGER hosted_account_analysis_immutable ON hosted_account_analysis;
CREATE TRIGGER hosted_account_analysis_immutable BEFORE UPDATE OR TRUNCATE ON hosted_account_analysis
 FOR EACH STATEMENT EXECUTE FUNCTION reject_hosted_account_analysis_mutation();
CREATE TRIGGER hosted_account_analysis_delete BEFORE DELETE ON hosted_account_analysis
 FOR EACH ROW EXECUTE FUNCTION guard_hosted_account_analysis_deletion();
CREATE TRIGGER hosted_account_analysis_no_recreation BEFORE INSERT ON hosted_account_analysis
 FOR EACH ROW EXECUTE FUNCTION guard_hosted_account_analysis_deletion();

-- fleet-migration: 001091_internal_hosted_report_generation_permission.up.sql
-- requires-shared: 1000
LOCK TABLE organization IN SHARE ROW EXCLUSIVE MODE;
LOCK TABLE hosted_account, hosted_account_ancestor_grant IN ACCESS EXCLUSIVE MODE;

ALTER TABLE hosted_account DROP CONSTRAINT hosted_account_capabilities_check;
ALTER TABLE hosted_account ADD CONSTRAINT hosted_account_capabilities_check CHECK (capabilities BETWEEN 1 AND 63);
ALTER TABLE hosted_account_ancestor_grant DROP CONSTRAINT hosted_account_ancestor_grant_capabilities_check;
ALTER TABLE hosted_account_ancestor_grant ADD CONSTRAINT hosted_account_ancestor_grant_capabilities_check CHECK (capabilities BETWEEN 1 AND 63);

-- Preserve existing direct administrators' generation access. Complete source
-- grants continue to support the administrator role; partial grants stay exact.
-- Generation still requires direct membership and never comes from preview.
UPDATE hosted_account SET capabilities=capabilities | 32,
 revision=revision+1, authority_revision=authority_revision+1, updated_at=CURRENT_TIMESTAMP;
UPDATE hosted_account_ancestor_grant SET capabilities=63,
 revision=revision+1, updated_at=CURRENT_TIMESTAMP WHERE capabilities=31;

CREATE OR REPLACE FUNCTION create_hosted_account_root() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
 INSERT INTO hosted_account(account_id,organization_id,depth,name,capabilities)
 VALUES(gen_random_uuid()::text,NEW.id,0,COALESCE(NULLIF(btrim(NEW.name),''),'Host ' || NEW.id::text),63);
 RETURN NEW;
END;
$$;

-- fleet-migration: 001092_internal_hosted_curtailment_observations.up.sql
-- requires-shared: 1000
-- Prospective application capture only. Mutable target summaries are not a
-- historical source and are deliberately not backfilled. No triggers are added.
CREATE TABLE hosted_curtailment_confirmation (
 id bigserial PRIMARY KEY,
 evidence_id uuid NOT NULL DEFAULT gen_random_uuid(),
 organization_id bigint NOT NULL,
 event_id bigint NOT NULL,
 device_id bigint NOT NULL,
 curtail_batch text NOT NULL,
 phase text NOT NULL CHECK (phase IN ('curtail','restore')),
 phase_batch text NOT NULL,
 sample_at timestamptz NOT NULL,
 confirmed_at timestamptz NOT NULL CHECK(confirmed_at>=sample_at),
 captured_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 CONSTRAINT uq_hosted_curtailment_confirmation_evidence UNIQUE(evidence_id),
 CONSTRAINT uq_hosted_curtailment_confirmation_cycle UNIQUE(organization_id,event_id,device_id,curtail_batch,phase,phase_batch)
);
CREATE INDEX idx_hosted_curtailment_confirmation_restore
 ON hosted_curtailment_confirmation(organization_id,event_id,device_id,curtail_batch,confirmed_at)
 WHERE phase='restore';

CREATE INDEX idx_hosted_curtailment_confirmation_next
 ON hosted_curtailment_confirmation(organization_id,device_id,confirmed_at) WHERE phase='curtail';

-- Freeze the original customer cohort, including physical installation for
-- shared containers. Later allocations cannot acquire earlier observations.
CREATE TABLE hosted_curtailment_asset (
 confirmation_id bigint NOT NULL REFERENCES hosted_curtailment_confirmation(id),
 interval_id bigint NOT NULL REFERENCES hosted_account_asset_interval(id),
 installation_id bigint REFERENCES hashboard_installation(id),
 organization_id bigint NOT NULL,
 client_id bigint NOT NULL,
 subclient_id bigint,
 site_id bigint,
 kind integer NOT NULL CHECK(kind IN (5,7)),
 asset_id bigint NOT NULL,
 PRIMARY KEY(confirmation_id,interval_id)
);
CREATE INDEX idx_hosted_curtailment_asset_client
 ON hosted_curtailment_asset(organization_id,client_id,confirmation_id);
CREATE INDEX idx_hosted_curtailment_asset_subclient
 ON hosted_curtailment_asset(organization_id,subclient_id,confirmation_id) WHERE subclient_id IS NOT NULL;

-- fleet-migration: 001093_internal_minute_rollup_daily_chunks.up.sql
-- requires-shared: 1000
-- Whole-chunk expiry should track the minute rollup's 14-day retention more
-- closely. Only future chunks change; existing chunks expire on their old bounds.
SELECT set_chunk_time_interval('miner_state_snapshot_device_1m', INTERVAL '1 day');

-- fleet-migration: 001094_internal_remove_minute_rollup_device_index.up.sql
-- requires-shared: 1000
-- Both minute-rollup readers filter by org_id. Keep their org/device/bucket
-- index and the bucket-only and org/bucket indexes used by fleet-wide reads.
-- Dropping the parent index also removes its chunk indexes and stops Timescale
-- from copying it to future chunks. The migration runner owns the transaction.
DO $$
DECLARE
    materialization_schema text;
    materialization_table text;
BEGIN
    SELECT materialization_hypertable_schema, materialization_hypertable_name
    INTO STRICT materialization_schema, materialization_table
    FROM timescaledb_information.continuous_aggregates
    WHERE view_schema = 'public'
      AND view_name = 'miner_state_snapshot_device_1m';

    -- Resolve the generated name rather than assuming a materialization ID.
    EXECUTE format('DROP INDEX %I.%I', materialization_schema,
                   materialization_table || '_device_identifier_bucket_idx');
END
$$;

-- fleet-migration: 001095_internal_diagnostic_history_compression.up.sql
-- requires-shared: 1000
-- Preserve every observation and the existing daily chunks/30-day retention.
-- Schedule compression rather than rewriting retained history in the migration.
ALTER TABLE hashboard_diagnostic_observation SET (
 timescaledb.compress,
 timescaledb.compress_segmentby = 'hashboard_id',
 timescaledb.compress_orderby = 'collected_at DESC'
);
SELECT add_compression_policy('hashboard_diagnostic_observation', INTERVAL '48 hours', schedule_interval => INTERVAL '1 hour');

ALTER TABLE container_module_diagnostic_observation SET (
 timescaledb.compress,
 timescaledb.compress_segmentby = 'module_id',
 timescaledb.compress_orderby = 'collected_at DESC'
);
SELECT add_compression_policy('container_module_diagnostic_observation', INTERVAL '48 hours', schedule_interval => INTERVAL '1 hour');

-- fleet-migration: 001096_internal_release_channel_controller_software.up.sql
-- requires-shared: 1074

-- A release channel's target controller software version for one container
-- model. It is a target only: Fleet reports which containers are behind it and
-- holds their hashboard firmware updates, and never starts a controller
-- software update from it. Clearing the target deletes the row.
CREATE TABLE release_channel_controller_software (
    channel_id   BIGINT NOT NULL REFERENCES release_channel (id) ON DELETE CASCADE,
    manufacturer TEXT NOT NULL,
    model        TEXT NOT NULL,
    version      TEXT NOT NULL CHECK (version <> ''),
    assigned_by  BIGINT NOT NULL,
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (channel_id, manufacturer, model)
);

-- One target per (channel, manufacturer, model) under the same folding as
-- firmware assignments, so observed spellings of a pair share it.
CREATE UNIQUE INDEX idx_release_channel_controller_software_pair
    ON release_channel_controller_software (channel_id, release_channel_pair_key(manufacturer), release_channel_pair_key(model));

-- Controller software versions are compared without surrounding whitespace or
-- a leading "v", so "v1.4.0" from a controller matches the package's "1.4.0".
CREATE FUNCTION release_channel_controller_software_key(version text)
RETURNS text LANGUAGE sql IMMUTABLE PARALLEL SAFE AS $$
    SELECT regexp_replace(btrim(COALESCE(version, '')), '^[vV]', '');
$$;

-- fleet-migration: 001097_internal_ticketing_categories.up.sql
-- requires-shared: 1000
ALTER TABLE repair_ticket DROP CONSTRAINT ck_repair_ticket_category;
ALTER TABLE repair_ticket ADD CONSTRAINT ck_repair_ticket_category CHECK (category BETWEEN 1 AND 6);
-- Client tickets created without a component were stored as "Unspecified";
-- the component triage filter now uses "Unknown" for the same meaning.
UPDATE repair_ticket SET component = 'Unknown' WHERE component = 'Unspecified';

-- fleet-migration: 001098_internal_activation_pool_inventory.up.sql
-- requires-shared: 1074
ALTER TABLE mining_group_pool_handoff ADD COLUMN placement_pending BOOLEAN NOT NULL DEFAULT FALSE;
CREATE UNIQUE INDEX idx_mining_group_pool_handoff_pending_device
 ON mining_group_pool_handoff(device_id) WHERE placement_pending;

-- Retained container intent must not be edited or retired before every board
-- has received its assignment. The handoff clears its fence in the same transaction.
CREATE FUNCTION protect_activation_pool_handoff() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF EXISTS (SELECT 1 FROM mining_group_pool_handoff h
  WHERE h.organization_id=CASE WHEN TG_OP='INSERT' THEN NEW.organization_id ELSE OLD.organization_id END
   AND h.device_id=CASE WHEN TG_OP='INSERT' THEN NEW.device_id ELSE OLD.device_id END AND h.placement_pending) THEN
  RAISE EXCEPTION 'Pool changes are blocked until all hashboard locations are confirmed.';
 END IF;
 RETURN CASE WHEN TG_OP='DELETE' THEN OLD ELSE NEW END;
END;
$$;
CREATE TRIGGER protect_activation_pool_handoff BEFORE INSERT OR UPDATE OR DELETE ON pool_configuration_assignment
 FOR EACH ROW EXECUTE FUNCTION protect_activation_pool_handoff();

CREATE FUNCTION protect_activation_hashboard_owner() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 -- A controller move changes placement and its revision, not ownership.
 -- Identity changes still require the fence, even when account fields match.
 IF TG_OP='UPDATE' AND NEW.organization_id=OLD.organization_id
  AND NEW.kind=OLD.kind AND NEW.asset_id=OLD.asset_id
  AND NEW.hashboard_id IS NOT DISTINCT FROM OLD.hashboard_id
  AND NEW.client_id IS NOT DISTINCT FROM OLD.client_id
  AND NEW.subclient_id IS NOT DISTINCT FROM OLD.subclient_id THEN
  RETURN NEW;
 END IF;
 IF EXISTS (SELECT 1 FROM mining_group_pool_handoff h
  CROSS JOIN LATERAL jsonb_array_elements(h.captured->'Inventory') b
  WHERE h.placement_pending AND (
   (TG_OP<>'INSERT' AND OLD.kind=7 AND h.organization_id=OLD.organization_id AND (b->>'hashboard_id')::bigint=OLD.asset_id)
   OR (TG_OP<>'DELETE' AND NEW.kind=7 AND h.organization_id=NEW.organization_id AND (b->>'hashboard_id')::bigint=NEW.asset_id))) THEN
  RAISE EXCEPTION 'Hashboard ownership cannot change until the activation pool assignment transfer is complete.';
 END IF;
 RETURN CASE WHEN TG_OP='DELETE' THEN OLD ELSE NEW END;
END;
$$;
CREATE TRIGGER protect_activation_hashboard_owner BEFORE INSERT OR UPDATE OR DELETE ON hosted_account_asset
 FOR EACH ROW EXECUTE FUNCTION protect_activation_hashboard_owner();

CREATE OR REPLACE FUNCTION retire_reallocated_pool_assignments(organization bigint, controller bigint, asset_kind integer, asset bigint)
RETURNS void LANGUAGE SQL AS $$
 WITH retired AS (
  UPDATE pool_configuration_assignment a SET retired_at=CURRENT_TIMESTAMP,
   version=a.version+1,updated_at=CURRENT_TIMESTAMP
  WHERE a.organization_id=organization AND a.retired_at IS NULL
   -- A placement move must not discard retained activation intent. The board
   -- observation still advances; normal identity checks stop automatic handoff.
   AND NOT (a.target_kind='device' AND EXISTS (
    SELECT 1 FROM mining_group_pool_handoff h
    WHERE h.organization_id=a.organization_id AND h.device_id=a.device_id AND h.placement_pending))
   AND ((a.target_kind='device' AND a.device_id=controller) OR
    (a.target_kind='hashboard_installation' AND EXISTS (
     SELECT 1 FROM hashboard_installation i WHERE i.id=a.target_id
      AND ((asset_kind=6 AND i.module_id=asset) OR (asset_kind=7 AND i.hashboard_id=asset)))))
  RETURNING a.organization_id,a.target_kind,a.target_id
 )
 UPDATE pool_configuration_operation_target t SET state='superseded',updated_at=CURRENT_TIMESTAMP
 FROM retired a WHERE t.organization_id=a.organization_id AND t.target_kind=a.target_kind AND t.target_id=a.target_id
  AND t.state IN ('pending','applying','failed','different','applied_unverified');
$$;

-- fleet-migration: 001099_internal_activation_board_intent.up.sql
-- requires-shared: 1074
CREATE TABLE activation_pool_board_intent (
 organization_id bigint NOT NULL,
 device_id bigint NOT NULL,
 hashboard_id bigint NOT NULL REFERENCES hashboard(id),
 operation_id uuid NOT NULL,
 snapshot_id varchar(36) NOT NULL,
 mac_address text NOT NULL,
 serial text NOT NULL,
 account_id bigint NOT NULL,
 ownership_revision bigint NOT NULL,
 PRIMARY KEY (organization_id,hashboard_id),
 FOREIGN KEY (organization_id,operation_id) REFERENCES mining_group_pool_handoff(organization_id,operation_id),
 FOREIGN KEY (device_id,organization_id) REFERENCES device(id,org_id),
 FOREIGN KEY (organization_id,snapshot_id) REFERENCES pool_configuration_group_snapshot(organization_id,snapshot_id)
);
CREATE INDEX idx_activation_pool_board_intent_device ON activation_pool_board_intent(device_id);

CREATE OR REPLACE FUNCTION protect_activation_pool_handoff() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE org bigint; controller bigint; kind text; target bigint;
BEGIN
 org := CASE WHEN TG_OP='INSERT' THEN NEW.organization_id ELSE OLD.organization_id END;
 controller := CASE WHEN TG_OP='INSERT' THEN NEW.device_id ELSE OLD.device_id END;
 kind := CASE WHEN TG_OP='INSERT' THEN NEW.target_kind ELSE OLD.target_kind END;
 target := CASE WHEN TG_OP='INSERT' THEN NEW.target_id ELSE OLD.target_id END;
 IF EXISTS (SELECT 1 FROM mining_group_pool_handoff h WHERE h.organization_id=org AND h.device_id=controller
   AND h.placement_pending AND (kind='device' OR NOT COALESCE((h.captured->>'PartialTransfer')::boolean,false)))
 OR EXISTS (SELECT 1 FROM activation_pool_board_intent p JOIN hashboard_installation i ON i.hashboard_id=p.hashboard_id
   WHERE p.organization_id=org AND kind='hashboard_installation' AND i.id=target) THEN
  RAISE EXCEPTION 'Pool assignment is waiting for hashboard placement recovery.';
 END IF;
 RETURN CASE WHEN TG_OP='DELETE' THEN OLD ELSE NEW END;
END;
$$;

-- Partial transfers no longer reserve ownership for the entire inventory.
-- Recovery checks the pending board's saved owner and allocation generation.
CREATE OR REPLACE FUNCTION protect_activation_hashboard_owner() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='UPDATE' AND NEW.organization_id=OLD.organization_id
  AND NEW.kind=OLD.kind AND NEW.asset_id=OLD.asset_id
  AND NEW.hashboard_id IS NOT DISTINCT FROM OLD.hashboard_id
  AND NEW.client_id IS NOT DISTINCT FROM OLD.client_id
  AND NEW.subclient_id IS NOT DISTINCT FROM OLD.subclient_id THEN RETURN NEW; END IF;
 IF EXISTS (SELECT 1 FROM mining_group_pool_handoff h
  CROSS JOIN LATERAL jsonb_array_elements(h.captured->'Inventory') b
  WHERE h.placement_pending AND NOT COALESCE((h.captured->>'PartialTransfer')::boolean,false) AND (
   (TG_OP<>'INSERT' AND OLD.kind=7 AND h.organization_id=OLD.organization_id AND (b->>'hashboard_id')::bigint=OLD.asset_id)
   OR (TG_OP<>'DELETE' AND NEW.kind=7 AND h.organization_id=NEW.organization_id AND (b->>'hashboard_id')::bigint=NEW.asset_id))) THEN
  RAISE EXCEPTION 'Hashboard ownership cannot change until the activation pool assignment transfer is complete.';
 END IF;
 RETURN CASE WHEN TG_OP='DELETE' THEN OLD ELSE NEW END;
END;
$$;

-- fleet-migration: 001100_internal_activation_review_guards.up.sql
-- requires-shared: 1074
CREATE OR REPLACE FUNCTION ct1_hashboard_allocation_ready(target_org bigint, controller bigint)
RETURNS boolean LANGUAGE sql VOLATILE AS $$
 SELECT EXISTS (
  SELECT 1 FROM container_mining_group_state s
  JOIN device_write_fence f ON f.device_id=s.device_id AND f.organization_id=s.organization_id
  WHERE s.organization_id=target_org AND s.device_id=controller
   AND NOT EXISTS (SELECT 1 FROM mining_group_pool_handoff h WHERE h.organization_id=target_org AND h.device_id=controller
    AND h.placement_pending AND NOT COALESCE((h.captured->>'PartialTransfer')::boolean,false))
   AND f.state='host' AND s.status NOT IN ('pending','applying','blocked')
   AND s.observed_at>=f.updated_at
   AND s.observed_at>clock_timestamp()-interval '2 minutes' AND s.observed_at<=clock_timestamp()+interval '5 seconds'
   AND COALESCE((s.observed->>'revision')::bigint,0)>0
   AND s.observed @> '{"supported":true,"enabled":true,"suspended":false,"pending":false}'::jsonb
   AND NOT EXISTS (SELECT 1 FROM device_write_attempt a WHERE a.device_id=controller
    AND a.organization_id=target_org AND a.operation_kind='mutation' AND a.status IN ('running','unknown'))
 );
$$;

CREATE OR REPLACE FUNCTION protect_activation_hashboard_owner() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='UPDATE' AND NEW.organization_id=OLD.organization_id
  AND NEW.kind=OLD.kind AND NEW.asset_id=OLD.asset_id
  AND NEW.hashboard_id IS NOT DISTINCT FROM OLD.hashboard_id
  AND NEW.client_id IS NOT DISTINCT FROM OLD.client_id
  AND NEW.subclient_id IS NOT DISTINCT FROM OLD.subclient_id THEN RETURN NEW; END IF;
 IF EXISTS (SELECT 1 FROM mining_group_pool_handoff h
  WHERE h.placement_pending AND NOT COALESCE((h.captured->>'PartialTransfer')::boolean,false)
   AND ((TG_OP='INSERT' AND NEW.client_id IS NOT NULL)
    OR (TG_OP='DELETE' AND OLD.client_id IS NOT NULL)
    OR (TG_OP='UPDATE' AND (NEW.client_id IS DISTINCT FROM OLD.client_id OR NEW.subclient_id IS DISTINCT FROM OLD.subclient_id))) AND (
   (TG_OP<>'INSERT' AND OLD.kind=7 AND h.organization_id=OLD.organization_id AND
    (h.device_id=OLD.device_id OR EXISTS (SELECT 1 FROM hashboard_installation i WHERE i.hashboard_id=OLD.asset_id AND i.device_id=h.device_id AND i.removed_at IS NULL)))
   OR (TG_OP<>'DELETE' AND NEW.kind=7 AND h.organization_id=NEW.organization_id AND
    (h.device_id=NEW.device_id OR EXISTS (SELECT 1 FROM hashboard_installation i WHERE i.hashboard_id=NEW.asset_id AND i.device_id=h.device_id AND i.removed_at IS NULL))))) THEN
  RAISE EXCEPTION 'Hashboard ownership cannot change until the activation pool assignment transfer is complete.';
 END IF;
 IF EXISTS (SELECT 1 FROM mining_group_pool_handoff h
  CROSS JOIN LATERAL jsonb_array_elements(h.captured->'Inventory') b
  WHERE h.placement_pending AND NOT COALESCE((h.captured->>'PartialTransfer')::boolean,false) AND (
   (TG_OP<>'INSERT' AND OLD.kind=7 AND h.organization_id=OLD.organization_id AND (b->>'hashboard_id')::bigint=OLD.asset_id)
   OR (TG_OP<>'DELETE' AND NEW.kind=7 AND h.organization_id=NEW.organization_id AND (b->>'hashboard_id')::bigint=NEW.asset_id))) THEN
  RAISE EXCEPTION 'Hashboard ownership cannot change until the activation pool assignment transfer is complete.';
 END IF;
 RETURN CASE WHEN TG_OP='DELETE' THEN OLD ELSE NEW END;
END;
$$;

-- fleet-migration: 001101_internal_node_connectivity_monitoring.up.sql
-- requires-shared: 1000

CREATE TABLE fleet_node_monitoring_exclusion (
 fleet_node_id bigint CONSTRAINT pk_fleet_node_monitoring_exclusion PRIMARY KEY REFERENCES fleet_node(id) ON DELETE CASCADE,
 reason text NOT NULL CONSTRAINT ck_fleet_node_monitoring_exclusion_reason CHECK (length(trim(reason)) BETWEEN 1 AND 256),
 created_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
 expires_at timestamptz NOT NULL,
 CONSTRAINT ck_fleet_node_monitoring_exclusion_expiry CHECK (
     expires_at > created_at AND expires_at <= created_at + INTERVAL '7 days'
 )
);

-- Authoritative heartbeat state is in fleet_node_live, not the legacy snapshot
-- fleet_node.last_seen_at. Nodes never shrink coverage merely by disconnecting.
CREATE VIEW fleet_node_connectivity_summary AS
WITH inventory AS (
 SELECT n.id,n.org_id,n.last_seen_at,
 COALESCE((SELECT max(p.consumed_at) FROM pending_enrollment p
           WHERE p.fleet_node_id=n.id AND p.org_id=n.org_id AND p.status='CONFIRMED'),n.created_at) AS admitted_at,
 COALESCE(e.created_at <= CURRENT_TIMESTAMP AND e.expires_at > CURRENT_TIMESTAMP,false) AS excluded
 FROM fleet_node_live n LEFT JOIN fleet_node_monitoring_exclusion e ON e.fleet_node_id=n.id
 WHERE n.enrollment_status='CONFIRMED' AND n.deleted_at IS NULL
), classified AS (
 SELECT *,NOT excluded AND admitted_at <= CURRENT_TIMESTAMP-INTERVAL '5 minutes' AS expected,
 last_seen_at IS NOT NULL AND last_seen_at >= admitted_at AND last_seen_at <= CURRENT_TIMESTAMP + INTERVAL '5 seconds' AS known
 FROM inventory
)
SELECT o.id AS org_id,
 count(c.id) FILTER (WHERE c.expected)::bigint AS expected_count,
 count(c.id) FILTER (WHERE c.expected AND c.known AND c.last_seen_at >= CURRENT_TIMESTAMP-INTERVAL '2 minutes')::bigint AS recent_count,
 count(c.id) FILTER (WHERE c.expected AND c.known AND c.last_seen_at < CURRENT_TIMESTAMP-INTERVAL '2 minutes')::bigint AS overdue_count,
 count(c.id) FILTER (WHERE c.expected AND NOT c.known)::bigint AS unknown_count,
 count(c.id) FILTER (WHERE c.excluded)::bigint AS excluded_count,
 count(c.id) FILTER (WHERE NOT c.excluded AND NOT c.expected)::bigint AS grace_count,
 GREATEST(max(EXTRACT(EPOCH FROM CURRENT_TIMESTAMP-c.last_seen_at)) FILTER (WHERE c.expected AND c.known),0)::double precision AS oldest_heartbeat_age_seconds
FROM organization o LEFT JOIN classified c ON c.org_id=o.id
WHERE o.deleted_at IS NULL
GROUP BY o.id;

-- HA provisions the monitoring role before Fleet migrations run.
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'grafana_ha_ro') THEN
        GRANT SELECT ON fleet_node_connectivity_summary TO grafana_ha_ro;
    END IF;
END $$;

-- fleet-migration: 001106_internal_notification_metric_sample_retention.up.sql
-- requires-shared: 1000
-- Update the existing retention job to preserve its schedule.
DO $$
DECLARE
    retention_job_id integer;
    retention_config jsonb;
BEGIN
    SELECT job_id, config INTO STRICT retention_job_id, retention_config
    FROM timescaledb_information.jobs
    WHERE hypertable_schema = 'public'
      AND hypertable_name = 'notification_metric_sample'
      AND proc_name = 'policy_retention';

    PERFORM alter_job(retention_job_id,
        config => retention_config || jsonb_build_object('drop_after', INTERVAL '3 days'));
END;
$$;

-- fleet-migration: 001107_internal_shorten_diagnostic_retention.up.sql
-- requires-shared: 1000
-- Update the existing retention jobs in place to keep their schedules.
DO $$
DECLARE
    diagnostic_table text;
    retention_job_id integer;
    retention_config jsonb;
BEGIN
    FOREACH diagnostic_table IN ARRAY ARRAY['hashboard_diagnostic_observation', 'container_module_diagnostic_observation']
    LOOP
        SELECT job_id, config INTO STRICT retention_job_id, retention_config
        FROM timescaledb_information.jobs
        WHERE hypertable_schema = 'public'
          AND hypertable_name = diagnostic_table
          AND proc_name = 'policy_retention';

        PERFORM alter_job(retention_job_id,
            config => retention_config || jsonb_build_object('drop_after', INTERVAL '14 days'));
    END LOOP;
END;
$$;

-- fleet-migration: 001108_internal_shorten_minute_uptime_retention.up.sql
-- requires-shared: 1000
-- Keep the existing job IDs, schedules, and operator settings.
DO $$
DECLARE
    updated_jobs integer;
BEGIN
    PERFORM alter_job(job_id, config => config || jsonb_build_object(
        CASE proc_name
            WHEN 'policy_refresh_continuous_aggregate' THEN 'start_offset'
            ELSE 'drop_after'
        END, INTERVAL '7 days'))
    FROM timescaledb_information.jobs
    WHERE hypertable_schema = 'public'
      AND hypertable_name = 'miner_state_snapshot_device_1m'
      AND proc_name IN ('policy_refresh_continuous_aggregate', 'policy_retention');

    GET DIAGNOSTICS updated_jobs = ROW_COUNT;
    IF updated_jobs <> 2 THEN
        RAISE EXCEPTION 'expected one refresh and one retention job for miner_state_snapshot_device_1m';
    END IF;
END;
$$;

-- fleet-migration: 001109_internal_device_status_rollup_retention.up.sql
-- requires-shared: 1000
-- Match the corresponding device metrics retention windows. Summary expiration
-- is independent of the telemetry recovery worker and raw data fence.
SELECT add_retention_policy('device_status_hourly', INTERVAL '3 months');
SELECT add_retention_policy('device_status_daily', INTERVAL '3 years');

-- fleet-migration: 001110_internal_ct1_psu_firmware_observation.up.sql
-- requires-shared: 1074

-- A container's PSU firmware, observed like its hashboard firmware (001073):
-- the version every located board reports for its module's PSU in one
-- complete, fresh inventory, or that those versions differ. Neither set means
-- the PSU firmware version is unknown.
ALTER TABLE container_inventory
    ADD COLUMN psu_firmware_version text NOT NULL DEFAULT '',
    ADD COLUMN psu_firmware_mixed boolean NOT NULL DEFAULT false;

-- fleet-migration: 001111_internal_release_channel_firmware_component.up.sql
-- requires-shared: 1074

-- Release channels assign firmware per component of a model. The empty
-- component is the model's main firmware, which for a CT1 container is its
-- hashboard firmware; 'psu' is its PSU firmware. Assignments, their
-- generations, rollouts and managed-deployment provenance are kept per
-- component, so two components of one model are assigned, rolled out and
-- verified independently.

ALTER TABLE release_channel_firmware
    ADD COLUMN component TEXT NOT NULL DEFAULT '',
    DROP CONSTRAINT release_channel_firmware_pkey,
    ADD PRIMARY KEY (channel_id, manufacturer, model, component);

DROP INDEX idx_release_channel_firmware_pair;
CREATE UNIQUE INDEX idx_release_channel_firmware_pair
    ON release_channel_firmware(channel_id, release_channel_pair_key(manufacturer), release_channel_pair_key(model), component);

ALTER TABLE firmware_rollout
    ADD COLUMN component TEXT NOT NULL DEFAULT '';

-- At most one active rollout per (channel, manufacturer, model, component).
DROP INDEX idx_one_active_rollout_per_pair;
CREATE UNIQUE INDEX idx_one_active_rollout_per_pair
    ON firmware_rollout(channel_id, release_channel_pair_key(manufacturer), release_channel_pair_key(model), component)
    WHERE status = 'active';

DROP INDEX idx_firmware_rollout_assignment_history;
CREATE INDEX idx_firmware_rollout_assignment_history
    ON firmware_rollout(channel_id, release_channel_pair_key(manufacturer),
        release_channel_pair_key(model), component, assignment_generation, id DESC);

-- One provenance row per device and component. A finished firmware command
-- serializes through, and invalidates, only its own component's row.
ALTER TABLE device_firmware_deployment
    ADD COLUMN component TEXT NOT NULL DEFAULT '',
    DROP CONSTRAINT device_firmware_deployment_pkey,
    ADD PRIMARY KEY (device_id, component);

-- A view keeps its column order, so component is appended.
CREATE OR REPLACE VIEW firmware_rollout_suppressed_device AS
SELECT channel_id, manufacturer_key, model_key, assignment_generation, device_id, component
FROM (
    SELECT r.channel_id,
           release_channel_pair_key(r.manufacturer) AS manufacturer_key,
           release_channel_pair_key(r.model) AS model_key,
           r.assignment_generation,
           rd.device_id,
           r.component,
           rd.halted_at,
           row_number() OVER (
               PARTITION BY r.channel_id, release_channel_pair_key(r.manufacturer), release_channel_pair_key(r.model),
                            r.component, r.assignment_generation, rd.device_id
               ORDER BY r.id DESC
           ) AS recency
    FROM firmware_rollout_device rd
    JOIN firmware_rollout r ON r.id = rd.rollout_id
) latest
WHERE recency = 1 AND halted_at IS NOT NULL;

-- The version a device reports for one component. A container reports its
-- hashboard firmware for the main component and its PSU firmware for 'psu',
-- each from fresh inventory: NULL when unknown, '' when versions differ. Any
-- other device reports its controller version for the main component and
-- nothing for another.
DROP FUNCTION release_channel_firmware_version(bigint, integer, text);
CREATE FUNCTION release_channel_firmware_version(device_id bigint, device_class integer, controller_version text, component text)
RETURNS SETOF text LANGUAGE sql STABLE PARALLEL SAFE ROWS 1 AS $$
    SELECT CASE
        WHEN component = 'psu' THEN (
            SELECT CASE WHEN i.psu_firmware_mixed THEN '' ELSE NULLIF(i.psu_firmware_version, '') END
            FROM container_inventory i
            WHERE device_class = 2 AND i.device_id = $1
              AND i.observed_at > statement_timestamp() - INTERVAL '2 minutes'
              AND i.observed_at <= statement_timestamp() + INTERVAL '5 seconds'
        )
        WHEN component <> '' THEN NULL
        WHEN device_class = 2 THEN (
            SELECT CASE WHEN i.hashboard_firmware_mixed THEN '' ELSE NULLIF(i.hashboard_firmware_version, '') END
            FROM container_inventory i
            WHERE i.device_id = $1 AND i.observed_at > statement_timestamp() - INTERVAL '2 minutes'
              AND i.observed_at <= statement_timestamp() + INTERVAL '5 seconds'
        )
        ELSE COALESCE(controller_version, '')
    END;
$$;

-- fleet-migration: 001112_internal_miner_reachability_monitoring.up.sql
-- requires-shared: 1000
-- Only the telemetry status writer records evidence for the current status.
-- Leave existing rows unknown until telemetry observes their status again.
ALTER TABLE device_status ADD COLUMN telemetry_status_at timestamptz;

CREATE TABLE device_reachability_exclusion (
 device_id bigint CONSTRAINT pk_device_reachability_exclusion PRIMARY KEY REFERENCES device(id) ON DELETE CASCADE,
 reason text NOT NULL CONSTRAINT ck_device_reachability_exclusion_reason CHECK (length(trim(reason)) BETWEEN 1 AND 256),
 created_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
 expires_at timestamptz NOT NULL,
 CONSTRAINT ck_device_reachability_exclusion_expiry CHECK (expires_at > created_at)
);

CREATE VIEW device_reachability_summary AS
WITH inventory AS (
 SELECT d.org_id,COALESCE(d.site_id,0)::bigint AS site_id,
 COALESCE(fnd.fleet_node_id,0)::bigint AS source_node_id,dd.device_class,
 GREATEST(COALESCE(dp.paired_at,dp.created_at,d.created_at),COALESCE(fnd.assigned_at,'epoch'::timestamptz)) AS admitted_at,
 ds.status,ds.status_timestamp,ds.telemetry_status_at,
 COALESCE(ex.expires_at>CURRENT_TIMESTAMP,false) AS excluded
 FROM device d JOIN device_pairing dp ON dp.device_id=d.id
 JOIN discovered_device dd ON dd.id=d.discovered_device_id
 LEFT JOIN fleet_node_device fnd ON fnd.device_id=d.id AND fnd.org_id=d.org_id
 LEFT JOIN device_status ds ON ds.device_id=d.id
 LEFT JOIN device_reachability_exclusion ex ON ex.device_id=d.id
 WHERE d.deleted_at IS NULL AND dp.pairing_status IN ('PAIRED','DEFAULT_PASSWORD','AUTHENTICATION_NEEDED')
), classified AS (
 SELECT *,NOT excluded AND admitted_at <= CURRENT_TIMESTAMP-INTERVAL '5 minutes' AS expected,
 COALESCE(telemetry_status_at = status_timestamp
 AND telemetry_status_at >= GREATEST(admitted_at,CURRENT_TIMESTAMP-INTERVAL '2 minutes')
 AND telemetry_status_at <= CURRENT_TIMESTAMP
 AND status IN ('ACTIVE','INACTIVE','MAINTENANCE','ERROR','NEEDS_MINING_POOL','OFFLINE'),false) AS known
 FROM inventory
)
SELECT org_id,site_id,source_node_id,device_class,
 count(*) FILTER (WHERE expected)::bigint AS expected_count,
 count(*) FILTER (WHERE expected AND known AND status <> 'OFFLINE')::bigint AS reachable_count,
 count(*) FILTER (WHERE expected AND known AND status = 'OFFLINE')::bigint AS unreachable_count,
 count(*) FILTER (WHERE expected AND NOT known)::bigint AS unknown_count,
 count(*) FILTER (WHERE excluded)::bigint AS excluded_count,
 count(*) FILTER (WHERE NOT excluded AND NOT expected)::bigint AS grace_count
FROM classified GROUP BY org_id,site_id,source_node_id,device_class;

-- HA provisions the monitoring role before Fleet migrations run.
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'grafana_ha_ro') THEN
        GRANT SELECT ON device_reachability_summary TO grafana_ha_ro;
    END IF;
END $$;

-- fleet-migration: 001113_internal_command_lifecycle_monitoring.up.sql
-- requires-shared: 1000
ALTER TABLE queue_message ADD COLUMN processing_started_at timestamptz;

CREATE INDEX idx_queue_message_monitoring_pending ON queue_message(created_at,id)
 WHERE status IN ('PENDING','PROCESSING');

-- Queued and outstanding age use original admission. Running age uses the
-- start of the current attempt. Rows already running at migration have no start.
CREATE VIEW command_backlog_summary AS
WITH backlog AS (
 SELECT d.org_id,
 CASE WHEN q.command_type='FirmwareUpdate' THEN 'firmware' ELSE 'control' END AS command_class,
 q.status,q.created_at,q.updated_at,q.processing_started_at
 FROM queue_message q JOIN device d ON d.id=q.device_id
 WHERE q.status IN ('PENDING','PROCESSING')
), scopes AS (
 SELECT id AS org_id,'control'::text AS command_class FROM organization
 UNION ALL SELECT id,'firmware'::text FROM organization
)
SELECT s.org_id,s.command_class,
 count(b.status) FILTER (WHERE b.status='PENDING')::bigint AS queued_count,
 count(b.status) FILTER (WHERE b.status='PROCESSING')::bigint AS running_count,
 count(b.status)::bigint AS outstanding_count,
 COALESCE(max(GREATEST(0,EXTRACT(EPOCH FROM CURRENT_TIMESTAMP-b.created_at))) FILTER (WHERE b.status='PENDING'),0)::double precision AS oldest_queued_age_seconds,
 COALESCE(max(GREATEST(0,EXTRACT(EPOCH FROM CURRENT_TIMESTAMP-b.processing_started_at))) FILTER (WHERE b.status='PROCESSING' AND b.processing_started_at IS NOT NULL),0)::double precision AS oldest_running_age_seconds,
 COALESCE(max(GREATEST(0,EXTRACT(EPOCH FROM CURRENT_TIMESTAMP-b.created_at))),0)::double precision AS oldest_outstanding_age_seconds,
 count(b.status) FILTER (WHERE b.created_at>CURRENT_TIMESTAMP OR b.updated_at>CURRENT_TIMESTAMP
     OR (b.status='PROCESSING' AND b.processing_started_at>CURRENT_TIMESTAMP))::bigint AS invalid_clock_count
FROM scopes s LEFT JOIN backlog b ON b.org_id=s.org_id AND b.command_class=s.command_class
GROUP BY s.org_id,s.command_class;

-- HA provisions the monitoring role before Fleet migrations run.
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'grafana_ha_ro') THEN
        GRANT SELECT ON command_backlog_summary TO grafana_ha_ro;
    END IF;
END $$;

-- fleet-migration: 001114_internal_hashboard_repair_obligations.up.sql
-- requires-shared: 1000
-- The client of each hashboard in a module when a module ticket opened. The
-- module target has no client when its hashboards have different clients, so a
-- hashboard that an operator selects later takes its client from this record.
-- The rows are written under an ownership lock in the ticket opening.
CREATE TABLE repair_ticket_board_owner (
 org_id BIGINT NOT NULL,
 ticket_id BIGINT NOT NULL,
 hashboard_id BIGINT NOT NULL REFERENCES hashboard(id),
 client_id BIGINT NOT NULL,
 subclient_id BIGINT,
 PRIMARY KEY (org_id, ticket_id, hashboard_id),
 FOREIGN KEY (ticket_id, org_id) REFERENCES repair_ticket(id, org_id),
 FOREIGN KEY (org_id, client_id) REFERENCES hosted_account(organization_id, id),
 FOREIGN KEY (org_id, subclient_id) REFERENCES hosted_account(organization_id, id)
);

-- A repair obligation is the period in which a host must restore a client's
-- hashboard. A ticket joins the open obligation of each hashboard it names. A
-- module ticket names no hashboard until an operator selects the affected one.
-- Closing a ticket does not end the obligation; only evidenced restoration
-- sets restored_at. The obligation stores no start, subclient or placement:
-- read them from the hashboard target of the linked ticket that opened first.
CREATE TABLE hashboard_repair_obligation (
 id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 org_id BIGINT NOT NULL,
 hashboard_id BIGINT NOT NULL REFERENCES hashboard(id),
 client_id BIGINT NOT NULL,
 restored_at TIMESTAMPTZ,
 created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
 CONSTRAINT uq_hashboard_repair_obligation_id_org UNIQUE (id, org_id),
 FOREIGN KEY (org_id, client_id) REFERENCES hosted_account(organization_id, id)
);
-- Concurrent tickets for one hashboard converge on one open obligation per client.
CREATE UNIQUE INDEX idx_hashboard_repair_obligation_open ON hashboard_repair_obligation(org_id, hashboard_id, client_id) WHERE restored_at IS NULL;

-- linked_at is later than the ticket opening when an operator selects the
-- hashboard on a module ticket.
CREATE TABLE hashboard_repair_obligation_ticket (
 org_id BIGINT NOT NULL,
 obligation_id BIGINT NOT NULL,
 ticket_id BIGINT NOT NULL,
 linked_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
 PRIMARY KEY (org_id, obligation_id, ticket_id),
 FOREIGN KEY (obligation_id, org_id) REFERENCES hashboard_repair_obligation(id, org_id),
 FOREIGN KEY (ticket_id, org_id) REFERENCES repair_ticket(id, org_id)
);
CREATE INDEX idx_hashboard_repair_obligation_ticket_ticket ON hashboard_repair_obligation_ticket(org_id, ticket_id);
