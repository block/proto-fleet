import { type ReactNode, useLayoutEffect, useMemo, useRef, useState } from "react";
import { timestampMs } from "@bufbuild/protobuf/wkt";

import ActiveUpdateBanners from "./ActiveUpdateBanners";
import RolloutDetailModal from "./RolloutDetailModal";
import { canRetryRemaining, isActive, pairGeneration, pairLabel } from "./rolloutStatus";
import type { Rollout } from "@/protoFleet/api/generated/rollout/v1/rollout_pb";
import type { ReleaseChannelsApi } from "@/protoFleet/api/useReleaseChannels";
import { Alert } from "@/shared/assets/icons";
import { variants } from "@/shared/components/Button";
import Dialog, { DialogIcon } from "@/shared/components/Dialog";
import { pushToast, STATUSES } from "@/shared/features/toaster";

// Something another surface asked the monitor to do: open a rollout's
// detail, or confirm rolling back to one.
export type MonitorRequest = { kind: "view"; rollout: Rollout } | { kind: "rollback"; rollout: Rollout };

interface ActiveUpdatesMonitorProps {
  api: Pick<
    ReleaseChannelsApi,
    | "channels"
    | "rollouts"
    | "minerNames"
    | "continueRollout"
    | "pauseRollout"
    | "resumeRollout"
    | "cancelRollout"
    | "rollbackFirmware"
    | "retryFailedDevices"
    | "listRolloutDevices"
  >;
  // Drills into the release channel behind a rollout.
  onManageChannel: (channelId: bigint) => void;
  // From another surface (e.g. the history modal); cleared via onRequestHandled.
  request?: MonitorRequest | null;
  onRequestHandled?: () => void;
  refreshWarning?: ReactNode;
}

