-- Display labels for firmware update (rollout) activity events. Keep in sync
-- with the client label maps (client/src/protoFleet/features/activity/utils).
-- Preserves the 000142 implementation as a helper so the down migration can
-- restore it byte-for-byte.
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
                WHEN 'rollout_started'                 THEN 'Started firmware update'
                WHEN 'rollout_review_ready'            THEN 'Firmware update ready for review'
                WHEN 'rollout_continued'               THEN 'Continued firmware update'
                WHEN 'rollout_paused'                  THEN 'Paused firmware update'
                WHEN 'rollout_resumed'                 THEN 'Resumed firmware update'
                WHEN 'rollout_canceled'                THEN 'Canceled remaining firmware updates'
                WHEN 'rollout_completed'               THEN 'Completed firmware update'
                WHEN 'rollout_completed_with_failures' THEN 'Completed firmware update with failures'
                WHEN 'rollout_retried'                 THEN 'Retried failed firmware updates'
                ELSE 'Firmware update'
            END,
            COALESCE(
                ': ' || NULLIF(CONCAT_WS(' ',
                    metadata->>'channel_name',
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
