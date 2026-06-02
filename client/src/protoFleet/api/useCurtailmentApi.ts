import { useCallback, useMemo, useRef, useState } from "react";
import { create } from "@bufbuild/protobuf";

import {
  applyActiveCurtailmentEvent,
  dismissActiveCurtailmentEvent,
  fetchActiveCurtailmentData,
  getActiveCurtailmentSnapshot,
  useActiveCurtailmentEvent,
} from "@/protoFleet/api/activeCurtailmentData";
import { curtailmentClient } from "@/protoFleet/api/clients";
import { emitCurtailmentChanged } from "@/protoFleet/api/curtailmentEvents";
import {
  mapActiveCurtailmentEvent,
  mapActiveCurtailmentHistoryEvent,
  mapCurtailmentEventToFormValues,
  mapCurtailmentHistoryEvent,
} from "@/protoFleet/api/curtailmentMappers";
import {
  CurtailmentEventSchema,
  ListCurtailmentEventsRequestSchema,
  type CurtailmentEvent as ProtoCurtailmentEvent,
  CurtailmentEventState as ProtoCurtailmentEventState,
  StopCurtailmentRequestSchema,
} from "@/protoFleet/api/generated/curtailment/v1/curtailment_pb";
import { assertNotAborted, isAbortError, toError } from "@/protoFleet/api/requestErrors";
import type { ActiveCurtailmentEvent } from "@/protoFleet/features/energy/ActiveCurtailmentStatus";
import {
  type CurtailmentEventState,
  curtailmentEventStates,
  mapCurtailmentEventState,
} from "@/protoFleet/features/energy/curtailmentDisplayUtils";
import type { CurtailmentHistoryEvent } from "@/protoFleet/features/energy/CurtailmentHistory";
import {
  buildStartCurtailmentRequest,
  buildUpdateCurtailmentEventRequest,
} from "@/protoFleet/features/energy/curtailmentRequestBuilders";
import type { CurtailmentSubmitValues } from "@/protoFleet/features/energy/CurtailmentStartModal";
import { useAuthErrors } from "@/protoFleet/store";

export interface RefreshCurtailmentOptions {
  background?: boolean;
  historyPage?: number;
  includeActive?: boolean;
  signal?: AbortSignal;
}

interface CurtailmentSnapshot {
  activeEvent: ActiveCurtailmentEvent | null;
  activeEventId: string | null;
  activeEventFormValues: CurtailmentSubmitValues | null;
  historyEvents: CurtailmentHistoryEvent[];
}

interface CurtailmentHistoryPage {
  events: ProtoCurtailmentEvent[];
  nextPageToken: string;
}

interface CurtailmentHistoryPaginationState {
  currentPage: number;
  nextPageToken: string;
  pageTokens: (string | undefined)[];
}

export interface UseCurtailmentApiResult extends CurtailmentSnapshot {
  isLoading: boolean;
  isStarting: boolean;
  isUpdating: boolean;
  stoppingEventId: string | null;
  loadError: string | null;
  startError: string | null;
  updateError: string | null;
  stopError: string | null;
  historyCurrentPage: number;
  historyHasNextPage: boolean;
  historyHasPreviousPage: boolean;
  historyPageSize: number;
  historyStatusFilter?: CurtailmentEventState;
  historyStatusFilters: CurtailmentEventState[];
  refreshCurtailment: (options?: RefreshCurtailmentOptions) => Promise<CurtailmentSnapshot>;
  goToHistoryPage: (
    historyPage: number,
    options?: Pick<RefreshCurtailmentOptions, "signal">,
  ) => Promise<CurtailmentSnapshot>;
  setHistoryStatusFilter: (
    stateFilter?: CurtailmentEventState,
    options?: Pick<RefreshCurtailmentOptions, "signal">,
  ) => Promise<CurtailmentSnapshot>;
  setHistoryStatusFilters: (
    stateFilters?: CurtailmentEventState[],
    options?: Pick<RefreshCurtailmentOptions, "signal">,
  ) => Promise<CurtailmentSnapshot>;
  startCurtailment: (values: CurtailmentSubmitValues) => Promise<ProtoCurtailmentEvent>;
  dismissTerminalCurtailment: () => void;
  updateCurtailment: (
    eventUuid: string,
    values: CurtailmentSubmitValues,
    initialValues?: Partial<CurtailmentSubmitValues>,
  ) => Promise<ProtoCurtailmentEvent>;
  stopCurtailment: (eventUuid: string) => Promise<ProtoCurtailmentEvent>;
}

