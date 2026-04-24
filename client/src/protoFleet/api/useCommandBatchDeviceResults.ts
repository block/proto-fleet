import { useCallback, useRef, useState } from "react";
import { minerCommandClient } from "@/protoFleet/api/clients";
import type { GetCommandBatchDeviceResultsResponse } from "@/protoFleet/api/generated/minercommand/v1/command_pb";
import { getErrorMessage } from "@/protoFleet/api/getErrorMessage";
import { useAuthErrors } from "@/protoFleet/store";

interface BatchDeviceResultsState {
  data: GetCommandBatchDeviceResultsResponse | null;
  isLoading: boolean;
  error: string | null;
}

export function useCommandBatchDeviceResults() {
  const { handleAuthErrors } = useAuthErrors();
  const [cache, setCache] = useState<Record<string, BatchDeviceResultsState>>({});
  const inflightRef = useRef<Set<string>>(new Set());
  const fetchedRef = useRef<Set<string>>(new Set());

  const fetch = useCallback(
    async (batchId: string) => {
      if (fetchedRef.current.has(batchId) || inflightRef.current.has(batchId)) return;
      inflightRef.current.add(batchId);

      setCache((prev) => ({
        ...prev,
        [batchId]: { data: prev[batchId]?.data ?? null, isLoading: true, error: null },
      }));

      try {
        const response = await minerCommandClient.getCommandBatchDeviceResults({
          batchIdentifier: batchId,
        });
        setCache((prev) => ({
          ...prev,
          [batchId]: { data: response, isLoading: false, error: null },
        }));
        if (response.status === "finished" || response.detailsPruned) {
          fetchedRef.current.add(batchId);
        }
      } catch (err) {
        handleAuthErrors({
          error: err,
          onError: (e) => {
            setCache((prev) => ({
              ...prev,
              [batchId]: {
                data: null,
                isLoading: false,
                error: getErrorMessage(e, "Failed to load batch details"),
              },
            }));
          },
        });
      } finally {
        inflightRef.current.delete(batchId);
      }
    },
    [handleAuthErrors],
  );

  const getResult = useCallback(
    (batchId: string): BatchDeviceResultsState => {
      return cache[batchId] ?? { data: null, isLoading: false, error: null };
    },
    [cache],
  );

  return { fetch, getResult };
}
