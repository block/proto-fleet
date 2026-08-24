import { useCallback, useEffect, useMemo } from "react";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import {
  useAuthRetry,
  useMiningTargetBounds,
  useMiningTargetDefault,
  useMiningTargetError,
  useMiningTargetPending,
  useMiningTargetPerformanceMode,
  useMiningTargetStatus,
  useMiningTargetValue,
  useSetMiningTargetError,
  useSetMiningTargetFromResponse,
  useSetMiningTargetPending,
} from "@/protoOS/store";
import type { MiningTargetUpdate } from "@/protoOS/store/contract";
import { beginStoreWrite } from "@/protoOS/store/storeWriteGuard";

const useMiningTarget = () => {
  const { adapter } = useMinerHosting();
  const authRetry = useAuthRetry();

  // State selectors
  const miningTarget = useMiningTargetValue();
  const defaultTarget = useMiningTargetDefault();
  const performanceMode = useMiningTargetPerformanceMode();
  const bounds = useMiningTargetBounds();
  const status = useMiningTargetStatus();
  const pending = useMiningTargetPending();
  const error = useMiningTargetError();

  // Action selectors
  const setFromResponse = useSetMiningTargetFromResponse();
  const setPending = useSetMiningTargetPending();
  const setError = useSetMiningTargetError();

  // Fetch mining target data
  const fetchData = useCallback(() => {
    if (!adapter) return;

    setPending(true);
    const accept = beginStoreWrite("miningTarget:module:root");
    adapter
      .getMiningTarget()
      .then((target) => {
        if (accept()) setFromResponse(target);
      })
      .catch((err) => {
        if (accept()) setError(err?.error?.message ?? "An error occurred");
      });
  }, [adapter, setPending, setFromResponse, setError]);

  // Load initial data
  useEffect(() => {
    if (adapter && status === "idle") {
      fetchData();
    }
  }, [adapter, status, fetchData]);

  // Update mining target
  const updateMiningTarget = useCallback(
    (newTarget: MiningTargetUpdate) => {
      if (!adapter) return;

      // Clear any prior error first: setError(null) resolves the resource to
      // "ready", so it must run BEFORE setPending(true) — otherwise it clobbers
      // the "loading" status and the Apply button never shows its spinner while
      // the (multi-second) request is in flight.
      setError(null);
      setPending(true);
      const accept = beginStoreWrite("miningTarget:module:root");
      authRetry({
        request: (header) => adapter.updateMiningTarget(newTarget, header),
        onSuccess: (target) => {
          if (accept()) setFromResponse(target);
        },
        onError: (error) => {
          // Gate the error write behind the same token as onSuccess: a superseded
          // request must not clobber the state a newer request already settled.
          if (accept()) {
            setPending(false);
            setError(error?.error?.message ?? "An error occurred");
          }
        },
      });
    },
    [adapter, authRetry, setPending, setError, setFromResponse],
  );

  return useMemo(
    () => ({
      miningTarget,
      defaultTarget,
      performanceMode,
      bounds,
      pending,
      error,
      updateMiningTarget,
      setPending,
      // User-initiated retry: fetchData runs regardless of the current resource
      // status (only the initial-load effect gates on "idle"), so this leaves the
      // terminal "error" state on demand without reintroducing an automatic loop.
      retry: fetchData,
    }),
    [miningTarget, defaultTarget, performanceMode, bounds, pending, error, updateMiningTarget, setPending, fetchData],
  );
};

export { useMiningTarget };
