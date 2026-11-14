LOCK TABLE hosted_report_artifact,hosted_report_artifact_chunk IN ACCESS EXCLUSIVE MODE;
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM hosted_report_artifact) THEN
  RAISE EXCEPTION 'preserve frozen report artifact bytes; restore a coordinated backup or fix forward';
 END IF;
END $$;
DROP TABLE hosted_report_artifact_chunk;
DROP TABLE hosted_report_artifact;
DROP FUNCTION guard_hosted_report_artifact();
