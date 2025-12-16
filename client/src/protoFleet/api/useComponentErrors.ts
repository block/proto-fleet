import { useCallback, useEffect, useRef, useState } from "react";
import { create } from "@bufbuild/protobuf";
import { errorQueryClient } from "@/protoFleet/api/clients";
import {
  type ComponentError,
  ComponentType,
  QueryRequestSchema,
  ResultView,
  type Summary,
  WatchRequestSchema,
  type WatchResponse,
  WatchResponse_Kind,
} from "@/protoFleet/api/generated/errors/v1/errors_pb";
import { useAuthErrors, useFleetStore } from "@/protoFleet/store";
import { streamCleanupManager } from "@/protoFleet/utils/streamCleanup";

interface ComponentErrorCounts {
  controlBoardErrors: number;
  fanErrors: number;
  hashboardErrors: number;
  psuErrors: number;
}

interface UseComponentErrorsReturn extends ComponentErrorCounts {
  isLoading: boolean;
  isStreaming: boolean;
  error: Error | null;
  refetch: () => Promise<void>;
}

/**
 * Hook to fetch and stream component error counts for the dashboard.
 * Performs an initial query and then subscribes to real-time updates.
 */
export const useComponentErrors = (): UseComponentErrorsReturn => {
  // Get auth loading state
  const authLoading = useFleetStore((state) => state.auth.authLoading);

  // State for error counts
  const [errorCounts, setErrorCounts] = useState<ComponentErrorCounts>({
    controlBoardErrors: 0,
    fanErrors: 0,
    hashboardErrors: 0,
    psuErrors: 0,
  });

  // Loading and error states
  const [isLoading, setIsLoading] = useState(true);
  const [isStreaming, setIsStreaming] = useState(false);
  const [error, setError] = useState<Error | null>(null);

  // Track devices with errors per component type
  const devicesByComponent = useRef<Map<ComponentType, Set<string>>>(
    new Map([
      [ComponentType.CONTROL_BOARD, new Set()],
      [ComponentType.FAN, new Set()],
      [ComponentType.HASH_BOARD, new Set()],
      [ComponentType.PSU, new Set()],
    ]),
  );

  // Abort controller for streaming
  const abortControllerRef = useRef<AbortController | null>(null);

  // Track if streaming has started
  const streamingStartedRef = useRef(false);

  // Auth error handler
  const { handleAuthErrors } = useAuthErrors();

  // Process component errors and update counts
  const processComponentErrors = useCallback((components: ComponentError[]) => {
    // Clear existing tracking
    devicesByComponent.current.forEach((deviceSet) => deviceSet.clear());

    // Track devices by component type
    components.forEach((component: ComponentError) => {
      const deviceSet = devicesByComponent.current.get(component.componentType);
      if (deviceSet) {
        // Add device ID to the set for this component type
        deviceSet.add(component.deviceIdentifier.toString());
      }
    });

    // Calculate counts from unique devices per component
    const counts = {
      controlBoardErrors: devicesByComponent.current.get(ComponentType.CONTROL_BOARD)?.size || 0,
      fanErrors: devicesByComponent.current.get(ComponentType.FAN)?.size || 0,
      hashboardErrors: devicesByComponent.current.get(ComponentType.HASH_BOARD)?.size || 0,
      psuErrors: devicesByComponent.current.get(ComponentType.PSU)?.size || 0,
    };
    setErrorCounts(counts);
  }, []);

  // Process streaming updates (incremental changes)
  const processStreamingUpdate = useCallback((response: WatchResponse, kind: WatchResponse_Kind) => {
    if (!response.result?.value) return;

    // Check if the result is components type
    if (response.result.case !== "components") return;

    // Get the component errors from the response
    const components = response.result.value.items || [];

    components.forEach((component) => {
      const deviceSet = devicesByComponent.current.get(component.componentType);
      if (!deviceSet) return;

      const deviceIdStr = component.deviceIdentifier.toString();

      if (kind === WatchResponse_Kind.OPENED) {
        // New error opened - add device to set
        deviceSet.add(deviceIdStr);
      } else if (kind === WatchResponse_Kind.CLOSED) {
        // Error closed - check if this device still has other errors for this component
        // For now, we'll need to re-query to be accurate
        // In a more sophisticated implementation, we'd track error IDs per device
        deviceSet.delete(deviceIdStr);
      }
      // KIND_UPDATED doesn't change the count (same device, error updated)
    });

    // Update counts based on current sets
    const updatedCounts = {
      controlBoardErrors: devicesByComponent.current.get(ComponentType.CONTROL_BOARD)?.size || 0,
      fanErrors: devicesByComponent.current.get(ComponentType.FAN)?.size || 0,
      hashboardErrors: devicesByComponent.current.get(ComponentType.HASH_BOARD)?.size || 0,
      psuErrors: devicesByComponent.current.get(ComponentType.PSU)?.size || 0,
    };
    setErrorCounts(updatedCounts);
  }, []);

  // Initial fetch function
  const fetchComponentErrors = useCallback(async () => {
    setIsLoading(true);
    setError(null);

    try {
      const request = create(QueryRequestSchema, {
        resultView: ResultView.COMPONENT,
        filter: {
          simple: {},
          includeClosed: false,
        },
        pageSize: 1000, // Should be enough for all component groups
      });

      const response = await errorQueryClient.query(request);

      // Process the response based on result type
      if (response.result?.case === "components" && response.result.value) {
        processComponentErrors(response.result.value.items);
      }
    } catch (err) {
      handleAuthErrors({
        error: err,
        onError: (error) => {
          console.error("Error fetching component errors:", error);
          setError(error instanceof Error ? error : new Error("Failed to fetch component errors"));
        },
      });
    } finally {
      setIsLoading(false);
    }
  }, [handleAuthErrors, processComponentErrors]);

  // Start streaming function - no dependencies to avoid infinite loops
  const startStreaming = useCallback(async () => {
    // Abort any existing stream
    if (abortControllerRef.current) {
      abortControllerRef.current.abort();
    }

    const controller = new AbortController();
    abortControllerRef.current = controller;

    // Register with cleanup manager for page unload handling
    streamCleanupManager.register(controller);

    setIsStreaming(true);

    try {
      const request = create(WatchRequestSchema, {
        filter: {
          simple: {},
          includeClosed: false,
        },
      });

      // Start the streaming loop
      (async () => {
        try {
          for await (const response of errorQueryClient.watch(request, {
            signal: controller.signal,
          })) {
            // Only process component view responses
            if (response.result?.case === "components" && response.kind) {
              processStreamingUpdate(response, response.kind);
            }
          }
        } catch (streamError) {
          if (!controller.signal.aborted) {
            handleAuthErrors({
              error: streamError,
              onError: (error) => {
                console.error("Error streaming component errors:", error);
                // Don't automatically retry - let the user refresh if needed
                // This prevents infinite retry loops on persistent errors
              },
            });
          }
        } finally {
          if (abortControllerRef.current === controller) {
            setIsStreaming(false);
          }
        }
      })();
    } catch (err) {
      console.error("Error starting error stream:", err);
      setIsStreaming(false);
    }
    // Remove dependencies to avoid infinite loops
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // Stop streaming function
  const stopStreaming = useCallback(() => {
    if (abortControllerRef.current) {
      abortControllerRef.current.abort();
      streamCleanupManager.unregister(abortControllerRef.current);
      abortControllerRef.current = null;
    }
    setIsStreaming(false);
  }, []);

  // Initial fetch on mount, but wait for auth to be ready
  useEffect(() => {
    // Don't fetch if auth is still loading
    if (authLoading) {
      return;
    }

    fetchComponentErrors();
    // Only run once on mount after auth is ready
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [authLoading]);

  // Start streaming after initial fetch completes
  useEffect(() => {
    // Track if component is mounted to prevent state updates after unmount
    let isMounted = true;

    // Don't start streaming if auth is still loading or data is still loading
    if (authLoading || isLoading) {
      return;
    }

    // Only start streaming once
    if (streamingStartedRef.current) {
      return;
    }

    streamingStartedRef.current = true;

    // Start stream asynchronously to avoid blocking render
    (async () => {
      if (isMounted) {
        await startStreaming();
      }
    })();

    return () => {
      isMounted = false;
      stopStreaming();
      streamingStartedRef.current = false;
    };
    // Only depend on authLoading and isLoading to avoid loops
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [authLoading, isLoading]);

  return {
    ...errorCounts,
    isLoading,
    isStreaming,
    error,
    refetch: fetchComponentErrors,
  };
};

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
          // No errors for this component - this is common with the mock API
          // since it randomly generates errors and may not have errors for every component
          setResult({
            summary: undefined, // No fallback - keep undefined if not from server
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
