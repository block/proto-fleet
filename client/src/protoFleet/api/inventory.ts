import { useCallback } from "react";

// import { inventoryClient } from "@/protoFleet/api/clients";
import { getErrorMessage } from "@/protoFleet/api/getErrorMessage";
import { useAuthErrors } from "@/protoFleet/store";

interface ListPartsProps {
  filter?: Record<string, unknown>;
  pageSize?: number;
  pageToken?: string;
  signal?: AbortSignal;
  onSuccess?: (parts: unknown[], nextPageToken: string) => void;
  onError?: (message: string) => void;
  onFinally?: () => void;
}

interface GetInsightsProps {
  signal?: AbortSignal;
  onSuccess?: (insights: unknown) => void;
  onError?: (message: string) => void;
  onFinally?: () => void;
}

interface UpdatePartProps {
  id: bigint;
  onHand?: number;
  reorderPoint?: number;
  binLocation?: string;
  reason?: number;
  notes?: string;
  signal?: AbortSignal;
  onSuccess?: (part: unknown) => void;
  onError?: (message: string) => void;
  onFinally?: () => void;
}

interface ListPartsBySiteProps {
  siteId: bigint;
  signal?: AbortSignal;
  onSuccess?: (parts: unknown[]) => void;
  onError?: (message: string) => void;
  onFinally?: () => void;
}

interface ImportCsvProps {
  csvData: Uint8Array;
  signal?: AbortSignal;
  onSuccess?: (preview: unknown) => void;
  onError?: (message: string) => void;
  onFinally?: () => void;
}

interface ConfirmImportProps {
  csvData: Uint8Array;
  signal?: AbortSignal;
  onSuccess?: (importedCount: number) => void;
  onError?: (message: string) => void;
  onFinally?: () => void;
}

export const useInventoryApi = () => {
  const { handleAuthErrors } = useAuthErrors();

  const listParts = useCallback(
    async ({ signal, onSuccess, onError, onFinally }: ListPartsProps) => {
      try {
        // TODO: wire to inventoryClient.listInventoryParts
        if (signal?.aborted) return;
        onSuccess?.([], "");
      } catch (err) {
        if (signal?.aborted) return;
        handleAuthErrors({
          error: err,
          onError: (error: unknown) => onError?.(getErrorMessage(error)),
        });
      } finally {
        onFinally?.();
      }
    },
    [handleAuthErrors],
  );

  const getInsights = useCallback(
    async ({ signal, onSuccess, onError, onFinally }: GetInsightsProps) => {
      try {
        // TODO: wire to inventoryClient.getInventoryInsights
        if (signal?.aborted) return;
        onSuccess?.(undefined);
      } catch (err) {
        if (signal?.aborted) return;
        handleAuthErrors({
          error: err,
          onError: (error: unknown) => onError?.(getErrorMessage(error)),
        });
      } finally {
        onFinally?.();
      }
    },
    [handleAuthErrors],
  );

  const updatePart = useCallback(
    async ({ id, signal, onSuccess, onError, onFinally }: UpdatePartProps) => {
      try {
        // TODO: wire to inventoryClient.updateInventoryPart
        if (signal?.aborted) return;
        onSuccess?.(undefined);
      } catch (err) {
        if (signal?.aborted) return;
        handleAuthErrors({
          error: err,
          onError: (error: unknown) => onError?.(getErrorMessage(error)),
        });
      } finally {
        onFinally?.();
      }
    },
    [handleAuthErrors],
  );

  const listPartsBySite = useCallback(
    async ({ siteId, signal, onSuccess, onError, onFinally }: ListPartsBySiteProps) => {
      try {
        // TODO: wire to inventoryClient.listPartsBySite
        if (signal?.aborted) return;
        onSuccess?.([]);
      } catch (err) {
        if (signal?.aborted) return;
        handleAuthErrors({
          error: err,
          onError: (error: unknown) => onError?.(getErrorMessage(error)),
        });
      } finally {
        onFinally?.();
      }
    },
    [handleAuthErrors],
  );

  const importCsv = useCallback(
    async ({ csvData, signal, onSuccess, onError, onFinally }: ImportCsvProps) => {
      try {
        // TODO: wire to inventoryClient.importInventoryCsv
        if (signal?.aborted) return;
        onSuccess?.(undefined);
      } catch (err) {
        if (signal?.aborted) return;
        handleAuthErrors({
          error: err,
          onError: (error: unknown) => onError?.(getErrorMessage(error)),
        });
      } finally {
        onFinally?.();
      }
    },
    [handleAuthErrors],
  );

  const confirmImport = useCallback(
    async ({ csvData, signal, onSuccess, onError, onFinally }: ConfirmImportProps) => {
      try {
        // TODO: wire to inventoryClient.confirmInventoryImport
        if (signal?.aborted) return;
        onSuccess?.(0);
      } catch (err) {
        if (signal?.aborted) return;
        handleAuthErrors({
          error: err,
          onError: (error: unknown) => onError?.(getErrorMessage(error)),
        });
      } finally {
        onFinally?.();
      }
    },
    [handleAuthErrors],
  );

  return { listParts, getInsights, updatePart, listPartsBySite, importCsv, confirmImport };
};
