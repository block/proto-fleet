import { useCallback, useEffect, useRef, useState } from "react";
import { toInventoryInsights, toInventoryPart } from "../mappers";
import type { InventoryInsightsItem, InventoryPartItem } from "../types";
import type { InventoryFilter } from "@/protoFleet/api/generated/inventory/v1/inventory_pb";
import { type InventoryCsvPreview, useInventoryApi } from "@/protoFleet/api/inventory";

const PAGE_SIZE = 50;

export const useInventory = (initialFilter: Partial<InventoryFilter> = {}) => {
  const { listParts, getInsights, createPart, updatePart, deletePart, importCsv, confirmImport } = useInventoryApi();
  const [data, setData] = useState<InventoryPartItem[]>([]);
  const [insights, setInsights] = useState<InventoryInsightsItem | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [total, setTotal] = useState(0);
  const [nextPageToken, setNextPageToken] = useState("");
  const [currentPage, setCurrentPage] = useState(0);
  const [filter, setFilterState] = useState(initialFilter);
  const controller = useRef<AbortController | undefined>(undefined);
  const sequence = useRef(0);
  const currentPageRef = useRef(0);
  const cursorHistoryRef = useRef<string[]>([""]);

  const load = useCallback(
    async (page = 0, pageToken = "") => {
      controller.current?.abort();
      const current = new AbortController();
      controller.current = current;
      const request = ++sequence.current;
      setLoading(true);
      setError(null);
      await Promise.all([
        listParts({
          filter,
          pageSize: PAGE_SIZE,
          pageToken,
          signal: current.signal,
          onSuccess: (value) => {
            if (request !== sequence.current) return;
            setData(value.parts.map(toInventoryPart));
            setTotal(value.totalCount);
            setNextPageToken(value.nextPageToken);
            setCurrentPage(page);
            currentPageRef.current = page;
            if (value.nextPageToken) {
              const cursors = [...cursorHistoryRef.current];
              cursors[page + 1] = value.nextPageToken;
              cursorHistoryRef.current = cursors;
            }
          },
          onError: setError,
        }),
        getInsights({
          signal: current.signal,
          onSuccess: (value) => {
            if (request === sequence.current && value) setInsights(toInventoryInsights(value));
          },
          onError: setError,
        }),
      ]);
      if (request === sequence.current) setLoading(false);
    },
    [filter, getInsights, listParts],
  );

  const resetPagination = useCallback(() => {
    setCurrentPage(0);
    currentPageRef.current = 0;
    cursorHistoryRef.current = [""];
    setNextPageToken("");
    return load(0, "");
  }, [load]);

  useEffect(() => {
    let active = true;
    queueMicrotask(() => {
      if (active) void resetPagination();
    });
    return () => {
      active = false;
      controller.current?.abort();
    };
  }, [resetPagination]);

  const setFilter = useCallback((value: Partial<InventoryFilter>) => {
    setFilterState(value);
  }, []);

  const refreshCurrentPage = useCallback(
    () => load(currentPageRef.current, cursorHistoryRef.current[currentPageRef.current]),
    [load],
  );

  const create = useCallback(
    async (input: Parameters<typeof createPart>[0]) => {
      let ok = false;
      await createPart({
        ...input,
        onSuccess: () => {
          ok = true;
        },
        onError: setError,
      });
      if (ok) await resetPagination();
      return ok;
    },
    [createPart, resetPagination],
  );

  const adjust = useCallback(
    async (input: Parameters<typeof updatePart>[0]) => {
      let ok = false;
      await updatePart({
        ...input,
        onSuccess: () => {
          ok = true;
        },
        onError: setError,
      });
      if (ok) await refreshCurrentPage();
      return ok;
    },
    [refreshCurrentPage, updatePart],
  );

  const remove = useCallback(
    async (id: string) => {
      let ok = false;
      await deletePart({
        id: BigInt(id),
        onSuccess: () => {
          ok = true;
        },
        onError: setError,
      });
      if (ok) await refreshCurrentPage();
      return ok;
    },
    [deletePart, refreshCurrentPage],
  );

  const previewCsv = useCallback(
    async (csvData: Uint8Array): Promise<InventoryCsvPreview | null> => {
      let preview: InventoryCsvPreview | null = null;
      await importCsv({
        csvData,
        onSuccess: (value) => {
          preview = value;
        },
        onError: setError,
      });
      return preview;
    },
    [importCsv],
  );

  const applyCsv = useCallback(
    async (csvData: Uint8Array) => {
      let count: number | null = null;
      await confirmImport({
        csvData,
        onSuccess: (value) => {
          count = value;
        },
        onError: setError,
      });
      if (count !== null) await resetPagination();
      return count;
    },
    [confirmImport, resetPagination],
  );

  return {
    data,
    insights,
    loading,
    error,
    total,
    nextPageToken,
    currentPage,
    hasPreviousPage: currentPage > 0,
    filter,
    setFilter,
    refresh: refreshCurrentPage,
    nextPage: () => {
      const token = cursorHistoryRef.current[currentPageRef.current + 1] ?? nextPageToken;
      return token ? load(currentPageRef.current + 1, token) : Promise.resolve();
    },
    previousPage: () => {
      const previous = currentPageRef.current - 1;
      return previous >= 0 ? load(previous, cursorHistoryRef.current[previous]) : Promise.resolve();
    },
    create,
    adjust,
    remove,
    previewCsv,
    applyCsv,
  };
};
