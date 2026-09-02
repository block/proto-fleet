import { useMemo, useState } from "react";
import { timestampMs } from "@bufbuild/protobuf/wkt";

import ActiveUpdateBanners from "./ActiveUpdateBanners";
import RolloutDetailModal from "./RolloutDetailModal";
import { type Rollout, RolloutStatus } from "@/protoFleet/api/generated/rollout/v1/rollout_pb";
import type { RolloutLanesApi } from "@/protoFleet/api/useRolloutLanes";
import { Alert } from "@/shared/assets/icons";
import { variants } from "@/shared/components/Button";
import Dialog, { DialogIcon } from "@/shared/components/Dialog";
import { pushToast, STATUSES } from "@/shared/features/toaster";

interface ActiveUpdatesMonitorProps {
  api: Pick<
    RolloutLanesApi,
    "rollouts" | "minerNames" | "continueRollout" | "pauseRollout" | "resumeRollout" | "abortRollout"
  >;
  // Drills into the release channel behind a rollout.
  onManageChannel: (laneId: bigint) => void;
}

// Everything about ongoing firmware updates that lives above the firmware
// page tabs: the banner stack, the full-screen update detail it opens, and
// the lifecycle actions (continue, pause, resume, abort with confirmation).
const ActiveUpdatesMonitor = ({ api, onManageChannel }: ActiveUpdatesMonitorProps) => {
  const { rollouts, minerNames, continueRollout, pauseRollout, resumeRollout, abortRollout } = api;
  // Rollout open in the update detail modal, resolved live on each poll.
  const [viewUpdateId, setViewUpdateId] = useState<bigint | null>(null);
  const [abortTarget, setAbortTarget] = useState<Rollout | null>(null);
  const [isAborting, setIsAborting] = useState(false);

  // Most recently started first.
  const activeRollouts = useMemo(
    () =>
      rollouts
        .filter((rollout) => rollout.status === RolloutStatus.ACTIVE)
        .sort((a, b) => (b.createdAt ? timestampMs(b.createdAt) : 0) - (a.createdAt ? timestampMs(a.createdAt) : 0)),
    [rollouts],
  );
  const viewedRollout = viewUpdateId !== null ? rollouts.find((rollout) => rollout.id === viewUpdateId) : undefined;

  const handleContinue = (rollout: Rollout) =>
    continueRollout(rollout.id)
      .then(() => {
        pushToast({ message: `Continuing ${rollout.model} update in ${rollout.laneName}`, status: STATUSES.success });
      })
      .catch((error) => {
        pushToast({ message: error?.message || "Couldn't continue the update", status: STATUSES.error });
      });

  const togglePause = (rollout: Rollout, pause: boolean) =>
    (pause ? pauseRollout(rollout.id) : resumeRollout(rollout.id))
      .then(() => {
        pushToast({
          message: `${pause ? "Paused" : "Resumed"} ${rollout.model} update in ${rollout.laneName}`,
          status: STATUSES.success,
        });
      })
      .catch((error) => {
        pushToast({
          message: error?.message || `Couldn't ${pause ? "pause" : "resume"} the update`,
          status: STATUSES.error,
        });
      });

  const handleAbort = () => {
    if (!abortTarget) return;
    const rollout = abortTarget;
    setIsAborting(true);
    abortRollout(rollout.id)
      .then((result) => {
        setAbortTarget(null);
        setViewUpdateId(null);
        pushToast({
          message: result.restoredPrevious
            ? `Canceled ${rollout.model} update in ${rollout.laneName}; restoring ${result.previousFirmwareVersion}`
            : `Canceled ${rollout.model} update in ${rollout.laneName}; firmware assignment cleared`,
          status: STATUSES.success,
        });
      })
      .catch((error) => {
        pushToast({ message: error?.message || "Couldn't cancel the update", status: STATUSES.error });
      })
      .finally(() => setIsAborting(false));
  };

  return (
    <>
      <ActiveUpdateBanners rollouts={activeRollouts} onViewUpdate={(rollout) => setViewUpdateId(rollout.id)} />

      {viewedRollout ? (
        <RolloutDetailModal
          rollout={viewedRollout}
          minerNames={minerNames}
          onClose={() => setViewUpdateId(null)}
          onContinue={handleContinue}
          onPause={(rollout) => togglePause(rollout, true)}
          onResume={(rollout) => togglePause(rollout, false)}
          onAbort={setAbortTarget}
          onManage={(rollout) => {
            setViewUpdateId(null);
            onManageChannel(rollout.laneId);
          }}
        />
      ) : null}

      <Dialog
        open={abortTarget !== null}
        title="Cancel the remaining update?"
        subtitle={
          abortTarget
            ? abortTarget.previousFirmwareVersion && abortTarget.previousFirmwareVersion !== abortTarget.firmwareVersion
              ? `The ${abortTarget.model} update in ${abortTarget.laneName} stops now. The previous assignment (${abortTarget.previousFirmwareVersion}) is restored and miners already on ${abortTarget.firmwareVersion} are rolled back to it.`
              : `The ${abortTarget.model} update in ${abortTarget.laneName} stops now. There is no previous assignment to restore, so the model's firmware assignment is cleared; miners keep whatever version they are on.`
            : ""
        }
        testId="abort-rollout-dialog"
        onDismiss={() => {
          if (!isAborting) setAbortTarget(null);
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
            onClick: () => setAbortTarget(null),
            disabled: isAborting,
          },
          {
            text: "Cancel update",
            variant: variants.danger,
            onClick: handleAbort,
            loading: isAborting,
          },
        ]}
      />
    </>
  );
};

export default ActiveUpdatesMonitor;
