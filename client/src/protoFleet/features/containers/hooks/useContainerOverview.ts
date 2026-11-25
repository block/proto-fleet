import { useState } from "react";
import { Code, ConnectError } from "@connectrpc/connect";
import { containersClient, fleetInventoryClient } from "@/protoFleet/api/clients";
import type { Container } from "@/protoFleet/api/generated/containers/v1/containers_pb";
import {
  EntityKind,
  type InventoryItem,
  InventoryView,
} from "@/protoFleet/api/generated/fleetinventory/v1/fleetinventory_pb";
import { getErrorMessage } from "@/protoFleet/api/getErrorMessage";
import { POLL_INTERVAL_MS } from "@/protoFleet/constants/polling";
import { useAuthErrors } from "@/protoFleet/store";
import { useMountedCallback } from "@/shared/hooks/useMountedCallback";
import { usePoll } from "@/shared/hooks/usePoll";

export interface ContainerOverviewData {
  container: Container;
  tanks: InventoryItem[];
  modules: InventoryItem[];
}

async function loadChildren(id: bigint, view: InventoryView, signal: AbortSignal) {
  const items: InventoryItem[] = [];
  const seen = new Set<string>();
  let pageToken = "";
  do {
    signal.throwIfAborted();
    const response = await fleetInventoryClient.listInventory(
      {
        view,
        ancestors: [{ kind: EntityKind.CONTAINER, id }],
        kinds: [view === InventoryView.EQUIPMENT_GROUPS ? EntityKind.TANK : EntityKind.MODULE],
        pageSize: 1000,
        pageToken,
      },
      { signal },
    );
    items.push(...response.items);
    pageToken = response.nextPageToken;
    if (pageToken && seen.has(pageToken)) throw new Error("Unable to finish loading container inventory.");
    seen.add(pageToken);
  } while (pageToken);
  // Live cursor pages can overlap as inventory changes; a stable Fleet ID wins.
  return [...new Map(items.map((item) => [item.ref?.id.toString(), item])).values()];
}

export function useContainerOverview(identifier: string) {
  const { handleAuthErrors } = useAuthErrors();
  const [revision, setRevision] = useState(0);
  const [result, setResult] = useState<{
    key: string;
    data?: ContainerOverviewData;
    error?: string;
    notFound?: boolean;
  }>();
  usePoll({
    enabled: !!identifier,
    params: `${identifier}:${revision}`,
    poll: true,
    pollIntervalMs: POLL_INTERVAL_MS,
    fetchData: async (signal) => {
      try {
        const { container } = await containersClient.getContainer({ deviceIdentifier: identifier }, { signal });
        if (!container || container.id <= 0n) throw new ConnectError("Container not found", Code.NotFound);
        const [tanks, modules] = await Promise.all([
          loadChildren(container.id, InventoryView.EQUIPMENT_GROUPS, signal),
          loadChildren(container.id, InventoryView.MINING_ASSETS, signal),
        ]);
        if (!signal.aborted) setResult({ key: identifier, data: { container, tanks, modules } });
      } catch (error) {
        if (signal.aborted) return;
        const code = ConnectError.from(error).code;
        const denied = [Code.NotFound, Code.PermissionDenied, Code.Unauthenticated].includes(code);
        handleAuthErrors({
          error,
          onError: () =>
            setResult((previous) => ({
              key: identifier,
              data: !denied && previous?.key === identifier ? previous.data : undefined,
              error: getErrorMessage(error),
              notFound: code === Code.NotFound,
            })),
        });
      }
    },
  });
  const retry = useMountedCallback(() => setRevision((value) => value + 1));
  const current = result?.key === identifier ? result : undefined;
  return { ...current, loading: !!identifier && !current, retry };
}
