import { useCallback, useEffect, useRef, useState } from "react";
import { create } from "@bufbuild/protobuf";
import { errorQueryClient } from "@/protoFleet/api/clients";
import {
  type DeviceError,
  type ErrorMessage,
  QueryRequestSchema,
  ResultView,
} from "@/protoFleet/api/generated/errors/v1/errors_pb";
import { useAuthErrors } from "@/protoFleet/store";

interface UseDeviceErrorsReturn {
  errorsByDevice: Record<string, ErrorMessage[]>;
  isLoading: boolean;
  /** True once at least one successful fetch has completed. */
  hasLoaded: boolean;
  error: Error | null;
  refetch: () => Promise<void>;
}

/**
 * Hook to fetch device errors for a list of miner IDs.
 * Returns errors grouped by device ID. All state is local to this hook.
 */
export const useDeviceErrors = (deviceIds: string[]): UseDeviceErrorsReturn => {
  const { handleAuthErrors } = useAuthErrors();
  const [errorsByDevice, setErrorsByDevice] = useState<Record<string, ErrorMessage[]>>({});
  const [isLoading, setIsLoading] = useState(false);
  const [hasLoaded, setHasLoaded] = useState(false);
  const [error, setError] = useState<Error | null>(null);
  // Ref mirror of hasLoaded — used inside fetchDeviceErrors to gate isLoading
  const hasLoadedRef = useRef(false);

  // Keep a ref to deviceIds so refetch() always uses the latest value
  const deviceIdsRef = useRef(deviceIds);
  useEffect(() => {
    deviceIdsRef.current = deviceIds;
  });

  // Request sequencing — ignore responses from stale requests
  const requestIdRef = useRef(0);

  const fetchDeviceErrors = useCallback(
    async (ids: string[]) => {
      // Bump counter before any early return so in-flight requests for the
      // previous device set are discarded when they resolve.
      const thisRequestId = ++requestIdRef.current;

      if (!ids || ids.length === 0) {
        setErrorsByDevice({});
        setIsLoading(false);
        return;
      }

      // Only show loading state on initial fetch, not on poll refreshes
      if (!hasLoadedRef.current) {
        setIsLoading(true);
      }
      setError(null);

      try {
        const request = create(QueryRequestSchema, {
          resultView: ResultView.DEVICE,
          filter: {
            simple: {
              deviceIdentifiers: ids,
            },
            includeClosed: false,
          },
          pageSize: 1000,
        });

        const response = await errorQueryClient.query(request);

        // Discard if a newer request has been issued since this one started
        if (thisRequestId !== requestIdRef.current) return;

        const byDevice: Record<string, ErrorMessage[]> = {};

        if (response.result?.case === "devices" && response.result.value) {
          const deviceErrors = response.result.value.items;

          deviceErrors.forEach((deviceError: DeviceError) => {
            const deviceId = deviceError.deviceIdentifier;
            if (deviceId && deviceError.errors) {
              byDevice[deviceId] = [...deviceError.errors];
            }
          });
        }

        // Only update state if error data actually changed — avoids unnecessary
        // re-renders of MinerList/deviceItems on every poll when errors are unchanged.
        setErrorsByDevice((prev) => {
          const prevKeys = Object.keys(prev);
          const nextKeys = Object.keys(byDevice);
          if (prevKeys.length !== nextKeys.length) return byDevice;
          for (const key of nextKeys) {
            const prevErrors = prev[key];
            const nextErrors = byDevice[key];
            if (!prevErrors || prevErrors.length !== nextErrors.length) return byDevice;
            // Compare error IDs to catch type/content changes at the same count
            for (let i = 0; i < nextErrors.length; i++) {
              if (prevErrors[i].errorId !== nextErrors[i].errorId) return byDevice;
            }
          }
          return prev;
        });

        hasLoadedRef.current = true;
        setHasLoaded(true);
      } catch (err) {
        // Discard errors from stale requests
        if (thisRequestId !== requestIdRef.current) return;

        handleAuthErrors({
          error: err,
          onError: (error) => {
            console.error("Error fetching device errors:", error);
            setError(error instanceof Error ? error : new Error("Failed to fetch device errors"));
          },
        });
      } finally {
        if (thisRequestId === requestIdRef.current) {
          setIsLoading(false);
        }
      }
    },
    [handleAuthErrors],
  );

  // Track the previous deviceIds to detect meaningful changes (not just poll refreshes)
  const prevDeviceIdsRef = useRef<string[]>(deviceIds);

  // Fetch errors when device IDs change
  useEffect(() => {
    // Reset loading state when the device list actually changes (pagination/filter),
    // but not on the same list (poll refreshes are handled by refetch which skips loading).
    const prevIds = prevDeviceIdsRef.current;
    const idsChanged = prevIds.length !== deviceIds.length || deviceIds.some((id, i) => id !== prevIds[i]);
    if (idsChanged) {
      hasLoadedRef.current = false;
      setHasLoaded(false);
    }
    prevDeviceIdsRef.current = deviceIds;

    // eslint-disable-next-line react-hooks/set-state-in-effect -- refetch on deviceIds change; setState inside async fetch is the external-sync pattern
    fetchDeviceErrors(deviceIds);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [deviceIds]);

  // Stable refetch that uses the latest deviceIds
  const refetch = useCallback(async () => {
    await fetchDeviceErrors(deviceIdsRef.current);
  }, [fetchDeviceErrors]);

  return {
    errorsByDevice,
    isLoading,
    hasLoaded,
    error,
    refetch,
  };
};
