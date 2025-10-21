import { useCallback, useEffect, useMemo } from "react";
import type { MiningTarget } from "@/protoOS/api/generatedApi";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import {
  useAuthErrors,
  useAuthHeader,
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
  const authHeader = useAuthHeader();
  const { handleAuthErrors } = useAuthErrors();

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
        handleAuthErrors({
          error: err,
          onError: (error) => {
            setError(error?.error?.message ?? "An error occurred");
          },
          onSuccess: () => {
            // Retry fetch after successful token refresh
            fetchData();
          },
        });
      });
  }, [api, setPending, setFromResponse, setError, handleAuthErrors]);

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

      api
        .editMiningTarget(newTarget, authHeader)
        .then((res) => {
          setFromResponse(res);
        })
        .catch((err) => {
          handleAuthErrors({
            error: err,
            onError: (error) => {
              setError(error?.error?.message ?? "An error occurred");
            },
            onSuccess: () => {
              // Refresh worked! Retry now
              updateMiningTarget(newTarget);
            },
          });
        });
    },
    [api, authHeader, handleAuthErrors, setPending, setError, setFromResponse],
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
    [
      miningTarget,
      defaultTarget,
      performanceMode,
      bounds,
      pending,
      error,
      updateMiningTarget,
      setPending,
    ],
  );
};

export { useMiningTarget };
