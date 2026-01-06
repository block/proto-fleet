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
 * Uses the dashboard slice for component error count tracking.
 */
export const useComponentErrors = (): UseComponentErrorsReturn => {
  // Get auth loading state
  const authLoading = useFleetStore((state) => state.auth.authLoading);

  // Dashboard store actions and state
  const componentErrorCounts = useFleetStore((state) => state.dashboard.componentErrors.counts);
  const setComponentErrorCounts = useFleetStore((state) => state.dashboard.setComponentErrorCounts);
  const handleComponentErrorStream = useFleetStore((state) => state.dashboard.handleComponentErrorStream);
  const clearComponentErrors = useFleetStore((state) => state.dashboard.clearComponentErrors);

  // Get error counts from dashboard store
  const errorCounts: ComponentErrorCounts = {
    controlBoardErrors: componentErrorCounts[ComponentType.CONTROL_BOARD] || 0,
    fanErrors: componentErrorCounts[ComponentType.FAN] || 0,
    hashboardErrors: componentErrorCounts[ComponentType.HASH_BOARD] || 0,
    psuErrors: componentErrorCounts[ComponentType.PSU] || 0,
  };

  // Loading and error states
  const [isLoading, setIsLoading] = useState(true);
  const [isStreaming, setIsStreaming] = useState(false);
  const [error, setError] = useState<Error | null>(null);

  // Abort controller for streaming
  const abortControllerRef = useRef<AbortController | null>(null);

  // Track if streaming has started
  const streamingStartedRef = useRef(false);

  // Auth error handler
  const { handleAuthErrors } = useAuthErrors();

  // Process component errors and update dashboard store with counts
  const processComponentErrors = useCallback(
    (components: ComponentError[]) => {
      // Track unique devices per component type using Sets
      const devicesByComponentType: Partial<Record<ComponentType, Set<string>>> = {};
      // Map of component -> device -> error IDs for proper tracking
      const deviceErrorMap: Partial<Record<ComponentType, Record<string, string[]>>> = {};

      components.forEach((component: ComponentError) => {
        if (
          component.componentType !== undefined &&
          component.deviceIdentifier &&
          component.errors &&
          component.errors.length > 0
        ) {
          // Track unique devices per component type
          if (!devicesByComponentType[component.componentType]) {
            devicesByComponentType[component.componentType] = new Set();
          }
          devicesByComponentType[component.componentType]!.add(component.deviceIdentifier);

          // Build device-error map for streaming update tracking
          if (!deviceErrorMap[component.componentType]) {
            deviceErrorMap[component.componentType] = {};
          }
          // Merge error IDs if device already has errors for this component type
          const existingErrors = deviceErrorMap[component.componentType]![component.deviceIdentifier] || [];
          const newErrors = component.errors.map((error) => error.errorId);
          deviceErrorMap[component.componentType]![component.deviceIdentifier] = [...existingErrors, ...newErrors];
        }
      });

      // Convert Sets to counts (number of unique devices per component type)
      const counts: Partial<Record<ComponentType, number>> = {};
      Object.entries(devicesByComponentType).forEach(([type, devices]) => {
        counts[Number(type) as ComponentType] = devices.size;
      });

      // Update the dashboard store with counts and device-error map
      setComponentErrorCounts(counts, deviceErrorMap);
    },
    [setComponentErrorCounts],
  );

  // Process streaming updates (incremental changes)
  const processStreamingUpdate = useCallback(
    (response: WatchResponse, kind: WatchResponse_Kind) => {
      if (!response.result?.value) return;

      // Check if the result is components type
      if (response.result.case !== "components") return;

      // Get the component errors from the response
      const components = response.result.value.items || [];

      // Process each component's errors
      components.forEach((component) => {
        if (component.errors && component.componentType !== undefined && component.deviceIdentifier) {
          component.errors.forEach((error) => {
            // Map WatchResponse_Kind to our event type
            let event: "OPENED" | "UPDATED" | "CLOSED";
            if (kind === WatchResponse_Kind.OPENED) {
              event = "OPENED";
            } else if (kind === WatchResponse_Kind.UPDATED) {
              event = "UPDATED";
            } else if (kind === WatchResponse_Kind.CLOSED) {
              event = "CLOSED";
            } else {
              return; // Skip unspecified kind
            }

            // Update the dashboard store with streaming event (now includes deviceId)
            handleComponentErrorStream(event, component.deviceIdentifier, component.componentType, error.errorId);
          });
        }
      });
    },
    [handleComponentErrorStream],
  );

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

    // Clear component errors and fetch fresh data when dashboard mounts
    clearComponentErrors();
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
