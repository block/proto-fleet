-- An uploaded package is the controller software (frigate) or the ProtoOS
-- browser UI (proto_os). The kind comes from the signed release statement and
-- selects the upload rules and the job state machine.
ALTER TABLE frigate_update_package
    ADD COLUMN kind TEXT NOT NULL DEFAULT 'frigate'
    CONSTRAINT ck_frigate_update_package_kind CHECK (kind IN ('frigate', 'proto_os'));

-- What each controller reports about a ProtoOS job: the state of the job that
-- Fleet staged (empty while the controller reports another job or none), the
-- installed proto-os version, and the release tag of the ProtoOS that it
-- serves now.
ALTER TABLE frigate_update_instance
    ADD COLUMN protoos_job_state TEXT NOT NULL DEFAULT '',
    ADD COLUMN protoos_installed_version TEXT NOT NULL DEFAULT '',
    ADD COLUMN protoos_served_release_tag TEXT NOT NULL DEFAULT '';
