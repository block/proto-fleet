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
