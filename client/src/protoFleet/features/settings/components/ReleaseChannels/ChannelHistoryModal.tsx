import { type Timestamp, timestampMs } from "@bufbuild/protobuf/wkt";

import { rolloutDeviceCounts, rolloutOutcomeLabel, rolloutStatusTone } from "./rolloutStatus";
import StatusChip from "./StatusChip";
import { type ReleaseChannel, type Rollout, RolloutStatus } from "@/protoFleet/api/generated/rollout/v1/rollout_pb";
import Button, { sizes as buttonSizes, variants } from "@/shared/components/Button";
import Modal, { sizes } from "@/shared/components/Modal";
import { formatTimestamp } from "@/shared/utils/formatTimestamp";

interface ChannelHistoryModalProps {
  channel: ReleaseChannel;
  // This channel's rollouts, newest first (server order).
  rollouts: Rollout[];
  onView: (rollout: Rollout) => void;
  onRollback: (rollout: Rollout) => void;
  onClose: () => void;
}

const formatRolloutTimestamp = (timestamp?: Timestamp): string =>
  timestamp ? formatTimestamp(Math.floor(timestampMs(timestamp) / 1000)) : "—";

// Every update a channel has run, newest first, with a way back into each
// one's detail and a Roll back action for versions that are not the current
// assignment.
const ChannelHistoryModal = ({ channel, rollouts, onView, onRollback, onClose }: ChannelHistoryModalProps) => {
  // Rolling an entry back restores the assignment that was in place before
  // it (A for an A-to-B update). Entries with nothing before them, or whose
  // "before" is already the current assignment, get no rollback action.
  const assignedFileIds = new Map(channel.modelGroups.map((group) => [group.model, group.firmwareFileId]));

  return (
    <Modal
      open
      size={sizes.large}
      title="Update history"
      description={channel.name}
      onDismiss={onClose}
      buttons={[{ text: "Done", variant: variants.primary, onClick: onClose }]}
    >
      {rollouts.length === 0 ? (
        <div className="py-6 text-center text-text-primary-50">No updates for this channel yet.</div>
      ) : (
        <table className="w-full text-left text-200">
          <thead>
            <tr className="text-text-primary-50">
              <th className="py-1.5 pr-4 font-normal">Status</th>
              <th className="py-1.5 pr-4 font-normal">Model</th>
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
            {rollouts.map((rollout) => {
              const counts = rolloutDeviceCounts(rollout);
              const progress =
                rollout.status === RolloutStatus.CANCELED && counts.total === 0
                  ? "—"
                  : `${counts.updated} of ${counts.total} updated${counts.failed > 0 ? `, ${counts.failed} failed` : ""}`;
              const canRollBack =
                rollout.previousFirmwareFileId !== "" &&
                assignedFileIds.get(rollout.model) !== rollout.previousFirmwareFileId;
              return (
                <tr
                  key={rollout.id.toString()}
                  className="border-t border-border-5"
                  data-testid={`history-row-${rollout.id.toString()}`}
                >
                  <td className="py-2 pr-4">
                    <StatusChip label={rolloutOutcomeLabel(rollout)} tone={rolloutStatusTone(rollout)} />
                  </td>
                  <td className="py-2 pr-4">{rollout.model}</td>
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
                      {canRollBack ? (
                        <Button
                          variant={variants.secondary}
                          size={buttonSizes.compact}
                          text={`Roll back to ${rollout.previousFirmwareVersion}`}
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
      )}
    </Modal>
  );
};

export default ChannelHistoryModal;
