import { useState } from "react";
import type { AdapterRequestOptions } from "@/protoOS/api/adapters/types";
import { useAuthRetry } from "@/protoOS/store/hooks/useAuthRetry";
import { usePoll } from "@/shared/hooks/usePoll";

export const CONTROLLER_RECORDS_POLL_MS = 10_000;

interface ReadState<Selection, Data> {
  selection: Selection;
  data?: Data;
  error?: string;
}

const errorMessage = (error: unknown) => {
  const value = error as { error?: { message?: string }; message?: string } | undefined;
  return value?.error?.message || value?.message || "The controller did not respond.";
};

/**
 * Polls `read` for a memoized `selection`. A failed refresh keeps the last
 * result for the same selection and reports the error, so callers can tell
 * stale data (data and error) from unavailable data (error only).
 *
 * `inconclusive` names results that say nothing either way (it returns the
 * reason). A first read shows them; a refresh treats them as a failure, so
 * conclusive data already on screen is kept and marked stale.
 */
export function usePolledRead<Selection, Data>(
  selection: Selection,
  read: ((selection: Selection, options: AdapterRequestOptions) => Promise<Data>) | undefined,
  inconclusive?: (data: Data) => string | undefined,
) {
  const authRetry = useAuthRetry();
  const [state, setState] = useState<ReadState<Selection, Data>>();
  usePoll({
    params: selection,
    enabled: Boolean(read),
    poll: true,
    pollIntervalMs: CONTROLLER_RECORDS_POLL_MS,
    fetchData: async (signal) => {
      if (!read) return;
      await authRetry({
        signal,
        request: (options) => read(selection, options),
        onSuccess: (data) => {
          if (signal.aborted) return;
          setState((previous) => {
            const reason = inconclusive?.(data);
            const kept = previous?.selection === selection ? previous.data : undefined;
            return reason && kept !== undefined && !inconclusive?.(kept)
              ? { selection, data: kept, error: reason }
              : { selection, data };
          });
        },
        onError: (error) => {
          if (signal.aborted) return;
          setState((previous) => ({
            selection,
            data: previous?.selection === selection ? previous.data : undefined,
            error: errorMessage(error),
          }));
        },
      });
    },
  });
  const current = state?.selection === selection ? state : undefined;
  return { data: current?.data, error: current?.error, loading: current === undefined };
}
