-- Backfill user_organization_role from the legacy user_organization
-- table. Existing user/org pairs become org-scope assignments pointing
-- at the same role_id, preserving every user's current effective
-- access. No flag day, no re-login required.
--
-- Plan deviation note: the plan's U5 also renamed
-- user_organization.role_id to role_id_deprecated_do_not_use and
-- added a raising trigger on non-NULL writes. That part is moved to
-- PR 2 (the security-critical PR) so it lands together with the
-- caller swap in U6/U7; doing the rename here would explode the
-- existing onboarding flow the moment PR 1 deploys and before PR 2
-- can land. The plan's intent (loud failure during soak) is still
-- honored — the soak window just shifts from "between PR 1 and U12"
-- to "between PR 2 and U12".

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
ON CONFLICT (user_id, organization_id, role_id, scope_type, scope_id) DO NOTHING;
