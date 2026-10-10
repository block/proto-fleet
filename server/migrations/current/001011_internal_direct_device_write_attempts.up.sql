-- requires-shared: 1000

-- Refuse unsupported intermediate hosted runtimes before advancing beyond
-- checkpoint 1010 or changing queue-claim nullability. Keep unexpected attempt evidence.
LOCK TABLE device_write_attempt IN ACCESS EXCLUSIVE MODE;
DO $$ BEGIN
 IF EXISTS (SELECT 1 FROM device_write_attempt) THEN
  RAISE EXCEPTION 'device write history exists; intermediate hosted schema upgrades are unsupported';
 END IF;
END $$;

-- Direct SDK and proxy attempts have no queue claim.
ALTER TABLE device_write_attempt ALTER COLUMN queue_message_id DROP NOT NULL;
