ALTER TABLE site
    ADD COLUMN address     TEXT,
    ADD COLUMN postal_code TEXT,
    ADD COLUMN country     TEXT NOT NULL DEFAULT 'US',
    ADD COLUMN notes       TEXT;
