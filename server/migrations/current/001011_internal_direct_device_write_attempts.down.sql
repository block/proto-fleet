-- Do not erase direct-operation evidence when downgrading the runtime.
LOCK TABLE device_write_attempt IN ACCESS EXCLUSIVE MODE;
DO $$ BEGIN
 IF EXISTS (SELECT 1 FROM device_write_attempt WHERE queue_message_id IS NULL) THEN
  RAISE EXCEPTION 'direct device write evidence exists; rollback is unsafe';
 END IF;
END $$;
ALTER TABLE device_write_attempt ALTER COLUMN queue_message_id SET NOT NULL;