// Everything about ongoing firmware updates that lives above the firmware
// page tabs: the banner stack, the full-screen update detail it opens, and
// the lifecycle actions (continue, pause, resume, retry failed, cancel
// remaining and roll back, the last two with confirmation).
const ActiveUpdatesMonitor = ({
  api,
  onManageChannel,
  request = null,
  onRequestHandled,
  refreshWarning,
}: ActiveUpdatesMonitorProps) => {
  const {
    rollouts,
    minerNames,
    continueRollout,
    pauseRollout,
    resumeRollout,
    cancelRollout,
    rollbackFirmware,
    listRolloutDevices,
    retryFailedDevices,
  } = api;
  // Retain the opened or returned snapshot if a subsequent poll fails.
  const [viewUpdate, setViewUpdate] = useState<Rollout | null>(null);
  const [localCancelTarget, setLocalCancelTarget] = useState<Rollout | null>(null);
  const [localRollbackTarget, setLocalRollbackTarget] = useState<Rollout | null>(null);
  const [isBusy, setIsBusy] = useState(false);

  // Most recently started first.
  const activeRollouts = useMemo(
    () =>
      rollouts
        .filter(isActive)
        .sort((a, b) => (b.createdAt ? timestampMs(b.createdAt) : 0) - (a.createdAt ? timestampMs(a.createdAt) : 0)),
    [rollouts],
  );
  const byId = (id: bigint | null | undefined) =>
    id != null ? rollouts.find((rollout) => rollout.id === id) : undefined;
  const availableSnapshot = (rollout: Rollout | null) =>
    rollout && api.channels.some((channel) => channel.id === rollout.channelId) ? rollout : null;
  // A deleted channel invalidates its requests before they can take
  // precedence over a selection from a surviving channel.
  const validRequest = request && availableSnapshot(request.rollout) ? request : null;
  // History and successful actions can supply rows absent from the current
  // poll. Prefer equally recent or newer live rows without losing that detail.
  const selectedRollout = validRequest?.kind === "view" ? validRequest.rollout : viewUpdate;
  const selectedSnapshot = availableSnapshot(selectedRollout);
  const currentRollout = byId(selectedSnapshot?.id);
  const viewedRollout =
    selectedSnapshot && (!currentRollout || selectedSnapshot.revision > currentRollout.revision)
      ? selectedSnapshot
      : currentRollout;
  // Keep the snapshot that opened confirmation, even when polling advances it.
  const cancelTarget = availableSnapshot(localCancelTarget);
  const rollbackTarget = availableSnapshot(
    validRequest?.kind === "rollback" ? validRequest.rollout : localRollbackTarget,
  );
  // Mutation results may arrive after the operator closes, reopens or changes
  // the selection. Track that intent separately from revisions updated by polls.
  const selectionEpoch = useRef(0);
  useLayoutEffect(() => {
    selectionEpoch.current += 1;
    return () => {
      selectionEpoch.current += 1;
    };
  }, [validRequest, selectedSnapshot, cancelTarget, rollbackTarget]);
  const selectionChanged = () => {
    selectionEpoch.current += 1;
  };
  const clearsAssignment = rollbackTarget?.previousFirmwareVersion === "";
  const setCancelTarget = (target: Rollout | null) => {
    selectionChanged();
    setLocalCancelTarget(target);
  };
  const setRollbackTarget = (target: Rollout | null) => {
    selectionChanged();
    setLocalRollbackTarget(target);
    if (target === null && request?.kind === "rollback") onRequestHandled?.();
  };
  const closeDetail = () => {
    selectionChanged();
    setViewUpdate(null);
    if (request?.kind === "view") onRequestHandled?.();
  };

  const handleContinue = (rollout: Rollout) =>
    continueRollout(rollout.id, rollout.revision)
      .then(() => {
        pushToast({
          message: `Continuing ${pairLabel(rollout)} update in ${rollout.channelName}`,
          status: STATUSES.success,
        });
      })
      .catch((error) => {
        pushToast({ message: error?.message || "Couldn't continue the update", status: STATUSES.error });
      });

  const togglePause = (rollout: Rollout, pause: boolean) =>
    (pause ? pauseRollout(rollout.id, rollout.revision) : resumeRollout(rollout.id, rollout.revision))
      .then(() => {
        pushToast({
          message: `${pause ? "Paused" : "Resumed"} ${pairLabel(rollout)} update in ${rollout.channelName}`,
          status: STATUSES.success,
        });
      })
      .catch((error) => {
        pushToast({
          message: error?.message || `Couldn't ${pause ? "pause" : "resume"} the update`,
          status: STATUSES.error,
        });
      });

  const handleRetry = (rollout: Rollout) => {
    const startedAtSelection = selectionEpoch.current;
    return retryFailedDevices(rollout.id, rollout.revision)
      .then((next) => {
        if (next && next.id !== rollout.id && selectionEpoch.current === startedAtSelection) {
          selectionChanged();
          if (request?.kind === "view") onRequestHandled?.();
          setViewUpdate(next);
        }
        pushToast({
          message: `Retry requested for remaining ${pairLabel(rollout)} miners in ${rollout.channelName}`,
          status: STATUSES.success,
        });
      })
      .catch((error) => {
        pushToast({ message: error?.message || "Couldn't retry the remaining miners", status: STATUSES.error });
      });
  };

  const handleCancel = () => {
    if (!cancelTarget) return;
    const rollout = cancelTarget;
    const startedAtSelection = selectionEpoch.current;
    setIsBusy(true);
    cancelRollout(rollout.id, rollout.revision)
      .then(() => {
        if (selectionEpoch.current === startedAtSelection) setCancelTarget(null);
        pushToast({
          message: `Canceled the remaining ${pairLabel(rollout)} updates in ${rollout.channelName}`,
          status: STATUSES.success,
        });
      })
      .catch((error) => {
        pushToast({ message: error?.message || "Couldn't cancel the update", status: STATUSES.error });
      })
      .finally(() => setIsBusy(false));
  };

  const handleRollback = () => {
    if (!rollbackTarget) return;
    const rollout = rollbackTarget;
    const startedAtSelection = selectionEpoch.current;
    setIsBusy(true);
    rollbackFirmware(rollout.id, rollout.revision)
      .then((started) => {
        if (selectionEpoch.current === startedAtSelection) {
          setRollbackTarget(null);
          closeDetail();
          if (started[0]) setViewUpdate(started[0]);
        }
        pushToast({
          message: rollout.previousFirmwareVersion
            ? `Rolling ${pairLabel(rollout)} in ${rollout.channelName} back to ${rollout.previousFirmwareVersion}`
            : `Cleared the firmware assignment for ${pairLabel(rollout)} in ${rollout.channelName}`,
          status: STATUSES.success,
        });
      })
      .catch((error) => {
        pushToast({ message: error?.message || "Couldn't roll back the firmware", status: STATUSES.error });
      })
      .finally(() => setIsBusy(false));
  };

  return (
    <>
      <ActiveUpdateBanners
        rollouts={activeRollouts}
        onViewUpdate={(rollout) => {
          selectionChanged();
          if (request) onRequestHandled?.();
          setViewUpdate(rollout);
        }}
      />

      {viewedRollout ? (
        <RolloutDetailModal
          key={viewedRollout.id.toString()}
          rollout={viewedRollout}
          refreshWarning={refreshWarning}
          currentGeneration={pairGeneration(api.channels, viewedRollout)}
          canRetryRemaining={canRetryRemaining(viewedRollout, api.channels, rollouts)}
          minerNames={minerNames}
          listRolloutDevices={listRolloutDevices}
          onClose={closeDetail}
          onContinue={handleContinue}
          onPause={(rollout) => togglePause(rollout, true)}
          onResume={(rollout) => togglePause(rollout, false)}
          onCancel={setCancelTarget}
          onRollback={setRollbackTarget}
          onRetryFailed={handleRetry}
          onManage={(rollout) => {
            closeDetail();
            onManageChannel(rollout.channelId);
          }}
        />
      ) : null}

      <Dialog
        open={cancelTarget !== null}
        title="Cancel the remaining updates?"
        subtitle={
          cancelTarget
            ? `Cancel the remaining ${pairLabel(cancelTarget)} updates in ${cancelTarget.channelName}. Miners already updated keep ${cancelTarget.firmwareVersion}. No new update commands will be sent, but commands already sent may still finish. Canceled work is not retried until you retry it or change the assignment.`
            : ""
        }
        testId="cancel-rollout-dialog"
        onDismiss={() => {
          if (!isBusy) setCancelTarget(null);
        }}
        icon={
          <DialogIcon intent="critical">
            <Alert />
          </DialogIcon>
        }
        buttons={[
          {
            text: "Keep updating",
            variant: variants.secondary,
            onClick: () => setCancelTarget(null),
            disabled: isBusy,
          },
          {
            text: "Cancel remaining",
            variant: variants.danger,
            onClick: handleCancel,
            loading: isBusy,
          },
        ]}
      />

      <Dialog
        open={rollbackTarget !== null}
        title={clearsAssignment ? "Clear the firmware assignment?" : "Roll back firmware?"}
        subtitle={
          rollbackTarget
            ? clearsAssignment
              ? `The firmware assignment for ${pairLabel(rollbackTarget)} in ${rollbackTarget.channelName} will be cleared, canceling remaining update work. No firmware version will be enforced and no rollback update will start. Miners keep their installed firmware; update commands already sent may still finish.`
              : `${pairLabel(rollbackTarget)} in ${rollbackTarget.channelName} goes back to ${rollbackTarget.previousFirmwareVersion}. Any in-progress update for this manufacturer and model is canceled and a new update restores that version on every miner not running it.`
            : ""
        }
        testId="rollback-firmware-dialog"
        onDismiss={() => {
          if (!isBusy) setRollbackTarget(null);
        }}
        buttons={[
          {
            text: "Cancel",
            variant: variants.secondary,
            onClick: () => setRollbackTarget(null),
            disabled: isBusy,
          },
          {
            text: clearsAssignment ? "Clear assignment" : "Roll back",
            variant: variants.primary,
            onClick: handleRollback,
            loading: isBusy,
          },
        ]}
      />
    </>
  );
};

export default ActiveUpdatesMonitor;
