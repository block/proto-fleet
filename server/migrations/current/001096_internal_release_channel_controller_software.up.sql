-- requires-shared: 1074

-- A release channel's target controller software version for one container
-- model. It is a target only: Fleet reports which containers are behind it and
-- holds their hashboard firmware updates, and never starts a controller
-- software update from it. Clearing the target deletes the row.
CREATE TABLE release_channel_controller_software (
    channel_id   BIGINT NOT NULL REFERENCES release_channel (id) ON DELETE CASCADE,
    manufacturer TEXT NOT NULL,
    model        TEXT NOT NULL,
    version      TEXT NOT NULL CHECK (version <> ''),
    assigned_by  BIGINT NOT NULL,
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (channel_id, manufacturer, model)
);

-- One target per (channel, manufacturer, model) under the same folding as
-- firmware assignments, so observed spellings of a pair share it.
CREATE UNIQUE INDEX idx_release_channel_controller_software_pair
    ON release_channel_controller_software (channel_id, release_channel_pair_key(manufacturer), release_channel_pair_key(model));

-- Controller software versions are compared without surrounding whitespace or
-- a leading "v", so "v1.4.0" from a controller matches the package's "1.4.0".
CREATE FUNCTION release_channel_controller_software_key(version text)
RETURNS text LANGUAGE sql IMMUTABLE PARALLEL SAFE AS $$
    SELECT regexp_replace(btrim(COALESCE(version, '')), '^[vV]', '');
$$;
