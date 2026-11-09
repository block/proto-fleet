-- requires-shared: 1067
-- Clearing retains the current assignment until empty readback succeeds.
CREATE OR REPLACE FUNCTION validate_pool_update_attempt() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'UPDATE' AND OLD.physical_attempt_id IS NOT NULL AND
        NEW.physical_attempt_id IS DISTINCT FROM OLD.physical_attempt_id THEN
        RAISE EXCEPTION 'pool update attempt is immutable' USING ERRCODE = '23514';
    END IF;
    IF NEW.physical_attempt_id IS NOT NULL AND (TG_OP = 'INSERT' OR OLD.physical_attempt_id IS NULL) AND NOT EXISTS (
        SELECT 1 FROM device_write_attempt w
        JOIN pool_configuration_assignment a ON a.organization_id = NEW.organization_id
            AND a.target_kind = NEW.target_kind AND a.target_id = NEW.target_id
        WHERE w.attempt_id = NEW.physical_attempt_id AND w.organization_id = a.organization_id
            AND w.device_id = a.device_id AND w.operation_kind = 'mutation' AND w.status = 'running'
            AND (a.operation_id = NEW.operation_id OR EXISTS(SELECT 1 FROM pool_configuration_operation o WHERE o.operation_id=NEW.operation_id AND o.organization_id=NEW.organization_id AND o.kind IN ('clear','unassign') AND o.configuration_id=a.configuration_id AND o.revision_id=a.revision_id)) AND a.version = NEW.assignment_version AND a.retired_at IS NULL
            AND NEW.state = 'applying'
    ) THEN
        RAISE EXCEPTION 'pool update requires its current physical admission' USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;

ALTER TABLE device_write_recovery DROP CONSTRAINT device_write_recovery_prior_status_check;
ALTER TABLE device_write_recovery ADD CONSTRAINT device_write_recovery_prior_status_check
    CHECK (prior_status IN ('running','unknown','complete'));
