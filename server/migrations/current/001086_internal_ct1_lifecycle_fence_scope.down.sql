-- requires-shared: 1000
CREATE OR REPLACE FUNCTION guard_pool_lifecycle_mining_group_deactivation() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 PERFORM 1 FROM device_write_fence WHERE device_id=NEW.device_id AND organization_id=NEW.organization_id FOR UPDATE;
 IF NEW.state='pending' AND EXISTS (SELECT 1 FROM container_mining_group_state WHERE device_id=NEW.device_id
  AND organization_id=NEW.organization_id AND operation_kind='deactivate' AND status IN ('pending','applying','blocked')) THEN
  NEW.state := 'deferred';
  NEW.result_code := 'mining_groups_deactivation';
 END IF;
 RETURN NEW;
END $$;
