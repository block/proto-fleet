import { useCallback, useEffect, useMemo } from "react";
import type { MiningTarget } from "@/protoOS/api/generatedApi";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
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
  const { api } = useMinerHosting();
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
    if (!api) return;

    setPending(true);
    api
      .getMiningTarget()
      .then((res) => {
        setFromResponse(res);
      })
      .catch((err) => {
        setError(err?.error?.message ?? "An error occurred");
      });
  }, [api, setPending, setFromResponse, setError]);

  // Load initial data
  useEffect(() => {
    if (api && miningTarget === undefined && !pending) {
      fetchData();
    }
  }, [api, miningTarget, pending, fetchData]);

  // Update mining target
  const updateMiningTarget = useCallback(
    (newTarget: MiningTarget) => {
      if (!api) return;

      setPending(true);
      setError(null);
      authRetry({
        request: (header) => api.editMiningTarget(newTarget, header),
        onSuccess: (res) => setFromResponse(res),
        onError: (error) => {
          setPending(false);
          setError(error?.error?.message ?? "An error occurred");
        },
      });
    },
    [api, authRetry, setPending, setError, setFromResponse],
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
