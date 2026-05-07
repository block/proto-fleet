-- Multi-site support: per-user-per-org dismissal timestamp for the upgrade
-- migration banner. Existing rows MUST stay NULL (those users see the
-- banner once); rows inserted after this migration default to
-- CURRENT_TIMESTAMP so newly-added users in upgraded orgs do NOT see it.
-- See docs/plans/2026-05-05-multi-site-support-plan.md (J5).
--
-- The two-step ADD then SET DEFAULT is intentional: a single
-- `ADD COLUMN ... DEFAULT CURRENT_TIMESTAMP` would backfill every existing
-- row with the migration's run-time, making the banner inert for
-- everyone. Adding NULL-able first and setting the default afterwards
-- preserves NULL on the existing rows while applying the default to
-- subsequent inserts.

ALTER TABLE user_organization
    ADD COLUMN migration_banner_dismissed_at TIMESTAMPTZ NULL;

ALTER TABLE user_organization
    ALTER COLUMN migration_banner_dismissed_at SET DEFAULT CURRENT_TIMESTAMP;
