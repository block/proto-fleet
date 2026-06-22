ALTER TABLE organization
    DROP COLUMN IF EXISTS miner_auth_private_key;

ALTER TABLE fleet_node
    DROP COLUMN IF EXISTS miner_signing_pubkey;
