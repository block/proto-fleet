import { type Timestamp, timestampMs } from "@bufbuild/protobuf/wkt";

import { rolloutStatusLabels, rolloutStatusTone } from "./rolloutStatus";
import StatusChip from "./StatusChip";
import {
  type Rollout,
  RolloutDeviceState,
  type RolloutLane,
  RolloutStatus,
} from "@/protoFleet/api/generated/rollout/v1/rollout_pb";
import { variants } from "@/shared/components/Button";
import Modal, { sizes } from "@/shared/components/Modal";
import { formatTimestamp } from "@/shared/utils/formatTimestamp";

interface LaneHistoryModalProps {
  lane: RolloutLane;
  // This lane's rollouts, newest first (server order).
  rollouts: Rollout[];
  onClose: () => void;
}

const formatRolloutTimestamp = (timestamp?: Timestamp): string =>
  timestamp ? formatTimestamp(Math.floor(timestampMs(timestamp) / 1000)) : "—";

const LaneHistoryModal = ({ lane, rollouts, onClose }: LaneHistoryModalProps) => (
  <Modal
    open
    size={sizes.large}
    title="Rollout history"
    description={lane.name}
    onDismiss={onClose}
    buttons={[{ text: "Done", variant: variants.primary, onClick: onClose }]}
  >
    {rollouts.length === 0 ? (
      <div className="py-6 text-center text-text-primary-50">No rollouts for this lane yet.</div>
    ) : (
      <table className="w-full text-left text-200">
        <thead>
          <tr className="text-text-primary-50">
            <th className="py-1.5 pr-4 font-normal">Status</th>
            <th className="py-1.5 pr-4 font-normal">Model</th>
            <th className="py-1.5 pr-4 font-normal">Firmware</th>
            <th className="py-1.5 pr-4 font-normal">Progress</th>
            <th className="py-1.5 pr-4 font-normal">Started</th>
            <th className="py-1.5 font-normal">Finished</th>
          </tr>
        </thead>
        <tbody className="text-text-primary">
          {rollouts.map((rollout) => {
            const total = rollout.devices.length;
            const updated = rollout.devices.filter((device) => device.state === RolloutDeviceState.UPDATED).length;
            // Progress for a canceled rollout is unknowable after the fact:
            // its targets have been re-counted against live lane membership.
            const progress = rollout.status === RolloutStatus.CANCELED ? "—" : `${updated}/${total} updated`;
            return (
              <tr key={rollout.id.toString()} className="border-t border-border-5">
                <td className="py-2 pr-4">
                  <StatusChip label={rolloutStatusLabels[rollout.status]} tone={rolloutStatusTone(rollout.status)} />
                </td>
                <td className="py-2 pr-4">{rollout.model}</td>
                <td className="py-2 pr-4">{rollout.firmwareVersion}</td>
                <td className="py-2 pr-4">{progress}</td>
                <td className="py-2 pr-4">{formatRolloutTimestamp(rollout.createdAt)}</td>
                <td className="py-2">{formatRolloutTimestamp(rollout.finishedAt)}</td>
              </tr>
            );
          })}
        </tbody>
      </table>
    )}
  </Modal>
);

export default LaneHistoryModal;
