-- name: UpsertMinerCredentials :exec
INSERT INTO miner_credentials (device_id, username_enc, password_enc)
VALUES ($1, $2, $3)
ON CONFLICT (device_id) DO UPDATE SET
    username_enc = EXCLUDED.username_enc,
    password_enc = EXCLUDED.password_enc;

-- name: GetMinerCredentialsByDeviceID :one
SELECT * FROM miner_credentials
WHERE device_id = $1;
