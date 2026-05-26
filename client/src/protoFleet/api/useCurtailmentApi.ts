import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { create } from "@bufbuild/protobuf";

import { curtailmentClient } from "@/protoFleet/api/clients";
import { isUnimplementedConnectError } from "@/protoFleet/api/connectErrorHelpers";
import {
  type CurtailmentMappedTargetState,
  curtailmentTargetRollupStates,
  getCurtailmentDecisionSnapshotNumber,
  getCurtailmentEstimatedReductionKw,
  getCurtailmentScopeLabel,
  getCurtailmentSelectedMinerCount,
  getCurtailmentTargetSummary,
  isActiveCurtailmentEventState,
  mapCurtailmentEventState,
  mapCurtailmentEventStateToProto,
  mapCurtailmentTargetState,
} from "@/protoFleet/api/curtailmentEventMappers";
import {
  type CurtailmentEvent,
  type FixedKwParams,
  GetActiveCurtailmentRequestSchema,
  ListCurtailmentEventsRequestSchema,
  CurtailmentMode as ProtoCurtailmentMode,
  CurtailmentPriority as ProtoCurtailmentPriority,
  type StartCurtailmentRequest,
  StopCurtailmentRequestSchema,
  type UpdateCurtailmentEventRequest,
} from "@/protoFleet/api/generated/curtailment/v1/curtailment_pb";
import {
  type CurtailmentActiveEvent,
  type CurtailmentApi,
  type CurtailmentEventState,
  type CurtailmentHistoryEvent,
  type CurtailmentListState,
  type CurtailmentPriority,
  type CurtailmentTargetRollup,
} from "@/protoFleet/features/energy/types";
import { useAuthErrors } from "@/protoFleet/store";

function ensureError(error: unknown, fallbackMessage: string): Error {
  if (error instanceof Error) {
    return error;
  }

  if (typeof error === "string") {
    return new Error(error);
  }

  return new Error(fallbackMessage);
}

function timestampToIso(timestamp?: { seconds: bigint; nanos: number }): string | undefined {
  if (!timestamp) {
    return undefined;
  }

  return new Date(Number(timestamp.seconds) * 1000 + Math.floor(timestamp.nanos / 1_000_000)).toISOString();
}

function getFixedKwParams(event: Pick<CurtailmentEvent, "modeParams">): FixedKwParams | undefined {
  return event.modeParams.case === "fixedKw" ? event.modeParams.value : undefined;
}

function getModeLabel(event: CurtailmentEvent): string {
  if (event.mode === ProtoCurtailmentMode.FIXED_KW) {
    const fixedKw = getFixedKwParams(event);
    return fixedKw ? `${fixedKw.targetKw.toLocaleString()} kW target` : "Power target";
  }

  return "Unsupported mode";
}

function mapPriority(priority: ProtoCurtailmentPriority): CurtailmentPriority {
  return priority === ProtoCurtailmentPriority.EMERGENCY ? "emergency" : "normal";
}

function buildRollups(event: CurtailmentEvent): CurtailmentTargetRollup[] {
  const serverRollup = event.targetRollup;

  if (serverRollup) {
    return curtailmentTargetRollupStates.map((state) => ({ state, count: serverRollup[state] }));
  }

  const counts = new Map<CurtailmentMappedTargetState, number>();
  for (const target of event.targets) {
    const state = mapCurtailmentTargetState(target.state);
    counts.set(state, (counts.get(state) ?? 0) + 1);
  }

  return curtailmentTargetRollupStates.map((state) => ({ state, count: counts.get(state) ?? 0 }));
}

function getRemainingPowerKw(event: CurtailmentEvent): number | undefined {
  return getCurtailmentDecisionSnapshotNumber(event, ["estimated_remaining_power_kw", "estimatedRemainingPowerKw"]);
}

function getSourceLabel(event: CurtailmentEvent): string {
  if (event.externalSource && event.externalReference) {
    return `${event.externalSource} - ${event.externalReference}`;
  }

  if (event.externalSource) {
    return event.externalSource;
  }

  return "Manual/API";
}

