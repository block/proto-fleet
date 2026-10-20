import { useCallback, useEffect, useState } from "react";
import { Code, ConnectError } from "@connectrpc/connect";
import { useAccountScope } from "./AccountScope";
import { isRetryableReadError } from "./api";
import { getErrorMessage } from "@/protoFleet/api/getErrorMessage";
import { useFleetStore } from "@/protoFleet/store";

export const useAccountQuery = <T>(
  load: (signal: AbortSignal) => Promise<T>,
  enabled = true,
  retainWhileRefreshing = false,
  pollIntervalMs = 0,
) => {
  const invalidateAccount = useAccountScope()?.invalidateAccount;
  const [revision, setRevision] = useState(0);
  const [result, setResult] = useState<{
    load: typeof load;
    revision: number;
    data: T | null;
    error: string | null;
  } | null>(null);
  const refresh = useCallback(() => setRevision((r) => r + 1), []);
  const invalidate = useCallback(() => {
    setResult(null);
    setRevision((r) => r + 1);
  }, []);
  useEffect(() => {
    if (!enabled) return;
    const controller = new AbortController();
    const generation = useFleetStore.getState().auth.sessionGeneration;
    load(controller.signal).then(
      (data) => {
        if (!controller.signal.aborted) setResult({ load, revision, data, error: null });
      },
      (error: unknown) => {
        if (!controller.signal.aborted) {
          setResult((previous) => ({
            load,
            revision,
            data:
              retainWhileRefreshing && isRetryableReadError(error) && previous?.load === load ? previous.data : null,
            error: getErrorMessage(error),
          }));
          if (ConnectError.from(error).code === Code.PermissionDenied) invalidateAccount?.();
          if (
            ConnectError.from(error).code === Code.Unauthenticated &&
            useFleetStore.getState().auth.sessionGeneration === generation
          )
            useFleetStore.getState().auth.logout();
        }
      },
    );
    return () => controller.abort();
  }, [load, enabled, revision, invalidateAccount, retainWhileRefreshing]);
  useEffect(() => {
    if (!enabled || !pollIntervalMs) return;
    const timer = window.setInterval(refresh, pollIntervalMs);
    return () => window.clearInterval(timer);
  }, [enabled, pollIntervalMs, refresh]);
  const current =
    enabled && result?.load === load && (result?.revision === revision || retainWhileRefreshing) ? result : null;
  return {
    data: current?.data ?? null,
    error: current?.error ?? null,
    loading: enabled && !current,
    refresh,
    invalidate,
  };
};
