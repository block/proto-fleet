-- Customer pool destinations are usable immediately, without host allowlists.
CREATE TABLE hosted_account_pool (
    pool_id VARCHAR(36) PRIMARY KEY,
    organization_id BIGINT NOT NULL,
    account_id BIGINT NOT NULL,
    archived BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (organization_id, account_id, pool_id),
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
    UNIQUE (pool_id, number),
    UNIQUE (organization_id, account_id, revision_id),
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
