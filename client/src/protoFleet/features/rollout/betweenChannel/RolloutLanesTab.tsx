import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useSearchParams } from "react-router-dom";
import type { Timestamp } from "@bufbuild/protobuf/wkt";

import { isAbortError, toError } from "@/protoFleet/api/requestErrors";
import { ROLLOUT_CHANGED_EVENT } from "@/protoFleet/api/rolloutEvents";
import { mapRolloutToEvent } from "@/protoFleet/api/rolloutMappers";
import { newestRollout, newestRolloutGroup } from "@/protoFleet/api/rolloutMerge";
import { registerRolloutLaneTabPollingOwner } from "@/protoFleet/api/rolloutPollingOwnership";
import { type FirmwareFileInfo, useFirmwareApi } from "@/protoFleet/api/useFirmwareApi";
import { useRolloutApi } from "@/protoFleet/api/useRolloutApi";
import AggregateRolloutStatus from "@/protoFleet/features/rollout/betweenChannel/AggregateRolloutStatus";
import BetweenChannelRolloutStatus from "@/protoFleet/features/rollout/betweenChannel/BetweenChannelRolloutStatus";
import {
  canCompleteWithFailures,
  canRevertRollout,
  compareRolloutChildren,
  firstActiveFirmwareConvergenceLane,
  hasActiveFirmwareConvergence,
  isFirmwareConvergenceReady,
  laneForRollout,
  rolloutIdempotencyKey,
  shouldMonitorRollout,
} from "@/protoFleet/features/rollout/betweenChannel/betweenChannelUtils";
import CreateRolloutLaneModal, {
  type CreateRolloutLaneValues,
} from "@/protoFleet/features/rollout/betweenChannel/CreateRolloutLaneModal";
import DeleteRolloutLaneDialog from "@/protoFleet/features/rollout/betweenChannel/DeleteRolloutLaneDialog";
import LaneFirmwareConvergenceStatus from "@/protoFleet/features/rollout/betweenChannel/LaneFirmwareConvergenceStatus";
import ManageRolloutLaneDeclarationsModal from "@/protoFleet/features/rollout/betweenChannel/ManageRolloutLaneDeclarationsModal";
import ManageRolloutLaneMembersModal from "@/protoFleet/features/rollout/betweenChannel/ManageRolloutLaneMembersModal";
import { admitRolloutChild } from "@/protoFleet/features/rollout/betweenChannel/rolloutChildAdmission";
import RolloutLanesTable, { type LaneTableRow } from "@/protoFleet/features/rollout/betweenChannel/RolloutLanesTable";
import StartRolloutLaneModal, {
  type StartRolloutLaneValues,
} from "@/protoFleet/features/rollout/betweenChannel/StartRolloutLaneModal";
import {
  isCompletedRolloutResult,
  useAcknowledgedRolloutResultId,
} from "@/protoFleet/features/rollout/rolloutResultAcknowledgement";
import type {
  RolloutGroup,
  RolloutLane,
  RolloutLaneMembershipUpdateResult,
  RolloutLaneTopologyAnomaly,
  RolloutLaneTopologyReadiness,
  RolloutRecord,
} from "@/protoFleet/features/rollout/rolloutTypes";
import ViewRolloutModal from "@/protoFleet/features/rollout/ViewRolloutModal";
import { Alert, Info } from "@/shared/assets/icons";
import Button, { sizes, variants } from "@/shared/components/Button";
import Callout, { intents } from "@/shared/components/Callout";
import ProgressCircular from "@/shared/components/ProgressCircular";
import { pushToast, STATUSES } from "@/shared/features/toaster";

function deleteLaneIdempotencyKey(laneId: string, expectedRevision: bigint): string {
  return `delete-lane:${laneId}:${expectedRevision}`;
}

function latestRolloutForLane(
  lane: RolloutLane,
  rolloutsById: ReadonlyMap<string, RolloutRecord>,
): RolloutRecord | undefined {
  return [...lane.channels]
    .sort((a, b) => b.position - a.position)
    .flatMap((channel) => (channel.rolloutId ? [rolloutsById.get(channel.rolloutId)] : []))
    .find((rollout): rollout is RolloutRecord => rollout !== undefined);
}

function latestFirmwareConvergenceMemberUpdate(lane: RolloutLane | undefined): Timestamp | undefined {
  return lane?.firmwareConvergence.members.reduce<Timestamp | undefined>(
    (latest, member) =>
      !latest ||
      (member.updatedAt &&
        (member.updatedAt.seconds > latest.seconds ||
          (member.updatedAt.seconds === latest.seconds && member.updatedAt.nanos > latest.nanos)))
        ? member.updatedAt
        : latest,
    undefined,
  );
}

function resolveLane(
  lanes: RolloutLane[],
  loadedLane: RolloutLane | null,
  laneId: string | null,
  preferDetailed = false,
): RolloutLane | undefined {
  if (!laneId) {
    return undefined;
  }
  const aggregateLane = lanes.find((lane) => lane.id === laneId);
  const detailedLane = loadedLane?.id === laneId ? loadedLane : undefined;
  if (preferDetailed) {
    return detailedLane ?? aggregateLane;
  }
  if (!aggregateLane || !detailedLane) {
    return aggregateLane ?? detailedLane;
  }
  return detailedLane.revision >= aggregateLane.revision ? detailedLane : aggregateLane;
}

function topologyAnomalyLabel(anomaly: RolloutLaneTopologyAnomaly): string {
  const labels: Record<RolloutLaneTopologyAnomaly["type"], string> = {
    nullIdentity: "Missing model identity",
    ambiguousTargetMatch: "Ambiguous firmware target",
    noTargetMatch: "No compatible firmware target",
    physicalMismatch: "Physical channel mismatch",
    missingBinding: "Missing model binding",
    duplicateActiveBinding: "Duplicate active binding",
    unknown: "Unknown topology anomaly",
  };
  return `${anomaly.deviceIdentifier || "Lane"}: ${labels[anomaly.type]}`;
}

function rolloutModelLabel(child: RolloutRecord): string {
  return [child.manufacturer, child.model].filter(Boolean).join(" ") || child.modelIdentityKey || "Model";
}

