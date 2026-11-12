-- requires-shared: 1074
-- A NULL name uses the plugin default; an empty name explicitly omits the suffix.
ALTER TABLE hashboard
    ADD COLUMN worker_name TEXT,
    ADD COLUMN worker_name_mode TEXT NOT NULL DEFAULT 'none'
        CONSTRAINT hashboard_worker_name_mode_check CHECK (worker_name_mode IN ('none', 'mac_address', 'custom'));
