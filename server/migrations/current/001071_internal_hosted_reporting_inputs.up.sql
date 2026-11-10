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
