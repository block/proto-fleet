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
