DROP FUNCTION IF EXISTS activity_display_label(TEXT, TEXT, TEXT, JSONB, TEXT);

ALTER FUNCTION activity_display_label_v114(TEXT, TEXT, TEXT, JSONB, TEXT)
    RENAME TO activity_display_label;
