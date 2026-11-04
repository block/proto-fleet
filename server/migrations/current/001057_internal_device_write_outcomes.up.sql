ALTER TABLE device_write_attempt
 ADD COLUMN write_outcome JSONB NOT NULL DEFAULT '{}'::jsonb
 CHECK (jsonb_typeof(write_outcome) = 'object');