interface TopologyReadinessAdministrationProps {
  readiness: RolloutLaneTopologyReadiness | null;
  isLoading: boolean;
  error: string | null;
  forbidden: boolean;
  stale: boolean;
  canManage: boolean;
  onRetry: () => void;
  onLoadMore: (pageToken: string) => void;
  onRepair: (anomaly: RolloutLaneTopologyAnomaly) => void;
  onEnable: (readiness: RolloutLaneTopologyReadiness) => void;
}

function TopologyReadinessAdministration({
  readiness,
  isLoading,
  error,
  forbidden,
  stale,
  canManage,
  onRetry,
  onLoadMore,
  onRepair,
  onEnable,
}: TopologyReadinessAdministrationProps) {
  if (readiness?.enabled) {
    return null;
  }
  if (isLoading && !readiness) {
    return (
      <Callout
        intent={intents.information}
        prefixIcon={<Info />}
        title="Checking model topology readiness"
        subtitle="Loaded rollout lanes remain available while readiness is checked."
      />
    );
  }
  if (forbidden) {
    return (
      <Callout
        intent={intents.warning}
        prefixIcon={<Alert />}
        title="Topology readiness is read only"
        subtitle="Your channel access does not include this readiness report. Loaded rollout lanes remain available."
      />
    );
  }
  if (!readiness) {
    return (
      <Callout
        intent={intents.warning}
        prefixIcon={<Alert />}
        title="Topology readiness is unavailable"
        subtitle={error ?? "Try loading the readiness report again."}
        buttonText="Retry readiness"
        buttonOnClick={onRetry}
      />
    );
  }
  const ready = readiness.anomalyCount === 0n && readiness.activeLegacyRolloutCount === 0n;
  const repairable = readiness.anomalies.filter(
    (anomaly) =>
      anomaly.laneModelId &&
      anomaly.laneModelRevision !== undefined &&
      anomaly.supportedRepairActions.includes("repairBinding"),
  );
  return (
    <Callout
      intent={error || stale || !ready ? intents.warning : intents.information}
      prefixIcon={error || stale || !ready ? <Alert /> : <Info />}
      title={
        stale ? "Topology readiness may be stale" : ready ? "Model topology is ready" : "Model topology is not ready"
      }
      subtitle={
        <div className="grid gap-3">
          <div>
            {readiness.anomalyCount.toLocaleString()} anomalies · {readiness.activeLegacyRolloutCount.toLocaleString()}{" "}
            active legacy rollouts
          </div>
          {error ? <div>{error}</div> : null}
          {readiness.anomalies.length > 0 ? (
            <ul className="list-disc pl-5">
              {readiness.anomalies.map((anomaly) => (
                <li key={anomaly.id}>{topologyAnomalyLabel(anomaly)}</li>
              ))}
            </ul>
          ) : null}
          {readiness.nextAnomalyPageToken ? (
            <Button
              text="Load more anomalies"
              variant={variants.secondary}
              size={sizes.compact}
              disabled={isLoading}
              onClick={() => onLoadMore(readiness.nextAnomalyPageToken ?? "")}
            />
          ) : null}
          {canManage ? (
            <div className="flex flex-wrap gap-2">
              {repairable.map((anomaly) => (
                <Button
                  key={anomaly.id}
                  text={`Repair ${anomaly.deviceIdentifier}`}
                  variant={variants.secondary}
                  size={sizes.compact}
                  disabled={isLoading}
                  onClick={() => onRepair(anomaly)}
                />
              ))}
              <Button
                text="Enable model topology"
                variant={variants.primary}
                size={sizes.compact}
                disabled={!ready || isLoading}
                onClick={() => onEnable(readiness)}
              />
              {error || stale ? (
                <Button
                  text="Refresh readiness"
                  variant={variants.secondary}
                  size={sizes.compact}
                  disabled={isLoading}
                  onClick={onRetry}
                />
              ) : null}
            </div>
          ) : (
            <div>Channel management access is required to repair anomalies or enable model topology.</div>
          )}
        </div>
      }
    />
  );
}

