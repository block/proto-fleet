-- Preserve the exact 000114 implementation as a helper, then layer the new
-- break-glass label over it. This keeps the migration small while allowing the
-- down migration to restore the prior function byte-for-byte.
ALTER FUNCTION activity_display_label(TEXT, TEXT, TEXT, JSONB, TEXT)
    RENAME TO activity_display_label_v114;

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
    WHEN event_type = 'cli_reset_password'
        THEN CONCAT(
            'Break-glass password reset',
            COALESCE(' for ' || COALESCE(metadata->>'target_username', scope_label), '')
        )
    ELSE activity_display_label_v114(event_type, scope_type, scope_label, metadata, description)
END
$$;