function getStartedAt(event: CurtailmentEvent): string {
  return timestampToIso(event.startedAt) ?? timestampToIso(event.createdAt) ?? new Date().toISOString();
}

function getObservedReductionKw(event: CurtailmentEvent, estimatedReductionKw: number): number {
  const observedWatts = event.targets.reduce((total, target) => total + (target.observedPowerW ?? 0), 0);

  if (observedWatts <= 0) {
    return estimatedReductionKw;
  }

  return Math.max(estimatedReductionKw - observedWatts / 1000, 0);
}

const curtailmentHistoryPageSize = 100;
const maxCurtailmentHistoryPages = 10;

async function listCurtailmentEventsPageSet(stateFilter?: CurtailmentEventState): Promise<CurtailmentEvent[]> {
  const events: CurtailmentEvent[] = [];
  let pageToken = "";
  let pageCount = 0;

  do {
    const response = await curtailmentClient.listCurtailmentEvents(
      create(ListCurtailmentEventsRequestSchema, {
        pageSize: curtailmentHistoryPageSize,
        pageToken,
        stateFilter: stateFilter ? mapCurtailmentEventStateToProto(stateFilter) : undefined,
      }),
    );

    events.push(...response.events);
    pageCount += 1;
    pageToken = response.nextPageToken;
  } while (pageToken && pageCount < maxCurtailmentHistoryPages);

  return events;
}

async function listAllCurtailmentEvents(stateFilters?: CurtailmentEventState[]): Promise<CurtailmentEvent[]> {
  if (!stateFilters || stateFilters.length === 0) {
    return listCurtailmentEventsPageSet();
  }

  const eventsByState = await Promise.all(stateFilters.map((stateFilter) => listCurtailmentEventsPageSet(stateFilter)));
  return eventsByState.flat();
}

async function listAvailableCurtailmentEvents(stateFilters?: CurtailmentEventState[]): Promise<CurtailmentEvent[]> {
  try {
    return await listAllCurtailmentEvents(stateFilters);
  } catch (error) {
    if (isUnimplementedConnectError(error)) {
      return [];
    }

    throw error;
  }
}

function mapActiveEvent(event: CurtailmentEvent): CurtailmentActiveEvent {
  const fixedKw = getFixedKwParams(event);
  const estimatedReductionKw = getCurtailmentEstimatedReductionKw(event);

  return {
    id: event.eventUuid,
    reason: event.reason,
    state: mapCurtailmentEventState(event.state),
    priority: mapPriority(event.priority),
    scopeLabel: getCurtailmentScopeLabel(event),
    sourceLabel: getSourceLabel(event),
    startedAt: getStartedAt(event),
    modeLabel: getModeLabel(event),
    targetSummary: getCurtailmentTargetSummary(event),
    selectedMiners: getCurtailmentSelectedMinerCount(event),
    estimatedReductionKw,
    targetKw: fixedKw?.targetKw,
    toleranceKw: fixedKw?.toleranceKw,
    observedReductionKw: getObservedReductionKw(event, estimatedReductionKw),
    remainingPowerKw: getRemainingPowerKw(event),
    restoreBatchSize: event.effectiveBatchSize || event.restoreBatchSize,
    restoreBatchIntervalSec: event.restoreBatchIntervalSec,
    rollups: buildRollups(event),
    rawEvent: event,
  };
}

function mapHistoryEvent(event: CurtailmentEvent): CurtailmentHistoryEvent {
  const fixedKw = getFixedKwParams(event);

  return {
    id: event.eventUuid,
    reason: event.reason,
    state: mapCurtailmentEventState(event.state),
    mode: "fixedKw",
    modeLabel: getModeLabel(event),
    priority: mapPriority(event.priority),
    scopeLabel: getCurtailmentScopeLabel(event),
    selectedMiners: getCurtailmentSelectedMinerCount(event),
    estimatedReductionKw: getCurtailmentEstimatedReductionKw(event),
    targetKw: fixedKw?.targetKw,
    toleranceKw: fixedKw?.toleranceKw,
    sourceLabel: getSourceLabel(event),
    startedAt: timestampToIso(event.startedAt),
    endedAt: timestampToIso(event.endedAt),
    scheduledAt: timestampToIso(event.scheduledStartAt),
    createdAt: timestampToIso(event.createdAt),
    rawEvent: event,
  };
}