const curtailmentHistoryPageSize = 50;
const initialHistoryPagination: CurtailmentHistoryPaginationState = {
  currentPage: 0,
  nextPageToken: "",
  pageTokens: [undefined],
};
const initialCurtailmentSnapshot: CurtailmentSnapshot = {
  activeEvent: null,
  activeEventId: null,
  activeEventFormValues: null,
  historyEvents: [],
};

const visibleActiveCurtailmentEventStates = new Set<CurtailmentEventState>([
  "pending",
  "active",
  "restoring",
  "completed",
  "completedWithFailures",
]);

const historyTerminalCurtailmentEventStates = new Set<CurtailmentEventState>([
  "completed",
  "completedWithFailures",
  "cancelled",
  "failed",
]);

function mapHistoryStateFilter(stateFilter?: CurtailmentEventState): ProtoCurtailmentEventState {
  switch (stateFilter) {
    case "pending":
      return ProtoCurtailmentEventState.PENDING;
    case "active":
      return ProtoCurtailmentEventState.ACTIVE;
    case "restoring":
      return ProtoCurtailmentEventState.RESTORING;
    case "completed":
      return ProtoCurtailmentEventState.COMPLETED;
    case "completedWithFailures":
      return ProtoCurtailmentEventState.COMPLETED_WITH_FAILURES;
    case "cancelled":
      return ProtoCurtailmentEventState.CANCELLED;
    case "failed":
      return ProtoCurtailmentEventState.FAILED;
    default:
      return ProtoCurtailmentEventState.UNSPECIFIED;
  }
}

function normalizeHistoryStateFilters(stateFilters: readonly CurtailmentEventState[] = []): CurtailmentEventState[] {
  const selectedStateFilters = new Set(stateFilters);
  return curtailmentEventStates.filter((state) => selectedStateFilters.has(state));
}

function mapHistoryStateFilters(stateFilters: readonly CurtailmentEventState[]): ProtoCurtailmentEventState[] {
  return normalizeHistoryStateFilters(stateFilters)
    .map(mapHistoryStateFilter)
    .filter((state) => state !== ProtoCurtailmentEventState.UNSPECIFIED);
}

function getActiveSnapshotEvent(activeEvent: ProtoCurtailmentEvent | undefined): ActiveCurtailmentEvent | null {
  if (!activeEvent) {
    return null;
  }

  const activeState = mapCurtailmentEventState(activeEvent.state);
  if (!visibleActiveCurtailmentEventStates.has(activeState)) {
    return null;
  }

  return mapActiveCurtailmentEvent(activeEvent);
}

function getActiveSnapshotFields(
  activeEvent: ProtoCurtailmentEvent | undefined,
): Omit<CurtailmentSnapshot, "historyEvents"> {
  const nextActiveEvent = getActiveSnapshotEvent(activeEvent);

  return {
    activeEvent: nextActiveEvent,
    activeEventId: activeEvent && nextActiveEvent ? activeEvent.eventUuid : null,
    activeEventFormValues: activeEvent && nextActiveEvent ? mapCurtailmentEventToFormValues(activeEvent) : null,
  };
}

