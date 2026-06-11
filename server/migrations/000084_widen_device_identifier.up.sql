-- Widen device.device_identifier to match discovered_device (VARCHAR(255)). Fleet-node
-- pairing inserts a device keyed by the discovered identifier (a synthesized
-- "serial:<...>" can reach 255 chars), which overflowed the old VARCHAR(36) and failed
-- the persist after the node had already paired the miner. Lengthening doesn't rewrite.
ALTER TABLE device ALTER COLUMN device_identifier TYPE VARCHAR(255);
