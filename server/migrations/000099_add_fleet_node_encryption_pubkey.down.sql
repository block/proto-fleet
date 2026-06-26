ALTER TABLE fleet_node
    DROP CONSTRAINT IF EXISTS ck_fleet_node_encryption_pubkey_len,
    DROP COLUMN IF EXISTS encryption_pubkey;
