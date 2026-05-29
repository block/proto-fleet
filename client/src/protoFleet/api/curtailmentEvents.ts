export const CURTAILMENT_CHANGED_EVENT = "protoFleet:curtailment-changed";

export function emitCurtailmentChanged(): void {
  if (typeof window === "undefined") {
    return;
  }

  window.dispatchEvent(new CustomEvent(CURTAILMENT_CHANGED_EVENT));
}
