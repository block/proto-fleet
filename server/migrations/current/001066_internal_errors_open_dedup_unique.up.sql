-- requires-shared: 1064

-- Hold the write fence through cleanup and index creation in the checkpoint transaction.
LOCK TABLE errors IN SHARE ROW EXCLUSIVE MODE;

-- Legacy device clocks may have left open occurrences ahead of server time.
UPDATE errors
SET last_seen_at = CURRENT_TIMESTAMP
WHERE closed_at IS NULL
  AND last_seen_at > CURRENT_TIMESTAMP;

-- Keep the oldest open occurrence per dedup key; the unique index cannot be built over duplicates.
UPDATE errors
SET closed_at = CURRENT_TIMESTAMP
WHERE id IN (
    SELECT id
    FROM (
        SELECT
            id,
            row_number() OVER (
                PARTITION BY org_id, device_id, miner_error, component_id, component_type
                ORDER BY id
            ) AS occurrence
        FROM errors
        WHERE closed_at IS NULL
    ) open_errors
    WHERE occurrence > 1
);

CREATE UNIQUE INDEX idx_errors_open_dedup
    ON errors (org_id, device_id, miner_error, component_id, component_type) NULLS NOT DISTINCT
    WHERE closed_at IS NULL;
