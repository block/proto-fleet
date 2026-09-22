import { useCallback, useEffect, useMemo, useRef, useState } from "react";

import { useFleetStore, useIsAuthenticated, useSessionGeneration, useUsername } from "@/protoFleet/store";

// One complete read at a time. Changing the reader or login invalidates its
// data; refreshes within that context retain the last complete result.
export function useRefreshingRead<T>({
  read,
  errorMessage,
  refreshKey,
  trailingRefresh = false,
  enabled = true,
  timeoutMs,
  timeoutMessage,
}: {
  read: (signal: AbortSignal) => Promise<T>;
  errorMessage: string;
  refreshKey?: unknown;
  trailingRefresh?: boolean;
  enabled?: boolean;
  timeoutMs?: number;
  timeoutMessage?: string;
}) {
  const username = useUsername();
  const sessionGeneration = useSessionGeneration();
  const isAuthenticated = useIsAuthenticated();
  const identity = JSON.stringify([username, sessionGeneration, isAuthenticated]);
  const context = useMemo(() => ({ read, identity, enabled }), [read, identity, enabled]);
  const [snapshot, setSnapshot] = useState({
    context,
    data: null as T | null,
    error: null as string | null,
    isLoading: true,
  });
  const controls = useRef<{ refresh: () => void; cancel: () => void } | null>(null);
  const refresh = useCallback(() => controls.current?.refresh(), []);
  const cancel = useCallback(() => controls.current?.cancel(), []);

  useEffect(() => {
    let canceled = false;
    let queued = false;
    let controller: AbortController | null = null;
    let timeout: ReturnType<typeof setTimeout> | undefined;
    const isCurrent = () => {
      const auth = useFleetStore.getState().auth;
      return (
        !canceled &&
        context.enabled &&
        JSON.stringify([auth.username, auth.sessionGeneration, auth.isAuthenticated]) === context.identity
      );
    };
    const load = () => {
      if (!isCurrent()) return;
      if (controller) {
        queued = trailingRefresh;
        return;
      }
      controller = new AbortController();
      const { signal } = controller;
      if (timeoutMs !== undefined) {
        const requestController = controller;
        timeout = setTimeout(() => requestController.abort(new Error(timeoutMessage)), timeoutMs);
      }
      setSnapshot((previous) => ({
        ...(previous.context === context ? previous : { context, data: null, error: null }),
        isLoading: true,
      }));
      void (async () => {
        try {
          const data = await context.read(signal);
          signal.throwIfAborted();
          if (isCurrent()) setSnapshot({ context, data, error: null, isLoading: false });
        } catch (error) {
          if (isCurrent())
            setSnapshot((previous) => ({
              ...previous,
              isLoading: false,
              error: error instanceof Error && error.message ? error.message : errorMessage,
            }));
        } finally {
          clearTimeout(timeout);
          controller = null;
          if (queued) {
            queued = false;
            load();
          }
        }
      })();
    };
    const stop = () => {
      canceled = true;
      queued = false;
      clearTimeout(timeout);
      controller?.abort();
    };
    controls.current = { refresh: load, cancel: stop };
    return () => {
      stop();
      controls.current = null;
    };
  }, [context, errorMessage, timeoutMs, timeoutMessage, trailingRefresh]);

  useEffect(() => {
    // Summary changes queue a refresh without canceling a slow complete scan.
    refresh();
  }, [context, errorMessage, timeoutMs, timeoutMessage, trailingRefresh, refreshKey, refresh]);

  const current = snapshot.context === context ? snapshot : undefined;
  return {
    data: current?.data ?? null,
    error: current?.error ?? null,
    isLoading: enabled && (current?.isLoading ?? true),
    refresh,
    cancel,
  };
}
