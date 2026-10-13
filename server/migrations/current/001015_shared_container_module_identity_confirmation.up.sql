-- Historical reported_id identifies the hardware whose history belongs to this
-- row; only a fresh identity-bearing observation confirms it for commands.
ALTER TABLE container_module ADD COLUMN identity_confirmed boolean NOT NULL DEFAULT false;