function getActiveHistoryEvent(
  activeEvent: ProtoCurtailmentEvent,
  historyEvents: CurtailmentHistoryEvent[],
): CurtailmentHistoryEvent {
  const mappedActiveEvent = mapActiveCurtailmentHistoryEvent(activeEvent);
  const matchingHistoryEvent = historyEvents.find((event) => event.id === mappedActiveEvent.id);

  if (!matchingHistoryEvent) {
    return mappedActiveEvent;
  }

  if (!mappedActiveEvent.displayState) {
    return matchingHistoryEvent;
  }

  return {
    ...mappedActiveEvent,
    displayState: mappedActiveEvent.displayState,
    sourceLabel: matchingHistoryEvent.sourceLabel,
  };
}

function createSnapshot(
  activeEvent: ProtoCurtailmentEvent | undefined,
  historyEvents: ProtoCurtailmentEvent[],
  includeActiveInHistory = true,
): CurtailmentSnapshot {
  const nextHistoryEvents = historyEvents.map(mapCurtailmentHistoryEvent);

  if (includeActiveInHistory && activeEvent) {
    const activeHistoryEvent = getActiveHistoryEvent(activeEvent, nextHistoryEvents);
    return {
      ...getActiveSnapshotFields(activeEvent),
      historyEvents: [activeHistoryEvent, ...nextHistoryEvents.filter((event) => event.id !== activeHistoryEvent.id)],
    };
  }

  return {
    ...getActiveSnapshotFields(activeEvent),
    historyEvents: nextHistoryEvents,
  };
}

function reconcileActiveEventWithHistory(
  activeEvent: ProtoCurtailmentEvent | undefined,
  historyEvents: ProtoCurtailmentEvent[],
): ProtoCurtailmentEvent | undefined {
  if (!activeEvent) {
    return undefined;
  }

  const matchingHistoryEvent = historyEvents.find((event) => event.eventUuid === activeEvent.eventUuid);
  if (!matchingHistoryEvent) {
    return activeEvent;
  }

  const historyState = mapCurtailmentEventState(matchingHistoryEvent.state);
  if (!historyTerminalCurtailmentEventStates.has(historyState)) {
    return activeEvent;
  }

  const hasHistoryTargetSummary = Boolean(matchingHistoryEvent.targetRollup || matchingHistoryEvent.targets.length > 0);
  const targets =
    matchingHistoryEvent.targets.length > 0
      ? matchingHistoryEvent.targets
      : hasHistoryTargetSummary
        ? activeEvent.targets
        : [];
  return create(CurtailmentEventSchema, {
    ...activeEvent,
    state: matchingHistoryEvent.state,
    endedAt: matchingHistoryEvent.endedAt ?? activeEvent.endedAt,
    updatedAt: matchingHistoryEvent.updatedAt ?? activeEvent.updatedAt,
    targetRollup: matchingHistoryEvent.targetRollup,
    targets,
  });
}

function shouldIncludeActiveEventInHistory(
  activeEvent: ProtoCurtailmentEvent | undefined,
  stateFilters: readonly CurtailmentEventState[],
): boolean {
  return (
    stateFilters.length === 0 ||
    Boolean(activeEvent && stateFilters.includes(mapCurtailmentEventState(activeEvent.state)))
  );
}

function removeInjectedActiveHistoryEvent(
  events: CurtailmentHistoryEvent[],
  activeEvent?: ProtoCurtailmentEvent,
): CurtailmentHistoryEvent[] {
  if (!activeEvent) {
    return events.filter((event) => !event.displayState);
  }

  return events.filter((event) => event.id !== activeEvent.eventUuid);
}

function getHistoryEventsWithActiveEvent(
  events: CurtailmentHistoryEvent[],
  activeEvent: ProtoCurtailmentEvent | undefined,
  stateFilters: readonly CurtailmentEventState[],
  currentPage: number,
): CurtailmentHistoryEvent[] {
  if (currentPage !== 0) {
    return events;
  }

  if (!activeEvent || !shouldIncludeActiveEventInHistory(activeEvent, stateFilters)) {
    return removeInjectedActiveHistoryEvent(events, activeEvent);
  }

  const activeHistoryEvent = getActiveHistoryEvent(activeEvent, events);
  return [activeHistoryEvent, ...events.filter((event) => event.id !== activeHistoryEvent.id)];
}

