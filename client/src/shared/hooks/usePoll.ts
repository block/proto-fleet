import { useEffect, useRef } from "react";

import { isAbortError } from "@/shared/utils/abort";

interface UsePollProps {
  /**
   * Fetches one tick. Receives the effect run's `AbortSignal` — thread it into
   * the request (`request({ signal })` / adapter `{ signal }`) so navigation or
   * unmount cancels in-flight work instead of leaving it to saturate the
   * connection pool. The signal may be ignored by callers that make no request.
   */
  fetchData: (signal: AbortSignal) => Promise<void> | void;
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
    // One controller for this effect run. Cleanup aborts it, cancelling the
    // in-flight request so it stops running (and frees its HTTP connection)
    // the moment the user navigates away or toggles `enabled` — rather than
    // running to completion and saturating the pool behind the next view.
    const controller = new AbortController();

    const pollWithDelay = async () => {
      if (!alive) return;

      try {
        await fetchDataRef.current(controller.signal);
      } catch (error) {
        // An abort from our own cleanup is the expected teardown path, not a
        // failure. Everything else is handled inside fetchData; log the rest.
        if (!isAbortError(error)) console.error("Poll request failed:", error);
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
      controller.abort();
      if (timeoutId !== null) {
        clearTimeout(timeoutId);
        timeoutId = null;
      }
    };
  }, [enabled, params, poll, pollIntervalMs]);
};

export { usePoll };
