-- requires-shared: 1000

-- Direct SDK and proxy attempts have no queue claim.
ALTER TABLE device_write_attempt ALTER COLUMN queue_message_id DROP NOT NULL;
