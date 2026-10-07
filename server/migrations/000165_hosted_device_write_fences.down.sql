-- An epoch or unresolved attempt is safety evidence; never erase it on rollback.
LOCK TABLE device_write_fence, device_write_attempt, queue_message IN ACCESS EXCLUSIVE MODE;
DO $$ BEGIN
 IF EXISTS (SELECT 1 FROM device_write_attempt)
    OR EXISTS (SELECT 1 FROM device_write_fence WHERE epoch<>0 OR state<>'host')
    OR EXISTS (SELECT 1 FROM queue_message WHERE write_epoch<>0) THEN
  RAISE EXCEPTION 'device write fencing has durable state; rollback is unsafe';
 END IF;
END $$;
DROP TABLE device_write_attempt;
ALTER TABLE queue_message DROP COLUMN write_epoch;
DROP TABLE device_write_fence;
