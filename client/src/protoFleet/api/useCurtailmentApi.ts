import { useCallback, useMemo, useRef, useState } from "react";
import { create } from "@bufbuild/protobuf";
import type { Timestamp } from "@bufbuild/protobuf/wkt";

import { curtailmentClient } from "@/protoFleet/api/clients";
import { emitCurtailmentChanged } from "@/protoFleet/api/curtailmentEvents";
import {
  GetActiveCurtailmentRequestSchema,
  ListCurtailmentEventsRequestSchema,
  type CurtailmentEvent as ProtoCurtailmentEvent,
  CurtailmentPriority as ProtoCurtailmentPriority,
  CurtailmentTargetState as ProtoCurtailmentTargetState,
  StopCurtailmentRequestSchema,
} from "@/protoFleet/api/generated/curtailment/v1/curtailment_pb";
import { getErrorMessage } from "@/protoFleet/api/getErrorMessage";
import type {
  ActiveCurtailmentEvent,
  CurtailmentTargetRollup,
} from "@/protoFleet/features/energy/ActiveCurtailmentStatus";
import {
  getCurtailmentEventEstimatedReductionKw,
  getCurtailmentEventScopeLabel,
  getCurtailmentEventSelectedMinerCount,
  isActiveCurtailmentEventState,
  mapCurtailmentEventState,
} from "@/protoFleet/features/energy/curtailmentDisplayUtils";
import type { CurtailmentHistoryEvent, CurtailmentPriority } from "@/protoFleet/features/energy/CurtailmentHistory";
import { buildStartCurtailmentRequest } from "@/protoFleet/features/energy/curtailmentRequestBuilders";
import type { CurtailmentSubmitValues } from "@/protoFleet/features/energy/CurtailmentStartModal";
import { useAuthErrors } from "@/protoFleet/store";

export interface RefreshCurtailmentOptions {
  background?: boolean;
}

interface CurtailmentSnapshot {
  activeEvent: ActiveCurtailmentEvent | null;
  activeEventId: string | null;
  historyEvents: CurtailmentHistoryEvent[];
}

interface ObservedPowerSummary {
  observedReductionKw: number;
  remainingPowerKw?: number;
}

export interface UseCurtailmentApiResult extends CurtailmentSnapshot {
  isLoading: boolean;
  isStarting: boolean;
  stoppingEventId: string | null;
  loadError: string | null;
  startError: string | null;
  stopError: string | null;
  refreshCurtailment: (options?: RefreshCurtailmentOptions) => Promise<CurtailmentSnapshot>;
  startCurtailment: (values: CurtailmentSubmitValues) => Promise<ProtoCurtailmentEvent>;
  stopCurtailment: (eventUuid: string) => Promise<ProtoCurtailmentEvent>;
}

const historyPageSize = 200;
const wattsPerKilowatt = 1000;

function toError(error: unknown, fallbackMessage: string): Error {
  const message = getErrorMessage(error);
  if (message) {
    return new Error(message);
  }

  return error instanceof Error ? error : new Error(fallbackMessage);
}

function timestampToIsoString(timestamp?: Timestamp): string | undefined {
  if (!timestamp) {
    return undefined;
  }

  const date = new Date(Number(timestamp.seconds) * 1000 + Math.floor(timestamp.nanos / 1_000_000));
  return Number.isNaN(date.getTime()) ? undefined : date.toISOString();
}

function getFixedKwTarget(event: ProtoCurtailmentEvent): number | undefined {
  return event.modeParams.case === "fixedKw" ? event.modeParams.value.targetKw : undefined;
}

function mapCurtailmentPriority(priority: ProtoCurtailmentPriority): CurtailmentPriority {
  switch (priority) {
    case ProtoCurtailmentPriority.EMERGENCY:
      return "emergency";
    case ProtoCurtailmentPriority.HIGH:
      return "high";
    case ProtoCurtailmentPriority.NORMAL:
    case ProtoCurtailmentPriority.UNSPECIFIED:
    default:
      return "normal";
  }
}

function mapCurtailmentTargetState(state: ProtoCurtailmentTargetState): CurtailmentTargetRollup["state"] {
  switch (state) {
    case ProtoCurtailmentTargetState.DISPATCHING:
    case ProtoCurtailmentTargetState.DISPATCHED:
      return "dispatched";
    case ProtoCurtailmentTargetState.CONFIRMED:
      return "confirmed";
    case ProtoCurtailmentTargetState.DRIFTED:
      return "drifted";
    case ProtoCurtailmentTargetState.RESOLVED:
      return "resolved";
    case ProtoCurtailmentTargetState.RELEASED:
      return "released";
    case ProtoCurtailmentTargetState.RESTORE_FAILED:
      return "restoreFailed";
    case ProtoCurtailmentTargetState.PENDING:
    case ProtoCurtailmentTargetState.UNSPECIFIED:
    default:
      return "pending";
  }
}

