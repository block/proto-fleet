DROP TABLE firmware_rollout_event;
ALTER TABLE firmware_rollout DROP COLUMN controller_waiting_since;

DROP FUNCTION activity_display_label(TEXT, TEXT, TEXT, JSONB, TEXT);
ALTER FUNCTION activity_display_label_v150(TEXT, TEXT, TEXT, JSONB, TEXT)
    RENAME TO activity_display_label;
