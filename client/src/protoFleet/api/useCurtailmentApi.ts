import { useCallback, useMemo, useRef, useState } from "react";
import { create } from "@bufbuild/protobuf";
import { Code, ConnectError } from "@connectrpc/connect";

import {
  type ActiveCurtailmentSnapshot,
  applyActiveCurtailmentEvent,
  clearMutationBackedActiveCurtailmentEvent,
  dismissActiveCurtailmentEvent,
  fetchActiveCurtailmentData,
  getActiveCurtailmentSnapshot,
  preserveActiveCurtailmentEvents,
  selectActiveCurtailmentEvent,
  useActiveCurtailmentEvent,
  useActiveCurtailmentEvents,
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
  GetCurtailmentEventRequestSchema,
  ListCurtailmentEventsRequestSchema,
  type CurtailmentEvent as ProtoCurtailmentEvent,
  CurtailmentEventState as ProtoCurtailmentEventState,
  StopCurtailmentRequestSchema,
} from "@/protoFleet/api/generated/curtailment/v1/curtailment_pb";
import { assertNotAborted, isAbortError, isAuthOrPermissionError, toError } from "@/protoFleet/api/requestErrors";
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
  activeEvents: CurtailmentHistoryEvent[];
  activeEventId: string | null;
  activeEventFormValues: CurtailmentSubmitValues | null;
  historyEvents: CurtailmentHistoryEvent[];
}

interface CurtailmentHistoryPage {
  events: ProtoCurtailmentEvent[];
  nextPageToken: string;
}

interface ReadableRestoringEvents {
  eventIds: Set<string>;
  terminalEvents: ProtoCurtailmentEvent[];
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
  selectActiveCurtailment: (
    eventUuid: string,
    options?: Pick<RefreshCurtailmentOptions, "signal">,
  ) => Promise<Omit<CurtailmentSnapshot, "activeEvents" | "historyEvents">>;
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
  activeEvents: [],
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
const activeReconciliationHistoryPageLimit = 3;
const activeReconciliationHistoryStateFilters: CurtailmentEventState[] = [
  "completed",
  "completedWithFailures",
  "cancelled",
  "failed",
];
const vanishedRestoringUnreadableErrorCodes = new Set<Code>([Code.NotFound, Code.PermissionDenied]);

function isVanishedRestoringUnreadableError(error: unknown): boolean {
  return error instanceof ConnectError && vanishedRestoringUnreadableErrorCodes.has(error.code);
}

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
): Omit<CurtailmentSnapshot, "activeEvents" | "historyEvents"> {
  const nextActiveEvent = getActiveSnapshotEvent(activeEvent);

  return {
    activeEvent: nextActiveEvent,
    activeEventId: activeEvent && nextActiveEvent ? activeEvent.eventUuid : null,
    activeEventFormValues: activeEvent && nextActiveEvent ? mapCurtailmentEventToFormValues(activeEvent) : null,
  };
}

function markInjectedActiveHistoryEvent(event: CurtailmentHistoryEvent): CurtailmentHistoryEvent {
  return {
    ...event,
    injectedActive: true,
  };
}

function getActiveHistoryEvent(
  activeEvent: ProtoCurtailmentEvent,
  historyEvents: CurtailmentHistoryEvent[],
): CurtailmentHistoryEvent {
  const mappedActiveEvent = mapActiveCurtailmentHistoryEvent(activeEvent);
  const matchingHistoryEvent = historyEvents.find((event) => event.id === mappedActiveEvent.id);

  if (!matchingHistoryEvent) {
    return markInjectedActiveHistoryEvent(mappedActiveEvent);
  }

  if (!mappedActiveEvent.displayState && mappedActiveEvent.state === matchingHistoryEvent.state) {
    return matchingHistoryEvent;
  }

  if (!mappedActiveEvent.targetMetricsAvailable && matchingHistoryEvent.targetMetricsAvailable) {
    return {
      ...mappedActiveEvent,
      selectedMiners: matchingHistoryEvent.selectedMiners,
      estimatedReductionKw: matchingHistoryEvent.estimatedReductionKw,
      targetKw: matchingHistoryEvent.targetKw,
      targetMetricsAvailable: true,
      sourceLabel: matchingHistoryEvent.sourceLabel,
    };
  }

  return {
    ...mappedActiveEvent,
    sourceLabel: matchingHistoryEvent.sourceLabel,
  };
}

