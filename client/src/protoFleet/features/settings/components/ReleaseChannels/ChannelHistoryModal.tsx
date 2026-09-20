import { type Timestamp, timestampMs } from "@bufbuild/protobuf/wkt";

import {
  canRollBack,
  pairKey,
  pairLabel,
  rollbackLabel,
  rolloutDeviceCounts,
  rolloutOutcomeLabel,
  rolloutStatusTone,
} from "./rolloutStatus";
import StatusChip from "./StatusChip";
import type { ChannelHistoryState } from "./useChannelHistory";
import { type Rollout, RolloutStatus } from "@/protoFleet/api/generated/rollout/v1/rollout_pb";
import {
  acknowledgeRollout,
  isRollbackAcknowledged,
  isRolloutSuperseded,
} from "@/protoFleet/api/rollbackAcknowledgements";
import type { ChannelView } from "@/protoFleet/api/useReleaseChannels";
import Button, { sizes as buttonSizes, variants } from "@/shared/components/Button";
import Modal, { sizes } from "@/shared/components/Modal";
import { formatTimestamp } from "@/shared/utils/formatTimestamp";

interface ChannelHistoryModalProps {
  channel: ChannelView;
  // This channel's rollouts, newest first (server order).
  rollouts: Rollout[];
  currentRollouts: readonly Rollout[];
  acknowledgedRollbacks: readonly Rollout[];
  historyState: ChannelHistoryState;
  onRetry: () => void;
  onView: (rollout: Rollout) => void;
  onRollback: (rollout: Rollout) => void;
  onClose: () => void;
}

const formatRolloutTimestamp = (timestamp?: Timestamp): string =>
  timestamp ? formatTimestamp(Math.floor(timestampMs(timestamp) / 1000)) : "—";

// Every update a channel has run, newest first, with a way back into each
// one's detail and a Roll back action while the entry is the pair's current
// assignment.
const ChannelHistoryModal = ({
  channel,
  rollouts,
  currentRollouts,
  acknowledgedRollbacks,
  historyState,
  onRetry,
  onView,
  onRollback,
  onClose,
}: ChannelHistoryModalProps) => {
  // Rolling an entry back reverses its lineage (A for an A-to-B update, or
  // clearing the firmware for a first assignment) while the entry is still
  // the pair's current assignment generation; older entries get no action.
  const generations = new Map(
    channel.modelGroups
      .filter((group) => !group.rollbackPending)
      .map((group) => [pairKey(group), group.assignmentGeneration]),
  );
  const observedRollouts = [...rollouts, ...currentRollouts];

  return (
    <Modal
      open
      size={sizes.large}
      title="Update history"
      description={channel.name}
      onDismiss={onClose}
      buttons={[{ text: "Done", variant: variants.primary, onClick: onClose }]}
    >
      {historyState.status === "loading" ? (
        <p role="status" className="py-4 text-text-primary-50">
          Loading update history…
        </p>
      ) : null}
      {historyState.status === "error" ? (
        <div role="alert" className="flex items-center justify-between gap-4 py-4">
          <p>{historyState.error || "Couldn't load update history"}</p>
          <Button text="Retry update history" variant={variants.secondary} onClick={onRetry} />
        </div>
      ) : null}
      {rollouts.length === 0 && historyState.status === "ready" ? (
        <div className="py-6 text-center text-text-primary-50">No updates for this channel yet.</div>
      ) : rollouts.length > 0 ? (
        <table className="w-full text-left text-200">
          <thead>
            <tr className="text-text-primary-50">
              <th className="py-1.5 pr-4 font-normal">Status</th>
              <th className="py-1.5 pr-4 font-normal">Manufacturer / model</th>
              <th className="py-1.5 pr-4 font-normal">Firmware</th>
              <th className="py-1.5 pr-4 font-normal">Progress</th>
              <th className="py-1.5 pr-4 font-normal">Started</th>
              <th className="py-1.5 pr-4 font-normal">Finished</th>
              <th className="py-1.5 font-normal">
                <span className="sr-only">Actions</span>
              </th>
            </tr>
          </thead>
          <tbody className="text-text-primary">
            {rollouts.map((snapshot) => {
              const rollout = acknowledgeRollout(snapshot, acknowledgedRollbacks, [channel], observedRollouts);
              const counts = rolloutDeviceCounts(rollout);
              const neutralCounts = [
                counts.skipped > 0 ? `${counts.skipped} skipped` : "",
                counts.excluded > 0 ? `${counts.excluded} excluded` : "",
              ].filter(Boolean);
              const progress =
                rollout.status === RolloutStatus.CANCELED && counts.total === 0 && neutralCounts.length === 0
                  ? "—"
                  : [
                      counts.total === 0 && neutralCounts.length > 0
                        ? "0 updated"
                        : `${counts.updated} of ${counts.total} updated`,
                      counts.failed > 0 ? `${counts.failed} failed` : "",
                      ...neutralCounts,
                    ]
                      .filter(Boolean)
                      .join(", ");
              const rollbackable =
                !isRollbackAcknowledged(rollout, acknowledgedRollbacks) &&
                !isRolloutSuperseded(rollout, [channel], observedRollouts) &&
                canRollBack(rollout, generations.get(pairKey(rollout)));
              return (
                <tr
                  key={rollout.id.toString()}
                  className="border-t border-border-5"
                  data-testid={`history-row-${rollout.id.toString()}`}
                >
                  <td className="py-2 pr-4">
                    <StatusChip label={rolloutOutcomeLabel(rollout)} tone={rolloutStatusTone(rollout)} />
                  </td>
                  <td className="py-2 pr-4">{pairLabel(rollout)}</td>
                  <td className="py-2 pr-4">{rollout.firmwareVersion}</td>
                  <td className="py-2 pr-4">{progress}</td>
                  <td className="py-2 pr-4">{formatRolloutTimestamp(rollout.createdAt)}</td>
                  <td className="py-2 pr-4">{formatRolloutTimestamp(rollout.finishedAt)}</td>
                  <td className="py-2 text-right">
                    <div className="flex justify-end gap-2">
                      <Button
                        variant={variants.secondary}
                        size={buttonSizes.compact}
                        text="View"
                        onClick={() => onView(rollout)}
                        testId={`history-view-${rollout.id.toString()}`}
                      />
                      {rollbackable ? (
                        <Button
                          variant={variants.secondary}
                          size={buttonSizes.compact}
                          text={rollbackLabel(rollout)}
                          onClick={() => onRollback(rollout)}
                          testId={`history-rollback-${rollout.id.toString()}`}
                        />
                      ) : null}
                    </div>
                  </td>
                </tr>
              );
            })}
          </tbody>
        </table>
      ) : null}
    </Modal>
  );
};

export default ChannelHistoryModal;
