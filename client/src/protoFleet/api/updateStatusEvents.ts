export const UPDATE_STATUS_INVALIDATED_EVENT = "protoFleet:update-status-invalidated";

export function emitUpdateStatusInvalidated(): void {
  if (typeof window === "undefined") {
    return;
  }

  window.dispatchEvent(new CustomEvent(UPDATE_STATUS_INVALIDATED_EVENT));
}