function getActiveEventInputs(
  activeEvents: ProtoCurtailmentEvent[],
  selectedActiveEvent: ProtoCurtailmentEvent | undefined,
): ProtoCurtailmentEvent[] {
  if (!selectedActiveEvent) {
    return activeEvents;
  }

  const selectedEventIndex = activeEvents.findIndex((event) => event.eventUuid === selectedActiveEvent.eventUuid);
  if (selectedEventIndex === -1) {
    return [selectedActiveEvent, ...activeEvents];
  }

  return activeEvents.map((event, index) => (index === selectedEventIndex ? selectedActiveEvent : event));
}

function isRestoringCurtailmentEvent(event: ProtoCurtailmentEvent): boolean {
  return event.state === ProtoCurtailmentEventState.RESTORING;
}

function isTerminalProtoCurtailmentEvent(event: ProtoCurtailmentEvent): boolean {
  return historyTerminalCurtailmentEventStates.has(mapCurtailmentEventState(event.state));
}

function mergeUniqueCurtailmentEvents(
  preferredEvents: ProtoCurtailmentEvent[],
  events: ProtoCurtailmentEvent[],
): ProtoCurtailmentEvent[] {
  if (preferredEvents.length === 0) {
    return events;
  }
  const preferredEventIds = new Set(preferredEvents.map((event) => event.eventUuid));
  return [...preferredEvents, ...events.filter((event) => !preferredEventIds.has(event.eventUuid))];
}

function hasTerminalHistoryEvent(event: ProtoCurtailmentEvent, historyEvents: ProtoCurtailmentEvent[]): boolean {
  const matchingHistoryEvent = historyEvents.find((historyEvent) => historyEvent.eventUuid === event.eventUuid);
  return Boolean(
    matchingHistoryEvent &&
    historyTerminalCurtailmentEventStates.has(mapCurtailmentEventState(matchingHistoryEvent.state)),
  );
}

function getVanishedRestoringEvents(
  previousActiveEvents: ProtoCurtailmentEvent[],
  nextActiveEvents: ProtoCurtailmentEvent[],
): ProtoCurtailmentEvent[] {
  const nextActiveEventIds = new Set(nextActiveEvents.map((event) => event.eventUuid));
  return previousActiveEvents.filter(
    (event) => isRestoringCurtailmentEvent(event) && !nextActiveEventIds.has(event.eventUuid),
  );
}

function getVanishedRestoringEventsToPreserve(
  previousActiveEvents: ProtoCurtailmentEvent[],
  nextActiveEvents: ProtoCurtailmentEvent[],
  historyEvents: ProtoCurtailmentEvent[],
): ProtoCurtailmentEvent[] {
  return getVanishedRestoringEvents(previousActiveEvents, nextActiveEvents).filter(
    (event) => !hasTerminalHistoryEvent(event, historyEvents),
  );
}

function hasTerminalHistoryEventId(eventUuid: string, historyEvents: CurtailmentHistoryEvent[]): boolean {
  const matchingHistoryEvent = historyEvents.find((event) => event.id === eventUuid);
  return Boolean(matchingHistoryEvent && historyTerminalCurtailmentEventStates.has(matchingHistoryEvent.state));
}

