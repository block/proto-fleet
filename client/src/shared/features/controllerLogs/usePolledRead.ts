import { useCallback, useMemo, useState } from "react";
import type { ControllerRecordsRead } from "./types";
import { usePoll } from "@/shared/hooks/usePoll";

interface ReadState<Selection, Data> {
  selection: Selection;
  data?: Data;
  error?: string;
}

// Accepts adapter errors ({ error: { message } }), Connect errors (rawMessage,
// without the "[code]" prefix) and plain errors.
const errorMessage = (error: unknown) => {
  const value = error as { error?: { message?: string }; rawMessage?: string; message?: string } | undefined;
  return value?.error?.message || value?.rawMessage || value?.message || "The controller did not respond.";
};

interface PolledReadOptions<Data> {
  /**
   * Names results that say nothing either way (it returns the reason). A first
   * read shows them; a refresh treats them as a failure, so conclusive data
   * already on screen is kept and marked stale.
   */
  inconclusive?: (data: Data) => string | undefined;
  /** Re-read on this interval. Omitted means read once per selection and on `refresh`. */
  pollIntervalMs?: number;
}

/**
 * Reads `read` for a memoized `selection`. A failed refresh keeps the last
 * result for the same selection and reports the error, so callers can tell
 * stale data (data and error) from unavailable data (error only).
 */
export function usePolledRead<Selection, Data>(
  selection: Selection,
  read: ControllerRecordsRead<Selection, Data> | undefined,
  { inconclusive, pollIntervalMs }: PolledReadOptions<Data> = {},
) {
  const [state, setState] = useState<ReadState<Selection, Data>>();
  const [revision, setRevision] = useState(0);
  const [reading, setReading] = useState(false);
  const params = useMemo(() => ({ selection, revision }), [selection, revision]);
  usePoll({
    params,
    enabled: Boolean(read),
    poll: pollIntervalMs !== undefined,
    pollIntervalMs,
    fetchData: async (signal) => {
      if (!read) return;
      setReading(true);
      try {
        const data = await read(selection, signal);
        if (signal.aborted) return;
        setState((previous) => {
          const reason = inconclusive?.(data);
          const kept = previous?.selection === selection ? previous.data : undefined;
          return reason && kept !== undefined && !inconclusive?.(kept)
            ? { selection, data: kept, error: reason }
            : { selection, data };
        });
      } catch (error) {
        if (signal.aborted) return;
        setState((previous) => ({
          selection,
          data: previous?.selection === selection ? previous.data : undefined,
          error: errorMessage(error),
        }));
      } finally {
        if (!signal.aborted) setReading(false);
      }
    },
  });
  const refresh = useCallback(() => setRevision((value) => value + 1), []);
  const current = state?.selection === selection ? state : undefined;
  return { data: current?.data, error: current?.error, loading: current === undefined, reading, refresh };
}