function getSourceLabel(event: ProtoCurtailmentEvent): string {
  return event.externalSource.trim() || "Manual";
}

function getRollupsFromTargets(event: ProtoCurtailmentEvent): CurtailmentTargetRollup[] {
  const counts = new Map<CurtailmentTargetRollup["state"], number>();

  for (const target of event.targets) {
    const state = mapCurtailmentTargetState(target.state);
    counts.set(state, (counts.get(state) ?? 0) + 1);
  }

  return Array.from(counts, ([state, count]) => ({ state, count }));
}

function getRollups(event: ProtoCurtailmentEvent): CurtailmentTargetRollup[] {
  const rollup = event.targetRollup;
  if (!rollup) {
    return getRollupsFromTargets(event);
  }

  const rollups: CurtailmentTargetRollup[] = [
    { state: "pending", count: rollup.pending },
    { state: "dispatched", count: rollup.dispatched },
    { state: "confirmed", count: rollup.confirmed },
    { state: "drifted", count: rollup.drifted },
    { state: "resolved", count: rollup.resolved },
    { state: "released", count: rollup.released },
    { state: "restoreFailed", count: rollup.restoreFailed },
  ];

  return rollups.filter((targetRollup) => targetRollup.count > 0);
}

function getObservedPowerSummary(event: ProtoCurtailmentEvent, estimatedReductionKw: number): ObservedPowerSummary {
  let observedPowerTotalW = 0;
  let observedReductionTotalW = 0;
  let hasObservedPower = false;
  let hasObservedReduction = false;

  for (const { baselinePowerW, observedPowerW } of event.targets) {
    if (observedPowerW !== undefined) {
      hasObservedPower = true;
      observedPowerTotalW += observedPowerW;
    }

    if (baselinePowerW !== undefined && observedPowerW !== undefined) {
      hasObservedReduction = true;
      observedReductionTotalW += Math.max(baselinePowerW - observedPowerW, 0);
    }
  }

  return {
    observedReductionKw: hasObservedReduction ? observedReductionTotalW / wattsPerKilowatt : estimatedReductionKw,
    remainingPowerKw: hasObservedPower ? observedPowerTotalW / wattsPerKilowatt : undefined,
  };
}

export function mapActiveCurtailmentEvent(event: ProtoCurtailmentEvent): ActiveCurtailmentEvent {
  const estimatedReductionKw = getCurtailmentEventEstimatedReductionKw(event);
  const observedPowerSummary = getObservedPowerSummary(event, estimatedReductionKw);

  return {
    reason: event.reason || "Curtailment",
    state: mapCurtailmentEventState(event.state),
    scopeLabel: getCurtailmentEventScopeLabel(event),
    endedAt: timestampToIsoString(event.endedAt),
    selectedMiners: getCurtailmentEventSelectedMinerCount(event),
    estimatedReductionKw,
    targetKw: getFixedKwTarget(event),
    observedReductionKw: observedPowerSummary.observedReductionKw,
    remainingPowerKw: observedPowerSummary.remainingPowerKw,
    restoreBatchSize: event.effectiveBatchSize || event.restoreBatchSize,
    restoreBatchIntervalSec: event.restoreBatchIntervalSec,
    rollups: getRollups(event),
  };
}

export function mapCurtailmentHistoryEvent(event: ProtoCurtailmentEvent): CurtailmentHistoryEvent {
  return {
    id: event.eventUuid,
    reason: event.reason || "Curtailment",
    state: mapCurtailmentEventState(event.state),
    priority: mapCurtailmentPriority(event.priority),
    scopeLabel: getCurtailmentEventScopeLabel(event),
    selectedMiners: getCurtailmentEventSelectedMinerCount(event),
    estimatedReductionKw: getCurtailmentEventEstimatedReductionKw(event),
    targetKw: getFixedKwTarget(event),
    sourceLabel: getSourceLabel(event),
    startedAt: timestampToIsoString(event.startedAt),
    endedAt: timestampToIsoString(event.endedAt),
    scheduledAt: timestampToIsoString(event.scheduledStartAt),
    createdAt: timestampToIsoString(event.createdAt),
  };
}

function getActiveSnapshotEvent(activeEvent: ProtoCurtailmentEvent | undefined): ActiveCurtailmentEvent | null {
  if (!activeEvent) {
    return null;
  }

  const activeState = mapCurtailmentEventState(activeEvent.state);
  if (!isActiveCurtailmentEventState(activeState)) {
    return null;
  }

  return mapActiveCurtailmentEvent(activeEvent);
}

function createSnapshot(
  activeEvent: ProtoCurtailmentEvent | undefined,
  historyEvents: ProtoCurtailmentEvent[],
): CurtailmentSnapshot {
  const nextActiveEvent = getActiveSnapshotEvent(activeEvent);
  const nextHistoryEvents = historyEvents.map(mapCurtailmentHistoryEvent);

  if (activeEvent && !nextHistoryEvents.some((event) => event.id === activeEvent.eventUuid)) {
    nextHistoryEvents.unshift(mapCurtailmentHistoryEvent(activeEvent));
  }

  return {
    activeEvent: nextActiveEvent,
    activeEventId: activeEvent && nextActiveEvent ? activeEvent.eventUuid : null,
    historyEvents: nextHistoryEvents,
  };
}

