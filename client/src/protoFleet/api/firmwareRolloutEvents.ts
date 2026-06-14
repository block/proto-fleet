export const FIRMWARE_ROLLOUT_CHANGED_EVENT = "protoFleet:firmware-rollout-changed";

/**
 * Notifies in-app listeners (e.g. the global header pill) that a firmware
 * rollout was created or changed, so they can refresh immediately instead of
 * waiting for their next poll.
 */
export function emitFirmwareRolloutChanged(): void {
  if (typeof window === "undefined") {
    return;
  }

  window.dispatchEvent(new CustomEvent(FIRMWARE_ROLLOUT_CHANGED_EVENT));
}
