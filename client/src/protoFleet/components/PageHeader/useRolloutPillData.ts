import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { create } from "@bufbuild/protobuf";

import { rolloutClient } from "@/protoFleet/api/clients";
import {
  ListRolloutLanesRequestSchema,
  ListRolloutsRequestSchema,
  RolloutState,
} from "@/protoFleet/api/generated/rollout/v1/rollout_pb";
import { isAbortError, isAuthOrPermissionError } from "@/protoFleet/api/requestErrors";
import { ROLLOUT_CHANGED_EVENT } from "@/protoFleet/api/rolloutEvents";
import { mapRollout, mapRolloutLane, mapRolloutToEvent } from "@/protoFleet/api/rolloutMappers";
import {
  BETWEEN_CHANNEL_STRATEGY_KEY,
  firstActiveFirmwareConvergenceLane,
  laneForRollout,
  shouldMonitorRollout,
} from "@/protoFleet/features/rollout/betweenChannel/betweenChannelUtils";
import { mapFirmwareTransitionToRolloutEvent } from "@/protoFleet/features/rollout/firmwareTransitionRolloutEvent";
import type { RolloutEvent, RolloutLane, RolloutRecord } from "@/protoFleet/features/rollout/rolloutTypes";
import { useAuthErrors, useHasPermission } from "@/protoFleet/store";

export interface UseRolloutPillDataOptions {
  enabled?: boolean;
}

export interface UseRolloutPillDataResult {
  activeEvent: RolloutEvent | null;
  detailsPath: string | null;
  hasVisiblePill: boolean;
}

interface RolloutPillSelection {
  event: RolloutEvent;
  detailsPath: string;
  fingerprint: string;
  source: "convergence" | "rollout";
}

const idlePollIntervalMs = 30_000;
const activePollIntervalMs = 5_000;
const rolloutLanesPath = "/settings/firmware?tab=rolloutLanes";
const activeRolloutStates = [
  RolloutState.CREATED,
  RolloutState.RUNNING,
  RolloutState.PAUSED,
  RolloutState.REVIEW,
  RolloutState.ABORTED,
  RolloutState.REVERTING,
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
): RolloutPillSelection {
  return {
    event,
    detailsPath,
    fingerprint: JSON.stringify({ detailsPath, event, source }),
    source,
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
    );
  }

  const lane = firstActiveFirmwareConvergenceLane(lanes);
  if (!lane) {
    return null;
  }

  return selection(
    mapFirmwareTransitionToRolloutEvent(lane.firmwareConvergence, {
      scopeLabel: lane.label,
      startedAt: lane.createdAt,
    }),
    `${rolloutLanesPath}&setupLane=${encodeURIComponent(lane.id)}`,
    "convergence",
  );
}

export function useRolloutPillData({ enabled = true }: UseRolloutPillDataOptions = {}): UseRolloutPillDataResult {
  const { handleAuthErrors } = useAuthErrors();
  const canReadRollouts = useHasPermission("rollout:read");
  const canReadChannels = useHasPermission("channel:read");
  const [selection, setSelection] = useState<RolloutPillSelection | null>(null);
  const rolloutsRef = useRef<RolloutRecord[]>([]);
  const lanesRef = useRef<RolloutLane[]>([]);
  const inFlightRefreshRef = useRef<Promise<void> | null>(null);
  const pendingFreshRefreshRef = useRef(false);
  const visibleSelection =
    enabled &&
    ((selection?.source === "rollout" && canReadRollouts) || (selection?.source === "convergence" && canReadChannels))
      ? selection
      : null;
  const pollIntervalMs = visibleSelection ? activePollIntervalMs : idlePollIntervalMs;

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
        if (!canReadRollouts) {
          rolloutsRef.current = [];
        }
        if (!canReadChannels) {
          lanesRef.current = [];
        }
        const [rolloutsResult, lanesResult] = await Promise.allSettled([
          canReadRollouts
            ? rolloutClient.listRollouts(create(ListRolloutsRequestSchema, { states: activeRolloutStates }), { signal })
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
            rolloutsRef.current = rolloutsResult.value.rollouts.map(mapRollout);
          }
        } else if (!isAbortError(rolloutsResult.reason, signal)) {
          if (isAuthOrPermissionError(rolloutsResult.reason)) {
            rolloutsRef.current = [];
          }
          handleAuthErrors({ error: rolloutsResult.reason });
        }

        if (lanesResult.status === "fulfilled") {
          if (lanesResult.value) {
            lanesRef.current = lanesResult.value.lanes.map((lane) => mapRolloutLane(lane));
          }
        } else if (!isAbortError(lanesResult.reason, signal)) {
          if (isAuthOrPermissionError(lanesResult.reason)) {
            lanesRef.current = [];
          }
          handleAuthErrors({ error: lanesResult.reason });
        }

        const nextSelection = selectPill(rolloutsRef.current, lanesRef.current);
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

  useEffect(() => {
    pendingFreshRefreshRef.current = false;
    inFlightRefreshRef.current = null;
    if (!enabled || (!canReadRollouts && !canReadChannels)) {
      rolloutsRef.current = [];
      lanesRef.current = [];
      // eslint-disable-next-line react-hooks/set-state-in-effect -- discard cached remote data when polling access ends
      setSelection(null);
      return;
    }
    if (!canReadRollouts) {
      rolloutsRef.current = [];
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
      visibleSelection?.source === "rollout" && !canReadChannels
        ? { ...visibleSelection.event, scopeLabel: "Rollout lane" }
        : (visibleSelection?.event ?? null);
    return {
      activeEvent,
      detailsPath: canReadChannels ? (visibleSelection?.detailsPath ?? null) : null,
      hasVisiblePill: activeEvent !== null,
    };
  }, [canReadChannels, visibleSelection]);
}
