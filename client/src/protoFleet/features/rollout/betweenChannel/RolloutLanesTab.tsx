import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useSearchParams } from "react-router-dom";
import type { Timestamp } from "@bufbuild/protobuf/wkt";

import { isAbortError, toError } from "@/protoFleet/api/requestErrors";
import { ROLLOUT_CHANGED_EVENT } from "@/protoFleet/api/rolloutEvents";
import { mapRolloutToEvent } from "@/protoFleet/api/rolloutMappers";
import { type FirmwareFileInfo, useFirmwareApi } from "@/protoFleet/api/useFirmwareApi";
import { useRolloutApi } from "@/protoFleet/api/useRolloutApi";
import BetweenChannelRolloutStatus from "@/protoFleet/features/rollout/betweenChannel/BetweenChannelRolloutStatus";
import {
  canCompleteWithFailures,
  canRevertRollout,
  hasActiveInitialEnforcement,
  isInitialFirmwareReady,
  shouldMonitorRollout,
} from "@/protoFleet/features/rollout/betweenChannel/betweenChannelUtils";
import CreateRolloutLaneModal, {
  type CreateRolloutLaneValues,
} from "@/protoFleet/features/rollout/betweenChannel/CreateRolloutLaneModal";
import InitialLaneFirmwareSetup from "@/protoFleet/features/rollout/betweenChannel/InitialLaneFirmwareSetup";
import RolloutLanesTable, { type LaneTableRow } from "@/protoFleet/features/rollout/betweenChannel/RolloutLanesTable";
import StartRolloutLaneModal, {
  type StartRolloutLaneValues,
} from "@/protoFleet/features/rollout/betweenChannel/StartRolloutLaneModal";
import type { RolloutLane, RolloutRecord } from "@/protoFleet/features/rollout/rolloutTypes";
import ViewRolloutModal from "@/protoFleet/features/rollout/ViewRolloutModal";
import { Alert, Info } from "@/shared/assets/icons";
import Button, { sizes, variants } from "@/shared/components/Button";
import Callout, { intents } from "@/shared/components/Callout";
import ProgressCircular from "@/shared/components/ProgressCircular";
import { pushToast, STATUSES } from "@/shared/features/toaster";

