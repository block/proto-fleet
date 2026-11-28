-- A ProtoOS package or job would read as controller software after the kind
-- column is gone, so refuse while any ProtoOS package row exists.
LOCK TABLE frigate_update_package IN ACCESS EXCLUSIVE MODE;
DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM frigate_update_package WHERE kind = 'proto_os') THEN
        RAISE EXCEPTION 'ProtoOS packages exist; remove them and their update jobs before this rollback';
    END IF;
END $$;

ALTER TABLE frigate_update_instance
    DROP COLUMN protoos_served_release_tag,
    DROP COLUMN protoos_installed_version,
    DROP COLUMN protoos_job_state;

ALTER TABLE frigate_update_package DROP COLUMN kind;
