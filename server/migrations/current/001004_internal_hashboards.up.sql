-- requires-shared: 1000
-- Hardware survives module replacement and movement between slots/controllers.
CREATE TABLE hashboard (
    id BIGSERIAL PRIMARY KEY,
    org_id BIGINT NOT NULL REFERENCES organization(id),
    first_seen_at TIMESTAMPTZ NOT NULL,
    last_seen_at TIMESTAMPTZ NOT NULL
);
CREATE UNIQUE INDEX hashboard_org_id_id ON hashboard(org_id, id);

CREATE TABLE hashboard_identity (
    org_id BIGINT NOT NULL,
    kind TEXT NOT NULL CHECK (kind IN ('mac', 'serial')),
    value TEXT NOT NULL CHECK (value <> ''),
    hashboard_id BIGINT NOT NULL,
    PRIMARY KEY (org_id, kind, value),
    FOREIGN KEY (org_id, hashboard_id) REFERENCES hashboard(org_id, id)
);
CREATE INDEX hashboard_identity_board ON hashboard_identity(hashboard_id);

CREATE TABLE hashboard_installation (
    id BIGSERIAL PRIMARY KEY,
    hashboard_id BIGINT NOT NULL REFERENCES hashboard(id),
    module_id BIGINT NOT NULL REFERENCES container_module(id),
    position INT NOT NULL CHECK (position BETWEEN 1 AND 3),
    -- Historical access follows the site when installed, not the board's next site.
    site_id BIGINT,
    installed_at TIMESTAMPTZ NOT NULL,
    last_seen_at TIMESTAMPTZ NOT NULL,
    removed_at TIMESTAMPTZ,
    metadata JSONB NOT NULL DEFAULT '{}',
    CHECK (removed_at IS NULL OR removed_at >= installed_at)
);
CREATE UNIQUE INDEX hashboard_current_board ON hashboard_installation(hashboard_id) WHERE removed_at IS NULL;
CREATE UNIQUE INDEX hashboard_current_slot ON hashboard_installation(module_id, position) WHERE removed_at IS NULL;
CREATE INDEX hashboard_installation_history ON hashboard_installation(hashboard_id, installed_at);

CREATE TABLE hashboard_slot (
    module_id BIGINT NOT NULL REFERENCES container_module(id),
    hashboard_id BIGINT REFERENCES hashboard(id),
    position INT NOT NULL CHECK (position BETWEEN 1 AND 3),
    unavailable_reason TEXT NOT NULL DEFAULT '',
    occupancy TEXT NOT NULL CHECK (occupancy IN ('occupied', 'empty', 'unknown')),
    observed_at TIMESTAMPTZ NOT NULL,
    health_observed_at TIMESTAMPTZ,
    state TEXT NOT NULL DEFAULT 'unknown',
    state_observed_at TIMESTAMPTZ,
    mining_state TEXT NOT NULL DEFAULT '',
    isolated BOOLEAN,
    PRIMARY KEY (module_id, position)
);

CREATE TABLE hashboard_metrics (
    hashboard_id BIGINT NOT NULL REFERENCES hashboard(id),
    installation_id BIGINT NOT NULL REFERENCES hashboard_installation(id),
    time TIMESTAMPTZ NOT NULL,
    collected_at TIMESTAMPTZ NOT NULL,
    state TEXT NOT NULL,
    state_observed_at TIMESTAMPTZ,
    mining_state TEXT NOT NULL,
    isolated BOOLEAN,
    hashrate_hs DOUBLE PRECISION,
    temperature_c DOUBLE PRECISION,
    hashrail_ok BOOLEAN,
    refclks_ok BOOLEAN,
    -- All MDK health fields, in firmware units. Shared PSU evidence is not board power.
    health JSONB,
    PRIMARY KEY (hashboard_id, time, collected_at)
);
SELECT create_hypertable('hashboard_metrics', 'time', chunk_time_interval => INTERVAL '1 day');
SELECT add_retention_policy('hashboard_metrics', INTERVAL '30 days');

CREATE TABLE hashboard_latest_metrics (
    hashboard_id BIGINT PRIMARY KEY REFERENCES hashboard(id),
    installation_id BIGINT NOT NULL REFERENCES hashboard_installation(id),
    time TIMESTAMPTZ NOT NULL,
    collected_at TIMESTAMPTZ NOT NULL,
    state TEXT NOT NULL,
    state_observed_at TIMESTAMPTZ,
    mining_state TEXT NOT NULL,
    isolated BOOLEAN,
    hashrate_hs DOUBLE PRECISION,
    temperature_c DOUBLE PRECISION,
    hashrail_ok BOOLEAN,
    refclks_ok BOOLEAN,
    health JSONB
);
