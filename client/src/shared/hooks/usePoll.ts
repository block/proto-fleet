import { useEffect, useRef } from "react";

interface UsePollProps {
  fetchData: () => Promise<void> | void;
  params?: any;
  /** When true, schedule recurring fetches after each response. When false, only the initial fetch runs. */
  poll?: boolean;
  pollIntervalMs?: number;
  /** Gates the entire hook. When false, no fetches or polls run at all. @default true */
  enabled?: boolean;
}

const usePoll = ({ fetchData, params, poll, pollIntervalMs = 10 * 1000, enabled = true }: UsePollProps) => {
  const fetchDataRef = useRef(fetchData);

  // Keep fetchData ref up to date
  // store this in a ref to avoid re-running the effect below on every
  // render in the case that usePoll is called inline without memoizing fetchData
  useEffect(() => {
    fetchDataRef.current = fetchData;
  }, [fetchData]);

  useEffect(() => {
    if (!enabled) return undefined;

    // Effect-local cancellation. Each effect run gets its own `alive`
    // flag and `timeoutId`. Cleanup flips this run's flag and clears
    // its timeout — so the in-flight fetch's continuation can't
    // accidentally schedule a follow-up after the user toggles
    // `enabled` off and back on (e.g. a BuildingCard scrolling out and
    // back into the viewport before its request resolves). Using a
    // shared ref here would let the new effect's `alive = true` reset
    // the flag the old continuation reads, attaching a second
    // concurrent poll loop to the same card.
    let alive = true;
    let timeoutId: ReturnType<typeof setTimeout> | null = null;

    const pollWithDelay = async () => {
      if (!alive) return;

      try {
        await fetchDataRef.current();
      } catch (error) {
        // Error handling is done in the fetchData function
        console.error("Poll request failed:", error);
      }

      // Schedule next poll only if this effect run is still alive and
      // polling is enabled.
      if (alive && poll) {
        timeoutId = setTimeout(pollWithDelay, pollIntervalMs);
      }
    };

    // Start polling
    pollWithDelay();

    return () => {
      alive = false;
      if (timeoutId !== null) {
        clearTimeout(timeoutId);
        timeoutId = null;
      }
    };
  }, [enabled, params, poll, pollIntervalMs]);
};

export { usePoll };
