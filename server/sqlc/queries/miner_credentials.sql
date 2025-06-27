-- name: CreateMinerCredentials :exec
INSERT INTO miner_credentials (device_id, username_enc, password_enc)
VALUES (?, ?, ?);

-- name: GetMinerCredentialsByDeviceID :one
SELECT * FROM miner_credentials
WHERE device_id = ?;
