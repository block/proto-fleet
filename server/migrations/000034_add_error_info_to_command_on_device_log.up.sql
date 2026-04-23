-- Persist per-device failure reasons so the activity log detail RPC can expose
-- them to operators. The column is nullable for backward compatibility with
-- existing rows and for SUCCESS rows (which have no error to report).

ALTER TABLE command_on_device_log
    ADD COLUMN error_info TEXT NULL;
