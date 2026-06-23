INSERT INTO permission (key, description) VALUES
    ('cohort:read', 'View cohorts, reservations, and effective desired state.'),
    ('cohort:manage', 'Create, release, and manage cohorts and cohort memberships.')
ON CONFLICT (key) DO NOTHING;
