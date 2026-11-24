-- name: SavePoolConfigurationObservation :exec
INSERT INTO pool_configuration_observation(organization_id,target_kind,target_id,assignment_version,state,result_code,last_verified_at)
VALUES($1,$2,$3,$4,$5,$6,CASE WHEN $5='verified' THEN clock_timestamp() END)
ON CONFLICT(organization_id,target_kind,target_id) DO UPDATE SET
 assignment_version=EXCLUDED.assignment_version,state=EXCLUDED.state,result_code=EXCLUDED.result_code,observed_at=EXCLUDED.observed_at,
 last_verified_at=CASE WHEN EXCLUDED.state='verified' THEN EXCLUDED.observed_at
  WHEN pool_configuration_observation.assignment_version=EXCLUDED.assignment_version THEN pool_configuration_observation.last_verified_at END;

-- name: GetPoolConfigurationObservation :one
SELECT * FROM pool_configuration_observation WHERE organization_id=$1 AND target_kind=$2 AND target_id=$3 AND assignment_version=$4;
