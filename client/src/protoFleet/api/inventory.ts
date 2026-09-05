import { useCallback } from "react";

import { inventoryClient } from "@/protoFleet/api/clients";
import {
  AdjustmentReason,
  type CsvPreviewRow,
  type InventoryFilter,
  type InventoryInsights,
  type InventoryPart,
} from "@/protoFleet/api/generated/inventory/v1/inventory_pb";
import { getErrorMessage } from "@/protoFleet/api/getErrorMessage";
import { isAbortError } from "@/protoFleet/api/requestErrors";
import { useAuthErrors } from "@/protoFleet/store";

type Callbacks<T> = {
  signal?: AbortSignal;
  onSuccess?: (value: T) => void;
  onError?: (message: string) => void;
  onFinally?: () => void;
};

export type InventoryCsvPreview = { rows: CsvPreviewRow[]; validCount: number; errorCount: number };

export const useInventoryApi = () => {
  const { handleAuthErrors } = useAuthErrors();
  const report = useCallback(
    (error: unknown, signal: AbortSignal | undefined, onError?: (message: string) => void) => {
      if (isAbortError(error, signal)) return;
      handleAuthErrors({ error, onError: (value: unknown) => onError?.(getErrorMessage(value)) });
    },
    [handleAuthErrors],
  );

  const listParts = useCallback(
    async ({
      filter,
      pageSize = 0,
      pageToken = "",
      signal,
      onSuccess,
      onError,
      onFinally,
    }: Callbacks<{ parts: InventoryPart[]; nextPageToken: string; totalCount: number }> & {
      filter?: Partial<InventoryFilter>;
      pageSize?: number;
      pageToken?: string;
    }) => {
      try {
        if (signal?.aborted) return;
        const response = await inventoryClient.listInventoryParts(
          {
            filter: {
              siteIds: filter?.siteIds ?? [],
              types: filter?.types ?? [],
              lowStockOnly: filter?.lowStockOnly ?? false,
            },
            pageSize,
            pageToken,
          },
          { signal },
        );
        if (!signal?.aborted)
          onSuccess?.({
            parts: response.parts,
            nextPageToken: response.nextPageToken,
            totalCount: response.totalCount,
          });
      } catch (error) {
        report(error, signal, onError);
      } finally {
        onFinally?.();
      }
    },
    [report],
  );

  const getPart = useCallback(
    async ({ id, signal, onSuccess, onError, onFinally }: Callbacks<InventoryPart | undefined> & { id: bigint }) => {
      try {
        if (signal?.aborted) return;
        const response = await inventoryClient.getInventoryPart({ id }, { signal });
        if (!signal?.aborted) onSuccess?.(response.part);
      } catch (error) {
        report(error, signal, onError);
      } finally {
        onFinally?.();
      }
    },
    [report],
  );

  const createPart = useCallback(
    async ({
      name,
      type,
      manufacturer = "",
      partNumber = "",
      siteId,
      onHand = 0,
      reorderPoint = 0,
      binLocation = "",
      signal,
      onSuccess,
      onError,
      onFinally,
    }: Callbacks<InventoryPart | undefined> & {
      name: string;
      type: string;
      manufacturer?: string;
      partNumber?: string;
      siteId?: bigint;
      onHand?: number;
      reorderPoint?: number;
      binLocation?: string;
    }) => {
      try {
        if (signal?.aborted) return;
        const response = await inventoryClient.createInventoryPart(
          { name, type, manufacturer, partNumber, siteId, onHand, reorderPoint, binLocation },
          { signal },
        );
        if (!signal?.aborted) onSuccess?.(response.part);
      } catch (error) {
        report(error, signal, onError);
      } finally {
        onFinally?.();
      }
    },
    [report],
  );

  const updatePart = useCallback(
    async ({
      id,
      onHand,
      expectedOnHand,
      reorderPoint,
      binLocation,
      siteId,
      reason = AdjustmentReason.UNSPECIFIED,
      signal,
      onSuccess,
      onError,
      onFinally,
    }: Callbacks<InventoryPart | undefined> & {
      id: bigint;
      onHand?: number;
      expectedOnHand?: number;
      reorderPoint?: number;
      binLocation?: string;
      siteId?: bigint;
      reason?: AdjustmentReason;
    }) => {
      try {
        if (signal?.aborted) return;
        const response = await inventoryClient.updateInventoryPart(
          { id, onHand, expectedOnHand, reorderPoint, binLocation, siteId, reason },
          { signal },
        );
        if (!signal?.aborted) onSuccess?.(response.part);
      } catch (error) {
        report(error, signal, onError);
      } finally {
        onFinally?.();
      }
    },
    [report],
  );

  const deletePart = useCallback(
    async ({ id, signal, onSuccess, onError, onFinally }: Callbacks<void> & { id: bigint }) => {
      try {
        if (signal?.aborted) return;
        await inventoryClient.deleteInventoryPart({ id }, { signal });
        if (!signal?.aborted) onSuccess?.();
      } catch (error) {
        report(error, signal, onError);
      } finally {
        onFinally?.();
      }
    },
    [report],
  );

  const getInsights = useCallback(
    async ({ signal, onSuccess, onError, onFinally }: Callbacks<InventoryInsights | undefined>) => {
      try {
        if (signal?.aborted) return;
        const response = await inventoryClient.getInventoryInsights({}, { signal });
        if (!signal?.aborted) onSuccess?.(response.insights);
      } catch (error) {
        report(error, signal, onError);
      } finally {
        onFinally?.();
      }
    },
    [report],
  );

  const listPartsBySite = useCallback(
    async ({ siteId, signal, onSuccess, onError, onFinally }: Callbacks<InventoryPart[]> & { siteId: bigint }) => {
      try {
        if (signal?.aborted) return;
        const response = await inventoryClient.listPartsBySite({ siteId }, { signal });
        if (!signal?.aborted) onSuccess?.(response.parts);
      } catch (error) {
        report(error, signal, onError);
      } finally {
        onFinally?.();
      }
    },
    [report],
  );

  const importCsv = useCallback(
    async ({
      csvData,
      signal,
      onSuccess,
      onError,
      onFinally,
    }: Callbacks<InventoryCsvPreview> & { csvData: Uint8Array }) => {
      try {
        if (signal?.aborted) return;
        const response = await inventoryClient.importInventoryCsv({ csvData }, { signal });
        if (!signal?.aborted)
          onSuccess?.({ rows: response.rows, validCount: response.validCount, errorCount: response.errorCount });
      } catch (error) {
        report(error, signal, onError);
      } finally {
        onFinally?.();
      }
    },
    [report],
  );

  const confirmImport = useCallback(
    async ({ csvData, signal, onSuccess, onError, onFinally }: Callbacks<number> & { csvData: Uint8Array }) => {
      try {
        if (signal?.aborted) return;
        const response = await inventoryClient.confirmInventoryImport({ csvData }, { signal });
        if (!signal?.aborted) onSuccess?.(response.importedCount);
      } catch (error) {
        report(error, signal, onError);
      } finally {
        onFinally?.();
      }
    },
    [report],
  );

  return {
    listParts,
    getPart,
    createPart,
    updatePart,
    deletePart,
    getInsights,
    listPartsBySite,
    importCsv,
    confirmImport,
  };
};
