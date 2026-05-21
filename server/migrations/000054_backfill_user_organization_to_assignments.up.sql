-- Backfill user_organization_role from the legacy user_organization
-- table. Existing user/org pairs become org-scope assignments pointing
-- at the same role_id, preserving every user's current effective
-- access. No flag day, no re-login required.
--
-- This migration intentionally leaves user_organization.role_id
-- untouched. The follow-up migration that neutralizes that column
-- (rename + raising trigger on non-NULL writes) ships alongside the
-- code change that swaps callers to the new assignment table — doing
-- them in the same release window means there is no deploy state in
-- which onboarding writes to a column whose trigger explodes.

INSERT INTO user_organization_role (
    user_id,
    organization_id,
    role_id,
    scope_type,
    scope_id
)
SELECT
    user_id,
    organization_id,
    role_id,
    'org',
    NULL
FROM user_organization
WHERE deleted_at IS NULL
ON CONFLICT (user_id, organization_id, role_id)
    WHERE scope_type = 'org' AND deleted_at IS NULL
    DO NOTHING;
