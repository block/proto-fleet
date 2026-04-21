import { deviceActions, settingsActions, statusColumnLoadingMessages } from "../components/MinerActionsMenu/constants";
import { DeviceStatus } from "@/protoFleet/api/generated/telemetry/v1/telemetry_pb";
import type { BatchOperation } from "@/protoFleet/features/fleetManagement/hooks/useBatchOperations";

/**
 * Check if a device has reached the expected status for a given batch action.
 * This logic is shared between status polling and UI display.
 */
export function hasReachedExpectedStatus(
  action: string,
  deviceStatus: DeviceStatus | undefined,
  startedAt?: number,
): boolean {
  if (deviceStatus === undefined) return false;

  // Check expected status based on action
  if (action === settingsActions.miningPool) {
    // Pool assignment: complete when no longer NEEDS_MINING_POOL
    return deviceStatus !== DeviceStatus.NEEDS_MINING_POOL;
  } else if (action === deviceActions.shutdown) {
    // Sleep: complete when status is INACTIVE
    return deviceStatus === DeviceStatus.INACTIVE;
  } else if (action === deviceActions.wakeUp) {
    // Wake up: complete when no longer INACTIVE
    return deviceStatus !== DeviceStatus.INACTIVE;
  } else if (action === deviceActions.reboot) {
    // Reboot: transient operation (ONLINE → OFFLINE → ONLINE)
    // Note: 15 seconds is a conservative minimum that works across all miner types:
    // - Proto miners typically reboot in 10-12 seconds
    // - Antminers can take 12-15 seconds depending on hardware
    // This ensures the device has time to go offline and come back online
    const minRebootDuration = 15000; // 15 seconds
    const elapsed = startedAt ? Date.now() - startedAt : 0;

    if (elapsed < minRebootDuration) {
      return false; // Too early, keep showing loading
    }

    // After 15s, complete when device is no longer OFFLINE
    return deviceStatus !== DeviceStatus.OFFLINE;
  } else if (action === deviceActions.firmwareUpdate) {
    return deviceStatus === DeviceStatus.REBOOT_REQUIRED;
  }

  return false;
}

/**
 * Check if a batch action is actively loading (has a loading message and
 * the device hasn't yet reached the expected status for that action).
 */
export function isActionLoading(batch: BatchOperation | undefined, deviceStatus: DeviceStatus | undefined): boolean {
  if (!batch || !statusColumnLoadingMessages[batch.action]) return false;
  return !hasReachedExpectedStatus(batch.action, deviceStatus, batch.startedAt);
}
