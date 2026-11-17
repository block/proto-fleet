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
