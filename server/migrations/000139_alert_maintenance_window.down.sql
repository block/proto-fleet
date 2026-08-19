-- Refuse while rows exist: the startup sweep deleted the Grafana silences these rows replaced,
-- so a silent downgrade would lift every active window's suppression (the pre-139 server reads
-- only Grafana) and drop the audit history. Delete the org's windows first — after confirming
-- no maintenance is in progress — or recreate them as silences on the old release; an empty
-- table drops cleanly.
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM alert_maintenance_window) THEN
        RAISE EXCEPTION 'alert_maintenance_window has rows the pre-139 server cannot read; delete the windows (suppression lifts!) before rolling back';
    END IF;
END $$;
DROP TABLE IF EXISTS alert_maintenance_window;
