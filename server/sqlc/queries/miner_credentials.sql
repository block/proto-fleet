-- name: UpsertMinerCredentials :exec
-- The parent lock also protects absence of credentials during dispatch.
WITH locked_device AS MATERIALIZED (
    SELECT device.id FROM device WHERE device.id=sqlc.arg('device_id') FOR NO KEY UPDATE
)
INSERT INTO miner_credentials (device_id, username_enc, password_enc)
VALUES ((SELECT locked_device.id FROM locked_device), sqlc.arg('username_enc'), sqlc.arg('password_enc'))
ON CONFLICT (device_id) DO UPDATE SET
    username_enc = EXCLUDED.username_enc,
    password_enc = EXCLUDED.password_enc;

-- name: GetMinerCredentialsByDeviceID :one
SELECT * FROM miner_credentials
WHERE device_id = $1;

-- name: DeleteMinerCredentialsByDeviceIDAndOrgID :execrows
DELETE FROM miner_credentials mc
USING device d
WHERE mc.device_id = d.id
  AND d.id = $1
  AND d.org_id = $2;

-- name: DeleteMinerCredentialsForFleetNode :execrows
DELETE FROM miner_credentials mc
USING fleet_node_device fnd
WHERE mc.device_id = fnd.device_id
  AND fnd.fleet_node_id = $1
  AND fnd.org_id = $2;

-- name: UpdateMinerPassword :execrows
UPDATE miner_credentials
SET password_enc = $1
WHERE device_id = $2;
