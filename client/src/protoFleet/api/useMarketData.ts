import { useCallback, useEffect, useState } from "react";
import { Code, ConnectError } from "@connectrpc/connect";
import { marketDataClient } from "@/protoFleet/api/clients";
import type { GetMarketDataResponse } from "@/protoFleet/api/generated/marketdata/v1/marketdata_pb";
import { useAuthErrors } from "@/protoFleet/store";

export const MARKET_DATA_POLL_MS = 60_000;

export const useMarketData = () => {
  const { handleAuthErrors } = useAuthErrors();
  const [attempt, setAttempt] = useState(0);
  const [state, setState] = useState<{
    data?: GetMarketDataResponse;
    error: boolean;
    now: number;
  }>(() => ({ error: false, now: Date.now() }));

  useEffect(() => {
    let disposed = false;
    let pending = false;
    let enabled = true;
    let timer: ReturnType<typeof setTimeout>;
    const controller = new AbortController();
    const isVisible = () => document.visibilityState === "visible";

    const fetchData = async () => {
      if (pending || disposed || !enabled || !isVisible()) return;
      clearTimeout(timer);
      pending = true;
      try {
        const data = await marketDataClient.getMarketData({}, { signal: controller.signal, timeoutMs: 12_000 });
        enabled = data.enabled;
        if (!disposed) setState({ data, error: false, now: Date.now() });
      } catch (error) {
        if (!disposed) {
          if (error instanceof ConnectError && error.code === Code.PermissionDenied) {
            // Org-only client permissions cannot represent site-scoped readers.
            // Use the server's capability verdict and stop polling denied callers.
            enabled = false;
            setState({ error: false, now: Date.now() });
          } else {
            handleAuthErrors({ error });
            setState((previous) => ({ ...previous, error: true, now: Date.now() }));
          }
        }
      } finally {
        pending = false;
        if (!disposed && enabled && isVisible()) {
          timer = setTimeout(fetchData, MARKET_DATA_POLL_MS);
        }
      }
    };

    const onVisibilityChange = () => {
      clearTimeout(timer);
      void fetchData();
    };

    // eslint-disable-next-line react-hooks/set-state-in-effect -- synchronize an external RPC on mount/retry; state updates occur after the async request
    void fetchData();
    document.addEventListener("visibilitychange", onVisibilityChange);
    return () => {
      disposed = true;
      controller.abort();
      clearTimeout(timer);
      document.removeEventListener("visibilitychange", onVisibilityChange);
    };
  }, [attempt, handleAuthErrors]);

  const refetch = useCallback(() => setAttempt((value) => value + 1), []);
  return { ...state, refetch };
};
