import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { create } from "@bufbuild/protobuf";
import { fleetInventoryClient } from "./clients";
import { inventorySort } from "./fleetInventory";
import { SortDirection } from "./generated/common/v1/sort_pb";
import { DeviceSetStatsSchema } from "./generated/device_set/v1/device_set_pb";
import { type InventoryItem, InventoryView } from "./generated/fleetinventory/v1/fleetinventory_pb";
import { getErrorMessage } from "./getErrorMessage";
import type { ListDeviceSetsProps } from "./useDeviceSets";

export function useInventoryRacks(enabled = true) {
  const [items, setItems] = useState<InventoryItem[]>([]);
  const requestID = useRef(0);
  const controller = useRef<AbortController | null>(null);
  const scopeKey = useRef("");
  useEffect(
    () => () => {
      controller.current?.abort();
      requestID.current++;
    },
    [],
  );
  useEffect(() => {
    if (!enabled) {
      controller.current?.abort();
      requestID.current++;
      // eslint-disable-next-line react-hooks/set-state-in-effect -- discard external query results when the effective scope becomes empty
      setItems([]);
    }
  }, [enabled]);
  const listRacks = useCallback(
    async (options: ListDeviceSetsProps) => {
      if (!enabled) {
        options.onSuccess?.([], "", 0);
        options.onFinally?.();
        return;
      }
      const id = ++requestID.current;
      controller.current?.abort();
      const abort = new AbortController();
      controller.current = abort;
      const { onSuccess: _success, onError: _error, onFinally: _finally, ...query } = options;
      const key = JSON.stringify(query, (_, value) => (typeof value === "bigint" ? value.toString() : value));
      if (key !== scopeKey.current) setItems([]);
      scopeKey.current = key;
      try {
        const response = await fleetInventoryClient.listInventory(
          {
            view: InventoryView.EQUIPMENT_GROUPS,
            pageSize: options.pageSize,
            pageToken: options.pageToken,
            sort: inventorySort(options.sort),
            descending: options.sort?.direction === SortDirection.DESC,
            siteIds: options.siteIds,
            includeUnassigned: options.includeUnassigned,
            buildingIds: options.buildingIds,
            includeNoBuilding: options.includeNoBuilding,
            zoneKeys: options.zones?.map((zone) => ({ zone, buildingId: 0n })),
            errorComponentTypes: options.errorComponentTypes,
            telemetryRanges: options.telemetryRanges,
          },
          { signal: abort.signal },
        );
        if (id !== requestID.current) return;
        setItems(response.items);
        options.onSuccess?.(
          response.items.flatMap((item) => (item.details.case === "rack" ? [item.details.value] : [])),
          response.nextPageToken,
          Number(response.totalCount),
        );
      } catch (error) {
        if (id === requestID.current && !abort.signal.aborted) options.onError?.(getErrorMessage(error));
      } finally {
        if (id === requestID.current) options.onFinally?.();
      }
    },
    [enabled],
  );
  const statsMap = useMemo(
    () =>
      new Map(
        items.flatMap((item) => {
          if (item.details.case !== "rack" || !item.listStats) return [];
          const { $typeName: _typeName, ...stats } = item.listStats;
          return [
            [
              item.details.value.id,
              create(DeviceSetStatsSchema, { ...stats, deviceSetId: item.details.value.id }),
            ] as const,
          ];
        }),
      ),
    [items],
  );
  return { items: enabled ? items : [], listRacks, statsMap: enabled ? statsMap : new Map() };
}
