import { useCallback, useEffect, useState } from "react";
import { create } from "@bufbuild/protobuf";
import { errorQueryClient } from "@/protoFleet/api/clients";
import {
  type DeviceError,
  type ErrorMessage,
  QueryRequestSchema,
  ResultView,
} from "@/protoFleet/api/generated/errors/v1/errors_pb";
import { useAuthErrors, useFleetStore } from "@/protoFleet/store";

interface UseDeviceErrorsReturn {
  errors: Record<string, DeviceError>;
  isLoading: boolean;
  error: Error | null;
  refetch: (deviceIds: string[]) => Promise<void>;
}

/**
 * Hook to fetch device errors for a list of miner IDs.
 * Returns error status mapped by device ID.
 * Uses the normalized error store for state management.
 */
export const useDeviceErrors = (deviceIds: string[]): UseDeviceErrorsReturn => {
  const { handleAuthErrors } = useAuthErrors();
  const [errors, setErrors] = useState<Record<string, DeviceError>>({});
  const [isLoading, setIsLoading] = useState(false);
  const [error, setError] = useState<Error | null>(null);

  // Get store actions
  const setNormalizedErrors = useFleetStore((state) => state.fleet.setErrors);

  const fetchDeviceErrors = useCallback(
    async (ids: string[]) => {
      if (!ids || ids.length === 0) {
        setErrors({});
        return;
      }

      setIsLoading(true);
      setError(null);

      try {
        const request = create(QueryRequestSchema, {
          resultView: ResultView.DEVICE,
          filter: {
            simple: {
              deviceIdentifiers: ids, // Use string device IDs directly
            },
            includeClosed: false,
          },
          pageSize: 1000, // Should be enough for all device errors
        });

        const response = await errorQueryClient.query(request);

        const errorMap: Record<string, DeviceError> = {};
        const allErrorMessages: ErrorMessage[] = [];

        if (response.result?.case === "devices" && response.result.value) {
          const deviceErrors = response.result.value.items;

          deviceErrors.forEach((deviceError) => {
            const deviceId = deviceError.deviceIdentifier;
            if (deviceId) {
              errorMap[deviceId] = deviceError;
              if (deviceError.errors) {
                allErrorMessages.push(...deviceError.errors);
              }
            }
          });
        }

        setErrors(errorMap);
        setNormalizedErrors(allErrorMessages, "devices", ids);
      } catch (err) {
        handleAuthErrors({
          error: err,
          onError: (error) => {
            console.error("Error fetching device errors:", error);
            setError(error instanceof Error ? error : new Error("Failed to fetch device errors"));
          },
        });
      } finally {
        setIsLoading(false);
      }
    },
    [handleAuthErrors, setNormalizedErrors],
  );

  // Fetch errors when device IDs change
  useEffect(() => {
    fetchDeviceErrors(deviceIds);
    // Only depend on deviceIds - the store actions are stable
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [deviceIds]);

  return {
    errors,
    isLoading,
    error,
    refetch: fetchDeviceErrors,
  };
};
