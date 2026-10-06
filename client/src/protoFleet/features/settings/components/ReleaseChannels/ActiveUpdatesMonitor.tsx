import { type ReactNode, useLayoutEffect, useMemo, useRef, useState } from "react";
import { timestampMs } from "@bufbuild/protobuf/wkt";

import ActiveUpdateBanners from "./ActiveUpdateBanners";
import RolloutDetailModal from "./RolloutDetailModal";
import RolloutLiveView from "./RolloutLiveView";
import RolloutMinersModal, { type RolloutMinerFilter } from "./RolloutMinersModal";
import { canRetryRemaining, isActive, pairGeneration, pairLabel } from "./rolloutStatus";
import type { Rollout } from "@/protoFleet/api/generated/rollout/v1/rollout_pb";
import {
  acknowledgeRollout,
  isRollbackAcknowledged,
  isRolloutSuperseded,
} from "@/protoFleet/api/rollbackAcknowledgements";
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
    | "acknowledgedRollbacks"
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
// page tabs: a single live card or concurrent banners, full-screen detail, and
// all lifecycle actions, confirmations and miner drill-downs. Dialog ownership
// stays here so switching presentation never discards an open dialog or read.
const ActiveUpdatesMonitor = ({
  api,
  onManageChannel,
  request = null,
  onRequestHandled,
  refreshWarning,
}: ActiveUpdatesMonitorProps) => {
  const {
    rollouts: polledRollouts,
    acknowledgedRollbacks,
    minerNames,
    continueRollout,
    pauseRollout,
    resumeRollout,
    cancelRollout,
    rollbackFirmware,
    listRolloutDevices,
    retryFailedDevices,
  } = api;
  const rollouts = useMemo(
    () =>
      polledRollouts.map((rollout) => acknowledgeRollout(rollout, acknowledgedRollbacks, api.channels, polledRollouts)),
    [polledRollouts, acknowledgedRollbacks, api.channels],
  );
  // Retain the opened or returned snapshot if a subsequent poll fails.
  const [viewUpdate, setViewUpdate] = useState<Rollout | null>(null);
  const [localRetryTarget, setLocalRetryTarget] = useState<Rollout | null>(null);
  const [minersSelection, setMinersSelection] = useState<{ rollout: Rollout; filter: RolloutMinerFilter } | null>(null);
  const [retryingId, setRetryingId] = useState<bigint | null>(null);
  const [localCancelTarget, setLocalCancelTarget] = useState<Rollout | null>(null);
  const [localRollbackTarget, setLocalRollbackTarget] = useState<Rollout | null>(null);
  const [isBusy, setIsBusy] = useState(false);
  const mutationInFlight = useRef(false);
  // The card can become banners (or vice versa) while a request is pending.
  // Keep the mutation lock here so remounting a view cannot dispatch it twice.
  const mutate = async (operation: () => Promise<void>) => {
    if (mutationInFlight.current) return;
    mutationInFlight.current = true;
    setIsBusy(true);
    try {
      await operation();
    } finally {
      mutationInFlight.current = false;
      setIsBusy(false);
    }
  };

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
  const latestSnapshot = (snapshot: Rollout | null) => {
    if (!snapshot) return null;
    const current = byId(snapshot.id);
    return acknowledgeRollout(
      current && current.revision >= snapshot.revision ? current : snapshot,
      acknowledgedRollbacks,
      api.channels,
      polledRollouts,
    );
  };
  const viewedRollout = latestSnapshot(selectedSnapshot);
  const minersRollout = latestSnapshot(availableSnapshot(minersSelection?.rollout ?? null));
  if (minersSelection && !minersRollout) setMinersSelection(null);
  const assignmentInvalidated = (rollout: Rollout) =>
    isRollbackAcknowledged(rollout, acknowledgedRollbacks) ||
    isRolloutSuperseded(rollout, api.channels, polledRollouts);
  // Keep the snapshot that opened confirmation, even when polling advances it.
  const cancelSnapshot = availableSnapshot(localCancelTarget);
  const rollbackSnapshot = availableSnapshot(
    validRequest?.kind === "rollback" ? validRequest.rollout : localRollbackTarget,
  );
  // A confirmed rollback eventually retires its temporary acknowledgment when
  // polling catches up. Retained rollback confirmations must still belong to the
  // current assignment, so clearing that acknowledgment cannot reopen an old dialog.
  const currentConfirmation = (snapshot: Rollout | null) =>
    snapshot &&
    snapshot.assignmentGeneration === pairGeneration(api.channels, snapshot) &&
    !assignmentInvalidated(snapshot)
      ? snapshot
      : null;
  // A returned successor can already be canceled while the channel assignment
  // still lags. Keep its captured revision unless it is known to be inactive.
  const cancelTarget =
    cancelSnapshot && isActive(acknowledgeRollout(cancelSnapshot, acknowledgedRollbacks, api.channels, polledRollouts))
      ? cancelSnapshot
      : null;
  const rollbackTarget = currentConfirmation(rollbackSnapshot);
  const retrySnapshot = availableSnapshot(localRetryTarget);
  const currentRetry = latestSnapshot(retrySnapshot);
  const retryEligible = (rollout: Rollout) =>
    !assignmentInvalidated(rollout) && canRetryRemaining(rollout, api.channels, rollouts);
  // Validate against live eligibility, but send the revision the operator saw
  // when opening confirmation. Invalidated confirmations must not reappear.
  const retryTarget = currentRetry && retryEligible(currentRetry) ? retrySnapshot : null;
  if (localRetryTarget && !retryTarget) setLocalRetryTarget(null);

  // Mutation results may arrive after the operator closes, reopens or changes
  // the selection. Track that intent separately from revisions updated by polls.
  const selectionEpoch = useRef(0);
  useLayoutEffect(() => {
    selectionEpoch.current += 1;
    return () => {
      selectionEpoch.current += 1;
    };
  }, [validRequest, selectedSnapshot, cancelSnapshot, rollbackSnapshot]);
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
    mutate(() =>
      continueRollout(rollout.id, rollout.revision)
        .then(() => {
          pushToast({
            message: `Continuing ${pairLabel(rollout)} update in ${rollout.channelName}`,
            status: STATUSES.success,
          });
        })
        .catch((error) => {
          pushToast({ message: error?.message || "Couldn't continue the update", status: STATUSES.error });
        }),
    );

  const togglePause = (rollout: Rollout, pause: boolean) =>
    mutate(() =>
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
        }),
    );

  const handleRetry = () => {
    if (!retryTarget || mutationInFlight.current) return;
    const rollout = retryTarget;
    setLocalRetryTarget(null);
    const startedAtSelection = selectionEpoch.current;
    return mutate(() => {
      setRetryingId(rollout.id);
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
        })
        .finally(() => setRetryingId(null));
    });
  };

  const handleCancel = () => {
    if (!cancelTarget) return;
    const rollout = cancelTarget;
    const startedAtSelection = selectionEpoch.current;
    return mutate(() =>
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
        }),
    );
  };

  const handleRollback = () => {
    if (!rollbackTarget) return;
    const rollout = rollbackTarget;
    const startedAtSelection = selectionEpoch.current;
    return mutate(() =>
      rollbackFirmware(rollout)
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
        }),
    );
  };

  const openDetail = (rollout: Rollout) => {
    selectionChanged();
    if (request) onRequestHandled?.();
    setViewUpdate(rollout);
  };
  const liveViewProps = (rollout: Rollout) => ({
    rollout,
    currentGeneration: assignmentInvalidated(rollout) ? undefined : pairGeneration(api.channels, rollout),
    canRetryRemaining: retryEligible(rollout),
    actionsDisabled: isBusy,
    isRetrying: retryingId === rollout.id,
    onViewMiners: (target: Rollout, filter: RolloutMinerFilter) => setMinersSelection({ rollout: target, filter }),
    onContinue: handleContinue,
    onPause: (target: Rollout) => togglePause(target, true),
    onResume: (target: Rollout) => togglePause(target, false),
    onCancel: setCancelTarget,
    onRollback: setRollbackTarget,
    onRetryFailed: (target: Rollout) => {
      selectionChanged();
      setLocalRetryTarget(target);
    },
    onManage: (target: Rollout) => {
      closeDetail();
      onManageChannel(target.channelId);
    },
  });
  const singleActiveRollout = activeRollouts.length === 1 && !viewedRollout ? activeRollouts[0] : null;

  return (
    <>
      {singleActiveRollout ? (
        <div data-testid="active-updates-section">
          <div data-testid={`active-update-${singleActiveRollout.id.toString()}`}>
            <RolloutLiveView {...liveViewProps(singleActiveRollout)} presentation="inline" onViewUpdate={openDetail} />
          </div>
        </div>
      ) : null}
      {!singleActiveRollout ? <ActiveUpdateBanners rollouts={activeRollouts} onViewUpdate={openDetail} /> : null}

      {viewedRollout ? (
        <RolloutDetailModal
          key={viewedRollout.id.toString()}
          {...liveViewProps(viewedRollout)}
          refreshWarning={refreshWarning}
          onClose={closeDetail}
        />
      ) : null}

      {minersRollout && minersSelection ? (
        <RolloutMinersModal
          key={`${minersRollout.id}-${minersSelection.filter}`}
          rollout={minersRollout}
          minerNames={minerNames}
          listRolloutDevices={listRolloutDevices}
          initialFilter={minersSelection.filter}
          onClose={() => setMinersSelection(null)}
        />
      ) : null}

      {retryTarget ? (
        <Dialog
          open
          testId="retry-rollout-dialog"
          title="Retry remaining updates?"
          subtitle={`Retry failed, skipped, or canceled work for ${pairLabel(retryTarget)} in ${retryTarget.channelName}, including earlier updates for this firmware assignment. This does not advance review gates.`}
          onDismiss={() => setLocalRetryTarget(null)}
          buttons={[
            { text: "Cancel", variant: variants.secondary, onClick: () => setLocalRetryTarget(null) },
            {
              text: "Retry remaining",
              testId: "confirm-rollout-retry",
              variant: variants.primary,
              onClick: handleRetry,
              disabled: isBusy,
            },
          ]}
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
