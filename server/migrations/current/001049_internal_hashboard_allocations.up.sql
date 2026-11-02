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
