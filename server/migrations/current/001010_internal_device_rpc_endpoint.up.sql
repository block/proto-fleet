-- requires-shared: 1000
ALTER TABLE discovered_device
  ADD COLUMN rpc_port integer NOT NULL DEFAULT 0 CHECK (rpc_port BETWEEN 0 AND 65535),
  ADD COLUMN rpc_tls boolean NOT NULL DEFAULT FALSE;
