DELETE FROM role_permission WHERE permission_id IN (
    SELECT id FROM permission WHERE key IN ('miner_channel:read', 'miner_channel:manage')
);
DELETE FROM permission WHERE key IN ('miner_channel:read', 'miner_channel:manage');
