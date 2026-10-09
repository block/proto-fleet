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
