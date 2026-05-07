DROP TABLE IF EXISTS curtailment_reconciler_heartbeat;

DROP INDEX IF EXISTS idx_curtailment_target_active_by_device;
DROP INDEX IF EXISTS idx_curtailment_target_pending_work;
DROP TABLE IF EXISTS curtailment_target;

DROP TRIGGER IF EXISTS update_curtailment_event_updated_at ON curtailment_event;
DROP INDEX IF EXISTS idx_curtailment_event_org_created;
DROP INDEX IF EXISTS idx_curtailment_event_active;
DROP INDEX IF EXISTS uq_curtailment_event_idempotency;
DROP INDEX IF EXISTS uq_curtailment_event_external_ref;
DROP TABLE IF EXISTS curtailment_event;

DROP TRIGGER IF EXISTS update_curtailment_org_config_updated_at ON curtailment_org_config;
DROP TABLE IF EXISTS curtailment_org_config;
