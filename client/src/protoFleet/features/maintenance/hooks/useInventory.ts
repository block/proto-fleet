import { useCallback, useEffect, useRef, useState } from "react";
import { toInventoryInsights, toInventoryPart } from "../mappers";
import type { InventoryInsightsItem, InventoryPartItem } from "../types";
import type { InventoryFilter } from "@/protoFleet/api/generated/inventory/v1/inventory_pb";
import { type InventoryCsvPreview, useInventoryApi } from "@/protoFleet/api/inventory";

export const useInventory = (initialFilter: Partial<InventoryFilter> = {}) => {
  const { listParts, getInsights, createPart, updatePart, deletePart, importCsv, confirmImport } = useInventoryApi();
  const [data, setData] = useState<InventoryPartItem[]>([]);
  const [insights, setInsights] = useState<InventoryInsightsItem | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [nextPageToken, setNextPageToken] = useState("");
  const [filter, setFilterState] = useState(initialFilter);
  const controller = useRef<AbortController | undefined>(undefined);
  const sequence = useRef(0);
  const load = useCallback(
    async (append = false) => {
      controller.current?.abort();
      const current = new AbortController();
      controller.current = current;
      const request = ++sequence.current;
      setLoading(true);
      setError(null);
      await Promise.all([
        listParts({
          filter,
          pageSize: 50,
          pageToken: append ? nextPageToken : "",
          signal: current.signal,
          onSuccess: (value) => {
            if (request !== sequence.current) return;
            const mapped = value.parts.map(toInventoryPart);
            setData((old) => (append ? [...old, ...mapped] : mapped));
            setNextPageToken(value.nextPageToken);
          },
          onError: setError,
        }),
        append
          ? Promise.resolve()
          : getInsights({
              signal: current.signal,
              onSuccess: (value) => {
                if (request === sequence.current && value) setInsights(toInventoryInsights(value));
              },
              onError: setError,
            }),
      ]);
      if (request === sequence.current) setLoading(false);
    },
    [filter, getInsights, listParts, nextPageToken],
  );
  useEffect(() => {
    let active = true;
    queueMicrotask(() => {
      if (active) void load();
    });
    return () => {
      active = false;
      controller.current?.abort();
    };
  }, [filter]); // eslint-disable-line react-hooks/exhaustive-deps
  const setFilter = useCallback((value: Partial<InventoryFilter>) => {
    setNextPageToken("");
    setFilterState(value);
  }, []);
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
      if (ok) await load();
      return ok;
    },
    [createPart, load],
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
      if (ok) await load();
      return ok;
    },
    [load, updatePart],
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
      if (ok) await load();
      return ok;
    },
    [deletePart, load],
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
      if (count !== null) await load();
      return count;
    },
    [confirmImport, load],
  );
  return {
    data,
    insights,
    loading,
    error,
    nextPageToken,
    filter,
    setFilter,
    refresh: () => load(),
    loadMore: () => load(true),
    create,
    adjust,
    remove,
    previewCsv,
    applyCsv,
  };
};