function upsertHistoryEvent(
  events: CurtailmentHistoryEvent[],
  event: ProtoCurtailmentEvent,
): CurtailmentHistoryEvent[] {
  const state = mapCurtailmentEventState(event.state);
  const mappedEvent = visibleActiveCurtailmentEventStates.has(state)
    ? mapActiveCurtailmentHistoryEvent(event)
    : mapCurtailmentHistoryEvent(event);
  return [mappedEvent, ...events.filter((currentEvent) => currentEvent.id !== mappedEvent.id)];
}

function getNormalizedHistoryPage(historyPage: number): number {
  return Number.isFinite(historyPage) && historyPage > 0 ? Math.floor(historyPage) : 0;
}

function getSafeNextPageToken(
  nextPageToken: string,
  currentPageToken: string,
  knownPageTokens: (string | undefined)[],
): string {
  if (!nextPageToken) {
    return "";
  }

  const seenPageTokens = new Set(knownPageTokens.map((pageToken) => pageToken ?? ""));
  return nextPageToken === currentPageToken || seenPageTokens.has(nextPageToken) ? "" : nextPageToken;
}

export function useCurtailmentApi(): UseCurtailmentApiResult {
  const { handleAuthErrors } = useAuthErrors();
  const activeCurtailmentEvent = useActiveCurtailmentEvent();
  const [snapshot, setSnapshot] = useState<CurtailmentSnapshot>(initialCurtailmentSnapshot);
  const [isLoading, setIsLoading] = useState(false);
  const [isStarting, setIsStarting] = useState(false);
  const [updatingEventId, setUpdatingEventId] = useState<string | null>(null);
  const [stoppingEventId, setStoppingEventId] = useState<string | null>(null);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [startError, setStartError] = useState<string | null>(null);
  const [updateError, setUpdateError] = useState<string | null>(null);
  const [stopError, setStopError] = useState<string | null>(null);
  const [historyPagination, setHistoryPagination] =
    useState<CurtailmentHistoryPaginationState>(initialHistoryPagination);
  const [historyStatusFilters, setHistoryStatusFiltersState] = useState<CurtailmentEventState[]>([]);
  const snapshotRef = useRef(snapshot);
  const historyPaginationRef = useRef(historyPagination);
  const historyStatusFiltersRef = useRef(historyStatusFilters);
  const latestRefreshRequestIdRef = useRef(0);
  const foregroundRefreshCountRef = useRef(0);

  const updateSnapshot = useCallback(
    (snapshotUpdater: CurtailmentSnapshot | ((current: CurtailmentSnapshot) => CurtailmentSnapshot)) => {
      setSnapshot((current) => {
        const nextSnapshot = typeof snapshotUpdater === "function" ? snapshotUpdater(current) : snapshotUpdater;
        snapshotRef.current = nextSnapshot;
        return nextSnapshot;
      });
    },
    [],
  );

  const updateHistoryPagination = useCallback((nextPagination: CurtailmentHistoryPaginationState) => {
    historyPaginationRef.current = nextPagination;
    setHistoryPagination(nextPagination);
  }, []);

  const updateHistoryStatusFilters = useCallback((nextStatusFilters: CurtailmentEventState[] = []) => {
    const normalizedStatusFilters = normalizeHistoryStateFilters(nextStatusFilters);
    historyStatusFiltersRef.current = normalizedStatusFilters;
    setHistoryStatusFiltersState(normalizedStatusFilters);
  }, []);

  const handleFailure = useCallback(
    (error: unknown, fallbackMessage: string) => {
      const resolvedError = toError(error, fallbackMessage);
      handleAuthErrors({ error });
      return resolvedError;
    },
    [handleAuthErrors],
  );

  const applyEvent = useCallback(
    (event: ProtoCurtailmentEvent) => {
      const state = mapCurtailmentEventState(event.state);
      const shouldShowActiveEvent = visibleActiveCurtailmentEventStates.has(state);
      applyActiveCurtailmentEvent(shouldShowActiveEvent ? event : undefined);
      const nextActiveEvent = shouldShowActiveEvent ? mapActiveCurtailmentEvent(event) : null;
      const activeStatusFilters = historyStatusFiltersRef.current;
      const shouldUpdateHistoryPage =
        historyPaginationRef.current.currentPage === 0 &&
        (activeStatusFilters.length === 0 || activeStatusFilters.includes(state));

      updateSnapshot((current) => ({
        activeEvent: nextActiveEvent,
        activeEventId: nextActiveEvent ? event.eventUuid : null,
        activeEventFormValues: nextActiveEvent ? mapCurtailmentEventToFormValues(event) : null,
        historyEvents: shouldUpdateHistoryPage
          ? upsertHistoryEvent(current.historyEvents, event)
          : current.historyEvents,
      }));
    },
    [updateSnapshot],
  );

  const listCurtailmentEventsPage = useCallback(
    async (
      pageToken: string,
      knownPageTokens: (string | undefined)[],
      stateFilters: CurtailmentEventState[],
      signal?: AbortSignal,
    ): Promise<CurtailmentHistoryPage> => {
      assertNotAborted(signal);

      const response = await curtailmentClient.listCurtailmentEvents(
        create(ListCurtailmentEventsRequestSchema, {
          pageSize: curtailmentHistoryPageSize,
          pageToken,
          stateFilters: mapHistoryStateFilters(stateFilters),
        }),
        signal ? { signal } : undefined,
      );
      assertNotAborted(signal);

      return {
        events: response.events,
        nextPageToken: getSafeNextPageToken(response.nextPageToken, pageToken, knownPageTokens),
      };
    },
    [],
  );

  const runRefresh = useCallback(
    (signal?: AbortSignal, requestedHistoryPage = historyPaginationRef.current.currentPage, includeActive = true) => {
      const historyPage = getNormalizedHistoryPage(requestedHistoryPage);
      const currentPagination = historyPaginationRef.current;
      const stateFilters = historyStatusFiltersRef.current;
      const pageToken = historyPage === 0 ? "" : currentPagination.pageTokens[historyPage];

      if (historyPage > 0 && pageToken === undefined) {
        return Promise.resolve(snapshotRef.current);
      }

      const requestId = ++latestRefreshRequestIdRef.current;
      const knownPageTokens = currentPagination.pageTokens.slice(0, historyPage + 1);

      return (async () => {
        try {
          const [activeRefresh, historyPageResponse] = await Promise.all([
            includeActive ? fetchActiveCurtailmentData({ signal }) : undefined,
            listCurtailmentEventsPage(pageToken ?? "", knownPageTokens, stateFilters, signal),
          ]);
          assertNotAborted(signal);
          const previewActiveSnapshot = activeRefresh ?? getActiveCurtailmentSnapshot();
          const previewActiveEvent = reconcileActiveEventWithHistory(
            previewActiveSnapshot.event,
            historyPageResponse.events,
          );
          const previewSnapshot = createSnapshot(
            previewActiveEvent,
            historyPageResponse.events,
            historyPage === 0 && shouldIncludeActiveEventInHistory(previewActiveEvent, stateFilters),
          );
          if (requestId !== latestRefreshRequestIdRef.current) {
            return previewSnapshot;
          }

          const activeSnapshot = activeRefresh ? activeRefresh.commit() : previewActiveSnapshot;
          const activeEvent = reconcileActiveEventWithHistory(activeSnapshot.event, historyPageResponse.events);
          if (activeEvent !== activeSnapshot.event) {
            applyActiveCurtailmentEvent(activeEvent);
          }
          const nextSnapshot =
            activeEvent === previewActiveEvent
              ? previewSnapshot
              : createSnapshot(
                  activeEvent,
                  historyPageResponse.events,
                  historyPage === 0 && shouldIncludeActiveEventInHistory(activeEvent, stateFilters),
                );

          const nextPageTokens = currentPagination.pageTokens.slice(0, historyPage + 1);
          nextPageTokens[historyPage] = pageToken || undefined;
          if (historyPageResponse.nextPageToken) {
            nextPageTokens[historyPage + 1] = historyPageResponse.nextPageToken;
          }

          updateSnapshot(nextSnapshot);
          updateHistoryPagination({
            currentPage: historyPage,
            nextPageToken: historyPageResponse.nextPageToken,
            pageTokens: nextPageTokens,
          });
          setLoadError(null);
          return nextSnapshot;
        } catch (error) {
          if (isAbortError(error, signal)) {
            throw error;
          }

          const resolvedError = handleFailure(error, "Failed to load curtailment data.");
          if (requestId === latestRefreshRequestIdRef.current) {
            setLoadError(resolvedError.message);
          }
          throw resolvedError;
        }
      })();
    },
    [handleFailure, listCurtailmentEventsPage, updateHistoryPagination, updateSnapshot],
  );

  const refreshCurtailment = useCallback(
    async ({ background = false, historyPage, includeActive = true, signal }: RefreshCurtailmentOptions = {}) => {
      if (background) {
        return runRefresh(signal, historyPage, includeActive);
      }

      foregroundRefreshCountRef.current += 1;
      setIsLoading(true);

      try {
        return await runRefresh(signal, historyPage, includeActive);
      } finally {
        foregroundRefreshCountRef.current = Math.max(0, foregroundRefreshCountRef.current - 1);
        if (!signal?.aborted) {
          setIsLoading(foregroundRefreshCountRef.current > 0);
        }
      }
    },
    [runRefresh],
  );

  const goToHistoryPage = useCallback(
    (historyPage: number, options: Pick<RefreshCurtailmentOptions, "signal"> = {}) =>
      refreshCurtailment({ historyPage, signal: options.signal }),
    [refreshCurtailment],
  );

  const setHistoryStatusFilters = useCallback(
    (stateFilters: CurtailmentEventState[] = [], options: Pick<RefreshCurtailmentOptions, "signal"> = {}) => {
      updateHistoryStatusFilters(stateFilters);
      updateHistoryPagination(initialHistoryPagination);
      return refreshCurtailment({ historyPage: 0, signal: options.signal });
    },
    [refreshCurtailment, updateHistoryPagination, updateHistoryStatusFilters],
  );

  const setHistoryStatusFilter = useCallback(
    (stateFilter?: CurtailmentEventState, options: Pick<RefreshCurtailmentOptions, "signal"> = {}) =>
      setHistoryStatusFilters(stateFilter ? [stateFilter] : [], options),
    [setHistoryStatusFilters],
  );

  const refreshAfterMutation = useCallback(async () => {
    emitCurtailmentChanged();

    try {
      await refreshCurtailment({ background: true, historyPage: 0, includeActive: false });
    } catch {
      // The mutation succeeded; keep the response-backed optimistic state and
      // leave the load error visible for the next explicit refresh.
    }
  }, [refreshCurtailment]);

  const startCurtailment = useCallback(
    async (values: CurtailmentSubmitValues) => {
      setIsStarting(true);
      setStartError(null);

      try {
        const response = await curtailmentClient.startCurtailment(buildStartCurtailmentRequest(values));
        if (!response.event) {
          throw new Error("Started curtailment response was missing an event.");
        }

        applyEvent(response.event);
        await refreshAfterMutation();
        return response.event;
      } catch (error) {
        const resolvedError = handleFailure(error, "Failed to start curtailment.");
        setStartError(resolvedError.message);
        throw resolvedError;
      } finally {
        setIsStarting(false);
      }
    },
    [applyEvent, handleFailure, refreshAfterMutation],
  );

  const updateCurtailment = useCallback(
    async (eventUuid: string, values: CurtailmentSubmitValues, initialValues?: Partial<CurtailmentSubmitValues>) => {
      setUpdatingEventId(eventUuid);
      setUpdateError(null);

      try {
        const response = await curtailmentClient.updateCurtailmentEvent(
          buildUpdateCurtailmentEventRequest(eventUuid, values, initialValues),
        );
        if (!response.event) {
          throw new Error("Updated curtailment response was missing an event.");
        }

        applyEvent(response.event);
        await refreshAfterMutation();
        return response.event;
      } catch (error) {
        const resolvedError = handleFailure(error, "Failed to update curtailment.");
        setUpdateError(resolvedError.message);
        throw resolvedError;
      } finally {
        setUpdatingEventId((currentEventId) => (currentEventId === eventUuid ? null : currentEventId));
      }
    },
    [applyEvent, handleFailure, refreshAfterMutation],
  );

  const stopCurtailment = useCallback(
    async (eventUuid: string) => {
      setStoppingEventId(eventUuid);
      setStopError(null);

      try {
        const response = await curtailmentClient.stopCurtailment(
          create(StopCurtailmentRequestSchema, { eventUuid, force: false }),
        );
        if (!response.event) {
          throw new Error("Stopped curtailment response was missing an event.");
        }

        applyEvent(response.event);
        await refreshAfterMutation();
        return response.event;
      } catch (error) {
        const resolvedError = handleFailure(error, "Failed to stop curtailment.");
        setStopError(resolvedError.message);
        throw resolvedError;
      } finally {
        setStoppingEventId((currentEventId) => (currentEventId === eventUuid ? null : currentEventId));
      }
    },
    [applyEvent, handleFailure, refreshAfterMutation],
  );

  const dismissTerminalCurtailment = useCallback(() => {
    dismissActiveCurtailmentEvent(activeCurtailmentEvent?.eventUuid);
  }, [activeCurtailmentEvent]);

  const activeSnapshotFields = useMemo(() => getActiveSnapshotFields(activeCurtailmentEvent), [activeCurtailmentEvent]);
  const historyStatusFilter = historyStatusFilters[0];
  const historyEvents = useMemo(
    () =>
      getHistoryEventsWithActiveEvent(
        snapshot.historyEvents,
        activeCurtailmentEvent,
        historyStatusFilters,
        historyPagination.currentPage,
      ),
    [activeCurtailmentEvent, historyPagination.currentPage, historyStatusFilters, snapshot.historyEvents],
  );

  return useMemo(
    () => ({
      ...snapshot,
      ...activeSnapshotFields,
      historyEvents,
      isLoading,
      isStarting,
      isUpdating: updatingEventId !== null,
      stoppingEventId,
      loadError,
      startError,
      updateError,
      stopError,
      historyCurrentPage: historyPagination.currentPage,
      historyHasNextPage: historyPagination.nextPageToken !== "",
      historyHasPreviousPage: historyPagination.currentPage > 0,
      historyPageSize: curtailmentHistoryPageSize,
      historyStatusFilter,
      historyStatusFilters,
      refreshCurtailment,
      goToHistoryPage,
      setHistoryStatusFilter,
      setHistoryStatusFilters,
      startCurtailment,
      dismissTerminalCurtailment,
      updateCurtailment,
      stopCurtailment,
    }),
    [
      activeSnapshotFields,
      goToHistoryPage,
      historyEvents,
      historyPagination.currentPage,
      historyPagination.nextPageToken,
      historyStatusFilter,
      historyStatusFilters,
      isLoading,
      isStarting,
      updatingEventId,
      loadError,
      refreshCurtailment,
      setHistoryStatusFilter,
      setHistoryStatusFilters,
      snapshot,
      startCurtailment,
      dismissTerminalCurtailment,
      updateCurtailment,
      stopCurtailment,
      stopError,
      stoppingEventId,
      startError,
      updateError,
    ],
  );
}