function requireResponseEvent(event: CurtailmentEvent | undefined, action: string): CurtailmentEvent {
  if (!event) {
    throw new Error(`${action} curtailment response was missing an event.`);
  }

  return event;
}

type HistoryEventUpdater = (
  currentEvents: CurtailmentHistoryEvent[],
  nextEvent: CurtailmentHistoryEvent,
) => CurtailmentHistoryEvent[];

function prependHistoryEvent(
  currentEvents: CurtailmentHistoryEvent[],
  nextEvent: CurtailmentHistoryEvent,
): CurtailmentHistoryEvent[] {
  return [nextEvent, ...currentEvents.filter((event) => event.id !== nextEvent.id)];
}

function replaceHistoryEvent(
  currentEvents: CurtailmentHistoryEvent[],
  nextEvent: CurtailmentHistoryEvent,
): CurtailmentHistoryEvent[] {
  return currentEvents.map((event) => (event.id === nextEvent.id ? nextEvent : event));
}

function isRestoredTerminalState(state: CurtailmentEventState): boolean {
  return state === "completed" || state === "completedWithFailures";
}

function hasActiveHistoryEvent(events: CurtailmentEvent[]): boolean {
  return events.some((event) => isActiveCurtailmentEventState(mapCurtailmentEventState(event.state)));
}

async function refreshHistoryEventsAfterActiveClears(
  activeEvent: CurtailmentEvent | undefined,
  historyEvents: CurtailmentEvent[],
  stateFilters?: CurtailmentEventState[],
): Promise<CurtailmentEvent[]> {
  if (activeEvent || !hasActiveHistoryEvent(historyEvents)) {
    return historyEvents;
  }

  return listAllCurtailmentEvents(stateFilters);
}

function getRestoredActiveEvent(
  currentActiveEvent: CurtailmentActiveEvent | undefined,
  events: CurtailmentEvent[],
): CurtailmentActiveEvent | undefined {
  if (
    currentActiveEvent === undefined ||
    (currentActiveEvent.state !== "restoring" && !isRestoredTerminalState(currentActiveEvent.state))
  ) {
    return undefined;
  }

  const restoredEvent = events.find((event) => {
    const state = mapCurtailmentEventState(event.state);
    return event.eventUuid === currentActiveEvent.id && isRestoredTerminalState(state);
  });

  return restoredEvent ? mapActiveEvent(restoredEvent) : undefined;
}

function getNextActiveEvent(
  activeResponseEvent: CurtailmentEvent | undefined,
  currentActiveEvent: CurtailmentActiveEvent | undefined,
  historyEvents: CurtailmentEvent[],
): CurtailmentActiveEvent | undefined {
  if (activeResponseEvent) {
    return mapActiveEvent(activeResponseEvent);
  }

  return getRestoredActiveEvent(currentActiveEvent, historyEvents);
}

