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
