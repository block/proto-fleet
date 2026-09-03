import { useMemo, useState } from "react";
import { timestampMs } from "@bufbuild/protobuf/wkt";

import ActiveUpdateBanners from "./ActiveUpdateBanners";
import RolloutDetailModal from "./RolloutDetailModal";
import { isActive } from "./rolloutStatus";
import type { Rollout } from "@/protoFleet/api/generated/rollout/v1/rollout_pb";
import type { ReleaseChannelsApi } from "@/protoFleet/api/useReleaseChannels";
import { Alert } from "@/shared/assets/icons";
import { variants } from "@/shared/components/Button";
import Dialog, { DialogIcon } from "@/shared/components/Dialog";
import { pushToast, STATUSES } from "@/shared/features/toaster";

// Something another surface asked the monitor to do: open a rollout's
// detail, or confirm rolling back to one.
export type MonitorRequest = { kind: "view" | "rollback"; rolloutId: bigint };

interface ActiveUpdatesMonitorProps {
  api: Pick<
    ReleaseChannelsApi,
    | "rollouts"
    | "minerNames"
    | "continueRollout"
    | "pauseRollout"
    | "resumeRollout"
    | "cancelRollout"
    | "rollbackFirmware"
    | "retryFailedDevices"
  >;
  // Drills into the release channel behind a rollout.
  onManageChannel: (channelId: bigint) => void;
  // From another surface (e.g. the history modal); cleared via onRequestHandled.
  request?: MonitorRequest | null;
  onRequestHandled?: () => void;
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
}: ActiveUpdatesMonitorProps) => {
  const {
    rollouts,
    minerNames,
    continueRollout,
    pauseRollout,
    resumeRollout,
    cancelRollout,
    rollbackFirmware,
    retryFailedDevices,
  } = api;
  // Rollout open in the update detail modal, resolved live on each poll.
  const [viewUpdateId, setViewUpdateId] = useState<bigint | null>(null);
  const [cancelTarget, setCancelTarget] = useState<Rollout | null>(null);
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
  const viewedRollout = byId(request?.kind === "view" ? request.rolloutId : viewUpdateId);
  const rollbackTarget = request?.kind === "rollback" ? (byId(request.rolloutId) ?? null) : localRollbackTarget;
  const setRollbackTarget = (target: Rollout | null) => {
    setLocalRollbackTarget(target);
    if (target === null && request?.kind === "rollback") onRequestHandled?.();
  };
  const closeDetail = () => {
    setViewUpdateId(null);
    if (request?.kind === "view") onRequestHandled?.();
  };

  const handleContinue = (rollout: Rollout) =>
    continueRollout(rollout.id)
      .then(() => {
        pushToast({
          message: `Continuing ${rollout.model} update in ${rollout.channelName}`,
          status: STATUSES.success,
        });
      })
      .catch((error) => {
        pushToast({ message: error?.message || "Couldn't continue the update", status: STATUSES.error });
      });

  const togglePause = (rollout: Rollout, pause: boolean) =>
    (pause ? pauseRollout(rollout.id) : resumeRollout(rollout.id))
      .then(() => {
        pushToast({
          message: `${pause ? "Paused" : "Resumed"} ${rollout.model} update in ${rollout.channelName}`,
          status: STATUSES.success,
        });
      })
      .catch((error) => {
        pushToast({
          message: error?.message || `Couldn't ${pause ? "pause" : "resume"} the update`,
          status: STATUSES.error,
        });
      });

  const handleRetry = (rollout: Rollout) =>
    retryFailedDevices(rollout.id)
      .then((next) => {
        if (next && next.id !== rollout.id) setViewUpdateId(next.id);
        pushToast({ message: `Retrying failed miners in ${rollout.channelName}`, status: STATUSES.success });
      })
      .catch((error) => {
        pushToast({ message: error?.message || "Couldn't retry the failed miners", status: STATUSES.error });
      });

  const handleCancel = () => {
    if (!cancelTarget) return;
    const rollout = cancelTarget;
    setIsBusy(true);
    cancelRollout(rollout.id)
      .then(() => {
        setCancelTarget(null);
        pushToast({
          message: `Canceled the remaining ${rollout.model} updates in ${rollout.channelName}`,
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
    setIsBusy(true);
    rollbackFirmware(rollout.id)
      .then((started) => {
        setRollbackTarget(null);
        closeDetail();
        if (started[0]) setViewUpdateId(started[0].id);
        pushToast({
          message: `Rolling ${rollout.model} in ${rollout.channelName} back to ${rollout.previousFirmwareVersion}`,
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
      <ActiveUpdateBanners rollouts={activeRollouts} onViewUpdate={(rollout) => setViewUpdateId(rollout.id)} />

      {viewedRollout ? (
        <RolloutDetailModal
          rollout={viewedRollout}
          minerNames={minerNames}
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
            ? `The ${cancelTarget.model} update in ${cancelTarget.channelName} stops now. Miners already on ${cancelTarget.firmwareVersion} keep it; miners not yet updated stay on their current version and are not retried until you retry them or change the assignment.`
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
        title="Roll back firmware?"
        subtitle={
          rollbackTarget
            ? `${rollbackTarget.model} in ${rollbackTarget.channelName} goes back to ${rollbackTarget.previousFirmwareVersion}. Any in-progress update for this model is canceled and a new update restores that version on every miner not running it.`
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
            text: "Roll back",
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