export function useCurtailmentApi(): CurtailmentApi {
  const { handleAuthErrors } = useAuthErrors();
  const [activeEvent, setActiveEvent] = useState<CurtailmentActiveEvent | undefined>();
  const activeEventRef = useRef<CurtailmentActiveEvent | undefined>(undefined);
  const [events, setEvents] = useState<CurtailmentHistoryEvent[]>([]);
  const [isLoading, setIsLoading] = useState(false);
  const throwCurtailmentApiError = useCallback(
    (error: unknown, fallbackMessage: string): never => {
      if (isUnimplementedConnectError(error)) {
        throw new Error(fallbackMessage);
      }

      const resolvedError = ensureError(error, fallbackMessage);
      handleAuthErrors({
        error,
        onError: () => {
          throw resolvedError;
        },
      });
      throw resolvedError;
    },
    [handleAuthErrors],
  );

  useEffect(() => {
    activeEventRef.current = activeEvent;
  }, [activeEvent]);

  const applyCurtailmentMutation = useCallback((event: CurtailmentEvent, updateHistory: HistoryEventUpdater) => {
    const nextActiveEvent = mapActiveEvent(event);
    const nextHistoryEvent = mapHistoryEvent(event);

    setActiveEvent(nextActiveEvent);
    setEvents((current) => updateHistory(current, nextHistoryEvent));

    return { event: nextActiveEvent };
  }, []);

  const refreshCurtailment = useCallback(
    async (stateFilters?: CurtailmentEventState[]): Promise<CurtailmentListState> => {
      setIsLoading(true);

      try {
        const [activeResponse, eventsResponse] = await Promise.all([
          curtailmentClient.getActiveCurtailment(create(GetActiveCurtailmentRequestSchema, {})),
          listAvailableCurtailmentEvents(stateFilters),
        ]);
        const historyEvents = await refreshHistoryEventsAfterActiveClears(
          activeResponse.event,
          eventsResponse,
          stateFilters,
        );
        const nextActiveEvent = getNextActiveEvent(activeResponse.event, activeEventRef.current, historyEvents);
        const nextState = {
          activeEvent: nextActiveEvent,
          events: historyEvents.map(mapHistoryEvent),
        };

        setActiveEvent(nextState.activeEvent);
        setEvents(nextState.events);
        return nextState;
      } catch (error) {
        if (isUnimplementedConnectError(error)) {
          throw Object.assign(new Error("Failed to load curtailment events."), { cause: error });
        }

        const resolvedError = ensureError(error, "Failed to load curtailment events.");
        handleAuthErrors({
          error,
          onError: () => {
            throw resolvedError;
          },
        });
        throw resolvedError;
      } finally {
        setIsLoading(false);
      }
    },
    [handleAuthErrors],
  );

  const startCurtailment = useCallback(
    async (request: StartCurtailmentRequest) => {
      try {
        const response = await curtailmentClient.startCurtailment(request);

        const responseEvent = requireResponseEvent(response.event, "Start");
        return applyCurtailmentMutation(responseEvent, prependHistoryEvent);
      } catch (error) {
        return throwCurtailmentApiError(error, "Failed to start curtailment.");
      }
    },
    [applyCurtailmentMutation, throwCurtailmentApiError],
  );

  const stopCurtailment = useCallback(
    async (eventId: string) => {
      try {
        const response = await curtailmentClient.stopCurtailment(
          create(StopCurtailmentRequestSchema, { eventUuid: eventId }),
        );

        const responseEvent = requireResponseEvent(response.event, "Stop");
        return applyCurtailmentMutation(responseEvent, replaceHistoryEvent);
      } catch (error) {
        return throwCurtailmentApiError(error, "Failed to stop curtailment.");
      }
    },
    [applyCurtailmentMutation, throwCurtailmentApiError],
  );

  const updateCurtailmentEvent = useCallback(
    async (request: UpdateCurtailmentEventRequest) => {
      try {
        const response = await curtailmentClient.updateCurtailmentEvent(request);

        const responseEvent = requireResponseEvent(response.event, "Update");
        return applyCurtailmentMutation(responseEvent, replaceHistoryEvent);
      } catch (error) {
        return throwCurtailmentApiError(error, "Failed to update curtailment.");
      }
    },
    [applyCurtailmentMutation, throwCurtailmentApiError],
  );

  return useMemo(
    () => ({
      activeEvent,
      events,
      isLoading,
      refreshCurtailment,
      startCurtailment,
      stopCurtailment,
      updateCurtailmentEvent,
    }),
    [activeEvent, events, isLoading, refreshCurtailment, startCurtailment, stopCurtailment, updateCurtailmentEvent],
  );
}

export default useCurtailmentApi;
