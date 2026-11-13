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
