import { useCallback, useMemo, useRef, useState } from "react";
import { create } from "@bufbuild/protobuf";
import type { Timestamp } from "@bufbuild/protobuf/wkt";

import { curtailmentClient } from "@/protoFleet/api/clients";
import { emitCurtailmentChanged } from "@/protoFleet/api/curtailmentEvents";
import {
  GetActiveCurtailmentRequestSchema,
  ListCurtailmentEventsRequestSchema,
  type CurtailmentEvent as ProtoCurtailmentEvent,
  CurtailmentEventState as ProtoCurtailmentEventState,
  CurtailmentPriority as ProtoCurtailmentPriority,
  CurtailmentTargetState as ProtoCurtailmentTargetState,
  StopCurtailmentRequestSchema,
} from "@/protoFleet/api/generated/curtailment/v1/curtailment_pb";
import { assertNotAborted, isAbortError, toError } from "@/protoFleet/api/requestErrors";
import type {
  ActiveCurtailmentEvent,
  CurtailmentTargetRollup,
} from "@/protoFleet/features/energy/ActiveCurtailmentStatus";
import {
  type CurtailmentEventState,
  getCurtailmentEventEstimatedReductionKw,
  getCurtailmentEventScopeLabel,
  getCurtailmentEventSelectedMinerCount,
  isActiveCurtailmentEventState,
  mapCurtailmentEventState,
} from "@/protoFleet/features/energy/curtailmentDisplayUtils";
import type { CurtailmentHistoryEvent, CurtailmentPriority } from "@/protoFleet/features/energy/CurtailmentHistory";
import {
  buildStartCurtailmentRequest,
  buildUpdateCurtailmentEventRequest,
} from "@/protoFleet/features/energy/curtailmentRequestBuilders";
import type { CurtailmentSubmitValues } from "@/protoFleet/features/energy/CurtailmentStartModal";
import { useAuthErrors } from "@/protoFleet/store";

export interface RefreshCurtailmentOptions {
  background?: boolean;
  historyPage?: number;
  signal?: AbortSignal;
}

interface CurtailmentSnapshot {
  activeEvent: ActiveCurtailmentEvent | null;
  activeEventId: string | null;
  activeEventFormValues: CurtailmentSubmitValues | null;
  historyEvents: CurtailmentHistoryEvent[];
}

interface ObservedPowerSummary {
  observedReductionKw: number;
  remainingPowerKw?: number;
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
  refreshCurtailment: (options?: RefreshCurtailmentOptions) => Promise<CurtailmentSnapshot>;
  goToHistoryPage: (
    historyPage: number,
    options?: Pick<RefreshCurtailmentOptions, "signal">,
  ) => Promise<CurtailmentSnapshot>;
  setHistoryStatusFilter: (
    stateFilter?: CurtailmentEventState,
    options?: Pick<RefreshCurtailmentOptions, "signal">,
  ) => Promise<CurtailmentSnapshot>;
  startCurtailment: (values: CurtailmentSubmitValues) => Promise<ProtoCurtailmentEvent>;
  updateCurtailment: (
    eventUuid: string,
    values: CurtailmentSubmitValues,
    initialValues?: Partial<CurtailmentSubmitValues>,
  ) => Promise<ProtoCurtailmentEvent>;
  stopCurtailment: (eventUuid: string) => Promise<ProtoCurtailmentEvent>;
}

const wattsPerKilowatt = 1000;
const curtailmentHistoryPageSize = 50;
const initialHistoryPagination: CurtailmentHistoryPaginationState = {
  currentPage: 0,
  nextPageToken: "",
  pageTokens: [undefined],
};

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

function getFixedKwTolerance(event: ProtoCurtailmentEvent): number | undefined {
  return event.modeParams.case === "fixedKw" ? event.modeParams.value.toleranceKw : undefined;
}

function formatNumberField(value: number | undefined): string {
  return value && value > 0 ? String(value) : "";
}

