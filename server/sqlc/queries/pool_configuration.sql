-- name: CreatePoolConfiguration :one
INSERT INTO pool_configuration(configuration_id, organization_id, account_id, name)
VALUES ($1, $2, $3, $4) RETURNING *;

-- name: CreatePoolConfigurationRevision :one
INSERT INTO pool_configuration_revision(revision_id, organization_id, configuration_id, number, slots)
VALUES ($1, $2, $3, $4, $5) RETURNING *;

-- name: LockPoolConfiguration :one
SELECT * FROM pool_configuration
WHERE organization_id = sqlc.arg(organization_id) AND configuration_id = sqlc.arg(configuration_id)
 AND account_id IS NOT DISTINCT FROM sqlc.narg(account_id)::bigint
FOR UPDATE;

-- name: GetPoolConfigurationRevision :one
SELECT r.* FROM pool_configuration_revision r
JOIN pool_configuration c ON c.organization_id = r.organization_id AND c.configuration_id = r.configuration_id
WHERE c.organization_id = sqlc.arg(organization_id) AND c.configuration_id = sqlc.arg(configuration_id)
 AND c.account_id IS NOT DISTINCT FROM sqlc.narg(account_id)::bigint
 AND r.revision_id = sqlc.arg(revision_id);

-- name: SetPoolConfigurationCurrentRevision :one
UPDATE pool_configuration SET current_revision_id = sqlc.arg(revision_id), name = sqlc.arg(name),
 version = version + 1, updated_at = CURRENT_TIMESTAMP
WHERE organization_id = sqlc.arg(organization_id) AND configuration_id = sqlc.arg(configuration_id)
 AND account_id IS NOT DISTINCT FROM sqlc.narg(account_id)::bigint
 AND version = sqlc.arg(expected_version) AND NOT archived
RETURNING *;

-- name: ListPoolConfigurations :many
SELECT c.*, r.slots, r.number AS revision_number,
 (SELECT count(*) FROM pool_configuration_assignment a WHERE a.organization_id=c.organization_id AND a.configuration_id=c.configuration_id AND a.retired_at IS NULL)::bigint AS assigned_count
FROM pool_configuration c
JOIN pool_configuration_revision r ON r.organization_id = c.organization_id
 AND r.configuration_id = c.configuration_id AND r.revision_id = c.current_revision_id
WHERE c.organization_id = sqlc.arg(organization_id)
 AND c.account_id IS NOT DISTINCT FROM sqlc.narg(account_id)::bigint
 AND (sqlc.arg(include_archived)::boolean OR NOT c.archived)
 AND c.configuration_id > sqlc.arg(after_id)::text
ORDER BY c.configuration_id LIMIT sqlc.arg(page_limit);

-- name: GetPoolConfiguration :one
SELECT c.*, r.slots,
 (SELECT count(*) FROM pool_configuration_assignment a WHERE a.organization_id=c.organization_id AND a.configuration_id=c.configuration_id AND a.retired_at IS NULL)::bigint AS assigned_count
FROM pool_configuration c
JOIN pool_configuration_revision r ON r.organization_id=c.organization_id AND r.configuration_id=c.configuration_id AND r.revision_id=c.current_revision_id
WHERE c.organization_id=sqlc.arg(organization_id) AND c.configuration_id=sqlc.arg(configuration_id)
 AND c.account_id IS NOT DISTINCT FROM sqlc.narg(account_id)::bigint;

-- name: ArchivePoolConfiguration :execrows
UPDATE pool_configuration c SET archived=TRUE, version=version+1, updated_at=CURRENT_TIMESTAMP
WHERE c.organization_id=sqlc.arg(organization_id) AND c.configuration_id=sqlc.arg(configuration_id)
 AND c.account_id IS NOT DISTINCT FROM sqlc.narg(account_id)::bigint
 AND c.version=sqlc.arg(expected_version) AND NOT c.archived
 AND NOT EXISTS (SELECT 1 FROM pool_configuration_assignment a WHERE a.organization_id=c.organization_id AND a.configuration_id=c.configuration_id AND a.retired_at IS NULL);

-- name: LockPoolConfigurationImportOrganization :one
-- Preserve serialization without blocking foreign-key KEY SHARE admission.
SELECT id FROM organization WHERE id = $1 FOR NO KEY UPDATE;

-- name: ListImportedPoolConfigurations :many
-- New source versions may reuse only active, current settings with import provenance.
SELECT c.configuration_id, c.name, r.revision_id, r.slots
FROM pool_configuration c
JOIN pool_configuration_revision r ON r.organization_id = c.organization_id
 AND r.configuration_id = c.configuration_id AND r.revision_id = c.current_revision_id
WHERE c.organization_id = sqlc.arg(organization_id)
 AND c.account_id IS NOT DISTINCT FROM sqlc.narg(account_id)::bigint
 AND NOT c.archived
 AND EXISTS (SELECT 1 FROM pool_configuration_import i
             WHERE i.organization_id = c.organization_id AND i.configuration_id = c.configuration_id)
ORDER BY c.configuration_id;

-- name: CountOperatorPoolConfigurations :one
SELECT count(*) FROM pool_configuration WHERE organization_id=$1 AND account_id IS NULL AND NOT archived;

-- name: GetPoolConfigurationImport :one
SELECT i.* FROM pool_configuration_import i
JOIN pool_configuration c ON c.organization_id = i.organization_id AND c.configuration_id = i.configuration_id
WHERE i.organization_id = sqlc.arg(organization_id)
 AND c.account_id IS NOT DISTINCT FROM sqlc.narg(account_id)::bigint
 AND i.source_kind = sqlc.arg(source_kind) AND i.source_id = sqlc.arg(source_id)
 AND i.source_version = sqlc.arg(source_version);

-- name: RecordPoolConfigurationImport :exec
INSERT INTO pool_configuration_import(organization_id, source_kind, source_id, source_version, configuration_id, revision_id)
VALUES ($1, $2, $3, $4, $5, $6);
