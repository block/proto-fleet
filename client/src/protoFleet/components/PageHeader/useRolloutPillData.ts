import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useNavigate } from "react-router-dom";
import { create } from "@bufbuild/protobuf";
import { Code, ConnectError } from "@connectrpc/connect";

import { rolloutClient } from "@/protoFleet/api/clients";
import {
  GetRolloutLaneForRolloutRequestSchema,
  ListRolloutGroupsRequestSchema,
  ListRolloutLanesRequestSchema,
  ListRolloutsRequestSchema,
  RolloutState,
} from "@/protoFleet/api/generated/rollout/v1/rollout_pb";
import { isAbortError, isAuthOrPermissionError } from "@/protoFleet/api/requestErrors";
import { ROLLOUT_CHANGED_EVENT } from "@/protoFleet/api/rolloutEvents";
import { mapRollout, mapRolloutGroup, mapRolloutLane, mapRolloutToEvent } from "@/protoFleet/api/rolloutMappers";
import {
  BETWEEN_CHANNEL_STRATEGY_KEY,
  compareRolloutChildren,
  firstActiveFirmwareConvergenceLane,
  laneForRollout,
  rolloutChildRank,
  shouldMonitorRollout,
} from "@/protoFleet/features/rollout/betweenChannel/betweenChannelUtils";
import { mapFirmwareTransitionToRolloutEvent } from "@/protoFleet/features/rollout/firmwareTransitionRolloutEvent";
import {
  isRolloutGroupResultAcknowledged,
  latestCompletedRolloutResult,
  rolloutGroupAcknowledgement,
  useAcknowledgedRolloutGroupResult,
  useAcknowledgedRolloutResultId,
} from "@/protoFleet/features/rollout/rolloutResultAcknowledgement";
import type {
  RolloutEvent,
  RolloutGroup,
  RolloutLane,
  RolloutRecord,
} from "@/protoFleet/features/rollout/rolloutTypes";
import { useAuthErrors, useHasPermission } from "@/protoFleet/store";

export interface UseRolloutPillDataOptions {
  enabled?: boolean;
}

export interface UseRolloutPillDataResult {
  activeEvent: RolloutEvent | null;
  detailsPath: string | null;
  hasVisiblePill: boolean;
  onViewRollout: (() => void) | null;
}

interface RolloutPillSelection {
  event: RolloutEvent;
  detailsPath: string;
  fingerprint: string;
  parent?: RolloutGroup;
  rolloutId?: string;
  source: "completedRollout" | "convergence" | "group" | "rollout";
}

const idlePollIntervalMs = 30_000;
const activePollIntervalMs = 5_000;
const exactLaneRevalidationIntervalMs = idlePollIntervalMs;
const rolloutLanesPath = "/settings/firmware?tab=rolloutLanes";
const pillRolloutStates = [
  RolloutState.CREATED,
  RolloutState.RUNNING,
  RolloutState.PAUSED,
  RolloutState.REVIEW,
  RolloutState.ABORTED,
  RolloutState.REVERTING,
  RolloutState.COMPLETED,
  RolloutState.COMPLETED_WITH_FAILURES,
  RolloutState.REVERTED,
];

function activeFirmwareRollout(rollouts: RolloutRecord[]): RolloutRecord | undefined {
  return rollouts.find(
    (rollout) => rollout.strategyKey === BETWEEN_CHANNEL_STRATEGY_KEY && shouldMonitorRollout(rollout),
  );
}

function selection(
  event: RolloutEvent,
  detailsPath: string,
  source: RolloutPillSelection["source"],
  rolloutId?: string,
): RolloutPillSelection {
  return {
    event,
    detailsPath,
    fingerprint: JSON.stringify({ detailsPath, event, rolloutId, source }),
    source,
    rolloutId,
  };
}

