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
DROP TABLE device_write_attempt;
ALTER TABLE queue_message DROP COLUMN write_epoch;
DROP TABLE device_write_fence;
