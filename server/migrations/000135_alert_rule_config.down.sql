-- Refuse while rows exist: these rules' only config lives here, so a silent downgrade would strand them uneditable and misreport scoped rules as org-wide.
-- Delete the affected user rules before rolling back; an empty table drops cleanly.
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM alert_rule_config) THEN
        RAISE EXCEPTION 'alert_rule_config has rows the pre-135 server cannot read; delete the affected user rules before rolling back';
    END IF;
END $$;
DROP TABLE IF EXISTS alert_rule_config;
