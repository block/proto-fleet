import { useCallback, useEffect, useState } from "react";
import { create } from "@bufbuild/protobuf";
import { errorQueryClient } from "@/protoFleet/api/clients";
import { type DeviceError, QueryRequestSchema, ResultView } from "@/protoFleet/api/generated/errors/v1/errors_pb";
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
 */
export const useDeviceErrors = (deviceIds: string[]): UseDeviceErrorsReturn => {
  const { handleAuthErrors } = useAuthErrors();
  const [errors, setErrors] = useState<Record<string, DeviceError>>({});
  const [isLoading, setIsLoading] = useState(false);
  const [error, setError] = useState<Error | null>(null);

  // Update store when errors change
  const updateMinersErrorStatuses = useFleetStore((state) => state.fleet.updateMinersErrorStatuses);

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

        // Process the response based on result type
        if (response.result?.case === "devices" && response.result.value) {
          const deviceErrors = response.result.value.items;
          const errorMap: Record<string, DeviceError> = {};

          // Map errors by device ID directly
          deviceErrors.forEach((deviceError) => {
            const deviceId = deviceError.deviceIdentifier;
            if (deviceId) {
              errorMap[deviceId] = deviceError;
            }
          });

          setErrors(errorMap);

          // Update store with error statuses
          updateMinersErrorStatuses(errorMap);
        }
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
    [handleAuthErrors, updateMinersErrorStatuses],
  );

  // Fetch errors when device IDs change
  useEffect(() => {
    fetchDeviceErrors(deviceIds);
  }, [deviceIds, fetchDeviceErrors]);

  return {
    errors,
    isLoading,
    error,
    refetch: fetchDeviceErrors,
  };
};
