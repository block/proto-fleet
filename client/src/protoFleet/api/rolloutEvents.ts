export const ROLLOUT_CHANGED_EVENT = "protoFleet:rollout-changed";

export function emitRolloutChanged(): void {
  if (typeof window === "undefined") {
    return;
  }

  window.dispatchEvent(new CustomEvent(ROLLOUT_CHANGED_EVENT));
}
