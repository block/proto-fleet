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
