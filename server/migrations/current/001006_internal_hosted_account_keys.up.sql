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
    UNIQUE (organization_id, api_key_id),
    FOREIGN KEY (organization_id, api_key_id) REFERENCES api_key(organization_id, id) ON DELETE CASCADE,
    FOREIGN KEY (organization_id, account_id) REFERENCES hosted_account(organization_id, id) ON DELETE CASCADE
);
CREATE INDEX hosted_account_key_account ON hosted_account_key (organization_id, account_id);
CREATE TABLE hosted_account_key_descendant (
    organization_id BIGINT NOT NULL,
    api_key_id BIGINT NOT NULL,
    account_id BIGINT NOT NULL,
    PRIMARY KEY (organization_id, api_key_id, account_id),
    FOREIGN KEY (organization_id, api_key_id) REFERENCES hosted_account_key(organization_id, api_key_id) ON DELETE CASCADE,
    FOREIGN KEY (organization_id, account_id) REFERENCES hosted_account(organization_id, id) ON DELETE CASCADE
);
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
