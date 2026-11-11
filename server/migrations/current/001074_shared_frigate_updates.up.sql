-- Fleet-triggered Frigate software updates for CT1 containers.
-- controller:software_update is not granted to the ADMIN built-in; SUPER_ADMIN
-- receives it from startup reconciliation, and operators grant it through custom roles.
INSERT INTO permission (key, description) VALUES
    ('controller:software_update', 'Upload Frigate software packages and update CT1 controller software. Updates stop mining controllers while they install.')
ON CONFLICT (key) DO NOTHING;

-- Uploaded Frigate Debian packages. The signed release statement and signature
-- are kept verbatim so every controller can re-verify them. The package bytes
-- live on disk under the Fleet host's package store.
CREATE TABLE frigate_update_package (
    id UUID PRIMARY KEY,
    org_id BIGINT NOT NULL REFERENCES organization (id),
    filename TEXT NOT NULL,
    version TEXT NOT NULL,
    architecture TEXT NOT NULL,
    sha256 TEXT NOT NULL CHECK (sha256 ~ '^[0-9a-f]{64}$'),
    size_bytes BIGINT NOT NULL CHECK (size_bytes > 0),
    repository TEXT NOT NULL,
    release_key_id TEXT NOT NULL,
    min_installed_version TEXT NOT NULL DEFAULT '',
    issued_at TIMESTAMPTZ NOT NULL,
    statement BYTEA NOT NULL,
    signature BYTEA NOT NULL,
    uploaded_by BIGINT REFERENCES "user" (id),
    uploaded_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_at TIMESTAMPTZ
);

CREATE UNIQUE INDEX idx_frigate_update_package_org_sha256
    ON frigate_update_package (org_id, sha256) WHERE deleted_at IS NULL;

-- One update of one container. The job id is the staged job id on every
-- controller and the Frigate rollout id.
CREATE TABLE frigate_update_job (
    id UUID PRIMARY KEY,
    org_id BIGINT NOT NULL REFERENCES organization (id),
    device_id BIGINT NOT NULL REFERENCES device (id),
    package_id UUID NOT NULL REFERENCES frigate_update_package (id),
    state TEXT NOT NULL CHECK (state IN (
        'staging', 'staged', 'starting', 'running',
        'succeeded', 'failed', 'cancelled', 'verified', 'abandoned')),
    stage_only BOOLEAN NOT NULL DEFAULT FALSE,
    rollout_phase TEXT NOT NULL DEFAULT '',
    from_version TEXT NOT NULL DEFAULT '',
    target_version TEXT NOT NULL,
    expected_instance_ids TEXT[] NOT NULL CHECK (cardinality(expected_instance_ids) > 0),
    driver_instance_id TEXT NOT NULL DEFAULT '',
    error_code TEXT NOT NULL DEFAULT '',
    error_message TEXT NOT NULL DEFAULT '',
    next_step TEXT NOT NULL DEFAULT '',
    retryable BOOLEAN NOT NULL DEFAULT FALSE,
    cancellable BOOLEAN NOT NULL DEFAULT TRUE,
    disruption_started BOOLEAN NOT NULL DEFAULT FALSE,
    served_by_maintenance BOOLEAN NOT NULL DEFAULT FALSE,
    -- The operator's idempotency key for StartUpdate.
    request_key TEXT NOT NULL,
    -- Set before the first Start is sent. A Start whose outcome is unknown is
    -- reconciled through Status, then repeated with the same derived key.
    start_attempted BOOLEAN NOT NULL DEFAULT FALSE,
    -- When the first Start was attempted. The start deadline counts from here.
    start_attempted_at TIMESTAMPTZ,
    -- A runner that is about to send Start holds this lease while the call can
    -- still be in flight. An operator cancel that would complete locally waits
    -- for it to expire, so a cancelled job is never started afterwards.
    start_lease_until TIMESTAMPTZ,
    -- An operator cancel that arrived while the start lease was live. The runner
    -- takes no new start lease once this is set; after the lease has expired it
    -- reads Frigate's status and either completes the cancel locally (Frigate has
    -- no record of the job) or sends Frigate a Cancel. cancel_request_key is the
    -- operator's idempotency key, which frigate_update_action also records.
    cancel_requested_at TIMESTAMPTZ,
    cancel_request_key TEXT NOT NULL DEFAULT '',
    cancel_request_actor TEXT NOT NULL DEFAULT '',
    -- Number of operator retry/cancel actions; it numbers the derived Frigate
    -- idempotency keys. Each action is recorded in frigate_update_action.
    action_counter INTEGER NOT NULL DEFAULT 0 CHECK (action_counter >= 0),
    -- Staged copies on the controllers still need a best-effort abort.
    cleanup_pending BOOLEAN NOT NULL DEFAULT FALSE,
    -- Last failed abort attempt. The sweep tries the least recently attempted
    -- job first so one unreachable container cannot starve the others.
    cleanup_attempted_at TIMESTAMPTZ,
    actor_user_id BIGINT NOT NULL REFERENCES "user" (id),
    actor_external_id TEXT NOT NULL,
    actor_username TEXT NOT NULL,
    rollout_updated_at TIMESTAMPTZ,
    last_polled_at TIMESTAMPTZ,
    last_contact_at TIMESTAMPTZ,
    unreachable_since TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    completed_at TIMESTAMPTZ,
    CONSTRAINT uq_frigate_update_job_org_request_key UNIQUE (org_id, request_key)
);

-- A container has at most one blocking job: one in progress, or one that
-- failed after stopping controllers and can still be retried.
CREATE UNIQUE INDEX idx_frigate_update_job_device_blocking
    ON frigate_update_job (device_id)
    WHERE state IN ('staging', 'staged', 'starting', 'running')
       OR (state = 'failed' AND retryable);

CREATE INDEX idx_frigate_update_job_device_created
    ON frigate_update_job (device_id, created_at DESC);

CREATE INDEX idx_frigate_update_job_package
    ON frigate_update_job (package_id);

CREATE TABLE frigate_update_instance (
    job_id UUID NOT NULL REFERENCES frigate_update_job (id) ON DELETE CASCADE,
    instance_id TEXT NOT NULL,
    stage_state TEXT NOT NULL DEFAULT 'pending' CHECK (stage_state IN ('pending', 'staging', 'staged', 'failed')),
    manifest_sha256 TEXT NOT NULL DEFAULT '',
    staged_bytes BIGINT NOT NULL DEFAULT 0 CHECK (staged_bytes >= 0),
    size_bytes BIGINT NOT NULL CHECK (size_bytes > 0),
    agent_phase TEXT NOT NULL DEFAULT '',
    last_contact TIMESTAMPTZ,
    contact_stale BOOLEAN NOT NULL DEFAULT FALSE,
    running_version TEXT NOT NULL DEFAULT '',
    migration_executed BOOLEAN NOT NULL DEFAULT FALSE,
    error_code TEXT NOT NULL DEFAULT '',
    error_message TEXT NOT NULL DEFAULT '',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (job_id, instance_id)
);

-- Every operator retry/cancel action, keyed by the operator's idempotency key,
-- so any repeated key replays its original outcome, not only the latest one.
-- The row is written before Frigate is called. counter numbers the derived
-- Frigate key; local is set once the action finished without Frigate.
CREATE TABLE frigate_update_action (
    job_id UUID NOT NULL REFERENCES frigate_update_job (id) ON DELETE CASCADE,
    kind TEXT NOT NULL CHECK (kind IN ('retry', 'cancel')),
    request_key TEXT NOT NULL,
    counter INTEGER NOT NULL CHECK (counter >= 0),
    local BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (job_id, kind, request_key)
);
