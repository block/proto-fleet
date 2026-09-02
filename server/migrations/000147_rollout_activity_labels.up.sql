-- Display labels for firmware rollout activity events. Keep in sync with
-- the client label maps (client/src/protoFleet/features/activity/utils).

ALTER FUNCTION activity_display_label(TEXT, TEXT, TEXT, JSONB, TEXT)
    RENAME TO activity_display_label_v142;

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
    WHEN event_type LIKE 'rollout_%'
        THEN CONCAT(
            CASE event_type
                WHEN 'rollout_started'      THEN 'Started firmware rollout'
                WHEN 'rollout_review_ready' THEN 'Firmware rollout ready for review'
                WHEN 'rollout_continued'    THEN 'Continued firmware rollout'
                WHEN 'rollout_paused'       THEN 'Paused firmware rollout'
                WHEN 'rollout_resumed'      THEN 'Resumed firmware rollout'
                WHEN 'rollout_aborted'      THEN 'Aborted firmware rollout'
                WHEN 'rollout_completed'    THEN 'Completed firmware rollout'
                ELSE 'Firmware rollout'
            END,
            COALESCE(
                ': ' || NULLIF(CONCAT_WS(' ',
                    metadata->>'lane_name',
                    metadata->>'model',
                    CASE WHEN metadata->>'firmware_version' IS NOT NULL
                         THEN '→ ' || (metadata->>'firmware_version') END
                ), ''),
                ''
            )
        )
    ELSE activity_display_label_v142(event_type, scope_type, scope_label, metadata, description)
END
$$;
