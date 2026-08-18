ALTER TABLE channel_firmware_enforcement
    ADD COLUMN next_reconcile_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP;

DROP INDEX idx_channel_firmware_enforcement_reconcile;

CREATE INDEX idx_channel_firmware_enforcement_reconcile
    ON channel_firmware_enforcement(next_reconcile_at, updated_at, id)
    WHERE state IN ('pending', 'held', 'dispatching', 'dispatched', 'verifying');
