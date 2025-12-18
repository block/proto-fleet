import { useCallback, useEffect, useRef } from "react";
import { create } from "@bufbuild/protobuf";
import { errorQueryClient } from "@/protoFleet/api/clients";
import {
  type ErrorMessage,
  WatchRequestSchema,
  type WatchResponse,
  WatchResponse_Kind,
} from "@/protoFleet/api/generated/errors/v1/errors_pb";
import { useAuthErrors, useFleetStore } from "@/protoFleet/store";
import { streamCleanupManager } from "@/protoFleet/utils/streamCleanup";

interface UseStreamDeviceErrorsOptions {
  deviceIds: string[];
  enabled?: boolean;
}

/**
 * Hook to stream device error updates for a list of miner IDs.
 * Handles OPENED/UPDATED/CLOSED events for real-time error updates.
 * Uses the normalized error store for state management.
 */
export const useStreamDeviceErrors = (options: UseStreamDeviceErrorsOptions) => {
  const { deviceIds, enabled = true } = options;
  const { handleAuthErrors } = useAuthErrors();
  const abortControllerRef = useRef<AbortController | null>(null);

  // Store action for handling error stream events
  const handleErrorStreamEvent = useFleetStore((state) => state.fleet.handleErrorStreamEvent);

  // Process streaming updates
  const processStreamingUpdate = useCallback(
    (response: WatchResponse) => {
      if (!response.result?.value || response.result.case !== "errors") {
        return;
      }

      const errors = response.result.value.items || [];
      const kind = response.kind;

      errors.forEach((error: ErrorMessage) => {
        // Use the device identifier directly from the response
        const deviceId = error.deviceIdentifier;

        if (!deviceId) return;

        // Map WatchResponse_Kind to our event type and use normalized store
        let event: "OPENED" | "UPDATED" | "CLOSED" | null = null;
        switch (kind) {
          case WatchResponse_Kind.OPENED:
            event = "OPENED";
            break;
          case WatchResponse_Kind.UPDATED:
            event = "UPDATED";
            break;
          case WatchResponse_Kind.CLOSED:
            event = "CLOSED";
            break;
        }

        if (event) {
          // Use normalized store action
          handleErrorStreamEvent(event, error);
        }
      });
    },
    [handleErrorStreamEvent],
  );

  // Start streaming function
  const startStreaming = useCallback(async () => {
    // Abort any existing stream
    if (abortControllerRef.current) {
      abortControllerRef.current.abort();
      streamCleanupManager.unregister(abortControllerRef.current);
    }

    if (!enabled || !deviceIds || deviceIds.length === 0) {
      return;
    }

    const controller = new AbortController();
    abortControllerRef.current = controller;

    // Register with cleanup manager for page unload handling
    streamCleanupManager.register(controller);

    try {
      const request = create(WatchRequestSchema, {
        filter: {
          simple: {
            deviceIdentifiers: deviceIds, // Use string device IDs directly
          },
          includeClosed: false,
        },
      });

      // Start the streaming loop
      for await (const response of errorQueryClient.watch(request, {
        signal: controller.signal,
      })) {
        // Check if stream is still active
        if (abortControllerRef.current !== controller) {
          return;
        }

        processStreamingUpdate(response);
      }
    } catch (error) {
      if (!controller.signal.aborted) {
        handleAuthErrors({
          error: error,
          onError: (err) => {
            console.error("Error streaming device errors:", err);
          },
        });
      }
    }
  }, [enabled, deviceIds, processStreamingUpdate, handleAuthErrors]);

  // Stop streaming function
  const stopStreaming = useCallback(() => {
    if (abortControllerRef.current) {
      abortControllerRef.current.abort();
      streamCleanupManager.unregister(abortControllerRef.current);
      abortControllerRef.current = null;
    }
  }, []);

  // Start/stop streaming when options change
  useEffect(() => {
    if (enabled && deviceIds.length > 0) {
      startStreaming();
    } else {
      stopStreaming();
    }

    return () => {
      stopStreaming();
    };
  }, [enabled, deviceIds, startStreaming, stopStreaming]);

  return {
    restart: startStreaming,
  };
};
