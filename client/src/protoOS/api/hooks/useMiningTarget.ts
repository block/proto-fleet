import { useCallback, useEffect, useMemo } from "react";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import type { MiningTargetUpdate } from "@/protoOS/store/contract";
import {
  useAuthRetry,
  useMiningTargetBounds,
  useMiningTargetDefault,
  useMiningTargetError,
  useMiningTargetPending,
  useMiningTargetPerformanceMode,
  useMiningTargetValue,
  useSetMiningTargetError,
  useSetMiningTargetFromResponse,
  useSetMiningTargetPending,
} from "@/protoOS/store";

const useMiningTarget = () => {
  const { adapter } = useMinerHosting();
  const authRetry = useAuthRetry();

  // State selectors
  const miningTarget = useMiningTargetValue();
  const defaultTarget = useMiningTargetDefault();
  const performanceMode = useMiningTargetPerformanceMode();
  const bounds = useMiningTargetBounds();
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
    adapter
      .getMiningTarget()
      .then(setFromResponse)
      .catch((err) => {
        setError(err?.error?.message ?? "An error occurred");
      });
  }, [adapter, setPending, setFromResponse, setError]);

  // Load initial data
  useEffect(() => {
    if (adapter && miningTarget === undefined && !pending) {
      fetchData();
    }
  }, [adapter, miningTarget, pending, fetchData]);

  // Update mining target
  const updateMiningTarget = useCallback(
    (newTarget: MiningTargetUpdate) => {
      if (!adapter) return;

      setPending(true);
      setError(null);
      authRetry({
        request: (header) => adapter.updateMiningTarget(newTarget, header),
        onSuccess: setFromResponse,
        onError: (error) => {
          setPending(false);
          setError(error?.error?.message ?? "An error occurred");
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
    }),
    [miningTarget, defaultTarget, performanceMode, bounds, pending, error, updateMiningTarget, setPending],
  );
};

export { useMiningTarget };