function mapCurtailmentEventScopeToFormValues(
  event: ProtoCurtailmentEvent,
): Pick<CurtailmentSubmitValues, "scopeType" | "scopeId" | "deviceSetIds" | "deviceIdentifiers"> {
  switch (event.scope.case) {
    case "deviceIdentifiers":
      return {
        scopeType: "explicitMiners",
        scopeId: "explicit-miners",
        deviceSetIds: [],
        deviceIdentifiers: [...event.scope.value.deviceIdentifiers],
      };
    case "deviceSetIds":
      return {
        scopeType: "deviceSet",
        scopeId: "device-sets",
        deviceSetIds: [...event.scope.value.deviceSetIds],
        deviceIdentifiers: [],
      };
    case "wholeOrg":
    default:
      return {
        scopeType: "wholeOrg",
        scopeId: "whole-org",
        deviceSetIds: [],
        deviceIdentifiers: [],
      };
  }
}

function mapCurtailmentEventToFormValues(event: ProtoCurtailmentEvent): CurtailmentSubmitValues {
  const fixedKwTarget = getFixedKwTarget(event);
  const fixedKwTolerance = getFixedKwTolerance(event);

  return {
    ...mapCurtailmentEventScopeToFormValues(event),
    responseProfileId: "customPlan",
    curtailmentMode: "fixedKwReduction",
    minerSelectionStrategy: "leastEfficientFirst",
    targetKw: fixedKwTarget !== undefined ? String(fixedKwTarget) : "",
    toleranceKw: fixedKwTolerance !== undefined ? String(fixedKwTolerance) : "",
    priority: event.priority === ProtoCurtailmentPriority.EMERGENCY ? "emergency" : "normal",
    minDurationSec: formatNumberField(event.minCurtailedDurationSec),
    maxDurationSec: formatNumberField(event.maxDurationSeconds),
    restoreBatchSize: formatNumberField(event.restoreBatchSize),
    restoreIntervalSec: formatNumberField(event.restoreBatchIntervalSec),
    reason: event.reason || "Curtailment",
    includeMaintenance: event.includeMaintenance,
  };
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
  includeActiveInHistory = true,
): CurtailmentSnapshot {
  const nextActiveEvent = getActiveSnapshotEvent(activeEvent);
  const nextHistoryEvents = historyEvents.map(mapCurtailmentHistoryEvent);

  if (includeActiveInHistory && activeEvent && !nextHistoryEvents.some((event) => event.id === activeEvent.eventUuid)) {
    nextHistoryEvents.unshift(mapCurtailmentHistoryEvent(activeEvent));
  }

  return {
    activeEvent: nextActiveEvent,
    activeEventId: activeEvent && nextActiveEvent ? activeEvent.eventUuid : null,
    activeEventFormValues: activeEvent && nextActiveEvent ? mapCurtailmentEventToFormValues(activeEvent) : null,
    historyEvents: nextHistoryEvents,
  };
}

function shouldIncludeActiveEventInHistory(
  activeEvent: ProtoCurtailmentEvent | undefined,
  stateFilter: CurtailmentEventState | undefined,
): boolean {
  return !stateFilter || Boolean(activeEvent && mapCurtailmentEventState(activeEvent.state) === stateFilter);
}

