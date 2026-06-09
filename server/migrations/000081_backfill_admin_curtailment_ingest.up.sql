-- Backfill curtailment:ingest onto existing ADMIN roles.
--
-- MQTT source settings now persist the configuring session user as the
-- runtime actor. The subscriber still verifies that actor can ingest
-- curtailment signals, so upgraded orgs need ADMIN to hold the ingest key
-- as well as curtailment:manage. SUPER_ADMIN converges on all permissions
-- at boot; FIELD_TECH does not receive this permission by design.

INSERT INTO permission (key, description) VALUES
    ('curtailment:ingest', 'Accept curtailment dispatch signals from external providers and configured MQTT sources.')
ON CONFLICT (key) DO UPDATE SET description = EXCLUDED.description;

-- Scoped to the built-in ADMIN role so operator-created custom roles are not
-- widened. ON CONFLICT makes this safe to replay against orgs that already
-- hold the key.
INSERT INTO role_permission (role_id, permission_id)
SELECT r.id, p.id
FROM role r, permission p
WHERE r.builtin_key = 'ADMIN'
  AND r.deleted_at IS NULL
  AND p.key = 'curtailment:ingest'
ON CONFLICT (role_id, permission_id) DO NOTHING;
