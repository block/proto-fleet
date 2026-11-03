-- requires-shared: 1000
-- A schema rollback cannot discard captured evidence. Stop writers first.
LOCK TABLE device_metrics,miner_state_snapshots,container_module_metrics,
 container_module_diagnostic_observation,hashboard_metrics,hashboard_diagnostic_observation,
 hosted_account_history_sample
 IN SHARE ROW EXCLUSIVE MODE;
LOCK TABLE hosted_account_reporting_evidence,hosted_account_reporting_allocation_snapshot
 IN ACCESS EXCLUSIVE MODE;
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM hosted_account_reporting_evidence)
  OR EXISTS(SELECT 1 FROM hosted_account_reporting_allocation_snapshot) THEN
  RAISE EXCEPTION 'preserve retained hosted reporting evidence; rollback requires an empty hold';
 END IF;
END $$;
DROP VIEW hosted_account_reporting_capture_status;
DROP TRIGGER record_hosted_reporting_evidence ON hosted_account_history_sample;
DROP FUNCTION record_hosted_reporting_evidence();
DROP TABLE hosted_account_reporting_evidence;
DROP TABLE hosted_account_reporting_allocation_snapshot;
DROP TABLE hosted_account_reporting_evidence_epoch;
DROP FUNCTION reject_hosted_reporting_evidence_mutation();
DROP TRIGGER record_hosted_hashboard_history ON hashboard_metrics;
CREATE TRIGGER record_hosted_hashboard_history AFTER INSERT OR UPDATE OR DELETE ON hashboard_metrics
 FOR EACH ROW EXECUTE FUNCTION record_hosted_hashboard_history_sample('metrics');