function upsertHistoryEvent(
  events: CurtailmentHistoryEvent[],
  event: ProtoCurtailmentEvent,
): CurtailmentHistoryEvent[] {
  const mappedEvent = mapCurtailmentHistoryEvent(event);
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
  const [snapshot, setSnapshot] = useState<CurtailmentSnapshot>({
    activeEvent: null,
    activeEventId: null,
    activeEventFormValues: null,
    historyEvents: [],
  });
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
  const [historyStatusFilter, setHistoryStatusFilterState] = useState<CurtailmentEventState | undefined>();
  const snapshotRef = useRef(snapshot);
  const historyPaginationRef = useRef(historyPagination);
  const historyStatusFilterRef = useRef(historyStatusFilter);
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

  const updateHistoryStatusFilter = useCallback((nextStatusFilter: CurtailmentEventState | undefined) => {
    historyStatusFilterRef.current = nextStatusFilter;
    setHistoryStatusFilterState(nextStatusFilter);
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
      const nextActiveEvent = isActiveCurtailmentEventState(state) ? mapActiveCurtailmentEvent(event) : null;
      const activeStatusFilter = historyStatusFilterRef.current;
      const shouldUpdateHistoryPage =
        historyPaginationRef.current.currentPage === 0 && (!activeStatusFilter || activeStatusFilter === state);

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
      stateFilter: CurtailmentEventState | undefined,
      signal?: AbortSignal,
    ): Promise<CurtailmentHistoryPage> => {
      assertNotAborted(signal);

      const response = await curtailmentClient.listCurtailmentEvents(
        create(ListCurtailmentEventsRequestSchema, {
          pageSize: curtailmentHistoryPageSize,
          pageToken,
          stateFilter: mapHistoryStateFilter(stateFilter),
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
    (signal?: AbortSignal, requestedHistoryPage = historyPaginationRef.current.currentPage) => {
      const historyPage = getNormalizedHistoryPage(requestedHistoryPage);
      const currentPagination = historyPaginationRef.current;
      const stateFilter = historyStatusFilterRef.current;
      const pageToken = historyPage === 0 ? "" : currentPagination.pageTokens[historyPage];

      if (historyPage > 0 && pageToken === undefined) {
        return Promise.resolve(snapshotRef.current);
      }

      const requestId = ++latestRefreshRequestIdRef.current;
      const knownPageTokens = currentPagination.pageTokens.slice(0, historyPage + 1);

      return (async () => {
        try {
          const [activeResponse, historyPageResponse] = await Promise.all([
            curtailmentClient.getActiveCurtailment(
              create(GetActiveCurtailmentRequestSchema, {}),
              signal ? { signal } : undefined,
            ),
            listCurtailmentEventsPage(pageToken ?? "", knownPageTokens, stateFilter, signal),
          ]);
          assertNotAborted(signal);

          const nextSnapshot = createSnapshot(
            activeResponse.event,
            historyPageResponse.events,
            historyPage === 0 && shouldIncludeActiveEventInHistory(activeResponse.event, stateFilter),
          );
          if (requestId !== latestRefreshRequestIdRef.current) {
            return nextSnapshot;
          }

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
    async ({ background = false, historyPage, signal }: RefreshCurtailmentOptions = {}) => {
      if (background) {
        return runRefresh(signal, historyPage);
      }

      foregroundRefreshCountRef.current += 1;
      setIsLoading(true);

      try {
        return await runRefresh(signal, historyPage);
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

  const setHistoryStatusFilter = useCallback(
    (stateFilter?: CurtailmentEventState, options: Pick<RefreshCurtailmentOptions, "signal"> = {}) => {
      updateHistoryStatusFilter(stateFilter);
      updateHistoryPagination(initialHistoryPagination);
      return refreshCurtailment({ historyPage: 0, signal: options.signal });
    },
    [refreshCurtailment, updateHistoryPagination, updateHistoryStatusFilter],
  );

  const refreshAfterMutation = useCallback(async () => {
    emitCurtailmentChanged();

    try {
      await refreshCurtailment({ background: true, historyPage: 0 });
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

  return useMemo(
    () => ({
      ...snapshot,
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
      refreshCurtailment,
      goToHistoryPage,
      setHistoryStatusFilter,
      startCurtailment,
      updateCurtailment,
      stopCurtailment,
    }),
    [
      goToHistoryPage,
      historyPagination.currentPage,
      historyPagination.nextPageToken,
      historyStatusFilter,
      isLoading,
      isStarting,
      updatingEventId,
      loadError,
      refreshCurtailment,
      setHistoryStatusFilter,
      snapshot,
      startCurtailment,
      updateCurtailment,
      stopCurtailment,
      stopError,
      stoppingEventId,
      startError,
      updateError,
    ],
  );
}
