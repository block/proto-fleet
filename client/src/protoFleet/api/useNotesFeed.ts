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
// stale until the next full refresh, which is normal feed behavior —
// unless headIsComplete reports the head as the entire feed, in which
// case anything below the window was deleted upstream and drops too.
// An empty head page means the feed itself is empty.
// Same note in the feed sense: id plus updated_at. Content cannot
// change without the server's updated_at trigger advancing, so this
// pair captures edits without comparing content bytes.
const sameNote = (a: Note, b: Note): boolean =>
  a.id === b.id &&
  (a.updatedAt?.seconds ?? 0n) === (b.updatedAt?.seconds ?? 0n) &&
  (a.updatedAt?.nanos ?? 0) === (b.updatedAt?.nanos ?? 0);

export const mergeHeadPage = (prev: Note[], head: Note[], headIsComplete = false): Note[] => {
  let next: Note[];
  if (head.length === 0 || headIsComplete) {
    next = head;
  } else {
    const windowFloor = head[head.length - 1];
    const headIds = new Set(head.map((n) => n.id));
    const olderThanWindow = prev.filter((n) => !headIds.has(n.id) && feedCmp(n, windowFloor) > 0);
    next = [...head, ...olderThanWindow];
  }
  // Same-reference bail: the poll tick calls this inside a setState
  // updater, and React only skips the re-render when the updater
  // returns the previous reference. Head rows are fresh objects every
  // fetch, so reference equality alone would never hold.
  if (next.length === prev.length && next.every((note, i) => sameNote(note, prev[i]))) {
    return prev;
  }
  return next;
};

// Feed state for the shared team notepad: cursor accumulation +
// Load more mirroring useActivity, plus refreshHead() for the poll
// tick so the visible top of the feed stays live while retaining
// loaded pages whenever the refreshed window reaches the cached feed.
export function useNotesFeed({ pageSize = 25 }: UseNotesFeedParams = {}): UseNotesFeedResult {
  const { handleAuthErrors } = useAuthErrors();

  const [notes, setNotes] = useState<Note[]>([]);
  const [isLoading, setIsLoading] = useState(false);
  const [hasLoaded, setHasLoaded] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [hasMore, setHasMore] = useState(false);
  const [pageToken, setPageToken] = useState("");

  const requestIdRef = useRef(0);
  const headRequestIdRef = useRef(0);
  const notesRef = useRef(notes);
  useEffect(() => {
    notesRef.current = notes;
  }, [notes]);

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
          setNotes((prev) => {
            const heldIds = new Set(prev.map((note) => note.id));
            return [...prev, ...newNotes.filter((note) => !heldIds.has(note.id))];
          });
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

  const hasLoadedRef = useRef(hasLoaded);
  useEffect(() => {
    hasLoadedRef.current = hasLoaded;
  }, [hasLoaded]);

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

  // Catch up through a contiguous window before merging into cached pages.
  // A bounded catch-up falls back to a fresh prefix and its matching cursor,
  // so even a large burst cannot leave an unreachable gap in the feed.
  const refreshHead = useCallback(async () => {
    const headRequestId = ++headRequestIdRef.current;
    const requestId = requestIdRef.current;
    const cachedHead = notesRef.current[0];
    const isCurrent = () => headRequestId === headRequestIdRef.current && requestId === requestIdRef.current;
    try {
      const head: Note[] = [];
      let nextPageToken = "";
      let reachedCache = cachedHead === undefined;
      const maxCatchUpPages = 10;
      for (let page = 0; page < maxCatchUpPages; page++) {
        const response = await notesClient.listNotes({
          pageSize: pageSizeRef.current,
          pageToken: nextPageToken,
        });
        if (!isCurrent()) return;
        head.push(...response.notes);
        nextPageToken = response.nextPageToken;
        const floor = head[head.length - 1];
        reachedCache = cachedHead === undefined || (floor !== undefined && feedCmp(floor, cachedHead) >= 0);
        if (nextPageToken === "" || reachedCache) break;
      }
      const headIsComplete = nextPageToken === "";
      const replaceCache = headIsComplete || !reachedCache || cachedHead === undefined;
      setNotes((prev) => mergeHeadPage(prev, head, replaceCache));
      setHasLoaded(true);
      setError(null);
      if (replaceCache) {
        // Invalidate any Load more that began against the old cache. Its
        // response must not restore an obsolete cursor or deleted rows.
        ++requestIdRef.current;
        setIsLoading(false);
        setPageToken(nextPageToken);
        setHasMore(nextPageToken !== "");
      }
    } catch (err) {
      // Poll-tick failures after a successful load are deliberately
      // silent: the feed keeps its last-good rows and the next tick
      // retries. Before the first load there are no rows to keep, so
      // surface the error instead of an indefinite spinner — the poll
      // keeps running, and a later success clears the callout. Auth
      // errors still route through the shared handler so an expired
      // session logs out.
      if (!isCurrent()) return;
      handleAuthErrors({
        error: err,
        onError: (e) => {
          if (!hasLoadedRef.current) {
            setError(getErrorMessage(e, "Failed to load notes"));
          }
        },
      });
    }
  }, [handleAuthErrors]);

  return { notes, isLoading, hasLoaded, error, hasMore, loadMore, refresh, refreshHead };
}
