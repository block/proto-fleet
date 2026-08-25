let laneTabOwnerCount = 0;
const listeners = new Set<() => void>();

function notify(): void {
  listeners.forEach((listener) => listener());
}

export function registerRolloutLaneTabPollingOwner(): () => void {
  laneTabOwnerCount += 1;
  notify();
  let registered = true;
  return () => {
    if (!registered) {
      return;
    }
    registered = false;
    laneTabOwnerCount = Math.max(0, laneTabOwnerCount - 1);
    notify();
  };
}

export function subscribeRolloutLaneTabPollingOwnership(listener: () => void): () => void {
  listeners.add(listener);
  return () => listeners.delete(listener);
}

export function rolloutLaneTabOwnsPolling(): boolean {
  return laneTabOwnerCount > 0;
}
