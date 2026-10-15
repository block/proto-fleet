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
