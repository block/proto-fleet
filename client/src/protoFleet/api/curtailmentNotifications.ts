export const curtailmentChangedEvent = "protofleet:curtailment-changed";

export function notifyCurtailmentChanged(): void {
  window.dispatchEvent(new Event(curtailmentChangedEvent));
}

export function subscribeToCurtailmentChanges(listener: () => void): () => void {
  window.addEventListener(curtailmentChangedEvent, listener);
  return () => window.removeEventListener(curtailmentChangedEvent, listener);
}
