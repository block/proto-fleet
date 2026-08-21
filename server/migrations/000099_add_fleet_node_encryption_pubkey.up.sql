ALTER TABLE fleet_node
    ADD COLUMN encryption_pubkey BYTEA NOT NULL,
    ADD CONSTRAINT ck_fleet_node_encryption_pubkey_len
        CHECK (length(encryption_pubkey) = 32);
