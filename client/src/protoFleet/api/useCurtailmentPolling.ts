import { useEffect, useRef } from "react";

interface PollingConsumer {
  priority: number;
  intervalMs: number;
  refresh: (signal: AbortSignal) => Promise<unknown>;
}

// Energy owns the richer refresh while mounted. Elsewhere the header owns the
// active-list refresh. One scheduler prevents their timers drifting apart.
const consumers = new Set<PollingConsumer>();
let timer: ReturnType<typeof setTimeout> | undefined;
let inFlight: AbortController | undefined;
let inFlightOwner: PollingConsumer | undefined;

function schedule(): void {
  clearTimeout(timer);
  const owner = [...consumers].sort((a, b) => b.priority - a.priority)[0];
  if (!owner || inFlight) return;
  timer = setTimeout(() => {
    const controller = new AbortController();
    inFlight = controller;
    inFlightOwner = owner;
    void owner
      .refresh(controller.signal)
      .catch(() => {})
      .finally(() => {
        if (inFlight === controller) {
          inFlight = undefined;
          inFlightOwner = undefined;
        }
        schedule();
      });
  }, owner.intervalMs);
}

export function useCurtailmentPolling({
  enabled = true,
  priority = 0,
  intervalMs,
  refresh,
}: PollingConsumer & { enabled?: boolean }): void {
  const refreshRef = useRef(refresh);
  refreshRef.current = refresh;
  useEffect(() => {
    if (!enabled) return;
    const consumer: PollingConsumer = { priority, intervalMs, refresh: (signal) => refreshRef.current(signal) };
    consumers.add(consumer);
    schedule();
    return () => {
      consumers.delete(consumer);
      if (consumers.size === 0 || inFlightOwner === consumer) {
        inFlight?.abort();
        inFlight = undefined;
        inFlightOwner = undefined;
      }
      schedule();
    };
  }, [enabled, priority, intervalMs]);
}