function selectPill(rollouts: RolloutRecord[], lanes: RolloutLane[]): RolloutPillSelection | null {
  const rollout = activeFirmwareRollout(rollouts);
  if (rollout) {
    const lane = laneForRollout(lanes, rollout.id);
    return selection(
      mapRolloutToEvent(rollout, {
        laneLabel: lane?.label,
      }),
      rolloutLanesPath,
      "rollout",
      rollout.id,
    );
  }

  const lane = firstActiveFirmwareConvergenceLane(lanes);
  if (lane) {
    return selection(
      mapFirmwareTransitionToRolloutEvent(lane.firmwareConvergence, {
        scopeLabel: lane.label,
        startedAt: lane.createdAt,
      }),
      `${rolloutLanesPath}&setupLane=${encodeURIComponent(lane.id)}`,
      "convergence",
    );
  }

  const completedRollout = latestCompletedRolloutResult(
    rollouts.filter((candidate) => candidate.strategyKey === BETWEEN_CHANNEL_STRATEGY_KEY),
  );
  if (!completedRollout) {
    return null;
  }
  const completedLane = laneForRollout(lanes, completedRollout.id);
  return selection(
    mapRolloutToEvent(completedRollout, {
      laneLabel: completedLane?.label,
    }),
    `${rolloutLanesPath}&rollout=${encodeURIComponent(completedRollout.id)}`,
    "completedRollout",
    completedRollout.id,
  );
}

function selectGroupPill(groups: RolloutGroup[], lanes: RolloutLane[]): RolloutPillSelection | null {
  const parent = groups.find((group) => group.lifecycle === "active") ?? groups.find((group) => group.resultReady);
  if (!parent || parent.children.length === 0) {
    return null;
  }
  const child = [...parent.children].sort(compareRolloutChildren)[0];
  const lane = lanes.find((candidate) => candidate.id === parent.laneId);
  const event = mapRolloutToEvent(child, { laneLabel: lane?.label });
  const actionCount = parent.children.filter((candidate) => rolloutChildRank(candidate) === 0).length;
  const aggregateEvent: RolloutEvent = {
    ...event,
    title: parent.name,
    scopeLabel: `${parent.children.length.toLocaleString()} model${
      parent.children.length === 1 ? "" : "s"
    }${actionCount > 0 ? ` · ${actionCount.toLocaleString()} need attention` : ""}`,
    totalTargets: parent.children.reduce((total, candidate) => total + candidate.members.length, 0),
    excludedTargets: 0,
  };
  return {
    event: aggregateEvent,
    detailsPath: `${rolloutLanesPath}&rolloutParent=${encodeURIComponent(parent.id)}&rolloutChild=${encodeURIComponent(
      child.id,
    )}`,
    fingerprint: JSON.stringify({
      activity: parent.activity,
      childId: child.id,
      parentId: parent.id,
      resultRevision: parent.resultRevision.toString(),
    }),
    parent,
    rolloutId: child.id,
    source: "group",
  };
}

