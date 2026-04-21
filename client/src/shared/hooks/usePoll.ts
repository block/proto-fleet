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
  const timeoutRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const isMountedRef = useRef(true);
  const fetchDataRef = useRef(fetchData);

  // Keep fetchData ref up to date
  // store this in a ref to avoid re-running the effect below on every
  // render in the case that usePoll is called inline without memoizing fetchData
  useEffect(() => {
    fetchDataRef.current = fetchData;
  }, [fetchData]);

  useEffect(() => {
    isMountedRef.current = true;

    if (!enabled) {
      return () => {
        isMountedRef.current = false;
        if (timeoutRef.current) {
          clearTimeout(timeoutRef.current);
          timeoutRef.current = null;
        }
      };
    }

    const pollWithDelay = async () => {
      if (!isMountedRef.current) return;

      try {
        await fetchDataRef.current();
      } catch (error) {
        // Error handling is done in the fetchData function
        console.error("Poll request failed:", error);
      }

      // Only schedule next poll if still mounted and polling is enabled
      if (isMountedRef.current && poll) {
        timeoutRef.current = setTimeout(pollWithDelay, pollIntervalMs);
      }
    };

    // Start polling
    pollWithDelay();

    return () => {
      isMountedRef.current = false;
      if (timeoutRef.current) {
        clearTimeout(timeoutRef.current);
        timeoutRef.current = null;
      }
    };
  }, [enabled, params, poll, pollIntervalMs]);
};

export { usePoll };
