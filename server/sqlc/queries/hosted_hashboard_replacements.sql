-- name: ReconcileHostedHashboardReplacements :exec
SELECT reconcile_hosted_hashboard_replacements(sqlc.arg(organization_id)::bigint,sqlc.arg(device_id)::bigint,sqlc.arg(observed_at)::timestamptz);
