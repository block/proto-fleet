import { useCallback, useEffect, useRef, useState } from "react";
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
    errorCode: Code | null;
  } | null>(null);
  const inFlight = useRef<AbortController | null>(null);
  const refresh = useCallback(() => setRevision((r) => r + 1), []);
  // Background refreshes never abort a load that is still running, so a slow
  // multi-request load can settle instead of restarting on every tick.
  const poll = useCallback(() => {
    if (!inFlight.current) refresh();
  }, [refresh]);
  const invalidate = useCallback(() => {
    setResult(null);
    setRevision((r) => r + 1);
  }, []);
  useEffect(() => {
    if (!enabled) return;
    const controller = new AbortController();
    const generation = useFleetStore.getState().auth.sessionGeneration;
    inFlight.current = controller;
    const settle = () => {
      if (inFlight.current === controller) inFlight.current = null;
    };
    load(controller.signal).then(
      (data) => {
        settle();
        if (!controller.signal.aborted) setResult({ load, revision, data, error: null, errorCode: null });
      },
      (error: unknown) => {
        settle();
        if (!controller.signal.aborted) {
          setResult((previous) => ({
            load,
            revision,
            data:
              retainWhileRefreshing && isRetryableReadError(error) && previous?.load === load ? previous.data : null,
            error: getErrorMessage(error),
            errorCode: ConnectError.from(error).code,
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
    return () => {
      settle();
      controller.abort();
    };
  }, [load, enabled, revision, invalidateAccount, retainWhileRefreshing]);
  useEffect(() => {
    if (!enabled || !pollIntervalMs) return;
    const timer = window.setInterval(poll, pollIntervalMs);
    return () => window.clearInterval(timer);
  }, [enabled, pollIntervalMs, poll]);
  const current =
    enabled && result?.load === load && (result?.revision === revision || retainWhileRefreshing) ? result : null;
  return {
    data: current?.data ?? null,
    error: current?.error ?? null,
    errorCode: current?.errorCode ?? null,
    loading: enabled && !current,
    refresh,
    poll,
    invalidate,
  };
};