export default function RolloutLanesTab() {
  useEffect(() => registerRolloutLaneTabPollingOwner(), []);
  const [searchParams, setSearchParams] = useSearchParams();
  const [acknowledgedResultId, setAcknowledgedResultId] = useAcknowledgedRolloutResultId();
  const {
    lane: loadedLane,
    lanes,
    rollouts,
    rolloutGroups,
    isMutating,
    loadError,
    mutationError,
    topologyReadiness = null,
    isTopologyReadinessLoading = false,
    topologyReadinessError = null,
    topologyReadinessForbidden = false,
    topologyReadinessStale = false,
    permissions,
    listRolloutLanes,
    getRolloutLane,
    listRolloutLaneMembers,
    previewRolloutLaneMembershipChange,
    updateRolloutLaneMembership,
    previewRolloutLaneModelDeclaration,
    createRolloutLaneModelDeclaration,
    publishRolloutLaneModelTarget,
    previewRolloutLaneModelMembershipChange,
    updateRolloutLaneModelMembership,
    previewRolloutLane,
    createRolloutLane,
    deleteRolloutLane,
    getRolloutLaneTopologyReadiness,
    repairRolloutLaneModelBinding,
    enableRolloutLaneModelTopology,
    startRolloutLane,
    listRolloutGroups,
    getRollout,
    getRolloutGroup,
    admitRollout,
    continueRollout,
    pauseRollout,
    resumeRollout,
    abortRollout,
    revertRollout,
    completeRollout,
  } = useRolloutApi();
  const { listFirmwareFiles } = useFirmwareApi();
  const [files, setFiles] = useState<FirmwareFileInfo[]>([]);
  const [pageError, setPageError] = useState<string | null>(null);
  const [isInitialLoading, setIsInitialLoading] = useState(true);
  const [isLoadingFiles, setIsLoadingFiles] = useState(false);
  const [showCreate, setShowCreate] = useState(false);
  const [manageMembersLaneId, setManageMembersLaneId] = useState<string | null>(null);
  const [manageDeclarationsLaneId, setManageDeclarationsLaneId] = useState<string | null>(null);
  const [deleteLaneId, setDeleteLaneId] = useState<string | null>(null);
  const [startLane, setStartLane] = useState<RolloutLane | null>(null);
  const [isPreparingLane, setIsPreparingLane] = useState(false);
  const [focusedRolloutId, setFocusedRolloutId] = useState<string | null>(null);
  const [activeParent, setActiveParent] = useState<RolloutGroup | null>(null);
  const [loadingParentId, setLoadingParentId] = useState<string | null>(null);
  const [childMutationState, setChildMutationState] = useState<Record<string, { loading: boolean; error?: string }>>(
    {},
  );
  const [modalRolloutId, setModalRolloutId] = useState<string | null>(null);
  const detailRefreshControllerRef = useRef<AbortController | null>(null);
  const groupSummaryRefreshRef = useRef<Promise<RolloutGroup[]> | null>(null);
  const laneListRefreshControllerRef = useRef<AbortController | null>(null);
  const retrySetupControllerRef = useRef<AbortController | null>(null);
  const prepareStartControllerRef = useRef<AbortController | null>(null);
  const locallyOpenedParentIdRef = useRef<string | null>(null);
  const skipSetupHydrationLaneIdRef = useRef<string | null>(null);
  const setupLaneRef = useRef<RolloutLane | undefined>(undefined);
  const setupLaneId = searchParams.get("setupLane");
  const resultRolloutId = searchParams.get("rollout");
  const parentRolloutId = searchParams.get("rolloutParent");
  const focusedChildId = searchParams.get("rolloutChild");
  const focusedChildIdRef = useRef(focusedChildId);
  focusedChildIdRef.current = focusedChildId;
  const resultRollout =
    resultRolloutId && permissions.canRead ? rollouts.find((rollout) => rollout.id === resultRolloutId) : undefined;
  const activeFirmwareConvergenceLane = useMemo(() => firstActiveFirmwareConvergenceLane(lanes), [lanes]);
  const selectedSetupLaneId = setupLaneId ?? activeFirmwareConvergenceLane?.id ?? null;

  const refreshGroupSummaries = useCallback(
    (signal?: AbortSignal): Promise<RolloutGroup[]> => {
      if (groupSummaryRefreshRef.current) {
        return groupSummaryRefreshRef.current;
      }
      let request: Promise<RolloutGroup[]>;
      request = listRolloutGroups({ signal }).finally(() => {
        if (groupSummaryRefreshRef.current === request) {
          groupSummaryRefreshRef.current = null;
        }
      });
      groupSummaryRefreshRef.current = request;
      return request;
    },
    [listRolloutGroups],
  );

  const updateSetupLaneParam = useCallback(
    (laneId: string | null) => {
      setSearchParams(
        (currentParams) => {
          const nextParams = new URLSearchParams(currentParams);
          if (laneId) {
            nextParams.set("tab", "rolloutLanes");
            nextParams.set("setupLane", laneId);
          } else {
            nextParams.delete("setupLane");
          }
          return nextParams;
        },
        { replace: true },
      );
    },
    [setSearchParams],
  );

  const updateParentFocusParams = useCallback(
    (parentId: string | null, childId: string | null) => {
      setSearchParams(
        (currentParams) => {
          const nextParams = new URLSearchParams(currentParams);
          if (parentId) {
            nextParams.set("tab", "rolloutLanes");
            nextParams.set("rolloutParent", parentId);
            if (childId) {
              nextParams.set("rolloutChild", childId);
            } else {
              nextParams.delete("rolloutChild");
            }
          } else {
            nextParams.delete("rolloutParent");
            nextParams.delete("rolloutChild");
          }
          return nextParams;
        },
        { replace: true },
      );
    },
    [setSearchParams],
  );

  const loadData = useCallback(async () => {
    if (!permissions.canReadChannels) {
      return;
    }
    setPageError(null);
    setIsLoadingFiles(permissions.canManageChannels);
    try {
      const requests: Promise<unknown>[] = [listRolloutLanes()];
      if (permissions.canRead) {
        requests.push(refreshGroupSummaries());
      }
      if (permissions.canManageChannels) {
        requests.push(listFirmwareFiles().then(setFiles));
      }
      await Promise.all(requests);
    } catch (error) {
      setPageError(error instanceof Error ? error.message : "Failed to load firmware rollout lanes.");
    } finally {
      setIsInitialLoading(false);
      setIsLoadingFiles(false);
    }
  }, [
    listFirmwareFiles,
    listRolloutLanes,
    permissions.canManageChannels,
    permissions.canRead,
    permissions.canReadChannels,
    refreshGroupSummaries,
  ]);

  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect -- loads durable lane state from external APIs
    void loadData();
  }, [loadData]);

  useEffect(() => {
    if (!permissions.canRead || !parentRolloutId) {
      if (permissions.canRead && locallyOpenedParentIdRef.current) {
        return;
      }
      // eslint-disable-next-line react-hooks/set-state-in-effect -- discard inaccessible URL-focused remote state
      setActiveParent(null);
      // eslint-disable-next-line react-hooks/set-state-in-effect -- discard inaccessible URL-focused remote state
      setFocusedRolloutId(null);
      // eslint-disable-next-line react-hooks/set-state-in-effect -- discard inaccessible URL-focused remote state
      setLoadingParentId(null);
      return;
    }
    if (locallyOpenedParentIdRef.current === parentRolloutId) {
      locallyOpenedParentIdRef.current = null;
      return;
    }
    if (activeParent?.id === parentRolloutId) {
      return;
    }
    const controller = new AbortController();
    // eslint-disable-next-line react-hooks/set-state-in-effect -- prevent stale parent content while the URL target reloads
    setActiveParent(null);
    // eslint-disable-next-line react-hooks/set-state-in-effect -- prevent stale child focus while the URL target reloads
    setFocusedRolloutId(null);
    // eslint-disable-next-line react-hooks/set-state-in-effect -- expose URL-focused aggregate hydration without hiding loaded lanes
    setLoadingParentId(parentRolloutId);
    void getRolloutGroup({ parentId: parentRolloutId, signal: controller.signal })
      .then((parent) => {
        if (controller.signal.aborted) {
          return;
        }
        setActiveParent(parent);
        setLoadingParentId(null);
        setFocusedRolloutId(
          parent.children.find((child) => child.id === focusedChildIdRef.current)?.id ?? parent.children[0]?.id ?? null,
        );
      })
      .catch((error) => {
        if (!isAbortError(error, controller.signal)) {
          setActiveParent(null);
          setFocusedRolloutId(null);
          setLoadingParentId(null);
          setPageError(error instanceof Error ? error.message : "Couldn't reload the aggregate rollout.");
        }
      });
    return () => controller.abort();
  }, [activeParent, getRolloutGroup, parentRolloutId, permissions.canRead]);

  useEffect(() => {
    if (!activeParent || activeParent.id !== parentRolloutId) {
      return;
    }
    const nextFocusedChildId = focusedChildId
      ? (activeParent.children.find((child) => child.id === focusedChildId)?.id ?? null)
      : null;
    // eslint-disable-next-line react-hooks/set-state-in-effect -- keep browser navigation and aggregate expansion synchronized
    setFocusedRolloutId(nextFocusedChildId);
  }, [activeParent, focusedChildId, parentRolloutId]);

  useEffect(() => {
    if (!permissions.canReadChannels) {
      return;
    }
    const controller = new AbortController();
    void getRolloutLaneTopologyReadiness({ signal: controller.signal }).catch(() => {
      // Readiness failures stay local to the administration callout.
    });
    return () => controller.abort();
  }, [getRolloutLaneTopologyReadiness, permissions.canReadChannels]);

  useEffect(() => {
    if (!resultRolloutId || !permissions.canRead) {
      return;
    }
    if (resultRollout) {
      if (isCompletedRolloutResult(resultRollout) && acknowledgedResultId !== resultRollout.id) {
        setAcknowledgedResultId(resultRollout.id);
      }
      return;
    }

    const controller = new AbortController();
    void getRollout({ rolloutId: resultRolloutId, signal: controller.signal })
      .then((rollout) => {
        if (!controller.signal.aborted && isCompletedRolloutResult(rollout) && acknowledgedResultId !== rollout.id) {
          setAcknowledgedResultId(rollout.id);
        }
      })
      .catch(() => {
        // The API hook exposes invalid or inaccessible rollout IDs through its normal load error.
      });
    return () => controller.abort();
  }, [acknowledgedResultId, getRollout, permissions.canRead, resultRollout, resultRolloutId, setAcknowledgedResultId]);

  const hydrateSetupLane = useCallback(
    async (laneId: string, signal: AbortSignal, firmwareConvergenceMembersUpdatedAfter?: Timestamp): Promise<void> => {
      try {
        await getRolloutLane({
          laneId,
          includeDeviceSetMembers: false,
          includeFirmwareConvergenceMembers: true,
          firmwareConvergenceMembersUpdatedAfter,
          signal,
        });
      } catch (error) {
        if (!isAbortError(error, signal)) {
          setPageError(toError(error, "Firmware convergence status is unavailable.").message);
        }
      }
    },
    [getRolloutLane],
  );

  useEffect(() => {
    if (!selectedSetupLaneId || !permissions.canReadChannels) {
      return;
    }
    if (skipSetupHydrationLaneIdRef.current === selectedSetupLaneId) {
      skipSetupHydrationLaneIdRef.current = null;
      return;
    }

    const controller = new AbortController();
    setPageError(null);
    void hydrateSetupLane(selectedSetupLaneId, controller.signal);

    return () => {
      controller.abort();
    };
  }, [hydrateSetupLane, permissions.canReadChannels, selectedSetupLaneId]);

  const allRollouts = useMemo(() => {
    const byId = new Map(rolloutGroups.flatMap((parent) => parent.children).map((rollout) => [rollout.id, rollout]));
    rollouts.forEach((rollout) => byId.set(rollout.id, rollout));
    return [...byId.values()];
  }, [rolloutGroups, rollouts]);
  const rows = useMemo<LaneTableRow[]>(() => {
    const rolloutsById = new Map(allRollouts.map((rollout) => [rollout.id, rollout]));
    return lanes.map((lane) => ({
      id: lane.id,
      lane,
      latestRollout: latestRolloutForLane(lane, rolloutsById),
    }));
  }, [allRollouts, lanes]);
  const setupLane = useMemo(
    () => resolveLane(lanes, loadedLane, selectedSetupLaneId, true),
    [lanes, loadedLane, selectedSetupLaneId],
  );
  const laneToDelete = useMemo(() => resolveLane(lanes, loadedLane, deleteLaneId), [deleteLaneId, lanes, loadedLane]);
  const managedMembersLane = useMemo(
    () => resolveLane(lanes, loadedLane, manageMembersLaneId),
    [lanes, loadedLane, manageMembersLaneId],
  );
  const managedDeclarationsLane = useMemo(
    () => resolveLane(lanes, loadedLane, manageDeclarationsLaneId),
    [lanes, loadedLane, manageDeclarationsLaneId],
  );
  const managedMembersLatestRollout = manageMembersLaneId
    ? rows.find((row) => row.id === manageMembersLaneId)?.latestRollout
    : undefined;
  setupLaneRef.current = setupLane;
  const shouldPollSetupLane = Boolean(setupLane && !isFirmwareConvergenceReady(setupLane));
  const focusedRollout = focusedRolloutId ? allRollouts.find((rollout) => rollout.id === focusedRolloutId) : undefined;
  const modalRollout = modalRolloutId ? allRollouts.find((rollout) => rollout.id === modalRolloutId) : undefined;
  const monitoredRolloutIdsKey = allRollouts
    .filter((rollout) => shouldMonitorRollout(rollout))
    .map((rollout) => rollout.id)
    .sort()
    .join("\0");
  const monitoredRolloutIds = useMemo(
    () => (monitoredRolloutIdsKey ? monitoredRolloutIdsKey.split("\0") : []),
    [monitoredRolloutIdsKey],
  );
  const hasMonitoredFirmwareConvergence = activeFirmwareConvergenceLane !== undefined;
  const monitoredRollout =
    resultRolloutId !== null
      ? resultRollout
      : (focusedRollout ??
        rows.find((row) => shouldMonitorRollout(row.latestRollout))?.latestRollout ??
        rows.find((row) => row.latestRollout && canRevertRollout(row.latestRollout))?.latestRollout);
  const monitoredLane = monitoredRollout ? laneForRollout(lanes, monitoredRollout.id) : undefined;
  const parentChildren = useMemo(() => {
    if (!activeParent) {
      return [];
    }
    const currentById = new Map(allRollouts.map((rollout) => [rollout.id, rollout]));
    return activeParent.children
      .map((child) => {
        const current = currentById.get(child.id);
        return newestRollout(current, child);
      })
      .sort(compareRolloutChildren);
  }, [activeParent, allRollouts]);
  const canManageLanes = permissions.canManageChannels;
  const canStartLane = permissions.canManageChannels && permissions.canManage;
  const refreshMonitoredDetails = useCallback(async () => {
    if (!permissions.canReadChannels || detailRefreshControllerRef.current) {
      return;
    }
    const controller = new AbortController();
    detailRefreshControllerRef.current = controller;
    try {
      await Promise.allSettled([
        ...(selectedSetupLaneId && shouldPollSetupLane
          ? [
              hydrateSetupLane(
                selectedSetupLaneId,
                controller.signal,
                latestFirmwareConvergenceMemberUpdate(setupLaneRef.current),
              ),
            ]
          : []),
        ...(permissions.canRead
          ? [
              refreshGroupSummaries(controller.signal).then(async (parents) => {
                if (!controller.signal.aborted) {
                  setActiveParent((current) => {
                    if (!current) {
                      return current;
                    }
                    const incoming = parents.find((parent) => parent.id === current.id);
                    return incoming ? newestRolloutGroup(current, incoming) : current;
                  });
                }
                if (activeParent?.id && !controller.signal.aborted) {
                  const detailedParent = await getRolloutGroup({
                    parentId: activeParent.id,
                    signal: controller.signal,
                  });
                  if (!controller.signal.aborted) {
                    setActiveParent((current) =>
                      current?.id === detailedParent.id ? newestRolloutGroup(current, detailedParent) : current,
                    );
                  }
                }
              }),
            ]
          : []),
      ]);
    } finally {
      if (detailRefreshControllerRef.current === controller) {
        detailRefreshControllerRef.current = null;
      }
    }
  }, [
    activeParent?.id,
    getRolloutGroup,
    hydrateSetupLane,
    permissions.canRead,
    permissions.canReadChannels,
    refreshGroupSummaries,
    selectedSetupLaneId,
    shouldPollSetupLane,
  ]);

  const refreshLaneList = useCallback(async () => {
    if (!permissions.canReadChannels || laneListRefreshControllerRef.current) {
      return;
    }
    const controller = new AbortController();
    laneListRefreshControllerRef.current = controller;
    try {
      await listRolloutLanes({ signal: controller.signal });
    } catch {
      // listRolloutLanes records non-abort failures in loadError.
    } finally {
      if (laneListRefreshControllerRef.current === controller) {
        laneListRefreshControllerRef.current = null;
      }
    }
  }, [listRolloutLanes, permissions.canReadChannels]);

  useEffect(() => {
    const refresh = () => {
      void refreshMonitoredDetails();
      void refreshLaneList();
    };
    window.addEventListener(ROLLOUT_CHANGED_EVENT, refresh);
    const detailInterval =
      monitoredRolloutIds.length > 0 || hasMonitoredFirmwareConvergence || shouldPollSetupLane
        ? window.setInterval(() => void refreshMonitoredDetails(), 5000)
        : undefined;
    const laneListPollIntervalMs = monitoredRolloutIds.length > 0 ? 5_000 : shouldPollSetupLane ? 30_000 : undefined;
    const laneListInterval =
      laneListPollIntervalMs === undefined
        ? undefined
        : window.setInterval(() => void refreshLaneList(), laneListPollIntervalMs);
    return () => {
      window.removeEventListener(ROLLOUT_CHANGED_EVENT, refresh);
      if (detailInterval !== undefined) {
        window.clearInterval(detailInterval);
      }
      if (laneListInterval !== undefined) {
        window.clearInterval(laneListInterval);
      }
    };
  }, [
    hasMonitoredFirmwareConvergence,
    monitoredRolloutIds.length,
    refreshLaneList,
    refreshMonitoredDetails,
    shouldPollSetupLane,
  ]);

  const retryLoadData = useCallback(async () => {
    retrySetupControllerRef.current?.abort();
    const controller = new AbortController();
    retrySetupControllerRef.current = controller;
    setPageError(null);
    try {
      await Promise.allSettled([
        loadData(),
        ...(selectedSetupLaneId ? [hydrateSetupLane(selectedSetupLaneId, controller.signal)] : []),
      ]);
    } finally {
      if (retrySetupControllerRef.current === controller) {
        retrySetupControllerRef.current = null;
      }
    }
  }, [hydrateSetupLane, loadData, selectedSetupLaneId]);

  const handleCreate = useCallback(
    async (values: CreateRolloutLaneValues) => {
      try {
        const createdLane = await createRolloutLane({
          ...values,
          idempotencyKey: rolloutIdempotencyKey("create-lane"),
        });
        setShowCreate(false);
        if (createdLane.firmwareConvergence.members.length >= createdLane.firmwareConvergence.totalCount) {
          skipSetupHydrationLaneIdRef.current = createdLane.id;
        }
        updateSetupLaneParam(createdLane.id);
        pushToast({ message: `Created ${values.label}`, status: STATUSES.success });
      } catch {
        // mutationError is rendered in the open modal.
      }
    },
    [createRolloutLane, updateSetupLaneParam],
  );

  const prepareStart = useCallback(
    async (lane: RolloutLane) => {
      prepareStartControllerRef.current?.abort();
      const controller = new AbortController();
      prepareStartControllerRef.current = controller;
      setIsPreparingLane(true);
      setPageError(null);
      try {
        const freshLane = await getRolloutLane({
          laneId: lane.id,
          includeDeviceSetMembers: true,
          includeFirmwareConvergenceMembers: false,
          signal: controller.signal,
        });
        if (controller.signal.aborted || prepareStartControllerRef.current !== controller) {
          return;
        }
        setStartLane(freshLane);
      } catch (error) {
        if (prepareStartControllerRef.current !== controller || isAbortError(error, controller.signal)) {
          return;
        }
        setPageError(error instanceof Error ? error.message : "Failed to load fresh lane membership.");
      } finally {
        if (prepareStartControllerRef.current === controller) {
          prepareStartControllerRef.current = null;
          setIsPreparingLane(false);
        }
      }
    },
    [getRolloutLane],
  );

  const openSetup = useCallback(
    (lane: RolloutLane) => {
      setPageError(null);
      updateSetupLaneParam(lane.id);
    },
    [updateSetupLaneParam],
  );

  const handleDelete = useCallback(async () => {
    if (!laneToDelete) {
      return;
    }
    try {
      await deleteRolloutLane({
        laneId: laneToDelete.id,
        expectedRevision: laneToDelete.revision,
        idempotencyKey: deleteLaneIdempotencyKey(laneToDelete.id, laneToDelete.revision),
        reason: "Delete rollout lane",
      });
      setDeleteLaneId(null);
      if (selectedSetupLaneId === laneToDelete.id) {
        updateSetupLaneParam(null);
      }
      pushToast({ message: `Deleted ${laneToDelete.label}`, status: STATUSES.success });
    } catch {
      // mutationError is rendered in the open dialog.
    }
  }, [deleteRolloutLane, laneToDelete, selectedSetupLaneId, updateSetupLaneParam]);

  const handleMembershipUpdated = useCallback(
    (result: RolloutLaneMembershipUpdateResult) => {
      if (result.transitionMembers.length > 0) {
        setManageMembersLaneId(null);
        skipSetupHydrationLaneIdRef.current = result.lane.id;
        updateSetupLaneParam(result.lane.id);
      }
      pushToast({ message: `Updated ${result.lane.label} membership`, status: STATUSES.success });
    },
    [updateSetupLaneParam],
  );

  const handleDeclarationUpdated = useCallback((_updatedLane: RolloutLane, message: string) => {
    setManageDeclarationsLaneId(null);
    pushToast({ message, status: STATUSES.success });
  }, []);

  const retryTopologyReadiness = useCallback(() => {
    void getRolloutLaneTopologyReadiness().catch(() => {
      // The readiness callout renders this failure without replacing lane content.
    });
  }, [getRolloutLaneTopologyReadiness]);

  const loadMoreTopologyAnomalies = useCallback(
    (anomalyPageToken: string) => {
      void getRolloutLaneTopologyReadiness({ anomalyPageToken }).catch(() => {
        // The readiness callout keeps the already loaded anomaly pages visible.
      });
    },
    [getRolloutLaneTopologyReadiness],
  );

  const repairTopologyAnomaly = useCallback(
    (anomaly: RolloutLaneTopologyAnomaly) => {
      if (!anomaly.laneModelId || anomaly.laneModelRevision === undefined) {
        return;
      }
      void repairRolloutLaneModelBinding({
        laneId: anomaly.laneId,
        laneModelId: anomaly.laneModelId,
        deviceIdentifier: anomaly.deviceIdentifier,
        expectedRevision: anomaly.laneModelRevision,
        idempotencyKey: rolloutIdempotencyKey("repair-model-binding", anomaly.id),
        reason: "Repair rollout lane model binding from topology readiness",
      }).catch(() => {
        // The readiness callout keeps the last loaded report visible with the local error.
      });
    },
    [repairRolloutLaneModelBinding],
  );

  const enableTopology = useCallback(
    (readiness: RolloutLaneTopologyReadiness) => {
      void enableRolloutLaneModelTopology({
        expectedRevision: readiness.revision,
        idempotencyKey: rolloutIdempotencyKey("enable-model-topology"),
        reason: "Enable rollout lane model topology after readiness review",
      })
        .then(() => listRolloutLanes())
        .catch(() => {
          // The readiness callout keeps the last loaded report visible with the local error.
        });
    },
    [enableRolloutLaneModelTopology, listRolloutLanes],
  );

  useEffect(
    () => () => {
      detailRefreshControllerRef.current?.abort();
      detailRefreshControllerRef.current = null;
      laneListRefreshControllerRef.current?.abort();
      laneListRefreshControllerRef.current = null;
      retrySetupControllerRef.current?.abort();
      retrySetupControllerRef.current = null;
      prepareStartControllerRef.current?.abort();
      prepareStartControllerRef.current = null;
    },
    [],
  );

  const handleStart = useCallback(
    async (values: StartRolloutLaneValues) => {
      try {
        const parentStartKey = rolloutIdempotencyKey("start-rollout");
        const result = values.modelPlans
          ? await startRolloutLane({
              laneId: values.laneId,
              name: values.name,
              reason: values.reason,
              modelPlans: values.modelPlans.map((plan) => ({
                ...plan,
                modelStartKey: `${parentStartKey}:${plan.laneModelId}`,
              })),
              idempotencyKey: parentStartKey,
            })
          : await startRolloutLane({
              laneId: values.laneId,
              name: values.name,
              reason: values.reason,
              firmwareFileIds: values.firmwareFileIds,
              batches: values.batches,
              hashratePolicy: values.hashratePolicy,
              idempotencyKey: parentStartKey,
            });
        setStartLane(null);
        updateSetupLaneParam(null);
        const initialFocus =
          result.children.find(({ rollout }) => rollout.state === "created")?.rollout.id ?? result.rollout.id;
        setFocusedRolloutId(initialFocus);
        if (result.parent) {
          locallyOpenedParentIdRef.current = result.parent.id;
          updateParentFocusParams(result.parent.id, initialFocus);
          setActiveParent(result.parent);
        }
        await Promise.allSettled(
          result.children.map(async ({ rollout, firstBatchId }) => {
            await admitRolloutChild({
              rollout,
              batchId: firstBatchId,
              admissionAttempt: rollout.batches[0]?.admissionAttempt ?? 0,
              reason: "Start first manual batch",
              admit: admitRollout,
              updateState: (rolloutId, state) =>
                setChildMutationState((current) => ({ ...current, [rolloutId]: state })),
              onAdmitted: (admitted) =>
                setActiveParent((current) =>
                  current
                    ? {
                        ...current,
                        children: current.children.map((child) => (child.id === admitted.id ? admitted : child)),
                      }
                    : current,
                ),
            });
          }),
        );
        pushToast({ message: `Started ${values.name}`, status: STATUSES.success });
      } catch {
        // mutationError is rendered in the open modal.
      }
    },
    [admitRollout, startRolloutLane, updateParentFocusParams, updateSetupLaneParam],
  );

  const retryAdmission = useCallback(
    async (rollout: RolloutRecord) => {
      const batch = rollout.batches.find((candidate) => candidate.state === "pending");
      if (!batch) {
        return;
      }
      try {
        await admitRolloutChild({
          rollout,
          batchId: batch.id,
          admissionAttempt: batch.admissionAttempt ?? 0,
          reason: "Retry model rollout admission",
          admit: admitRollout,
          updateState: (rolloutId, state) => setChildMutationState((current) => ({ ...current, [rolloutId]: state })),
          onAdmitted: (admitted) => setFocusedRolloutId(admitted.id),
        });
      } catch (error) {
        pushToast({
          message: error instanceof Error ? error.message : "Couldn't retry the model rollout.",
          status: STATUSES.error,
        });
      }
    },
    [admitRollout],
  );

  const runControl = useCallback(
    async (rollout: RolloutRecord, action: "continue" | "pause" | "resume" | "abort" | "revert", reason: string) => {
      const input = {
        rolloutId: rollout.id,
        expectedRevision: rollout.revision,
        idempotencyKey: rolloutIdempotencyKey(action),
        reason,
      };
      try {
        const operations = {
          continue: continueRollout,
          pause: pauseRollout,
          resume: resumeRollout,
          abort: abortRollout,
          revert: revertRollout,
        };
        const updated = await operations[action](input);
        setFocusedRolloutId(updated.id);
        pushToast({
          message: `${action.charAt(0).toUpperCase()}${action.slice(1)} request accepted`,
          status: STATUSES.success,
        });
      } catch (error) {
        pushToast({
          message: error instanceof Error ? error.message : `Failed to ${action} rollout.`,
          status: STATUSES.error,
        });
      }
    },
    [abortRollout, continueRollout, pauseRollout, resumeRollout, revertRollout],
  );

  const runCompleteWithFailures = useCallback(
    async (rollout: RolloutRecord) => {
      try {
        const updated = await completeRollout({
          rolloutId: rollout.id,
          expectedRevision: rollout.revision,
          idempotencyKey: rolloutIdempotencyKey("complete-with-failures"),
          reason: "Complete final batch with terminal failures",
          withFailures: true,
        });
        setFocusedRolloutId(updated.id);
        pushToast({ message: "Rollout completed with failures", status: STATUSES.success });
      } catch (error) {
        pushToast({
          message: error instanceof Error ? error.message : "Failed to complete rollout with failures.",
          status: STATUSES.error,
        });
      }
    },
    [completeRollout],
  );

  if (!permissions.canReadChannels) {
    return (
      <Callout
        intent={intents.information}
        prefixIcon={<Info />}
        title="Rollout lane access required"
        subtitle="Ask an administrator for channel read access to view stable firmware rollout lanes."
      />
    );
  }

  return (
    <div className="grid gap-8">
      <div className="flex items-start justify-between gap-4 phone:flex-col phone:items-stretch">
        <div>
          <div className="text-heading-100 text-text-primary">Stable firmware lanes</div>
          <div className="mt-1 max-w-2xl text-300 text-text-primary-70">
            Deploy immutable releases while miners stay attached to one stable operator-facing lane.
          </div>
        </div>
        {canManageLanes ? (
          <Button
            text="Create lane"
            variant={variants.primary}
            size={sizes.compact}
            className="phone:w-full"
            onClick={() => setShowCreate(true)}
          />
        ) : null}
      </div>

      {pageError || loadError ? (
        <Callout
          intent={intents.danger}
          prefixIcon={<Alert />}
          title="Firmware rollout lanes are unavailable"
          subtitle={pageError ?? loadError ?? undefined}
          buttonText="Retry"
          buttonOnClick={() => void retryLoadData()}
        />
      ) : null}

      <TopologyReadinessAdministration
        readiness={topologyReadiness}
        isLoading={isTopologyReadinessLoading}
        error={topologyReadinessError}
        forbidden={topologyReadinessForbidden}
        stale={topologyReadinessStale}
        canManage={permissions.canManageChannels}
        onRetry={retryTopologyReadiness}
        onLoadMore={loadMoreTopologyAnomalies}
        onRepair={repairTopologyAnomaly}
        onEnable={enableTopology}
      />

      {isInitialLoading || isLoadingFiles ? (
        <div className="flex items-center justify-center gap-3 py-10 text-300 text-text-primary-70">
          <ProgressCircular indeterminate className="text-core-primary-fill" />
          Loading rollout lanes...
        </div>
      ) : (
        <RolloutLanesTable
          rows={rows}
          canStart={canStartLane}
          canDelete={canManageLanes}
          deletePermissionBlockedReason={
            canManageLanes && !permissions.canRead
              ? "Rollout read access is required to verify this lane is safe to delete."
              : undefined
          }
          isPreparingStart={isPreparingLane}
          onSetup={(lane) => void openSetup(lane)}
          onManageMembers={(lane) => setManageMembersLaneId(lane.id)}
          onManageDeclarations={(lane) => setManageDeclarationsLaneId(lane.id)}
          onStart={(lane) => void prepareStart(lane)}
          onView={(rollout) => {
            if (!rollout.parentId) {
              void getRollout({ rolloutId: rollout.id })
                .then(() => {
                  setFocusedRolloutId(rollout.id);
                  setModalRolloutId(rollout.id);
                })
                .catch((error) => {
                  setPageError(error instanceof Error ? error.message : "Couldn't load rollout details.");
                });
              return;
            }
            const parent = rolloutGroups.find((candidate) => candidate.id === rollout.parentId);
            setFocusedRolloutId(rollout.id);
            if (parent) {
              locallyOpenedParentIdRef.current = parent.id;
              setActiveParent(parent);
            }
            updateParentFocusParams(rollout.parentId, rollout.id);
          }}
          onDelete={(lane) => setDeleteLaneId(lane.id)}
        />
      )}

      {setupLane ? (
        <LaneFirmwareConvergenceStatus
          lane={setupLane}
          canStart={canStartLane}
          onClose={hasActiveFirmwareConvergence(setupLane) ? undefined : () => updateSetupLaneParam(null)}
          onStart={() => void prepareStart(setupLane)}
        />
      ) : null}

      {parentRolloutId && loadingParentId === parentRolloutId && !activeParent ? (
        <div className="flex items-center justify-center gap-3 py-6 text-300 text-text-primary-70" role="status">
          <ProgressCircular indeterminate className="text-core-primary-fill" />
          Loading overall rollout...
        </div>
      ) : null}

      {activeParent ? (
        <AggregateRolloutStatus
          parent={activeParent}
          children={parentChildren}
          focusedChildId={focusedRolloutId}
          laneLabel={lanes.find((lane) => lane.id === activeParent.laneId)?.label ?? "Rollout lane"}
          canControl={permissions.canControl}
          childMutationState={childMutationState}
          onFocusChange={(next) => {
            setFocusedRolloutId(next);
            updateParentFocusParams(activeParent.id, next);
          }}
          onAdmit={(child) => void retryAdmission(child)}
          onPause={(child) => void runControl(child, "pause", `Pause ${rolloutModelLabel(child)}`)}
          onResume={(child) => void runControl(child, "resume", `Resume ${rolloutModelLabel(child)}`)}
          onContinue={(child, reason) =>
            void runControl(child, "continue", reason ?? `Continue ${rolloutModelLabel(child)} after review`)
          }
          onAbort={(child) => void runControl(child, "abort", `Abort new ${rolloutModelLabel(child)} rollout work`)}
          onRevert={(child) =>
            void runControl(child, "revert", `Restore the captured ${rolloutModelLabel(child)} release`)
          }
          onCompleteWithFailures={(child) => void runCompleteWithFailures(child)}
        />
      ) : monitoredRollout && monitoredLane ? (
        <BetweenChannelRolloutStatus
          rollout={monitoredRollout}
          laneLabel={monitoredLane.label}
          canControl={permissions.canControl}
          isMutating={isMutating}
          onAdmit={() => void retryAdmission(monitoredRollout)}
          onPause={() => void runControl(monitoredRollout, "pause", "Paused by operator")}
          onResume={() => void runControl(monitoredRollout, "resume", "Resumed by operator")}
          onContinue={(reason) =>
            void runControl(monitoredRollout, "continue", reason ?? "Continue after manual review")
          }
          onAbort={() => void runControl(monitoredRollout, "abort", "Abort new rollout work")}
          onRevert={() => void runControl(monitoredRollout, "revert", "Restore the captured source release")}
          onCompleteWithFailures={() => void runCompleteWithFailures(monitoredRollout)}
        />
      ) : null}

      {showCreate ? (
        <CreateRolloutLaneModal
          open
          files={files}
          isSubmitting={isMutating}
          error={mutationError}
          onDismiss={() => setShowCreate(false)}
          onPreview={previewRolloutLane}
          onCreate={(values) => void handleCreate(values)}
        />
      ) : null}

      {startLane ? (
        <StartRolloutLaneModal
          open
          lane={startLane}
          files={files}
          isSubmitting={isMutating}
          error={mutationError}
          onDismiss={() => setStartLane(null)}
          onStart={(values) => void handleStart(values)}
        />
      ) : null}

      {managedMembersLane ? (
        <ManageRolloutLaneMembersModal
          open
          lane={managedMembersLane}
          latestRollout={managedMembersLatestRollout}
          canManage={permissions.canManageChannels}
          isSubmitting={isMutating}
          error={mutationError}
          onDismiss={() => setManageMembersLaneId(null)}
          onListMembers={listRolloutLaneMembers}
          {...(managedMembersLane.topologyEnabled
            ? {
                mode: "model" as const,
                onPreviewModel: previewRolloutLaneModelMembershipChange,
                onUpdateModel: updateRolloutLaneModelMembership,
              }
            : {
                mode: "legacy" as const,
                onPreview: previewRolloutLaneMembershipChange,
                onUpdate: updateRolloutLaneMembership,
              })}
          onUpdated={handleMembershipUpdated}
        />
      ) : null}

      {managedDeclarationsLane ? (
        <ManageRolloutLaneDeclarationsModal
          open
          lane={managedDeclarationsLane}
          files={files}
          isSubmitting={isMutating}
          error={mutationError}
          onDismiss={() => setManageDeclarationsLaneId(null)}
          onPreview={previewRolloutLaneModelDeclaration}
          onCreate={createRolloutLaneModelDeclaration}
          onPublish={publishRolloutLaneModelTarget}
          onUpdated={handleDeclarationUpdated}
        />
      ) : null}

      {laneToDelete ? (
        <DeleteRolloutLaneDialog
          open
          laneLabel={laneToDelete.label}
          isSubmitting={isMutating}
          error={mutationError}
          onDismiss={() => setDeleteLaneId(null)}
          onConfirm={() => void handleDelete()}
        />
      ) : null}

      <ViewRolloutModal
        event={
          modalRollout
            ? mapRolloutToEvent(modalRollout, {
                laneLabel: laneForRollout(lanes, modalRollout.id)?.label ?? "Rollout lane",
              })
            : null
        }
        onDismiss={() => setModalRolloutId(null)}
        canManage={false}
        canControl={permissions.canControl}
        onPause={modalRollout ? () => void runControl(modalRollout, "pause", "Paused by operator") : undefined}
        onResume={modalRollout ? () => void runControl(modalRollout, "resume", "Resumed by operator") : undefined}
        onContinueFromReview={
          modalRollout
            ? (reason) => void runControl(modalRollout, "continue", reason ?? "Continue after manual review")
            : undefined
        }
        onAbort={modalRollout ? () => void runControl(modalRollout, "abort", "Abort new rollout work") : undefined}
        onRevert={
          modalRollout && canRevertRollout(modalRollout)
            ? () => void runControl(modalRollout, "revert", "Restore the captured source release")
            : undefined
        }
        onCompleteWithFailures={
          modalRollout && permissions.canControl && canCompleteWithFailures(modalRollout)
            ? () => void runCompleteWithFailures(modalRollout)
            : undefined
        }
      />
    </div>
  );
}
