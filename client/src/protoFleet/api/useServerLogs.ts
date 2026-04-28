import { useCallback, useEffect, useMemo, useRef, useState } from "react";

import { serverLogClient } from "@/protoFleet/api/clients";
import { type LogEntry, type LogLevel } from "@/protoFleet/api/generated/serverlog/v1/serverlog_pb";
import { getErrorMessage } from "@/protoFleet/api/getErrorMessage";
import { useAuthErrors } from "@/protoFleet/store";

// Maximum entries we keep client-side. The buffer on the server is also
// bounded; this cap protects the UI in case the user requests more.
const CLIENT_MAX_ENTRIES = 2000;

// Default poll cadence when tailing. 3s gives the user a near-live feel
// without putting meaningful load on a server logging at typical rates.
const DEFAULT_POLL_INTERVAL_MS = 3000;

interface UseServerLogsParams {
  /** Inclusive minimum level to fetch from the server. */
  minLevel: LogLevel;
  /** Case-insensitive substring filter applied server-side. */
  searchText: string;
  /**
   * When true, the hook polls and appends new entries as they arrive.
   * When false, it fetches once on mount and on subsequent param/refresh
   * changes only.
   */
  follow: boolean;
  /** Poll interval in milliseconds; only used when `follow` is true. */
  pollIntervalMs?: number;
  /** Per-request limit on how many entries to fetch. */
  pageLimit?: number;
}

interface UseServerLogsResult {
  entries: LogEntry[];
  /** True while the very first fetch is in flight (no entries yet). */
  isInitialLoading: boolean;
  /** True while any fetch is in flight (initial or polling). */
  isFetching: boolean;
  /** Last error message if a fetch failed; null on success. */
  error: string | null;
  /** Server-reported buffer fill (current size / capacity). */
  bufferSize: number;
  bufferCapacity: number;
  /** True when the last response was clipped by `pageLimit`. */
  truncated: boolean;
  /** Imperative refresh; safe to call mid-poll. Resets the cursor. */
  refresh: () => void;
  /** Drop all client-side entries without re-fetching. */
  clear: () => void;
}

/**
 * useServerLogs polls ServerLogService.ListServerLogs and accumulates
 * entries into a single growing list, capped at CLIENT_MAX_ENTRIES.
 *
 * The hook keeps a `since_id` cursor server-side so each poll only
 * transfers new records — a refilter (level/search change) resets the
 * cursor and re-fetches from scratch.
 */
export function useServerLogs({
  minLevel,
  searchText,
  follow,
  pollIntervalMs = DEFAULT_POLL_INTERVAL_MS,
  pageLimit = 500,
}: UseServerLogsParams): UseServerLogsResult {
  const { handleAuthErrors } = useAuthErrors();

  const [entries, setEntries] = useState<LogEntry[]>([]);
  const [isInitialLoading, setIsInitialLoading] = useState(true);
  const [isFetching, setIsFetching] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [bufferSize, setBufferSize] = useState(0);
  const [bufferCapacity, setBufferCapacity] = useState(0);
  const [truncated, setTruncated] = useState(false);

  // sinceId is the highest id we've already fetched, used as the cursor
  // for incremental polls. We hold it in a ref so polling callbacks see
  // the latest value without re-binding the interval.
  const sinceIdRef = useRef<bigint>(0n);

  // requestId guards against late responses from a prior, now-stale
  // request landing after a newer one (e.g. when filters change rapidly).
  const requestIdRef = useRef(0);

  const fetchLogs = useCallback(
    async (cursor: bigint) => {
      const requestId = ++requestIdRef.current;

      // A zero cursor means "fresh start" (initial mount, refilter, or
      // explicit refresh): drop any prior entries and re-enter the
      // initial-loading state. Doing the reset here — inside the fetch
      // fn rather than in the calling useEffect — keeps state mutations
      // out of effect bodies, which the React lint rules flag as a
      // cascading-render risk. React batches these with setIsFetching
      // below into a single render.
      if (cursor === 0n) {
        setEntries([]);
        setIsInitialLoading(true);
      }
      setIsFetching(true);

      try {
        const response = await serverLogClient.listServerLogs({
          minLevel,
          searchText,
          sinceId: cursor,
          limit: pageLimit,
        });

        if (requestId !== requestIdRef.current) return;

        if (response.latestId > sinceIdRef.current) {
          sinceIdRef.current = response.latestId;
        }

        if (response.entries.length > 0) {
          setEntries((prev) => {
            // Append-only when polling. On a fresh start we already
            // cleared `prev` above, so this still yields the right list.
            const combined = prev.concat(response.entries);
            if (combined.length > CLIENT_MAX_ENTRIES) {
              return combined.slice(combined.length - CLIENT_MAX_ENTRIES);
            }
            return combined;
          });
        }

        setBufferSize(response.bufferSize);
        setBufferCapacity(response.bufferCapacity);
        setTruncated(response.truncated);
        setError(null);
      } catch (err) {
        if (requestId !== requestIdRef.current) return;
        handleAuthErrors({
          error: err,
          onError: (e) => {
            setError(getErrorMessage(e, "Failed to load server logs"));
          },
        });
      } finally {
        if (requestId === requestIdRef.current) {
          setIsFetching(false);
          setIsInitialLoading(false);
        }
      }
    },
    [minLevel, searchText, pageLimit, handleAuthErrors],
  );

  // Hold a ref to the latest fetch fn so the polling effect can call it
  // without re-running on every minLevel/searchText change.
  const fetchRef = useRef(fetchLogs);
  useEffect(() => {
    fetchRef.current = fetchLogs;
  }, [fetchLogs]);

  // Refilter (level/search change) → reset cursor and re-fetch from
  // scratch. The fetch fn handles clearing entries and flipping back to
  // the initial-loading state internally; we only mutate the ref here so
  // this effect body doesn't call setState synchronously (React lint
  // rule react-hooks/set-state-in-effect).
  useEffect(() => {
    sinceIdRef.current = 0n;
    void fetchRef.current(0n);
  }, [minLevel, searchText]);

  // Polling loop, only active when `follow` is on. Each tick uses the
  // current cursor in the ref so we incrementally fetch only new records.
  useEffect(() => {
    if (!follow) return;
    const id = window.setInterval(() => {
      void fetchRef.current(sinceIdRef.current);
    }, pollIntervalMs);
    return () => window.clearInterval(id);
  }, [follow, pollIntervalMs]);

  const refresh = useCallback(() => {
    // Reset the cursor and let fetchLogs do the entries / initial-loading
    // reset itself when it sees the zero cursor. Keeps the reset logic in
    // one place and matches the refilter effect above.
    sinceIdRef.current = 0n;
    void fetchRef.current(0n);
  }, []);

  const clear = useCallback(() => {
    setEntries([]);
  }, []);

  return useMemo(
    () => ({
      entries,
      isInitialLoading,
      isFetching,
      error,
      bufferSize,
      bufferCapacity,
      truncated,
      refresh,
      clear,
    }),
    [entries, isInitialLoading, isFetching, error, bufferSize, bufferCapacity, truncated, refresh, clear],
  );
}
