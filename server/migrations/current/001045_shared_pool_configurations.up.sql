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
