import { useState } from "react";
import { fleetInventoryClient } from "./clients";
import { POLL_INTERVAL_MS } from "@/protoFleet/constants/polling";
import type { FilterLabelSource } from "@/protoFleet/features/fleetManagement/views/viewSummary";
import { usePoll } from "@/shared/hooks/usePoll";

const EMPTY_OPTIONS: FilterLabelSource[] = [];

// Keep selected tanks available even when the list's site/container scope changes.
export function useTankFilterOptions(enabled: boolean) {
  const [options, setOptions] = useState<FilterLabelSource[]>(EMPTY_OPTIONS);
  usePoll({
    enabled,
    poll: true,
    pollIntervalMs: POLL_INTERVAL_MS,
    fetchData: async (signal) => {
      const catalog = new Map<string, FilterLabelSource>();
      const seen = new Set<string>();
      let pageToken = "";
      try {
        do {
          signal.throwIfAborted();
          const response = await fleetInventoryClient.listTankOptions(
            {
              pageSize: 1000,
              pageToken,
            },
            { signal },
          );
          for (const item of response.options) {
            const id = item.id.toString();
            catalog.set(id, { id, label: `${item.name} · ${item.siteName || "Unassigned"}` });
          }
          pageToken = response.nextPageToken;
          if (pageToken && seen.has(pageToken)) throw new Error("Unable to finish loading tank options.");
          seen.add(pageToken);
        } while (pageToken);
        if (!signal.aborted) setOptions([...catalog.values()]);
      } catch {
        // Keep URL selections intact when the readable catalog is unavailable.
        if (!signal.aborted) setOptions(EMPTY_OPTIONS);
      }
    },
  });
  return enabled ? options : EMPTY_OPTIONS;
}