function idempotencyKey(action: string): string {
  const unique =
    typeof crypto !== "undefined" && typeof crypto.randomUUID === "function"
      ? crypto.randomUUID()
      : `${Math.random().toString(36).slice(2)}-${Date.now().toString(36)}`;
  return `${action}-${unique}`;
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

function laneForRollout(lanes: RolloutLane[], rolloutId: string): RolloutLane | undefined {
  return lanes.find((lane) => lane.channels.some((channel) => channel.rolloutId === rolloutId));
}

function latestInitialEnforcementMemberUpdate(lane: RolloutLane | undefined): Timestamp | undefined {
  return lane?.initialEnforcement.members.reduce<Timestamp | undefined>(
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

export default function RolloutLanesTab() {
  const [searchParams, setSearchParams] = useSearchParams();
  const {
    lane: loadedLane,
    lanes,
    rollouts,
    isMutating,
    loadError,
    mutationError,
    permissions,
    listRolloutLanes,
    getRolloutLane,
    previewRolloutLane,
    createRolloutLane,
    startRolloutLane,
    listRollouts,
    getRollout,
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
  const [startLane, setStartLane] = useState<RolloutLane | null>(null);
  const [isPreparingLane, setIsPreparingLane] = useState(false);
  const [focusedRolloutId, setFocusedRolloutId] = useState<string | null>(null);
  const [modalRolloutId, setModalRolloutId] = useState<string | null>(null);
  const refreshControllerRef = useRef<AbortController | null>(null);
  const prepareStartControllerRef = useRef<AbortController | null>(null);
  const skipSetupHydrationLaneIdRef = useRef<string | null>(null);
  const setupLaneRef = useRef<RolloutLane | undefined>(undefined);
  const setupLaneId = searchParams.get("setupLane");

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

  const loadData = useCallback(async () => {
    if (!permissions.canReadChannels) {
      return;
    }
    setPageError(null);
    setIsLoadingFiles(permissions.canManageChannels);
    try {
      const requests: Promise<unknown>[] = [listRolloutLanes()];
      if (permissions.canRead) {
        requests.push(listRollouts());
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
    listRollouts,
    permissions.canManageChannels,
    permissions.canRead,
    permissions.canReadChannels,
  ]);

  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect -- loads durable lane state from external APIs
    void loadData();
  }, [loadData]);

  useEffect(() => {
    if (!setupLaneId || !permissions.canReadChannels) {
      return;
    }
    if (skipSetupHydrationLaneIdRef.current === setupLaneId) {
      skipSetupHydrationLaneIdRef.current = null;
      return;
    }

    const controller = new AbortController();
    setPageError(null);
    void getRolloutLane({
      laneId: setupLaneId,
      includeDeviceSetMembers: false,
      includeInitialEnforcementMembers: true,
      signal: controller.signal,
    }).catch((error) => {
      if (!isAbortError(error, controller.signal)) {
        setPageError(toError(error, "Initial firmware setup is unavailable.").message);
      }
    });

    return () => {
      controller.abort();
    };
  }, [getRolloutLane, permissions.canReadChannels, setupLaneId]);

  const rows = useMemo<LaneTableRow[]>(() => {
    const rolloutsById = new Map(rollouts.map((rollout) => [rollout.id, rollout]));
    return lanes.map((lane) => ({
      id: lane.id,
      lane,
      latestRollout: latestRolloutForLane(lane, rolloutsById),
    }));
  }, [lanes, rollouts]);
  const setupLane = useMemo(() => {
    if (!setupLaneId) {
      return undefined;
    }
    if (loadedLane?.id === setupLaneId) {
      return loadedLane;
    }
    return lanes.find((lane) => lane.id === setupLaneId);
  }, [lanes, loadedLane, setupLaneId]);
  setupLaneRef.current = setupLane;
  const shouldPollSetupLane = Boolean(setupLane && !isInitialFirmwareReady(setupLane));
  const focusedRollout = focusedRolloutId ? rollouts.find((rollout) => rollout.id === focusedRolloutId) : undefined;
  const modalRollout = modalRolloutId ? rollouts.find((rollout) => rollout.id === modalRolloutId) : undefined;
  const monitoredRolloutIdsKey = rollouts
    .filter((rollout) => shouldMonitorRollout(rollout))
    .map((rollout) => rollout.id)
    .sort()
    .join("\0");
  const monitoredRolloutIds = useMemo(
    () => (monitoredRolloutIdsKey ? monitoredRolloutIdsKey.split("\0") : []),
    [monitoredRolloutIdsKey],
  );
  const hasMonitoredInitialEnforcement = lanes.some(hasActiveInitialEnforcement);
  const monitoredRollout =
    focusedRollout ??
    rows.find((row) => shouldMonitorRollout(row.latestRollout))?.latestRollout ??
    rows.find((row) => row.latestRollout && canRevertRollout(row.latestRollout))?.latestRollout;
  const monitoredLane = monitoredRollout ? laneForRollout(lanes, monitoredRollout.id) : undefined;
  const canCreateLane = permissions.canManageChannels;
  const canStartLane = permissions.canManageChannels && permissions.canManage;

  const refreshMonitoredRollouts = useCallback(async () => {
    if (!permissions.canReadChannels || refreshControllerRef.current) {
      return;
    }
    const controller = new AbortController();
    refreshControllerRef.current = controller;
    const shouldRefreshLaneList = !shouldPollSetupLane || monitoredRolloutIds.length > 0;
    try {
      await Promise.allSettled([
        ...(shouldRefreshLaneList ? [listRolloutLanes({ signal: controller.signal })] : []),
        ...(setupLaneId && shouldPollSetupLane
          ? [
              getRolloutLane({
                laneId: setupLaneId,
                includeDeviceSetMembers: false,
                includeInitialEnforcementMembers: true,
                initialEnforcementMembersUpdatedAfter: latestInitialEnforcementMemberUpdate(setupLaneRef.current),
                signal: controller.signal,
              }),
            ]
          : []),
        ...(permissions.canRead
          ? monitoredRolloutIds.map((rolloutId) => getRollout({ rolloutId, signal: controller.signal }))
          : []),
      ]);
    } finally {
      if (refreshControllerRef.current === controller) {
        refreshControllerRef.current = null;
      }
    }
  }, [
    getRollout,
    getRolloutLane,
    listRolloutLanes,
    monitoredRolloutIds,
    permissions.canRead,
    permissions.canReadChannels,
    setupLaneId,
    shouldPollSetupLane,
  ]);

  useEffect(() => {
    const refresh = () => void refreshMonitoredRollouts();
    window.addEventListener(ROLLOUT_CHANGED_EVENT, refresh);
    const interval =
      monitoredRolloutIds.length > 0 || hasMonitoredInitialEnforcement || shouldPollSetupLane
        ? window.setInterval(refresh, 5000)
        : undefined;
    return () => {
      window.removeEventListener(ROLLOUT_CHANGED_EVENT, refresh);
      if (interval !== undefined) {
        window.clearInterval(interval);
      }
    };
  }, [hasMonitoredInitialEnforcement, monitoredRolloutIds.length, refreshMonitoredRollouts, shouldPollSetupLane]);

  const handleCreate = useCallback(
    async (values: CreateRolloutLaneValues) => {
      try {
        const createdLane = await createRolloutLane({
          ...values,
          idempotencyKey: idempotencyKey("create-lane"),
        });
        setShowCreate(false);
        if (createdLane.initialEnforcement.members.length >= createdLane.initialEnforcement.totalCount) {
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
          includeInitialEnforcementMembers: false,
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

  useEffect(
    () => () => {
      refreshControllerRef.current?.abort();
      refreshControllerRef.current = null;
      prepareStartControllerRef.current?.abort();
      prepareStartControllerRef.current = null;
    },
    [],
  );

  const handleStart = useCallback(
    async (values: StartRolloutLaneValues) => {
      try {
        const result = await startRolloutLane({
          ...values,
          idempotencyKey: idempotencyKey("start-rollout"),
        });
        setStartLane(null);
        updateSetupLaneParam(null);
        setFocusedRolloutId(result.rollout.id);
        const firstBatch = result.rollout.batches[0];
        if (firstBatch) {
          try {
            const admitted = await admitRollout({
              rolloutId: result.rollout.id,
              batchId: firstBatch.id,
              expectedRevision: result.rollout.revision,
              idempotencyKey: idempotencyKey("admit-first-batch"),
              reason: "Start first manual batch",
            });
            setFocusedRolloutId(admitted.id);
          } catch (error) {
            pushToast({
              message:
                error instanceof Error
                  ? `Rollout was created but the first batch did not start: ${error.message}`
                  : "Rollout was created but the first batch did not start.",
              status: STATUSES.error,
            });
          }
        }
        pushToast({ message: `Started ${values.name}`, status: STATUSES.success });
      } catch {
        // mutationError is rendered in the open modal.
      }
    },
    [admitRollout, startRolloutLane, updateSetupLaneParam],
  );

  const runControl = useCallback(
    async (rollout: RolloutRecord, action: "continue" | "pause" | "resume" | "abort" | "revert", reason: string) => {
      const input = {
        rolloutId: rollout.id,
        expectedRevision: rollout.revision,
        idempotencyKey: idempotencyKey(action),
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
          idempotencyKey: idempotencyKey("complete-with-failures"),
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
        {canCreateLane ? (
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
          buttonOnClick={() => void loadData()}
        />
      ) : null}

      {isInitialLoading || isLoadingFiles ? (
        <div className="flex items-center justify-center gap-3 py-10 text-300 text-text-primary-70">
          <ProgressCircular indeterminate className="text-core-primary-fill" />
          Loading rollout lanes...
        </div>
      ) : (
        <RolloutLanesTable
          rows={rows}
          canStart={canStartLane}
          isPreparingStart={isPreparingLane}
          onSetup={(lane) => void openSetup(lane)}
          onStart={(lane) => void prepareStart(lane)}
          onView={(rollout) => setModalRolloutId(rollout.id)}
        />
      )}

      {setupLane ? (
        <InitialLaneFirmwareSetup
          lane={setupLane}
          canStart={canStartLane}
          onClose={() => updateSetupLaneParam(null)}
          onStart={() => void prepareStart(setupLane)}
        />
      ) : null}

      {monitoredRollout && monitoredLane ? (
        <BetweenChannelRolloutStatus
          rollout={monitoredRollout}
          laneLabel={monitoredLane.label}
          canControl={permissions.canControl}
          isMutating={isMutating}
          onPause={() => void runControl(monitoredRollout, "pause", "Paused by operator")}
          onResume={() => void runControl(monitoredRollout, "resume", "Resumed by operator")}
          onContinue={() => void runControl(monitoredRollout, "continue", "Continue after manual review")}
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
          modalRollout ? () => void runControl(modalRollout, "continue", "Continue after manual review") : undefined
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