function upsertHistoryEvent(
  events: CurtailmentHistoryEvent[],
  event: ProtoCurtailmentEvent,
): CurtailmentHistoryEvent[] {
  const mappedEvent = mapCurtailmentHistoryEvent(event);
  return [mappedEvent, ...events.filter((currentEvent) => currentEvent.id !== mappedEvent.id)];
}

export function useCurtailmentApi(): UseCurtailmentApiResult {
  const { handleAuthErrors } = useAuthErrors();
  const [snapshot, setSnapshot] = useState<CurtailmentSnapshot>({
    activeEvent: null,
    activeEventId: null,
    historyEvents: [],
  });
  const [isLoading, setIsLoading] = useState(false);
  const [isStarting, setIsStarting] = useState(false);
  const [stoppingEventId, setStoppingEventId] = useState<string | null>(null);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [startError, setStartError] = useState<string | null>(null);
  const [stopError, setStopError] = useState<string | null>(null);
  const inFlightRefreshRef = useRef<Promise<CurtailmentSnapshot> | null>(null);
  const foregroundRefreshCountRef = useRef(0);

  const handleFailure = useCallback(
    (error: unknown, fallbackMessage: string) => {
      const resolvedError = toError(error, fallbackMessage);
      handleAuthErrors({ error });
      return resolvedError;
    },
    [handleAuthErrors],
  );

  const applyEvent = useCallback((event: ProtoCurtailmentEvent) => {
    const state = mapCurtailmentEventState(event.state);
    const nextActiveEvent = isActiveCurtailmentEventState(state) ? mapActiveCurtailmentEvent(event) : null;

    setSnapshot((current) => ({
      activeEvent: nextActiveEvent,
      activeEventId: nextActiveEvent ? event.eventUuid : null,
      historyEvents: upsertHistoryEvent(current.historyEvents, event),
    }));
  }, []);

  const listCurtailmentEvents = useCallback(async (): Promise<ProtoCurtailmentEvent[]> => {
    const events: ProtoCurtailmentEvent[] = [];
    let pageToken = "";

    do {
      const response = await curtailmentClient.listCurtailmentEvents(
        create(ListCurtailmentEventsRequestSchema, {
          pageSize: historyPageSize,
          pageToken,
        }),
      );
      events.push(...response.events);
      pageToken = response.nextPageToken;
    } while (pageToken);

    return events;
  }, []);

  const runRefresh = useCallback(() => {
    if (inFlightRefreshRef.current) {
      return inFlightRefreshRef.current;
    }

    const refreshPromise = (async () => {
      try {
        const [activeResponse, historyEvents] = await Promise.all([
          curtailmentClient.getActiveCurtailment(create(GetActiveCurtailmentRequestSchema, {})),
          listCurtailmentEvents(),
        ]);
        const nextSnapshot = createSnapshot(activeResponse.event, historyEvents);
        setSnapshot(nextSnapshot);
        setLoadError(null);
        return nextSnapshot;
      } catch (error) {
        const resolvedError = handleFailure(error, "Failed to load curtailment data.");
        setLoadError(resolvedError.message);
        throw resolvedError;
      }
    })();

    inFlightRefreshRef.current = refreshPromise;
    const clearInFlightRefresh = () => {
      if (inFlightRefreshRef.current === refreshPromise) {
        inFlightRefreshRef.current = null;
      }
    };

    void refreshPromise.then(clearInFlightRefresh, clearInFlightRefresh);

    return refreshPromise;
  }, [handleFailure, listCurtailmentEvents]);

  const refreshCurtailment = useCallback(
    async ({ background = false }: RefreshCurtailmentOptions = {}) => {
      if (background) {
        return runRefresh();
      }

      foregroundRefreshCountRef.current += 1;
      setIsLoading(true);

      try {
        return await runRefresh();
      } finally {
        foregroundRefreshCountRef.current = Math.max(0, foregroundRefreshCountRef.current - 1);
        setIsLoading(foregroundRefreshCountRef.current > 0);
      }
    },
    [runRefresh],
  );

  const refreshAfterMutation = useCallback(async () => {
    emitCurtailmentChanged();

    try {
      await refreshCurtailment({ background: true });
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

  return useMemo(
    () => ({
      ...snapshot,
      isLoading,
      isStarting,
      stoppingEventId,
      loadError,
      startError,
      stopError,
      refreshCurtailment,
      startCurtailment,
      stopCurtailment,
    }),
    [
      isLoading,
      isStarting,
      loadError,
      refreshCurtailment,
      snapshot,
      startCurtailment,
      stopCurtailment,
      stopError,
      stoppingEventId,
      startError,
    ],
  );
}
