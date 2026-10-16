import { useCallback, useState } from "react";
import { create, type MessageInitShape, toJsonString } from "@bufbuild/protobuf";
import { fleetInventoryClient } from "./clients";
import {
  ListInventoryRequestSchema,
  type ListInventoryResponse,
} from "./generated/fleetinventory/v1/fleetinventory_pb";
import { getErrorMessage } from "./getErrorMessage";
import { useFleetStore } from "@/protoFleet/store/useFleetStore";
import { usePoll } from "@/shared/hooks/usePoll";

// Each page is selected and ordered by the server across all entity kinds.
// Keys include every filter; previous-scope rows are never returned while a
// new request is pending, even if a cancelled transport resolves late.
export function useFleetInventory(input: MessageInitShape<typeof ListInventoryRequestSchema>, enabled = true) {
  const sitesRevision = useFleetStore((state) => state.ui.sitesRevision);
  const request = create(ListInventoryRequestSchema, input);
  const key = `${toJsonString(ListInventoryRequestSchema, request)}:${sitesRevision}`;
  const [navigation, setNavigation] = useState({ key, page: 0, cursors: [""] });
  const [revision, setRevision] = useState(0);
  const [result, setResult] = useState<{
    key: string;
    page: number;
    response?: ListInventoryResponse;
    error?: string;
  }>();
  const page = navigation.key === key ? navigation.page : 0;
  const cursors = navigation.key === key ? navigation.cursors : [""];
  const cursor = cursors[page] ?? "";
  usePoll({
    enabled,
    poll: true,
    params: `${key}:${page}:${cursor}:${revision}`,
    fetchData: async (signal) => {
      try {
        const response = await fleetInventoryClient.listInventory({ ...request, pageToken: cursor }, { signal });
        if (!signal.aborted) setResult({ key, page, response });
      } catch (error) {
        if (!signal.aborted)
          setResult((previous) => ({
            key,
            page,
            error: getErrorMessage(error),
            response: previous?.key === key && previous.page === page ? previous.response : undefined,
          }));
      }
    },
  });
  const current = enabled && result?.key === key && result.page === page ? result : undefined;
  const refresh = useCallback(() => setRevision((value) => value + 1), []);
  const reset = useCallback(() => {
    setNavigation({ key, page: 0, cursors: [""] });
    refresh();
  }, [key, refresh]);
  return {
    items: current?.response?.items ?? [],
    total: Number(current?.response?.totalCount ?? 0n),
    loading: enabled && !current,
    error: current?.error,
    currentPage: page,
    hasNextPage: !!current?.response?.nextPageToken,
    hasPreviousPage: page > 0,
    refresh,
    reset,
    nextPage: () => {
      const token = current?.response?.nextPageToken;
      if (token) setNavigation({ key, page: page + 1, cursors: [...cursors.slice(0, page + 1), token] });
    },
    previousPage: () => setNavigation({ key, page: Math.max(0, page - 1), cursors }),
  };
}
