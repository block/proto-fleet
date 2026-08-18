import { useCallback, useEffect, useMemo, useState } from "react";

import { ROLLOUT_CHANGED_EVENT } from "@/protoFleet/api/rolloutEvents";
import { mapRolloutToEvent } from "@/protoFleet/api/rolloutMappers";
import { type FirmwareFileInfo, useFirmwareApi } from "@/protoFleet/api/useFirmwareApi";
import { useRolloutApi } from "@/protoFleet/api/useRolloutApi";
import BetweenChannelRolloutStatus from "@/protoFleet/features/rollout/betweenChannel/BetweenChannelRolloutStatus";
import { isActiveRollout } from "@/protoFleet/features/rollout/betweenChannel/betweenChannelUtils";
import CreateRolloutLaneModal, {
  type CreateRolloutLaneValues,
} from "@/protoFleet/features/rollout/betweenChannel/CreateRolloutLaneModal";
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

export default function RolloutLanesTab() {
  const {
    lanes,
    rollouts,
    isLoading,
    isMutating,
    loadError,
    mutationError,
    permissions,
    listRolloutLanes,
    getRolloutLane,
    createRolloutLane,
    startRolloutLane,
    listRollouts,
    admitRollout,
    continueRollout,
    pauseRollout,
    resumeRollout,
    abortRollout,
    revertRollout,
  } = useRolloutApi();
  const { listFirmwareFiles } = useFirmwareApi();
  const [files, setFiles] = useState<FirmwareFileInfo[]>([]);
  const [pageError, setPageError] = useState<string | null>(null);
  const [isLoadingFiles, setIsLoadingFiles] = useState(false);
  const [showCreate, setShowCreate] = useState(false);
  const [startLane, setStartLane] = useState<RolloutLane | null>(null);
  const [isPreparingLane, setIsPreparingLane] = useState(false);
  const [focusedRolloutId, setFocusedRolloutId] = useState<string | null>(null);
  const [modalRolloutId, setModalRolloutId] = useState<string | null>(null);

  const loadData = useCallback(async () => {
    if (!permissions.canReadChannels) {
      return;
    }
    setPageError(null);
    setIsLoadingFiles(true);
    try {
      const requests: Promise<unknown>[] = [listRolloutLanes()];
      if (permissions.canRead) {
        requests.push(listRollouts());
      }
      if (permissions.canManageChannels || permissions.canManage) {
        requests.push(listFirmwareFiles().then(setFiles));
      }
      await Promise.all(requests);
    } catch (error) {
      setPageError(error instanceof Error ? error.message : "Failed to load firmware rollout lanes.");
    } finally {
      setIsLoadingFiles(false);
    }
  }, [
    listFirmwareFiles,
    listRolloutLanes,
    listRollouts,
    permissions.canManage,
    permissions.canManageChannels,
    permissions.canRead,
    permissions.canReadChannels,
  ]);

  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect -- loads durable lane state from external APIs
    void loadData();
  }, [loadData]);

  const rows = useMemo<LaneTableRow[]>(() => {
    const rolloutsById = new Map(rollouts.map((rollout) => [rollout.id, rollout]));
    return lanes.map((lane) => ({
      id: lane.id,
      lane,
      latestRollout: latestRolloutForLane(lane, rolloutsById),
    }));
  }, [lanes, rollouts]);
  const focusedRollout = focusedRolloutId ? rollouts.find((rollout) => rollout.id === focusedRolloutId) : undefined;
  const modalRollout = modalRolloutId ? rollouts.find((rollout) => rollout.id === modalRolloutId) : undefined;
  const monitoredRollout =
    focusedRollout ??
    rows.find((row) => isActiveRollout(row.latestRollout))?.latestRollout ??
    rows.find((row) => row.latestRollout?.availableActions.revert)?.latestRollout;
  const monitoredLane = monitoredRollout ? laneForRollout(lanes, monitoredRollout.id) : undefined;
  const canCreateLane = permissions.canManageChannels;
  const canStartLane = permissions.canManageChannels && permissions.canManage;

  const refreshRollouts = useCallback(async () => {
    if (!permissions.canRead) {
      return;
    }
    try {
      await listRollouts();
    } catch {
      // The hook exposes loadError and routes auth failures. Keep the current
      // durable record visible instead of clearing the whole tab on refresh.
    }
  }, [listRollouts, permissions.canRead]);

  useEffect(() => {
    const refresh = () => void refreshRollouts();
    window.addEventListener(ROLLOUT_CHANGED_EVENT, refresh);
    const hasActiveRollout = rows.some((row) => isActiveRollout(row.latestRollout));
    const interval = hasActiveRollout ? window.setInterval(refresh, 5000) : undefined;
    return () => {
      window.removeEventListener(ROLLOUT_CHANGED_EVENT, refresh);
      if (interval !== undefined) {
        window.clearInterval(interval);
      }
    };
  }, [refreshRollouts, rows]);

  const handleCreate = useCallback(
    async (values: CreateRolloutLaneValues) => {
      try {
        await createRolloutLane({
          ...values,
          idempotencyKey: idempotencyKey("create-lane"),
        });
        setShowCreate(false);
        pushToast({ message: `Created ${values.label}`, status: STATUSES.success });
      } catch {
        // mutationError is rendered in the open modal.
      }
    },
    [createRolloutLane],
  );

  const prepareStart = useCallback(
    async (lane: RolloutLane) => {
      setIsPreparingLane(true);
      setPageError(null);
      try {
        const freshLane = await getRolloutLane({ laneId: lane.id });
        setStartLane(freshLane);
      } catch (error) {
        setPageError(error instanceof Error ? error.message : "Failed to load fresh lane membership.");
      } finally {
        setIsPreparingLane(false);
      }
    },
    [getRolloutLane],
  );

  const handleStart = useCallback(
    async (values: StartRolloutLaneValues) => {
      try {
        const result = await startRolloutLane({
          ...values,
          idempotencyKey: idempotencyKey("start-rollout"),
        });
        setStartLane(null);
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
    [admitRollout, startRolloutLane],
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

      {isLoading || isLoadingFiles || isPreparingLane ? (
        <div className="flex items-center justify-center gap-3 py-10 text-300 text-text-primary-70">
          <ProgressCircular indeterminate className="text-core-primary-fill" />
          {isPreparingLane ? "Refreshing lane membership..." : "Loading rollout lanes..."}
        </div>
      ) : (
        <RolloutLanesTable
          rows={rows}
          canStart={canStartLane}
          onStart={(lane) => void prepareStart(lane)}
          onView={(rollout) => setModalRolloutId(rollout.id)}
        />
      )}

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
        />
      ) : null}

      {showCreate ? (
        <CreateRolloutLaneModal
          open
          files={files}
          isSubmitting={isMutating}
          error={mutationError}
          onDismiss={() => setShowCreate(false)}
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
      />
    </div>
  );
}