export function useRolloutPillData({ enabled = true }: UseRolloutPillDataOptions = {}): UseRolloutPillDataResult {
  const navigate = useNavigate();
  const { handleAuthErrors } = useAuthErrors();
  const canReadRollouts = useHasPermission("rollout:read");
  const canReadChannels = useHasPermission("channel:read");
  const [acknowledgedResultId, setAcknowledgedResultId] = useAcknowledgedRolloutResultId();
  const [acknowledgedGroupResult, setAcknowledgedGroupResult] = useAcknowledgedRolloutGroupResult();
  const [selection, setSelection] = useState<RolloutPillSelection | null>(null);
  const rolloutsRef = useRef<RolloutRecord[]>([]);
  const groupsRef = useRef<RolloutGroup[]>([]);
  const lanesRef = useRef<RolloutLane[]>([]);
  const missingLaneRolloutIdRef = useRef<string | null>(null);
  const exactLaneValidationRef = useRef<{ rolloutId: string; validatedAt: number } | null>(null);
  const inFlightRefreshRef = useRef<Promise<void> | null>(null);
  const pendingFreshRefreshRef = useRef(false);
  const visibleSelection =
    enabled &&
    ((selection?.source !== "convergence" && canReadRollouts) ||
      (selection?.source === "convergence" && canReadChannels)) &&
    !(selection?.source === "completedRollout" && selection.rolloutId === acknowledgedResultId) &&
    !(
      selection?.source === "group" &&
      selection.parent &&
      isRolloutGroupResultAcknowledged(selection.parent, acknowledgedGroupResult)
    )
      ? selection
      : null;
  const pollIntervalMs =
    visibleSelection && visibleSelection.source !== "completedRollout" ? activePollIntervalMs : idlePollIntervalMs;

  const refresh = useCallback(
    (signal: AbortSignal, forceFresh = false): Promise<void> => {
      if (signal.aborted || !enabled || (!canReadRollouts && !canReadChannels)) {
        return Promise.resolve();
      }
      if (inFlightRefreshRef.current) {
        if (!forceFresh) {
          return inFlightRefreshRef.current;
        }
        pendingFreshRefreshRef.current = true;
        return inFlightRefreshRef.current.then(() => {
          if (!pendingFreshRefreshRef.current || signal.aborted) {
            return;
          }
          pendingFreshRefreshRef.current = false;
          return refresh(signal, true);
        });
      }

      pendingFreshRefreshRef.current = false;
      let refreshPromise: Promise<void>;
      refreshPromise = (async () => {
        let nextRollouts = canReadRollouts ? rolloutsRef.current : [];
        let nextGroups = canReadRollouts ? groupsRef.current : [];
        let nextLanes = canReadChannels ? lanesRef.current : [];
        let listedLanes: RolloutLane[] | null = null;
        let exactLaneLookupAllowed = canReadChannels;
        const canListGroups = canReadRollouts && typeof rolloutClient.listRolloutGroups === "function";
        const [rolloutsResult, groupsResult, lanesResult] = await Promise.allSettled([
          canReadRollouts && !canListGroups
            ? rolloutClient.listRollouts(create(ListRolloutsRequestSchema, { states: pillRolloutStates }), { signal })
            : Promise.resolve(null),
          canListGroups
            ? rolloutClient.listRolloutGroups(create(ListRolloutGroupsRequestSchema), { signal })
            : Promise.resolve(null),
          canReadChannels
            ? rolloutClient.listRolloutLanes(
                create(ListRolloutLanesRequestSchema, { activeFirmwareConvergenceOnly: true }),
                { signal },
              )
            : Promise.resolve(null),
        ]);
        if (signal.aborted) {
          return;
        }

        if (rolloutsResult.status === "fulfilled") {
          if (rolloutsResult.value) {
            nextRollouts = rolloutsResult.value.rollouts.map(mapRollout);
          }
        } else if (!isAbortError(rolloutsResult.reason, signal)) {
          if (isAuthOrPermissionError(rolloutsResult.reason)) {
            nextRollouts = [];
          }
          handleAuthErrors({ error: rolloutsResult.reason });
        }

        if (groupsResult.status === "fulfilled") {
          if (groupsResult.value) {
            nextGroups = groupsResult.value.parents.map(mapRolloutGroup);
            nextRollouts = (groupsResult.value.legacyHistory ?? []).map(mapRollout);
          }
        } else if (!isAbortError(groupsResult.reason, signal)) {
          if (isAuthOrPermissionError(groupsResult.reason)) {
            nextGroups = [];
          }
          handleAuthErrors({ error: groupsResult.reason });
        }

        if (lanesResult.status === "fulfilled") {
          if (lanesResult.value) {
            listedLanes = lanesResult.value.lanes.map((lane) => mapRolloutLane(lane));
            nextLanes = listedLanes;
          }
        } else if (!isAbortError(lanesResult.reason, signal)) {
          if (isAuthOrPermissionError(lanesResult.reason)) {
            nextLanes = [];
            exactLaneLookupAllowed = false;
          }
          handleAuthErrors({ error: lanesResult.reason });
        }

        let nextSelection = selectGroupPill(nextGroups, nextLanes) ?? selectPill(nextRollouts, nextLanes);
        const selectedRolloutId =
          nextSelection?.source === "rollout" || nextSelection?.source === "completedRollout"
            ? nextSelection.rolloutId
            : undefined;
        const cachedExactLane = selectedRolloutId ? laneForRollout(lanesRef.current, selectedRolloutId) : undefined;
        const listedExactLane =
          listedLanes && selectedRolloutId ? laneForRollout(listedLanes, selectedRolloutId) : undefined;
        if (listedExactLane && selectedRolloutId) {
          missingLaneRolloutIdRef.current = null;
          exactLaneValidationRef.current = { rolloutId: selectedRolloutId, validatedAt: Date.now() };
        }
        if (listedLanes && !forceFresh && selectedRolloutId && !listedExactLane) {
          if (cachedExactLane) {
            nextLanes = [...listedLanes, cachedExactLane];
            nextSelection = selectGroupPill(nextGroups, nextLanes) ?? selectPill(nextRollouts, nextLanes);
          }
        }

        const exactLaneValidation = exactLaneValidationRef.current;
        const cachedExactLaneNeedsRevalidation =
          cachedExactLane !== undefined &&
          (exactLaneValidation === null ||
            exactLaneValidation.rolloutId !== selectedRolloutId ||
            Date.now() - exactLaneValidation.validatedAt >= exactLaneRevalidationIntervalMs);
        const shouldLoadExactLane =
          exactLaneLookupAllowed &&
          selectedRolloutId &&
          !listedExactLane &&
          (forceFresh || !laneForRollout(nextLanes, selectedRolloutId) || cachedExactLaneNeedsRevalidation) &&
          (forceFresh || cachedExactLaneNeedsRevalidation || missingLaneRolloutIdRef.current !== selectedRolloutId);
        if (shouldLoadExactLane) {
          try {
            const exactLaneResult = await rolloutClient.getRolloutLaneForRollout(
              create(GetRolloutLaneForRolloutRequestSchema, { rolloutId: selectedRolloutId }),
              { signal },
            );
            if (signal.aborted) {
              return;
            }
            if (exactLaneResult.lane) {
              const exactLane = mapRolloutLane(exactLaneResult.lane);
              nextLanes = [...nextLanes.filter((lane) => lane.id !== exactLane.id), exactLane];
              nextSelection = selectGroupPill(nextGroups, nextLanes) ?? selectPill(nextRollouts, nextLanes);
              missingLaneRolloutIdRef.current = null;
              exactLaneValidationRef.current = { rolloutId: selectedRolloutId, validatedAt: Date.now() };
            }
          } catch (error) {
            if (signal.aborted || isAbortError(error, signal)) {
              return;
            }
            if (isAuthOrPermissionError(error)) {
              nextLanes = [];
              nextSelection = selectGroupPill(nextGroups, nextLanes) ?? selectPill(nextRollouts, nextLanes);
              exactLaneValidationRef.current = null;
            }
            if (error instanceof ConnectError && error.code === Code.NotFound) {
              if (cachedExactLane) {
                nextLanes = nextLanes.filter((lane) => lane.id !== cachedExactLane.id);
                nextSelection = selectGroupPill(nextGroups, nextLanes) ?? selectPill(nextRollouts, nextLanes);
              }
              missingLaneRolloutIdRef.current = selectedRolloutId;
              exactLaneValidationRef.current = null;
            } else {
              if (!isAuthOrPermissionError(error) && cachedExactLane) {
                nextLanes = [...nextLanes.filter((lane) => lane.id !== cachedExactLane.id), cachedExactLane];
                nextSelection = selectPill(nextRollouts, nextLanes);
                exactLaneValidationRef.current = { rolloutId: selectedRolloutId, validatedAt: Date.now() };
              }
              handleAuthErrors({ error });
            }
          }
        }
        if (signal.aborted) {
          return;
        }
        rolloutsRef.current = nextRollouts;
        groupsRef.current = nextGroups;
        lanesRef.current = nextLanes;
        setSelection((currentSelection) =>
          currentSelection?.fingerprint === nextSelection?.fingerprint ? currentSelection : nextSelection,
        );
      })().finally(() => {
        if (inFlightRefreshRef.current === refreshPromise) {
          inFlightRefreshRef.current = null;
        }
      });
      inFlightRefreshRef.current = refreshPromise;
      return refreshPromise;
    },
    [canReadChannels, canReadRollouts, enabled, handleAuthErrors],
  );

  const onViewRollout = useMemo(() => {
    if (!canReadChannels || !visibleSelection) {
      return null;
    }
    if (visibleSelection.source === "group" && visibleSelection.parent?.resultReady) {
      const { detailsPath, parent } = visibleSelection;
      return () => {
        const acknowledgement = rolloutGroupAcknowledgement(parent);
        if (acknowledgement) {
          setAcknowledgedGroupResult(acknowledgement);
        }
        navigate(detailsPath);
      };
    }
    if (visibleSelection.source !== "completedRollout" || !visibleSelection.rolloutId) {
      return null;
    }
    const { detailsPath, rolloutId } = visibleSelection;
    return () => {
      setAcknowledgedResultId(rolloutId);
      navigate(detailsPath);
    };
  }, [canReadChannels, navigate, setAcknowledgedGroupResult, setAcknowledgedResultId, visibleSelection]);

  useEffect(() => {
    pendingFreshRefreshRef.current = false;
    inFlightRefreshRef.current = null;
    if (!enabled || (!canReadRollouts && !canReadChannels)) {
      rolloutsRef.current = [];
      groupsRef.current = [];
      lanesRef.current = [];
      missingLaneRolloutIdRef.current = null;
      exactLaneValidationRef.current = null;
      // eslint-disable-next-line react-hooks/set-state-in-effect -- discard cached remote data when polling access ends
      setSelection(null);
      return;
    }
    if (!canReadRollouts) {
      rolloutsRef.current = [];
      groupsRef.current = [];
    }
    if (!canReadChannels) {
      lanesRef.current = [];
    }
    const controller = new AbortController();
    const forceRefresh = () => void refresh(controller.signal, true);
    const initialRefreshId = window.setTimeout(forceRefresh, 0);
    window.addEventListener(ROLLOUT_CHANGED_EVENT, forceRefresh);
    return () => {
      window.clearTimeout(initialRefreshId);
      window.removeEventListener(ROLLOUT_CHANGED_EVENT, forceRefresh);
      controller.abort();
    };
  }, [canReadChannels, canReadRollouts, enabled, refresh]);

  useEffect(() => {
    if (!enabled || (!canReadRollouts && !canReadChannels)) {
      return;
    }
    const controller = new AbortController();
    const intervalId = window.setInterval(() => void refresh(controller.signal), pollIntervalMs);
    return () => {
      window.clearInterval(intervalId);
      controller.abort();
    };
  }, [canReadChannels, canReadRollouts, enabled, pollIntervalMs, refresh]);

  return useMemo(() => {
    const activeEvent =
      visibleSelection && visibleSelection.source !== "convergence" && !canReadChannels
        ? { ...visibleSelection.event, scopeLabel: "Rollout lane" }
        : (visibleSelection?.event ?? null);
    return {
      activeEvent,
      detailsPath: canReadChannels ? (visibleSelection?.detailsPath ?? null) : null,
      hasVisiblePill: activeEvent !== null,
      onViewRollout,
    };
  }, [canReadChannels, onViewRollout, visibleSelection]);
}