function getActiveSnapshotForReconciliation(
  currentSnapshot: ActiveCurtailmentSnapshot,
  previousSnapshot: ActiveCurtailmentSnapshot,
  historyEvents: CurtailmentHistoryEvent[],
  readableVanishedRestoringEventIds = new Set<string>(),
): ActiveCurtailmentSnapshot {
  const currentEventUuids = new Set(currentSnapshot.events.map((event) => event.eventUuid));
  if (currentSnapshot.event) {
    currentEventUuids.add(currentSnapshot.event.eventUuid);
  }
  const vanishedRestoringEvents = previousSnapshot.events.filter(
    (event) =>
      event.state === ProtoCurtailmentEventState.RESTORING &&
      !currentEventUuids.has(event.eventUuid) &&
      readableVanishedRestoringEventIds.has(event.eventUuid) &&
      !hasTerminalHistoryEventId(event.eventUuid, historyEvents),
  );
  const event =
    previousSnapshot.event?.state === ProtoCurtailmentEventState.RESTORING &&
    !currentEventUuids.has(previousSnapshot.event.eventUuid) &&
    readableVanishedRestoringEventIds.has(previousSnapshot.event.eventUuid) &&
    !hasTerminalHistoryEventId(previousSnapshot.event.eventUuid, historyEvents)
      ? previousSnapshot.event
      : currentSnapshot.event;

  return {
    event,
    events:
      vanishedRestoringEvents.length > 0
        ? [...currentSnapshot.events, ...vanishedRestoringEvents]
        : currentSnapshot.events,
  };
}

function getActiveHistoryEvents(
  activeEvents: ProtoCurtailmentEvent[],
  selectedActiveEvent: ProtoCurtailmentEvent | undefined,
  historyEvents: CurtailmentHistoryEvent[],
  stateFilters: readonly CurtailmentEventState[] = [],
): CurtailmentHistoryEvent[] {
  return getActiveEventInputs(activeEvents, selectedActiveEvent)
    .filter((event) => shouldIncludeActiveEventInHistory(event, stateFilters))
    .map((event) => getActiveHistoryEvent(event, historyEvents));
}

