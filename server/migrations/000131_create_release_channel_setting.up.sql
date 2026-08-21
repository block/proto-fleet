-- Per-org release channel for server update notifications: 'stable' offers
-- only stable releases; 'stable_and_rc' also offers release candidates.
-- A missing row is valid — the service layer applies the 'stable' default —
-- so no rows are seeded here.
CREATE TABLE release_channel_setting (
    organization_id BIGINT      PRIMARY KEY,
    channel         TEXT        NOT NULL,
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT fk_release_channel_setting_org FOREIGN KEY (organization_id)
        REFERENCES organization(id),
    CONSTRAINT ck_release_channel_setting_channel
        CHECK (channel IN ('stable', 'stable_and_rc'))
);

-- Seed the instance:update permission row only. The catalog row must exist
-- before SeedOrgBuiltins runs: the onboarding path resolves seed keys
-- against the permission table and relies on migrations to populate it
-- (see reconcile.go). No role_permission grants here — SUPER_ADMIN
-- converges via full reconcile at boot, and ADMIN deliberately excludes
-- instance:update (see adminSeedPermissions in builtin.go).
INSERT INTO permission (key, description) VALUES
    ('instance:update', 'See available server updates, change the release channel, and apply server upgrades.')
ON CONFLICT (key) DO UPDATE SET description = EXCLUDED.description;
