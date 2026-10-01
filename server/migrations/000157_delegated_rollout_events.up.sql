-- A delegated wait clock survives restarts and resets when controller work starts.
ALTER TABLE firmware_rollout ADD COLUMN controller_waiting_since TIMESTAMPTZ;

-- Writers acquire LockReleaseChannelScopes before any channel/rollout lock.
-- This serializes event allocation and commit within an organization, so a
-- cursor ordered by id cannot jump over an event still waiting to commit.
CREATE TABLE firmware_rollout_event (
    id BIGSERIAL PRIMARY KEY,
    org_id BIGINT NOT NULL REFERENCES organization(id) ON DELETE CASCADE,
    rollout_id BIGINT NOT NULL REFERENCES firmware_rollout(id) ON DELETE CASCADE,
    channel_id BIGINT NOT NULL REFERENCES release_channel(id) ON DELETE CASCADE,
    type TEXT NOT NULL,
    occurred_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    actor_type TEXT NOT NULL CHECK (actor_type IN ('user', 'api_key', 'system')),
    actor_id BIGINT NOT NULL,
    actor_name TEXT NOT NULL,
    rollout_revision BIGINT NOT NULL CHECK (rollout_revision > 0),
    note TEXT NOT NULL DEFAULT '',
    device_identifiers TEXT[] NOT NULL DEFAULT '{}'
);
CREATE INDEX firmware_rollout_event_org_id ON firmware_rollout_event(org_id, id);
CREATE INDEX firmware_rollout_event_rollout_id ON firmware_rollout_event(org_id, rollout_id, id);
CREATE INDEX firmware_rollout_event_channel_id ON firmware_rollout_event(org_id, channel_id, id);

-- Preserve prior labels exactly while naming the newly available lifecycle
-- actions in activity search and display. The down migration restores v150.
ALTER FUNCTION activity_display_label(TEXT, TEXT, TEXT, JSONB, TEXT)
    RENAME TO activity_display_label_v150;

CREATE OR REPLACE FUNCTION activity_display_label(
    event_type TEXT,
    scope_type TEXT,
    scope_label TEXT,
    metadata JSONB,
    description TEXT
) RETURNS TEXT
LANGUAGE SQL
IMMUTABLE
PARALLEL SAFE
AS $$
SELECT CASE
    WHEN event_type IN ('rollout_advanced', 'rollout_devices_skipped',
                        'rollout_device_failed', 'rollout_controller_timed_out')
        THEN CONCAT(
            CASE event_type
                WHEN 'rollout_advanced'             THEN 'Advanced firmware update'
                WHEN 'rollout_devices_skipped'      THEN 'Skipped firmware update targets'
                WHEN 'rollout_device_failed'        THEN 'Firmware update target failed'
                WHEN 'rollout_controller_timed_out' THEN 'Firmware update controller timed out'
            END,
            COALESCE(
                ': ' || NULLIF(CONCAT_WS(' ',
                    metadata->>'channel_name',
                    metadata->>'manufacturer',
                    metadata->>'model',
                    CASE WHEN metadata->>'firmware_version' IS NOT NULL
                         THEN '→ ' || (metadata->>'firmware_version') END
                ), ''),
                ''
            )
        )
    ELSE activity_display_label_v150(event_type, scope_type, scope_label, metadata, description)
END
$$;
