DROP INDEX idx_channel_firmware_enforcement_reconcile;

ALTER TABLE channel_firmware_enforcement
    DROP COLUMN next_reconcile_at;

CREATE INDEX idx_channel_firmware_enforcement_reconcile
    ON channel_firmware_enforcement(state, updated_at, id)
    WHERE state IN ('pending', 'held', 'dispatching', 'dispatched', 'verifying');
