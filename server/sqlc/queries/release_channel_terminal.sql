-- name: ListTerminalFirmwareRolloutsNeedingProvenance :many
-- Cancellation and completion cannot revoke a firmware command already sent.
-- Only load terminal rollouts whose retained dispatch and current completion
-- witness both succeeded for the artifact, with a matching live report and
-- provenance that could still be adopted. Resolve the current result directly
-- by UUID; obsolete history and missing witnesses do not become candidates.
-- The engine reloads the evidence under the rollout lock before adoption.
SELECT r.*
FROM firmware_rollout r
WHERE r.status <> 'active'
  AND EXISTS (
      SELECT 1
      FROM firmware_rollout_device rd
      JOIN device d ON d.id = rd.device_id AND d.deleted_at IS NULL
      JOIN discovered_device dd ON dd.id = d.discovered_device_id AND dd.deleted_at IS NULL
      JOIN command_batch_log batch ON batch.uuid = rd.last_dispatched_batch_uuid
          AND batch.organization_id = r.org_id
          AND batch.type = 'FirmwareUpdate'
          AND batch.payload->>'firmware_checksum' = r.firmware_checksum
      JOIN command_on_device_log result ON result.command_batch_log_id = batch.id
          AND result.device_id = rd.device_id
          AND result.org_id = r.org_id
          AND result.status = 'SUCCESS'
      JOIN device_firmware_deployment dep ON dep.device_id = rd.device_id
      JOIN command_batch_log current_batch ON current_batch.uuid = dep.last_command_batch_uuid
          AND (current_batch.organization_id = r.org_id OR current_batch.organization_id IS NULL)
          AND current_batch.type = 'FirmwareUpdate'
          AND current_batch.payload->>'firmware_checksum' = r.firmware_checksum
      JOIN command_on_device_log current_result ON current_result.command_batch_log_id = current_batch.id
          AND current_result.device_id = rd.device_id
          AND current_result.org_id = r.org_id
          AND current_result.status = 'SUCCESS'
      WHERE rd.rollout_id = r.id
        AND rd.last_dispatched_at IS NOT NULL
        AND dd.firmware_version = r.firmware_version
        AND COALESCE(dep.firmware_checksum, '') <> r.firmware_checksum
        AND (COALESCE(dep.firmware_checksum, '') = '' OR dep.deployed_at <= rd.last_dispatched_at)
  )
ORDER BY r.id;
