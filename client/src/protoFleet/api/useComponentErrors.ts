import { useCallback, useEffect, useRef, useState } from "react";
import { create } from "@bufbuild/protobuf";
import { errorQueryClient } from "@/protoFleet/api/clients";
import {
  type ComponentError,
  ComponentType,
  QueryRequestSchema,
  ResultView,
  type Summary,
} from "@/protoFleet/api/generated/errors/v1/errors_pb";
import { useAuthErrors, useFleetStore } from "@/protoFleet/store";

interface ComponentErrorCounts {
  controlBoardErrors: number;
  fanErrors: number;
  hashboardErrors: number;
  psuErrors: number;
}

interface UseComponentErrorsReturn extends ComponentErrorCounts {
  isLoading: boolean;
  hasLoaded: boolean;
  error: Error | null;
  refetch: () => Promise<void>;
}

interface UseComponentErrorsOptions {
  /** Optional device identifiers to scope errors to specific devices (e.g., a group's members) */
  deviceIdentifiers?: string[];
  /** Optional polling interval in milliseconds */
  pollIntervalMs?: number;
}

/**
 * Hook to fetch component error counts.
 * Manages its own local state — no dashboard store dependency.
 * Supports optional polling for periodic refresh.
 */
export const useComponentErrors = (options?: UseComponentErrorsOptions): UseComponentErrorsReturn => {
  const deviceIdentifiers = options?.deviceIdentifiers;
  const isEmptyScope = deviceIdentifiers !== undefined && deviceIdentifiers.length === 0;
  const deviceIdentifiersKey = deviceIdentifiers === undefined ? "__undefined__" : deviceIdentifiers.join(",");

  const authLoading = useFleetStore((state) => state.auth.authLoading);
  const { handleAuthErrors } = useAuthErrors();

  // Ref so fetchComponentErrors reads latest deviceIdentifiers without needing it as a dependency
  const deviceIdentifiersRef = useRef(deviceIdentifiers);
  useEffect(() => {
    deviceIdentifiersRef.current = deviceIdentifiers;
  });

  // Local state for error counts
  const [counts, setCounts] = useState<Partial<Record<ComponentType, number>>>({});
  const [isLoading, setIsLoading] = useState(true);
  const [hasLoaded, setHasLoaded] = useState(false);
  const [error, setError] = useState<Error | null>(null);

  const requestIdRef = useRef(0);
  const hasLoadedRef = useRef(false);

  // Reset on scope change — invalidate in-flight requests so stale responses can't land.
  // Driven via useState "adjust during render" pattern so React renders with the reset
  // values in the same pass that detects the change (avoids a flash of stale data).
  const [prevScope, setPrevScope] = useState(deviceIdentifiersKey);
  if (prevScope !== deviceIdentifiersKey) {
    setPrevScope(deviceIdentifiersKey);
    setHasLoaded(false);
    setCounts({});
  }
  useEffect(() => {
    hasLoadedRef.current = false;
    ++requestIdRef.current;
  }, [deviceIdentifiersKey]);

  const errorCounts: ComponentErrorCounts = {
    controlBoardErrors: counts[ComponentType.CONTROL_BOARD] || 0,
    fanErrors: counts[ComponentType.FAN] || 0,
    hashboardErrors: counts[ComponentType.HASH_BOARD] || 0,
    psuErrors: counts[ComponentType.PSU] || 0,
  };

  const fetchComponentErrors = useCallback(async () => {
    if (isEmptyScope) {
      ++requestIdRef.current;
      setCounts({});
      setIsLoading(false);
      return;
    }

    const thisRequestId = ++requestIdRef.current;

    if (!hasLoadedRef.current) {
      setIsLoading(true);
    }
    setError(null);

    try {
      const currentDeviceIdentifiers = deviceIdentifiersRef.current;
      const request = create(QueryRequestSchema, {
        resultView: ResultView.COMPONENT,
        filter: {
          simple: {
            ...(currentDeviceIdentifiers &&
              currentDeviceIdentifiers.length > 0 && { deviceIdentifiers: currentDeviceIdentifiers }),
          },
          includeClosed: false,
        },
        pageSize: 1000,
      });

      const response = await errorQueryClient.query(request);

      if (thisRequestId !== requestIdRef.current) return;

      if (response.result?.case === "components" && response.result.value) {
        const newCounts = processComponentErrorCounts(response.result.value.items);
        setCounts(newCounts);
      } else {
        setCounts({});
      }
      hasLoadedRef.current = true;
      setHasLoaded(true);
    } catch (err) {
      if (thisRequestId !== requestIdRef.current) return;
      handleAuthErrors({
        error: err,
        onError: (error) => {
          console.error("Error fetching component errors:", error);
          setError(error instanceof Error ? error : new Error("Failed to fetch component errors"));
        },
      });
    } finally {
      if (thisRequestId === requestIdRef.current) {
        setIsLoading(false);
      }
    }
  }, [handleAuthErrors, isEmptyScope]);

  // Initial fetch + refetch on scope change
  useEffect(() => {
    if (authLoading) return;
    fetchComponentErrors();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [authLoading, deviceIdentifiersKey]);

  // Polling
  useEffect(() => {
    if (!options?.pollIntervalMs || authLoading) return;

    const intervalId = setInterval(() => {
      void fetchComponentErrors();
    }, options.pollIntervalMs);

    return () => clearInterval(intervalId);
  }, [options?.pollIntervalMs, authLoading, fetchComponentErrors]);

  return {
    ...errorCounts,
    isLoading,
    hasLoaded,
    error,
    refetch: fetchComponentErrors,
  };
};

/** Count unique devices per component type from query response */
function processComponentErrorCounts(components: ComponentError[]): Partial<Record<ComponentType, number>> {
  const devicesByComponentType: Partial<Record<ComponentType, Set<string>>> = {};

  components.forEach((component) => {
    if (
      component.componentType !== undefined &&
      component.deviceIdentifier &&
      component.errors &&
      component.errors.length > 0
    ) {
      if (!devicesByComponentType[component.componentType]) {
        devicesByComponentType[component.componentType] = new Set();
      }
      devicesByComponentType[component.componentType]!.add(component.deviceIdentifier);
    }
  });

  const counts: Partial<Record<ComponentType, number>> = {};
  Object.entries(devicesByComponentType).forEach(([type, devices]) => {
    counts[Number(type) as ComponentType] = devices.size;
  });
  return counts;
}

// Additional types and hook for fetching specific component error details
interface ComponentErrorDetailResult {
  summary?: Summary;
  componentError?: ComponentError;
  loading: boolean;
  errorMessage?: string;
}

/**
 * Hook to fetch a specific component's errors and summary from the errors API.
 * This is used when navigating to a component view in the StatusModal.
 * @param deviceIdentifier - UUID of the device
 * @param componentId - Full component ID (e.g., "1_hashboard_0")
 * @param enabled - Whether to fetch (default true)
 */
export const useComponentErrorDetail = (
  deviceIdentifier: string | undefined,
  componentId: string | undefined,
  enabled = true,
): ComponentErrorDetailResult => {
  const [result, setResult] = useState<ComponentErrorDetailResult>({
    loading: false,
  });

  const { handleAuthErrors } = useAuthErrors();

  useEffect(() => {
    if (!deviceIdentifier || !componentId || !enabled) {
      return;
    }

    const fetchComponentDetail = async () => {
      setResult((prev) => ({ ...prev, loading: true }));

      try {
        // Create query request for component view
        const request = create(QueryRequestSchema, {
          resultView: ResultView.COMPONENT,
          filter: {
            simple: {
              deviceIdentifiers: [deviceIdentifier],
              componentIds: [componentId],
            },
          },
          pageSize: 100, // Increase to ensure we get all components
        });

        const response = await errorQueryClient.query(request);

        // Extract component error from response
        if (response.result?.case === "components" && response.result.value?.items?.length > 0) {
          const componentError = response.result.value.items[0];

          setResult({
            summary: componentError.summary,
            componentError,
            loading: false,
          });
        } else {
          setResult({
            summary: undefined,
            componentError: undefined,
            loading: false,
          });
        }
      } catch (err) {
        handleAuthErrors({
          error: err,
          onError: (error) => {
            console.error("Failed to fetch component error detail:", error);
            setResult({
              loading: false,
              errorMessage: error instanceof Error ? error.message : "Failed to fetch component errors",
            });
          },
        });
      }
    };

    fetchComponentDetail();
  }, [deviceIdentifier, componentId, enabled, handleAuthErrors]);

  return result;
};
