-- name: UpsertMinerCredentials :exec
INSERT INTO miner_credentials (device_id, username_enc, password_enc)
VALUES (?, ?, ?)
ON DUPLICATE KEY UPDATE username_enc = VALUES(username_enc), password_enc = VALUES(password_enc);

-- name: GetMinerCredentialsByDeviceID :one
SELECT * FROM miner_credentials
WHERE device_id = ?;
