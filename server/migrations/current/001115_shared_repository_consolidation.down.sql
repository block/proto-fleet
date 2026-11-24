-- Restore the coordinated application and database backup to roll back.
-- The numbered historical down scripts below are retained as fixtures.
DO $$ BEGIN RAISE EXCEPTION 'Repository consolidation cannot be reversed'; END $$;
/*

-- fleet-migration: 001114_internal_hashboard_repair_obligations.down.sql
LOCK TABLE repair_ticket_board_owner,hashboard_repair_obligation,hashboard_repair_obligation_ticket IN ACCESS EXCLUSIVE MODE;
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM hashboard_repair_obligation) OR EXISTS(SELECT 1 FROM repair_ticket_board_owner) THEN
  RAISE EXCEPTION 'hashboard repair obligations contain retained history; preserve it and fix forward';
 END IF;
END $$;
DROP TABLE hashboard_repair_obligation_ticket;
DROP TABLE hashboard_repair_obligation;
DROP TABLE repair_ticket_board_owner;

-- fleet-migration: 001113_internal_command_lifecycle_monitoring.down.sql
DROP VIEW command_backlog_summary;
DROP INDEX idx_queue_message_monitoring_pending;
ALTER TABLE queue_message DROP COLUMN processing_started_at;

-- fleet-migration: 001112_internal_miner_reachability_monitoring.down.sql
DROP VIEW device_reachability_summary;
DROP TABLE device_reachability_exclusion;
ALTER TABLE device_status DROP COLUMN telemetry_status_at;

-- fleet-migration: 001111_internal_release_channel_firmware_component.down.sql
DROP FUNCTION release_channel_firmware_version(bigint, integer, text, text);
CREATE FUNCTION release_channel_firmware_version(device_id bigint, device_class integer, controller_version text)
RETURNS SETOF text LANGUAGE sql STABLE PARALLEL SAFE ROWS 1 AS $$
    SELECT CASE WHEN device_class = 2 THEN (
        SELECT CASE WHEN i.hashboard_firmware_mixed THEN '' ELSE NULLIF(i.hashboard_firmware_version, '') END
        FROM container_inventory i
        WHERE i.device_id = $1 AND i.observed_at > statement_timestamp() - INTERVAL '2 minutes'
          AND i.observed_at <= statement_timestamp() + INTERVAL '5 seconds'
    ) ELSE COALESCE(controller_version, '') END;
$$;

-- Only the main component existed before; other components' rows have no
-- place in the earlier schema.
DROP VIEW firmware_rollout_suppressed_device;

DELETE FROM device_firmware_deployment WHERE component <> '';
ALTER TABLE device_firmware_deployment
    DROP CONSTRAINT device_firmware_deployment_pkey,
    DROP COLUMN component,
    ADD PRIMARY KEY (device_id);

DROP INDEX idx_firmware_rollout_assignment_history;
DROP INDEX idx_one_active_rollout_per_pair;
DELETE FROM firmware_rollout WHERE component <> '';
ALTER TABLE firmware_rollout DROP COLUMN component;
CREATE UNIQUE INDEX idx_one_active_rollout_per_pair
    ON firmware_rollout(channel_id, release_channel_pair_key(manufacturer), release_channel_pair_key(model))
    WHERE status = 'active';
CREATE INDEX idx_firmware_rollout_assignment_history
    ON firmware_rollout(channel_id, release_channel_pair_key(manufacturer),
        release_channel_pair_key(model), assignment_generation, id DESC);

DROP INDEX idx_release_channel_firmware_pair;
DELETE FROM release_channel_firmware WHERE component <> '';
ALTER TABLE release_channel_firmware
    DROP CONSTRAINT release_channel_firmware_pkey,
    DROP COLUMN component,
    ADD PRIMARY KEY (channel_id, manufacturer, model);
CREATE UNIQUE INDEX idx_release_channel_firmware_pair
    ON release_channel_firmware(channel_id, release_channel_pair_key(manufacturer), release_channel_pair_key(model));

CREATE VIEW firmware_rollout_suppressed_device AS
SELECT channel_id, manufacturer_key, model_key, assignment_generation, device_id
FROM (
    SELECT r.channel_id,
           release_channel_pair_key(r.manufacturer) AS manufacturer_key,
           release_channel_pair_key(r.model) AS model_key,
           r.assignment_generation,
           rd.device_id,
           rd.halted_at,
           row_number() OVER (
               PARTITION BY r.channel_id, release_channel_pair_key(r.manufacturer), release_channel_pair_key(r.model),
                            r.assignment_generation, rd.device_id
               ORDER BY r.id DESC
           ) AS recency
    FROM firmware_rollout_device rd
    JOIN firmware_rollout r ON r.id = rd.rollout_id
) latest
WHERE recency = 1 AND halted_at IS NOT NULL;

-- fleet-migration: 001110_internal_ct1_psu_firmware_observation.down.sql
ALTER TABLE container_inventory
    DROP COLUMN psu_firmware_mixed,
    DROP COLUMN psu_firmware_version;

-- fleet-migration: 001109_internal_device_status_rollup_retention.down.sql
-- Remove the policies. Data removed by earlier policy runs cannot be restored.
SELECT remove_retention_policy('device_status_daily', if_exists => true);
SELECT remove_retention_policy('device_status_hourly', if_exists => true);

-- fleet-migration: 001108_internal_shorten_minute_uptime_retention.down.sql
-- Keep the existing job IDs, schedules, and operator settings.
DO $$
DECLARE
    updated_jobs integer;
BEGIN
    PERFORM alter_job(job_id, config => config || jsonb_build_object(
        CASE proc_name
            WHEN 'policy_refresh_continuous_aggregate' THEN 'start_offset'
            ELSE 'drop_after'
        END, INTERVAL '14 days'))
    FROM timescaledb_information.jobs
    WHERE hypertable_schema = 'public'
      AND hypertable_name = 'miner_state_snapshot_device_1m'
      AND proc_name IN ('policy_refresh_continuous_aggregate', 'policy_retention');

    GET DIAGNOSTICS updated_jobs = ROW_COUNT;
    IF updated_jobs <> 2 THEN
        RAISE EXCEPTION 'expected one refresh and one retention job for miner_state_snapshot_device_1m';
    END IF;
END;
$$;

-- fleet-migration: 001107_internal_shorten_diagnostic_retention.down.sql
-- Restore the former limit for data still present in the database.
DO $$
DECLARE
    diagnostic_table text;
    retention_job_id integer;
    retention_config jsonb;
BEGIN
    FOREACH diagnostic_table IN ARRAY ARRAY['hashboard_diagnostic_observation', 'container_module_diagnostic_observation']
    LOOP
        SELECT job_id, config INTO STRICT retention_job_id, retention_config
        FROM timescaledb_information.jobs
        WHERE hypertable_schema = 'public'
          AND hypertable_name = diagnostic_table
          AND proc_name = 'policy_retention';

        PERFORM alter_job(retention_job_id,
            config => retention_config || jsonb_build_object('drop_after', INTERVAL '30 days'));
    END LOOP;
END;
$$;

-- fleet-migration: 001106_internal_notification_metric_sample_retention.down.sql
DO $$
DECLARE
    retention_job_id integer;
    retention_config jsonb;
BEGIN
    SELECT job_id, config INTO STRICT retention_job_id, retention_config
    FROM timescaledb_information.jobs
    WHERE hypertable_schema = 'public'
      AND hypertable_name = 'notification_metric_sample'
      AND proc_name = 'policy_retention';

    PERFORM alter_job(retention_job_id,
        config => retention_config || jsonb_build_object('drop_after', INTERVAL '7 days'));
END;
$$;

-- fleet-migration: 001101_internal_node_connectivity_monitoring.down.sql
DROP VIEW fleet_node_connectivity_summary;
DROP TABLE fleet_node_monitoring_exclusion;

-- fleet-migration: 001100_internal_activation_review_guards.down.sql
-- requires-shared: 1074
CREATE OR REPLACE FUNCTION ct1_hashboard_allocation_ready(target_org bigint, controller bigint)
RETURNS boolean LANGUAGE sql VOLATILE AS $$
 SELECT EXISTS (
  SELECT 1 FROM container_mining_group_state s
  JOIN device_write_fence f ON f.device_id=s.device_id AND f.organization_id=s.organization_id
  WHERE s.organization_id=target_org AND s.device_id=controller
   AND f.state='host' AND s.status NOT IN ('pending','applying','blocked')
   AND s.observed_at>=f.updated_at
   AND s.observed_at>clock_timestamp()-interval '2 minutes' AND s.observed_at<=clock_timestamp()+interval '5 seconds'
   AND COALESCE((s.observed->>'revision')::bigint,0)>0
   AND s.observed @> '{"supported":true,"enabled":true,"suspended":false,"pending":false}'::jsonb
   AND NOT EXISTS (SELECT 1 FROM device_write_attempt a WHERE a.device_id=controller
    AND a.organization_id=target_org AND a.operation_kind='mutation' AND a.status IN ('running','unknown'))
 );
$$;

CREATE OR REPLACE FUNCTION protect_activation_hashboard_owner() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='UPDATE' AND NEW.organization_id=OLD.organization_id
  AND NEW.kind=OLD.kind AND NEW.asset_id=OLD.asset_id
  AND NEW.hashboard_id IS NOT DISTINCT FROM OLD.hashboard_id
  AND NEW.client_id IS NOT DISTINCT FROM OLD.client_id
  AND NEW.subclient_id IS NOT DISTINCT FROM OLD.subclient_id THEN RETURN NEW; END IF;
 IF EXISTS (SELECT 1 FROM mining_group_pool_handoff h
  CROSS JOIN LATERAL jsonb_array_elements(h.captured->'Inventory') b
  WHERE h.placement_pending AND NOT COALESCE((h.captured->>'PartialTransfer')::boolean,false) AND (
   (TG_OP<>'INSERT' AND OLD.kind=7 AND h.organization_id=OLD.organization_id AND (b->>'hashboard_id')::bigint=OLD.asset_id)
   OR (TG_OP<>'DELETE' AND NEW.kind=7 AND h.organization_id=NEW.organization_id AND (b->>'hashboard_id')::bigint=NEW.asset_id))) THEN
  RAISE EXCEPTION 'Hashboard ownership cannot change until the activation pool assignment transfer is complete.';
 END IF;
 RETURN CASE WHEN TG_OP='DELETE' THEN OLD ELSE NEW END;
END;
$$;

-- fleet-migration: 001099_internal_activation_board_intent.down.sql
-- requires-shared: 1074
DO $$
BEGIN
 LOCK TABLE activation_pool_board_intent IN ACCESS EXCLUSIVE MODE;
 IF EXISTS (SELECT 1 FROM activation_pool_board_intent) THEN
  RAISE EXCEPTION 'Cannot roll back while activation board intent is pending.';
 END IF;
END;
$$;
CREATE OR REPLACE FUNCTION protect_activation_pool_handoff() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF EXISTS (SELECT 1 FROM mining_group_pool_handoff h
  WHERE h.organization_id=CASE WHEN TG_OP='INSERT' THEN NEW.organization_id ELSE OLD.organization_id END
   AND h.device_id=CASE WHEN TG_OP='INSERT' THEN NEW.device_id ELSE OLD.device_id END AND h.placement_pending) THEN
  RAISE EXCEPTION 'Pool changes are blocked until all hashboard locations are confirmed.';
 END IF;
 RETURN CASE WHEN TG_OP='DELETE' THEN OLD ELSE NEW END;
END;
$$;
CREATE OR REPLACE FUNCTION protect_activation_hashboard_owner() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 -- A controller move changes placement and its revision, not ownership.
 -- Identity changes still require the fence, even when account fields match.
 IF TG_OP='UPDATE' AND NEW.organization_id=OLD.organization_id
  AND NEW.kind=OLD.kind AND NEW.asset_id=OLD.asset_id
  AND NEW.hashboard_id IS NOT DISTINCT FROM OLD.hashboard_id
  AND NEW.client_id IS NOT DISTINCT FROM OLD.client_id
  AND NEW.subclient_id IS NOT DISTINCT FROM OLD.subclient_id THEN
  RETURN NEW;
 END IF;
 IF EXISTS (SELECT 1 FROM mining_group_pool_handoff h
  CROSS JOIN LATERAL jsonb_array_elements(h.captured->'Inventory') b
  WHERE h.placement_pending AND (
   (TG_OP<>'INSERT' AND OLD.kind=7 AND h.organization_id=OLD.organization_id AND (b->>'hashboard_id')::bigint=OLD.asset_id)
   OR (TG_OP<>'DELETE' AND NEW.kind=7 AND h.organization_id=NEW.organization_id AND (b->>'hashboard_id')::bigint=NEW.asset_id))) THEN
  RAISE EXCEPTION 'Hashboard ownership cannot change until the activation pool assignment transfer is complete.';
 END IF;
 RETURN CASE WHEN TG_OP='DELETE' THEN OLD ELSE NEW END;
END;
$$;
DROP TABLE activation_pool_board_intent;

-- fleet-migration: 001098_internal_activation_pool_inventory.down.sql
LOCK TABLE mining_group_pool_handoff IN EXCLUSIVE MODE;
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM mining_group_pool_handoff WHERE placement_pending) THEN
  RAISE EXCEPTION 'Complete activation pool assignment transfers before rollback.';
 END IF;
END $$;

CREATE OR REPLACE FUNCTION retire_reallocated_pool_assignments(organization bigint, controller bigint, asset_kind integer, asset bigint)
RETURNS void LANGUAGE SQL AS $$
 WITH retired AS (
  UPDATE pool_configuration_assignment a SET retired_at=CURRENT_TIMESTAMP,
   version=a.version+1,updated_at=CURRENT_TIMESTAMP
  WHERE a.organization_id=organization AND a.retired_at IS NULL
   AND ((a.target_kind='device' AND a.device_id=controller) OR
    (a.target_kind='hashboard_installation' AND EXISTS (
     SELECT 1 FROM hashboard_installation i WHERE i.id=a.target_id
      AND ((asset_kind=6 AND i.module_id=asset) OR (asset_kind=7 AND i.hashboard_id=asset)))))
  RETURNING a.organization_id,a.target_kind,a.target_id
 )
 UPDATE pool_configuration_operation_target t SET state='superseded',updated_at=CURRENT_TIMESTAMP
 FROM retired a WHERE t.organization_id=a.organization_id AND t.target_kind=a.target_kind AND t.target_id=a.target_id
  AND t.state IN ('pending','applying','failed','different','applied_unverified');
$$;

DROP TRIGGER protect_activation_hashboard_owner ON hosted_account_asset;
DROP FUNCTION protect_activation_hashboard_owner();
DROP TRIGGER protect_activation_pool_handoff ON pool_configuration_assignment;
DROP FUNCTION protect_activation_pool_handoff();
DROP INDEX idx_mining_group_pool_handoff_pending_device;
ALTER TABLE mining_group_pool_handoff DROP COLUMN placement_pending;

-- fleet-migration: 001097_internal_ticketing_categories.down.sql
-- Keep the writer fence, refusal guard, and constraint change in one statement
-- so standalone execution cannot drop Other tickets after a failed guard.
DO $$
BEGIN
 LOCK TABLE repair_ticket IN ACCESS EXCLUSIVE MODE;
 IF EXISTS (SELECT 1 FROM repair_ticket WHERE category > 5) THEN
  RAISE EXCEPTION 'Other tickets exist; preserve them and fix forward';
 END IF;
 ALTER TABLE repair_ticket DROP CONSTRAINT ck_repair_ticket_category;
 ALTER TABLE repair_ticket ADD CONSTRAINT ck_repair_ticket_category CHECK (category BETWEEN 1 AND 5);
END;
$$;

-- fleet-migration: 001096_internal_release_channel_controller_software.down.sql
DROP FUNCTION release_channel_controller_software_key(text);
DROP TABLE release_channel_controller_software;

-- fleet-migration: 001095_internal_diagnostic_history_compression.down.sql
-- Retained chunks must be decompressed before disabling compression. Rollback
-- needs enough free disk space and time to restore their uncompressed contents.
SELECT remove_compression_policy('hashboard_diagnostic_observation');
SELECT decompress_chunk(c, if_compressed => true) FROM show_chunks('hashboard_diagnostic_observation') c;
ALTER TABLE hashboard_diagnostic_observation SET (timescaledb.compress = false);

SELECT remove_compression_policy('container_module_diagnostic_observation');
SELECT decompress_chunk(c, if_compressed => true) FROM show_chunks('container_module_diagnostic_observation') c;
ALTER TABLE container_module_diagnostic_observation SET (timescaledb.compress = false);

-- fleet-migration: 001094_internal_remove_minute_rollup_device_index.down.sql
-- An explicit downgrade rebuilds every chunk index: allow a maintenance window
-- and enough free storage/WAL capacity. Application rollback does not run this.
DO $$
DECLARE
    materialization_table text;
BEGIN
    SELECT materialization_hypertable_name
    INTO STRICT materialization_table
    FROM timescaledb_information.continuous_aggregates
    WHERE view_schema = 'public'
      AND view_name = 'miner_state_snapshot_device_1m';

    -- Restore the original Timescale-generated name through the aggregate API.
    EXECUTE format(
        'CREATE INDEX %I ON public.miner_state_snapshot_device_1m (device_identifier, bucket DESC)',
        materialization_table || '_device_identifier_bucket_idx');
END
$$;

-- fleet-migration: 001093_internal_minute_rollup_daily_chunks.down.sql
-- Restores the interval for future chunks without merging existing daily chunks.
SELECT set_chunk_time_interval('miner_state_snapshot_device_1m', INTERVAL '10 days');

-- fleet-migration: 001092_internal_hosted_curtailment_observations.down.sql
LOCK TABLE hosted_curtailment_confirmation,hosted_curtailment_asset IN ACCESS EXCLUSIVE MODE;
DO $$ BEGIN
 IF EXISTS (SELECT 1 FROM hosted_curtailment_confirmation) THEN
  RAISE EXCEPTION 'cannot discard captured curtailment confirmations; fix forward or restore a coordinated backup';
 END IF;
END $$;
DROP TABLE hosted_curtailment_asset;
DROP TABLE hosted_curtailment_confirmation;

-- fleet-migration: 001091_internal_hosted_report_generation_permission.down.sql
LOCK TABLE organization IN SHARE ROW EXCLUSIVE MODE;
LOCK TABLE hosted_account, hosted_account_ancestor_grant IN ACCESS EXCLUSIVE MODE;
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM hosted_account WHERE (capabilities & 32)=0) THEN
  RAISE EXCEPTION 'disabled report generation prevents rollback';
 END IF;
END $$;

CREATE OR REPLACE FUNCTION create_hosted_account_root() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
 INSERT INTO hosted_account(account_id,organization_id,depth,name,capabilities)
 VALUES(gen_random_uuid()::text,NEW.id,0,COALESCE(NULLIF(btrim(NEW.name),''),'Host ' || NEW.id::text),31);
 RETURN NEW;
END;
$$;

UPDATE hosted_account SET capabilities=capabilities & 31,
 revision=revision+1, authority_revision=authority_revision+1, updated_at=CURRENT_TIMESTAMP;
UPDATE hosted_account_ancestor_grant SET capabilities=capabilities & 31,
 revision=revision+1, updated_at=CURRENT_TIMESTAMP WHERE (capabilities & 32)<>0;
ALTER TABLE hosted_account DROP CONSTRAINT hosted_account_capabilities_check;
ALTER TABLE hosted_account ADD CONSTRAINT hosted_account_capabilities_check CHECK (capabilities BETWEEN 1 AND 31);
ALTER TABLE hosted_account_ancestor_grant DROP CONSTRAINT hosted_account_ancestor_grant_capabilities_check;
ALTER TABLE hosted_account_ancestor_grant ADD CONSTRAINT hosted_account_ancestor_grant_capabilities_check CHECK (capabilities BETWEEN 1 AND 31);

-- fleet-migration: 001090_internal_account_analysis_deletion.down.sql
LOCK TABLE hosted_account_analysis, hosted_account_analysis_deletion IN ACCESS EXCLUSIVE MODE;
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM hosted_account_analysis_deletion) THEN
  RAISE EXCEPTION 'analysis deletion receipts prevent rollback';
 END IF;
END $$;
DROP TRIGGER hosted_account_analysis_delete ON hosted_account_analysis;
DROP TRIGGER hosted_account_analysis_no_recreation ON hosted_account_analysis;
DROP TRIGGER hosted_account_analysis_immutable ON hosted_account_analysis;
CREATE TRIGGER hosted_account_analysis_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON hosted_account_analysis
 FOR EACH STATEMENT EXECUTE FUNCTION reject_hosted_account_analysis_mutation();
DROP FUNCTION guard_hosted_account_analysis_deletion();
DROP TABLE hosted_account_analysis_deletion;
DROP FUNCTION guard_hosted_account_analysis_deletion_receipt();

-- fleet-migration: 001089_internal_hosted_reporting_delivery.down.sql
LOCK TABLE hosted_report_queue, hosted_report_queue_event, hosted_account_analysis IN ACCESS EXCLUSIVE MODE;
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM hosted_account_analysis WHERE request_digest<>'') OR EXISTS(SELECT 1 FROM hosted_report_queue WHERE cardinality(delivered_channel_ids)>0 OR notification_status IN ('unrouted','exhausted','blocked')) OR EXISTS(SELECT 1 FROM hosted_report_queue_event WHERE kind IN ('unrouted','exhausted','blocked')) THEN
  RAISE EXCEPTION 'reporting notification history prevents rollback';
 END IF;
END $$;
ALTER TABLE hosted_report_queue DROP CONSTRAINT ck_hosted_report_queue_notification;
ALTER TABLE hosted_report_queue ADD CONSTRAINT ck_hosted_report_queue_notification CHECK(notification_status IN ('pending','sent','failed','acknowledged','quiet'));
ALTER TABLE hosted_report_queue_event DROP CONSTRAINT ck_hosted_report_queue_event_kind;
ALTER TABLE hosted_report_queue_event ADD CONSTRAINT ck_hosted_report_queue_event_kind CHECK(kind IN ('changed','attempted','sent','failed','acknowledged','retry'));
ALTER TABLE hosted_report_queue DROP COLUMN delivered_channel_ids;

DROP INDEX uq_hosted_account_analysis_request;
ALTER TABLE hosted_account_analysis DROP COLUMN request_key, DROP COLUMN request_digest;

DROP INDEX idx_hosted_report_queue_active_page;

-- fleet-migration: 001088_internal_hosted_account_analyses.down.sql
LOCK TABLE hosted_account_analysis IN ACCESS EXCLUSIVE MODE;
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM hosted_account_analysis) THEN
  RAISE EXCEPTION 'saved analyses prevent rollback';
 END IF;
END $$;
DROP TRIGGER hosted_account_analysis_immutable ON hosted_account_analysis;
DROP FUNCTION reject_hosted_account_analysis_mutation();
DROP TABLE hosted_account_analysis;

-- fleet-migration: 001087_internal_hosted_report_request_fulfillment.down.sql
LOCK TABLE hosted_report_request_fulfillment IN ACCESS EXCLUSIVE MODE;
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM hosted_report_request_fulfillment) THEN
  RAISE EXCEPTION 'cannot remove retained report request fulfillment';
 END IF;
END $$;
DROP TABLE hosted_report_request_fulfillment;
DROP INDEX idx_hosted_report_issued_request;

-- fleet-migration: 001086_internal_hosted_report_queue.down.sql
LOCK TABLE hosted_report_queue,hosted_report_queue_event IN ACCESS EXCLUSIVE MODE;
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM hosted_report_queue) OR EXISTS(SELECT 1 FROM hosted_report_queue_event) THEN
  RAISE EXCEPTION 'retained report queue and notification history prevents rollback';
 END IF;
END $$;
DROP TABLE hosted_report_queue_event;
DROP FUNCTION reject_hosted_report_queue_event_mutation();
DROP TABLE hosted_report_queue;

-- fleet-migration: 001085_internal_container_power_feed_permission.down.sql
DELETE FROM role_permission WHERE permission_id IN (SELECT id FROM permission WHERE key = 'container:power_feed');
DELETE FROM permission WHERE key = 'container:power_feed';

-- fleet-migration: 001084_internal_mining_group_step_dispatch.down.sql
-- Fence step producers before checking their journals. Keep the guard and
-- removal atomic even when this file runs outside the checkpoint runner.
DO $$ BEGIN
 LOCK TABLE container_mining_group_state, mining_group_step_dispatch IN ACCESS EXCLUSIVE MODE;
 IF EXISTS (SELECT 1 FROM container_mining_group_state WHERE step_key IS NOT NULL AND status IN ('pending','applying','blocked')) THEN
  RAISE EXCEPTION 'Settle saved mining-group requests before removing dispatch tracking';
 END IF;
 DROP TABLE mining_group_step_dispatch;
END $$;

-- fleet-migration: 001083_internal_mining_group_default_return.down.sql
DO $$ BEGIN
 IF EXISTS (SELECT 1 FROM container_mining_group_state WHERE operation_kind='return_default') THEN
  RAISE EXCEPTION 'Cannot remove return-to-Default workflow history';
 END IF;
END $$;
ALTER TABLE container_mining_group_state DROP CONSTRAINT chk_container_mining_group_state_operation_kind;
ALTER TABLE container_mining_group_state ADD CONSTRAINT chk_container_mining_group_state_operation_kind
 CHECK (operation_kind IN ('','activate','deactivate','pools'));
ALTER TABLE container_mining_group_state DROP COLUMN default_return;

-- fleet-migration: 001082_internal_ct1_mining_group_guards.down.sql
-- Retained deactivation journals are physical-write evidence. Do not erase them
-- or make their retry identity unreadable during an application rollback.
DO $$ BEGIN
 IF EXISTS (SELECT 1 FROM container_mining_group_state WHERE operation_kind='deactivate') THEN
  RAISE EXCEPTION 'retain deactivation journals; use a coordinated application and database backup for rollback';
 END IF;
 IF EXISTS (SELECT 1 FROM pool_lifecycle_transition WHERE state='deferred') THEN
  RAISE EXCEPTION 'retain deferred pool lifecycle work; settle deactivation before database downgrade';
 END IF;
END $$;
DROP TRIGGER pool_lifecycle_mining_group_deactivation ON pool_lifecycle_transition;
DROP FUNCTION guard_pool_lifecycle_mining_group_deactivation();
DROP FUNCTION ct1_hashboard_allocation_ready(bigint,bigint);
DROP FUNCTION pool_lifecycle_clear_retryable(pool_lifecycle_transition,device_write_attempt);
DROP INDEX idx_pool_configuration_operation_target_open;
CREATE INDEX idx_pool_configuration_operation_target_open
 ON pool_configuration_operation_target(organization_id,target_kind,target_id)
 WHERE state IN ('pending','applying','failed','different','applied_unverified');
DROP INDEX idx_hosted_account_asset_allocated_hashboard_device;
ALTER TABLE pool_lifecycle_transition DROP CONSTRAINT chk_pool_lifecycle_transition_state;
ALTER TABLE pool_lifecycle_transition ADD CONSTRAINT pool_lifecycle_transition_state_check
 CHECK (state IN ('pending','ready','applying','verified','completed','failed','uncertain','superseded'));
DROP INDEX idx_pool_lifecycle_transition_open_asset;
CREATE INDEX idx_pool_lifecycle_transition_open_asset
 ON pool_lifecycle_transition(state,organization_id,asset_kind,asset_id)
 WHERE state IN ('pending','ready','applying','uncertain','failed');
ALTER TABLE container_mining_group_state DROP CONSTRAINT chk_container_mining_group_state_operation_kind;
ALTER TABLE container_mining_group_state ADD CONSTRAINT container_mining_group_state_operation_kind_check
 CHECK (operation_kind IN ('','activate','pools'));
DELETE FROM role_permission WHERE permission_id IN (SELECT id FROM permission WHERE key='miner:deactivate_mining_groups');
DELETE FROM permission WHERE key='miner:deactivate_mining_groups';

-- Restore the lifecycle trigger behavior from migration 1078.
CREATE OR REPLACE FUNCTION enqueue_ownership_pool_transition() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE owner bigint;
BEGIN
 IF TG_OP='UPDATE' AND NEW.client_id IS NOT DISTINCT FROM OLD.client_id
  AND NEW.subclient_id IS NOT DISTINCT FROM OLD.subclient_id AND NEW.device_id=OLD.device_id THEN RETURN NEW; END IF;
 IF TG_OP='INSERT' AND NEW.client_id IS NULL THEN RETURN NEW; END IF;
 owner := COALESCE(NEW.subclient_id,NEW.client_id,0);
 UPDATE pool_lifecycle_transition SET state='superseded',updated_at=clock_timestamp()
 WHERE organization_id=NEW.organization_id AND ((asset_kind=NEW.kind AND asset_id=NEW.asset_id) OR (asset_kind=0 AND NEW.kind=5 AND device_id=NEW.device_id))
  AND state IN ('pending','ready','failed');
 INSERT INTO pool_lifecycle_transition(organization_id,device_id,asset_kind,asset_id,owner_account_id,allocation_revision,reason,configuration_id,revision_id)
 SELECT NEW.organization_id,NEW.device_id,NEW.kind,NEW.asset_id,owner,NEW.revision,'ownership',c.configuration_id,c.current_revision_id
 FROM (SELECT 1) seed LEFT JOIN pool_configuration c ON c.organization_id=NEW.organization_id
  AND COALESCE(c.account_id,0)=owner AND c.is_default AND NOT c.archived;
 RETURN NEW;
END $$;

CREATE OR REPLACE FUNCTION enqueue_pairing_pool_transition() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE organization bigint; class integer; cycle uuid;
BEGIN
 -- Authentication/endpoint recovery is still the same pairing cycle. Only
 -- an explicit unpair resets it, including local unpairs without unpaired_at.
 IF NEW.pairing_status='UNPAIRED' THEN
  DELETE FROM pool_pairing_cycle WHERE device_id=NEW.device_id;
  -- Cancel unclaimed intent, including expanded CT1 children. Accepted work
  -- checks the cycle at dispatch; its physical evidence and recovery stay intact.
  UPDATE pool_lifecycle_transition SET state='superseded',result_code='pairing_cycle_ended',updated_at=clock_timestamp()
  WHERE organization_id=(SELECT org_id FROM device WHERE id=NEW.device_id) AND device_id=NEW.device_id
   AND reason='pairing' AND state IN ('pending','ready')
   AND operation_id IS NULL AND physical_attempt_id IS NULL;
  RETURN NEW;
 END IF;
 IF NEW.pairing_status NOT IN ('PAIRED','DEFAULT_PASSWORD') THEN RETURN NEW; END IF;
 SELECT d.org_id,dd.device_class INTO organization,class FROM device d JOIN discovered_device dd ON dd.id=d.discovered_device_id WHERE d.id=NEW.device_id AND d.deleted_at IS NULL;
 IF NOT FOUND THEN RETURN NEW; END IF;
 INSERT INTO pool_pairing_cycle(device_id) VALUES(NEW.device_id) ON CONFLICT DO NOTHING RETURNING cycle_id INTO cycle;
 IF NOT FOUND THEN RETURN NEW; END IF;
 IF class=2 THEN
  INSERT INTO pool_lifecycle_transition(organization_id,device_id,asset_kind,asset_id,reason,pairing_cycle_id,pairing_defaults)
  SELECT organization,NEW.device_id,0,NEW.device_id,'pairing',cycle,COALESCE(jsonb_object_agg(COALESCE(c.account_id,0)::text,jsonb_build_object('configuration_id',c.configuration_id,'revision_id',c.current_revision_id)),'{}')
  FROM pool_configuration c WHERE c.organization_id=organization AND c.is_default AND NOT c.archived
   AND (c.account_id IS NULL OR EXISTS(SELECT 1 FROM hosted_account_asset a WHERE a.organization_id=organization AND a.device_id=NEW.device_id AND COALESCE(a.subclient_id,a.client_id)=c.account_id));
 ELSE
  INSERT INTO pool_lifecycle_transition(organization_id,device_id,asset_kind,asset_id,owner_account_id,allocation_revision,reason,pairing_cycle_id,configuration_id,revision_id)
  SELECT organization,NEW.device_id,5,NEW.device_id,COALESCE(a.subclient_id,a.client_id,0),COALESCE(a.revision,0),'pairing',cycle,c.configuration_id,c.current_revision_id
  FROM (SELECT 1) seed LEFT JOIN hosted_account_asset a ON a.organization_id=organization AND a.kind=5 AND a.asset_id=NEW.device_id
  LEFT JOIN pool_configuration c ON c.organization_id=organization AND COALESCE(c.account_id,0)=COALESCE(a.subclient_id,a.client_id,0) AND c.is_default AND NOT c.archived;
 END IF;
 RETURN NEW;
END $$;

CREATE OR REPLACE FUNCTION retire_absent_board_pool_transition() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.asset_kind=7 AND NEW.state='pending' AND NEW.operation_id IS NULL AND NEW.physical_attempt_id IS NULL
  AND NOT EXISTS(SELECT 1 FROM hashboard_installation i WHERE i.hashboard_id=NEW.asset_id AND i.device_id=NEW.device_id AND i.removed_at IS NULL)
  AND EXISTS(SELECT 1 FROM hashboard_installation i WHERE i.hashboard_id=NEW.asset_id AND i.device_id=NEW.device_id AND i.removed_at IS NOT NULL) THEN
  NEW.state := 'superseded';
  NEW.result_code := 'board_removed';
 END IF;
 RETURN NEW;
END $$;

CREATE OR REPLACE FUNCTION retire_removed_board_pool_transitions() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 -- Accepted removal, not stale telemetry, ends this physical installation.
 -- Claimed operations and physical attempts retain their recovery machinery.
 UPDATE pool_lifecycle_transition SET state='superseded',result_code='board_removed',updated_at=clock_timestamp()
 WHERE device_id=NEW.device_id AND asset_kind=7 AND asset_id=NEW.hashboard_id
  AND state IN ('pending','ready') AND operation_id IS NULL AND physical_attempt_id IS NULL;
 RETURN NEW;
END $$;

CREATE OR REPLACE FUNCTION resume_reinstalled_board_pool_transition() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE organization bigint;
BEGIN
 SELECT org_id INTO organization FROM device WHERE id=NEW.device_id AND deleted_at IS NULL;
 IF NOT FOUND THEN RETURN NEW; END IF;
 -- Consume retired, undispatched intent once. A returning board gets a new
 -- policy for its current owner; an archived or changed default is not revived.
 UPDATE pool_lifecycle_transition SET result_code='board_reinstalled',updated_at=clock_timestamp()
 WHERE organization_id=organization AND asset_kind=7 AND asset_id=NEW.hashboard_id
  AND state='superseded' AND result_code='board_removed';
 IF NOT FOUND THEN RETURN NEW; END IF;
 INSERT INTO pool_lifecycle_transition(organization_id,device_id,asset_kind,asset_id,owner_account_id,allocation_revision,reason,configuration_id,revision_id)
 SELECT organization,NEW.device_id,7,NEW.hashboard_id,COALESCE(a.subclient_id,a.client_id,0),COALESCE(a.revision,0),'ownership',c.configuration_id,c.current_revision_id
 FROM (SELECT 1) seed LEFT JOIN hosted_account_asset a ON a.organization_id=organization AND a.kind=7 AND a.asset_id=NEW.hashboard_id
 LEFT JOIN pool_configuration c ON c.organization_id=organization
  AND COALESCE(c.account_id,0)=COALESCE(a.subclient_id,a.client_id,0) AND c.is_default AND NOT c.archived
 -- A controller move may already have captured the same current ownership
 -- policy when its observation updated placement, before installation insert.
 WHERE NOT EXISTS(SELECT 1 FROM pool_lifecycle_transition l
  WHERE l.organization_id=organization AND l.device_id=NEW.device_id AND l.asset_kind=7 AND l.asset_id=NEW.hashboard_id
   AND l.owner_account_id=COALESCE(a.subclient_id,a.client_id,0) AND l.allocation_revision=COALESCE(a.revision,0)
   AND l.state IN ('pending','ready','applying','uncertain'));
 RETURN NEW;
END $$;

-- fleet-migration: 001081_internal_hashboard_worker_names.down.sql
ALTER TABLE pool_configuration_assignment DROP COLUMN inherited_worker_name;
ALTER TABLE hashboard DROP COLUMN worker_name_mode, DROP COLUMN worker_name;

-- fleet-migration: 001080_internal_hosted_report_artifacts.down.sql
LOCK TABLE hosted_report_artifact,hosted_report_artifact_chunk IN ACCESS EXCLUSIVE MODE;
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM hosted_report_artifact) THEN
  RAISE EXCEPTION 'preserve frozen report artifact bytes; restore a coordinated backup or fix forward';
 END IF;
END $$;
DROP TABLE hosted_report_artifact_chunk;
DROP TABLE hosted_report_artifact;
DROP FUNCTION guard_hosted_report_artifact();

-- fleet-migration: 001079_internal_hosted_reports.down.sql
LOCK TABLE hosted_report,hosted_report_request IN ACCESS EXCLUSIVE MODE;
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM hosted_report) OR EXISTS(SELECT 1 FROM hosted_report_request) THEN
  RAISE EXCEPTION 'cannot remove retained report history';
 END IF;
END $$;
DROP TABLE hosted_report_request,hosted_report_delivery;
DROP FUNCTION reject_hosted_report_append_mutation();
DROP TABLE hosted_report;
DROP FUNCTION guard_hosted_report();
DROP FUNCTION hosted_report_evidence_cutoff(timestamptz);
ALTER TABLE hosted_account_reporting_hour_gap ALTER COLUMN recorded_at SET DEFAULT clock_timestamp();
ALTER TABLE hosted_account_reporting_hour ALTER COLUMN captured_at SET DEFAULT clock_timestamp();
DROP FUNCTION hosted_reporting_evidence_step_at();

-- fleet-migration: 001078_internal_default_pool_lifecycle.down.sql
-- requires-shared: 1067
-- Fence producers before checking history, and retain every lock through DDL.
-- This single statement is atomic even outside the normal migration runner.
DO $$
BEGIN
 LOCK TABLE organization IN EXCLUSIVE MODE;
 LOCK TABLE device_pairing, hosted_account_asset, hashboard_installation IN ACCESS EXCLUSIVE MODE;
 LOCK TABLE pool_lifecycle_transition IN ACCESS EXCLUSIVE MODE;
 IF EXISTS(SELECT 1 FROM pool_lifecycle_transition) THEN
  RAISE EXCEPTION 'retain pool lifecycle history; restore a coordinated backup to roll back';
 END IF;

 DROP TRIGGER hashboard_reinstalled_pool_transition ON hashboard_installation;
 DROP FUNCTION resume_reinstalled_board_pool_transition();
 DROP TRIGGER hashboard_removed_pool_transitions ON hashboard_installation;
 DROP FUNCTION retire_removed_board_pool_transitions();
 DROP TRIGGER pool_lifecycle_absent_board ON pool_lifecycle_transition;
 DROP FUNCTION retire_absent_board_pool_transition();
 DROP TRIGGER device_pairing_pool_transition ON device_pairing;
 DROP FUNCTION enqueue_pairing_pool_transition();
 DROP TRIGGER hosted_allocation_pool_transition ON hosted_account_asset;
 DROP FUNCTION enqueue_ownership_pool_transition();
 DROP TABLE pool_pairing_cycle;

 ALTER TABLE pool_configuration_operation DROP CONSTRAINT ck_pool_configuration_operation_actor;
 ALTER TABLE pool_configuration_operation ADD CONSTRAINT pool_configuration_operation_actor_id_check CHECK(actor_id>0);
 ALTER TABLE pool_configuration_operation DROP COLUMN lifecycle_id;
 DROP VIEW pool_lifecycle_removal;
 DROP TABLE pool_lifecycle_transition;
 DROP FUNCTION validate_pool_lifecycle_transition();

 DROP INDEX idx_pool_configuration_owner_default;
 ALTER TABLE pool_configuration DROP CONSTRAINT ck_pool_configuration_active_default;
 ALTER TABLE pool_configuration DROP COLUMN is_default;
END;
$$;

-- fleet-migration: 001077_internal_hourly_reporting_evidence.down.sql
-- requires-shared: 1000
-- A schema rollback cannot discard captured hourly evidence. Stop writers first.
LOCK TABLE device_metrics,miner_state_snapshots,container_module_metrics,
 container_module_diagnostic_observation,hashboard_metrics,hashboard_diagnostic_observation,
 hosted_account_history_sample
 IN SHARE ROW EXCLUSIVE MODE;
LOCK TABLE hosted_account_reporting_hour,hosted_account_reporting_hour_gap IN ACCESS EXCLUSIVE MODE;
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM hosted_account_reporting_hour) OR EXISTS(SELECT 1 FROM hosted_account_reporting_hour_gap) THEN
  RAISE EXCEPTION 'preserve hourly hosted reporting evidence; rollback requires an empty hold';
 END IF;
END $$;
SELECT delete_job(job_id) FROM timescaledb_information.jobs WHERE proc_name='hosted_reporting_hour_job';
DROP VIEW hosted_account_reporting_capture_status;
DROP PROCEDURE hosted_reporting_hour_job(integer,jsonb);
DROP TRIGGER enqueue_hosted_reporting_hour_interval ON hosted_account_asset_interval;
DROP TRIGGER enqueue_hosted_reporting_hour_sample_update ON hosted_account_history_sample;
DROP TRIGGER enqueue_hosted_reporting_hour_sample ON hosted_account_history_sample;
DROP FUNCTION enqueue_hosted_reporting_hour_interval();
DROP FUNCTION enqueue_hosted_reporting_hour_sample();
DROP FUNCTION hosted_reporting_hour_revise(timestamptz,interval,integer);
DROP FUNCTION hosted_reporting_hour_finish_close(timestamptz,interval);
DROP FUNCTION hosted_reporting_hour_begin_close(timestamptz,interval,interval,integer,integer);
DROP FUNCTION hosted_reporting_hour_rows(timestamptz,timestamptz,bigint[]);
DROP FUNCTION hosted_reporting_expected_streams();
DROP FUNCTION hosted_reporting_hour_window_floor(timestamptz,interval);
DROP FUNCTION hosted_reporting_hour_summary(timestamptz,timestamptz,bigint[]);
DROP TABLE hosted_account_reporting_hour_marker;
DROP TABLE hosted_account_reporting_hour_gap;
DROP TABLE hosted_account_reporting_hour;
DROP TABLE hosted_account_reporting_hour_control;

-- Restore the empty 1055 raw hold exactly as 1055 created it, with a new epoch.
-- Retired raw events cannot be restored; prefer fixing forward.
CREATE TABLE hosted_account_reporting_evidence_epoch (
 singleton boolean CONSTRAINT pk_hosted_reporting_evidence_epoch PRIMARY KEY DEFAULT true
  CONSTRAINT ck_hosted_reporting_evidence_epoch_singleton CHECK(singleton),
 started_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
INSERT INTO hosted_account_reporting_evidence_epoch(singleton) VALUES(true);

CREATE TABLE hosted_account_reporting_evidence (
 event_id bigserial CONSTRAINT pk_hosted_reporting_evidence PRIMARY KEY,
 epoch_started_at timestamptz NOT NULL,
 captured_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 transaction_id bigint NOT NULL DEFAULT txid_current(),
 operation text NOT NULL CONSTRAINT ck_hosted_reporting_evidence_operation CHECK(operation IN ('INSERT','UPDATE','DELETE')),
 image text NOT NULL CONSTRAINT ck_hosted_reporting_evidence_image CHECK(image IN ('before','after')),
 time timestamptz NOT NULL,
 interval_id bigint NOT NULL REFERENCES hosted_account_reporting_allocation_snapshot(interval_id),
 metric smallint NOT NULL,
 value double precision,
 complete boolean,
 source_oldest_at timestamptz NOT NULL,
 source_newest_at timestamptz NOT NULL,
 source_id smallint NOT NULL,
 origin_key text NOT NULL,
 origin_time timestamptz NOT NULL,
 origin_collected_at timestamptz NOT NULL
);
CREATE INDEX idx_hosted_account_reporting_evidence_stream_event
 ON hosted_account_reporting_evidence(interval_id,source_id,metric,event_id DESC);
CREATE INDEX idx_hosted_account_reporting_evidence_stream_observation
 ON hosted_account_reporting_evidence(interval_id,source_id,metric,event_id DESC)
 WHERE operation IN ('INSERT','UPDATE') AND image='after';

CREATE TRIGGER hosted_reporting_evidence_immutable
 BEFORE UPDATE OR DELETE OR TRUNCATE ON hosted_account_reporting_evidence
 FOR EACH STATEMENT EXECUTE FUNCTION reject_hosted_reporting_evidence_mutation();
CREATE TRIGGER hosted_reporting_evidence_epoch_immutable
 BEFORE UPDATE OR DELETE OR TRUNCATE ON hosted_account_reporting_evidence_epoch
 FOR EACH STATEMENT EXECUTE FUNCTION reject_hosted_reporting_evidence_mutation();

CREATE FUNCTION record_hosted_reporting_evidence() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
 sample hosted_account_history_sample;
 sample_image text;
 images text[] := CASE TG_OP WHEN 'UPDATE' THEN ARRAY['before','after']
  WHEN 'DELETE' THEN ARRAY['before'] ELSE ARRAY['after'] END;
BEGIN
 FOREACH sample_image IN ARRAY images LOOP
  IF sample_image='before' THEN sample := OLD; ELSE sample := NEW; END IF;
  INSERT INTO hosted_account_reporting_allocation_snapshot(interval_id,organization_id,client_id,subclient_id,
   site_id,kind,asset_id,device_id,device_identifier,started_at)
  SELECT id,organization_id,client_id,subclient_id,site_id,kind,asset_id,device_id,device_identifier,started_at
  FROM hosted_account_asset_interval WHERE id=sample.interval_id
  ON CONFLICT(interval_id) DO NOTHING;
  INSERT INTO hosted_account_reporting_evidence(epoch_started_at,operation,image,time,interval_id,metric,value,complete,
   source_oldest_at,source_newest_at,source_id,origin_key,origin_time,origin_collected_at)
  SELECT started_at,TG_OP,sample_image,sample.time,sample.interval_id,sample.metric,sample.value,sample.complete,
   sample.source_oldest_at,sample.source_newest_at,sample.source_id,sample.origin_key,sample.origin_time,sample.origin_collected_at
  FROM hosted_account_reporting_evidence_epoch WHERE singleton;
  IF NOT FOUND THEN RAISE EXCEPTION 'hosted reporting evidence epoch is missing'; END IF;
 END LOOP;
 RETURN NULL;
END;
$$;
-- DELETE means normalized-sample deletion. Corrections commonly delete and
-- insert in one transaction; neither deletion nor chunk expiry proves retraction.
CREATE TRIGGER record_hosted_reporting_evidence
 AFTER INSERT OR UPDATE OR DELETE ON hosted_account_history_sample
 FOR EACH ROW EXECUTE FUNCTION record_hosted_reporting_evidence();

CREATE VIEW hosted_account_reporting_capture_status AS
SELECT i.id AS interval_id,i.organization_id,i.client_id,i.subclient_id,i.site_id,i.kind,
 i.asset_id,i.device_id,i.device_identifier,i.started_at AS allocation_started_at,
 epoch.started_at AS hold_started_at,stream.source_id,stream.metric,
 latest.event_id AS last_event_id,latest.operation AS last_event_operation,
 latest.captured_at AS last_event_captured_at,
 observation.event_id AS last_observation_event_id,
 observation.captured_at AS last_observation_captured_at,
 observation.value AS last_observation_value,observation.complete AS last_observation_complete,
 observation.origin_key AS last_observation_origin_key,
 observation.origin_time AS last_observation_origin_time,
 observation.origin_collected_at AS last_observation_collected_at,
 observation.source_oldest_at AS last_observation_source_oldest_at,
 observation.source_newest_at AS last_observation_source_newest_at
FROM hosted_account_asset_interval i
CROSS JOIN hosted_account_reporting_evidence_epoch epoch
JOIN (
 SELECT 5 AS kind,1::smallint AS source_id,hosted_history_metric_code('hashrate') AS metric
 UNION ALL SELECT 5,1::smallint,hosted_history_metric_code('power')
 UNION ALL SELECT 5,1::smallint,hosted_history_metric_code('temperature')
 UNION ALL SELECT 5,2::smallint,hosted_history_metric_code('state')
 UNION ALL SELECT 6,3::smallint,hosted_history_metric_code('hashrate')
 UNION ALL SELECT 6,3::smallint,hosted_history_metric_code('power')
 UNION ALL SELECT 6,3::smallint,hosted_history_metric_code('temperature')
 UNION ALL SELECT 6,4::smallint,hosted_history_metric_code('state')
 UNION ALL SELECT 7,5::smallint,hosted_history_metric_code('hashrate')
 UNION ALL SELECT 7,5::smallint,hosted_history_metric_code('power')
 UNION ALL SELECT 7,5::smallint,hosted_history_metric_code('temperature')
 UNION ALL SELECT 7,6::smallint,hosted_history_metric_code('state')
) stream ON stream.kind=i.kind
LEFT JOIN LATERAL (
 SELECT event_id,operation,captured_at FROM hosted_account_reporting_evidence e
 WHERE e.interval_id=i.id AND e.source_id=stream.source_id AND e.metric=stream.metric
 ORDER BY event_id DESC LIMIT 1
) latest ON true
LEFT JOIN LATERAL (
 SELECT event_id,captured_at,value,complete,origin_key,origin_time,origin_collected_at,source_oldest_at,source_newest_at
 FROM hosted_account_reporting_evidence e WHERE e.interval_id=i.id
  AND e.source_id=stream.source_id AND e.metric=stream.metric
  AND operation IN ('INSERT','UPDATE') AND image='after'
 ORDER BY event_id DESC LIMIT 1
) observation ON true
WHERE i.ended_at IS NULL;

-- fleet-migration: 001076_internal_telemetry_freshness.down.sql
DROP VIEW telemetry_freshness_summary;
DROP TABLE telemetry_freshness_exclusion;
DROP TABLE telemetry_freshness;

-- fleet-migration: 001075_internal_frigate_updates_checkpoint.down.sql
SELECT 1;

-- fleet-migration: 001074_shared_frigate_updates.down.sql
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM frigate_update_job WHERE state IN ('staging', 'staged', 'starting', 'running')
                OR (state = 'failed' AND retryable)) THEN
  RAISE EXCEPTION 'finish or cancel in-progress Frigate updates before rolling back';
 END IF;
END $$;
DROP TABLE frigate_update_action;
DROP TABLE frigate_update_instance;
DROP TABLE frigate_update_job;
DROP TABLE frigate_update_package;
DELETE FROM role_permission WHERE permission_id IN (SELECT id FROM permission WHERE key = 'controller:software_update');
DELETE FROM permission WHERE key = 'controller:software_update';

-- fleet-migration: 001073_internal_ct1_firmware_observation.down.sql
DROP FUNCTION release_channel_firmware_version(bigint, integer, text);
ALTER TABLE container_inventory
    DROP COLUMN hashboard_firmware_mixed,
    DROP COLUMN hashboard_firmware_version;

-- fleet-migration: 001072_internal_topology_cleanup_runtime_guard.down.sql
-- Restore the lock-handoff procedure while preserving job configuration,
-- scheduling, cleanup progress, and the original deferral counters.
CREATE OR REPLACE PROCEDURE compact_container_topology_job(job_id integer,config jsonb)
LANGUAGE plpgsql AS $$
DECLARE
 target bigint;
 cutoff timestamptz := now()-INTERVAL '10 days';
 batch_size integer := GREATEST(1,LEAST(COALESCE((config->>'batch_size')::integer,50),50));
 configured_microbatch_size integer := GREATEST(1,LEAST(COALESCE((config->>'microbatch_size')::integer,3),batch_size));
 remaining_budget integer;
 microbatch_size integer;
 microbatch_result jsonb;
 made_progress boolean := false;
 end_run boolean;
BEGIN
 IF NOT COALESCE((config->>'enabled')::boolean,false) THEN RETURN; END IF;
 SELECT device_id INTO target FROM container_topology_compaction_state WHERE next_run_at<=now()
  ORDER BY next_run_at,device_id LIMIT 1 FOR UPDATE SKIP LOCKED;
 IF target IS NULL THEN RETURN; END IF;
 -- Persist only a scheduling lease before beginning destructive work. A worker
 -- timeout or termination leaves this backoff and all completed microbatches
 -- committed, so a slow device cannot monopolize subsequent runs.
 UPDATE container_topology_compaction_state SET next_run_at=now()+INTERVAL '1 minute' WHERE device_id=target;
 COMMIT;
 remaining_budget := batch_size;
 LOOP
  microbatch_size := LEAST(remaining_budget,configured_microbatch_size);
  end_run := false;
  BEGIN
   SELECT compact_container_topology(target,cutoff,microbatch_size) INTO microbatch_result;
   made_progress := made_progress
    OR COALESCE((microbatch_result->>'examined')::integer,0)>0
    OR COALESCE((microbatch_result->>'deleted')::integer,0)>0
    OR COALESCE((microbatch_result->>'pinned')::integer,0)>0;
  EXCEPTION WHEN lock_not_available THEN
   -- Live ingestion owns the lock: this is expected backpressure, not a failure.
   -- A device that already made progress stays due behind older work. A device
   -- that could not start keeps the longer backoff and cannot spin each second.
   UPDATE container_topology_compaction_state SET
    next_run_at=CASE WHEN made_progress THEN now() ELSE now()+INTERVAL '1 minute' END,
    deferrals=deferrals+1,last_deferred_at=now() WHERE device_id=target;
   end_run := true;
  WHEN OTHERS THEN
   -- The failed subtransaction releases all its locks and mutations.
   UPDATE container_topology_compaction_state SET next_run_at=now()+INTERVAL '1 minute',
    failures=failures+1,last_error=SQLERRM WHERE device_id=target;
   end_run := true;
  END;
  IF NOT end_run THEN
   remaining_budget := remaining_budget-microbatch_size;
   IF remaining_budget<=0 THEN
    end_run := true;
   ELSE
    -- Renew the cancellation-safe lease only if compact_container_topology left
    -- more due work. FOUND avoids a separate state query on every microbatch.
    UPDATE container_topology_compaction_state SET next_run_at=now()+INTERVAL '1 minute'
     WHERE device_id=target AND next_run_at<=now();
    IF NOT FOUND THEN
     end_run := true;
    END IF;
   END IF;
  END IF;
  -- This is the lock handoff. It commits progress and the renewed lease before
  -- releasing the organization row for ingestion.
  COMMIT;
  IF end_run THEN RETURN; END IF;
 END LOOP;
END;
$$;

ALTER TABLE container_topology_compaction_state
 DROP COLUMN last_runtime_yield_at,
 DROP COLUMN runtime_yields;

-- fleet-migration: 001071_internal_hosted_reporting_inputs.down.sql
LOCK TABLE hosted_reporting_authority,hosted_reporting_inputs,hosted_reporting_fact,hosted_reporting_fact_site IN ACCESS EXCLUSIVE MODE;
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM hosted_reporting_inputs) OR EXISTS(SELECT 1 FROM hosted_reporting_authority) THEN
  RAISE EXCEPTION 'preserve reporting inputs; restore a coordinated backup or fix forward';
 END IF;
END $$;
DROP TABLE hosted_reporting_fact_site;
DROP TABLE hosted_reporting_fact;
DROP TABLE hosted_reporting_inputs;
DROP TABLE hosted_reporting_authority;

-- fleet-migration: 001070_internal_activation_pool_handoff.down.sql
-- Keep the lock, refusal guard, and DROP atomic without relying on the runner
-- to supply an outer transaction. Completed rows also retain recovery evidence.
DO $$
BEGIN
 LOCK TABLE mining_group_pool_handoff IN ACCESS EXCLUSIVE MODE;
 IF EXISTS (SELECT 1 FROM mining_group_pool_handoff) THEN
  RAISE EXCEPTION 'retain activation pool handoff evidence; rollback requires a coordinated backup restore';
 END IF;
 DROP TABLE mining_group_pool_handoff;
END;
$$;

-- fleet-migration: 001069_internal_topology_cleanup_lock_handoff.down.sql
-- Restore one transaction per scheduled batch while keeping the job and its
-- operator-controlled configuration and scheduling state unchanged.
CREATE OR REPLACE PROCEDURE compact_container_topology_job(job_id integer,config jsonb)
LANGUAGE plpgsql AS $$
DECLARE
 target bigint;
 cutoff timestamptz := now()-INTERVAL '10 days';
 batch_size integer := LEAST(COALESCE((config->>'batch_size')::integer,50),50);
BEGIN
 IF NOT COALESCE((config->>'enabled')::boolean,false) THEN RETURN; END IF;
 SELECT device_id INTO target FROM container_topology_compaction_state WHERE next_run_at<=now()
  ORDER BY next_run_at,device_id LIMIT 1 FOR UPDATE SKIP LOCKED;
 IF target IS NULL THEN RETURN; END IF;
 UPDATE container_topology_compaction_state SET next_run_at=now()+INTERVAL '1 minute' WHERE device_id=target;
 COMMIT;
 BEGIN
  PERFORM compact_container_topology(target,cutoff,batch_size);
 EXCEPTION WHEN lock_not_available THEN
  UPDATE container_topology_compaction_state SET next_run_at=now()+INTERVAL '1 minute' WHERE device_id=target;
 WHEN OTHERS THEN
  UPDATE container_topology_compaction_state SET next_run_at=now()+INTERVAL '1 minute',failures=failures+1,last_error=SQLERRM WHERE device_id=target;
 END;
END;
$$;

ALTER TABLE container_topology_compaction_state
 DROP COLUMN last_deferred_at,
 DROP COLUMN deferrals;

-- fleet-migration: 001068_internal_pool_removal_bindings.down.sql
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM pool_configuration_operation o JOIN pool_configuration_operation_target t USING(operation_id) WHERE o.kind<>'apply' AND t.state IN ('pending','applying','uncertain')) THEN
  RAISE EXCEPTION 'resolve unfinished pool operations before rollback';
 END IF;
 IF EXISTS(SELECT 1 FROM device_write_recovery WHERE prior_status='complete') THEN
  RAISE EXCEPTION 'retain completed pool-removal recovery evidence; rollback requires a coordinated backup restore';
 END IF;
END $$;
ALTER TABLE device_write_recovery DROP CONSTRAINT device_write_recovery_prior_status_check;
ALTER TABLE device_write_recovery ADD CONSTRAINT device_write_recovery_prior_status_check CHECK (prior_status IN ('running','unknown'));
CREATE OR REPLACE FUNCTION validate_pool_update_attempt() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'UPDATE' AND OLD.physical_attempt_id IS NOT NULL AND
        NEW.physical_attempt_id IS DISTINCT FROM OLD.physical_attempt_id THEN
        RAISE EXCEPTION 'pool update attempt is immutable' USING ERRCODE = '23514';
    END IF;
    IF NEW.physical_attempt_id IS NOT NULL AND (TG_OP = 'INSERT' OR OLD.physical_attempt_id IS NULL) AND NOT EXISTS (
        SELECT 1 FROM device_write_attempt w
        JOIN pool_configuration_assignment a ON a.organization_id = NEW.organization_id
            AND a.target_kind = NEW.target_kind AND a.target_id = NEW.target_id
        WHERE w.attempt_id = NEW.physical_attempt_id AND w.organization_id = a.organization_id
            AND w.device_id = a.device_id AND w.operation_kind = 'mutation' AND w.status = 'running'
            AND a.operation_id = NEW.operation_id AND a.version = NEW.assignment_version AND a.retired_at IS NULL
            AND NEW.state = 'applying'
    ) THEN
        RAISE EXCEPTION 'pool update requires its current physical admission' USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;

-- fleet-migration: 001067_shared_pool_removal.down.sql
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM pool_configuration_operation WHERE kind<>'apply')
    OR EXISTS(SELECT 1 FROM pool_configuration_review WHERE kind='remove') THEN
  RAISE EXCEPTION 'retain pool operation history; rollback requires a coordinated backup restore';
 END IF;
END $$;
ALTER TABLE pool_configuration_operation_target DROP CONSTRAINT pool_configuration_operation_target_state_check;
ALTER TABLE pool_configuration_operation_target ADD CONSTRAINT pool_configuration_operation_target_state_check
    CHECK (state IN ('pending','applying','applied_unverified','verified','failed','different','superseded'));
ALTER TABLE pool_configuration_operation DROP COLUMN kind;
ALTER TABLE pool_configuration_review DROP CONSTRAINT pool_configuration_review_kind_check;
ALTER TABLE pool_configuration_review ADD CONSTRAINT pool_configuration_review_kind_check CHECK (kind IN ('edit','assign'));

-- fleet-migration: 001066_internal_errors_open_dedup_unique.down.sql
DROP INDEX IF EXISTS idx_errors_open_dedup;

-- fleet-migration: 001065_internal_curtailment_schedules.down.sql
DROP VIEW curtailment_automation_rule_binding;
CREATE VIEW curtailment_automation_rule_binding WITH (security_invoker = true) AS
SELECT r.*, rule_revision.response_profile_revision,
       src.source_name AS mqtt_source_name,
       profile.profile_name AS response_profile_name,
       profile.site_id AS response_profile_site_id,
       profile.scope_json AS response_profile_scope_json
FROM curtailment_automation_rule r
JOIN curtailment_automation_rule_profile_revision rule_revision
    ON rule_revision.automation_rule_id = r.id
JOIN curtailment_mqtt_source_config src
    ON src.id = r.mqtt_source_id
    AND src.organization_id = r.org_id
JOIN curtailment_response_profile profile
    ON profile.id = r.response_profile_id
    AND profile.org_id = r.org_id;

-- fleet-migration: 001064_shared_curtailment_schedules.down.sql
DROP TABLE curtailment_automation_schedule;
DROP INDEX idx_curtailment_event_schedule_window;
ALTER TABLE curtailment_event DROP COLUMN schedule_revision;

-- Schedule rules cannot be represented without a schedule; remove them before
-- restoring the MQTT-only constraints.
DELETE FROM curtailment_automation_execution WHERE mqtt_source_id IS NULL;
ALTER TABLE curtailment_automation_execution
    DROP COLUMN schedule_revision,
    ALTER COLUMN mqtt_source_id SET NOT NULL;

DELETE FROM curtailment_automation_rule WHERE trigger_type = 'SCHEDULE';
ALTER TABLE curtailment_automation_rule
    DROP CONSTRAINT ck_curtailment_automation_rule_trigger_type,
    ALTER COLUMN mqtt_source_id SET NOT NULL,
    ADD CONSTRAINT ck_curtailment_automation_rule_trigger_type CHECK ((trigger_type = 'MQTT'::text));

-- fleet-migration: 001063_internal_pool_review_authorization_checkpoint.down.sql
SELECT 1;

-- fleet-migration: 001062_shared_pool_review_authorization.down.sql
DROP TABLE pool_configuration_review_authorization;

-- fleet-migration: 001061_internal_curtailment_wave4_runtime.down.sql
DROP TRIGGER request_curtailment_rig_config_on_building_delete ON building;
DROP TRIGGER request_curtailment_rig_config_on_device_set_delete ON device_set;
DROP TRIGGER request_curtailment_rig_config_on_rack_placement_delete ON device_set_rack;
DROP TRIGGER request_curtailment_rig_config_on_rack_placement ON device_set_rack;
DROP FUNCTION request_curtailment_rig_config_on_placement_change();
DROP TRIGGER request_curtailment_rig_config_on_device_placement ON device;
DROP FUNCTION request_curtailment_rig_config_on_device_placement_change();
DROP TRIGGER request_curtailment_rig_config_on_membership_delete ON device_set_membership;
DROP TRIGGER request_curtailment_rig_config_on_membership_update ON device_set_membership;
DROP TRIGGER request_curtailment_rig_config_on_membership_insert ON device_set_membership;
DROP FUNCTION request_curtailment_rig_config_on_membership_change();
DROP FUNCTION request_curtailment_rig_config_for_devices(bigint, bigint[]);
DROP TRIGGER guard_curtailment_mqtt_source_disable ON curtailment_mqtt_source_config;
DROP FUNCTION guard_curtailment_mqtt_source_disable();
DROP TRIGGER request_curtailment_rig_config_on_rule_change ON curtailment_automation_rule;
DROP FUNCTION request_curtailment_rig_config_on_rule_change();
CREATE OR REPLACE FUNCTION request_curtailment_rig_config_reconciliation(p_org_id bigint, p_requested_by bigint, p_full boolean)
RETURNS bigint LANGUAGE sql VOLATILE SECURITY INVOKER AS $$
 INSERT INTO curtailment_rig_config_reconciliation AS reconciliation (organization_id, requested_by, full_reconcile_generation)
 VALUES (p_org_id, p_requested_by, CASE WHEN p_full THEN 1 ELSE 0 END)
 ON CONFLICT (organization_id) DO UPDATE SET requested_by = EXCLUDED.requested_by,
 desired_generation = reconciliation.desired_generation + 1,
 full_reconcile_generation = CASE WHEN p_full THEN reconciliation.desired_generation + 1 ELSE reconciliation.full_reconcile_generation END,
 retry_at = CURRENT_TIMESTAMP, last_error = NULL
 RETURNING reconciliation.desired_generation;
$$;

-- fleet-migration: 001060_shared_curtailment_wave4_runtime.down.sql
DROP TABLE curtailment_rig_config_delivery_attempt;
ALTER TABLE curtailment_rig_config_reconciliation DROP COLUMN delivery_attempts;
DROP TABLE curtailment_automation_execution;
DROP TABLE curtailment_automation_execution_grant;
ALTER TABLE curtailment_target DROP COLUMN curtail_possibly_sent;
ALTER TABLE curtailment_event DROP COLUMN dispatch_rejection_reason;

-- fleet-migration: 001059_internal_telemetry_device_site_lookup_index.down.sql
DROP INDEX idx_device_telemetry_site_lookup;

-- fleet-migration: 001058_internal_bound_topology_reference_planning.down.sql
-- requires-shared: 1000
CREATE OR REPLACE FUNCTION container_topology_references(ids bigint[])
RETURNS TABLE(observation_id bigint,reference_kind text) LANGUAGE sql STABLE AS $$
 WITH observations AS MATERIALIZED (
  SELECT o.id,o.device_id,o.collected_at,d.device_identifier FROM container_topology_observation o
  JOIN device d ON d.id=o.device_id WHERE o.id=ANY(ids)
 )
 SELECT DISTINCT m.topology_observation_id,'raw'::text FROM observations o
 JOIN LATERAL (SELECT topology_observation_id FROM device_metrics m
  WHERE m.time=o.collected_at AND m.device_identifier=o.device_identifier
   AND m.topology_observation_id=o.id LIMIT 1) m ON true
 UNION
 SELECT DISTINCT m.topology_observation_id,'raw'::text FROM observations o
 JOIN LATERAL (SELECT topology_observation_id FROM container_tank_metrics m
  WHERE m.time=o.collected_at AND m.topology_observation_id=o.id
   AND m.tank_id=ANY(ARRAY(SELECT t.id FROM container_tank t WHERE t.device_id=o.device_id))
  LIMIT 1) m ON true
 UNION
 SELECT DISTINCT topology_observation_id,'latest'::text FROM container_tank_latest_metrics WHERE topology_observation_id=ANY(ids)
 UNION
 SELECT DISTINCT topology_observation_id,'latest'::text FROM container_latest_metrics WHERE topology_observation_id=ANY(ids);
$$;

-- fleet-migration: 001057_internal_device_write_outcomes.down.sql
ALTER TABLE device_write_attempt DROP COLUMN write_outcome;

-- fleet-migration: 001056_internal_pool_configuration_cutover.down.sql
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM pool_configuration_cutover) OR EXISTS(SELECT 1 FROM pool_configuration_board_intent) THEN
  RAISE EXCEPTION 'pool cutover rollback requires a coordinated backup; retained execution intent cannot be discarded';
 END IF;
END $$;
DROP TRIGGER legacy_pool_command_cutover ON queue_message;
DROP FUNCTION reject_legacy_pool_command();
DROP TRIGGER legacy_board_intent_cutover ON hashboard_pool_intent;
DROP TRIGGER legacy_pool_profile_cutover ON hosted_account_pool_profile;
DROP TRIGGER legacy_pool_revision_cutover ON hosted_account_pool_revision;
DROP TRIGGER legacy_hosted_pool_cutover ON hosted_account_pool;
DROP TRIGGER legacy_pool_cutover ON pool;
DROP FUNCTION reject_legacy_pool_writer();
DROP TABLE pool_configuration_cutover;
DROP TABLE pool_configuration_board_intent;
ALTER TABLE pool_configuration_group_snapshot DROP CONSTRAINT uq_pool_group_snapshot_device;

-- fleet-migration: 001055_internal_hosted_reporting_evidence.down.sql
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

-- fleet-migration: 001054_internal_pool_equipment_integration.down.sql
-- Retained controller bindings must keep their immutable execution snapshots.
DO $$
BEGIN
    LOCK TABLE pool_configuration_group_snapshot, pool_configuration_operation_target IN ACCESS EXCLUSIVE MODE;
    IF EXISTS (SELECT 1 FROM pool_configuration_group_snapshot) THEN
        RAISE EXCEPTION 'cannot roll back shared board configurations with retained controller snapshots';
    END IF;

    ALTER TABLE device_write_attempt DROP COLUMN command_type;

    DROP INDEX idx_pool_configuration_operation_target_group_attempt;
    DROP INDEX idx_pool_configuration_operation_target_device_attempt;
    CREATE UNIQUE INDEX idx_pool_configuration_operation_target_attempt
     ON pool_configuration_operation_target(physical_attempt_id) WHERE physical_attempt_id IS NOT NULL;
    ALTER TABLE pool_configuration_operation_target DROP COLUMN group_snapshot_id;
    DROP TABLE pool_configuration_group_snapshot;

    DROP TRIGGER pool_assignment_adoption ON pool_configuration_assignment;
    DROP FUNCTION validate_pool_assignment_adoption();

END;
$$;

-- fleet-migration: 001053_shared_pool_equipment_state.down.sql
-- Imported intent must survive a rollback that cannot reconstruct its provenance.
DO $$
BEGIN
    LOCK TABLE pool_configuration_assignment, pool_configuration_observation IN ACCESS EXCLUSIVE MODE;
    IF EXISTS (SELECT 1 FROM pool_configuration_assignment WHERE operation_id IS NULL) THEN
        RAISE EXCEPTION 'cannot roll back pool adoption while imported assignments lack reviewed operations';
    END IF;
    DROP TABLE pool_configuration_observation;
    ALTER TABLE pool_configuration_assignment DROP CONSTRAINT chk_pool_configuration_assignment_origin;
    ALTER TABLE pool_configuration_assignment DROP COLUMN adopted_at;
    ALTER TABLE pool_configuration_assignment ALTER COLUMN operation_id SET NOT NULL;
END;
$$;

-- fleet-migration: 001052_internal_hashboard_allocation_consumers.down.sql
DROP TRIGGER hosted_allocation_pool_intent ON hosted_account_asset;
DROP FUNCTION retire_hosted_allocation_pool_intent();
DROP FUNCTION retire_reallocated_pool_assignments(bigint,bigint,integer,bigint);
CREATE OR REPLACE VIEW repair_ticket_asset_option AS
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

-- fleet-migration: 001051_internal_hashboard_allocation_order.down.sql
-- Restore the previous reconciliation function before removing its ordering data.
LOCK TABLE organization IN EXCLUSIVE MODE;
CREATE OR REPLACE FUNCTION record_hosted_account_asset_placement() RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE boundary TIMESTAMPTZ := clock_timestamp();
BEGIN
 IF NEW.deleted_at IS NOT NULL THEN
  UPDATE hosted_account_asset_interval SET ended_at=boundary WHERE device_id=NEW.id AND ended_at IS NULL;
  RETURN NEW;
 END IF;
 IF NEW.site_id IS NOT DISTINCT FROM OLD.site_id AND NEW.device_identifier=OLD.device_identifier THEN RETURN NEW; END IF;
 WITH closed AS (
  UPDATE hosted_account_asset_interval SET ended_at=boundary WHERE device_id=NEW.id AND ended_at IS NULL RETURNING *
 )
 INSERT INTO hosted_account_asset_interval(organization_id,kind,asset_id,device_id,client_id,subclient_id,started_at,site_id,device_identifier)
 SELECT organization_id,kind,asset_id,device_id,client_id,subclient_id,boundary,NEW.site_id,NEW.device_identifier FROM closed;
 RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION reconcile_hosted_hashboard_replacements(target_org bigint, controller bigint, observed timestamptz)
RETURNS bigint LANGUAGE plpgsql AS $$
DECLARE
 replacement record;
 previous_boundary text := COALESCE(current_setting('fleet.allocation_boundary',true),'');
 transferred bigint := 0;
BEGIN
 PERFORM id FROM organization WHERE id=target_org AND deleted_at IS NULL FOR NO KEY UPDATE;
 IF NOT FOUND THEN RETURN 0; END IF;

 FOR replacement IN
  SELECT a.asset_id AS old_board, current.hashboard_id AS new_board, current.module_id, current.position,
   a.client_id,a.subclient_id,root.account_id AS root_uuid,client.account_id AS client_uuid,
   COALESCE(child.account_id,client.account_id) AS owner_uuid
  FROM hashboard_installation current
  JOIN container_module module ON module.id=current.module_id AND module.retired_at IS NULL
   AND module.identity_confirmed AND module.last_seen_at=observed
  JOIN container_tank tank ON tank.id=module.tank_id AND tank.device_id=controller AND tank.present
  JOIN container_inventory inventory ON inventory.device_id=controller AND inventory.observed_at=observed
  JOIN device device ON device.id=controller AND device.org_id=target_org AND device.deleted_at IS NULL
   AND device.site_id IS NOT DISTINCT FROM current.site_id
  JOIN container_module_identity identity ON identity.module_id=module.id
   AND identity.org_id=target_org AND identity.reported_module_id=module.reported_id
  JOIN hashboard_slot slot ON slot.module_id=current.module_id AND slot.position=current.position
   AND slot.hashboard_id=current.hashboard_id AND slot.occupancy='occupied'
   AND slot.unavailable_reason='' AND slot.observed_at=observed
  JOIN hashboard_observation incoming ON incoming.hashboard_id=current.hashboard_id
   AND incoming.device_id=controller AND incoming.site_id IS NOT DISTINCT FROM current.site_id
   AND incoming.released_at IS NULL AND incoming.last_seen_at=observed
  CROSS JOIN LATERAL (
   SELECT p.* FROM hashboard_installation p
   WHERE p.module_id=current.module_id AND p.position=current.position
    AND (p.installed_at,p.id)<(current.installed_at,current.id)
   ORDER BY p.installed_at DESC,p.id DESC LIMIT 1
  ) prior
  JOIN hosted_account_asset a ON a.organization_id=target_org AND a.kind=7
   AND a.asset_id=prior.hashboard_id AND a.client_id IS NOT NULL AND a.device_id=prior.device_id
  JOIN hosted_account_asset_interval period ON period.organization_id=a.organization_id AND period.kind=7
   AND period.asset_id=a.asset_id AND period.ended_at IS NULL AND period.client_id=a.client_id
   AND period.subclient_id IS NOT DISTINCT FROM a.subclient_id AND period.started_at<=prior.removed_at
  JOIN hashboard_observation outgoing ON outgoing.hashboard_id=prior.hashboard_id
   AND outgoing.released_at IS NOT NULL
  JOIN hosted_account client ON client.id=a.client_id
  JOIN hosted_account root ON root.organization_id=target_org AND root.depth=0
  LEFT JOIN hosted_account child ON child.id=a.subclient_id
  LEFT JOIN hosted_account_asset destination ON destination.organization_id=target_org AND destination.kind=7
   AND destination.asset_id=current.hashboard_id
  WHERE current.device_id=controller AND current.removed_at IS NULL AND current.last_seen_at=observed
   AND prior.hashboard_id<>current.hashboard_id AND prior.removed_at<=current.installed_at
   -- Never take an allocated board, or override a manual decision made after
   -- the incoming board was installed. Recalled spare boards remain usable.
   AND destination.client_id IS NULL
   AND (destination.asset_id IS NULL OR destination.updated_at<=current.installed_at)
   -- The outgoing board must have disappeared from its last known installation,
   -- not moved to another slot or appeared elsewhere in this same controller map.
   AND NOT EXISTS (SELECT 1 FROM hashboard_installation later WHERE later.hashboard_id=prior.hashboard_id
    AND (later.removed_at IS NULL OR (later.installed_at,later.id)>(prior.installed_at,prior.id)))
   AND NOT EXISTS (
    SELECT 1 FROM hosted_account policy
    WHERE policy.organization_id=target_org AND policy.id IN (root.id,a.client_id,a.subclient_id)
     AND (policy.suspended
      OR (policy.sites_restricted AND NOT EXISTS (SELECT 1 FROM hosted_account_site allowed
       WHERE allowed.account_id=policy.id AND NOT allowed.parent_controlled AND allowed.site_id=current.site_id))
      OR (policy.site_ceiling_restricted AND NOT EXISTS (SELECT 1 FROM hosted_account_site allowed
       WHERE allowed.account_id=policy.id AND allowed.parent_controlled AND allowed.site_id=current.site_id)))
   )
 LOOP
  -- Both ownership periods meet at the accepted observation. A delayed
  -- confirmation starts ownership now; it never backfills earlier telemetry.
  PERFORM set_config('fleet.allocation_boundary',observed::text,true);
  UPDATE hosted_account_asset SET client_id=NULL,subclient_id=NULL,revision=revision+1,updated_at=clock_timestamp()
   WHERE organization_id=target_org AND kind=7 AND asset_id=replacement.old_board;
  INSERT INTO hosted_account_asset(organization_id,kind,asset_id,hashboard_id,device_id,client_id,subclient_id)
   VALUES(target_org,7,replacement.new_board,replacement.new_board,controller,replacement.client_id,replacement.subclient_id)
   ON CONFLICT(organization_id,kind,asset_id) DO UPDATE SET device_id=EXCLUDED.device_id,
    client_id=EXCLUDED.client_id,subclient_id=EXCLUDED.subclient_id,revision=hosted_account_asset.revision+1,updated_at=clock_timestamp();
  INSERT INTO activity_log(event_id,event_category,event_type,description,result,actor_type,organization_id,scope_type,scope_count,metadata)
   VALUES(gen_random_uuid(),'fleet_management','account_hashboard_replaced','Automatically replaced allocated hashboard','success','system',target_org,'account',1,
    jsonb_build_object('account_id',replacement.root_uuid,'child_account_id',replacement.client_uuid,
     'allocated_account_id',replacement.owner_uuid,'asset_count',1,'automatic',true,
     'old_hashboard_id',replacement.old_board,'replacement_hashboard_id',replacement.new_board,
     'module_id',replacement.module_id,'position',replacement.position,'observed_at',observed));
  transferred := transferred+1;
 END LOOP;
 PERFORM set_config('fleet.allocation_boundary',previous_boundary,true);
 RETURN transferred;
END;
$$;

DROP TRIGGER hashboard_installation_order ON hashboard_installation;
DROP FUNCTION record_hashboard_installation_order();
DROP TRIGGER hosted_hashboard_allocation_order ON hosted_account_asset;
DROP FUNCTION record_hosted_hashboard_allocation_order();
ALTER TABLE hashboard_installation DROP COLUMN installed_order, DROP COLUMN removed_order;
ALTER TABLE hosted_account_asset DROP COLUMN allocation_decision_order;
ALTER TABLE container_inventory DROP COLUMN hashboard_inventory_order, DROP COLUMN hashboard_inventory_complete;
DROP SEQUENCE hosted_hashboard_allocation_order;

-- fleet-migration: 001050_internal_automatic_hashboard_allocation.down.sql
-- Ownership and audit history remain valid when automatic detection is disabled.
DROP FUNCTION reconcile_hosted_hashboard_replacements(bigint,bigint,timestamptz);
DROP INDEX idx_hashboard_installation_slot_history;

-- fleet-migration: 001049_internal_hashboard_allocations.down.sql
DO $rollback$ BEGIN
 LOCK TABLE device,hosted_account_asset,hosted_account_asset_interval,hashboard_observation IN ACCESS EXCLUSIVE MODE;
 IF EXISTS (SELECT 1 FROM hosted_account_asset WHERE kind=7) OR EXISTS (SELECT 1 FROM hosted_account_asset_interval WHERE kind=7) THEN
  RAISE EXCEPTION 'hashboard allocations contain retained history; restore a coordinated backup to roll back';
 END IF;
DROP TRIGGER record_hosted_hashboard_history ON hashboard_metrics;
DROP TRIGGER record_hosted_hashboard_history ON hashboard_diagnostic_observation;
DROP FUNCTION record_hosted_hashboard_history_sample();
DROP INDEX idx_hosted_account_asset_interval_capture_hashboard;
DROP VIEW hosted_mining_asset;
DROP VIEW hosted_account_asset_projection;
DROP TRIGGER hosted_hashboard_placement ON hashboard_observation;
DROP FUNCTION move_hosted_hashboard_allocation();
DROP FUNCTION hosted_account_source_authorized(bigint,integer,bigint,bigint,bigint,bigint,text,timestamptz,timestamptz);
DROP VIEW hosted_module_membership;
ALTER TABLE hosted_account_asset DROP CONSTRAINT ck_hosted_account_asset_kind;
ALTER TABLE hosted_account_asset DROP CONSTRAINT ck_hosted_account_asset_identity;
ALTER TABLE hosted_account_asset DROP COLUMN hashboard_id;
ALTER TABLE hosted_account_asset ADD CONSTRAINT hosted_account_asset_kind_check CHECK (kind IN (5,6));
ALTER TABLE hosted_account_asset ADD CONSTRAINT hosted_account_asset_check CHECK (
 (kind=5 AND miner_id IS NOT NULL AND miner_id=asset_id AND module_id IS NULL AND device_id=asset_id) OR
 (kind=6 AND module_id IS NOT NULL AND module_id=asset_id AND miner_id IS NULL));
ALTER TABLE hosted_account_asset_interval DROP CONSTRAINT ck_hosted_account_asset_interval_kind;
ALTER TABLE hosted_account_asset_interval ADD CONSTRAINT hosted_account_asset_interval_kind_check CHECK (kind IN (5,6));
CREATE OR REPLACE FUNCTION check_hosted_account_asset() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.kind=6 AND NOT EXISTS (SELECT 1 FROM container_module m JOIN container_tank t ON t.id=m.tank_id WHERE m.id=NEW.module_id AND t.device_id=NEW.device_id) THEN
  RAISE EXCEPTION 'module controller mismatch' USING ERRCODE='23514';
 END IF;
 IF NEW.client_id IS NOT NULL AND NOT EXISTS (SELECT 1 FROM hosted_account a WHERE a.id=NEW.client_id AND a.organization_id=NEW.organization_id AND a.depth=1) THEN
  RAISE EXCEPTION 'allocation requires customer' USING ERRCODE='23514';
 END IF;
 IF NEW.subclient_id IS NOT NULL AND NOT EXISTS (SELECT 1 FROM hosted_account a WHERE a.id=NEW.subclient_id AND a.organization_id=NEW.organization_id AND a.parent_id=NEW.client_id AND a.depth=2) THEN
  RAISE EXCEPTION 'allocation requires direct subclient' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END;
$$;
CREATE OR REPLACE FUNCTION record_hosted_account_asset_interval() RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE boundary TIMESTAMPTZ := clock_timestamp();
BEGIN
 IF TG_OP='UPDATE' AND NEW.client_id IS NOT DISTINCT FROM OLD.client_id AND NEW.subclient_id IS NOT DISTINCT FROM OLD.subclient_id THEN RETURN NEW; END IF;
 UPDATE hosted_account_asset_interval SET ended_at=boundary
 WHERE organization_id=NEW.organization_id AND kind=NEW.kind AND asset_id=NEW.asset_id AND ended_at IS NULL;
 IF NEW.client_id IS NOT NULL THEN
  INSERT INTO hosted_account_asset_interval(organization_id,kind,asset_id,device_id,client_id,subclient_id,started_at,site_id,device_identifier)
  SELECT NEW.organization_id,NEW.kind,NEW.asset_id,NEW.device_id,NEW.client_id,NEW.subclient_id,boundary,d.site_id,d.device_identifier FROM device d WHERE d.id=NEW.device_id AND d.deleted_at IS NULL;
 END IF;
 RETURN NEW;
END;
$$;
CREATE FUNCTION hosted_account_source_authorized(
 organization BIGINT, asset_kind INTEGER, asset BIGINT, client BIGINT, subclient BIGINT,
 sample_site BIGINT, identifier TEXT, oldest TIMESTAMPTZ, newest TIMESTAMPTZ
) RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
 SELECT COALESCE(oldest IS NOT NULL AND newest IS NOT NULL AND newest>=oldest AND EXISTS (
  SELECT 1 FROM hosted_account_asset_interval i
  WHERE i.organization_id=organization AND i.kind=asset_kind AND i.asset_id=asset
   AND i.client_id=client AND COALESCE(i.subclient_id,0)=COALESCE(subclient,0)
   AND COALESCE(i.site_id,0)=COALESCE(sample_site,0) AND i.device_identifier=identifier
   AND oldest>=i.started_at AND (i.ended_at IS NULL OR newest<i.ended_at)
 ),false);
$$;
END;
$rollback$;

-- fleet-migration: 001048_internal_client_ticketing.down.sql
-- Keep the writer fence, refusal guard, and destructive DDL in one statement
-- so standalone execution cannot discard client context after a failed guard.
DO $$
BEGIN
 LOCK TABLE repair_ticket, repair_ticket_part, repair_ticket_comment,
  repair_ticket_target, repair_ticket_audience, container_module, hosted_account IN ACCESS EXCLUSIVE MODE;
 IF EXISTS (SELECT 1 FROM repair_ticket WHERE category>2 OR description<>''
   OR created_by_account_id IS NOT NULL OR shared_with_clients
   OR resolution_note<>'' OR resolution_note_account_id IS NOT NULL)
  OR EXISTS (SELECT 1 FROM repair_ticket_target)
  OR EXISTS (SELECT 1 FROM repair_ticket_audience)
  OR EXISTS (SELECT 1 FROM repair_ticket_comment WHERE NOT internal OR account_id IS NOT NULL)
  OR EXISTS (SELECT 1 FROM container_module WHERE model<>'')
  OR EXISTS (SELECT 1 FROM repair_ticket_part GROUP BY ticket_id,inventory_part_id HAVING count(*)>1) THEN
  RAISE EXCEPTION 'client ticketing has durable data; preserve it and fix forward';
 END IF;

 ALTER TABLE repair_ticket DROP COLUMN resolution_note_account_id;
 ALTER TABLE repair_ticket DROP COLUMN resolution_note;
 DROP VIEW repair_ticket_asset_option;
 ALTER TABLE repair_ticket_comment DROP CONSTRAINT ck_ticket_comment_internal_scope;
 ALTER TABLE repair_ticket_comment DROP CONSTRAINT fk_ticket_comment_audience;
 ALTER TABLE repair_ticket_comment DROP COLUMN account_id;
 DROP INDEX idx_repair_ticket_comment_client_page;
 ALTER TABLE repair_ticket_comment DROP COLUMN internal;
 DROP TABLE repair_ticket_target;
 DROP TABLE repair_ticket_audience;
 ALTER TABLE repair_ticket DROP CONSTRAINT fk_repair_ticket_creator_account;
 ALTER TABLE repair_ticket DROP COLUMN shared_with_clients;
 ALTER TABLE repair_ticket DROP COLUMN created_by_account_id;
 ALTER TABLE repair_ticket DROP COLUMN description;
 ALTER TABLE repair_ticket DROP CONSTRAINT ck_repair_ticket_category;
 ALTER TABLE repair_ticket ADD CONSTRAINT ck_repair_ticket_category CHECK(category BETWEEN 1 AND 2);
 ALTER TABLE container_module DROP COLUMN model;
 ALTER TABLE hosted_account DROP COLUMN ticketing_enabled;

 DROP INDEX idx_repair_ticket_part_active_inventory_part;
 ALTER TABLE repair_ticket_part ADD CONSTRAINT uq_ticket_inventory_part UNIQUE(ticket_id,inventory_part_id);
END;
$$;

-- fleet-migration: 001047_internal_pool_operation_bindings.down.sql
-- Reviewed identity cannot be reconstructed after dropping this column.
DO $$
BEGIN
    LOCK TABLE pool_configuration_operation_target IN ACCESS EXCLUSIVE MODE;
    IF EXISTS (SELECT 1 FROM pool_configuration_operation_target WHERE reviewed_binding <> '') THEN
        RAISE EXCEPTION 'cannot roll back with retained pool operation bindings';
    END IF;
    IF EXISTS (SELECT 1 FROM pool_configuration_operation_target WHERE physical_attempt_id IS NOT NULL) THEN
        RAISE EXCEPTION 'cannot roll back with retained pool update attempts';
    END IF;
    DROP TRIGGER pool_update_attempt ON pool_configuration_operation_target;
    DROP FUNCTION validate_pool_update_attempt();
    DROP INDEX idx_pool_configuration_operation_target_recovery;
    DROP INDEX idx_pool_configuration_operation_target_open;
    DROP INDEX idx_pool_configuration_operation_target_attempt;
    ALTER TABLE pool_configuration_operation_target DROP COLUMN physical_attempt_id;
    DROP TRIGGER pool_operation_target_identity ON pool_configuration_operation_target;
    DROP FUNCTION keep_pool_operation_target_identity();
    ALTER TABLE pool_configuration_operation_target DROP COLUMN reviewed_binding;
END;
$$;

-- fleet-migration: 001046_internal_pool_configuration_constraints.down.sql
DO $$
BEGIN
    LOCK TABLE pool_configuration, pool_configuration_assignment IN ACCESS EXCLUSIVE MODE;
    IF EXISTS (SELECT 1 FROM pool_configuration_assignment WHERE target_kind = 'hashboard_installation') THEN
        RAISE EXCEPTION 'cannot roll back with hashboard pool assignment history';
    END IF;
    DROP TRIGGER pool_configuration_assignment_installation ON pool_configuration_assignment;
    DROP FUNCTION validate_pool_assignment_installation();
    ALTER TABLE pool_configuration DROP CONSTRAINT fk_pool_configuration_hosted_owner;
END;
$$;

-- fleet-migration: 001045_shared_pool_configurations.down.sql
DO $$
BEGIN
    LOCK TABLE pool_configuration, pool_configuration_revision, pool_configuration_import,
        pool_configuration_assignment, pool_configuration_operation, pool_configuration_operation_target, pool_configuration_review IN ACCESS EXCLUSIVE MODE;
    IF EXISTS (SELECT 1 FROM pool_configuration) THEN
        RAISE EXCEPTION 'cannot roll back with pool configuration data';
    END IF;
    IF EXISTS (SELECT 1 FROM pool_configuration_assignment) OR EXISTS (SELECT 1 FROM pool_configuration_operation)
       OR EXISTS (SELECT 1 FROM pool_configuration_review WHERE saved_version IS NOT NULL) THEN
        RAISE EXCEPTION 'cannot roll back with pool assignment or operation history';
    END IF;
    DROP TABLE pool_configuration_review;
    DROP FUNCTION keep_pool_configuration_review_immutable();
    DROP TABLE pool_configuration_operation_target;
    DROP TABLE pool_configuration_assignment;
    DROP TABLE pool_configuration_operation;
    DROP FUNCTION advance_pool_configuration_assignments();
    DROP TABLE pool_configuration_import;
    ALTER TABLE pool_configuration DROP CONSTRAINT fk_pool_configuration_current_revision;
    DROP TABLE pool_configuration_revision;
    DROP TABLE pool_configuration;
    DROP FUNCTION keep_pool_configuration_revision_immutable();
    DROP FUNCTION keep_pool_configuration_owner_immutable();
    DROP FUNCTION valid_pool_configuration_slots(JSONB);
END;
$$;

-- fleet-migration: 001044_internal_hosted_history_summaries.down.sql
-- requires-shared: 1000
DROP TRIGGER record_hosted_account_history ON device_metrics;
DROP TRIGGER record_hosted_account_history ON miner_state_snapshots;
DROP TRIGGER record_hosted_account_history ON container_module_metrics;
DROP TRIGGER record_hosted_account_history ON container_module_diagnostic_observation;
DROP FUNCTION record_hosted_account_history_sample();
DROP FUNCTION hosted_history_metric_coverage(jsonb);
DROP FUNCTION hosted_module_uptime_state(text);
DROP FUNCTION hosted_account_history_watermarks(timestamptz);
DROP FUNCTION hosted_history_authorized_intervals(jsonb,bigint,boolean,bigint[],jsonb);
DROP MATERIALIZED VIEW hosted_account_history_daily;
DROP MATERIALIZED VIEW hosted_account_history_hourly;
DROP TABLE hosted_account_history_sample;
DROP FUNCTION hosted_history_metric_code(text);
DROP INDEX idx_hosted_account_asset_interval_capture_miner;
DROP INDEX idx_hosted_account_asset_interval_capture_module;
DROP TABLE hosted_account_history_summary_epoch;

-- fleet-migration: 001043_internal_curtailment_query_helpers.down.sql
DROP FUNCTION request_curtailment_rig_config_reconciliation(bigint, bigint, boolean);
DROP FUNCTION curtailment_automation_rule_has_live_event(bigint, bigint);
DROP VIEW curtailment_automation_rule_binding;

-- fleet-migration: 001042_internal_fleet_node_standby_ha.down.sql
-- Fence writers before checking operational state, and keep refusal and DDL
-- atomic even when the caller does not provide an outer transaction.
DO $$
BEGIN
 LOCK TABLE fleet_node_ha_pair, pending_enrollment IN ACCESS EXCLUSIVE MODE;
 IF EXISTS (SELECT 1 FROM fleet_node_ha_pair) THEN
  RAISE EXCEPTION 'remove Fleet Node HA pairs before rollback';
 END IF;
 IF EXISTS (SELECT 1 FROM pending_enrollment
            WHERE standby_for_node_id IS NOT NULL
              AND status IN ('PENDING', 'AWAITING_CONFIRMATION')
              AND expires_at > now()) THEN
  RAISE EXCEPTION 'cancel pending standby enrollments before rollback';
 END IF;
 DROP TABLE fleet_node_ha_pair;
 ALTER TABLE pending_enrollment DROP COLUMN standby_for_node_id;
END;
$$;

-- fleet-migration: 001041_internal_device_endpoints.down.sql
DROP TABLE container_ha_observation;
DROP INDEX idx_discovered_device_endpoint_key;
ALTER TABLE discovered_device DROP COLUMN endpoints, DROP COLUMN endpoint_key;

-- fleet-migration: 001040_internal_fleet_asset_node_liveness.down.sql
-- requires-shared: 1000
CREATE OR REPLACE VIEW fleet_mining_asset AS
SELECT 5::int AS kind, d.id, d.org_id, d.site_id,
       COALESCE(NULLIF(d.custom_name, ''), concat_ws(' ', dd.manufacturer, dd.model))::text AS name,
       d.device_identifier::text AS device_identifier, 0::int AS position, COALESCE(d.serial_number, '')::text AS reported_id,
       1::int AS facility_kind, COALESCE(d.building_id, rack.building_id) AS facility_id,
       3::int AS group_kind, rack.device_set_id AS group_id,
       CASE WHEN ds.status = 'OFFLINE' OR (ds.status IS NULL AND dp.pairing_status IN ('PAIRED','DEFAULT_PASSWORD')) OR (fn.last_seen_at < now() - INTERVAL '2 minutes' AND fn.enrollment_status = 'CONFIRMED' AND fn.deleted_at IS NULL) THEN 'offline'
            WHEN ds.status IN ('MAINTENANCE','INACTIVE') AND dp.pairing_status <> 'AUTHENTICATION_NEEDED' THEN lower(ds.status::text)
            WHEN dp.pairing_status='AUTHENTICATION_NEEDED' OR EXISTS (SELECT 1 FROM errors e WHERE e.device_id=d.id AND e.org_id=d.org_id AND e.closed_at IS NULL AND e.severity IN (1,2,3,4)) THEN 'error'
            ELSE COALESCE(lower(ds.status::text),'unknown') END::text AS state,
       dm.time AS observed_at, dm.hash_rate_hs AS hashrate_hs, dm.power_w, dm.temp_c AS temperature_c, (dm.efficiency_jh * 1e12)::double precision AS miner_efficiency_jth
FROM device d
JOIN discovered_device dd ON dd.id = d.discovered_device_id AND dd.org_id = d.org_id
JOIN device_pairing dp ON dp.device_id = d.id
LEFT JOIN device_status ds ON ds.device_id = d.id
LEFT JOIN fleet_node_device fnd ON fnd.device_id = d.id AND fnd.org_id = d.org_id
LEFT JOIN fleet_node fn ON fn.id = fnd.fleet_node_id
LEFT JOIN LATERAL (
  SELECT r.device_set_id, r.building_id
  FROM device_set_membership ms JOIN device_set s ON s.id = ms.device_set_id AND s.deleted_at IS NULL
  JOIN device_set_rack r ON r.device_set_id = s.id
  WHERE ms.device_id = d.id AND ms.device_set_type = 'rack' AND ms.org_id = d.org_id
) rack ON TRUE
LEFT JOIN LATERAL (
  SELECT time, hash_rate_hs, power_w, temp_c, efficiency_jh FROM device_metrics
  WHERE device_identifier = d.device_identifier AND time > now() - INTERVAL '10 minutes'
    AND time <= now() + INTERVAL '5 seconds'
    AND site_id IS NOT DISTINCT FROM d.site_id
    AND ds.status IS DISTINCT FROM 'OFFLINE'
    AND NOT COALESCE(fn.last_seen_at < now() - INTERVAL '2 minutes' AND fn.enrollment_status = 'CONFIRMED' AND fn.deleted_at IS NULL, FALSE)
  ORDER BY time DESC LIMIT 1
) dm ON TRUE
WHERE d.deleted_at IS NULL AND dd.deleted_at IS NULL AND dd.is_active AND dd.device_class <> 2
 AND dp.pairing_status IN ('PAIRED','AUTHENTICATION_NEEDED','DEFAULT_PASSWORD')
UNION ALL
SELECT 6::int, m.id, d.org_id, d.site_id,
       (COALESCE(NULLIF(d.custom_name, ''), 'Container ' || COALESCE(NULLIF(d.serial_number, ''), d.device_identifier)) || ' T' || t.position || ' M' || m.position)::text,
       d.device_identifier::text, m.position, m.reported_id,
       2::int, d.id, 4::int, t.id,
       CASE WHEN m.state_observed_at > now() - INTERVAL '2 minutes' AND m.state_observed_at <= now() + INTERVAL '5 seconds'
              AND m.last_seen_at > now() - INTERVAL '2 minutes' AND m.observation_site_id IS NOT DISTINCT FROM d.site_id THEN m.state ELSE 'unknown' END::text,
       mm.time, mm.hashrate_hs, mm.power_w, mm.temperature_c, (mm.efficiency_jh * 1e12)::double precision
FROM container_module m JOIN container_tank t ON t.id = m.tank_id
JOIN device d ON d.id = t.device_id
JOIN discovered_device dd ON dd.id = d.discovered_device_id AND dd.org_id = d.org_id
LEFT JOIN container_equipment_current_metrics mm ON mm.kind=6 AND mm.id = m.id
  AND mm.time > now() - INTERVAL '2 minutes' AND mm.time <= now() + INTERVAL '5 seconds'
  AND m.last_seen_at > now() - INTERVAL '2 minutes' AND m.state <> 'offline'
WHERE m.retired_at IS NULL AND t.present AND d.deleted_at IS NULL AND dd.deleted_at IS NULL;

-- fleet-migration: 001039_internal_mining_groups.down.sql
-- Lock before inspecting state so a concurrent writer cannot commit a journal
-- between the refusal guard and DROP. Keep the guard and DDL in one statement.
DO $$
BEGIN
 LOCK TABLE container_mining_group_state, hashboard_pool_intent, mining_group_request IN ACCESS EXCLUSIVE MODE;
 IF EXISTS (SELECT 1 FROM container_mining_group_state
            WHERE status='failed' OR (transfer_failure_count>0 AND status IN ('pending','applying','blocked'))) THEN
  RAISE EXCEPTION 'mining group transfer recovery state must be resolved before rollback';
 END IF;
 IF EXISTS(SELECT 1 FROM container_mining_group_state WHERE status IN ('pending','applying','blocked')) THEN
  RAISE EXCEPTION 'finish mining group operations before rollback';
 END IF;
 IF EXISTS(SELECT 1 FROM container_mining_group_state WHERE bindings <> '{}'::jsonb)
    OR EXISTS(SELECT 1 FROM hashboard_pool_intent) THEN
  RAISE EXCEPTION 'preserve or remove mining group bindings and intents before rollback';
 END IF;
 DROP TABLE mining_group_request;
 DROP TABLE hashboard_pool_intent;
 DROP TABLE container_mining_group_state;
 DELETE FROM role_permission WHERE permission_id IN (SELECT id FROM permission WHERE key='miner:activate_mining_groups');
 DELETE FROM permission WHERE key='miner:activate_mining_groups';
END;
$$;

-- fleet-migration: 001038_internal_module_psu_identity.down.sql
-- A position-keyed runtime cannot safely interpret retained PSU movement history.
DO $$ BEGIN
 IF EXISTS (SELECT 1 FROM container_module_placement WHERE ended_at IS NOT NULL) THEN
  RAISE EXCEPTION 'module placement history exists; preserve evidence and fix forward';
 END IF;
END $$;
DROP TRIGGER container_module_site_readings ON device;
DROP FUNCTION withdraw_container_module_site_readings();
DROP TRIGGER container_module_placement ON container_module;
DROP FUNCTION record_container_module_placement();
DROP TRIGGER hashboard_installation_placement ON hashboard_installation;
DROP FUNCTION capture_hashboard_installation_placement();
ALTER TABLE hashboard_installation DROP COLUMN device_id,DROP COLUMN tank_id,DROP COLUMN module_position;
DROP TABLE container_module_placement;
DROP TRIGGER container_module_placement_org ON container_module;
DROP FUNCTION check_container_module_placement_org();
DROP TABLE container_module_identity;
DROP FUNCTION check_container_module_identity_org();

-- fleet-migration: 001037_internal_diagnostic_observations.down.sql
-- requires-shared: 1000
DROP TABLE container_module_diagnostic_observation;
DROP TABLE hashboard_diagnostic_observation;

ALTER TABLE container_module DROP CONSTRAINT ck_container_module_state;
UPDATE container_module SET state=CASE state WHEN 'active' THEN 'hashing' WHEN 'inactive' THEN 'sleeping' WHEN 'offline' THEN 'offline' WHEN 'error' THEN 'attention' ELSE 'unknown' END;
ALTER TABLE container_module ADD CONSTRAINT container_module_state_check CHECK(state IN ('unknown','hashing','attention','offline','sleeping','isolated'));
UPDATE hashboard_slot SET state=CASE state WHEN 'active' THEN 'hashing' WHEN 'inactive' THEN 'sleeping' WHEN 'offline' THEN 'offline' WHEN 'error' THEN 'attention' ELSE 'unknown' END;
UPDATE hashboard_latest_metrics SET state=CASE state WHEN 'active' THEN 'hashing' WHEN 'inactive' THEN 'sleeping' WHEN 'offline' THEN 'offline' WHEN 'error' THEN 'attention' ELSE 'unknown' END;

CREATE OR REPLACE VIEW fleet_mining_asset AS
SELECT 5::int AS kind, d.id, d.org_id, d.site_id,
       COALESCE(NULLIF(d.custom_name, ''), concat_ws(' ', dd.manufacturer, dd.model))::text AS name,
       d.device_identifier::text AS device_identifier, 0::int AS position, COALESCE(d.serial_number, '')::text AS reported_id,
       1::int AS facility_kind, COALESCE(d.building_id, rack.building_id) AS facility_id,
       3::int AS group_kind, rack.device_set_id AS group_id,
       CASE WHEN ds.status = 'OFFLINE' OR (ds.status IS NULL AND dp.pairing_status IN ('PAIRED','DEFAULT_PASSWORD')) OR (fn.last_seen_at < now() - INTERVAL '2 minutes' AND fn.enrollment_status = 'CONFIRMED' AND fn.deleted_at IS NULL) THEN 'offline'
            WHEN ds.status IN ('MAINTENANCE', 'INACTIVE') AND dp.pairing_status <> 'AUTHENTICATION_NEEDED' THEN 'sleeping'
            WHEN ds.status IN ('ERROR', 'NEEDS_MINING_POOL', 'UPDATING', 'REBOOT_REQUIRED') OR dp.pairing_status='AUTHENTICATION_NEEDED' OR EXISTS (SELECT 1 FROM errors e WHERE e.device_id=d.id AND e.org_id=d.org_id AND e.closed_at IS NULL AND e.severity IN (1,2,3,4)) THEN 'attention'
            WHEN ds.status = 'ACTIVE' THEN 'hashing' ELSE 'unknown' END::text AS state,
       dm.time AS observed_at, dm.hash_rate_hs AS hashrate_hs, dm.power_w, dm.temp_c AS temperature_c, (dm.efficiency_jh * 1e12)::double precision AS miner_efficiency_jth
FROM device d
JOIN discovered_device dd ON dd.id = d.discovered_device_id AND dd.org_id = d.org_id
JOIN device_pairing dp ON dp.device_id = d.id
LEFT JOIN device_status ds ON ds.device_id = d.id
LEFT JOIN fleet_node_device fnd ON fnd.device_id = d.id AND fnd.org_id = d.org_id
LEFT JOIN fleet_node fn ON fn.id = fnd.fleet_node_id
LEFT JOIN LATERAL (
  SELECT r.device_set_id, r.building_id
  FROM device_set_membership ms JOIN device_set s ON s.id = ms.device_set_id AND s.deleted_at IS NULL
  JOIN device_set_rack r ON r.device_set_id = s.id
  WHERE ms.device_id = d.id AND ms.device_set_type = 'rack' AND ms.org_id = d.org_id
) rack ON TRUE
LEFT JOIN LATERAL (
  SELECT time, hash_rate_hs, power_w, temp_c, efficiency_jh FROM device_metrics
  WHERE device_identifier = d.device_identifier AND time > now() - INTERVAL '10 minutes'
    AND time <= now() + INTERVAL '5 seconds'
    AND site_id IS NOT DISTINCT FROM d.site_id
    AND ds.status IS DISTINCT FROM 'OFFLINE'
    AND NOT COALESCE(fn.last_seen_at < now() - INTERVAL '2 minutes' AND fn.enrollment_status = 'CONFIRMED' AND fn.deleted_at IS NULL, FALSE)
  ORDER BY time DESC LIMIT 1
) dm ON TRUE
WHERE d.deleted_at IS NULL AND dd.deleted_at IS NULL AND dd.is_active AND dd.device_class <> 2
 AND dp.pairing_status IN ('PAIRED','AUTHENTICATION_NEEDED','DEFAULT_PASSWORD')
UNION ALL
SELECT 6::int, m.id, d.org_id, d.site_id,
       (COALESCE(NULLIF(d.custom_name, ''), 'Container ' || COALESCE(NULLIF(d.serial_number, ''), d.device_identifier)) || ' T' || t.position || ' M' || m.position)::text,
       d.device_identifier::text, m.position, m.reported_id,
       2::int, d.id, 4::int, t.id,
       CASE WHEN m.state_observed_at > now() - INTERVAL '2 minutes' AND m.state_observed_at <= now() + INTERVAL '5 seconds'
              AND m.last_seen_at > now() - INTERVAL '2 minutes' THEN m.state ELSE 'unknown' END::text,
       mm.time, mm.hashrate_hs, mm.power_w, mm.temperature_c, (mm.efficiency_jh * 1e12)::double precision
FROM container_module m JOIN container_tank t ON t.id = m.tank_id
JOIN device d ON d.id = t.device_id
JOIN discovered_device dd ON dd.id = d.discovered_device_id AND dd.org_id = d.org_id
LEFT JOIN container_equipment_current_metrics mm ON mm.kind=6 AND mm.id = m.id
  AND mm.time > now() - INTERVAL '2 minutes' AND mm.time <= now() + INTERVAL '5 seconds'
  AND m.last_seen_at > now() - INTERVAL '2 minutes' AND m.state <> 'offline'
WHERE m.retired_at IS NULL AND t.present AND d.deleted_at IS NULL AND dd.deleted_at IS NULL;

ALTER TABLE container_module DROP COLUMN attribution_since;
ALTER TABLE container_module DROP COLUMN observation_site_id;

-- fleet-migration: 001036_internal_team_invitation.down.sql
DROP TABLE organization_invitation;
DROP FUNCTION team_invitation_authority_fingerprint(BIGINT, BIGINT, BIGINT);
DROP TRIGGER role_permission_revision ON role_permission;
DROP FUNCTION bump_role_permission_revision();
ALTER TABLE role DROP COLUMN permission_revision;

-- fleet-migration: 001035_internal_topology_cleanup_throughput.down.sql
-- Restore the original fixed cadence without changing enablement or progress.
SELECT alter_job(job_id, schedule_interval => INTERVAL '10 seconds', fixed_schedule => true)
FROM timescaledb_information.jobs
WHERE proc_schema = 'public' AND proc_name = 'compact_container_topology_job';

-- fleet-migration: 001034_internal_invitation_rate_limit.down.sql
DROP TABLE hosted_invitation_rate_limit;

-- fleet-migration: 001033_internal_hosted_history_compression.down.sql
-- requires-shared: 1000
-- Keep collection bounds usable on compressed module history without assuming
-- that the partition timestamp and independent contributor windows agree.
ALTER TABLE container_module_metrics SET (
 timescaledb.compress_orderby = 'time DESC'
);
-- Changing the hypertable setting affects new chunks. Apply it to retained
-- compressed chunks too. Rehearse recompression/storage in the upgrade window.
DO $$
DECLARE chunk regclass;
BEGIN
 FOR chunk IN
  SELECT format('%I.%I',chunk_schema,chunk_name)::regclass
  FROM timescaledb_information.chunks
  WHERE hypertable_schema='public' AND hypertable_name='container_module_metrics' AND is_compressed
 LOOP
  PERFORM compress_chunk(chunk,if_not_compressed=>true,recompress=>true);
 END LOOP;
END;
$$;

-- fleet-migration: 001032_internal_hosted_history_collection_index.down.sql
DROP INDEX idx_container_module_metrics_module_collected_at;

-- fleet-migration: 001031_internal_hosted_allocation_history.down.sql
-- Allocation periods cannot be reconstructed after rollback. Preserve both
-- active and recalled periods; use a coordinated backup or fix forward.
DO $$
BEGIN
 LOCK TABLE device,hosted_account_asset,hosted_account_asset_interval IN ACCESS EXCLUSIVE MODE;
 IF EXISTS (SELECT 1 FROM hosted_account_asset_interval) THEN
  RAISE EXCEPTION 'preserve established allocation intervals; restore a coordinated backup or fix forward instead of rolling back hosted account history';
 END IF;
 DROP TRIGGER hosted_account_asset_placement ON device;
 DROP FUNCTION record_hosted_account_asset_placement();
 DROP TRIGGER hosted_account_asset_history ON hosted_account_asset;
 DROP FUNCTION record_hosted_account_asset_interval();
 DROP FUNCTION hosted_account_source_authorized(BIGINT,INTEGER,BIGINT,BIGINT,BIGINT,BIGINT,TEXT,TIMESTAMPTZ,TIMESTAMPTZ);
 DROP TABLE hosted_account_asset_interval;
 DROP TABLE hosted_account_history_epoch;
END;
$$;

-- fleet-migration: 001030_internal_telemetry_summary_recovery.down.sql
-- Keep coverage and deletion fences: returning to independent raw retention is
-- unsafe. Older binaries reject this checkpoint; preserve the database and fix
-- forward with a release that includes this migration.
DO $$ BEGIN
 RAISE EXCEPTION 'telemetry recovery protects retained evidence; downgrade is unsupported: preserve the database and fix forward with a release that includes this migration';
END $$;

-- fleet-migration: 001029_internal_bound_topology_reference_checks.down.sql
CREATE OR REPLACE FUNCTION container_topology_references(ids bigint[])
RETURNS TABLE(observation_id bigint,reference_kind text) LANGUAGE sql STABLE AS $$
 -- Ingestion stamps device_metrics only at the observation's collection time
 -- (StampContainerSample). Constrain time and device for chunk/segment pruning.
 WITH observations AS MATERIALIZED (
  SELECT o.id,o.device_id,o.collected_at,d.device_identifier FROM container_topology_observation o
  JOIN device d ON d.id=o.device_id WHERE o.id=ANY(ids)
 )
 SELECT DISTINCT m.topology_observation_id,'raw'::text FROM observations o
 JOIN LATERAL (SELECT topology_observation_id FROM device_metrics m
  WHERE m.time=o.collected_at AND m.device_identifier=o.device_identifier
   AND m.topology_observation_id=o.id LIMIT 1) m ON true
 UNION
 -- Tank rollups use the same collection time as their observation. Include
 -- historical tanks too: retired equipment can still have retained telemetry.
 SELECT DISTINCT m.topology_observation_id,'raw'::text FROM observations o
 JOIN container_tank t ON t.device_id=o.device_id
 JOIN LATERAL (SELECT topology_observation_id FROM container_tank_metrics m
  WHERE m.time=o.collected_at AND m.tank_id=t.id
   AND m.topology_observation_id=o.id LIMIT 1) m ON true
 UNION
 SELECT DISTINCT topology_observation_id,'latest'::text FROM container_tank_latest_metrics WHERE topology_observation_id=ANY(ids)
 UNION
 SELECT DISTINCT topology_observation_id,'latest'::text FROM container_latest_metrics WHERE topology_observation_id=ANY(ids);
$$;

-- fleet-migration: 001028_internal_hosted_account_assets.down.sql
-- Preserve allocation history and its stale-write witnesses on manual rollback.
DO $$
BEGIN
 LOCK TABLE hosted_account_asset IN ACCESS EXCLUSIVE MODE;
 IF EXISTS (SELECT 1 FROM hosted_account_asset) THEN
  RAISE EXCEPTION 'preserve reporting allocation data and recalled-row revisions; restore a coordinated backup or fix forward instead of rolling back hosted account assets';
 END IF;
 DROP TABLE hosted_account_asset;
 DROP FUNCTION check_hosted_account_asset();
END;
$$;

-- fleet-migration: 001027_internal_container_telemetry_compression.down.sql
-- Disabling compression requires decompressing retained chunks first. Rollback
-- can need substantial disk space and time; normally prefer a forward fix.
DROP FUNCTION stamp_hashboard_topology(bigint,timestamptz,jsonb);

SELECT remove_compression_policy('container_plant_metrics');
SELECT decompress_chunk(c, if_compressed => true) FROM show_chunks('container_plant_metrics') c;
ALTER TABLE container_plant_metrics SET (timescaledb.compress = false);
SELECT set_chunk_time_interval('container_plant_metrics', INTERVAL '7 days');

SELECT remove_compression_policy('container_tank_metrics');
SELECT decompress_chunk(c, if_compressed => true) FROM show_chunks('container_tank_metrics') c;
ALTER TABLE container_tank_metrics SET (timescaledb.compress = false);
SELECT set_chunk_time_interval('container_tank_metrics', INTERVAL '1 day');
ALTER TABLE container_tank_metrics
 DROP CONSTRAINT fk_container_tank_metrics_observation_time,
 ADD CONSTRAINT container_tank_metrics_topology_observation_id_fkey
 FOREIGN KEY(topology_observation_id) REFERENCES container_topology_observation(id);
ALTER TABLE container_topology_observation
 DROP CONSTRAINT uq_container_topology_observation_id_collected_at;

SELECT remove_compression_policy('container_module_metrics');
SELECT decompress_chunk(c, if_compressed => true) FROM show_chunks('container_module_metrics') c;
ALTER TABLE container_module_metrics SET (timescaledb.compress = false);
SELECT set_chunk_time_interval('container_module_metrics', INTERVAL '7 days');

SELECT remove_compression_policy('hashboard_metrics');
SELECT decompress_chunk(c, if_compressed => true) FROM show_chunks('hashboard_metrics') c;
ALTER TABLE hashboard_metrics SET (timescaledb.compress = false);
SELECT set_chunk_time_interval('hashboard_metrics', INTERVAL '1 day');

CREATE OR REPLACE FUNCTION container_topology_references(ids bigint[])
RETURNS TABLE(observation_id bigint,reference_kind text) LANGUAGE sql STABLE AS $$
 -- Ingestion stamps device_metrics only at the observation's collection time
 -- (StampContainerSample). Constrain time and device for chunk/segment pruning.
 WITH observations AS MATERIALIZED (
  SELECT o.id,o.collected_at,d.device_identifier FROM container_topology_observation o
  JOIN device d ON d.id=o.device_id WHERE o.id=ANY(ids)
 )
 SELECT DISTINCT m.topology_observation_id,'raw'::text FROM observations o
 JOIN LATERAL (SELECT topology_observation_id FROM device_metrics m
  WHERE m.time=o.collected_at AND m.device_identifier=o.device_identifier
   AND m.topology_observation_id=o.id LIMIT 1) m ON true
 UNION
 SELECT DISTINCT topology_observation_id,'raw'::text FROM container_tank_metrics WHERE topology_observation_id=ANY(ids)
 UNION
 SELECT DISTINCT topology_observation_id,'latest'::text FROM container_tank_latest_metrics WHERE topology_observation_id=ANY(ids)
 UNION
 SELECT DISTINCT topology_observation_id,'latest'::text FROM container_latest_metrics WHERE topology_observation_id=ANY(ids);
$$;

CREATE OR REPLACE PROCEDURE compact_container_topology_job(job_id integer,config jsonb)
LANGUAGE plpgsql AS $$
DECLARE
 target bigint;
 cutoff timestamptz := now()-INTERVAL '10 days';
 batch_size integer := COALESCE((config->>'batch_size')::integer,500);
BEGIN
 IF NOT COALESCE((config->>'enabled')::boolean,false) THEN RETURN; END IF;
 SELECT device_id INTO target FROM container_topology_compaction_state WHERE next_run_at<=now()
  ORDER BY next_run_at,device_id LIMIT 1 FOR UPDATE SKIP LOCKED;
 IF target IS NULL THEN RETURN; END IF;
 -- Persist only a scheduling lease before beginning destructive work. A worker
 -- timeout or termination rolls back the entire batch but not this backoff,
 -- so a slow device cannot monopolize subsequent runs. CALL outside a transaction.
 UPDATE container_topology_compaction_state SET next_run_at=now()+INTERVAL '1 minute' WHERE device_id=target;
 COMMIT;
 BEGIN
  PERFORM compact_container_topology(target,cutoff,batch_size);
 EXCEPTION WHEN lock_not_available THEN
  -- Live ingestion owns the lock: this is expected backpressure, not a failure.
  UPDATE container_topology_compaction_state SET next_run_at=now()+INTERVAL '1 minute' WHERE device_id=target;
 WHEN OTHERS THEN
  -- The failed subtransaction releases all its locks and mutations.
  UPDATE container_topology_compaction_state SET next_run_at=now()+INTERVAL '1 minute',failures=failures+1,last_error=SQLERRM WHERE device_id=target;
 END;
END;
$$;

-- fleet-migration: 001026_internal_fleet_node_details.down.sql
DROP INDEX idx_pending_enrollment_confirmed_node;
ALTER TABLE pending_enrollment DROP COLUMN confirmed_by;
ALTER TABLE fleet_node_heartbeat DROP COLUMN version, DROP COLUMN ip_addresses;

-- fleet-migration: 001025_internal_container_site_scope.down.sql
DROP TRIGGER container_class_placement ON discovered_device;
DROP FUNCTION enforce_container_class_placement();
DROP TRIGGER container_rack_placement ON device_set_membership;
DROP FUNCTION enforce_container_rack_placement();
DROP TRIGGER container_device_placement ON device;
DROP FUNCTION enforce_container_device_placement();
DROP VIEW container_scope;
ALTER TABLE container_inventory DROP CONSTRAINT fk_container_inventory_device_org;
ALTER TABLE container_inventory DROP COLUMN org_id;

-- fleet-migration: 001024_internal_device_reboot_required_after.down.sql
ALTER TABLE device_status DROP COLUMN reboot_required_after;

-- fleet-migration: 001023_internal_hashboard_observation.down.sql
DROP TABLE hashboard_observation;

-- fleet-migration: 001022_internal_topology_compaction.down.sql
-- Hold the evidence check and DDL under the same migration transaction.
LOCK TABLE container_topology_observation IN ACCESS EXCLUSIVE MODE;
-- Dropping confirmation spans after deleting their receipts loses evidence.
DO $$ BEGIN
 IF EXISTS (SELECT 1 FROM container_topology_observation WHERE last_confirmed_at>collected_at) THEN
  RAISE EXCEPTION 'topology has been compacted; disable cleanup instead of dropping confirmation evidence';
 END IF;
END $$;
SELECT delete_job(job_id) FROM timescaledb_information.jobs WHERE proc_name='compact_container_topology_job';
DROP PROCEDURE compact_container_topology_job(integer,jsonb);
DROP FUNCTION compact_container_topology(bigint,timestamptz,integer);
DROP FUNCTION container_topology_candidates(bigint,timestamptz,integer);
DROP FUNCTION container_topology_references(bigint[]);
DROP TRIGGER register_container_topology_compaction ON container_topology_observation;
DROP FUNCTION register_container_topology_compaction();
DROP TABLE container_topology_compaction_pending;
DROP TABLE container_topology_compaction_state;
DROP INDEX idx_device_metrics_topology_observation;
DROP INDEX idx_container_tank_metrics_topology_observation;
DROP INDEX idx_container_tank_latest_metrics_topology_observation;
DROP INDEX idx_container_latest_metrics_topology_observation;
ALTER TABLE container_topology_observation DROP COLUMN last_confirmed_at;

-- fleet-migration: 001021_internal_hashboard_metric_summaries.down.sql
-- Serialize the evidence check with ingestion and summary refresh.
LOCK TABLE hashboard_metrics IN ACCESS EXCLUSIVE MODE;
-- Summaries may be the only remaining board evidence. Preserve them and fix
-- forward; an application rollback does not require dropping this schema.
DO $$ BEGIN
 IF EXISTS (SELECT 1 FROM hashboard_metrics_hourly) OR EXISTS (SELECT 1 FROM hashboard_metrics_daily) THEN
  RAISE EXCEPTION 'hashboard summaries contain retained evidence; disable jobs or roll back the application instead';
 END IF;
END $$;
SELECT delete_job(job_id) FROM timescaledb_information.jobs WHERE proc_name='initialize_hashboard_metric_summaries';
DROP PROCEDURE initialize_hashboard_metric_summaries(integer,jsonb);
DROP VIEW hashboard_metric_summaries;
DROP MATERIALIZED VIEW hashboard_metrics_daily;
DROP MATERIALIZED VIEW hashboard_metrics_hourly;
ALTER TABLE hashboard_metrics DROP COLUMN topology_version_id;
SELECT remove_retention_policy('hashboard_metrics',if_exists=>true);
SELECT add_retention_policy('hashboard_metrics',INTERVAL '30 days');

-- fleet-migration: 001019_internal_container_module_identity_confirmation.down.sql
ALTER TABLE container_module DROP COLUMN identity_confirmed;

-- fleet-migration: 001018_internal_hosted_curtailment_control.down.sql
-- Older servers cannot honor retained ownership-independent curtailment work.
LOCK TABLE device_write_attempt, queue_message IN ACCESS EXCLUSIVE MODE;
DO $$ BEGIN
 IF EXISTS (SELECT 1 FROM device_write_attempt WHERE curtailment_command IS NOT NULL)
 OR EXISTS (SELECT 1 FROM queue_message WHERE status IN ('PENDING','PROCESSING')
  AND command_type IN ('Curtail','Uncurtail','ApplyCurtailmentConfig')) THEN
  RAISE EXCEPTION 'fleet curtailment has durable state; rollback is unsafe';
 END IF;
END $$;
ALTER TABLE device_write_attempt DROP COLUMN curtailment_command;

-- fleet-migration: 001016_internal_ct1_metric_summaries.down.sql
-- requires-shared: 1000
SELECT delete_job(job_id) FROM timescaledb_information.jobs WHERE proc_name='initialize_container_metric_summaries';
DROP PROCEDURE initialize_container_metric_summaries(integer,jsonb);
DROP VIEW container_equipment_history;
DROP FUNCTION container_history_resolution(timestamptz,timestamptz);
DROP MATERIALIZED VIEW container_module_metrics_daily;
DROP MATERIALIZED VIEW container_module_metrics_hourly;
DROP MATERIALIZED VIEW container_tank_metrics_daily;
DROP MATERIALIZED VIEW container_tank_metrics_hourly;
DROP MATERIALIZED VIEW container_metrics_daily;
DROP MATERIALIZED VIEW container_metrics_hourly;
ALTER TABLE container_module_metrics DROP COLUMN topology_version_id, DROP COLUMN site_id;
ALTER TABLE container_tank_metrics DROP COLUMN topology_version_id;
ALTER TABLE device_metrics DROP COLUMN topology_version_id;
SELECT remove_retention_policy('device_metrics',if_exists=>true);
SELECT add_retention_policy('device_metrics',INTERVAL '10 days');
SELECT remove_retention_policy('container_tank_metrics',if_exists=>true);
SELECT add_retention_policy('container_tank_metrics',INTERVAL '30 days');
SELECT remove_retention_policy('container_module_metrics',if_exists=>true);
SELECT add_retention_policy('container_module_metrics',INTERVAL '30 days');

-- fleet-migration: 001015_internal_ct1_poll_rollups.down.sql
CREATE OR REPLACE VIEW fleet_mining_asset AS
SELECT 5::int AS kind, d.id, d.org_id, d.site_id,
       COALESCE(NULLIF(d.custom_name, ''), concat_ws(' ', dd.manufacturer, dd.model))::text AS name,
       d.device_identifier::text AS device_identifier, 0::int AS position, COALESCE(d.serial_number, '')::text AS reported_id,
       1::int AS facility_kind, COALESCE(d.building_id, rack.building_id) AS facility_id,
       3::int AS group_kind, rack.device_set_id AS group_id,
       CASE WHEN ds.status = 'OFFLINE' OR (ds.status IS NULL AND dp.pairing_status IN ('PAIRED','DEFAULT_PASSWORD')) OR (fn.last_seen_at < now() - INTERVAL '2 minutes' AND fn.enrollment_status = 'CONFIRMED' AND fn.deleted_at IS NULL) THEN 'offline'
            WHEN ds.status IN ('MAINTENANCE', 'INACTIVE') AND dp.pairing_status <> 'AUTHENTICATION_NEEDED' THEN 'sleeping'
            WHEN ds.status IN ('ERROR', 'NEEDS_MINING_POOL', 'UPDATING', 'REBOOT_REQUIRED') OR dp.pairing_status='AUTHENTICATION_NEEDED' OR EXISTS (SELECT 1 FROM errors e WHERE e.device_id=d.id AND e.org_id=d.org_id AND e.closed_at IS NULL AND e.severity IN (1,2,3,4)) THEN 'attention'
            WHEN ds.status = 'ACTIVE' THEN 'hashing' ELSE 'unknown' END::text AS state,
       dm.time AS observed_at, dm.hash_rate_hs AS hashrate_hs, dm.power_w, dm.temp_c AS temperature_c, (dm.efficiency_jh * 1e12)::double precision AS miner_efficiency_jth
FROM device d
JOIN discovered_device dd ON dd.id = d.discovered_device_id AND dd.org_id = d.org_id
JOIN device_pairing dp ON dp.device_id = d.id
LEFT JOIN device_status ds ON ds.device_id = d.id
LEFT JOIN fleet_node_device fnd ON fnd.device_id = d.id AND fnd.org_id = d.org_id
LEFT JOIN fleet_node fn ON fn.id = fnd.fleet_node_id
LEFT JOIN LATERAL (
  SELECT r.device_set_id, r.building_id
  FROM device_set_membership ms JOIN device_set s ON s.id = ms.device_set_id AND s.deleted_at IS NULL
  JOIN device_set_rack r ON r.device_set_id = s.id
  WHERE ms.device_id = d.id AND ms.device_set_type = 'rack' AND ms.org_id = d.org_id
) rack ON TRUE
LEFT JOIN LATERAL (
  SELECT time, hash_rate_hs, power_w, temp_c, efficiency_jh FROM device_metrics
  WHERE device_identifier = d.device_identifier AND time > now() - INTERVAL '10 minutes'
    AND time <= now() + INTERVAL '5 seconds'
    AND ds.status IS DISTINCT FROM 'OFFLINE'
    AND NOT COALESCE(fn.last_seen_at < now() - INTERVAL '2 minutes' AND fn.enrollment_status = 'CONFIRMED' AND fn.deleted_at IS NULL, FALSE)
  ORDER BY time DESC LIMIT 1
) dm ON TRUE
WHERE d.deleted_at IS NULL AND dd.deleted_at IS NULL AND dd.is_active AND dd.device_class <> 2
 AND dp.pairing_status IN ('PAIRED','AUTHENTICATION_NEEDED','DEFAULT_PASSWORD')
UNION ALL
SELECT 6::int, m.id, d.org_id, d.site_id,
       (COALESCE(NULLIF(d.custom_name, ''), 'Container ' || COALESCE(NULLIF(d.serial_number, ''), d.device_identifier)) || ' T' || t.position || ' M' || m.position)::text,
       d.device_identifier::text, m.position, m.reported_id,
       2::int, d.id, 4::int, t.id,
       CASE WHEN m.state_observed_at > now() - INTERVAL '2 minutes' AND m.state_observed_at <= now() + INTERVAL '5 seconds'
              AND m.last_seen_at > now() - INTERVAL '2 minutes' THEN m.state ELSE 'unknown' END::text,
       mm.time, mm.hashrate_hs, mm.power_w, mm.temperature_c, NULL::double precision
FROM container_module m JOIN container_tank t ON t.id = m.tank_id
JOIN device d ON d.id = t.device_id
JOIN discovered_device dd ON dd.id = d.discovered_device_id AND dd.org_id = d.org_id
LEFT JOIN container_module_latest_metrics mm ON mm.module_id = m.id
  AND mm.time > now() - INTERVAL '2 minutes' AND mm.time <= now() + INTERVAL '5 seconds'
  AND m.last_seen_at > now() - INTERVAL '2 minutes' AND m.state <> 'offline'
WHERE m.retired_at IS NULL AND t.present AND d.deleted_at IS NULL AND dd.deleted_at IS NULL;
DROP VIEW container_equipment_current_metrics;
DROP TABLE container_latest_metrics;
ALTER TABLE device_metrics DROP COLUMN topology_observation_id, DROP COLUMN coverage, DROP COLUMN metric_semantics;
DROP TABLE container_tank_latest_metrics;
DROP TABLE container_tank_metrics;
DROP TABLE container_topology_observation;
DROP TABLE container_topology_version;
ALTER TABLE container_module_latest_metrics DROP COLUMN source_times, DROP COLUMN coverage, DROP COLUMN efficiency_jh;
ALTER TABLE container_module_metrics DROP COLUMN coverage, DROP COLUMN efficiency_jh;
ALTER TABLE hashboard_latest_metrics DROP COLUMN power_w, DROP COLUMN efficiency_jh;
ALTER TABLE hashboard_metrics DROP COLUMN power_w, DROP COLUMN efficiency_jh;

-- fleet-migration: 001014_internal_discovery_identity.down.sql
ALTER TABLE discovered_device
    DROP COLUMN mac_address,
    DROP COLUMN serial_number;

-- fleet-migration: 001013_internal_fleet_node_heartbeat.down.sql
UPDATE fleet_node fn SET last_seen_at=h.last_seen_at
 FROM fleet_node_heartbeat h WHERE h.fleet_node_id=fn.id AND h.org_id=fn.org_id;
-- Restore the original asset dependency before dropping the live-node view.
CREATE OR REPLACE VIEW fleet_mining_asset AS
SELECT 5::int AS kind, d.id, d.org_id, d.site_id,
       COALESCE(NULLIF(d.custom_name, ''), concat_ws(' ', dd.manufacturer, dd.model))::text AS name,
       d.device_identifier::text AS device_identifier, 0::int AS position, COALESCE(d.serial_number, '')::text AS reported_id,
       1::int AS facility_kind, COALESCE(d.building_id, rack.building_id) AS facility_id,
       3::int AS group_kind, rack.device_set_id AS group_id,
       CASE WHEN ds.status = 'OFFLINE' OR (ds.status IS NULL AND dp.pairing_status IN ('PAIRED','DEFAULT_PASSWORD')) OR (fn.last_seen_at < now() - INTERVAL '2 minutes' AND fn.enrollment_status = 'CONFIRMED' AND fn.deleted_at IS NULL) THEN 'offline'
            WHEN ds.status IN ('MAINTENANCE', 'INACTIVE') AND dp.pairing_status <> 'AUTHENTICATION_NEEDED' THEN 'sleeping'
            WHEN ds.status IN ('ERROR', 'NEEDS_MINING_POOL', 'UPDATING', 'REBOOT_REQUIRED') OR dp.pairing_status='AUTHENTICATION_NEEDED' OR EXISTS (SELECT 1 FROM errors e WHERE e.device_id=d.id AND e.org_id=d.org_id AND e.closed_at IS NULL AND e.severity IN (1,2,3,4)) THEN 'attention'
            WHEN ds.status = 'ACTIVE' THEN 'hashing' ELSE 'unknown' END::text AS state,
       dm.time AS observed_at, dm.hash_rate_hs AS hashrate_hs, dm.power_w, dm.temp_c AS temperature_c, (dm.efficiency_jh * 1e12)::double precision AS miner_efficiency_jth
FROM device d
JOIN discovered_device dd ON dd.id = d.discovered_device_id AND dd.org_id = d.org_id
JOIN device_pairing dp ON dp.device_id = d.id
LEFT JOIN device_status ds ON ds.device_id = d.id
LEFT JOIN fleet_node_device fnd ON fnd.device_id = d.id AND fnd.org_id = d.org_id
LEFT JOIN fleet_node fn ON fn.id = fnd.fleet_node_id
LEFT JOIN LATERAL (
  SELECT r.device_set_id, r.building_id
  FROM device_set_membership ms JOIN device_set s ON s.id = ms.device_set_id AND s.deleted_at IS NULL
  JOIN device_set_rack r ON r.device_set_id = s.id
  WHERE ms.device_id = d.id AND ms.device_set_type = 'rack' AND ms.org_id = d.org_id
) rack ON TRUE
LEFT JOIN LATERAL (
  SELECT time, hash_rate_hs, power_w, temp_c, efficiency_jh FROM device_metrics
  WHERE device_identifier = d.device_identifier AND time > now() - INTERVAL '10 minutes'
    AND time <= now() + INTERVAL '5 seconds'
    AND ds.status IS DISTINCT FROM 'OFFLINE'
    AND NOT COALESCE(fn.last_seen_at < now() - INTERVAL '2 minutes' AND fn.enrollment_status = 'CONFIRMED' AND fn.deleted_at IS NULL, FALSE)
  ORDER BY time DESC LIMIT 1
) dm ON TRUE
WHERE d.deleted_at IS NULL AND dd.deleted_at IS NULL AND dd.is_active AND dd.device_class <> 2
 AND dp.pairing_status IN ('PAIRED','AUTHENTICATION_NEEDED','DEFAULT_PASSWORD')
UNION ALL
SELECT 6::int, m.id, d.org_id, d.site_id,
       (COALESCE(NULLIF(d.custom_name, ''), 'Container ' || COALESCE(NULLIF(d.serial_number, ''), d.device_identifier)) || ' T' || t.position || ' M' || m.position)::text,
       d.device_identifier::text, m.position, m.reported_id,
       2::int, d.id, 4::int, t.id,
       CASE WHEN m.state_observed_at > now() - INTERVAL '2 minutes' AND m.state_observed_at <= now() + INTERVAL '5 seconds'
              AND m.last_seen_at > now() - INTERVAL '2 minutes' THEN m.state ELSE 'unknown' END::text,
       mm.time, mm.hashrate_hs, mm.power_w, mm.temperature_c, NULL::double precision
FROM container_module m JOIN container_tank t ON t.id = m.tank_id
JOIN device d ON d.id = t.device_id
JOIN discovered_device dd ON dd.id = d.discovered_device_id AND dd.org_id = d.org_id
LEFT JOIN container_module_latest_metrics mm ON mm.module_id = m.id
  AND mm.time > now() - INTERVAL '2 minutes' AND mm.time <= now() + INTERVAL '5 seconds'
  AND m.last_seen_at > now() - INTERVAL '2 minutes' AND m.state <> 'offline'
WHERE m.retired_at IS NULL AND t.present AND d.deleted_at IS NULL AND dd.deleted_at IS NULL;

DROP VIEW fleet_node_live;
DROP TABLE fleet_node_heartbeat;

-- fleet-migration: 001012_internal_retired_device_write_permits.down.sql
LOCK TABLE device_deletion_unpair_permit,device_write_attempt IN ACCESS EXCLUSIVE MODE;
DO $$ BEGIN
 IF EXISTS (SELECT 1 FROM device_deletion_unpair_permit)
 OR EXISTS (SELECT 1 FROM device_write_attempt WHERE status<>'complete') THEN
  RAISE EXCEPTION 'retired device write evidence exists; rollback is unsafe';
 END IF;
END $$;
DROP TABLE device_deletion_unpair_permit;
DROP INDEX idx_device_write_attempt_outstanding_identity_keys;
ALTER TABLE device_write_attempt DROP COLUMN identity_keys;
DROP FUNCTION device_write_identity_keys(bigint);
DROP FUNCTION device_write_identity_keys_values(bigint,text,text,text,text,text);

-- fleet-migration: 001011_internal_direct_device_write_attempts.down.sql
-- Do not erase direct-operation evidence when downgrading the runtime.
LOCK TABLE device_write_attempt IN ACCESS EXCLUSIVE MODE;
DO $$ BEGIN
 IF EXISTS (SELECT 1 FROM device_write_attempt WHERE queue_message_id IS NULL) THEN
  RAISE EXCEPTION 'direct device write evidence exists; rollback is unsafe';
 END IF;
END $$;
ALTER TABLE device_write_attempt ALTER COLUMN queue_message_id SET NOT NULL;

-- fleet-migration: 001010_internal_device_rpc_endpoint.down.sql
ALTER TABLE discovered_device DROP COLUMN rpc_tls, DROP COLUMN rpc_port;

-- fleet-migration: 001009_internal_queue_pending_order_index.down.sql
DROP INDEX idx_queue_message_pending_created_id;

-- fleet-migration: 001008_internal_hosted_device_write_fences.down.sql
-- An epoch or unresolved attempt is safety evidence; never erase it on rollback.
LOCK TABLE device_write_fence, device_write_attempt, queue_message IN ACCESS EXCLUSIVE MODE;
DO $$ BEGIN
 IF EXISTS (SELECT 1 FROM device_write_attempt) THEN
  RAISE EXCEPTION 'device write attempt history exists; preserve physical outcome evidence; rollback is unsafe';
 END IF;
 IF EXISTS (SELECT 1 FROM device_write_fence WHERE epoch<>0 OR state<>'host') THEN
  RAISE EXCEPTION 'device write fences have advanced epochs or non-host ownership; preserve ownership evidence; rollback is unsafe';
 END IF;
 IF EXISTS (SELECT 1 FROM queue_message WHERE write_epoch<>0) THEN
  RAISE EXCEPTION 'queued device writes retain nonzero epochs; preserve queued ownership evidence; rollback is unsafe';
 END IF;
END $$;
DROP TABLE device_write_recovery;
DROP TABLE device_write_attempt;
ALTER TABLE queue_message DROP COLUMN write_epoch;
DROP TABLE device_write_fence;

-- fleet-migration: 001007_internal_hosted_account_pools.down.sql
-- Keep the refusal guard and destructive DDL in one statement so standalone
-- execution cannot continue dropping data after the guard raises an exception.
DO $$
BEGIN
    LOCK TABLE hosted_account_pool, hosted_account_pool_revision, hosted_account_pool_profile IN ACCESS EXCLUSIVE MODE;
    IF EXISTS (SELECT 1 FROM hosted_account_pool)
       OR EXISTS (SELECT 1 FROM hosted_account_pool_revision)
       OR EXISTS (SELECT 1 FROM hosted_account_pool_profile) THEN
        RAISE EXCEPTION 'preserve or remove pool configuration data before rolling back hosted account pools';
    END IF;

    DROP TABLE hosted_account_pool_profile;
    DROP TABLE hosted_account_pool_revision;
    DROP FUNCTION keep_hosted_account_pool_revision_immutable();
    DROP FUNCTION valid_hosted_account_pool_slots(JSONB);
    DROP TABLE hosted_account_pool;
END;
$$;

-- fleet-migration: 001006_internal_hosted_account_keys.down.sql
-- Keep the guard, lock, and conversion in one statement so this also fails
-- atomically when run by psql without an enclosing transaction.
DO $$
BEGIN
    -- Exclude key creators before taking the live-key snapshot; retain this
    -- lock through conversion so no account credential enters the old schema.
    LOCK TABLE api_key IN ACCESS EXCLUSIVE MODE;

    -- Never turn a live customer credential into an organization credential.
    IF EXISTS (SELECT 1 FROM api_key WHERE subject_kind='account' AND revoked_at IS NULL) THEN
        RAISE EXCEPTION 'revoke all account API keys before rolling back account key support';
    END IF;

    DROP TABLE hosted_account_key_descendant;
    DROP FUNCTION check_hosted_account_key_descendant();
    DROP TABLE hosted_account_key;
    DROP FUNCTION check_hosted_account_key();
    ALTER TABLE api_key DROP CONSTRAINT uq_api_key_org_id;
    ALTER TABLE api_key DROP CONSTRAINT ck_api_key_subject;
    UPDATE api_key SET subject_kind='user' WHERE subject_kind='account';
    ALTER TABLE api_key ADD CONSTRAINT ck_api_key_subject CHECK (
        (subject_kind = 'user' AND user_id IS NOT NULL AND fleet_node_id IS NULL) OR
        (subject_kind = 'fleet_node' AND user_id IS NULL AND fleet_node_id IS NOT NULL)
    );
END;
$$;

-- fleet-migration: 001005_internal_hosted_account_administration.down.sql
-- Fence new organizations before taking the global rollback snapshot. Existing
-- account writers use the organization row serializer; retain all row locks
-- through the checks and destructive DDL in this migration transaction.
LOCK TABLE organization IN SHARE ROW EXCLUSIVE MODE;
-- Match the writer lock mode so concurrent foreign-key KEY SHARE checks can
-- finish while rollback waits for another organization's writer.
SELECT id FROM organization ORDER BY id FOR NO KEY UPDATE;

-- Check before any destructive rollback steps. Older servers cannot represent
-- customer-only affiliations; leave all data intact until those affiliations
-- have an explicitly assigned organization role.
DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM user_organization WHERE role_id IS NULL) THEN
        RAISE EXCEPTION 'cannot roll back hosted administration while customer-only affiliations exist';
    END IF;
    -- Foundation servers cannot resolve ancestor grants. Require a live direct
    -- administrator before removing those grants; leave recovery explicit.
    IF EXISTS (
        SELECT 1 FROM hosted_account a
        JOIN organization o ON o.id=a.organization_id AND o.deleted_at IS NULL
        JOIN hosted_account root ON root.organization_id=a.organization_id AND root.depth=0
        LEFT JOIN hosted_account parent ON parent.id=a.parent_id AND parent.organization_id=a.organization_id
        WHERE a.depth>0 AND NOT EXISTS (
            SELECT 1 FROM hosted_account_membership m
            JOIN "user" u ON u.id=m.user_id AND u.deleted_at IS NULL
            JOIN user_organization uo ON uo.user_id=u.id AND uo.organization_id=m.organization_id AND uo.deleted_at IS NULL
            WHERE m.organization_id=a.organization_id AND m.account_id=a.id
            AND m.role='account_administrator' AND m.revoked_at IS NULL
            AND (a.capabilities & COALESCE(parent.capabilities,31) & root.capabilities & 9)=9
        )
    ) THEN
        RAISE EXCEPTION 'cannot roll back hosted administration until every customer account has a live direct administrator';
    END IF;
END $$;
DROP TABLE hosted_account_invitation;
DROP TABLE hosted_account_ancestor_grant;
DROP FUNCTION check_hosted_account_ancestor_grant();
DROP TABLE hosted_account_site;
ALTER TABLE hosted_account DROP COLUMN sites_restricted;
ALTER TABLE hosted_account DROP COLUMN site_ceiling_restricted;
ALTER TABLE hosted_account DROP COLUMN authority_revision;
ALTER TABLE user_organization ALTER COLUMN role_id SET NOT NULL;

-- fleet-migration: 001004_internal_hashboards.down.sql
DROP TABLE hashboard_latest_metrics;
DROP TABLE hashboard_metrics;
DROP TABLE hashboard_slot;
DROP TABLE hashboard_installation;
DROP TABLE hashboard_identity;
DROP TABLE hashboard;

-- fleet-migration: 001003_internal_hosted_accounts.down.sql
DROP TRIGGER organization_hosted_account_root ON organization;
DROP FUNCTION create_hosted_account_root();
DROP TABLE hosted_account_membership;
DROP TABLE hosted_account;
DROP FUNCTION check_hosted_account_hierarchy();
DELETE FROM role_permission WHERE permission_id IN (SELECT id FROM permission WHERE key = 'account:manage');
DELETE FROM permission WHERE key = 'account:manage';

-- fleet-migration: 001002_internal_container_module_issues.down.sql
ALTER TABLE container_module
    DROP COLUMN issues,
    DROP COLUMN issues_observed_at,
    DROP COLUMN issues_source_times;

-- fleet-migration: 001001_internal_baseline.down.sql
-- Baseline adoption is forward-only. Restore a coordinated backup to return
-- to an earlier application release; never discard application or private data.
DO $$
BEGIN
    RAISE EXCEPTION 'baseline migration cannot be reverted; restore the coordinated backup';
END
$$;

*/
