DELETE FROM role_permission
WHERE permission_id IN (
    SELECT id
    FROM permission
    WHERE key IN (
        'channel:read',
        'channel:manage',
        'rollout:read',
        'rollout:manage',
        'rollout:control'
    )
);

DELETE FROM permission
WHERE key IN (
    'channel:read',
    'channel:manage',
    'rollout:read',
    'rollout:manage',
    'rollout:control'
);
