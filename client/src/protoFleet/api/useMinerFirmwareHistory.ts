import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Code, ConnectError } from "@connectrpc/connect";

import { rolloutClient } from "@/protoFleet/api/clients";
import type { MinerFirmwareHistoryEntry } from "@/protoFleet/api/generated/rollout/v1/rollout_pb";
import {
  useAuthErrors,
  useFleetStore,
  useHasPermission,
  useIsAuthenticated,
  useSessionGeneration,
  useUsername,
} from "@/protoFleet/store";

const PAGE_SIZE = 50;
const TIMEOUT_MS = 30_000;

export interface MinerFirmwareHistoryState {
  entries: MinerFirmwareHistoryEntry[];
  canRead: boolean;
  hasLoaded: boolean;
  isLoading: boolean;
  isLoadingMore: boolean;
  hasMore: boolean;
  error: string | null;
  refresh: () => void;
  loadMore: () => void;
  retry: () => void;
}

// Mounted only while a miner's history is open. Each request reads one page;
// account, session, permission and miner changes invalidate both data and work.
export function useMinerFirmwareHistory(deviceIdentifier: string): MinerFirmwareHistoryState {
  const username = useUsername();
  const sessionGeneration = useSessionGeneration();
  const authenticated = useIsAuthenticated();
  const permitted = useHasPermission("miner:firmware_update");
  const { handleAuthErrors } = useAuthErrors();
  const canRead = authenticated && permitted;
  const context = useMemo(
    () => ({ deviceIdentifier, username, sessionGeneration, canRead }),
    [deviceIdentifier, username, sessionGeneration, canRead],
  );
  const [snapshot, setSnapshot] = useState({
    context,
    entries: [] as MinerFirmwareHistoryEntry[],
    hasLoaded: false,
    isLoading: canRead,
    isLoadingMore: false,
    cursor: "",
    error: null as string | null,
  });
  const controls = useRef<{ refresh: () => void; loadMore: () => void; retry: () => void } | null>(null);
  const refresh = useCallback(() => controls.current?.refresh(), []);
  const loadMore = useCallback(() => controls.current?.loadMore(), []);
  const retry = useCallback(() => controls.current?.retry(), []);

  useEffect(() => {
    let disposed = false;
    let controller: AbortController | null = null;
    let cursor = "";
    let failedCursor = "";
    let entries: MinerFirmwareHistoryEntry[] = [];
    const isCurrent = () => {
      const auth = useFleetStore.getState().auth;
      return (
        !disposed &&
        context.canRead &&
        auth.isAuthenticated &&
        auth.permissions.includes("miner:firmware_update") &&
        auth.username === context.username &&
        auth.sessionGeneration === context.sessionGeneration
      );
    };
    const read = async (requestedCursor: string) => {
      if (!isCurrent() || controller) return;
      const request = new AbortController();
      controller = request;
      failedCursor = requestedCursor;
      setSnapshot((previous) => ({
        ...(previous.context === context
          ? previous
          : { context, entries: [], cursor: "", hasLoaded: false, error: null }),
        isLoading: requestedCursor === "",
        isLoadingMore: requestedCursor !== "",
        error: null,
      }));
      try {
        const response = await rolloutClient.listMinerFirmwareHistory(
          { deviceIdentifier: context.deviceIdentifier, pageSize: PAGE_SIZE, cursor: requestedCursor },
          { signal: request.signal, timeoutMs: TIMEOUT_MS },
        );
        if (!isCurrent() || request.signal.aborted) return;
        entries = requestedCursor ? [...entries, ...response.entries] : response.entries;
        cursor = response.cursor;
        setSnapshot({
          context,
          entries,
          cursor,
          hasLoaded: true,
          isLoading: false,
          isLoadingMore: false,
          error: null,
        });
      } catch (error) {
        if (!isCurrent() || request.signal.aborted) return;
        handleAuthErrors({ error });
        if (!isCurrent()) return;
        const unavailable =
          error instanceof ConnectError &&
          [Code.PermissionDenied, Code.NotFound, Code.InvalidArgument].includes(error.code);
        if (unavailable) {
          entries = [];
          cursor = "";
          failedCursor = "";
        }
        setSnapshot((previous) => ({
          ...previous,
          ...(unavailable ? { entries: [], cursor: "", hasLoaded: false } : {}),
          isLoading: false,
          isLoadingMore: false,
          error: error instanceof Error && error.message ? error.message : "Couldn't load firmware update history.",
        }));
      } finally {
        if (controller === request) controller = null;
      }
    };
    controls.current = {
      refresh: () => void read(""),
      loadMore: () => {
        if (cursor) void read(cursor);
      },
      retry: () => void read(failedCursor),
    };
    void read("");
    return () => {
      disposed = true;
      controller?.abort();
      controls.current = null;
    };
  }, [context, handleAuthErrors]);

  const current = snapshot.context === context && canRead ? snapshot : undefined;
  return {
    entries: current?.entries ?? [],
    canRead,
    hasLoaded: current?.hasLoaded ?? false,
    isLoading: canRead && (current?.isLoading ?? true),
    isLoadingMore: current?.isLoadingMore ?? false,
    hasMore: !!current?.cursor,
    error: current?.error ?? null,
    refresh,
    loadMore,
    retry,
  };
}
