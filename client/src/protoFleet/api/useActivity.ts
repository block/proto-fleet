import { useCallback, useEffect, useRef, useState } from "react";
import { equals } from "@bufbuild/protobuf";
import { activityClient } from "@/protoFleet/api/clients";
import {
  type ActivityEntry,
  type ActivityFilter,
  ActivityFilterSchema,
} from "@/protoFleet/api/generated/activity/v1/activity_pb";
import { getErrorMessage } from "@/protoFleet/api/getErrorMessage";
import { useAuthErrors } from "@/protoFleet/store";

interface UseActivityParams {
  filter?: ActivityFilter;
  pageSize?: number;
}

interface UseActivityResult {
  activities: ActivityEntry[];
  totalCount: number;
  isLoading: boolean;
  error: string | null;
  hasMore: boolean;
  loadMore: () => void;
  refresh: () => void;
}

export function useActivity({ filter, pageSize = 50 }: UseActivityParams): UseActivityResult {
  const { handleAuthErrors } = useAuthErrors();

  const [activities, setActivities] = useState<ActivityEntry[]>([]);
  const [totalCount, setTotalCount] = useState(0);
  const [isLoading, setIsLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [hasMore, setHasMore] = useState(false);
  const [pageToken, setPageToken] = useState("");

  const requestIdRef = useRef(0);

  const fetchActivities = useCallback(
    async (currentFilter: ActivityFilter | undefined, token: string, append: boolean) => {
      const requestId = ++requestIdRef.current;
      setIsLoading(true);
      setError(null);

      try {
        const response = await activityClient.listActivities({
          filter: currentFilter,
          pageSize,
          pageToken: token,
        });

        if (requestId !== requestIdRef.current) return;

        const { activities: newActivities, nextPageToken, totalCount: responseTotalCount } = response;

        if (append) {
          setActivities((prev) => [...prev, ...newActivities]);
        } else {
          setActivities(newActivities);
          setTotalCount(responseTotalCount);
        }

        setPageToken(nextPageToken);
        setHasMore(nextPageToken !== "");
      } catch (err) {
        if (requestId !== requestIdRef.current) return;
        handleAuthErrors({
          error: err,
          onError: (e) => {
            setError(getErrorMessage(e, "Failed to load activities"));
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

  // Ref-based stability (same pattern as useFleet.ts)
  const fetchRef = useRef(fetchActivities);
  useEffect(() => {
    fetchRef.current = fetchActivities;
  }, [fetchActivities]);

  const filterRef = useRef(filter);
  useEffect(() => {
    filterRef.current = filter;
  }, [filter]);

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

  const loadMore = useCallback(() => {
    if (hasMoreRef.current && !isLoadingRef.current) {
      fetchRef.current(filterRef.current, pageTokenRef.current, true);
    }
  }, []);

  const refresh = useCallback(() => {
    if (isLoadingRef.current) return;
    setActivities([]);
    setPageToken("");
    setHasMore(false);
    setTotalCount(0);
    fetchRef.current(filterRef.current, "", false);
  }, []);

  // Re-fetch when filter or pageSize changes (deep equality for filter)
  const previousFilterRef = useRef<ActivityFilter | undefined>(undefined);
  const previousPageSizeRef = useRef(pageSize);
  const hasLoadedRef = useRef(false);

  useEffect(() => {
    const filtersEqual =
      previousFilterRef.current === filter ||
      (previousFilterRef.current !== undefined &&
        filter !== undefined &&
        equals(ActivityFilterSchema, previousFilterRef.current, filter));
    const pageSizeChanged = previousPageSizeRef.current !== pageSize;

    if (hasLoadedRef.current && filtersEqual && !pageSizeChanged) return;

    previousFilterRef.current = filter;
    previousPageSizeRef.current = pageSize;
    hasLoadedRef.current = true;

    setActivities([]);
    setPageToken("");
    setHasMore(false);
    setTotalCount(0);

    void fetchRef.current(filter, "", false);
  }, [filter, pageSize]);

  return { activities, totalCount, isLoading, error, hasMore, loadMore, refresh };
}
