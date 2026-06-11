import { useCallback, useEffect, useRef, useState } from "react";
import { notesClient } from "@/protoFleet/api/clients";
import { type Note } from "@/protoFleet/api/generated/notes/v1/notes_pb";
import { getErrorMessage } from "@/protoFleet/api/getErrorMessage";
import { useAuthErrors } from "@/protoFleet/store";

interface UseNotesFeedParams {
  pageSize?: number;
}

interface UseNotesFeedResult {
  notes: Note[];
  isLoading: boolean;
  // True once any fetch has succeeded — distinguishes "loading the
  // feed for the first time" (spinner) from "feed is genuinely empty".
  hasLoaded: boolean;
  error: string | null;
  hasMore: boolean;
  loadMore: () => void;
  refresh: () => void;
  refreshHead: () => Promise<void>;
}

// Compare two notes by the server feed order: (created_at, id)
// descending. Positive when a sorts after b in the feed (i.e. a is
// older). Compares the raw Timestamp fields so sub-millisecond
// distinctions survive (Date would truncate to ms).
const feedCmp = (a: Note, b: Note): number => {
  const at = a.createdAt;
  const bt = b.createdAt;
  const as = at?.seconds ?? 0n;
  const bs = bt?.seconds ?? 0n;
  if (as !== bs) return as < bs ? 1 : -1;
  const an = at?.nanos ?? 0;
  const bn = bt?.nanos ?? 0;
  if (an !== bn) return an < bn ? 1 : -1;
  if (a.id !== b.id) return a.id < b.id ? 1 : -1;
  return 0;
};

// mergeHeadPage folds a freshly fetched first page into the
// accumulated feed without collapsing pages loaded via Load more.
//
// The head page is authoritative for its own window (everything at or
// newer than its oldest row): rows it carries replace held copies
// (picking up edits), rows it doesn't carry were deleted upstream and
// drop out. Held rows older than the window are kept untouched —
// stale until the next full refresh, which is normal feed behavior.
// An empty head page means the feed itself is empty.
export const mergeHeadPage = (prev: Note[], head: Note[]): Note[] => {
  if (head.length === 0) return [];
  const windowFloor = head[head.length - 1];
  const headIds = new Set(head.map((n) => n.id));
  const olderThanWindow = prev.filter((n) => !headIds.has(n.id) && feedCmp(n, windowFloor) > 0);
  return [...head, ...olderThanWindow];
};

// Feed state for the shared team notepad: cursor accumulation +
// Load more mirroring useActivity, plus refreshHead() for the poll
// tick so the visible top of the feed stays live without resetting
// scroll position or loaded pages.
export function useNotesFeed({ pageSize = 25 }: UseNotesFeedParams = {}): UseNotesFeedResult {
  const { handleAuthErrors } = useAuthErrors();

  const [notes, setNotes] = useState<Note[]>([]);
  const [isLoading, setIsLoading] = useState(false);
  const [hasLoaded, setHasLoaded] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [hasMore, setHasMore] = useState(false);
  const [pageToken, setPageToken] = useState("");

  const requestIdRef = useRef(0);

  const fetchNotes = useCallback(
    async (token: string, append: boolean) => {
      const requestId = ++requestIdRef.current;
      setIsLoading(true);
      setError(null);

      try {
        const response = await notesClient.listNotes({ pageSize, pageToken: token });
        if (requestId !== requestIdRef.current) return;

        const { notes: newNotes, nextPageToken } = response;
        if (append) {
          setNotes((prev) => [...prev, ...newNotes]);
        } else {
          setNotes(newNotes);
        }
        setPageToken(nextPageToken);
        setHasMore(nextPageToken !== "");
        setHasLoaded(true);
      } catch (err) {
        if (requestId !== requestIdRef.current) return;
        handleAuthErrors({
          error: err,
          onError: (e) => {
            setError(getErrorMessage(e, "Failed to load notes"));
          },
        });
      } finally {
        if (requestId === requestIdRef.current) {
          setIsLoading(false);
        }
      }
    },
    [pageSize, handleAuthErrors],
  );

  // Ref-based stability (same pattern as useActivity.ts).
  const fetchRef = useRef(fetchNotes);
  useEffect(() => {
    fetchRef.current = fetchNotes;
  }, [fetchNotes]);

  const pageTokenRef = useRef(pageToken);
  useEffect(() => {
    pageTokenRef.current = pageToken;
  }, [pageToken]);

  const isLoadingRef = useRef(isLoading);
  useEffect(() => {
    isLoadingRef.current = isLoading;
  }, [isLoading]);

  const hasMoreRef = useRef(hasMore);
  useEffect(() => {
    hasMoreRef.current = hasMore;
  }, [hasMore]);

  const pageSizeRef = useRef(pageSize);
  useEffect(() => {
    pageSizeRef.current = pageSize;
  }, [pageSize]);

  const loadMore = useCallback(() => {
    if (hasMoreRef.current && !isLoadingRef.current) {
      void fetchRef.current(pageTokenRef.current, true);
    }
  }, []);

  const refresh = useCallback(() => {
    if (isLoadingRef.current) return;
    setNotes([]);
    setPageToken("");
    setHasMore(false);
    void fetchRef.current("", false);
  }, []);

  // refreshHead is the poll tick: re-fetch page 1 only and merge it
  // into the accumulated list. It deliberately bypasses the
  // isLoading/cursor state so an in-flight Load more and a poll can't
  // corrupt each other — the merge is associative with appends.
  const refreshHead = useCallback(async () => {
    try {
      const response = await notesClient.listNotes({ pageSize: pageSizeRef.current, pageToken: "" });
      const head = response.notes;
      setNotes((prev) => mergeHeadPage(prev, head));
      setHasLoaded(true);
      if (head.length === 0) {
        // Feed emptied upstream: any held cursor points at deleted rows.
        setPageToken("");
        setHasMore(false);
      }
    } catch (err) {
      // Poll-tick failures are deliberately silent: the feed keeps its
      // last-good rows and the next tick retries. Auth errors still
      // route through the shared handler so an expired session logs out.
      handleAuthErrors({ error: err, onError: () => undefined });
    }
  }, [handleAuthErrors]);

  return { notes, isLoading, hasLoaded, error, hasMore, loadMore, refresh, refreshHead };
}