function createSnapshot(
  activeEvent: ProtoCurtailmentEvent | undefined,
  activeEvents: ProtoCurtailmentEvent[],
  historyEvents: ProtoCurtailmentEvent[],
  stateFilters: readonly CurtailmentEventState[] = [],
  includeActiveInHistory = true,
): CurtailmentSnapshot {
  const nextHistoryEvents = historyEvents.map(mapCurtailmentHistoryEvent);
  const activeHistoryEvents = getActiveHistoryEvents(activeEvents, activeEvent, nextHistoryEvents);

  if (includeActiveInHistory && activeHistoryEvents.length > 0) {
    const filteredActiveHistoryEvents = getActiveHistoryEvents(
      activeEvents,
      activeEvent,
      nextHistoryEvents,
      stateFilters,
    );
    const filteredActiveEventIds = new Set(filteredActiveHistoryEvents.map((event) => event.id));
    return {
      ...getActiveSnapshotFields(activeEvent),
      activeEvents: activeHistoryEvents,
      historyEvents: [
        ...filteredActiveHistoryEvents,
        ...nextHistoryEvents.filter((event) => !filteredActiveEventIds.has(event.id)),
      ],
    };
  }

  return {
    ...getActiveSnapshotFields(activeEvent),
    activeEvents: activeHistoryEvents,
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

function getRestoringEventForTerminalReconciliation(
  activeEvent: ProtoCurtailmentEvent | undefined,
  fallbackEvent: ProtoCurtailmentEvent | undefined,
): ProtoCurtailmentEvent | undefined {
  if (activeEvent?.state === ProtoCurtailmentEventState.RESTORING) {
    return activeEvent;
  }

  return fallbackEvent?.state === ProtoCurtailmentEventState.RESTORING ? fallbackEvent : undefined;
}

function reconcileTerminalRestoringEventWithHistory(
  restoringEvent: ProtoCurtailmentEvent | undefined,
  historyEvents: ProtoCurtailmentEvent[],
): ProtoCurtailmentEvent | undefined {
  if (!restoringEvent) {
    return undefined;
  }

  const matchingHistoryEvent = historyEvents.find((event) => event.eventUuid === restoringEvent.eventUuid);
  if (!matchingHistoryEvent) {
    return undefined;
  }

  const historyState = mapCurtailmentEventState(matchingHistoryEvent.state);
  return historyTerminalCurtailmentEventStates.has(historyState)
    ? reconcileActiveEventWithHistory(restoringEvent, historyEvents)
    : undefined;
}

function reconcileActiveEventWithTerminalFallback(
  activeEvent: ProtoCurtailmentEvent | undefined,
  fallbackEvent: ProtoCurtailmentEvent | undefined,
  historyEvents: ProtoCurtailmentEvent[],
): ProtoCurtailmentEvent | undefined {
  const terminalFallbackEvent = reconcileTerminalRestoringEventWithHistory(fallbackEvent, historyEvents);
  if (terminalFallbackEvent) {
    return terminalFallbackEvent;
  }

  const reconciledActiveEvent = reconcileActiveEventWithHistory(activeEvent, historyEvents);
  if (reconciledActiveEvent || activeEvent) {
    return reconciledActiveEvent;
  }

  return fallbackEvent?.state === ProtoCurtailmentEventState.RESTORING ? fallbackEvent : undefined;
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

function removeInjectedActiveHistoryEvent(events: CurtailmentHistoryEvent[]): CurtailmentHistoryEvent[] {
  return events.filter((event) => !event.injectedActive);
}

function getHistoryEventsWithActiveEvent(
  events: CurtailmentHistoryEvent[],
  activeEvents: ProtoCurtailmentEvent[],
  selectedActiveEvent: ProtoCurtailmentEvent | undefined,
  stateFilters: readonly CurtailmentEventState[],
  currentPage: number,
): CurtailmentHistoryEvent[] {
  if (currentPage !== 0) {
    return events;
  }

  const activeHistoryEvents = getActiveHistoryEvents(activeEvents, selectedActiveEvent, events, stateFilters);
  if (activeHistoryEvents.length === 0) {
    return removeInjectedActiveHistoryEvent(events);
  }

  const activeHistoryEventIds = new Set(activeHistoryEvents.map((event) => event.id));
  return [
    ...activeHistoryEvents,
    ...events.filter((event) => !activeHistoryEventIds.has(event.id) && !event.injectedActive),
  ];
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
  const activeCurtailmentEvents = useActiveCurtailmentEvents();
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
  const activeReconciliationSnapshotRef = useRef<ActiveCurtailmentSnapshot>(getActiveCurtailmentSnapshot());

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
      const activeSnapshot = applyActiveCurtailmentEvent(event, {
        mergeActiveEvents: true,
        preserveAgainstStaleRefresh: true,
      });
      activeReconciliationSnapshotRef.current = activeSnapshot;
      const nextActiveSnapshotEvent = activeSnapshot.event;
      const nextActiveEvent = getActiveSnapshotEvent(nextActiveSnapshotEvent);
      const activeStatusFilters = historyStatusFiltersRef.current;
      const shouldUpdateHistoryPage =
        historyPaginationRef.current.currentPage === 0 &&
        (activeStatusFilters.length === 0 || activeStatusFilters.includes(state));

      updateSnapshot((current) => ({
        activeEvent: nextActiveEvent,
        activeEvents: getActiveHistoryEvents(
          activeSnapshot.events,
          activeSnapshot.event,
          current.historyEvents,
          activeStatusFilters,
        ),
        activeEventId: nextActiveSnapshotEvent && nextActiveEvent ? nextActiveSnapshotEvent.eventUuid : null,
        activeEventFormValues:
          nextActiveSnapshotEvent && nextActiveEvent ? mapCurtailmentEventToFormValues(nextActiveSnapshotEvent) : null,
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

  const findCurtailmentEventInHistory = useCallback(
    async (
      eventUuid: string,
      stateFilters: CurtailmentEventState[],
      pageLimit: number,
      signal?: AbortSignal,
    ): Promise<ProtoCurtailmentEvent | undefined> => {
      let pageToken = "";
      const knownPageTokens: (string | undefined)[] = [undefined];
      let pageCount = 0;

      while (pageCount < pageLimit) {
        const page = await listCurtailmentEventsPage(pageToken, knownPageTokens, stateFilters, signal);
        const matchingEvent = page.events.find((event) => event.eventUuid === eventUuid);
        if (matchingEvent) {
          return matchingEvent;
        }

        if (!page.nextPageToken) {
          return undefined;
        }

        pageToken = page.nextPageToken;
        knownPageTokens.push(pageToken);
        pageCount += 1;
      }

      return undefined;
    },
    [listCurtailmentEventsPage],
  );

  const findReadableRestoringEvents = useCallback(
    async (events: ProtoCurtailmentEvent[], signal?: AbortSignal): Promise<ReadableRestoringEvents> => {
      const readableEvents: ReadableRestoringEvents = {
        eventIds: new Set<string>(),
        terminalEvents: [],
      };
      if (events.length === 0) {
        return readableEvents;
      }

      await Promise.all(
        events.map(async (event) => {
          try {
            const response = await curtailmentClient.getCurtailmentEvent(
              create(GetCurtailmentEventRequestSchema, {
                eventUuid: event.eventUuid,
                targetPageSize: 1,
              }),
              signal ? { signal } : undefined,
            );
            assertNotAborted(signal);
            if (response.event?.state === ProtoCurtailmentEventState.RESTORING) {
              readableEvents.eventIds.add(event.eventUuid);
            } else if (response.event && isTerminalProtoCurtailmentEvent(response.event)) {
              readableEvents.eventIds.add(event.eventUuid);
              readableEvents.terminalEvents.push(response.event);
            }
          } catch (error) {
            if (isAbortError(error, signal)) {
              throw error;
            }
            if (isVanishedRestoringUnreadableError(error)) {
              return;
            }
            throw error;
          }
        }),
      );

      return readableEvents;
    },
    [],
  );

  const getHistoryEventsForActiveReconciliation = useCallback(
    async (
      activeEvent: ProtoCurtailmentEvent | undefined,
      historyEvents: ProtoCurtailmentEvent[],
      signal?: AbortSignal,
    ): Promise<ProtoCurtailmentEvent[]> => {
      if (
        !activeEvent ||
        activeEvent.state !== ProtoCurtailmentEventState.RESTORING ||
        historyEvents.some((historyEvent) => historyEvent.eventUuid === activeEvent.eventUuid)
      ) {
        return historyEvents;
      }

      const matchingEvent = await findCurtailmentEventInHistory(
        activeEvent.eventUuid,
        activeReconciliationHistoryStateFilters,
        activeReconciliationHistoryPageLimit,
        signal,
      );
      return matchingEvent ? [matchingEvent] : historyEvents;
    },
    [findCurtailmentEventInHistory],
  );

  const getHistoryEventsForVanishedRestoringReconciliation = useCallback(
    async (
      previousActiveEvents: ProtoCurtailmentEvent[],
      nextActiveEvents: ProtoCurtailmentEvent[],
      historyEvents: ProtoCurtailmentEvent[],
      signal?: AbortSignal,
    ): Promise<ProtoCurtailmentEvent[]> => {
      const vanishedRestoringEvents = getVanishedRestoringEvents(previousActiveEvents, nextActiveEvents).filter(
        (event) => !hasTerminalHistoryEvent(event, historyEvents),
      );
      if (vanishedRestoringEvents.length === 0) {
        return historyEvents;
      }

      const terminalEvents = (
        await Promise.all(
          vanishedRestoringEvents.map((event) =>
            findCurtailmentEventInHistory(
              event.eventUuid,
              activeReconciliationHistoryStateFilters,
              activeReconciliationHistoryPageLimit,
              signal,
            ),
          ),
        )
      ).filter((event): event is ProtoCurtailmentEvent => Boolean(event));

      const historyEventIds = new Set(historyEvents.map((event) => event.eventUuid));
      return [...terminalEvents.filter((event) => !historyEventIds.has(event.eventUuid)), ...historyEvents];
    },
    [findCurtailmentEventInHistory],
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
          const currentActiveDataSnapshot = getActiveCurtailmentSnapshot();
          const previousReconciliationSnapshot = activeReconciliationSnapshotRef.current;
          const readableFallbackRestoringEvents = await findReadableRestoringEvents(
            getVanishedRestoringEvents(
              getActiveEventInputs(previousReconciliationSnapshot.events, previousReconciliationSnapshot.event),
              currentActiveDataSnapshot.events,
            ),
            signal,
          );
          const fallbackActiveSnapshot = getActiveSnapshotForReconciliation(
            currentActiveDataSnapshot,
            previousReconciliationSnapshot,
            snapshotRef.current.historyEvents,
            readableFallbackRestoringEvents.eventIds,
          );
          const fallbackActiveEvent = fallbackActiveSnapshot.event;
          const fallbackActiveEvents = fallbackActiveSnapshot.events;
          const [activeRefresh, historyPageResponse] = await Promise.all([
            includeActive ? fetchActiveCurtailmentData({ signal }) : undefined,
            listCurtailmentEventsPage(pageToken ?? "", knownPageTokens, stateFilters, signal),
          ]);
          assertNotAborted(signal);
          const currentActiveSnapshot = getActiveSnapshotForReconciliation(
            getActiveCurtailmentSnapshot(),
            activeReconciliationSnapshotRef.current,
            snapshotRef.current.historyEvents,
            readableFallbackRestoringEvents.eventIds,
          );
          const currentActiveEvent = currentActiveSnapshot.event ?? fallbackActiveEvent;
          const previewActiveSnapshot = activeRefresh ?? currentActiveSnapshot;
          const previewActiveEvents = previewActiveSnapshot.events;
          const reconciliationBaseEvent = getRestoringEventForTerminalReconciliation(
            previewActiveSnapshot.event,
            currentActiveEvent,
          );
          const fallbackNonSelectedActiveEvents = reconciliationBaseEvent
            ? fallbackActiveEvents.filter((event) => event.eventUuid !== reconciliationBaseEvent.eventUuid)
            : fallbackActiveEvents;
          const reconciliationEvents = mergeUniqueCurtailmentEvents(
            readableFallbackRestoringEvents.terminalEvents,
            await getHistoryEventsForActiveReconciliation(reconciliationBaseEvent, historyPageResponse.events, signal),
          );
          let activeReconciliationEvents = await getHistoryEventsForVanishedRestoringReconciliation(
            fallbackNonSelectedActiveEvents,
            previewActiveSnapshot.events,
            reconciliationEvents,
            signal,
          );
          assertNotAborted(signal);
          const previewActiveEvent = reconcileActiveEventWithTerminalFallback(
            previewActiveSnapshot.event,
            currentActiveEvent,
            activeReconciliationEvents,
          );
          const previewSnapshot = createSnapshot(
            previewActiveEvent,
            previewActiveEvents,
            historyPageResponse.events,
            stateFilters,
            historyPage === 0,
          );
          if (requestId !== latestRefreshRequestIdRef.current) {
            return previewSnapshot;
          }

          activeReconciliationEvents
            .filter((event) => historyTerminalCurtailmentEventStates.has(mapCurtailmentEventState(event.state)))
            .forEach((event) => clearMutationBackedActiveCurtailmentEvent(event.eventUuid));
          const activeSnapshot = activeRefresh ? activeRefresh.commit() : previewActiveSnapshot;
          const preservedRestoringCandidates = getVanishedRestoringEventsToPreserve(
            fallbackNonSelectedActiveEvents,
            activeSnapshot.events,
            activeReconciliationEvents,
          );
          const readablePreservedRestoringEvents = await findReadableRestoringEvents(
            preservedRestoringCandidates,
            signal,
          );
          assertNotAborted(signal);
          if (requestId !== latestRefreshRequestIdRef.current) {
            return previewSnapshot;
          }
          activeReconciliationEvents = mergeUniqueCurtailmentEvents(
            readablePreservedRestoringEvents.terminalEvents,
            activeReconciliationEvents,
          );
          const preservedRestoringEvents = preservedRestoringCandidates.filter((event) =>
            readablePreservedRestoringEvents.eventIds.has(event.eventUuid),
          );
          const activeEvents =
            preservedRestoringEvents.length > 0
              ? preserveActiveCurtailmentEvents(preservedRestoringEvents).events
              : activeSnapshot.events;
          const activeEvent = reconcileActiveEventWithTerminalFallback(
            activeSnapshot.event,
            currentActiveEvent,
            activeReconciliationEvents,
          );
          if (activeEvent !== activeSnapshot.event) {
            applyActiveCurtailmentEvent(activeEvent, { mergeActiveEvents: true });
          }
          activeReconciliationSnapshotRef.current = { event: activeEvent, events: activeEvents };
          const nextSnapshot =
            activeEvent === previewActiveEvent && preservedRestoringEvents.length === 0
              ? previewSnapshot
              : createSnapshot(activeEvent, activeEvents, historyPageResponse.events, stateFilters, historyPage === 0);

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
            if (isAuthOrPermissionError(error)) {
              activeReconciliationSnapshotRef.current = applyActiveCurtailmentEvent(undefined);
            }
            setLoadError(resolvedError.message);
          }
          throw resolvedError;
        }
      })();
    },
    [
      findReadableRestoringEvents,
      getHistoryEventsForVanishedRestoringReconciliation,
      getHistoryEventsForActiveReconciliation,
      handleFailure,
      listCurtailmentEventsPage,
      updateHistoryPagination,
      updateSnapshot,
    ],
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

  const selectActiveCurtailment = useCallback(
    async (eventUuid: string, { signal }: Pick<RefreshCurtailmentOptions, "signal"> = {}) => {
      try {
        const activeSnapshot = await selectActiveCurtailmentEvent(eventUuid, { signal });
        activeReconciliationSnapshotRef.current = activeSnapshot;
        const activeSnapshotFields = getActiveSnapshotFields(activeSnapshot.event);
        const activeStatusFilters = historyStatusFiltersRef.current;
        updateSnapshot((current) => ({
          ...current,
          ...activeSnapshotFields,
          activeEvents: getActiveHistoryEvents(
            activeSnapshot.events,
            activeSnapshot.event,
            current.historyEvents,
            activeStatusFilters,
          ),
          historyEvents: getHistoryEventsWithActiveEvent(
            current.historyEvents,
            activeSnapshot.events,
            activeSnapshot.event,
            activeStatusFilters,
            historyPaginationRef.current.currentPage,
          ),
        }));
        setLoadError(null);
        return activeSnapshotFields;
      } catch (error) {
        if (isAbortError(error, signal)) {
          throw error;
        }

        const resolvedError = handleFailure(error, "Failed to load curtailment detail.");
        if (isAuthOrPermissionError(error)) {
          activeReconciliationSnapshotRef.current = applyActiveCurtailmentEvent(undefined);
        }
        setLoadError(resolvedError.message);
        throw resolvedError;
      }
    },
    [handleFailure, updateSnapshot],
  );

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
    activeReconciliationSnapshotRef.current = dismissActiveCurtailmentEvent(activeCurtailmentEvent?.eventUuid);
  }, [activeCurtailmentEvent]);

  const activeSnapshotFields = useMemo(() => getActiveSnapshotFields(activeCurtailmentEvent), [activeCurtailmentEvent]);
  const activeHistoryEvents = useMemo(
    () => getActiveHistoryEvents(activeCurtailmentEvents, activeCurtailmentEvent, snapshot.historyEvents),
    [activeCurtailmentEvent, activeCurtailmentEvents, snapshot.historyEvents],
  );
  const historyStatusFilter = historyStatusFilters[0];
  const historyEvents = useMemo(
    () =>
      getHistoryEventsWithActiveEvent(
        snapshot.historyEvents,
        activeCurtailmentEvents,
        activeCurtailmentEvent,
        historyStatusFilters,
        historyPagination.currentPage,
      ),
    [
      activeCurtailmentEvent,
      activeCurtailmentEvents,
      historyPagination.currentPage,
      historyStatusFilters,
      snapshot.historyEvents,
    ],
  );

  return useMemo(
    () => ({
      ...snapshot,
      ...activeSnapshotFields,
      activeEvents: activeHistoryEvents,
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
      selectActiveCurtailment,
      startCurtailment,
      dismissTerminalCurtailment,
      updateCurtailment,
      stopCurtailment,
    }),
    [
      activeSnapshotFields,
      activeHistoryEvents,
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
      selectActiveCurtailment,
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
