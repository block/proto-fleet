ALTER TABLE pool_configuration ADD COLUMN is_default BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE pool_configuration ADD CONSTRAINT ck_pool_configuration_active_default CHECK (NOT (archived AND is_default));

-- Establish a deterministic default without changing existing equipment.
WITH ranked AS (
 SELECT configuration_id, row_number() OVER (
  PARTITION BY organization_id, account_id ORDER BY created_at, configuration_id
 ) AS position FROM pool_configuration WHERE NOT archived
)
UPDATE pool_configuration c SET is_default=TRUE FROM ranked r
WHERE c.configuration_id=r.configuration_id AND r.position=1;

CREATE UNIQUE INDEX idx_pool_configuration_owner_default
 ON pool_configuration(organization_id, COALESCE(account_id, 0)) WHERE is_default;
